package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

type fakeMessageSubscriber struct {
	runStarted chan struct{}
	releaseRun chan struct{}
	wake       chan struct{}
	runOnce    sync.Once
}

func newFakeMessageSubscriber() *fakeMessageSubscriber {
	return &fakeMessageSubscriber{
		runStarted: make(chan struct{}),
		releaseRun: make(chan struct{}),
		wake:       make(chan struct{}, 1),
	}
}

func (f *fakeMessageSubscriber) Run(ctx context.Context) error {
	f.runOnce.Do(func() { close(f.runStarted) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.releaseRun:
		return fmt.Errorf("subscriber released")
	}
}

func (f *fakeMessageSubscriber) Started() <-chan struct{} {
	return f.runStarted
}

func (f *fakeMessageSubscriber) Ready() <-chan struct{} {
	return make(chan struct{})
}

func (f *fakeMessageSubscriber) Wake() <-chan struct{} {
	return f.wake
}

type fakeMessageConsumer struct {
	mu     sync.Mutex
	errors []error
	calls  int
}

func (f *fakeMessageConsumer) Sync(_ context.Context, _ string, _ string, _ func(platform.Message) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.errors) == 0 {
		return nil
	}
	err := f.errors[0]
	f.errors = f.errors[1:]
	return err
}

func (f *fakeMessageConsumer) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type recordingRunnersClient struct {
	touched chan struct{}
}

func (r *recordingRunnersClient) TouchWorkload(_ context.Context, _ string) error {
	select {
	case r.touched <- struct{}{}:
	default:
	}
	return nil
}

type blockingMessageConsumer struct {
	started           chan struct{}
	release           chan struct{}
	subscriberStarted <-chan struct{}
	startedBeforeSync bool
	mu                sync.Mutex
	once              sync.Once
}

func (b *blockingMessageConsumer) Sync(ctx context.Context, _ string, _ string, _ func(platform.Message) error) error {
	select {
	case <-b.subscriberStarted:
		b.mu.Lock()
		b.startedBeforeSync = true
		b.mu.Unlock()
	default:
	}
	b.once.Do(func() { close(b.started) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.release:
		return fmt.Errorf("sync released")
	}
}

func (b *blockingMessageConsumer) StartedBeforeSync() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.startedBeforeSync
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

func TestRunStartsKeepaliveAndSubscriberBeforeInitialSync(t *testing.T) {
	subscriber := newFakeMessageSubscriber()
	consumer := &blockingMessageConsumer{
		started:           make(chan struct{}),
		release:           make(chan struct{}),
		subscriberStarted: subscriber.runStarted,
	}
	runners := &recordingRunnersClient{touched: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	daemon := &Daemon{
		cfg: config.Config{
			AgentID:    uuid.MustParse(testAgentID),
			WorkloadID: "workload-1",
		},
		runners:    runners,
		subscriber: subscriber,
		consumer:   consumer,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Run(ctx)
	}()

	select {
	case <-subscriber.runStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("subscriber did not start")
	}
	select {
	case <-consumer.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("initial sync did not run")
	}
	if !consumer.StartedBeforeSync() {
		t.Fatal("initial sync started before subscriber")
	}
	select {
	case <-runners.touched:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("keepalive did not touch active workload during initial sync")
	}
	cancel()
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("unexpected run error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not stop")
	}
}

func TestRunInitialSyncProceedsWhenSubscriberNotReady(t *testing.T) {
	subscriber := newFakeMessageSubscriber()
	consumer := &fakeMessageConsumer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	daemon := &Daemon{
		cfg:        config.Config{AgentID: uuid.MustParse(testAgentID)},
		runners:    &fakeRunnersClient{},
		subscriber: subscriber,
		consumer:   consumer,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Run(ctx)
	}()

	select {
	case <-subscriber.runStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("subscriber did not start")
	}
	deadline := time.After(500 * time.Millisecond)
	for consumer.Calls() == 0 {
		select {
		case err := <-errCh:
			t.Fatalf("Run returned before sync: %v", err)
		case <-deadline:
			t.Fatal("initial sync did not run while subscriber was not ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not stop")
	}
}

func TestRunRetriesSyncAfterFailure(t *testing.T) {
	subscriber := newFakeMessageSubscriber()
	consumer := &fakeMessageConsumer{errors: []error{fmt.Errorf("transient")}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	daemon := &Daemon{
		cfg:        config.Config{AgentID: uuid.MustParse(testAgentID)},
		runners:    &fakeRunnersClient{},
		subscriber: subscriber,
		consumer:   consumer,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Run(ctx)
	}()

	deadline := time.After(1500 * time.Millisecond)
	for consumer.Calls() < 2 {
		select {
		case <-deadline:
			t.Fatalf("expected sync retry, got %d calls", consumer.Calls())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not stop")
	}
}

func TestNextSyncRetryBackoffCapsAtMaximum(t *testing.T) {
	if got := nextSyncRetryBackoff(0); got != syncRetryInitialBackoff {
		t.Fatalf("expected initial backoff, got %s", got)
	}
	if got := nextSyncRetryBackoff(10 * time.Second); got != 20*time.Second {
		t.Fatalf("expected doubled backoff, got %s", got)
	}
	if got := nextSyncRetryBackoff(20 * time.Second); got != syncRetryMaxBackoff {
		t.Fatalf("expected max backoff, got %s", got)
	}
}
