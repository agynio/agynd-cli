package tracingproxy

import (
	"crypto/rand"
	"crypto/sha256"
	"strconv"
	"strings"
	"time"

	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

// Codex ships its structured events on the OTLP log signal, which nothing here
// stores: the tracing service speaks traces alone. These are the events worth
// keeping, translated into the span vocabulary the run view already renders.
const (
	eventUserPrompt = "codex.user_prompt"
	eventAPIRequest = "codex.api_request"
	eventSSE        = "codex.sse_event"

	spanInvocationMessage = "invocation.message"
	spanLLMCall           = "llm.call"

	// Names the spans this proxy synthesised, as distinct from the ones codex
	// exported itself.
	semanticScopeName = "agynd.codex.semantic"

	sseResponseCompleted = "response.completed"
)

type logAttrs map[string]*commonv1.AnyValue

func attrsOf(record *logsv1.LogRecord) logAttrs {
	attrs := make(logAttrs, len(record.Attributes))
	for _, attr := range record.Attributes {
		attrs[attr.Key] = attr.Value
	}
	return attrs
}

func (a logAttrs) str(key string) string {
	value, ok := a[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.Value.(type) {
	case *commonv1.AnyValue_StringValue:
		return v.StringValue
	case *commonv1.AnyValue_IntValue:
		return strconv.FormatInt(v.IntValue, 10)
	case *commonv1.AnyValue_BoolValue:
		return strconv.FormatBool(v.BoolValue)
	}
	return ""
}

// Codex is inconsistent about whether a count is an int or a decimal string --
// input_token_count arrives as a string while cached_token_count is an int --
// so both spellings are read.
func (a logAttrs) int(key string) (int64, bool) {
	value, ok := a[key]
	if !ok || value == nil {
		return 0, false
	}
	switch v := value.Value.(type) {
	case *commonv1.AnyValue_IntValue:
		return v.IntValue, true
	case *commonv1.AnyValue_StringValue:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v.StringValue), 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

func (a logAttrs) eventTime(fallback uint64) uint64 {
	raw := a.str("event.timestamp")
	if raw == "" {
		return fallback
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return fallback
	}
	return uint64(parsed.UnixNano())
}

func stringAttr(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{
		Key:   key,
		Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}},
	}
}

func intAttr(key string, value int64) *commonv1.KeyValue {
	return &commonv1.KeyValue{
		Key:   key,
		Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: value}},
	}
}

// traceIDForMessage keeps every span of one message in one trace. Codex opens a
// fresh trace per JSON-RPC call, so its own ids scatter a single turn across
// dozens; the message is the unit the run view shows.
func traceIDForMessage(seed string) []byte {
	sum := sha256.Sum256([]byte("agyn.trace." + seed))
	return sum[:16]
}

func newSpanID() []byte {
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		// A collision only merges two spans in the upsert; refusing to emit
		// loses the event outright, so a time-derived id is the better failure.
		now := uint64(time.Now().UnixNano())
		for i := range id {
			id[i] = byte(now >> (8 * i))
		}
	}
	return id
}

// pendingLLMCall is held so the usage counts, which arrive on a later event than
// the request itself, can be written onto the same span. Ingest upserts on
// (trace_id, span_id) and replaces the attributes wholesale, so the span is
// re-emitted complete rather than patched.
type pendingLLMCall struct {
	span *tracev1.Span
}

// semanticSpans translates one OTLP log export into the spans the run view
// renders. Records it has no mapping for are dropped rather than passed
// through: the raw event is already in the container log, and a span per SSE
// delta is the noise this is meant to remove.
func (p *Proxy) semanticSpans(request *collectorlogsv1.ExportLogsServiceRequest) []*tracev1.Span {
	var spans []*tracev1.Span
	for _, resourceLogs := range request.GetResourceLogs() {
		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			for _, record := range scopeLogs.GetLogRecords() {
				attrs := attrsOf(record)
				span := p.spanForRecord(record, attrs)
				if span != nil {
					spans = append(spans, span)
					continue
				}
				if name := attrs.str("event.name"); name != "" && name != eventSSE {
					logUntranslatedRecord(record, name)
				}
			}
		}
	}
	return spans
}

func (p *Proxy) spanForRecord(record *logsv1.LogRecord, attrs logAttrs) *tracev1.Span {
	switch attrs.str("event.name") {
	case eventUserPrompt:
		return p.messageSpan(record, attrs)
	case eventAPIRequest:
		return p.llmCallSpan(record, attrs)
	case eventSSE:
		if attrs.str("event.kind") != sseResponseCompleted {
			return nil
		}
		return p.llmCallUsageSpan(attrs)
	}
	return nil
}

