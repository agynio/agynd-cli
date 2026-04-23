package platform

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	runnerv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/runner/v1"
	runnersv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/runners/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeRunnersGatewayClient struct {
	listReq  *runnersv1.ListWorkloadsByThreadRequest
	listResp *runnersv1.ListWorkloadsByThreadResponse
	listErr  error

	touchReq *runnersv1.TouchWorkloadRequest
	touchErr error
}

var _ gatewayv1.RunnersGatewayClient = (*fakeRunnersGatewayClient)(nil)

func (f *fakeRunnersGatewayClient) RegisterRunner(ctx context.Context, in *runnersv1.RegisterRunnerRequest, opts ...grpc.CallOption) (*runnersv1.RegisterRunnerResponse, error) {
	return nil, fmt.Errorf("RegisterRunner not implemented")
}

func (f *fakeRunnersGatewayClient) GetRunner(ctx context.Context, in *runnersv1.GetRunnerRequest, opts ...grpc.CallOption) (*runnersv1.GetRunnerResponse, error) {
	return nil, fmt.Errorf("GetRunner not implemented")
}

func (f *fakeRunnersGatewayClient) ListRunners(ctx context.Context, in *runnersv1.ListRunnersRequest, opts ...grpc.CallOption) (*runnersv1.ListRunnersResponse, error) {
	return nil, fmt.Errorf("ListRunners not implemented")
}

func (f *fakeRunnersGatewayClient) UpdateRunner(ctx context.Context, in *runnersv1.UpdateRunnerRequest, opts ...grpc.CallOption) (*runnersv1.UpdateRunnerResponse, error) {
	return nil, fmt.Errorf("UpdateRunner not implemented")
}

func (f *fakeRunnersGatewayClient) DeleteRunner(ctx context.Context, in *runnersv1.DeleteRunnerRequest, opts ...grpc.CallOption) (*runnersv1.DeleteRunnerResponse, error) {
	return nil, fmt.Errorf("DeleteRunner not implemented")
}

func (f *fakeRunnersGatewayClient) EnrollRunner(ctx context.Context, in *runnersv1.EnrollRunnerRequest, opts ...grpc.CallOption) (*runnersv1.EnrollRunnerResponse, error) {
	return nil, fmt.Errorf("EnrollRunner not implemented")
}

func (f *fakeRunnersGatewayClient) ListWorkloads(ctx context.Context, in *runnersv1.ListWorkloadsRequest, opts ...grpc.CallOption) (*runnersv1.ListWorkloadsResponse, error) {
	return nil, fmt.Errorf("ListWorkloads not implemented")
}

func (f *fakeRunnersGatewayClient) ListWorkloadsByThread(ctx context.Context, in *runnersv1.ListWorkloadsByThreadRequest, opts ...grpc.CallOption) (*runnersv1.ListWorkloadsByThreadResponse, error) {
	f.listReq = in
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResp, nil
}

func (f *fakeRunnersGatewayClient) GetWorkload(ctx context.Context, in *runnersv1.GetWorkloadRequest, opts ...grpc.CallOption) (*runnersv1.GetWorkloadResponse, error) {
	return nil, fmt.Errorf("GetWorkload not implemented")
}

func (f *fakeRunnersGatewayClient) TouchWorkload(ctx context.Context, in *runnersv1.TouchWorkloadRequest, opts ...grpc.CallOption) (*runnersv1.TouchWorkloadResponse, error) {
	f.touchReq = in
	if f.touchErr != nil {
		return nil, f.touchErr
	}
	return &runnersv1.TouchWorkloadResponse{}, nil
}

func (f *fakeRunnersGatewayClient) StreamWorkloadLogs(ctx context.Context, in *runnerv1.StreamWorkloadLogsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[runnerv1.StreamWorkloadLogsResponse], error) {
	return nil, fmt.Errorf("StreamWorkloadLogs not implemented")
}

func (f *fakeRunnersGatewayClient) GetVolume(ctx context.Context, in *runnersv1.GetVolumeRequest, opts ...grpc.CallOption) (*runnersv1.GetVolumeResponse, error) {
	return nil, fmt.Errorf("GetVolume not implemented")
}

func (f *fakeRunnersGatewayClient) ListVolumes(ctx context.Context, in *runnersv1.ListVolumesRequest, opts ...grpc.CallOption) (*runnersv1.ListVolumesResponse, error) {
	return nil, fmt.Errorf("ListVolumes not implemented")
}

func (f *fakeRunnersGatewayClient) ListVolumesByThread(ctx context.Context, in *runnersv1.ListVolumesByThreadRequest, opts ...grpc.CallOption) (*runnersv1.ListVolumesByThreadResponse, error) {
	return nil, fmt.Errorf("ListVolumesByThread not implemented")
}

