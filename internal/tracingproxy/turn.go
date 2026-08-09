package tracingproxy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

// A tracing plugin reads the agent CLI's session transcript and posts the turns
// it finds here. The shape is the agent CLI's own vocabulary -- a turn, the
// model steps it took, the tools each step called -- rather than spans, because
// a plugin should not have to know how this platform models a trace.
type Turn struct {
	// Identifies the turn within its session, so a resumed session that replays
	// the transcript lands on the spans it wrote before rather than beside them.
	ID          string      `json:"id"`
	SessionID   string      `json:"sessionId"`
	StartedAt   time.Time   `json:"startedAt"`
	EndedAt     time.Time   `json:"endedAt"`
	Model       string      `json:"model"`
	Provider    string      `json:"provider"`
	UserInput   string      `json:"userInput"`
	FinalOutput string      `json:"finalOutput"`
	Steps       []ModelStep `json:"steps"`
	Usage       *TokenUsage `json:"usage"`
	// A turn the agent abandoned still describes what it did, so it is stored
	// and marked rather than dropped.
	Aborted bool   `json:"aborted"`
	Error   string `json:"error"`
	// Set on a subagent's turns, naming the turn that spawned it.
	ParentTurnID string `json:"parentTurnId"`
}

type ModelStep struct {
	StartedAt time.Time   `json:"startedAt"`
	EndedAt   time.Time   `json:"endedAt"`
	Reasoning string      `json:"reasoning"`
	Text      string      `json:"text"`
	Context   any         `json:"context"`
	ToolCalls []ToolCall  `json:"toolCalls"`
	Usage     *TokenUsage `json:"usage"`
}