// traceSeed prefers the message the daemon is processing, so a turn's spans
// group the way the run view reads them, and falls back to codex's own
// conversation when no message is set -- a sandbox has no message at all.
func (p *Proxy) traceSeed(attrs logAttrs) string {
	if messageID := p.messageIDValue(); messageID != "" {
		return messageID
	}
	return attrs.str("conversation.id")
}

func (p *Proxy) messageSpan(record *logsv1.LogRecord, attrs logAttrs) *tracev1.Span {
	at := attrs.eventTime(record.GetTimeUnixNano())
	span := &tracev1.Span{
		TraceId:           traceIDForMessage(p.traceSeed(attrs)),
		SpanId:            newSpanID(),
		Name:              spanInvocationMessage,
		Kind:              tracev1.Span_SPAN_KIND_INTERNAL,
		StartTimeUnixNano: at,
		EndTimeUnixNano:   at,
		Attributes: []*commonv1.KeyValue{
			stringAttr("agyn.message.role", "user"),
			stringAttr("agyn.message.text", attrs.str("prompt")),
		},
		Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
	}
	if length, ok := attrs.int("prompt_length"); ok {
		span.Attributes = append(span.Attributes, intAttr("agyn.message.length", length))
	}
	return span
}

func (p *Proxy) llmCallSpan(record *logsv1.LogRecord, attrs logAttrs) *tracev1.Span {
	end := attrs.eventTime(record.GetTimeUnixNano())
	start := end
	if duration, ok := attrs.int("duration_ms"); ok && duration > 0 {
		start = end - uint64(duration)*uint64(time.Millisecond)
	}
	span := &tracev1.Span{
		TraceId:           traceIDForMessage(p.traceSeed(attrs)),
		SpanId:            newSpanID(),
		Name:              spanLLMCall,
		Kind:              tracev1.Span_SPAN_KIND_CLIENT,
		StartTimeUnixNano: start,
		EndTimeUnixNano:   end,
		Attributes: []*commonv1.KeyValue{
			stringAttr("gen_ai.system", attrs.str("provider_name")),
			stringAttr("gen_ai.request.model", attrs.str("model")),
			stringAttr("agyn.llm.endpoint", attrs.str("endpoint")),
		},
		Status: &tracev1.Status{Code: tracev1.Status_STATUS_CODE_OK},
	}
	if status, ok := attrs.int("http.response.status_code"); ok {
		span.Attributes = append(span.Attributes, intAttr("http.response.status_code", status))
		if status >= 400 {
			span.Status = &tracev1.Status{Code: tracev1.Status_STATUS_CODE_ERROR}
		}
	}
	if attempt, ok := attrs.int("attempt"); ok {
		span.Attributes = append(span.Attributes, intAttr("agyn.llm.attempt", attempt))
	}
	if message := attrs.str("error.message"); message != "" {
		span.Status = &tracev1.Status{Code: tracev1.Status_STATUS_CODE_ERROR, Message: message}
	}
	p.rememberLLMCall(attrs.str("conversation.id"), span)
	return span
}

// llmCallUsageSpan re-emits the call this usage belongs to. Without the
// remembered span there is nothing to attach to -- usage alone is not a call --
// so the record is dropped.
func (p *Proxy) llmCallUsageSpan(attrs logAttrs) *tracev1.Span {
	pending := p.takeLLMCall(attrs.str("conversation.id"))
	if pending == nil {
		return nil
	}
	usage := []struct {
		source string
		key    string
	}{
		{"input_token_count", "gen_ai.usage.input_tokens"},
		{"output_token_count", "gen_ai.usage.output_tokens"},
		{"cached_token_count", "gen_ai.usage.cache_read.input_tokens"},
		{"reasoning_token_count", "agyn.usage.reasoning_tokens"},
	}
	for _, entry := range usage {
		if value, ok := attrs.int(entry.source); ok {
			pending.span.Attributes = append(pending.span.Attributes, intAttr(entry.key, value))
		}
	}
	return pending.span
}

func (p *Proxy) rememberLLMCall(conversationID string, span *tracev1.Span) {
	if conversationID == "" {
		return
	}
	p.llmCallMu.Lock()
	defer p.llmCallMu.Unlock()
	if p.llmCalls == nil {
		p.llmCalls = make(map[string]*pendingLLMCall, 1)
	}
	p.llmCalls[conversationID] = &pendingLLMCall{span: span}
}

func (p *Proxy) takeLLMCall(conversationID string) *pendingLLMCall {
	if conversationID == "" {
		return nil
	}
	p.llmCallMu.Lock()
	defer p.llmCallMu.Unlock()
	pending, ok := p.llmCalls[conversationID]
	if !ok {
		return nil
	}
	delete(p.llmCalls, conversationID)
	return pending
}
