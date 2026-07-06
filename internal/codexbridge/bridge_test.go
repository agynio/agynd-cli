package codexbridge

import (
	"errors"
	"strings"
	"testing"
	"time"

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

func TestBridgeTerminalErrorNotificationCompletesTurn(t *testing.T) {
	tracker := NewTurnTracker()
	bridge := New(tracker)
	ch := tracker.Register("turn-error")
	details := "body stream ended"

	bridge.OnError(&codex.ErrorNotification{
		ThreadID: "thread-1",
		TurnID:   "turn-error",
		Error: codex.TurnError{
			Message:           "failed to read body",
			AdditionalDetails: &details,
		},
		WillRetry: false,
	})

	result := receiveResult(t, ch)
	if result.ThreadID != "thread-1" {
		t.Fatalf("unexpected thread id: %q", result.ThreadID)
	}
	var notificationErr *ErrorNotificationError
	if !errors.As(result.Err, &notificationErr) {
		t.Fatalf("expected ErrorNotificationError, got %T", result.Err)
	}
	for _, expected := range []string{"failed to read body", "turn-error", "thread-1", details} {
		if !strings.Contains(result.Err.Error(), expected) {
			t.Fatalf("expected %q in error: %v", expected, result.Err)
		}
	}
	assertClosed(t, ch)
}

func TestBridgeTerminalErrorNotificationWithoutTurnCompletesActiveTurn(t *testing.T) {
	tracker := NewTurnTracker()
	bridge := New(tracker)
	ch := tracker.Register("turn-active")

	bridge.OnError(&codex.ErrorNotification{
		ThreadID:  "thread-1",
		Error:     codex.TurnError{Message: "failed to read body"},
		WillRetry: false,
	})

	result := receiveResult(t, ch)
	if result.TurnID != "turn-active" {
		t.Fatalf("expected active turn id, got %q", result.TurnID)
	}
	var notificationErr *ErrorNotificationError
	if !errors.As(result.Err, &notificationErr) {
		t.Fatalf("expected ErrorNotificationError, got %T", result.Err)
	}
	if notificationErr.TurnID != "turn-active" {
		t.Fatalf("expected error turn id to be set, got %q", notificationErr.TurnID)
	}
	assertClosed(t, ch)
}

func TestBridgeRetryingErrorNotificationDoesNotCompleteTurn(t *testing.T) {
	tracker := NewTurnTracker()
	bridge := New(tracker)
	ch := tracker.Register("turn-retry")

	bridge.OnError(&codex.ErrorNotification{
		ThreadID:  "thread-1",
		TurnID:    "turn-retry",
		Error:     codex.TurnError{Message: "temporary provider failure"},
		WillRetry: true,
	})

	select {
	case result := <-ch:
		t.Fatalf("unexpected turn completion: %+v", result)
	case <-time.After(25 * time.Millisecond):
	}
	tracker.Cancel("turn-retry")
}
