package tracingproxy

import (
	"testing"

	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
)

func logRecord(attrs ...*commonv1.KeyValue) *logsv1.LogRecord {
	return &logsv1.LogRecord{Attributes: attrs}
}

func logExport(records ...*logsv1.LogRecord) *collectorlogsv1.ExportLogsServiceRequest {
	return &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{
			ScopeLogs: []*logsv1.ScopeLogs{{LogRecords: records}},
		}},
	}
}

func spanAttr(t *testing.T, span *tracev1.Span, key string) *commonv1.AnyValue {
	t.Helper()
	for _, attr := range span.Attributes {
		if attr.Key == key {
			return attr.Value
		}
	}
	return nil
}

func requireStringAttr(t *testing.T, span *tracev1.Span, key, want string) {
	t.Helper()
	value := spanAttr(t, span, key)
	if value == nil {
		t.Fatalf("expected attribute %q on %s", key, span.Name)
	}
	if got := value.GetStringValue(); got != want {
		t.Fatalf("expected %s=%q, got %q", key, want, got)
	}
}

func requireIntAttr(t *testing.T, span *tracev1.Span, key string, want int64) {
	t.Helper()
	value := spanAttr(t, span, key)
	if value == nil {
		t.Fatalf("expected attribute %q on %s", key, span.Name)
	}
	if got := value.GetIntValue(); got != want {
		t.Fatalf("expected %s=%d, got %d", key, want, got)
	}
}

func TestSemanticSpansTranslatesUserPrompt(t *testing.T) {
	proxy := &Proxy{}
	spans := proxy.semanticSpans(logExport(logRecord(
		stringAttr("event.name", eventUserPrompt),
		stringAttr("prompt", "hello"),
		stringAttr("prompt_length", "67"),
		stringAttr("conversation.id", "conv-1"),
		stringAttr("event.timestamp", "2026-08-09T00:48:22.310Z"),
	)))

	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name != spanInvocationMessage {
		t.Fatalf("expected %s, got %s", spanInvocationMessage, spans[0].Name)
	}
	requireStringAttr(t, spans[0], "agyn.message.text", "hello")
	requireStringAttr(t, spans[0], "agyn.message.role", "user")
	// prompt_length arrives as a decimal string, not an int.
	requireIntAttr(t, spans[0], "agyn.message.length", 67)
	if spans[0].StartTimeUnixNano == 0 {
		t.Fatal("expected the event timestamp to be parsed")
	}
}

func TestSemanticSpansTranslatesAPIRequest(t *testing.T) {
	proxy := &Proxy{}
	spans := proxy.semanticSpans(logExport(logRecord(
		stringAttr("event.name", eventAPIRequest),
		stringAttr("provider_name", "Agyn LLM"),
		stringAttr("model", "gpt-5.5"),
		stringAttr("endpoint", "/responses"),
		intAttr("http.response.status_code", 200),
		stringAttr("duration_ms", "1551"),
		intAttr("attempt", 0),
		stringAttr("conversation.id", "conv-1"),
		stringAttr("event.timestamp", "2026-08-09T00:48:23.889Z"),
	)))

	if len(spans) != 1 || spans[0].Name != spanLLMCall {
		t.Fatalf("expected one %s span, got %#v", spanLLMCall, spans)
	}
	requireStringAttr(t, spans[0], "gen_ai.request.model", "gpt-5.5")
	requireStringAttr(t, spans[0], "gen_ai.system", "Agyn LLM")
	requireIntAttr(t, spans[0], "http.response.status_code", 200)
	// duration_ms is a string; the span still has to span the call.
	if spans[0].EndTimeUnixNano-spans[0].StartTimeUnixNano != 1551*1_000_000 {
		t.Fatalf("expected a 1551ms span, got %d ns", spans[0].EndTimeUnixNano-spans[0].StartTimeUnixNano)
	}
	if spans[0].Status.GetCode() != tracev1.Status_STATUS_CODE_OK {
		t.Fatalf("expected a 200 to be OK, got %v", spans[0].Status.GetCode())
	}
}

