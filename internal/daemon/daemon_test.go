package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	"github.com/google/uuid"
)

const testAgentID = "550e8400-e29b-41d4-a716-446655440000"

func testConfig(sdk string) config.Config {
	binary := SDKCodex
	switch sdk {
	case SDKAgn:
		binary = SDKAgn
	case SDKClaude:
		binary = SDKClaude
	}
	return config.Config{
		AgentID:        uuid.MustParse(testAgentID),
		GatewayAddress: "127.0.0.1:0",
		LLMBaseURL:     "https://llm.example",
		SDK:            sdk,
		AgentBinary:    binary,
		WorkDir:        "/tmp",
	}
}

func TestBuildInputText(t *testing.T) {
	message := platform.Message{
		ID:   "msg-1",
		Body: " hello ",
	}
	got, err := buildInput(message)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "hello" {
		t.Fatalf("expected trimmed text, got %q", got)
	}
}

func TestBuildInputFilesOnly(t *testing.T) {
	message := platform.Message{
		ID:      "msg-2",
		FileIDs: []string{"file-a", "file-b"},
	}
	got, err := buildInput(message)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "agyn://file/file-a\nagyn://file/file-b" {
		t.Fatalf("unexpected file-only input: %q", got)
	}
}

func TestBuildInputTextWithFiles(t *testing.T) {
	message := platform.Message{
		ID:      "msg-3",
		Body:    " status ",
		FileIDs: []string{"file-a", "file-b"},
	}
	got, err := buildInput(message)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "status\nagyn://file/file-a\nagyn://file/file-b" {
		t.Fatalf("unexpected text-with-files input: %q", got)
	}
}

func TestBuildInputEmpty(t *testing.T) {
	message := platform.Message{ID: "msg-4"}
	_, err := buildInput(message)
	if err == nil {
		t.Fatal("expected error for empty message")
	}
	if !strings.Contains(err.Error(), "has no content") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewUnknownSDK(t *testing.T) {
	_, err := New(context.Background(), testConfig("unknown"), "test")
	if err == nil {
		t.Fatal("expected error for unknown sdk")
	}
	if !strings.Contains(err.Error(), "unknown sdk") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewCodexDispatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := New(ctx, testConfig(SDKCodex), "test")
	if err == nil {
		t.Fatal("expected error for codex dispatch")
	}
	if strings.Contains(err.Error(), "not yet supported") || strings.Contains(err.Error(), "unknown sdk") {
		t.Fatalf("unexpected dispatch error: %v", err)
	}
}

func TestNewAgnDispatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := New(ctx, testConfig(SDKAgn), "test")
	if err == nil {
		t.Fatal("expected error for agn dispatch")
	}
	if strings.Contains(err.Error(), "not yet supported") || strings.Contains(err.Error(), "unknown sdk") {
		t.Fatalf("unexpected dispatch error: %v", err)
	}
}

func TestNewClaudeDispatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := New(ctx, testConfig(SDKClaude), "test")
	if err == nil {
		t.Fatal("expected error for claude dispatch")
	}
	if strings.Contains(err.Error(), "not yet supported") || strings.Contains(err.Error(), "unknown sdk") {
		t.Fatalf("unexpected dispatch error: %v", err)
	}
}
