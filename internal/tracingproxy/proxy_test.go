package tracingproxy

import (
	"context"
	"net"
	"testing"
	"time"

	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type captureTraceServer struct {
	collectortracev1.UnimplementedTraceServiceServer
	requests chan *collectortracev1.ExportTraceServiceRequest
}

func (s *captureTraceServer) Export(ctx context.Context, req *collectortracev1.ExportTraceServiceRequest) (*collectortracev1.ExportTraceServiceResponse, error) {
	s.requests <- req
	return &collectortracev1.ExportTraceServiceResponse{}, nil
}

func TestInjectThreadID(t *testing.T) {
	req := &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{Key: "existing", Value: stringValue("keep")},
					},
				},
			},
		},
	}

	injectThreadID(req, "thread-1")

	resource := req.ResourceSpans[0].Resource
	if value, ok := findAttribute(resource, "existing"); !ok || value.GetStringValue() != "keep" {
		t.Fatalf("expected existing attribute preserved, got %v", value)
	}
	if value, ok := findAttribute(resource, threadIDAttributeKey); !ok || value.GetStringValue() != "thread-1" {
		t.Fatalf("expected thread id injected, got %v", value)
	}
	if len(resource.Attributes) != 2 {
		t.Fatalf("expected 2 attributes, got %d", len(resource.Attributes))
	}
}

func TestInjectThreadIDOverwritesExisting(t *testing.T) {
	req := &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{
						{Key: threadIDAttributeKey, Value: stringValue("old")},
					},
				},
			},
		},
	}

	injectThreadID(req, "new")

	resource := req.ResourceSpans[0].Resource
	if len(resource.Attributes) != 1 {
		t.Fatalf("expected 1 attribute, got %d", len(resource.Attributes))
	}
	value, ok := findAttribute(resource, threadIDAttributeKey)
	if !ok || value.GetStringValue() != "new" {
		t.Fatalf("expected updated thread id, got %v", value)
	}
}

func TestInjectThreadIDNilResource(t *testing.T) {
	req := &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{{}},
	}

	injectThreadID(req, "thread-2")

	resource := req.ResourceSpans[0].Resource
	if resource == nil {
		t.Fatal("expected resource to be created")
	}
	value, ok := findAttribute(resource, threadIDAttributeKey)
	if !ok || value.GetStringValue() != "thread-2" {
		t.Fatalf("expected injected thread id, got %v", value)
	}
}

func TestInjectThreadIDMultipleResourceSpans(t *testing.T) {
	req := &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{Resource: &resourcev1.Resource{}},
			{Resource: &resourcev1.Resource{}},
		},
	}

	injectThreadID(req, "thread-3")

	for i, span := range req.ResourceSpans {
		value, ok := findAttribute(span.Resource, threadIDAttributeKey)
		if !ok || value.GetStringValue() != "thread-3" {
			t.Fatalf("resource span %d missing thread id", i)
		}
	}
}

func TestProxyForwardsToUpstream(t *testing.T) {
	server, listener, requests := startCaptureServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	proxy, err := Start(ctx, Config{TracingAddress: listener.Addr().String(), ThreadID: "thread-4"})
	if err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	defer proxy.Close()

	conn, err := grpc.NewClient(listenAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	client := collectortracev1.NewTraceServiceClient(conn)
	resp, err := client.Export(ctx, &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{Resource: &resourcev1.Resource{}},
		},
	})
	if err != nil {
		t.Fatalf("export via proxy: %v", err)
	}
	if resp == nil {
		t.Fatal("expected export response")
	}

	forwarded := awaitRequest(t, requests)
	value, ok := findAttribute(forwarded.ResourceSpans[0].Resource, threadIDAttributeKey)
	if !ok || value.GetStringValue() != "thread-4" {
		t.Fatalf("expected forwarded request to include thread id, got %v", value)
	}
}

func TestProxyNoThreadIDPassthrough(t *testing.T) {
	server, listener, requests := startCaptureServer(t)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	proxy, err := Start(ctx, Config{TracingAddress: listener.Addr().String(), ThreadID: ""})
	if err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	defer proxy.Close()

	conn, err := grpc.NewClient(listenAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	client := collectortracev1.NewTraceServiceClient(conn)
	_, err = client.Export(ctx, &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{
			{Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{{Key: "existing", Value: stringValue("keep")}}}},
		},
	})
	if err != nil {
		t.Fatalf("export via proxy: %v", err)
	}

	forwarded := awaitRequest(t, requests)
	if _, ok := findAttribute(forwarded.ResourceSpans[0].Resource, threadIDAttributeKey); ok {
		t.Fatal("expected no thread id injected")
	}
	value, ok := findAttribute(forwarded.ResourceSpans[0].Resource, "existing")
	if !ok || value.GetStringValue() != "keep" {
		t.Fatalf("expected passthrough attribute, got %v", value)
	}
}

func startCaptureServer(t *testing.T) (*grpc.Server, net.Listener, chan *collectortracev1.ExportTraceServiceRequest) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	requests := make(chan *collectortracev1.ExportTraceServiceRequest, 1)
	collectortracev1.RegisterTraceServiceServer(server, &captureTraceServer{requests: requests})
	go func() {
		_ = server.Serve(listener)
	}()
	return server, listener, requests
}

func awaitRequest(t *testing.T, requests <-chan *collectortracev1.ExportTraceServiceRequest) *collectortracev1.ExportTraceServiceRequest {
	t.Helper()
	select {
	case req := <-requests:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for forwarded request")
		return nil
	}
}

func stringValue(value string) *commonv1.AnyValue {
	return &commonv1.AnyValue{
		Value: &commonv1.AnyValue_StringValue{StringValue: value},
	}
}

func findAttribute(resource *resourcev1.Resource, key string) (*commonv1.AnyValue, bool) {
	if resource == nil {
		return nil, false
	}
	for _, attribute := range resource.Attributes {
		if attribute != nil && attribute.Key == key {
			return attribute.Value, true
		}
	}
	return nil, false
}
