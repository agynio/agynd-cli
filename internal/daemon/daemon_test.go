package daemon

import (
	"strings"
	"testing"

	"github.com/agynio/agynd-cli/internal/platform"
)

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
	if got != "Received files: file-a, file-b" {
		t.Fatalf("unexpected file-only input: %q", got)
	}
}

func TestBuildInputEmpty(t *testing.T) {
	message := platform.Message{ID: "msg-3"}
	_, err := buildInput(message)
	if err == nil {
		t.Fatal("expected error for empty message")
	}
	if !strings.Contains(err.Error(), "has no content") {
		t.Fatalf("unexpected error: %v", err)
	}
}
