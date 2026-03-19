package codexbridge

import "sync"

type TurnResult struct {
	ThreadID string
	TurnID   string
	Message  string
	Err      error
}

type TurnTracker struct {
	mu      sync.Mutex
	waiters map[string]chan TurnResult
	pending map[string]TurnResult
}

func NewTurnTracker() *TurnTracker {
	return &TurnTracker{
		waiters: make(map[string]chan TurnResult),
		pending: make(map[string]TurnResult),
	}
}

func (t *TurnTracker) Register(turnID string) <-chan TurnResult {
	ch := make(chan TurnResult, 1)
	t.mu.Lock()
	if result, ok := t.pending[turnID]; ok {
		delete(t.pending, turnID)
		t.mu.Unlock()
		ch <- result
		close(ch)
		return ch
	}
	t.waiters[turnID] = ch
	t.mu.Unlock()
	return ch
}

func (t *TurnTracker) Cancel(turnID string) {
	t.mu.Lock()
	delete(t.waiters, turnID)
	delete(t.pending, turnID)
	t.mu.Unlock()
}

func (t *TurnTracker) Notify(result TurnResult) {
	t.mu.Lock()
	ch, ok := t.waiters[result.TurnID]
	if ok {
		delete(t.waiters, result.TurnID)
	}
	if !ok {
		t.pending[result.TurnID] = result
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()
	ch <- result
	close(ch)
}
