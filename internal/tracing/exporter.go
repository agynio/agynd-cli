// Package tracing exports spans to the Tracing service.
//
// A producer dials the service itself and is attributed to the identity it
// holds, so nothing enriches spans on the way out. What the agent CLI did is
// exported by the tracing plugin the agent runtime carries; what the platform
// handed it is exported here.
package tracing

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/agynio/agynd-cli/internal/transcript"

	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	scopeName = "agynd"

	spanInvocationMessage = "invocation.message"

	messageIDAttributeKey  = "agyn.thread.message.id"
	workloadIDAttributeKey = "agyn.workload.id"
)

type Config struct {
	// Address of the Tracing service, reached over the OpenZiti overlay.
	Address string
	// Names the wake cycle. Every producer in the container derives the trace
	// from it, which is how a turn's spans and the message that opened it meet
	// without anything being passed between them.
	WorkloadID string
}

type Exporter struct {
	conn       *grpc.ClientConn
	client     collectortracev1.TraceServiceClient
	workloadID string
}

func NewExporter(cfg Config) (*Exporter, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("tracing address is required")
	}
	conn, err := grpc.NewClient(cfg.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial tracing address: %w", err)
	}
	return &Exporter{
		conn:       conn,
		client:     collectortracev1.NewTraceServiceClient(conn),
		workloadID: cfg.WorkloadID,
	}, nil
}

// Message is what the platform handed the agent CLI to answer.
type Message struct {
	ID        string
	ThreadID  string
	SenderID  string
	Body      string
	CreatedAt time.Time
}

// InvocationMessage records the item that opened a turn. The agent CLI's own
// work hangs off it, exported separately by the tracing plugin, which finds it
// through the trace they both derive from the workload.
func (e *Exporter) InvocationMessage(ctx context.Context, message Message) error {
	if message.ID == "" {
		return fmt.Errorf("message id is required")
	}
	at := message.CreatedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	span := &tracev1.Span{
		TraceId: TraceID(e.workloadID),
		SpanId:  SpanID(message.ID, "message"),
		Name:    spanInvocationMessage,
		Kind:    tracev1.Span_SPAN_KIND_INTERNAL,
		// A message is an instant, not an interval: the turn's duration belongs
		// to the work that answers it.
		StartTimeUnixNano: uint64(at.UnixNano()),
		EndTimeUnixNano:   uint64(at.UnixNano()),
		Attributes: []*commonv1.KeyValue{
			stringAttr("agyn.message.role", "user"),
			stringAttr("agyn.message.text", message.Body),
		},
		Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
	}
	if message.SenderID != "" {
		span.Attributes = append(span.Attributes, stringAttr("agyn.message.sender.id", message.SenderID))
	}

	_, err := e.client.Export(ctx, &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{{
			Resource: &resourcev1.Resource{Attributes: e.resourceAttributes(message)},
			ScopeSpans: []*tracev1.ScopeSpans{{
				Scope: &commonv1.InstrumentationScope{Name: scopeName},
				Spans: []*tracev1.Span{span},
			}},
		}},
	})
	return err
}

// The message is the one attribution a producer asserts. The thread it belongs
// to is resolved from it, and the identity the connection carries settles the
// rest.
func (e *Exporter) resourceAttributes(message Message) []*commonv1.KeyValue {
	attrs := []*commonv1.KeyValue{stringAttr(messageIDAttributeKey, message.ID)}
	if e.workloadID != "" {
		attrs = append(attrs, stringAttr(workloadIDAttributeKey, e.workloadID))
	}
	return attrs
}

func (e *Exporter) Close() error {
	return e.conn.Close()
}

// TraceID derives the trace from the workload, which is one wake cycle. Every
// producer in the container derives it the same way, so they share a trace
// without passing an identifier between them -- and without drifting apart if
// one of them restarts inside the pod.
func TraceID(workloadID string) []byte {
	sum := sha256.Sum256([]byte("agyn.trace." + workloadID))
	return sum[:16]
}

// SpanID derives a span from what it describes rather than drawing one, so
// re-exporting the same thing lands on the row already written instead of
// beside it.
func SpanID(subject, part string) []byte {
	sum := sha256.Sum256([]byte("agyn.span." + subject + "." + part))
	return sum[:8]
}

func stringAttr(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{
		Key:   key,
		Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}},
	}
}