// The counts arrive on a later event than the call, so the call is re-emitted
// with the same ids and ingest upserts it.
func TestSemanticSpansAttachesUsageToTheCall(t *testing.T) {
	proxy := &Proxy{}
	call := proxy.semanticSpans(logExport(logRecord(
		stringAttr("event.name", eventAPIRequest),
		stringAttr("model", "gpt-5.5"),
		stringAttr("conversation.id", "conv-1"),
		stringAttr("event.timestamp", "2026-08-09T00:48:23.889Z"),
	)))
	if len(call) != 1 {
		t.Fatalf("expected the call span, got %d", len(call))
	}

	// Codex completes the response twice; only the second carries counts, and
	// the first must not spend the pending call.
	if empty := proxy.semanticSpans(logExport(logRecord(
		stringAttr("event.name", eventSSE),
		stringAttr("event.kind", sseResponseCompleted),
		stringAttr("conversation.id", "conv-1"),
	))); len(empty) != 0 {
		t.Fatalf("expected a completion with no counts to emit nothing, got %d", len(empty))
	}

	usage := proxy.semanticSpans(logExport(logRecord(
		stringAttr("event.name", eventSSE),
		stringAttr("event.kind", sseResponseCompleted),
		stringAttr("input_token_count", "8607"),
		stringAttr("output_token_count", "11"),
		intAttr("cached_token_count", 0),
		intAttr("reasoning_token_count", 0),
		stringAttr("conversation.id", "conv-1"),
	)))
	if len(usage) != 1 {
		t.Fatalf("expected the call re-emitted with usage, got %d", len(usage))
	}
	if string(usage[0].SpanId) != string(call[0].SpanId) {
		t.Fatal("expected usage to land on the same span the call was emitted as")
	}
	requireIntAttr(t, usage[0], "gen_ai.usage.input_tokens", 8607)
	requireIntAttr(t, usage[0], "gen_ai.usage.output_tokens", 11)
	requireIntAttr(t, usage[0], "gen_ai.usage.cache_read.input_tokens", 0)
}

func TestSemanticSpansDropsSSEDeltasAndUnpairedUsage(t *testing.T) {
	proxy := &Proxy{}
	spans := proxy.semanticSpans(logExport(
		logRecord(
			stringAttr("event.name", eventSSE),
			stringAttr("event.kind", "response.output_text.delta"),
			stringAttr("conversation.id", "conv-1"),
		),
		// Usage with no call before it describes nothing on its own.
		logRecord(
			stringAttr("event.name", eventSSE),
			stringAttr("event.kind", sseResponseCompleted),
			stringAttr("input_token_count", "10"),
			stringAttr("conversation.id", "conv-unknown"),
		),
	))
	if len(spans) != 0 {
		t.Fatalf("expected no spans, got %#v", spans)
	}
}

// A turn is one trace, whatever codex does with its own trace ids.
func TestSemanticSpansGroupSpansByMessage(t *testing.T) {
	proxy := &Proxy{}
	proxy.SetMessageID("message-1")
	spans := proxy.semanticSpans(logExport(
		logRecord(
			stringAttr("event.name", eventUserPrompt),
			stringAttr("prompt", "hello"),
			stringAttr("conversation.id", "conv-1"),
		),
		logRecord(
			stringAttr("event.name", eventAPIRequest),
			stringAttr("model", "gpt-5.5"),
			stringAttr("conversation.id", "conv-2"),
		),
	))
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	if string(spans[0].TraceId) != string(spans[1].TraceId) {
		t.Fatal("expected both spans in the message's trace, despite different conversations")
	}
	if len(spans[0].TraceId) != 16 || len(spans[0].SpanId) != 8 {
		t.Fatalf("expected OTLP id widths, got trace=%d span=%d", len(spans[0].TraceId), len(spans[0].SpanId))
	}
}

// The call answers the message, so it hangs off it rather than relying on a
// start derived by subtracting codex's reported duration.
func TestSemanticSpansParentTheCallToItsMessage(t *testing.T) {
	proxy := &Proxy{}
	message := proxy.semanticSpans(logExport(logRecord(
		stringAttr("event.name", eventUserPrompt),
		stringAttr("prompt", "hello"),
		stringAttr("conversation.id", "conv-1"),
		stringAttr("event.timestamp", "2026-08-09T00:48:22.310Z"),
	)))
	if len(message) != 1 {
		t.Fatalf("expected the message span, got %d", len(message))
	}

	// A duration long enough to place the call before the message it answers.
	call := proxy.semanticSpans(logExport(logRecord(
		stringAttr("event.name", eventAPIRequest),
		stringAttr("model", "gpt-5.5"),
		stringAttr("duration_ms", "5000"),
		stringAttr("conversation.id", "conv-1"),
		stringAttr("event.timestamp", "2026-08-09T00:48:23.889Z"),
	)))
	if len(call) != 1 {
		t.Fatalf("expected the call span, got %d", len(call))
	}
	if string(call[0].ParentSpanId) != string(message[0].SpanId) {
		t.Fatal("expected the call to hang off the message it answers")
	}
	if call[0].StartTimeUnixNano < message[0].StartTimeUnixNano {
		t.Fatal("expected the call not to open before the message it answers")
	}
}
