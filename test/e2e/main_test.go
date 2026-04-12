//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agynio/codex-sdk-go"
)

type turnCompletedHandler struct {
	codex.NopNotificationHandler
	completed chan *codex.TurnCompletedNotification
}

func (h *turnCompletedHandler) OnTurnCompleted(notification *codex.TurnCompletedNotification) {
	select {
	case h.completed <- notification:
	default:
	}
}

func TestCodexClientHelloResponse(t *testing.T) {
	codexHome := t.TempDir()
	writeCodexConfig(t, codexHome)

	workDir, err := os.MkdirTemp("", "codex-workdir-")
	if err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	handler := &turnCompletedHandler{completed: make(chan *codex.TurnCompletedNotification, 1)}
	ctx := context.Background()
	client, err := codex.NewClient(ctx,
		codex.WithBinary("codex"),
		codex.WithArgs("app-server"),
		codex.WithWorkDir(workDir),
		codex.WithEnv(map[string]string{
			"CODEX_HOME":     codexHome,
			"OPENAI_API_KEY": "test-key",
		}),
		codex.WithNotificationHandler(handler),
		codex.WithApprovalHandler(codex.AutoApprovalHandler{}),
		codex.WithClientInfo("e2e-test", "0.1.0"),
	)
	if err != nil {
		_ = os.RemoveAll(workDir)
		t.Fatalf("start codex client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		if err := removeAllWithRetry(workDir, 10, 200*time.Millisecond); err != nil {
			t.Fatalf("cleanup workdir: %v", err)
		}
	})

	model := "simple-hello"
	threadResp, err := client.StartThread(ctx, &codex.ThreadStartParams{Model: &model})
	if err != nil {
		t.Fatalf("thread start: %v", err)
	}

	_, err = client.StartTurn(ctx, &codex.TurnStartParams{
		ThreadID: threadResp.Thread.ID,
		Input:    []codex.UserInput{codex.NewTextUserInput("hello")},
	})
	if err != nil {
		t.Fatalf("turn start: %v", err)
	}

	select {
	case notification := <-handler.completed:
		if notification.Turn.Error != nil {
			t.Fatalf("turn error: %s", notification.Turn.Error.Message)
		}
		threadState, err := client.ReadThread(ctx, &codex.ThreadReadParams{
			ThreadID:     threadResp.Thread.ID,
			IncludeTurns: true,
		})
		if err != nil {
			t.Fatalf("thread read: %v", err)
		}
		if len(threadState.Thread.Turns) == 0 {
			t.Fatalf("thread has no turns")
		}
		turn := threadState.Thread.Turns[len(threadState.Thread.Turns)-1]
		message, ok := findAgentMessage(turn.Items)
		if !ok {
			t.Fatalf("missing agent message in completed turn: %s", describeTurnItems(turn.Items))
		}
		if message != "Hi! How are you?" {
			t.Fatalf("unexpected agent message: %q", message)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("timeout waiting for turn completion")
	}
}

func writeCodexConfig(t *testing.T, dir string) {
	t.Helper()
	configPath := filepath.Join(dir, "config.toml")
	config := `model = "simple-hello"
approval_policy = "never"
model_provider = "testllm"

[model_providers.testllm]
name = "Test LLM"
base_url = "https://testllm.dev/v1/org/agynio/suite/codex"
wire_api = "responses"
request_max_retries = 0
stream_max_retries = 0
supports_websockets = false
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
}

func removeAllWithRetry(path string, attempts int, delay time.Duration) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := os.RemoveAll(path); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(delay)
	}
	return lastErr
}

func findAgentMessage(items []codex.ThreadItem) (string, bool) {
	for _, item := range items {
		if item.AgentMessage != nil {
			return item.AgentMessage.Text, true
		}
	}
	return "", false
}

func describeTurnItems(items []codex.ThreadItem) string {
	data, err := json.Marshal(items)
	if err != nil {
		return fmt.Sprintf("marshal turn items: %v", err)
	}
	return string(data)
}
