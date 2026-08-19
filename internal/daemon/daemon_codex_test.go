package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	"github.com/agynio/agynd-cli/internal/codexbridge"
	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	codex "github.com/agynio/codex-sdk-go"
	"github.com/google/uuid"
)

type fakeCodexClient struct {
	mu                sync.Mutex
	startThreadCalls  int
	resumeThreadCalls int
	startParams       *codex.ThreadStartParams
	resumeParams      *codex.ThreadResumeParams
	startTurnCtx      context.Context
	startTurnErr      error
	readThreadResp    *codex.ThreadReadResponse
	readThreadQueue   []*codex.ThreadReadResponse
	readThreadErr     error
	readThreadCalls   int
}

func (f *fakeCodexClient) StartThread(_ context.Context, params *codex.ThreadStartParams) (*codex.ThreadStartResponse, error) {
	f.startThreadCalls++
	f.startParams = params
	return &codex.ThreadStartResponse{Thread: codex.Thread{ID: "codex-started"}}, nil
}

func (f *fakeCodexClient) ResumeThread(_ context.Context, params *codex.ThreadResumeParams) (*codex.ThreadResumeResponse, error) {
	f.resumeThreadCalls++
	f.resumeParams = params
	return &codex.ThreadResumeResponse{Thread: codex.Thread{ID: params.ThreadID}}, nil
}

func (f *fakeCodexClient) StartTurn(ctx context.Context, _ *codex.TurnStartParams) (*codex.TurnStartResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startTurnCtx = ctx
	if f.startTurnErr != nil {
		return nil, f.startTurnErr
	}
	return &codex.TurnStartResponse{Turn: codex.Turn{ID: "turn-1"}}, nil
}

func (f *fakeCodexClient) ResetStartTurnContext() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startTurnCtx = nil
}

func (f *fakeCodexClient) StartTurnContext() context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startTurnCtx
}

func (f *fakeCodexClient) ReadThread(_ context.Context, _ *codex.ThreadReadParams) (*codex.ThreadReadResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readThreadCalls++
	if f.readThreadErr != nil {
		return nil, f.readThreadErr
	}
	if len(f.readThreadQueue) > 0 {
		resp := f.readThreadQueue[0]
		f.readThreadQueue = f.readThreadQueue[1:]
		return resp, nil
	}
	if f.readThreadResp != nil {
		return f.readThreadResp, nil
	}
	return &codex.ThreadReadResponse{}, nil
}

func (f *fakeCodexClient) ReadThreadCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readThreadCalls
}

func (f *fakeCodexClient) Close() error {
	return nil
}

func TestEnsureCodexThreadResumesFromStore(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	record := codexbridge.ThreadMappingRecord{
		InstanceID:       testAgentID,
		CodexThreadID:    "codex-1",
		CreatedAtUnixMs:  1700000000000,
		LastUsedAtUnixMs: 1700000000100,
	}
	if err := store.Save(record); err != nil {
		t.Fatalf("expected mapping save to succeed, got %v", err)
	}
	client := &fakeCodexClient{}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		agent:        &agentsv1.Agent{},
	}
	threadID, err := daemon.ensureCodexThread(context.Background())
	if err != nil {
		t.Fatalf("expected ensureCodexThread to succeed, got %v", err)
	}
	if threadID != record.CodexThreadID {
		t.Fatalf("expected thread id %q, got %q", record.CodexThreadID, threadID)
	}
	if client.resumeThreadCalls != 1 {
		t.Fatalf("expected ResumeThread to be called once, got %d", client.resumeThreadCalls)
	}
	if client.startThreadCalls != 0 {
		t.Fatalf("expected StartThread not to be called, got %d", client.startThreadCalls)
	}
	if client.resumeParams == nil || client.resumeParams.ThreadID != record.CodexThreadID {
		t.Fatalf("expected resume params for %q", record.CodexThreadID)
	}
}

