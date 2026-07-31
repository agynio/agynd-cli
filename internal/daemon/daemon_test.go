package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	"github.com/agynio/agynd-cli/internal/codexbridge"
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

type handlingMessageConsumer struct {
	mu      sync.Mutex
	calls   int
	message platform.Message
	done    chan struct{}
}

func (h *handlingMessageConsumer) Sync(_ context.Context, _ string, _ string, handle func(platform.Message) error) error {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()
	err := handle(h.message)
	if h.done != nil {
		select {
		case h.done <- struct{}{}:
		default:
		}
	}
	return err
}

func (h *handlingMessageConsumer) Calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

type recordingMessageConsumer struct {
	mu       sync.Mutex
	calls    int
	messages []platform.Message
	started  chan struct{}
	done     chan struct{}
}

func (r *recordingMessageConsumer) Sync(ctx context.Context, _ string, _ string, handle func(platform.Message) error) error {
	r.mu.Lock()
	r.calls++
	callIndex := r.calls - 1
	if callIndex >= len(r.messages) {
		r.mu.Unlock()
		<-ctx.Done()
		return ctx.Err()
	}
	message := r.messages[callIndex]
	r.mu.Unlock()
	if r.started != nil {
		select {
		case r.started <- struct{}{}:
		default:
		}
	}

	err := handle(message)
	if r.done != nil {
		select {
		case r.done <- struct{}{}:
		default:
		}
	}
	return err
}

func (r *recordingMessageConsumer) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
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

