package codexbridge

import (
	"strings"
	"testing"

	codex "github.com/agynio/codex-sdk-go"
)

func agentMessageItem(text string, phase *codex.MessagePhase) codex.ThreadItem {
	return codex.ThreadItem{
		AgentMessage: &codex.AgentMessageThreadItem{
			Text:  text,
			Phase: phase,
		},
	}
}

func TestExtractFinalAnswerFinalPhase(t *testing.T) {
	phase := codex.MessagePhaseFinalAnswer
	turn := codex.Turn{
		ID: "turn-final",
		Items: []codex.ThreadItem{
			agentMessageItem("final response", &phase),
		},
	}
	got, err := extractFinalAnswer(turn)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "final response" {
		t.Fatalf("unexpected final answer: %q", got)
	}
}

func TestExtractFinalAnswerFallback(t *testing.T) {
	turn := codex.Turn{
		ID: "turn-fallback",
		Items: []codex.ThreadItem{
			agentMessageItem("draft", nil),
			agentMessageItem("last response", nil),
		},
	}
	got, err := extractFinalAnswer(turn)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "last response" {
		t.Fatalf("expected fallback to last agent message, got %q", got)
	}
}

func TestExtractFinalAnswerEmptyTurn(t *testing.T) {
	turn := codex.Turn{ID: "turn-empty"}
	got, err := extractFinalAnswer(turn)
	if err == nil {
		t.Fatal("expected error for empty turn")
	}
	if got != "" {
		t.Fatalf("expected empty answer, got %q", got)
	}
	if !strings.Contains(err.Error(), "missing agent message") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractFinalAnswerNoAgentMessages(t *testing.T) {
	turn := codex.Turn{
		ID: "turn-no-agent",
		Items: []codex.ThreadItem{
			{
				UserMessage: &codex.UserMessageThreadItem{ID: "user-1"},
			},
		},
	}
	got, err := extractFinalAnswer(turn)
	if err == nil {
		t.Fatal("expected error for missing agent messages")
	}
	if got != "" {
		t.Fatalf("expected empty answer, got %q", got)
	}
	if !strings.Contains(err.Error(), "missing agent message") {
		t.Fatalf("unexpected error: %v", err)
	}
}
