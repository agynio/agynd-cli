package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/agynio/agynd-cli/internal/config"
	"github.com/google/uuid"
)

type fakeRunnersClient struct {
	touchCalls []string
	err        error
}

func (f *fakeRunnersClient) TouchWorkload(_ context.Context, workloadID string) error {
	f.touchCalls = append(f.touchCalls, workloadID)
	return f.err
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

func TestTouchActiveWorkloadWrapsError(t *testing.T) {
	touchErr := fmt.Errorf("rpc failed")
	fake := &fakeRunnersClient{err: touchErr}
	daemon := &Daemon{
		cfg:     config.Config{AgentID: uuid.MustParse(testAgentID), WorkloadID: "workload-1"},
		runners: fake,
	}
	daemon.processing.Store(true)
	touched, err := daemon.touchActiveWorkload(context.Background(), "workload-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if touched {
		t.Fatal("expected workload touch to fail")
	}
	for _, expected := range []string{"keepalive_touch", "5s", "workload-1"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected %q in error: %v", expected, err)
		}
	}
	if !errors.Is(err, touchErr) {
		t.Fatalf("expected wrapped touch error, got %v", err)
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
