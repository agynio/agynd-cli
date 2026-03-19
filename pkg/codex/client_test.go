package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"
)

type notificationRecorder struct {
	turnCompleted int
	notified      string
}

func (n *notificationRecorder) OnTurnStarted(*TurnStartedNotification)                        {}
func (n *notificationRecorder) OnTurnCompleted(*TurnCompletedNotification)                    { n.turnCompleted++ }
func (n *notificationRecorder) OnItemStarted(*ItemStartedNotification)                        {}
func (n *notificationRecorder) OnItemCompleted(*ItemCompletedNotification)                    {}
func (n *notificationRecorder) OnAgentMessageDelta(*AgentMessageDeltaNotification)            {}
func (n *notificationRecorder) OnCommandOutputDelta(*CommandExecutionOutputDeltaNotification) {}
func (n *notificationRecorder) OnFileChangeDelta(*FileChangeOutputDeltaNotification)          {}
func (n *notificationRecorder) OnTokenUsageUpdated(*ThreadTokenUsageUpdatedNotification)      {}
func (n *notificationRecorder) OnError(*ErrorNotification)                                    {}
func (n *notificationRecorder) OnNotification(method string, _ json.RawMessage)               { n.notified = method }

type approvalRecorder struct {
	called  bool
	command string
	cwd     string
}

func (a *approvalRecorder) OnCommandApproval(_ context.Context, params *CommandExecutionRequestApprovalParams) (*CommandExecutionRequestApprovalResponse, error) {
	a.called = true
	if params != nil {
		a.command = params.Command
		a.cwd = params.Cwd
	}
	return &CommandExecutionRequestApprovalResponse{Decision: ApprovalDecisionAccept}, nil
}

func (a *approvalRecorder) OnFileChangeApproval(_ context.Context, _ *FileChangeRequestApprovalParams) (*FileChangeRequestApprovalResponse, error) {
	return nil, errors.New("unexpected file change approval")
}

func (a *approvalRecorder) OnPermissionsApproval(_ context.Context, _ *PermissionsRequestApprovalParams) (*PermissionsRequestApprovalResponse, error) {
	return nil, errors.New("unexpected permissions approval")
}

func (a *approvalRecorder) OnToolUserInput(_ context.Context, _ *ToolRequestUserInputParams) (*ToolRequestUserInputResponse, error) {
	return nil, errors.New("unexpected tool user input")
}

func (a *approvalRecorder) OnDynamicToolCall(_ context.Context, _ *DynamicToolCallParams) (*DynamicToolCallResponse, error) {
	return nil, errors.New("unexpected dynamic tool call")
}

func TestBuildEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("PATH", "/bin")
	t.Setenv("HOME", "/tmp")
	if _, err := buildEnv(nil); err == nil {
		t.Fatalf("expected missing OPENAI_API_KEY error")
	}

	t.Setenv("OPENAI_API_KEY", "key")
	env, err := buildEnv(map[string]string{"CUSTOM": "value"})
	if err != nil {
		t.Fatalf("build env: %v", err)
	}
	values := envSliceToMap(env)
	if values["CUSTOM"] != "value" {
		t.Fatalf("expected custom env value, got %q", values["CUSTOM"])
	}
}

func TestSendNotification(t *testing.T) {
	buf := &bytes.Buffer{}
	transport := newJSONLTransport(bytes.NewBuffer(nil), buf)
	client := &Client{transport: transport}
	if err := client.sendNotification("initialized", nil); err != nil {
		t.Fatalf("send notification: %v", err)
	}
	payload := parseJSONLine(t, buf)
	if payload["method"] != "initialized" {
		t.Fatalf("unexpected method: %v", payload["method"])
	}
	if _, ok := payload["params"]; ok {
		t.Fatalf("unexpected params for nil payload")
	}

	buf.Reset()
	if err := client.sendNotification("custom", map[string]string{"name": "codex"}); err != nil {
		t.Fatalf("send notification with params: %v", err)
	}
	payload = parseJSONLine(t, buf)
	params, ok := payload["params"].(map[string]any)
	if !ok {
		t.Fatalf("expected params map")
	}
	if params["name"] != "codex" {
		t.Fatalf("unexpected params content: %v", params["name"])
	}
}

func TestHandleNotification(t *testing.T) {
	handler := &notificationRecorder{}
	client := &Client{notificationHandler: handler}
	params := json.RawMessage(`{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[]}}`)
	client.handleNotification(rpcMessage{Method: "turn/completed", Params: params})
	if handler.turnCompleted != 1 {
		t.Fatalf("expected turn completed handler")
	}

	var logBuf bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(original)
	handler.notified = ""
	client.handleNotification(rpcMessage{Method: "turn/completed", Params: json.RawMessage("{invalid")})
	if !strings.Contains(logBuf.String(), "failed to decode turn/completed") {
		t.Fatalf("expected log entry for decode error")
	}
	if handler.notified != "turn/completed" {
		t.Fatalf("expected fallback notification handler")
	}
}

func TestHandleServerRequest(t *testing.T) {
	buf := &bytes.Buffer{}
	transport := newJSONLTransport(bytes.NewBuffer(nil), buf)
	approval := &approvalRecorder{}
	client := &Client{transport: transport, approvalHandler: approval, ctx: context.Background()}
	requestID := int64(12)
	params := json.RawMessage(`{"command":"ls","cwd":"/tmp"}`)
	client.handleServerRequest(rpcMessage{ID: &requestID, Method: "item/commandExecution/requestApproval", Params: params})
	if !approval.called {
		t.Fatalf("expected approval handler to be called")
	}
	if approval.command != "ls" || approval.cwd != "/tmp" {
		t.Fatalf("unexpected approval params: %s %s", approval.command, approval.cwd)
	}
	line := strings.TrimSpace(buf.String())
	var resp struct {
		ID     int64                                   `json:"id"`
		Result CommandExecutionRequestApprovalResponse `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ID != requestID {
		t.Fatalf("unexpected response id: %d", resp.ID)
	}
	if resp.Result.Decision != ApprovalDecisionAccept {
		t.Fatalf("unexpected decision: %s", resp.Result.Decision)
	}
}

func parseJSONLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatalf("expected JSON output")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	return payload
}

func envSliceToMap(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	return values
}