func TestEnsureCodexThreadStartsNonEphemeral(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		agent:        &agentsv1.Agent{},
	}
	threadID, err := daemon.ensureCodexThread(context.Background())
	if err != nil {
		t.Fatalf("expected ensureCodexThread to succeed, got %v", err)
	}
	if threadID != "codex-started" {
		t.Fatalf("expected thread id %q, got %q", "codex-started", threadID)
	}
	if client.startThreadCalls != 1 {
		t.Fatalf("expected StartThread to be called once, got %d", client.startThreadCalls)
	}
	if client.resumeThreadCalls != 0 {
		t.Fatalf("expected ResumeThread not to be called, got %d", client.resumeThreadCalls)
	}
	if client.startParams == nil || client.startParams.Ephemeral == nil {
		t.Fatal("expected Ephemeral to be set")
	}
	if *client.startParams.Ephemeral {
		t.Fatal("expected Ephemeral to be false")
	}
	stored, ok, err := store.Load(testAgentID)
	if err != nil {
		t.Fatalf("expected mapping to be saved, got %v", err)
	}
	if !ok {
		t.Fatal("expected mapping to be saved")
	}
	if stored.CodexThreadID != "codex-started" {
		t.Fatalf("expected stored codex id %q, got %q", "codex-started", stored.CodexThreadID)
	}
}

func TestHandleCodexMessageWaitsWithoutCompletionTimeout(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{},
		threads:      platform.NewThreads(&fakeClaudeThreadsClient{}),
	}
	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.handleCodexMessage(context.Background(), message)
	}()

	select {
	case err := <-errCh:
		t.Fatalf("handleCodexMessage returned before turn completion: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	daemon.tracker.Notify(codexbridge.TurnResult{
		ThreadID: "codex-started",
		TurnID:   "turn-1",
		Message:  "done",
	})
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleCodexMessage did not finish after turn completion")
	}
	if client.StartTurnContext() == nil {
		t.Fatal("expected StartTurn to be called")
	}
}

func TestHandleCodexMessageWrapsStartTurnError(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{startTurnErr: fmt.Errorf("rpc unavailable")}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{},
	}
	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	err := daemon.handleCodexMessage(context.Background(), message)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, expected := range []string{"codex_start_turn", "5m0s", "start codex turn for message msg-1"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected %q in error: %v", expected, err)
		}
	}
}

func TestHandleCodexMessageWrapsWaitCancellation(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.handleCodexMessage(ctx, message)
	}()
	for client.StartTurnContext() == nil {
		select {
		case err := <-errCh:
			t.Fatalf("handleCodexMessage returned before cancellation: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	var err error
	select {
	case err = <-errCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleCodexMessage did not stop after cancellation")
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, expected := range []string{"codex_wait_turn_completion", "wait for codex turn", "canceled"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected %q in error: %v", expected, err)
		}
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestHandleCodexMessageWrapsTurnResultError(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{},
	}
	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.handleCodexMessage(context.Background(), message)
	}()
	turnErr := fmt.Errorf("turn failed")
	daemon.tracker.Notify(codexbridge.TurnResult{
		ThreadID: "codex-started",
		TurnID:   "turn-1",
		Err:      turnErr,
	})
	var err error
	select {
	case err = <-errCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleCodexMessage did not finish after turn failure")
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, expected := range []string{"codex_turn_result", "codex turn turn-1 failed", "msg-1"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected %q in error: %v", expected, err)
		}
	}
	if !errors.Is(err, turnErr) {
		t.Fatalf("expected wrapped turn error, got %v", err)
	}
}

func TestHandleCodexMessageReturnsTransientNotificationAsRetryable(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{},
	}
	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.handleCodexMessage(context.Background(), message)
	}()
	turnErr := &codexbridge.ErrorNotificationError{
		ThreadID: "codex-started",
		TurnID:   "turn-1",
		Message:  "stream disconn",
	}
	daemon.tracker.Notify(codexbridge.TurnResult{
		ThreadID: "codex-started",
		TurnID:   "turn-1",
		Err:      turnErr,
	})
	var err error
	select {
	case err = <-errCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleCodexMessage did not finish after transient turn failure")
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, expected := range []string{"codex_turn_result", "transient failure", "stream disconn", "msg-1"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected %q in error: %v", expected, err)
		}
	}
	if !errors.Is(err, turnErr) {
		t.Fatalf("expected wrapped transient turn error, got %v", err)
	}
	if isTerminalAgentProcessingError(err) {
		t.Fatalf("expected transient notification to remain retryable, got terminal error: %v", err)
	}
}

