package tracing

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/agynio/agynd-cli/internal/transcript"

	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

func int64Ptr(value int64) *int64 { return &value }

func sampleTurn() transcript.Turn {
	started := time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC)
	return transcript.Turn{
		ID:          "turn-1",
		SessionID:   "session-1",
		StartedAt:   started,
		EndedAt:     started.Add(3 * time.Second),
		Model:       "gpt-5",
		UserInput:   "list the files",
		FinalOutput: "there are two",
		Steps: []transcript.Step{{
			ID:        "turn-1@step",
			StartedAt: started,
			EndedAt:   started.Add(3 * time.Second),
			Model:     "gpt-5",
			Text:      "there are two",
			Reasoning: "check the directory first",
			Usage: &transcript.Usage{
				InputTokens:     int64Ptr(120),
				OutputTokens:    int64Ptr(40),
				CachedTokens:    int64Ptr(100),
				ReasoningTokens: int64Ptr(12),
			},
			ToolCalls: []transcript.ToolCall{{
				CallID:    "call-1",
				Name:      "shell",
				StartedAt: started.Add(time.Second),
				EndedAt:   started.Add(2 * time.Second),
				Arguments: map[string]any{"command": "ls"},
				Output:    "a\nb",
			}},
		}},
	}
}

func exportedSpans(t *testing.T, service *recordingService) []*tracev1.Span {
	t.Helper()
	if len(service.requests) != 1 {
		t.Fatalf("expected 1 export, got %d", len(service.requests))
	}
	resourceSpans := service.requests[0].ResourceSpans
	if len(resourceSpans) != 1 || len(resourceSpans[0].ScopeSpans) != 1 {
		t.Fatal("expected one scope in one resource")
	}
	return resourceSpans[0].ScopeSpans[0].Spans
}

func spanNamed(t *testing.T, spans []*tracev1.Span, name string) *tracev1.Span {
	t.Helper()
	for _, span := range spans {
		if span.Name == name {
			return span
		}
	}
	t.Fatalf("expected a %s span, got %d spans", name, len(spans))
	return nil
}

func TestTurnBecomesACallAndItsToolExecution(t *testing.T) {
	exporter, service := startExporter(t, "workload-1")

	if err := exporter.Turns(context.Background(), []transcript.Turn{sampleTurn()}); err != nil {
		t.Fatalf("expected the turn to export, got %v", err)
	}

	spans := exportedSpans(t, service)
	if len(spans) != 2 {
		t.Fatalf("expected a call and an execution, got %d spans", len(spans))
	}

	call := spanNamed(t, spans, spanLLMCall)
	if got := attr(call.Attributes, "gen_ai.request.model"); got != "gpt-5" {
		t.Fatalf("expected the model on the call, got %q", got)
	}
	if got := attr(call.Attributes, "agyn.llm.response_text"); got != "there are two" {
		t.Fatalf("expected the reply on the call, got %q", got)
	}
	if got := attr(call.Attributes, "agyn.llm.reasoning"); got != "check the directory first" {
		t.Fatalf("expected the reasoning on the call, got %q", got)
	}

	tool := spanNamed(t, spans, spanToolExecution)
	if !bytes.Equal(tool.ParentSpanId, call.SpanId) {
		t.Fatal("expected the execution to hang off the call that invoked it")
	}
	if got := attr(tool.Attributes, "agyn.tool.input"); got != `{"command":"ls"}` {
		t.Fatalf("expected the tool input as JSON, got %q", got)
	}
	if got := attr(tool.Attributes, "agyn.tool.output"); got != `"a\nb"` {
		t.Fatalf("expected the tool output, got %q", got)
	}
}

// The counts are what a run is read for, so they are asserted by name rather
// than assumed to have travelled.
func TestCallCarriesTheUsageCounts(t *testing.T) {
	exporter, service := startExporter(t, "workload-1")

	if err := exporter.Turns(context.Background(), []transcript.Turn{sampleTurn()}); err != nil {
		t.Fatalf("expected the turn to export, got %v", err)
	}

	call := spanNamed(t, exportedSpans(t, service), spanLLMCall)
	for key, want := range map[string]int64{
		"gen_ai.usage.input_tokens":            120,
		"gen_ai.usage.output_tokens":           40,
		"gen_ai.usage.cache_read.input_tokens": 100,
		"agyn.usage.reasoning_tokens":          12,
	} {
		if got := intAttrValue(call.Attributes, key); got != want {
			t.Fatalf("expected %s %d, got %d", key, want, got)
		}
	}
}

