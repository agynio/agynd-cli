package daemon

import (
	"context"
	"testing"

	"github.com/agynio/agynd-cli/internal/config"
	"github.com/google/uuid"
)

type fakeRunnersClient struct {
	touchCalls []string
}

func (f *fakeRunnersClient) TouchWorkload(_ context.Context, workloadID string) error {
	f.touchCalls = append(f.touchCalls, workloadID)
	return nil
}

func TestTouchActiveWorkloadCallsTouch(t *testing.T) {
	fake := &fakeRunnersClient{}
	daemon := &Daemon{
		cfg:     config.Config{AgentID: uuid.MustParse(testAgentID), WorkloadID: "workload-1"},
		runners: fake,
	}
	daemon.processing.Store(true)
	touched, err := daemon.touchActiveWorkload(context.Background(), "workload-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !touched {
		t.Fatal("expected workload to be touched")
	}
	if len(fake.touchCalls) != 1 || fake.touchCalls[0] != "workload-1" {
		t.Fatalf("unexpected touch calls: %v", fake.touchCalls)
	}
}

func TestTouchActiveWorkloadSkipsWhenIdle(t *testing.T) {
	fake := &fakeRunnersClient{}
	daemon := &Daemon{
		cfg:     config.Config{AgentID: uuid.MustParse(testAgentID), WorkloadID: "workload-1"},
		runners: fake,
	}
	touched, err := daemon.touchActiveWorkload(context.Background(), "workload-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if touched {
		t.Fatal("expected no workload to be touched")
	}
	if len(fake.touchCalls) != 0 {
		t.Fatalf("unexpected touch calls: %v", fake.touchCalls)
	}
}
func TestTouchActiveWorkloadSkipsWhenMissingWorkloadID(t *testing.T) {
	fake := &fakeRunnersClient{}
	daemon := &Daemon{
		cfg:     config.Config{AgentID: uuid.MustParse(testAgentID), WorkloadID: ""},
		runners: fake,
	}
	daemon.processing.Store(true)
	touched, err := daemon.touchActiveWorkload(context.Background(), "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if touched {
		t.Fatal("expected no workload to be touched")
	}
	if len(fake.touchCalls) != 0 {
		t.Fatalf("unexpected touch calls: %v", fake.touchCalls)
	}
}