func TestHandleCodexMessageReadsBackTurnMessage(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{readThreadResp: &codex.ThreadReadResponse{Thread: codex.Thread{Turns: []codex.Turn{{
		ID: "turn-1",
		Items: []codex.ThreadItem{{AgentMessage: &codex.AgentMessageThreadItem{
			ID:   "agent-message-1",
			Type: codex.ThreadItemTypeAgentMessage,
			Text: "readback response",
		}}},
	}}}}}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{},
		threads:      platform.NewThreads(&fakeClaudeThreadsClient{}),
	}
	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.handleCodexMessage(context.Background(), message)
	}()
	turnErr := codexbridge.ErrMissingAgentMessage
	daemon.tracker.Notify(codexbridge.TurnResult{
		ThreadID: "codex-started",
		TurnID:   "turn-1",
		Err:      turnErr,
	})
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected readback response to recover turn result, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleCodexMessage did not finish after turn readback")
	}
}

func TestHandleCodexMessagePollsReadbackUntilAgentMessage(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{readThreadQueue: []*codex.ThreadReadResponse{
		{Thread: codex.Thread{Turns: []codex.Turn{{ID: "turn-1"}}}},
		{Thread: codex.Thread{Turns: []codex.Turn{{
			ID: "turn-1",
			Items: []codex.ThreadItem{{AgentMessage: &codex.AgentMessageThreadItem{
				ID:   "agent-message-1",
				Type: codex.ThreadItemTypeAgentMessage,
				Text: "eventually visible response",
			}}},
		}}}},
	}}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{},
		threads:      platform.NewThreads(&fakeClaudeThreadsClient{}),
	}
	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.handleCodexMessage(context.Background(), message)
	}()
	turnErr := codexbridge.ErrMissingAgentMessage
	daemon.tracker.Notify(codexbridge.TurnResult{
		ThreadID: "codex-started",
		TurnID:   "turn-1",
		Err:      turnErr,
	})
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected delayed readback response to recover turn result, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleCodexMessage did not finish after delayed turn readback")
	}
	if calls := client.ReadThreadCalls(); calls < 2 {
		t.Fatalf("expected readback polling, got %d calls", calls)
	}
}

func TestHandleCodexMessageReturnsReadbackTransportError(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	readErr := fmt.Errorf("read transport unavailable")
	client := &fakeCodexClient{readThreadErr: readErr}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{},
		threads:      platform.NewThreads(&fakeClaudeThreadsClient{}),
	}
	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.handleCodexMessage(context.Background(), message)
	}()
	daemon.tracker.Notify(codexbridge.TurnResult{
		ThreadID: "codex-started",
		TurnID:   "turn-1",
		Err:      codexbridge.ErrMissingAgentMessage,
	})
	var err error
	select {
	case err = <-errCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleCodexMessage did not finish after readback transport error")
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, readErr) {
		t.Fatalf("expected readback transport error to remain unwrap-able, got %v", err)
	}
	if calls := client.ReadThreadCalls(); calls != 1 {
		t.Fatalf("expected readback transport error to fail without retry, got %d calls", calls)
	}
}

func TestReadCodexTurnMessageTimeoutWrapsRetryableCause(t *testing.T) {
	client := &fakeCodexClient{readThreadResp: &codex.ThreadReadResponse{Thread: codex.Thread{Turns: []codex.Turn{{ID: "turn-1"}}}}}
	daemon := &Daemon{codex: client}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := daemon.readCodexTurnMessage(ctx, "codex-thread-1", "turn-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, codexbridge.ErrMissingAgentMessage) {
		t.Fatalf("expected retryable cause to remain unwrap-able, got %v", err)
	}
	if strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected timeout text without double wrapping context deadline: %v", err)
	}
}

