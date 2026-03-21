//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agynio/codex-sdk-go"
)

const testLLMURL = "https://testllm.dev/v1/org/agynio/suite/codex/responses"

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

type testLLMResponse struct {
	ID     string            `json:"id"`
	Output []json.RawMessage `json:"output"`
}

type responseRef struct {
	ID string `json:"id"`
}

type responseCreatedEvent struct {
	Type     string      `json:"type"`
	Response responseRef `json:"response"`
}

type responseOutputItemDoneEvent struct {
	Type        string          `json:"type"`
	OutputIndex int             `json:"output_index"`
	Item        json.RawMessage `json:"item"`
}

type responseOutputItemAddedEvent struct {
	Type        string          `json:"type"`
	OutputIndex int             `json:"output_index"`
	Item        json.RawMessage `json:"item"`
}

type responseOutputTextDeltaEvent struct {
	Type         string `json:"type"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Delta        string `json:"delta"`
}

type responseCompletedEvent struct {
	Type     string         `json:"type"`
	Response map[string]any `json:"response"`
}

type responseOutputItem struct {
	Content []responseOutputContent `json:"content"`
}

type responseOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func TestCodexClientHelloResponse(t *testing.T) {
	proxyURL := startTestProxy(t)
	codexHome := t.TempDir()
	writeCodexConfig(t, codexHome, proxyURL)

	handler := &turnCompletedHandler{completed: make(chan *codex.TurnCompletedNotification, 1)}
	ctx := context.Background()
	client, err := codex.NewClient(ctx,
		codex.WithBinary("codex"),
		codex.WithArgs("app-server"),
		codex.WithWorkDir(t.TempDir()),
		codex.WithEnv(map[string]string{
			"CODEX_HOME":     codexHome,
			"OPENAI_API_KEY": "test-key",
		}),
		codex.WithNotificationHandler(handler),
		codex.WithApprovalHandler(codex.AutoApprovalHandler{}),
		codex.WithClientInfo("e2e-test", "0.1.0"),
	)
	if err != nil {
		t.Fatalf("start codex client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
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

func startTestProxy(t *testing.T) string {
	t.Helper()

	client := &http.Client{Timeout: 30 * time.Second}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	mux.HandleFunc("/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request body", http.StatusBadRequest)
			return
		}
		if err := r.Body.Close(); err != nil {
			http.Error(w, "close request body", http.StatusBadRequest)
			return
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "invalid json payload", http.StatusBadRequest)
			return
		}
		if err := normalizeInputContent(payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		normalized, err := json.Marshal(payload)
		if err != nil {
			http.Error(w, "normalize payload", http.StatusBadRequest)
			return
		}

		forwardReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, testLLMURL, bytes.NewReader(normalized))
		if err != nil {
			http.Error(w, "build upstream request", http.StatusInternalServerError)
			return
		}
		forwardReq.Header.Set("Content-Type", "application/json")
		if auth := r.Header.Get("Authorization"); auth != "" {
			forwardReq.Header.Set("Authorization", auth)
		}

		resp, err := client.Do(forwardReq)
		if err != nil {
			http.Error(w, "upstream request failed", http.StatusBadGateway)
			return
		}
		respBody, err := io.ReadAll(resp.Body)
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			http.Error(w, "read upstream response", http.StatusBadGateway)
			return
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			http.Error(w, fmt.Sprintf("upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))), http.StatusBadGateway)
			return
		}

		var llmResp testLLMResponse
		if err := json.Unmarshal(respBody, &llmResp); err != nil {
			http.Error(w, "invalid upstream response", http.StatusBadGateway)
			return
		}
		var llmRespPayload map[string]any
		if err := json.Unmarshal(respBody, &llmRespPayload); err != nil {
			http.Error(w, "invalid upstream response", http.StatusBadGateway)
			return
		}
		if llmResp.ID == "" {
			http.Error(w, "upstream response missing id", http.StatusBadGateway)
			return
		}
		parsedOutput := make([]responseOutputItem, len(llmResp.Output))
		for index, item := range llmResp.Output {
			if err := json.Unmarshal(item, &parsedOutput[index]); err != nil {
				http.Error(w, "invalid upstream output", http.StatusBadGateway)
				return
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		if err := writeEvent(w, "response.created", responseCreatedEvent{
			Type:     "response.created",
			Response: responseRef{ID: llmResp.ID},
		}); err != nil {
			return
		}
		for index, item := range llmResp.Output {
			if err := writeEvent(w, "response.output_item.added", responseOutputItemAddedEvent{
				Type:        "response.output_item.added",
				OutputIndex: index,
				Item:        item,
			}); err != nil {
				return
			}
			for contentIndex, content := range parsedOutput[index].Content {
				if content.Type != "output_text" {
					continue
				}
				if err := writeEvent(w, "response.output_text.delta", responseOutputTextDeltaEvent{
					Type:         "response.output_text.delta",
					OutputIndex:  index,
					ContentIndex: contentIndex,
					Delta:        content.Text,
				}); err != nil {
					return
				}
			}
			if err := writeEvent(w, "response.output_item.done", responseOutputItemDoneEvent{
				Type:        "response.output_item.done",
				OutputIndex: index,
				Item:        item,
			}); err != nil {
				return
			}
		}
		llmRespPayload["usage"] = map[string]any{
			"input_tokens":  0,
			"output_tokens": 0,
			"total_tokens":  0,
		}
		_ = writeEvent(w, "response.completed", responseCompletedEvent{
			Type:     "response.completed",
			Response: llmRespPayload,
		})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

func normalizeInputContent(payload map[string]any) error {
	inputRaw, ok := payload["input"]
	if !ok {
		return nil
	}
	inputItems, ok := inputRaw.([]any)
	if !ok {
		return nil
	}
	if len(inputItems) == 0 {
		return nil
	}
	message, ok := inputItems[len(inputItems)-1].(map[string]any)
	if !ok {
		return fmt.Errorf("input item must be object")
	}
	contentRaw, ok := message["content"]
	if ok {
		contentItems, ok := contentRaw.([]any)
		if ok {
			text, err := joinContentText(contentItems)
			if err != nil {
				return err
			}
			message["content"] = text
		}
	}
	payload["input"] = []any{message}
	return nil
}

func joinContentText(contentItems []any) (string, error) {
	var builder strings.Builder
	for _, item := range contentItems {
		entry, ok := item.(map[string]any)
		if !ok {
			return "", fmt.Errorf("content item must be object")
		}
		textValue, ok := entry["text"]
		if !ok {
			return "", fmt.Errorf("content item missing text")
		}
		text, ok := textValue.(string)
		if !ok {
			return "", fmt.Errorf("content item text must be string")
		}
		builder.WriteString(text)
	}
	return builder.String(), nil
}

func writeEvent(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func writeCodexConfig(t *testing.T, dir, baseURL string) {
	t.Helper()
	configPath := filepath.Join(dir, "config.toml")
	config := fmt.Sprintf(`model = "simple-hello"
approval_policy = "never"
model_provider = "testllm"

[model_providers.testllm]
name = "Test LLM"
base_url = "%s/v1"
wire_api = "responses"
request_max_retries = 0
stream_max_retries = 0
supports_websockets = false
`, baseURL)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
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
