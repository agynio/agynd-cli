//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agynio/codex-sdk-go"
)

const testLLMBaseURL = "https://testllm.dev/v1/org/agynio/suite/codex"

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

	target, err := url.Parse(testLLMBaseURL)
	if err != nil {
		t.Fatalf("parse testllm base url: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(r *http.Request) {
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		r.Host = target.Host
		relativePath := strings.TrimPrefix(r.URL.Path, "/v1")
		if relativePath == "" {
			r.URL.Path = target.Path
			return
		}
		r.URL.Path = strings.TrimSuffix(target.Path, "/") + relativePath
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	mux.Handle("/v1/", proxy)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
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
