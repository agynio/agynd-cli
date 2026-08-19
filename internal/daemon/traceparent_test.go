package daemon

import (
	"encoding/hex"
	"testing"

	"github.com/agynio/agynd-cli/internal/tracing"
)

// The hook and agn are given the same trace, in the two spellings each reads.
// They diverged once and the turns of one wake cycle were split across two.
func TestTraceparentCarriesTheHooksTrace(t *testing.T) {
	const workloadID = "9f1c2f5e-2a4b-4c6d-8e0f-1a2b3c4d5e6f"

	traceparent := traceparentFor(workloadID)
	want := "00-" + traceHookTraceID(workloadID) + "-"
	if got := traceparent[:len(want)]; got != want {
		t.Fatalf("expected traceparent to carry trace %s, got %s", traceHookTraceID(workloadID), traceparent)
	}
	if len(traceparent) != len("00-")+32+1+16+len("-01") {
		t.Fatalf("expected a well-formed traceparent, got %q", traceparent)
	}
	if _, err := hex.DecodeString(traceparent[36:52]); err != nil {
		t.Fatalf("expected a hex span id, got %q: %v", traceparent[36:52], err)
	}
}

// Two starts of one workload reopen the trace rather than splitting it, so the
// parent they name has to be the same both times.
func TestTraceparentIsStableForAWorkload(t *testing.T) {
	const workloadID = "3b7d1a90-11cc-4f2e-9a55-77e8c0d13b42"
	if traceparentFor(workloadID) != traceparentFor(workloadID) {
		t.Fatal("expected the same workload to yield the same traceparent")
	}
	if traceparentFor(workloadID) == traceparentFor("0d2e4a61-88ab-4c3d-9e77-5f1b2c3d4e5a") {
		t.Fatal("expected different workloads to yield different traces")
	}
	_ = tracing.TraceID(workloadID)
}
