package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

const (
	workloadKeepaliveInterval = 10 * time.Second
	workloadKeepaliveTimeout  = 5 * time.Second
	workloadIdleReportTimeout = 5 * time.Second
)

// runKeepalive is the daemon's heartbeat. It touches the workload once a tick
// while a turn runs, which is what keeps the idle timeout off a busy agent,
// and it is where a wake from hibernation is noticed and acted on, whether the
// runner's SIGCONT announces it or the clock does. It handles the wake rather
// than Run because Run is inside the turn for as long as a turn takes, and a
// wake that lands mid-turn must not wait for the turn to end.
func (d *Daemon) runKeepalive(ctx context.Context, wake <-chan os.Signal) {
	workloadID := strings.TrimSpace(d.cfg.WorkloadID)
	if workloadID == "" {
		log.Printf("workload keepalive disabled: missing WORKLOAD_ID")
	}
	touch := func() {
		if workloadID == "" {
			return
		}
		if _, err := d.touchActiveWorkload(ctx, workloadID); err != nil && ctx.Err() == nil {
			log.Printf("workload keepalive failed: %v", err)
		}
	}
	touch()

	interval := d.keepaliveInterval
	if interval <= 0 {
		interval = workloadKeepaliveInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	watch := newWakeWatch(d.clocks)
	woken := func(cause string) {
		// Idempotent, and coalesced: the runner signals twice, the clock
		// confirms what the signal announced, and each repeat finds the wake
		// already handled. One pending inbox read is as good as any number.
		if !watch.admit() {
			log.Printf("wake by %s repeats one already handled", cause)
			return
		}
		log.Printf("woken from hibernation by %s; reopening the subscription and reading the inbox", cause)
		// Every connection is dead on the far side. The stream is the one
		// that would otherwise wait on its dead peer until TCP gave up, so it
		// is dropped and reopened; the unary calls fail and reconnect on their
		// own. What arrived while the workload slept is in the inbox, which
		// Run reads once asked. A turn in flight gets its keepalive now rather
		// than at the next tick. Nothing is replayed to the agent CLI.
		d.subscriber.Resubscribe()
		select {
		case d.wakeSync <- struct{}{}:
		default:
		}
		touch()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.processingWake:
			touch()
		case <-wake:
			woken("signal")
		case <-ticker.C:
			if jump := watch.tick(); jump > interval {
				log.Printf("wall clock moved %s further than monotonic time between ticks", jump)
				woken("clock")
			}
			touch()
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