func intAttrValue(attrs []*commonv1.KeyValue, key string) int64 {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value.GetIntValue()
		}
	}
	return 0
}

// A resumed session replays the transcript, and a failed export is retried, so
// the same turn is exported more than once. The ids have to be the ones already
// written or the retry lands beside its own spans rather than on them.
func TestTurnSpanIDsAreStableAcrossExports(t *testing.T) {
	first, firstService := startExporter(t, "workload-1")
	if err := first.Turns(context.Background(), []transcript.Turn{sampleTurn()}); err != nil {
		t.Fatalf("expected the turn to export, got %v", err)
	}
	second, secondService := startExporter(t, "workload-1")
	if err := second.Turns(context.Background(), []transcript.Turn{sampleTurn()}); err != nil {
		t.Fatalf("expected the turn to export again, got %v", err)
	}

	before, after := exportedSpans(t, firstService), exportedSpans(t, secondService)
	if len(before) != len(after) {
		t.Fatalf("expected the same spans, got %d then %d", len(before), len(after))
	}
	for i := range before {
		if !bytes.Equal(before[i].SpanId, after[i].SpanId) {
			t.Fatalf("span %d changed id between exports", i)
		}
		if !bytes.Equal(before[i].TraceId, after[i].TraceId) {
			t.Fatalf("span %d changed trace between exports", i)
		}
	}
}

// A subagent's turn is the one case the transcript names a parent for, so it is
// the one case a call hangs off something rather than rooting in the trace.
func TestSubagentTurnHangsOffTheTurnThatSpawnedIt(t *testing.T) {
	exporter, service := startExporter(t, "workload-1")
	parent := sampleTurn()
	child := sampleTurn()
	child.ID = "turn-2"
	child.ParentTurnID = parent.ID

	if err := exporter.Turns(context.Background(), []transcript.Turn{parent, child}); err != nil {
		t.Fatalf("expected both turns to export, got %v", err)
	}

	spans := exportedSpans(t, service)
	var root, nested *tracev1.Span
	for _, span := range spans {
		if span.Name != spanLLMCall {
			continue
		}
		if bytes.Equal(span.SpanId, SpanID("turn-1", "step.0")) {
			root = span
		}
		if bytes.Equal(span.SpanId, SpanID("turn-2", "step.0")) {
			nested = span
		}
	}
	if root == nil || nested == nil {
		t.Fatal("expected a call for each turn")
	}
	if len(root.ParentSpanId) != 0 {
		t.Fatal("expected the parent turn's call to root in the trace")
	}
	if !bytes.Equal(nested.ParentSpanId, root.SpanId) {
		t.Fatal("expected the subagent's call to hang off the turn that spawned it")
	}
}

// A tool that failed is the thing a reader goes looking for, so it is marked on
// the span rather than left to be inferred from its output.
func TestAFailedToolMarksItsSpan(t *testing.T) {
	exporter, service := startExporter(t, "workload-1")
	turn := sampleTurn()
	turn.Steps[0].ToolCalls[0].Error = "exit status 1"

	if err := exporter.Turns(context.Background(), []transcript.Turn{turn}); err != nil {
		t.Fatalf("expected the turn to export, got %v", err)
	}

	tool := spanNamed(t, exportedSpans(t, service), spanToolExecution)
	if tool.Status.GetCode() != tracev1.Status_STATUS_CODE_ERROR {
		t.Fatalf("expected an error status, got %v", tool.Status.GetCode())
	}
	if got := attr(tool.Attributes, "agyn.tool.error"); got != "exit status 1" {
		t.Fatalf("expected the error on the span, got %q", got)
	}
}