func TestWorkloadFromProtoValid(t *testing.T) {
	createdAt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	removedAt := time.Date(2024, 6, 1, 13, 0, 0, 0, time.UTC)
	proto := &runnersv1.Workload{
		Meta: &runnersv1.EntityMeta{
			Id:        "workload-1",
			CreatedAt: timestamppb.New(createdAt),
		},
		AgentId:   "agent-1",
		Status:    runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
		RemovedAt: timestamppb.New(removedAt),
	}
	workload, err := workloadFromProto(proto)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if workload.ID != "workload-1" {
		t.Fatalf("unexpected id: %s", workload.ID)
	}
	if workload.AgentID != "agent-1" {
		t.Fatalf("unexpected agent id: %s", workload.AgentID)
	}
	if workload.Status != runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING {
		t.Fatalf("unexpected status: %v", workload.Status)
	}
	if !workload.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected created at: %v", workload.CreatedAt)
	}
	if workload.RemovedAt == nil || !workload.RemovedAt.Equal(removedAt) {
		t.Fatalf("unexpected removed at: %v", workload.RemovedAt)
	}
}

func TestWorkloadFromProtoMissingFields(t *testing.T) {
	createdAt := timestamppb.New(time.Now())
	validMeta := &runnersv1.EntityMeta{Id: "workload-1", CreatedAt: createdAt}
	valid := &runnersv1.Workload{
		Meta:    validMeta,
		AgentId: "agent-1",
		Status:  runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
	}

	tests := []struct {
		name     string
		workload *runnersv1.Workload
	}{
		{name: "nil", workload: nil},
		{name: "missing-meta", workload: &runnersv1.Workload{AgentId: valid.AgentId, Status: valid.Status}},
		{name: "missing-id", workload: &runnersv1.Workload{Meta: &runnersv1.EntityMeta{CreatedAt: createdAt}, AgentId: valid.AgentId, Status: valid.Status}},
		{name: "missing-created-at", workload: &runnersv1.Workload{Meta: &runnersv1.EntityMeta{Id: validMeta.Id}, AgentId: valid.AgentId, Status: valid.Status}},
		{name: "missing-agent-id", workload: &runnersv1.Workload{Meta: validMeta, Status: valid.Status}},
		{name: "missing-status", workload: &runnersv1.Workload{Meta: validMeta, AgentId: valid.AgentId}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := workloadFromProto(test.workload)
			if err == nil {
				t.Fatal("expected error for missing fields")
			}
		})
	}
}

func TestRunnersListWorkloadsByThread(t *testing.T) {
	createdAt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	resp := &runnersv1.ListWorkloadsByThreadResponse{
		Workloads: []*runnersv1.Workload{{
			Meta:    &runnersv1.EntityMeta{Id: "workload-1", CreatedAt: timestamppb.New(createdAt)},
			AgentId: "agent-1",
			Status:  runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
		}},
		NextPageToken: "next",
	}
	fake := &fakeRunnersGatewayClient{listResp: resp}
	client := NewRunners(fake)
	workloads, next, err := client.ListWorkloadsByThread(context.Background(), "thread-1", 50, "token")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if fake.listReq == nil {
		t.Fatal("expected list request")
	}
	if fake.listReq.GetThreadId() != "thread-1" || fake.listReq.GetPageSize() != 50 || fake.listReq.GetPageToken() != "token" {
		t.Fatalf("unexpected list request: %+v", fake.listReq)
	}
	if next != "next" {
		t.Fatalf("unexpected next token: %q", next)
	}
	if len(workloads) != 1 {
		t.Fatalf("unexpected workload count: %d", len(workloads))
	}
	if workloads[0].ID != "workload-1" || workloads[0].AgentID != "agent-1" {
		t.Fatalf("unexpected workload: %+v", workloads[0])
	}
}

func TestRunnersListWorkloadsByThreadValidation(t *testing.T) {
	client := NewRunners(&fakeRunnersGatewayClient{})
	_, _, err := client.ListWorkloadsByThread(context.Background(), "", 10, "")
	if err == nil {
		t.Fatal("expected error for missing thread id")
	}
}

func TestRunnersTouchWorkload(t *testing.T) {
	fake := &fakeRunnersGatewayClient{}
	client := NewRunners(fake)
	if err := client.TouchWorkload(context.Background(), "workload-1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if fake.touchReq == nil || fake.touchReq.GetId() != "workload-1" {
		t.Fatalf("unexpected touch request: %+v", fake.touchReq)
	}
}

func TestRunnersTouchWorkloadValidation(t *testing.T) {
	client := NewRunners(&fakeRunnersGatewayClient{})
	if err := client.TouchWorkload(context.Background(), " "); err == nil {
		t.Fatal("expected error for missing workload id")
	}
}

func TestWorkloadFromProtoRemovedAtNil(t *testing.T) {
	createdAt := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	proto := &runnersv1.Workload{
		Meta:    &runnersv1.EntityMeta{Id: "workload-1", CreatedAt: timestamppb.New(createdAt)},
		AgentId: "agent-1",
		Status:  runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING,
	}
	workload, err := workloadFromProto(proto)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if workload.RemovedAt != nil {
		t.Fatalf("expected nil removed at, got %v", workload.RemovedAt)
	}
	if !reflect.DeepEqual(workload.CreatedAt, createdAt) {
		t.Fatalf("unexpected created at: %v", workload.CreatedAt)
	}
}