const (
	spanLLMCall       = "llm.call"
	spanToolExecution = "tool.execution"
)

// Turns exports what the agent CLI did. A call hangs off the message that
// opened the turn and an execution off the call that invoked it, so the trace
// reads in the order things happened rather than by sorting timestamps drawn
// from different clocks.
func (e *Exporter) Turns(ctx context.Context, turns []transcript.Turn) error {
	var spans []*tracev1.Span
	traceID := TraceID(e.workloadID)
	for _, turn := range turns {
		spans = append(spans, e.turnSpans(traceID, turn)...)
	}
	if len(spans) == 0 {
		return nil
	}
	_, err := e.client.Export(ctx, &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{{
			Resource: &resourcev1.Resource{Attributes: e.turnResourceAttributes()},
			ScopeSpans: []*tracev1.ScopeSpans{{
				Scope: &commonv1.InstrumentationScope{Name: scopeName},
				Spans: spans,
			}},
		}},
	})
	return err
}

func (e *Exporter) turnSpans(traceID []byte, turn transcript.Turn) []*tracev1.Span {
	// A call roots itself in the trace rather than hanging off the message.
	// The transcript names a turn in the agent CLI's own terms, which is not
	// the platform's message id, so there is no message span to point at --
	// the trace both producers derive from the workload is what joins them.
	var parent []byte
	if turn.ParentTurnID != "" {
		// A subagent's work is an exception: the turn that delegated it is in
		// the same transcript, so it can be pointed at.
		parent = SpanID(turn.ParentTurnID, "step.0")
	}

	var spans []*tracev1.Span
	for i, step := range turn.Steps {
		call := &tracev1.Span{
			TraceId:           traceID,
			SpanId:            SpanID(turn.ID, fmt.Sprintf("step.%d", i)),
			ParentSpanId:      parent,
			Name:              spanLLMCall,
			Kind:              tracev1.Span_SPAN_KIND_CLIENT,
			StartTimeUnixNano: instant(step.StartedAt, turn.StartedAt),
			EndTimeUnixNano:   instant(step.EndedAt, time.Time{}),
			Attributes: []*commonv1.KeyValue{
				stringAttr("gen_ai.request.model", firstNonEmpty(step.Model, turn.Model)),
			},
			Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
		}
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
	return spans
}

func toolSpan(traceID, parentID []byte, turnID string, step, index int, tool transcript.ToolCall) *tracev1.Span {
	span := &tracev1.Span{
		TraceId:           traceID,
		SpanId:            SpanID(turnID, fmt.Sprintf("step.%d.tool.%d", step, index)),
		ParentSpanId:      parentID,
		Name:              spanToolExecution,
		Kind:              tracev1.Span_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: instant(tool.StartedAt, time.Time{}),
		EndTimeUnixNano:   instant(tool.EndedAt, time.Time{}),
		Attributes: []*commonv1.KeyValue{
			stringAttr("agyn.tool.name", tool.Name),
		},
		Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
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

// The workload is asserted; the message is not, because a turn read from a
// transcript names the one the agent CLI was handed rather than one this
// process was told about.
func (e *Exporter) turnResourceAttributes() []*commonv1.KeyValue {
	if e.workloadID == "" {
		return nil
	}
	return []*commonv1.KeyValue{stringAttr(workloadIDAttributeKey, e.workloadID)}
}

// A span whose time is unknown is left in progress rather than stamped with
// now: the next export completes it through the upsert.
func instant(at, fallback time.Time) uint64 {
	if !at.IsZero() {
		return uint64(at.UnixNano())
	}
	if !fallback.IsZero() {
		return uint64(fallback.UnixNano())
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func usageAttrs(usage *transcript.Usage) []*commonv1.KeyValue {
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
	}
	attrs := make([]*commonv1.KeyValue, 0, len(counts))
	for _, count := range counts {
		if count.value != nil {
			attrs = append(attrs, intAttr(count.key, *count.value))
		}
	}
	return attrs
}

func intAttr(key string, value int64) *commonv1.KeyValue {
	return &commonv1.KeyValue{
		Key:   key,
		Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: value}},
	}
}

func jsonAttr(key string, value any) *commonv1.KeyValue {
	encoded, err := json.Marshal(value)
	if err != nil {
		return stringAttr(key, fmt.Sprintf("%v", value))
	}
	return stringAttr(key, string(encoded))
}
