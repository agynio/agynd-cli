package daemon

import (
	"context"
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
	cancel()
	if err != nil {
		return false, err
	}
	return true, nil
}
