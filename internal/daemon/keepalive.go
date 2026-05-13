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
