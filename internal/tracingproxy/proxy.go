package tracingproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	DefaultListenAddress   = "127.0.0.1:4317"
	ListenAddress          = DefaultListenAddress
	threadIDAttributeKey   = "agyn.thread.id"
	messageIDAttributeKey  = "agyn.thread.message.id"
	workloadIDAttributeKey = "agyn.workload.id"
	maxHTTPExportBytes     = 32 << 20
	maxLoggedValueBytes    = 400
)

type Config struct {
	TracingAddress string
	ListenAddress  string
	ThreadID       string
	WorkloadID     string
}

type Proxy struct {
	collectortracev1.UnimplementedTraceServiceServer

	server      *grpc.Server
	httpServer  *http.Server
	upstream    collectortracev1.TraceServiceClient
	conn        *grpc.ClientConn
	address     string
	httpAddress string
	workloadID  string
	// An instance serves many threads, so the thread is per-message and set
	// from the consumer goroutine while Export runs on gRPC's.
	threadMu  sync.RWMutex
	threadID  string
	messageMu sync.RWMutex
	messageID string
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

	listenAddress := cfg.ListenAddress
	if listenAddress == "" {
		listenAddress = DefaultListenAddress
	}
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("listen on %s: %w", listenAddress, err)
	}
	proxy.address = listener.Addr().String()

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Printf("tracing proxy stopped: %v", err)
		}
	}()

	// The same exporter over OTLP/HTTP, on its own port. Codex cannot use the
	// gRPC one -- its otlp-grpc exporter fails to build at all, against any
	// address -- and the console and any other OTLP client still can, so this
	// serves both rather than replacing one with the other.
	httpListener, err := net.Listen("tcp", httpListenAddress(listenAddress))
	if err != nil {
		server.Stop()
		_ = conn.Close()
		return nil, fmt.Errorf("listen for OTLP/HTTP: %w", err)
	}
	proxy.httpAddress = httpListener.Addr().String()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/traces", proxy.serveHTTPTraces)
	mux.HandleFunc("POST /v1/logs", proxy.serveHTTPLogs)
	proxy.httpServer = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := proxy.httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("tracing proxy OTLP/HTTP stopped: %v", err)
		}
	}()

	return proxy, nil
}

// httpListenAddress keeps the OTLP/HTTP listener on the same host as the gRPC
// one and lets the kernel choose its port, so the two never collide.
func httpListenAddress(grpcAddress string) string {
	host, _, err := net.SplitHostPort(grpcAddress)
	if err != nil || host == "" {
		return "127.0.0.1:0"
	}
	return net.JoinHostPort(host, "0")
}

// serveHTTPTraces accepts the binary protobuf encoding, which is what the OTLP
// specification defaults to and the only one codex emits.
func (p *Proxy) serveHTTPTraces(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHTTPExportBytes))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	request := &collectortracev1.ExportTraceServiceRequest{}
	if err := proto.Unmarshal(body, request); err != nil {
		http.Error(w, "decode OTLP request", http.StatusBadRequest)
		return
	}
	response, err := p.Export(r.Context(), request)
	if err != nil {
		http.Error(w, "export traces", http.StatusBadGateway)
		return
	}
	encoded, err := proto.Marshal(response)
	if err != nil {
		http.Error(w, "encode OTLP response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(encoded)
}

// serveHTTPLogs records what codex ships over the log signal -- prompts, tool
// calls and SSE events -- which has no ingest path yet, so it is logged and
// acknowledged rather than forwarded.
func (p *Proxy) serveHTTPLogs(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHTTPExportBytes))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	request := &collectorlogsv1.ExportLogsServiceRequest{}
	if err := proto.Unmarshal(body, request); err != nil {
		http.Error(w, "decode OTLP request", http.StatusBadRequest)
		return
	}
	logExportedRecords(request)
	encoded, err := proto.Marshal(&collectorlogsv1.ExportLogsServiceResponse{})
	if err != nil {
		http.Error(w, "encode OTLP response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(encoded)
}

func logExportedRecords(request *collectorlogsv1.ExportLogsServiceRequest) {
	for _, resourceLogs := range request.ResourceLogs {
		for _, scopeLogs := range resourceLogs.ScopeLogs {
			scope := ""
			if scopeLogs.Scope != nil {
				scope = scopeLogs.Scope.Name
			}
			for _, record := range scopeLogs.LogRecords {
				attrs := make([]string, 0, len(record.Attributes))
				for _, attr := range record.Attributes {
					attrs = append(attrs, attr.Key+"="+truncateValue(attr.Value))
				}
				log.Printf("otlp log: scope=%s severity=%s body=%s attrs={%s}",
					scope, record.SeverityText, truncateValue(record.Body), strings.Join(attrs, " "))
			}
		}
	}
}

func truncateValue(value *commonv1.AnyValue) string {
	if value == nil {
		return ""
	}
	rendered := strings.ReplaceAll(value.String(), "\n", "\\n")
	if len(rendered) > maxLoggedValueBytes {
		return rendered[:maxLoggedValueBytes] + "..."
	}
	return rendered
}

func (p *Proxy) Address() string {
	return p.address
}

// HTTPAddress is the OTLP/HTTP collector root; callers append the signal path.
func (p *Proxy) HTTPAddress() string {
	return p.httpAddress
}

func (p *Proxy) Export(ctx context.Context, req *collectortracev1.ExportTraceServiceRequest) (*collectortracev1.ExportTraceServiceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "trace export request missing")
	}
	if threadID := p.threadIDValue(); threadID != "" {
		injectThreadID(req, threadID)
	}
	if p.workloadID != "" {
		injectWorkloadID(req, p.workloadID)
	}
	if messageID := p.messageIDValue(); messageID != "" {
		injectMessageID(req, messageID)
	}
	return p.upstream.Export(ctx, req)
}

func (p *Proxy) SetThreadID(threadID string) {
	p.threadMu.Lock()
	p.threadID = threadID
	p.threadMu.Unlock()
}

func (p *Proxy) threadIDValue() string {
	p.threadMu.RLock()
	defer p.threadMu.RUnlock()
	return p.threadID
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
	if p.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = p.httpServer.Shutdown(shutdownCtx)
		cancel()
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
		for _, scopeSpans := range spans.GetScopeSpans() {
			for _, span := range scopeSpans.GetSpans() {
				upsertSpanAttribute(span, messageIDAttributeKey, messageID)
			}
		}
	}
}

func upsertSpanAttribute(span *tracev1.Span, key, value string) {
	if span == nil {
		return
	}
	attributeValue := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_StringValue{StringValue: value},
	}
	for _, attribute := range span.Attributes {
		if attribute != nil && attribute.Key == key {
			attribute.Value = attributeValue
			return
		}
	}
	span.Attributes = append(span.Attributes, &commonv1.KeyValue{
		Key:   key,
		Value: attributeValue,
	})
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
