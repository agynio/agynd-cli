package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	claude "github.com/agynio/claude-sdk-go"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// turnLog records what the daemon told the platform, in the order it said it.
type turnLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *turnLog) add(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
}

func (l *turnLog) Entries() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// An inbox that logs its acks.
type loggingInboxClient struct {
	gatewayv1.AgentsGatewayClient
	log    *turnLog
	ackErr error
}

func (c *loggingInboxClient) AckInboxItems(context.Context, *agentsv1.AckInboxItemsRequest, ...grpc.CallOption) (*agentsv1.AckInboxItemsResponse, error) {
	if c.ackErr != nil {
		return nil, c.ackErr
	}
	c.log.add("ack")
	return &agentsv1.AckInboxItemsResponse{}, nil
}

// A runners client that logs its idle reports, with the workload they name.
type loggingRunnersClient struct {
	log       *turnLog
	reportErr error
}

func (c *loggingRunnersClient) TouchWorkload(context.Context, string) error {
	return nil
}

func (c *loggingRunnersClient) ReportWorkloadIdle(_ context.Context, workloadID string) error {
	c.log.add("report " + workloadID)
	return c.reportErr
}

func newTurnBoundaryDaemon(log *turnLog, inbox *loggingInboxClient, runners *loggingRunnersClient) *Daemon {
	return &Daemon{
		sdk: SDKClaude,
		cfg: config.Config{
			AgentID:         uuid.MustParse(testAgentID),
			AgentInstanceID: uuid.MustParse(testAgentInstanceID),
			WorkloadID:      "workload-1",
		},
		threads:    platform.NewThreads(&fakeClaudeThreadsClient{}),
		agentInbox: platform.NewAgents(inbox),
		runners:    runners,
		claude:     &fakeClaudeClient{result: &claude.TurnResult{Response: "hello"}},
		agent:      &agentsv1.Agent{},
		consumer: &handlingMessageConsumer{message: platform.Message{
			ID:          "msg-1",
			InboxItemID: "item-1",
			ThreadID:    "thread-1",
			Body:        "hello",
		}},
	}
}

func assertTurnLog(t *testing.T, log *turnLog, want ...string) {
	t.Helper()
	got := log.Entries()
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// The ack is the last word of a turn and the report follows it: Runners learns
// the turn is over the moment it is, and never while the item is still
// outstanding, so the workload cannot be frozen between finishing an item and
// acknowledging it.
func TestTurnEndsWithTheAckAndThenTheIdleReport(t *testing.T) {
	log := &turnLog{}
	daemon := newTurnBoundaryDaemon(log, &loggingInboxClient{log: log}, &loggingRunnersClient{log: log})

	if err := daemon.syncMessages(context.Background()); err != nil {
		t.Fatalf("expected the turn to succeed, got %v", err)
	}
	assertTurnLog(t, log, "ack", "report workload-1")
}

// A report that fails costs nothing but latency: the activity sweep reaches the
// same state on its own. The turn has already succeeded and stays that way.
func TestFailedIdleReportDoesNotFailTheTurn(t *testing.T) {
	log := &turnLog{}
	runners := &loggingRunnersClient{log: log, reportErr: errors.New("gateway unavailable")}
	daemon := newTurnBoundaryDaemon(log, &loggingInboxClient{log: log}, runners)

	if err := daemon.syncMessages(context.Background()); err != nil {
		t.Fatalf("expected the turn to succeed despite the report, got %v", err)
	}
	assertTurnLog(t, log, "ack", "report workload-1")
}

// No ack, no report: a turn whose item is still outstanding is not over.
func TestNoIdleReportWhenTheAckFails(t *testing.T) {
	log := &turnLog{}
	ackErr := errors.New("gateway unavailable")
	daemon := newTurnBoundaryDaemon(log, &loggingInboxClient{log: log, ackErr: ackErr}, &loggingRunnersClient{log: log})

	err := daemon.syncMessages(context.Background())
	if !errors.Is(err, ackErr) {
		t.Fatalf("expected the ack failure, got %v", err)
	}
	assertTurnLog(t, log)
}

// In holder mode there is no turn, so there is no report; a sandbox's idleness
// is the Terminal Proxy's to observe.
func TestHolderModeReportsNoIdle(t *testing.T) {
	stubHolderChdir(t)
	runners := &fakeRunnersClient{}
	daemon := &Daemon{
		sdk:     config.ModeHolder,
		cfg:     config.Config{WorkDir: t.TempDir(), WorkloadID: "workload-1"},
		runners: runners,
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- daemon.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("holder did not stop after cancellation")
	}
	if len(runners.reportCalls) != 0 {
		t.Fatalf("expected no idle report from a holder, got %v", runners.reportCalls)
	}
}