func TestIsRetryableCodexErrorNotification(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{name: "stream disconn", message: "stream disconn"},
		{name: "stream disconnected", message: "stream disconnected"},
		{name: "connection reset", message: "provider connection reset by peer"},
		{name: "connection refused", message: "connection refused"},
		{name: "connection closed", message: "connection closed before response"},
		{name: "EOF", message: "EOF"},
		{name: "timeout", message: "request timeout"},
		{name: "temporary network failure", message: "temporary network failure"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &codexbridge.ErrorNotificationError{Message: tt.message}
			if !isRetryableCodexErrorNotification(err) {
				t.Fatalf("expected %q to be retryable", tt.message)
			}
		})
	}
}

func TestIsRetryableCodexErrorNotificationRejectsUnknown(t *testing.T) {
	unknownErr := &codexbridge.ErrorNotificationError{Message: "failed to read body"}
	if isRetryableCodexErrorNotification(unknownErr) {
		t.Fatal("expected unknown codex notification to remain terminal")
	}
	if isRetryableCodexErrorNotification(fmt.Errorf("stream disconn")) {
		t.Fatal("expected non-notification error to remain non-retryable")
	}
}

// An agent that discards its final message ends its turns silent by design, and
// the daemon must survive that: it used to treat an empty turn as terminal and
// exit, taking every other thread the workload was serving with it.
func TestHandleCodexMessageEmptyTurnAcksWithoutPosting(t *testing.T) {
	threadsClient := &fakeClaudeThreadsClient{}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        &fakeCodexClient{},
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: codexbridge.NewThreadMappingStore(t.TempDir()),
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{FinalMessage: agentsv1.AgentFinalMessage_AGENT_FINAL_MESSAGE_DISCARD},
		threads:      platform.NewThreads(threadsClient),
	}

	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.handleCodexMessage(context.Background(), message)
	}()
	time.Sleep(50 * time.Millisecond)
	daemon.tracker.Notify(codexbridge.TurnResult{ThreadID: "codex-started", TurnID: "turn-1", Message: "  "})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected an empty turn to be tolerated, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handleCodexMessage did not finish after turn completion")
	}
	if len(threadsClient.sendRequests) != 0 {
		t.Fatalf("expected nothing to be posted, got %d send requests", len(threadsClient.sendRequests))
	}
	if len(threadsClient.ackRequests) != 1 {
		t.Fatalf("expected the message to be acked, got %d ack requests", len(threadsClient.ackRequests))
	}
}

// An instance serves many threads and they share one conversation. Keyed by
// thread, a coordinator that asked three agents a question woke on each reply
// in a conversation that had never seen the question.
func TestEnsureCodexThreadIsOneConversationAcrossThreads(t *testing.T) {
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{}
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		agent:        &agentsv1.Agent{},
	}

	first, err := daemon.ensureCodexThread(context.Background())
	if err != nil {
		t.Fatalf("expected ensureCodexThread to succeed, got %v", err)
	}
	// A reply arriving on some other platform thread reaches the same instance.
	second, err := daemon.ensureCodexThread(context.Background())
	if err != nil {
		t.Fatalf("expected ensureCodexThread to succeed, got %v", err)
	}

	if first != second {
		t.Fatalf("expected one codex thread for the instance, got %q and %q", first, second)
	}
	if client.startThreadCalls != 1 {
		t.Fatalf("expected exactly one codex thread to be started, got %d", client.startThreadCalls)
	}
	stored, ok, err := store.Load(testAgentID)
	if err != nil || !ok {
		t.Fatalf("expected the mapping to be stored under the instance, got ok=%v err=%v", ok, err)
	}
	if stored.CodexThreadID != first {
		t.Fatalf("expected stored codex id %q, got %q", first, stored.CodexThreadID)
	}
}