// A turn still in flight has no end, and stamping one would record it as having
// finished when the export happened. It is left open for the next export to
// complete through the upsert.
func TestAnUnfinishedCallIsLeftOpen(t *testing.T) {
	exporter, service := startExporter(t, "workload-1")
	turn := sampleTurn()
	turn.Steps[0].EndedAt = time.Time{}

	if err := exporter.Turns(context.Background(), []transcript.Turn{turn}); err != nil {
		t.Fatalf("expected the turn to export, got %v", err)
	}

	call := spanNamed(t, exportedSpans(t, service), spanLLMCall)
	if call.EndTimeUnixNano != 0 {
		t.Fatalf("expected no end time, got %d", call.EndTimeUnixNano)
	}
	if call.StartTimeUnixNano == 0 {
		t.Fatal("expected the start to survive")
	}
}

func TestExportingNoTurnsSendsNothing(t *testing.T) {
	exporter, service := startExporter(t, "workload-1")

	if err := exporter.Turns(context.Background(), nil); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(service.requests) != 0 {
		t.Fatalf("expected no export, got %d", len(service.requests))
	}
}

// The context is what the run view pages through, and it is read from events
// rather than an attribute -- an attribute carrying a whole conversation cannot
// be paged.
func TestCallCarriesWhatTheModelWasShown(t *testing.T) {
	exporter, service := startExporter(t, "workload-1")
	turn := sampleTurn()
	at := turn.StartedAt
	turn.Steps[0].Context = []transcript.ContextItem{
		{Role: "user", Text: "list the files", IsNew: false, At: at},
		{Role: "tool", JSON: map[string]any{"out": "a"}, IsNew: true, At: at.Add(time.Second)},
	}

	if err := exporter.Turns(context.Background(), []transcript.Turn{turn}); err != nil {
		t.Fatalf("expected the turn to export, got %v", err)
	}

	call := spanNamed(t, exportedSpans(t, service), spanLLMCall)
	if len(call.Events) != 2 {
		t.Fatalf("expected one event per context item, got %d", len(call.Events))
	}
	if call.Events[0].Name != eventLLMContextItem {
		t.Fatalf("expected %q, got %q", eventLLMContextItem, call.Events[0].Name)
	}
	if got := attr(call.Events[0].Attributes, "agyn.context.role"); got != "user" {
		t.Fatalf("expected the role, got %q", got)
	}
	if got := attr(call.Events[0].Attributes, "agyn.context.text"); got != "list the files" {
		t.Fatalf("expected the text, got %q", got)
	}
	// Read as a string, not a bool: that is the shape the reader parses.
	if got := attr(call.Events[0].Attributes, "agyn.context.is_new"); got != "false" {
		t.Fatalf("expected carried-over to be false, got %q", got)
	}
	if got := intAttrValue(call.Events[0].Attributes, "agyn.context.size_bytes"); got != 14 {
		t.Fatalf("expected the size, got %d", got)
	}
	if got := attr(call.Events[1].Attributes, "agyn.context.is_new"); got != "true" {
		t.Fatalf("expected the added item to be new, got %q", got)
	}
	if got := attr(call.Events[1].Attributes, "agyn.context.content_json"); got != `{"out":"a"}` {
		t.Fatalf("expected structured content as JSON, got %q", got)
	}
}

// A step that only called tools says nothing at all unless the calls are on it:
// the reader lists them from the call, not from the executions beneath it.
func TestCallListsTheToolsItCalled(t *testing.T) {
	exporter, service := startExporter(t, "workload-1")

	if err := exporter.Turns(context.Background(), []transcript.Turn{sampleTurn()}); err != nil {
		t.Fatalf("expected the turn to export, got %v", err)
	}

	call := spanNamed(t, exportedSpans(t, service), spanLLMCall)
	got := attr(call.Attributes, "agyn.llm.tool_calls")
	// call_id and name are the two fields the reader requires of each entry.
	want := `[{"arguments":{"command":"ls"},"call_id":"call-1","name":"shell"}]`
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
	if got := attr(call.Attributes, "gen_ai.system"); got != llmSystem {
		t.Fatalf("expected the system, got %q", got)
	}
}
