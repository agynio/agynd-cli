package platform

import (
	"context"
	"fmt"
	"strings"
	"time"

	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	runnersv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/runners/v1"
)

type Workload struct {
	ID        string
	AgentID   string
	Status    runnersv1.WorkloadStatus
	CreatedAt time.Time
	RemovedAt *time.Time
}

type Runners struct {
	client gatewayv1.RunnersGatewayClient
}

func NewRunners(client gatewayv1.RunnersGatewayClient) *Runners {
	return &Runners{client: client}
}

func (r *Runners) ListWorkloadsByThread(ctx context.Context, threadID string, pageSize int32, pageToken string) ([]Workload, string, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, "", fmt.Errorf("thread id is required")
	}
	resp, err := r.client.ListWorkloadsByThread(ctx, &runnersv1.ListWorkloadsByThreadRequest{
		ThreadId:  threadID,
		PageSize:  pageSize,
		PageToken: pageToken,
	})
	if err != nil {
		return nil, "", err
	}
	workloads := make([]Workload, 0, len(resp.GetWorkloads()))
	for _, workload := range resp.GetWorkloads() {
		converted, err := workloadFromProto(workload)
		if err != nil {
			return nil, "", err
		}
		workloads = append(workloads, converted)
	}
	return workloads, resp.GetNextPageToken(), nil
}

func (r *Runners) TouchWorkload(ctx context.Context, workloadID string) error {
	workloadID = strings.TrimSpace(workloadID)
	if workloadID == "" {
		return fmt.Errorf("workload id is required")
	}
	_, err := r.client.TouchWorkload(ctx, &runnersv1.TouchWorkloadRequest{Id: workloadID})
	return err
}

func workloadFromProto(workload *runnersv1.Workload) (Workload, error) {
	if workload == nil {
		return Workload{}, fmt.Errorf("workload is required")
	}
	meta := workload.GetMeta()
	if meta == nil {
		return Workload{}, fmt.Errorf("workload meta is required")
	}
	workloadID := strings.TrimSpace(meta.GetId())
	if workloadID == "" {
		return Workload{}, fmt.Errorf("workload id is required")
	}
	agentID := strings.TrimSpace(workload.GetAgentId())
	if agentID == "" {
		return Workload{}, fmt.Errorf("workload agent id is required")
	}
	status := workload.GetStatus()
	if status == runnersv1.WorkloadStatus_WORKLOAD_STATUS_UNSPECIFIED {
		return Workload{}, fmt.Errorf("workload status is required")
	}
	createdAt := meta.GetCreatedAt()
	if createdAt == nil {
		return Workload{}, fmt.Errorf("workload created at is required")
	}
	createdAtTime := createdAt.AsTime()
	var removedAt *time.Time
	if workload.RemovedAt != nil {
		removedTime := workload.GetRemovedAt().AsTime()
		removedAt = &removedTime
	}

	return Workload{
		ID:        workloadID,
		AgentID:   agentID,
		Status:    status,
		CreatedAt: createdAtTime,
		RemovedAt: removedAt,
	}, nil
}
