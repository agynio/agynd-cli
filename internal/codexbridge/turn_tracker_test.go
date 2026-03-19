package codexbridge

import (
	"sync"
	"testing"
	"time"
)

func receiveResult(t *testing.T, ch <-chan TurnResult) TurnResult {
	t.Helper()
	select {
	case result, ok := <-ch:
		if !ok {
			t.Fatal("channel closed without result")
		}
		return result
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for result")
	}
	return TurnResult{}
}

func assertClosed(t *testing.T, ch <-chan TurnResult) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for channel close")
	}
}

func TestTurnTrackerRegisterThenNotify(t *testing.T) {
	tracker := NewTurnTracker()
	ch := tracker.Register("turn-1")
	result := TurnResult{ThreadID: "thread-1", TurnID: "turn-1", Message: "ok"}
	tracker.Notify(result)
	got := receiveResult(t, ch)
	if got != result {
		t.Fatalf("unexpected result: %+v", got)
	}
	assertClosed(t, ch)
}

func TestTurnTrackerNotifyBeforeRegister(t *testing.T) {
	tracker := NewTurnTracker()
	result := TurnResult{ThreadID: "thread-2", TurnID: "turn-2", Message: "done"}
	tracker.Notify(result)
	ch := tracker.Register("turn-2")
	got := receiveResult(t, ch)
	if got != result {
		t.Fatalf("unexpected result: %+v", got)
	}
	assertClosed(t, ch)
}

func TestTurnTrackerCancelDropsPending(t *testing.T) {
	tracker := NewTurnTracker()
	result := TurnResult{ThreadID: "thread-3", TurnID: "turn-3", Message: "ignored"}
	tracker.Notify(result)
	tracker.Cancel("turn-3")
	ch := tracker.Register("turn-3")
	select {
	case got := <-ch:
		t.Fatalf("unexpected result after cancel: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTurnTrackerMultipleConcurrentTurns(t *testing.T) {
	tracker := NewTurnTracker()
	turnIDs := []string{"turn-a", "turn-b", "turn-c"}
	channels := make(map[string]<-chan TurnResult, len(turnIDs))
	for _, id := range turnIDs {
		channels[id] = tracker.Register(id)
	}

	var wg sync.WaitGroup
	for _, id := range turnIDs {
		turnID := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.Notify(TurnResult{ThreadID: "thread", TurnID: turnID, Message: turnID + "-message"})
		}()
	}
	wg.Wait()

	for _, id := range turnIDs {
		got := receiveResult(t, channels[id])
		if got.TurnID != id {
			t.Fatalf("expected turn id %s, got %s", id, got.TurnID)
		}
		if got.Message != id+"-message" {
			t.Fatalf("unexpected message for %s: %q", id, got.Message)
		}
		assertClosed(t, channels[id])
	}
}