func TestRunWrapsShutdownCancellation(t *testing.T) {
	subscriber := newFakeMessageSubscriber()
	consumer := &fakeMessageConsumer{}
	ctx, cancel := context.WithCancel(context.Background())
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
	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		for _, expected := range []string{"process_signal/shutdown", "canceled"} {
			if !strings.Contains(err.Error(), expected) {
				t.Fatalf("expected %q in error: %v", expected, err)
			}
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
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

func TestRunReturnsTerminalCodexTurnFailureWithoutRetry(t *testing.T) {
	subscriber := newFakeMessageSubscriber()
	consumer := &handlingMessageConsumer{message: platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}}
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		runners:      &fakeRunnersClient{},
		subscriber:   subscriber,
		consumer:     consumer,
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Run(ctx)
	}()

	deadline := time.After(500 * time.Millisecond)
	for client.StartTurnContext() == nil {
		select {
		case err := <-errCh:
			t.Fatalf("Run returned before codex turn started: %v", err)
		case <-deadline:
			t.Fatal("codex turn did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	turnErr := &codexbridge.ErrorNotificationError{
		ThreadID: "codex-started",
		TurnID:   "turn-1",
		Message:  "failed to read body",
	}
	daemon.tracker.Notify(codexbridge.TurnResult{ThreadID: "codex-started", TurnID: "turn-1", Err: turnErr})

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected terminal error, got nil")
		}
		for _, expected := range []string{"codex_turn_result", "failed to read body", "msg-1", "turn-1"} {
			if !strings.Contains(err.Error(), expected) {
				t.Fatalf("expected %q in error: %v", expected, err)
			}
		}
		if !errors.Is(err, turnErr) {
			t.Fatalf("expected wrapped terminal turn error, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not return terminal codex turn failure")
	}
	if calls := consumer.Calls(); calls != 1 {
		t.Fatalf("expected terminal failure to avoid retry, got %d sync calls", calls)
	}
}

func TestRunRetriesTransientCodexStreamFailure(t *testing.T) {
	subscriber := newFakeMessageSubscriber()
	consumer := &recordingMessageConsumer{
		messages: []platform.Message{
			{ID: "msg-1", ThreadID: "thread-1", Body: "hello"},
			{ID: "msg-1", ThreadID: "thread-1", Body: "hello"},
		},
		started: make(chan struct{}, 2),
		done:    make(chan struct{}, 2),
	}
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	client := &fakeCodexClient{}
	threadsClient := &fakeClaudeThreadsClient{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	daemon := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		runners:      &fakeRunnersClient{},
		subscriber:   subscriber,
		consumer:     consumer,
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{},
		threads:      platform.NewThreads(threadsClient),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Run(ctx)
	}()

	select {
	case <-consumer.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("initial sync did not start")
	}

	waitForStartTurn := func() {
		t.Helper()
		deadline := time.After(500 * time.Millisecond)
		for client.StartTurnContext() == nil {
			select {
			case err := <-errCh:
				t.Fatalf("Run returned before codex turn started: %v", err)
			case <-deadline:
				t.Fatal("codex turn did not start")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}

	waitForStartTurn()
	turnErr := &codexbridge.ErrorNotificationError{
		ThreadID: "codex-started",
		TurnID:   "turn-1",
		Message:  "stream disconn",
	}
	daemon.tracker.Notify(codexbridge.TurnResult{ThreadID: "codex-started", TurnID: "turn-1", Err: turnErr})
	select {
	case <-consumer.done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first sync did not finish")
	}
	if len(threadsClient.ackRequests) != 0 {
		t.Fatalf("expected no ack for retryable failure, got %d", len(threadsClient.ackRequests))
	}
	client.ResetStartTurnContext()
	select {
	case err := <-errCh:
		t.Fatalf("Run returned instead of retrying transient codex failure: %v", err)
	case <-time.After(1100 * time.Millisecond):
	}
	if calls := consumer.Calls(); calls < 2 {
		t.Fatalf("expected transient codex failure to retry sync, got %d calls", calls)
	}

	select {
	case <-consumer.started:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("retry sync did not start")
	}
	waitForStartTurn()
	daemon.tracker.Notify(codexbridge.TurnResult{ThreadID: "codex-started", TurnID: "turn-1", Message: "recovered response"})
	select {
	case <-consumer.done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("retry sync did not finish")
	}
	if len(threadsClient.ackRequests) != 1 {
		t.Fatalf("expected ack after successful retry, got %d", len(threadsClient.ackRequests))
	}
	if got := threadsClient.ackRequests[0].GetMessageIds(); len(got) != 1 || got[0] != "msg-1" {
		t.Fatalf("unexpected ack message ids: %#v", got)
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not stop")
	}
}

func TestRunRetriesReadbackTransportFailure(t *testing.T) {
	subscriber := newFakeMessageSubscriber()
	consumer := &handlingMessageConsumer{
		message: platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"},
		done:    make(chan struct{}, 2),
	}
	store := codexbridge.NewThreadMappingStore(t.TempDir())
	readErr := fmt.Errorf("read transport unavailable")
	client := &fakeCodexClient{readThreadErr: readErr}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &Daemon{
		sdk:          SDKCodex,
		cfg:          config.Config{AgentID: uuid.MustParse(testAgentID), WorkDir: "/tmp"},
		runners:      &fakeRunnersClient{},
		subscriber:   subscriber,
		consumer:     consumer,
		codex:        client,
		mapping:      codexbridge.NewThreadMapping(),
		mappingStore: store,
		tracker:      codexbridge.NewTurnTracker(),
		agent:        &agentsv1.Agent{},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Run(ctx)
	}()

	waitForStartTurn := func() {
		t.Helper()
		deadline := time.After(500 * time.Millisecond)
		for client.StartTurnContext() == nil {
			select {
			case err := <-errCh:
				t.Fatalf("Run returned before codex turn started: %v", err)
			case <-deadline:
				t.Fatal("codex turn did not start")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	waitForStartTurn()
	d.tracker.Notify(codexbridge.TurnResult{ThreadID: "codex-started", TurnID: "turn-1", Err: codexbridge.ErrMissingAgentMessage})

	select {
	case <-consumer.done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first sync did not finish")
	}
	select {
	case err := <-errCh:
		t.Fatalf("Run returned instead of retrying readback transport failure: %v", err)
	case <-time.After(1100 * time.Millisecond):
	}
	if calls := consumer.Calls(); calls < 2 {
		t.Fatalf("expected readback transport failure to retry sync, got %d calls", calls)
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Run did not stop")
	}
}

func TestSyncMessagesWaitsForMCPReadyBeforeHandling(t *testing.T) {
	var requests atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := requests.Add(1)
		if attempt < 3 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"agynd-mcp-ready","result":{"protocolVersion":"2025-06-18"}}`))
	})}
	listener, port := startHTTPListener(t, server)
	defer listener.Close()

	consumer := &handlingMessageConsumer{message: platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}}
	daemon := &Daemon{
		sdk:      "unknown",
		cfg:      config.Config{AgentID: uuid.MustParse(testAgentID), MCPServers: []config.MCPServer{{Name: "memory", Port: port}}},
		consumer: consumer,
	}

	err := daemon.syncMessages(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unknown sdk") {
		t.Fatalf("expected handler to run after MCP readiness, got %v", err)
	}
	if got := requests.Load(); got < 3 {
		t.Fatalf("expected MCP readiness retries before handling, got %d request(s)", got)
	}
	if calls := consumer.Calls(); calls != 1 {
		t.Fatalf("expected one sync call, got %d", calls)
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

func TestNewHolderModeDoesNotConnectPlatform(t *testing.T) {
	daemon, err := New(context.Background(), config.Config{Mode: config.ModeHolder, WorkDir: config.HolderDefaultWorkDir}, "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if daemon.sdk != config.ModeHolder {
		t.Fatalf("expected holder sdk marker, got %q", daemon.sdk)
	}
	if daemon.gatewayConn != nil || daemon.subscriber != nil || daemon.consumer != nil || daemon.agents != nil || daemon.runners != nil {
		t.Fatal("holder mode initialized platform dependencies")
	}
	if daemon.codex != nil || daemon.agn != nil || daemon.claude != nil || daemon.tracingProxy != nil {
		t.Fatal("holder mode initialized agent runtime dependencies")
	}
}

func TestRunHolderModeUsesDefaultWorkDir(t *testing.T) {
	runHolderModeAndAssertChdir(t, config.Config{Mode: config.ModeHolder, WorkDir: config.HolderDefaultWorkDir}, config.HolderDefaultWorkDir)
}

func TestRunHolderModeUsesConfiguredWorkDir(t *testing.T) {
	workDir := t.TempDir()
	cfg := config.Config{Mode: config.ModeHolder, WorkDir: workDir}
	// os.Getwd reports the resolved path, and on macOS TempDir hands back
	// /var/..., a symlink to /private/var/....
	resolved, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve work dir: %v", err)
	}
	runHolderModeAndAssertWorkDir(t, cfg, resolved)
}

func runHolderModeAndAssertWorkDir(t *testing.T, cfg config.Config, expectedWorkDir string) {
	t.Helper()
	originalWorkDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get original work dir: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalWorkDir); err != nil {
			t.Fatalf("restore original work dir: %v", err)
		}
	}()

	daemon, err := New(context.Background(), cfg, "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Run(ctx)
	}()

	waitForWorkDir(t, errCh, expectedWorkDir)

	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancellation error, got nil")
		}
		for _, expected := range []string{"process_signal/shutdown", "canceled"} {
			if !strings.Contains(err.Error(), expected) {
				t.Fatalf("expected %q in error: %v", expected, err)
			}
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("holder mode did not stop after cancellation")
	}
}

func runHolderModeAndAssertChdir(t *testing.T, cfg config.Config, expectedWorkDir string) {
	t.Helper()
	originalChdir := holderChdir
	chdirCalled := make(chan string, 1)
	holderChdir = func(path string) error {
		chdirCalled <- path
		return nil
	}
	defer func() { holderChdir = originalChdir }()

	daemon, err := New(context.Background(), cfg, "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Run(ctx)
	}()

	select {
	case got := <-chdirCalled:
		if got != expectedWorkDir {
			t.Fatalf("expected holder chdir %s, got %s", expectedWorkDir, got)
		}
	case err := <-errCh:
		t.Fatalf("holder mode returned before chdir: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("holder mode did not apply work dir")
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("holder mode did not stop after cancellation")
	}
}

func waitForWorkDir(t *testing.T, errCh <-chan error, expectedWorkDir string) {
	t.Helper()
	deadline := time.After(500 * time.Millisecond)
	for {
		currentWorkDir, err := os.Getwd()
		if err != nil {
			t.Fatalf("get work dir: %v", err)
		}
		if currentWorkDir == expectedWorkDir {
			return
		}
		select {
		case err := <-errCh:
			t.Fatalf("holder mode returned before work dir was applied: %v", err)
		case <-deadline:
			t.Fatalf("expected holder work dir %s, got %s", expectedWorkDir, currentWorkDir)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
