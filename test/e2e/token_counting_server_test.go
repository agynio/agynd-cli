//go:build e2e

package e2e

import (
	"context"
	"errors"
	"net"
	"testing"

	tokencountingv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/token_counting/v1"
	"google.golang.org/grpc"
)

func startTokenCountingServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen token counting: %v", err)
	}
	server := grpc.NewServer()
	tokencountingv1.RegisterTokenCountingServiceServer(server, tokenCountingServer{})
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Fatalf("serve token counting: %v", err)
		}
	default:
	}
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		select {
		case err := <-serveErr:
			if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				t.Errorf("token counting server error: %v", err)
			}
		default:
		}
	})
	return listener.Addr().String()
}

type tokenCountingServer struct {
	tokencountingv1.UnimplementedTokenCountingServiceServer
}

func (tokenCountingServer) CountTokens(_ context.Context, req *tokencountingv1.CountTokensRequest) (*tokencountingv1.CountTokensResponse, error) {
	if req == nil {
		return nil, errors.New("token counting request is required")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("token counting messages are required")
	}
	tokens := make([]int32, len(req.Messages))
	for i := range tokens {
		tokens[i] = 1
	}
	return &tokencountingv1.CountTokensResponse{Tokens: tokens}, nil
}
