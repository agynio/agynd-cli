package daemon

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	"github.com/google/uuid"
)

// scriptedClocks hands out the given readings in order and the last one after
// that.
func scriptedClocks(readings ...clockReading) func() clockReading {
	next := 0
	return func() clockReading {
		reading := readings[next]
		if next < len(readings)-1 {
			next++
		}
		return reading
	}
}

func wallAt(d time.Duration) time.Time {
	return time.Unix(1_800_000_000, 0).Add(d)
}

// Between two ticks of a running workload the clocks move together. After a
// restore the wall clock has been set ahead by the sleep while the monotonic
// clock has not, and the tick that first sees it reports the sleep, once.
func TestWakeWatchTickReportsTheSleep(t *testing.T) {
	watch := newWakeWatch(scriptedClocks(
		clockReading{wall: wallAt(0), mono: 0},
		clockReading{wall: wallAt(10 * time.Second), mono: 10 * time.Second},
		clockReading{wall: wallAt(20*time.Second + time.Hour), mono: 20 * time.Second},
		clockReading{wall: wallAt(30*time.Second + time.Hour), mono: 30 * time.Second},
	))
	if got := watch.tick(); got != 0 {
		t.Fatalf("expected no jump while running, got %s", got)
	}
	if got := watch.tick(); got != time.Hour {
		t.Fatalf("expected the hour slept, got %s", got)
	}
	if got := watch.tick(); got != 0 {
		t.Fatalf("expected the sleep reported once, got %s again", got)
	}
}

// The runner's second signal and the clock's own detection follow a wake
// within a tick or two of it; all of them are the wake already handled. A
// wake far enough on in wall time is a wake of its own, and so is one behind a
// wall clock that was stepped back.
func TestWakeWatchAdmitFoldsRepeatsOfAWake(t *testing.T) {
	watch := newWakeWatch(scriptedClocks(
		clockReading{wall: wallAt(0), mono: 0},
		clockReading{wall: wallAt(time.Hour), mono: time.Second},
		clockReading{wall: wallAt(time.Hour + 3*time.Second), mono: 4 * time.Second},
		clockReading{wall: wallAt(time.Hour + wakeRepeatWindow - time.Millisecond), mono: 5 * time.Second},
		clockReading{wall: wallAt(time.Hour + wakeRepeatWindow), mono: 6 * time.Second},
		clockReading{wall: wallAt(time.Hour + wakeRepeatWindow + time.Second), mono: 7 * time.Second},
		clockReading{wall: wallAt(time.Minute), mono: 8 * time.Second},
	))
	if !watch.admit() {
		t.Fatal("expected the first wake admitted")
	}
	if watch.admit() {
		t.Fatal("expected the signal's repeat folded into the wake")
	}
	if watch.admit() {
		t.Fatal("expected a repeat at the edge of the window folded into the wake")
	}
	if !watch.admit() {
		t.Fatal("expected a wake at the end of the window admitted")
	}
	if watch.admit() {
		t.Fatal("expected a repeat of the second wake folded into it")
	}
	if !watch.admit() {
		t.Fatal("expected a wake behind a wall clock stepped back admitted")
	}
}

// fakeClocks reads like a guest's clocks: the monotonic one follows the test's
// own, and the wall one follows it too until the test moves it ahead, which is
// what a restore followed by the runner's clock set looks like from inside.
type fakeClocks struct {
	start time.Time
	ahead atomic.Int64
	reads atomic.Int64
}

func newFakeClocks() *fakeClocks {
	return &fakeClocks{start: time.Now()}
}

func (f *fakeClocks) read() clockReading {
	f.reads.Add(1)
	mono := time.Since(f.start)
	return clockReading{wall: f.start.Round(0).Add(mono + time.Duration(f.ahead.Load())), mono: mono}
}

// sleep is a hibernation of d as seen from inside: the wall clock is set ahead
// by it and the monotonic clock is not.
func (f *fakeClocks) sleep(d time.Duration) {
	f.ahead.Add(int64(d))
}

