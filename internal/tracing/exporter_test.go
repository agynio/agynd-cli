package tracing

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/agynio/agynd-cli/internal/transcript"

	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
)

type recordingService struct {
	collectortracev1.UnimplementedTraceServiceServer
	requests []*collectortracev1.ExportTraceServiceRequest
}

func (s *recordingService) Export(_ context.Context, req *collectortracev1.ExportTraceServiceRequest) (*collectortracev1.ExportTraceServiceResponse, error) {
	s.requests = append(s.requests, req)
	return &collectortracev1.ExportTraceServiceResponse{}, nil
}

func startExporter(t *testing.T, workloadID string) (*Exporter, *recordingService) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	service := &recordingService{}
	server := grpc.NewServer()
	collectortracev1.RegisterTraceServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	exporter, err := NewExporter(Config{Address: listener.Addr().String(), WorkloadID: workloadID})
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}
	t.Cleanup(func() { _ = exporter.Close() })
	return exporter, service
}

func onlySpan(t *testing.T, service *recordingService) (*tracev1.Span, []*commonv1.KeyValue) {
	t.Helper()
	if len(service.requests) != 1 {
		t.Fatalf("expected 1 export, got %d", len(service.requests))
	}
	resourceSpans := service.requests[0].ResourceSpans
	if len(resourceSpans) != 1 || len(resourceSpans[0].ScopeSpans) != 1 || len(resourceSpans[0].ScopeSpans[0].Spans) != 1 {
		t.Fatal("expected one span in one scope in one resource")
	}
	return resourceSpans[0].ScopeSpans[0].Spans[0], resourceSpans[0].Resource.GetAttributes()
}

func attr(attrs []*commonv1.KeyValue, key string) string {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value.GetStringValue()
		}
	}
	return ""
}

func sampleMessage() Message {
	return Message{
		ID:        "message-1",
		ThreadID:  "thread-1",
		SenderID:  "sender-1",
		Body:      "hello",
		CreatedAt: time.Date(2026, 8, 9, 5, 0, 0, 0, time.UTC),
	}
}

func TestInvocationMessageCarriesTheMessageAndItsAttribution(t *testing.T) {
	exporter, service := startExporter(t, "workload-1")
	if err := exporter.InvocationMessage(context.Background(), sampleMessage()); err != nil {
		t.Fatalf("invocation message: %v", err)
	}

	span, resource := onlySpan(t, service)
	if span.Name != spanInvocationMessage {
		t.Fatalf("expected %s, got %s", spanInvocationMessage, span.Name)
	}
	if attr(span.Attributes, "agyn.message.text") != "hello" {
		t.Fatal("expected the message body on the span")
	}
	// The message and its thread are both asserted: Tracing authorizes the one
	// against the other before storing it, and rejects a message named without
	// a thread.
	if got := attr(resource, messageIDAttributeKey); got != "message-1" {
		t.Fatalf("expected the message id as a resource attribute, got %q", got)
	}
	if got := attr(resource, workloadIDAttributeKey); got != "workload-1" {
		t.Fatalf("expected the workload id as a resource attribute, got %q", got)
	}
	if got := attr(resource, threadIDAttributeKey); got != "thread-1" {
		t.Fatalf("expected the thread id alongside the message, got %q", got)
	}
}

// A turn read from a transcript names the message the agent CLI was handed, not
// one this process was told about, so the hook's spans assert neither.
func TestTurnsAssertNoMessageOrThread(t *testing.T) {
	exporter, service := startExporter(t, "workload-1")

	if err := exporter.Turns(context.Background(), []transcript.Turn{sampleTurn()}); err != nil {
		t.Fatalf("expected the turn to export, got %v", err)
	}

	resource := service.requests[0].ResourceSpans[0].Resource.GetAttributes()
	if attr(resource, messageIDAttributeKey) != "" || attr(resource, threadIDAttributeKey) != "" {
		t.Fatal("expected the hook to assert only its workload")
	}
	if got := attr(resource, workloadIDAttributeKey); got != "workload-1" {
		t.Fatalf("expected the workload id, got %q", got)
	}
}

// Both producers derive the trace from the workload, which is how a turn's
// spans and the message that opened it meet without anything being passed.
func TestTraceIDFollowsTheWakeCycle(t *testing.T) {
	first := TraceID("workload-1")
	if len(first) != 16 {
		t.Fatalf("expected a 16-byte trace id, got %d", len(first))
	}
	if string(first) != string(TraceID("workload-1")) {
		t.Fatal("expected the same workload to derive the same trace")
	}
	if string(first) == string(TraceID("workload-2")) {
		t.Fatal("expected a different wake cycle to be a different trace")
	}
}

// A message re-fed after a restart lands on the span already written.
func TestInvocationMessageIsStableAcrossRestarts(t *testing.T) {
	exporterA, serviceA := startExporter(t, "workload-1")
	if err := exporterA.InvocationMessage(context.Background(), sampleMessage()); err != nil {
		t.Fatalf("invocation message: %v", err)
	}
	exporterB, serviceB := startExporter(t, "workload-1")
	if err := exporterB.InvocationMessage(context.Background(), sampleMessage()); err != nil {
		t.Fatalf("invocation message: %v", err)
	}

	first, _ := onlySpan(t, serviceA)
	second, _ := onlySpan(t, serviceB)
	if string(first.SpanId) != string(second.SpanId) {
		t.Fatal("expected the same message to keep its span id")
	}
	if string(first.TraceId) != string(second.TraceId) {
		t.Fatal("expected the same workload to keep its trace")
	}
}

func TestInvocationMessageRejectsAMessageItCannotIdentify(t *testing.T) {
	exporter, _ := startExporter(t, "workload-1")
	if err := exporter.InvocationMessage(context.Background(), Message{Body: "hello"}); err == nil {
		t.Fatal("expected a message with no id to be rejected")
	}
}

// A hook is handed a trace rather than deriving one, so what it exports has to
// land in the trace it was given and not in the one its workload would name.
func TestExporterWritesIntoTheTraceItWasGiven(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	service := &recordingService{}
	server := grpc.NewServer()
	collectortracev1.RegisterTraceServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	given := TraceID("opened-elsewhere")
	exporter, err := NewExporter(Config{
		Address:    listener.Addr().String(),
		TraceID:    given,
		WorkloadID: "workload-1",
	})
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}
	t.Cleanup(func() { _ = exporter.Close() })

	if err := exporter.InvocationMessage(context.Background(), sampleMessage()); err != nil {
		t.Fatalf("expected the message to export, got %v", err)
	}

	span, _ := onlySpan(t, service)
	if !bytes.Equal(span.TraceId, given) {
		t.Fatalf("expected the given trace %x, got %x", given, span.TraceId)
	}
	if bytes.Equal(span.TraceId, TraceID("workload-1")) {
		t.Fatal("expected the given trace rather than the workload's own")
	}
}
