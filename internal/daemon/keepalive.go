package daemon

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	workloadKeepaliveInterval = 10 * time.Second
	workloadKeepaliveTimeout  = 5 * time.Second
	workloadIdleReportTimeout = 5 * time.Second
)

func (d *Daemon) runKeepalive(ctx context.Context) {
	workloadID := strings.TrimSpace(d.cfg.WorkloadID)
	if workloadID == "" {
		log.Printf("workload keepalive disabled: missing WORKLOAD_ID")
		return
	}

	if _, err := d.touchActiveWorkload(ctx, workloadID); err != nil && ctx.Err() == nil {
		log.Printf("workload keepalive failed: %v", err)
	}

	ticker := time.NewTicker(workloadKeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.processingWake:
			if _, err := d.touchActiveWorkload(ctx, workloadID); err != nil && ctx.Err() == nil {
				log.Printf("workload keepalive failed: %v", err)
			}
		case <-ticker.C:
			if _, err := d.touchActiveWorkload(ctx, workloadID); err != nil && ctx.Err() == nil {
				log.Printf("workload keepalive failed: %v", err)
			}
		}
	}
}

// reportWorkloadIdle tells Runners the turn is over, once its inbox items are
// acked. The call flips the workload's agent_state to idle at once rather than
// after the keepalive grace, and is the moment the Orchestrator may hibernate
// the workload: between one turn and the next, if the inbox is still empty when
// it looks. A report that never arrives costs nothing but latency, because the
// activity sweep reaches the same state on its own schedule, so a failure is
// logged and never fatal. In holder mode there is no turn, and so no report.
func (d *Daemon) reportWorkloadIdle(ctx context.Context) {
	workloadID := strings.TrimSpace(d.cfg.WorkloadID)
	if workloadID == "" {
		return
	}
	reportCtx, cancel := context.WithTimeout(ctx, workloadIdleReportTimeout)
	err := d.runners.ReportWorkloadIdle(reportCtx, workloadID)
	err = operationContextErr(reportCtx, err)
	cancel()
	if err != nil && ctx.Err() == nil {
		log.Printf("report workload %s idle failed: %v", workloadID, err)
	}
}

func (d *Daemon) touchActiveWorkload(ctx context.Context, workloadID string) (bool, error) {
	if !d.processing.Load() {
		return false, nil
	}
	workloadID = strings.TrimSpace(workloadID)
	if workloadID == "" {
		return false, nil
	}
	touchCtx, cancel := context.WithTimeout(ctx, workloadKeepaliveTimeout)
	err := d.runners.TouchWorkload(touchCtx, workloadID)
	err = operationContextErr(touchCtx, err)
	cancel()
	if err != nil {
		return false, operationError(
			opKeepaliveTouch,
			workloadKeepaliveTimeout,
			fmt.Errorf("touch active workload %s: %w", workloadID, err),
		)
	}
	return true, nil
}