// waitForReads returns once the keepalive loop has read the clocks n more
// times, which is n more ticks, each after whatever the loop handled before it.
func (f *fakeClocks) waitForReads(t *testing.T, n int64) {
	t.Helper()
	target := f.reads.Load() + n
	deadline := time.After(2 * time.Second)
	for f.reads.Load() < target {
		select {
		case <-deadline:
			t.Fatalf("the keepalive loop stopped ticking at %d reads", f.reads.Load())
		case <-time.After(time.Millisecond):
		}
	}
}

// A consumer whose first sync is a turn that lasts until the test ends it; the
// syncs after it return at once.
type turnInFlightConsumer struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func newTurnInFlightConsumer() *turnInFlightConsumer {
	return &turnInFlightConsumer{started: make(chan struct{}), release: make(chan struct{})}
}

func (c *turnInFlightConsumer) Sync(ctx context.Context, _ string, _ string, _ func(platform.Message) error) error {
	c.mu.Lock()
	c.calls++
	first := c.calls == 1
	c.mu.Unlock()
	if first {
		close(c.started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.release:
		}
	}
	return nil
}

func (c *turnInFlightConsumer) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func waitForSyncCalls(t *testing.T, errCh <-chan error, calls func() int, want int, what string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for calls() < want {
		select {
		case err := <-errCh:
			t.Fatalf("Run returned before %s: %v", what, err)
		case <-deadline:
			t.Fatalf("expected %d sync calls after %s, got %d", want, what, calls())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func stopRun(t *testing.T, cancel context.CancelFunc, errCh <-chan error) {
	t.Helper()
	cancel()
	select {
	case <-errCh:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}

// A wake mid-turn resumes the keepalive at once: the turn in flight is touched
// on the wake rather than at the next tick, over whatever connection the touch
// now gets. The subscription is reopened and an inbox read is asked for.
func TestKeepaliveWakeTouchesTheTurnInFlight(t *testing.T) {
	subscriber := newFakeMessageSubscriber()
	runners := &recordingRunnersClient{touched: make(chan struct{}, 1)}
	daemon := &Daemon{
		cfg:            config.Config{AgentID: uuid.MustParse(testAgentID), WorkloadID: "workload-1"},
		runners:        runners,
		subscriber:     subscriber,
		processingWake: make(chan struct{}, 1),
		wakeSync:       make(chan struct{}, 1),
	}
	daemon.processing.Store(true)
	wake := make(chan os.Signal, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		daemon.runKeepalive(ctx, wake)
	}()

	// The touch the loop opens with, for a turn already running.
	select {
	case <-runners.touched:
	case <-time.After(time.Second):
		t.Fatal("expected the opening touch")
	}

	wake <- syscall.SIGCONT
	select {
	case <-runners.touched:
	case <-time.After(time.Second):
		t.Fatal("expected the turn in flight touched on the wake")
	}
	if got := subscriber.resubscribed.Load(); got != 1 {
		t.Fatalf("expected the subscription reopened once, got %d", got)
	}
	select {
	case <-daemon.wakeSync:
	default:
		t.Fatal("expected an inbox read asked for")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("keepalive loop did not stop")
	}
}

// A wake that lands mid-turn is handled while the turn runs: the subscription
// is reopened then, not when the turn ends. The inbox read waits for the turn,
// because the daemon runs one turn at a time, and follows it.
func TestRunWakeMidTurnResubscribesBeforeTheTurnEnds(t *testing.T) {
	subscriber := newFakeMessageSubscriber()
	consumer := newTurnInFlightConsumer()
	wake := make(chan os.Signal, 1)
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
		errCh <- daemon.Run(ctx, wake)
	}()

	select {
	case <-consumer.started:
	case err := <-errCh:
		t.Fatalf("Run returned before the turn: %v", err)
	case <-time.After(time.Second):
		t.Fatal("the turn did not start")
	}

	wake <- syscall.SIGCONT
	deadline := time.After(time.Second)
	for subscriber.resubscribed.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("expected the subscription reopened while the turn was in flight")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got := consumer.Calls(); got != 1 {
		t.Fatalf("expected the turn still in flight alone, got %d sync calls", got)
	}

	close(consumer.release)
	waitForSyncCalls(t, errCh, consumer.Calls, 2, "the turn ended")
	if got := subscriber.resubscribed.Load(); got != 1 {
		t.Fatalf("expected one resubscribe for one wake, got %d", got)
	}

	stopRun(t, cancel, errCh)
}

// The signal can go missing; the clock cannot. A wall clock that moved more
// than a tick further than monotonic time between two ticks is a restore, and
// wakes the daemon as the signal would. A second sleep later is a second wake.
func TestRunClockJumpWakes(t *testing.T) {
	subscriber := newFakeMessageSubscriber()
	consumer := &fakeMessageConsumer{}
	clocks := newFakeClocks()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	daemon := &Daemon{
		cfg:               config.Config{AgentID: uuid.MustParse(testAgentID)},
		runners:           &fakeRunnersClient{},
		subscriber:        subscriber,
		consumer:          consumer,
		clocks:            clocks.read,
		keepaliveInterval: 10 * time.Millisecond,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Run(ctx, nil)
	}()
	waitForSyncCalls(t, errCh, consumer.Calls, 1, "start")

	// Ticks with the clocks in step are not a wake.
	clocks.waitForReads(t, 3)
	if got := subscriber.resubscribed.Load(); got != 0 {
		t.Fatalf("resubscribed %d times with the clocks in step", got)
	}

	clocks.sleep(time.Hour)
	waitForSyncCalls(t, errCh, consumer.Calls, 2, "the clock jumped")
	if got := subscriber.resubscribed.Load(); got != 1 {
		t.Fatalf("expected one resubscribe for the jump, got %d", got)
	}

	clocks.sleep(time.Hour)
	waitForSyncCalls(t, errCh, consumer.Calls, 3, "the clock jumped again")
	if got := subscriber.resubscribed.Load(); got != 2 {
		t.Fatalf("expected a second resubscribe for a second sleep, got %d", got)
	}

	stopRun(t, cancel, errCh)
}

// One wake, three signs of it: the runner's two signals a few seconds apart and
// the clock's detection at the next tick. One reconnect, one inbox read.
func TestRunWakeRepeatsAreCoalesced(t *testing.T) {
	subscriber := newFakeMessageSubscriber()
	consumer := &fakeMessageConsumer{}
	clocks := newFakeClocks()
	wake := make(chan os.Signal, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	daemon := &Daemon{
		cfg:               config.Config{AgentID: uuid.MustParse(testAgentID)},
		runners:           &fakeRunnersClient{},
		subscriber:        subscriber,
		consumer:          consumer,
		clocks:            clocks.read,
		keepaliveInterval: 10 * time.Millisecond,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Run(ctx, wake)
	}()
	waitForSyncCalls(t, errCh, consumer.Calls, 1, "start")

	// The runner sets the clock, then signals, then signals again.
	clocks.sleep(time.Hour)
	wake <- syscall.SIGCONT
	wake <- syscall.SIGCONT
	// The second send landed once the first was taken; the loop takes the
	// second before its next tick, so a few ticks later both are handled and
	// the clock has had its say.
	for len(wake) > 0 {
		time.Sleep(time.Millisecond)
	}
	clocks.waitForReads(t, 3)

	waitForSyncCalls(t, errCh, consumer.Calls, 2, "the wake")
	if got := subscriber.resubscribed.Load(); got != 1 {
		t.Fatalf("expected one resubscribe for one wake, got %d", got)
	}
	// And it stays that way: no read piles up behind the first.
	clocks.waitForReads(t, 3)
	if got := consumer.Calls(); got != 2 {
		t.Fatalf("expected one inbox read for one wake, got %d", got-1)
	}
	if got := subscriber.resubscribed.Load(); got != 1 {
		t.Fatalf("expected the resubscribe count to stay at one, got %d", got)
	}

	stopRun(t, cancel, errCh)
}