type ToolCall struct {
	CallID    string    `json:"callId"`
	Name      string    `json:"name"`
	Server    string    `json:"server"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	Arguments any       `json:"arguments"`
	Output    any       `json:"output"`
	Error     string    `json:"error"`
}

type TokenUsage struct {
	InputTokens     *int64 `json:"inputTokens"`
	OutputTokens    *int64 `json:"outputTokens"`
	CachedTokens    *int64 `json:"cachedTokens"`
	ReasoningTokens *int64 `json:"reasoningTokens"`
	TotalTokens     *int64 `json:"totalTokens"`
}

const (
	spanToolExecution = "tool.execution"

	// Names the spans a plugin's turn produced, as distinct from anything the
	// agent CLI exported itself.
	turnScopeName = "agynd.turn"
)

// spansFromTurn renders one turn as the spans the run view reads: the message
// that opened it, a call per model step, and an execution per tool the step
// invoked. Each hangs off the last, so the view reads the turn as it happened
// rather than by sorting timestamps drawn from different clocks.
func (p *Proxy) spansFromTurn(turn Turn) ([]*tracev1.Span, error) {
	if turn.ID == "" {
		return nil, fmt.Errorf("turn id is required")
	}
	if turn.StartedAt.IsZero() {
		return nil, fmt.Errorf("turn %s has no start time", turn.ID)
	}

	traceID := traceIDForMessage(p.turnTraceSeed(turn))
	messageID := turnSpanID(turn.ID, "message")

	message := &tracev1.Span{
		TraceId:           traceID,
		SpanId:            messageID,
		ParentSpanId:      parentTurnSpanID(turn),
		Name:              spanInvocationMessage,
		Kind:              tracev1.Span_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: uint64(turn.StartedAt.UnixNano()),
		EndTimeUnixNano:   endOf(turn.EndedAt),
		Attributes: []*commonv1.KeyValue{
			stringAttr("agyn.message.role", "user"),
			stringAttr("agyn.message.text", turn.UserInput),
		},
		Status: turnStatus(turn),
	}
	if turn.FinalOutput != "" {
		message.Attributes = append(message.Attributes, stringAttr("agyn.llm.response_text", turn.FinalOutput))
	}
	if turn.SessionID != "" {
		message.Attributes = append(message.Attributes, stringAttr("agyn.agent.session.id", turn.SessionID))
	}
	spans := []*tracev1.Span{message}

	for i, step := range turn.Steps {
		call := &tracev1.Span{
			TraceId:           traceID,
			SpanId:            turnSpanID(turn.ID, fmt.Sprintf("step.%d", i)),
			ParentSpanId:      messageID,
			Name:              spanLLMCall,
			Kind:              tracev1.Span_SPAN_KIND_CLIENT,
			StartTimeUnixNano: startOf(step.StartedAt, turn.StartedAt),
			EndTimeUnixNano:   endOf(step.EndedAt),
			Attributes: []*commonv1.KeyValue{
				stringAttr("gen_ai.system", turn.Provider),
				stringAttr("gen_ai.request.model", turn.Model),
			},
			Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
		}
		// The context is what the model was shown, and is the reason tracing
		// stores a turn at all -- without it a call records that it happened.
		if step.Context != nil {
			call.Attributes = append(call.Attributes, jsonAttr("agyn.llm.context", step.Context))
		}
		if step.Text != "" {
			call.Attributes = append(call.Attributes, stringAttr("agyn.llm.response_text", step.Text))
		}
		if step.Reasoning != "" {
			call.Attributes = append(call.Attributes, stringAttr("agyn.llm.reasoning", step.Reasoning))
		}
		call.Attributes = append(call.Attributes, usageAttrs(step.Usage)...)
		spans = append(spans, call)

		for j, tool := range step.ToolCalls {
			spans = append(spans, toolSpan(traceID, call.SpanId, turn.ID, i, j, tool))
		}
	}

	// A turn reports usage of its own when the transcript totals it rather than
	// attributing it per step.
	if len(turn.Steps) == 0 {
		message.Attributes = append(message.Attributes, usageAttrs(turn.Usage)...)
	}
	return spans, nil
}

func toolSpan(traceID, parentID []byte, turnID string, step, index int, tool ToolCall) *tracev1.Span {
	span := &tracev1.Span{
		TraceId:           traceID,
		SpanId:            turnSpanID(turnID, fmt.Sprintf("step.%d.tool.%d", step, index)),
		ParentSpanId:      parentID,
		Name:              spanToolExecution,
		Kind:              tracev1.Span_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: uint64(tool.StartedAt.UnixNano()),
		EndTimeUnixNano:   endOf(tool.EndedAt),
		Attributes: []*commonv1.KeyValue{
			stringAttr("agyn.tool.name", tool.Name),
		},
		Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
	}
	if tool.StartedAt.IsZero() {
		span.StartTimeUnixNano = 0
	}
	if tool.CallID != "" {
		span.Attributes = append(span.Attributes, stringAttr("agyn.tool.call_id", tool.CallID))
	}
	if tool.Server != "" {
		span.Attributes = append(span.Attributes, stringAttr("agyn.tool.server", tool.Server))
	}
	if tool.Arguments != nil {
		span.Attributes = append(span.Attributes, jsonAttr("agyn.tool.input", tool.Arguments))
	}
	if tool.Output != nil {
		span.Attributes = append(span.Attributes, jsonAttr("agyn.tool.output", tool.Output))
	}
	if tool.Error != "" {
		span.Attributes = append(span.Attributes, stringAttr("agyn.tool.error", tool.Error))
		span.Status = &tracev1.Status{Code: tracev1.Status_STATUS_CODE_ERROR, Message: tool.Error}
	}
	return span
}

// turnSpanID derives a span's id from the turn it belongs to rather than
// drawing a new one. A plugin re-posting a turn -- a resumed session, a turn
// that was in progress when it was first sent -- then writes the same span ids,
// and ingest upserts them onto the rows already there instead of doubling them.
func turnSpanID(turnID, part string) []byte {
	sum := sha256.Sum256([]byte("agyn.span." + turnID + "." + part))
	return sum[:8]
}

// A subagent's turns hang off the turn that spawned them, so the run view shows
// the work a turn delegated as part of that turn.
func parentTurnSpanID(turn Turn) []byte {
	if turn.ParentTurnID == "" {
		return nil
	}
	return turnSpanID(turn.ParentTurnID, "message")
}

// turnTraceSeed keeps a turn in the trace of the message being answered. A
// sandbox has no message, so the session stands in and its turns still group.
func (p *Proxy) turnTraceSeed(turn Turn) string {
	if messageID := p.messageIDValue(); messageID != "" {
		return messageID
	}
	return turn.SessionID
}

// Arguments, output and context are whatever the agent CLI recorded, so they
// are carried as JSON rather than flattened into attributes that would differ
// per tool.
func jsonAttr(key string, value any) *commonv1.KeyValue {
	encoded, err := json.Marshal(value)
	if err != nil {
		return stringAttr(key, fmt.Sprintf("%v", value))
	}
	return stringAttr(key, string(encoded))
}

func turnStatus(turn Turn) *tracev1.Status {
	switch {
	case turn.Error != "":
		return &tracev1.Status{Code: tracev1.Status_STATUS_CODE_ERROR, Message: turn.Error}
	case turn.Aborted:
		return &tracev1.Status{Code: tracev1.Status_STATUS_CODE_ERROR, Message: "turn aborted"}
	default:
		return &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK}
	}
}

// A turn that has not ended is stored in progress: the run view shows it
// running, and the plugin's next post completes it through the upsert.
func endOf(at time.Time) uint64 {
	if at.IsZero() {
		return 0
	}
	return uint64(at.UnixNano())
}

func startOf(at, fallback time.Time) uint64 {
	if at.IsZero() {
		return uint64(fallback.UnixNano())
	}
	return uint64(at.UnixNano())
}

func usageAttrs(usage *TokenUsage) []*commonv1.KeyValue {
	if usage == nil {
		return nil
	}
	counts := []struct {
		value *int64
		key   string
	}{
		{usage.InputTokens, "gen_ai.usage.input_tokens"},
		{usage.OutputTokens, "gen_ai.usage.output_tokens"},
		{usage.CachedTokens, "gen_ai.usage.cache_read.input_tokens"},
		{usage.ReasoningTokens, "agyn.usage.reasoning_tokens"},
		{usage.TotalTokens, "gen_ai.usage.total_tokens"},
	}
	attrs := make([]*commonv1.KeyValue, 0, len(counts))
	for _, count := range counts {
		if count.value != nil {
			attrs = append(attrs, intAttr(count.key, *count.value))
		}
	}
	return attrs
}
