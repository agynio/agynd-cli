package daemon

import (
	"context"
	"log"
	"strings"
	"time"

	runnersv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/runners/v1"
	"github.com/agynio/agynd-cli/internal/platform"
)

const (
	workloadKeepaliveInterval = 10 * time.Second
	workloadKeepaliveTimeout  = 5 * time.Second
	workloadListTimeout       = 5 * time.Second
)

func (d *Daemon) runKeepalive(ctx context.Context) {
	threadID := strings.TrimSpace(d.cfg.ThreadID)
	if threadID == "" {
		return
	}

	if _, err := d.touchActiveWorkload(ctx, threadID); err != nil && ctx.Err() == nil {
		log.Printf("workload keepalive failed: %v", err)
	}

	ticker := time.NewTicker(workloadKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := d.touchActiveWorkload(ctx, threadID); err != nil && ctx.Err() == nil {
				log.Printf("workload keepalive failed: %v", err)
			}
		}
	}
}

func (d *Daemon) touchActiveWorkload(ctx context.Context, threadID string) (bool, error) {
	workload, ok, err := d.findActiveWorkload(ctx, threadID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	touchCtx, cancel := context.WithTimeout(ctx, workloadKeepaliveTimeout)
	err = d.runners.TouchWorkload(touchCtx, workload.ID)
	cancel()
	if err != nil {
		return false, err
	}
	return true, nil
}

func (d *Daemon) findActiveWorkload(ctx context.Context, threadID string) (platform.Workload, bool, error) {
	pageToken := ""
	var selected platform.Workload
	found := false
	for {
		listCtx, cancel := context.WithTimeout(ctx, workloadListTimeout)
		workloads, nextToken, err := d.runners.ListWorkloadsByThread(listCtx, threadID, pageSize, pageToken)
		cancel()
		if err != nil {
			return platform.Workload{}, false, err
		}
		for _, workload := range workloads {
			if workload.AgentID != d.cfg.AgentID.String() {
				continue
			}
			if workload.Status != runnersv1.WorkloadStatus_WORKLOAD_STATUS_RUNNING {
				continue
			}
			if workload.RemovedAt != nil {
				continue
			}
			if !found || workload.CreatedAt.After(selected.CreatedAt) {
				selected = workload
				found = true
			}
		}
		if nextToken == "" {
			break
		}
		pageToken = nextToken
	}
	if !found {
		return platform.Workload{}, false, nil
	}
	return selected, true, nil
}
