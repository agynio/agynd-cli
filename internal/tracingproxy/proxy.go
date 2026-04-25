package tracingproxy

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	ListenAddress          = "localhost:4317"
	threadIDAttributeKey   = "agyn.thread.id"
	messageIDAttributeKey  = "agyn.thread.message.id"
	workloadIDAttributeKey = "agyn.workload.id"
)

type Config struct {
	TracingAddress string
	ThreadID       string
	WorkloadID     string
}

type Proxy struct {
	collectortracev1.UnimplementedTraceServiceServer

	server     *grpc.Server
	upstream   collectortracev1.TraceServiceClient
	conn       *grpc.ClientConn
	threadID   string
	workloadID string
	messageMu  sync.RWMutex
	messageID  string
}

func Start(ctx context.Context, cfg Config) (*Proxy, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(cfg.TracingAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial tracing address: %w", err)
	}
	server := grpc.NewServer()
	proxy := &Proxy{
		server:     server,
		upstream:   collectortracev1.NewTraceServiceClient(conn),
		conn:       conn,
		threadID:   cfg.ThreadID,
		workloadID: cfg.WorkloadID,
	}
	collectortracev1.RegisterTraceServiceServer(server, proxy)

	listener, err := net.Listen("tcp", ListenAddress)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("listen on %s: %w", ListenAddress, err)
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("tracing proxy stopped: %v", err)
		}
	}()

	return proxy, nil
}

func (p *Proxy) Export(ctx context.Context, req *collectortracev1.ExportTraceServiceRequest) (*collectortracev1.ExportTraceServiceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "trace export request missing")
	}
	if p.threadID != "" {
		injectThreadID(req, p.threadID)
	}
	if p.workloadID != "" {
		injectWorkloadID(req, p.workloadID)
	}
	if messageID := p.messageIDValue(); messageID != "" {
		injectMessageID(req, messageID)
	}
	return p.upstream.Export(ctx, req)
}

func (p *Proxy) SetMessageID(messageID string) {
	p.messageMu.Lock()
	p.messageID = messageID
	p.messageMu.Unlock()
}

func (p *Proxy) ClearMessageID() {
	p.messageMu.Lock()
	p.messageID = ""
	p.messageMu.Unlock()
}

func (p *Proxy) messageIDValue() string {
	p.messageMu.RLock()
	defer p.messageMu.RUnlock()
	return p.messageID
}

func (p *Proxy) Close() {
	done := make(chan struct{})
	go func() {
		p.server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		p.server.Stop()
	}
	_ = p.conn.Close()
}

func injectThreadID(req *collectortracev1.ExportTraceServiceRequest, threadID string) {
	for _, spans := range req.ResourceSpans {
		if spans == nil {
			continue
		}
		resource := spans.Resource
		if resource == nil {
			resource = &resourcev1.Resource{}
			spans.Resource = resource
		}
		upsertAttribute(resource, threadIDAttributeKey, threadID)
	}
}

func injectWorkloadID(req *collectortracev1.ExportTraceServiceRequest, workloadID string) {
	for _, spans := range req.ResourceSpans {
		if spans == nil {
			continue
		}
		resource := spans.Resource
		if resource == nil {
			resource = &resourcev1.Resource{}
			spans.Resource = resource
		}
		upsertAttribute(resource, workloadIDAttributeKey, workloadID)
	}
}

func injectMessageID(req *collectortracev1.ExportTraceServiceRequest, messageID string) {
	for _, spans := range req.ResourceSpans {
		if spans == nil {
			continue
		}
		resource := spans.Resource
		if resource == nil {
			resource = &resourcev1.Resource{}
			spans.Resource = resource
		}
		upsertAttribute(resource, messageIDAttributeKey, messageID)
	}
}

func upsertAttribute(resource *resourcev1.Resource, key, value string) {
	attributeValue := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_StringValue{StringValue: value},
	}
	for _, attribute := range resource.Attributes {
		if attribute != nil && attribute.Key == key {
			attribute.Value = attributeValue
			return
		}
	}
	resource.Attributes = append(resource.Attributes, &commonv1.KeyValue{
		Key:   key,
		Value: attributeValue,
	})
}
