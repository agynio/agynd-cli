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
	"fmt"
	"time"

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
