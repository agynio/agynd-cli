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
	got, err := ExtractFinalAnswer(turn)
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
	got, err := ExtractFinalAnswer(turn)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "last response" {
		t.Fatalf("expected fallback to last agent message, got %q", got)
	}
}

func TestExtractFinalAnswerEmptyTurn(t *testing.T) {
	turn := codex.Turn{ID: "turn-empty"}
	got, err := ExtractFinalAnswer(turn)
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
	got, err := ExtractFinalAnswer(turn)
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

func TestBridgeAccumulatesItems(t *testing.T) {
	tracker := NewTurnTracker()
	bridge := New(tracker)
	ch := tracker.Register("turn-acc")

	bridge.OnItemCompleted(&codex.ItemCompletedNotification{
		ThreadID: "thread-1",
		TurnID:   "turn-acc",
		Item:     agentMessageItem("Hi! How are you?", nil),
	})

	bridge.OnTurnCompleted(&codex.TurnCompletedNotification{
		ThreadID: "thread-1",
		Turn: codex.Turn{
			ID:     "turn-acc",
			Status: codex.TurnStatusCompleted,
			Items:  nil,
		},
	})

	result := receiveResult(t, ch)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Message != "Hi! How are you?" {
		t.Fatalf("unexpected message: %q", result.Message)
	}
	assertClosed(t, ch)
}

func TestBridgeUsesAgentMessageDeltas(t *testing.T) {
	tracker := NewTurnTracker()
	bridge := New(tracker)
	ch := tracker.Register("turn-delta")

	bridge.OnAgentMessageDelta(&codex.AgentMessageDeltaNotification{
		ThreadID: "thread-1",
		TurnID:   "turn-delta",
		ItemID:   "item-agent-1",
		Delta:    "Hello",
	})
	bridge.OnAgentMessageDelta(&codex.AgentMessageDeltaNotification{
		ThreadID: "thread-1",
		TurnID:   "turn-delta",
		ItemID:   "item-agent-1",
		Delta:    " from delta",
	})

	bridge.OnTurnCompleted(&codex.TurnCompletedNotification{
		ThreadID: "thread-1",
		Turn: codex.Turn{
			ID:     "turn-delta",
			Status: codex.TurnStatusCompleted,
			Items:  nil,
		},
	})

	result := receiveResult(t, ch)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Message != "Hello from delta" {
		t.Fatalf("unexpected message: %q", result.Message)
	}
	assertClosed(t, ch)
}
