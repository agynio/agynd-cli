package tracingproxy

import (
	"testing"
	"time"

	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

func at(offsetMillis int) time.Time {
	return time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC).Add(time.Duration(offsetMillis) * time.Millisecond)
}

func count(t *testing.T, spans []*tracev1.Span, name string) int {
	t.Helper()
	total := 0
	for _, span := range spans {
		if span.Name == name {
			total++
		}
	}
	return total
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

func sampleTurn() Turn {
	input, output := int64(8607), int64(11)
	return Turn{
		ID:          "turn-1",
		SessionID:   "session-1",
		StartedAt:   at(0),
		EndedAt:     at(3000),
		Model:       "gpt-5.5",
		Provider:    "Agyn LLM",
		UserInput:   "hello",
		FinalOutput: "hi there",
		Steps: []ModelStep{{
			StartedAt: at(10),
			EndedAt:   at(2000),
			Text:      "hi there",
			Context:   []map[string]string{{"role": "user", "content": "hello"}},
			Usage:     &TokenUsage{InputTokens: &input, OutputTokens: &output},
			ToolCalls: []ToolCall{{
				CallID:    "call-1",
				Name:      "shell",
				StartedAt: at(100),
				EndedAt:   at(900),
				Arguments: map[string]any{"command": "ls"},
				Output:    "a\nb",
			}},
		}},
	}
}

func TestSpansFromTurnCarriesWhatTheModelSawAndSaid(t *testing.T) {
	spans, err := (&Proxy{}).spansFromTurn(sampleTurn())
	if err != nil {
		t.Fatalf("spans from turn: %v", err)
	}
	if len(spans) != 3 {
		t.Fatalf("expected message, call and tool, got %d spans", len(spans))
	}

	message := spanNamed(t, spans, spanInvocationMessage)
	requireStringAttr(t, message, "agyn.message.text", "hello")
	requireStringAttr(t, message, "agyn.llm.response_text", "hi there")

	call := spanNamed(t, spans, spanLLMCall)
	requireStringAttr(t, call, "gen_ai.request.model", "gpt-5.5")
	// The context is the reason a turn is stored at all.
	requireStringAttr(t, call, "agyn.llm.context", `[{"content":"hello","role":"user"}]`)
	requireIntAttr(t, call, "gen_ai.usage.input_tokens", 8607)
	requireIntAttr(t, call, "gen_ai.usage.output_tokens", 11)

	tool := spanNamed(t, spans, spanToolExecution)
	requireStringAttr(t, tool, "agyn.tool.name", "shell")
	requireStringAttr(t, tool, "agyn.tool.input", `{"command":"ls"}`)
	requireStringAttr(t, tool, "agyn.tool.output", `"a\nb"`)
}

// The view reads the turn as it happened rather than by sorting timestamps.
func TestSpansFromTurnNestMessageCallTool(t *testing.T) {
	spans, err := (&Proxy{}).spansFromTurn(sampleTurn())
	if err != nil {
		t.Fatalf("spans from turn: %v", err)
	}
	message := spanNamed(t, spans, spanInvocationMessage)
	call := spanNamed(t, spans, spanLLMCall)
	tool := spanNamed(t, spans, spanToolExecution)

	if len(message.ParentSpanId) != 0 {
		t.Fatal("expected the message to root the turn")
	}
	if string(call.ParentSpanId) != string(message.SpanId) {
		t.Fatal("expected the call to hang off the message")
	}
	if string(tool.ParentSpanId) != string(call.SpanId) {
		t.Fatal("expected the tool to hang off the call that invoked it")
	}
	for _, span := range spans {
		if string(span.TraceId) != string(message.TraceId) {
			t.Fatal("expected one trace for the turn")
		}
	}
}

// A resumed session replays the transcript, so the same turn must land on the
// spans it wrote before rather than beside them.
func TestSpansFromTurnAreStableAcrossReposts(t *testing.T) {
	first, err := (&Proxy{}).spansFromTurn(sampleTurn())
	if err != nil {
		t.Fatalf("spans from turn: %v", err)
	}
	second, err := (&Proxy{}).spansFromTurn(sampleTurn())
	if err != nil {
		t.Fatalf("spans from turn: %v", err)
	}
	for i := range first {
		if string(first[i].SpanId) != string(second[i].SpanId) {
			t.Fatalf("span %d changed id between posts", i)
		}
		if string(first[i].TraceId) != string(second[i].TraceId) {
			t.Fatalf("span %d changed trace between posts", i)
		}
	}
}

// A turn still running is stored in progress, and the plugin's next post
// completes it through the upsert.
func TestSpansFromTurnLeaveAnUnfinishedTurnInProgress(t *testing.T) {
	turn := sampleTurn()
	turn.EndedAt = time.Time{}
	turn.Steps[0].EndedAt = time.Time{}

	spans, err := (&Proxy{}).spansFromTurn(turn)
	if err != nil {
		t.Fatalf("spans from turn: %v", err)
	}
	if spanNamed(t, spans, spanInvocationMessage).EndTimeUnixNano != 0 {
		t.Fatal("expected an unfinished turn to be in progress")
	}
	if spanNamed(t, spans, spanLLMCall).EndTimeUnixNano != 0 {
		t.Fatal("expected an unfinished step to be in progress")
	}
}

func TestSpansFromTurnMarksFailures(t *testing.T) {
	turn := sampleTurn()
	turn.Steps[0].ToolCalls[0].Error = "exit 1"
	turn.Aborted = true

	spans, err := (&Proxy{}).spansFromTurn(turn)
	if err != nil {
		t.Fatalf("spans from turn: %v", err)
	}
	if spanNamed(t, spans, spanInvocationMessage).Status.GetCode() != tracev1.Status_STATUS_CODE_ERROR {
		t.Fatal("expected an aborted turn to be an error")
	}
	tool := spanNamed(t, spans, spanToolExecution)
	if tool.Status.GetCode() != tracev1.Status_STATUS_CODE_ERROR {
		t.Fatal("expected a failed tool call to be an error")
	}
	requireStringAttr(t, tool, "agyn.tool.error", "exit 1")
}

// A subagent's work belongs to the turn that delegated it.
func TestSpansFromTurnNestSubagentUnderItsParent(t *testing.T) {
	proxy := &Proxy{}
	parent, err := proxy.spansFromTurn(sampleTurn())
	if err != nil {
		t.Fatalf("spans from turn: %v", err)
	}
	child, err := proxy.spansFromTurn(Turn{
		ID:           "turn-2",
		SessionID:    "session-1",
		StartedAt:    at(200),
		EndedAt:      at(800),
		ParentTurnID: "turn-1",
	})
	if err != nil {
		t.Fatalf("spans from subagent turn: %v", err)
	}
	if string(spanNamed(t, child, spanInvocationMessage).ParentSpanId) !=
		string(spanNamed(t, parent, spanInvocationMessage).SpanId) {
		t.Fatal("expected the subagent turn to hang off the turn that spawned it")
	}
}

func TestSpansFromTurnRejectsATurnItCannotPlace(t *testing.T) {
	if _, err := (&Proxy{}).spansFromTurn(Turn{StartedAt: at(0)}); err == nil {
		t.Fatal("expected a turn with no id to be rejected")
	}
	if _, err := (&Proxy{}).spansFromTurn(Turn{ID: "turn-1"}); err == nil {
		t.Fatal("expected a turn with no start time to be rejected")
	}
}

func TestSpansFromTurnCountsStepsAndTools(t *testing.T) {
	turn := sampleTurn()
	turn.Steps = append(turn.Steps, ModelStep{
		StartedAt: at(2100),
		EndedAt:   at(2900),
		Text:      "done",
		ToolCalls: []ToolCall{
			{CallID: "call-2", Name: "read", StartedAt: at(2200), EndedAt: at(2300)},
			{CallID: "call-3", Name: "write", StartedAt: at(2400), EndedAt: at(2500)},
		},
	})

	spans, err := (&Proxy{}).spansFromTurn(turn)
	if err != nil {
		t.Fatalf("spans from turn: %v", err)
	}
	if got := count(t, spans, spanLLMCall); got != 2 {
		t.Fatalf("expected a call per step, got %d", got)
	}
	if got := count(t, spans, spanToolExecution); got != 3 {
		t.Fatalf("expected an execution per tool call, got %d", got)
	}
}
