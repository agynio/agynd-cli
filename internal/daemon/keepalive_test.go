package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	runnersv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/runners/v1"
	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	"github.com/google/uuid"
)

type listResponse struct {
	workloads []platform.Workload
	nextToken string
	err       error
}

type fakeRunnersClient struct {
	listResponses []listResponse
	listCalls     int
	listTokens    []string
	listThreads   []string
	listSizes     []int32

	touchCalls []string
}

func (f *fakeRunnersClient) ListWorkloadsByThread(_ context.Context, threadID string, pageSize int32, pageToken string) ([]platform.Workload, string, error) {
	f.listCalls++
	f.listThreads = append(f.listThreads, threadID)
	f.listSizes = append(f.listSizes, pageSize)
	f.listTokens = append(f.listTokens, pageToken)
	if f.listCalls > len(f.listResponses) {
		return nil, "", fmt.Errorf("unexpected list call")
	}
	resp := f.listResponses[f.listCalls-1]
	return resp.workloads, resp.nextToken, resp.err
}

func (f *fakeRunnersClient) TouchWorkload(_ context.Context, workloadID string) error {
	f.touchCalls = append(f.touchCalls, workloadID)
	return nil
}

func TestFindActiveWorkloadSelectsLatestRunning(t *testing.T) {
	base := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	first := platform.Workload{
		ID:        "workload-1",
		AgentID:   testAgentID,
		Status:    runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
		CreatedAt: base,
	}
	second := platform.Workload{
		ID:        "workload-2",
		AgentID:   testAgentID,
		Status:    runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
		CreatedAt: base.Add(5 * time.Minute),
	}
	other := platform.Workload{
		ID:        "workload-3",
		AgentID:   "other-agent",
		Status:    runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
		CreatedAt: base.Add(10 * time.Minute),
	}
	fake := &fakeRunnersClient{listResponses: []listResponse{
		{workloads: []platform.Workload{first, other}, nextToken: "next"},
		{workloads: []platform.Workload{second}, nextToken: ""},
	}}
	daemon := &Daemon{
		cfg:     config.Config{AgentID: uuid.MustParse(testAgentID)},
		runners: fake,
	}
	workload, ok, err := daemon.findActiveWorkload(context.Background(), "thread-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Fatal("expected active workload")
	}
	if workload.ID != second.ID {
		t.Fatalf("expected %q, got %q", second.ID, workload.ID)
	}
	if fake.listCalls != 2 {
		t.Fatalf("expected 2 list calls, got %d", fake.listCalls)
	}
}

func TestTouchActiveWorkloadCallsTouch(t *testing.T) {
	base := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	workload := platform.Workload{
		ID:        "workload-1",
		AgentID:   testAgentID,
		Status:    runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
		CreatedAt: base,
	}
	fake := &fakeRunnersClient{listResponses: []listResponse{{workloads: []platform.Workload{workload}}}}
	daemon := &Daemon{
		cfg:     config.Config{AgentID: uuid.MustParse(testAgentID)},
		runners: fake,
	}
	daemon.processing.Store(true)
	touched, err := daemon.touchActiveWorkload(context.Background(), "thread-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !touched {
		t.Fatal("expected workload to be touched")
	}
	if len(fake.touchCalls) != 1 || fake.touchCalls[0] != workload.ID {
		t.Fatalf("unexpected touch calls: %v", fake.touchCalls)
	}
}

func TestTouchActiveWorkloadSkipsWhenIdle(t *testing.T) {
	base := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	workload := platform.Workload{
		ID:        "workload-1",
		AgentID:   testAgentID,
		Status:    runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
		CreatedAt: base,
	}
	fake := &fakeRunnersClient{listResponses: []listResponse{{workloads: []platform.Workload{workload}}}}
	daemon := &Daemon{
		cfg:     config.Config{AgentID: uuid.MustParse(testAgentID)},
		runners: fake,
	}
	touched, err := daemon.touchActiveWorkload(context.Background(), "thread-1")
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

func TestTouchActiveWorkloadSkipsNonRunning(t *testing.T) {
	base := time.Date(2024, 7, 1, 12, 0, 0, 0, time.UTC)
	removedAt := base.Add(1 * time.Hour)
	workload := platform.Workload{
		ID:        "workload-1",
		AgentID:   testAgentID,
		Status:    runnersv1.WorkloadStatus_WORKLOAD_STATUS_STOPPED,
		CreatedAt: base,
		RemovedAt: &removedAt,
	}
	fake := &fakeRunnersClient{listResponses: []listResponse{{workloads: []platform.Workload{workload}}}}
	daemon := &Daemon{
		cfg:     config.Config{AgentID: uuid.MustParse(testAgentID)},
		runners: fake,
	}
	daemon.processing.Store(true)
	touched, err := daemon.touchActiveWorkload(context.Background(), "thread-1")
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
