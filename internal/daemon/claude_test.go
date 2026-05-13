package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	threadsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/threads/v1"
	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	claude "github.com/agynio/claude-sdk-go"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeClaudeClient struct {
	turnCalls int
	params    claude.TurnParams
	ctx       context.Context
	result    *claude.TurnResult
	err       error
}

func (f *fakeClaudeClient) Turn(ctx context.Context, params claude.TurnParams, _ claude.EventHandler) (*claude.TurnResult, error) {
	f.turnCalls++
	f.params = params
	f.ctx = ctx
	return f.result, f.err
}

func (f *fakeClaudeClient) Close() error {
	return nil
}

type fakeClaudeThreadsClient struct {
	sendRequests []*threadsv1.SendMessageRequest
	ackRequests  []*threadsv1.AckMessagesRequest
}

var _ gatewayv1.ThreadsGatewayClient = (*fakeClaudeThreadsClient)(nil)

func (f *fakeClaudeThreadsClient) CreateThread(ctx context.Context, in *threadsv1.CreateThreadRequest, opts ...grpc.CallOption) (*threadsv1.CreateThreadResponse, error) {
	return nil, fmt.Errorf("CreateThread not implemented")
}

func (f *fakeClaudeThreadsClient) ArchiveThread(ctx context.Context, in *threadsv1.ArchiveThreadRequest, opts ...grpc.CallOption) (*threadsv1.ArchiveThreadResponse, error) {
	return nil, fmt.Errorf("ArchiveThread not implemented")
}

func (f *fakeClaudeThreadsClient) AddParticipant(ctx context.Context, in *threadsv1.AddParticipantRequest, opts ...grpc.CallOption) (*threadsv1.AddParticipantResponse, error) {
	return nil, fmt.Errorf("AddParticipant not implemented")
}

func (f *fakeClaudeThreadsClient) SendMessage(ctx context.Context, in *threadsv1.SendMessageRequest, opts ...grpc.CallOption) (*threadsv1.SendMessageResponse, error) {
	f.sendRequests = append(f.sendRequests, in)
	return &threadsv1.SendMessageResponse{
		Message: &threadsv1.Message{
			Id:        "msg-out",
			ThreadId:  in.GetThreadId(),
			SenderId:  in.GetSenderId(),
			Body:      in.GetBody(),
			FileIds:   append([]string{}, in.GetFileIds()...),
			CreatedAt: timestamppb.New(time.Now()),
		},
	}, nil
}

func (f *fakeClaudeThreadsClient) GetThreads(ctx context.Context, in *threadsv1.GetThreadsRequest, opts ...grpc.CallOption) (*threadsv1.GetThreadsResponse, error) {
	return nil, fmt.Errorf("GetThreads not implemented")
}

func (f *fakeClaudeThreadsClient) GetOrganizationThreads(ctx context.Context, in *threadsv1.GetOrganizationThreadsRequest, opts ...grpc.CallOption) (*threadsv1.GetOrganizationThreadsResponse, error) {
	return nil, fmt.Errorf("GetOrganizationThreads not implemented")
}

func (f *fakeClaudeThreadsClient) ListOrganizationThreads(ctx context.Context, in *threadsv1.ListOrganizationThreadsRequest, opts ...grpc.CallOption) (*threadsv1.ListOrganizationThreadsResponse, error) {
	return nil, fmt.Errorf("ListOrganizationThreads not implemented")
}

func (f *fakeClaudeThreadsClient) GetThread(ctx context.Context, in *threadsv1.GetThreadRequest, opts ...grpc.CallOption) (*threadsv1.GetThreadResponse, error) {
	return nil, fmt.Errorf("GetThread not implemented")
}

func (f *fakeClaudeThreadsClient) GetMessages(ctx context.Context, in *threadsv1.GetMessagesRequest, opts ...grpc.CallOption) (*threadsv1.GetMessagesResponse, error) {
	return nil, fmt.Errorf("GetMessages not implemented")
}

func (f *fakeClaudeThreadsClient) GetUnackedMessages(ctx context.Context, in *threadsv1.GetUnackedMessagesRequest, opts ...grpc.CallOption) (*threadsv1.GetUnackedMessagesResponse, error) {
	return nil, fmt.Errorf("GetUnackedMessages not implemented")
}

func (f *fakeClaudeThreadsClient) GetUnackedMessageCounts(ctx context.Context, in *threadsv1.GetUnackedMessageCountsRequest, opts ...grpc.CallOption) (*threadsv1.GetUnackedMessageCountsResponse, error) {
	return nil, fmt.Errorf("GetUnackedMessageCounts not implemented")
}

func (f *fakeClaudeThreadsClient) AckMessages(ctx context.Context, in *threadsv1.AckMessagesRequest, opts ...grpc.CallOption) (*threadsv1.AckMessagesResponse, error) {
	f.ackRequests = append(f.ackRequests, in)
	return &threadsv1.AckMessagesResponse{}, nil
}

func TestHandleClaudeMessageSuccess(t *testing.T) {
	client := &fakeClaudeClient{result: &claude.TurnResult{Response: "hello"}}
	threadsClient := &fakeClaudeThreadsClient{}
	threads := platform.NewThreads(threadsClient)
	agentID := uuid.MustParse(testAgentID)
	daemon := &Daemon{
		sdk:     SDKClaude,
		cfg:     config.Config{AgentID: agentID},
		threads: threads,
		claude:  client,
	}

	message := platform.Message{
		ID:       "msg-1",
		ThreadID: "thread-1",
		Body:     " hello ",
	}

	if err := daemon.handleClaudeMessage(context.Background(), message); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client.turnCalls != 1 {
		t.Fatalf("expected Turn to be called once, got %d", client.turnCalls)
	}
	if client.params.Prompt != "hello" {
		t.Fatalf("expected prompt %q, got %q", "hello", client.params.Prompt)
	}
	if _, ok := client.ctx.Deadline(); ok {
		t.Fatal("expected claude turn context without completion deadline")
	}
	if len(threadsClient.sendRequests) != 1 {
		t.Fatalf("expected SendMessage to be called once, got %d", len(threadsClient.sendRequests))
	}
	sendReq := threadsClient.sendRequests[0]
	if sendReq.GetThreadId() != "thread-1" {
		t.Fatalf("expected thread id %q, got %q", "thread-1", sendReq.GetThreadId())
	}
	if sendReq.GetSenderId() != agentID.String() {
		t.Fatalf("expected sender id %q, got %q", agentID.String(), sendReq.GetSenderId())
	}
	if sendReq.GetBody() != "hello" {
		t.Fatalf("expected response body %q, got %q", "hello", sendReq.GetBody())
	}
	if len(threadsClient.ackRequests) != 1 {
		t.Fatalf("expected AckMessages to be called once, got %d", len(threadsClient.ackRequests))
	}
	ackReq := threadsClient.ackRequests[0]
	if ackReq.GetParticipantId() != agentID.String() {
		t.Fatalf("expected participant id %q, got %q", agentID.String(), ackReq.GetParticipantId())
	}
	if len(ackReq.GetMessageIds()) != 1 || ackReq.GetMessageIds()[0] != "msg-1" {
		t.Fatalf("unexpected ack message ids: %#v", ackReq.GetMessageIds())
	}
}

func TestHandleClaudeMessageEmptyResponse(t *testing.T) {
	client := &fakeClaudeClient{result: &claude.TurnResult{Response: " "}}
	threadsClient := &fakeClaudeThreadsClient{}
	daemon := &Daemon{
		sdk:     SDKClaude,
		cfg:     config.Config{AgentID: uuid.MustParse(testAgentID)},
		threads: platform.NewThreads(threadsClient),
		claude:  client,
	}

	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	if err := daemon.handleClaudeMessage(context.Background(), message); err == nil {
		t.Fatal("expected error, got nil")
	} else if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(threadsClient.sendRequests) != 0 {
		t.Fatalf("expected no send requests, got %d", len(threadsClient.sendRequests))
	}
	if len(threadsClient.ackRequests) != 0 {
		t.Fatalf("expected no ack requests, got %d", len(threadsClient.ackRequests))
	}
}

func TestHandleClaudeMessageWrapsTurnError(t *testing.T) {
	turnErr := fmt.Errorf("turn failed")
	client := &fakeClaudeClient{err: turnErr}
	daemon := &Daemon{
		sdk:    SDKClaude,
		cfg:    config.Config{AgentID: uuid.MustParse(testAgentID)},
		claude: client,
	}

	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	err := daemon.handleClaudeMessage(context.Background(), message)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, expected := range []string{"claude_turn", "run claude turn for message msg-1", "thread-1"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected %q in error: %v", expected, err)
		}
	}
	if !errors.Is(err, turnErr) {
		t.Fatalf("expected wrapped turn error, got %v", err)
	}
}

func TestHandleClaudeMessageMissingThreadID(t *testing.T) {
	client := &fakeClaudeClient{result: &claude.TurnResult{Response: "hello"}}
	daemon := &Daemon{
		sdk:    SDKClaude,
		cfg:    config.Config{AgentID: uuid.MustParse(testAgentID)},
		claude: client,
	}

	message := platform.Message{ID: "msg-1", ThreadID: " ", Body: "hello"}
	if err := daemon.handleClaudeMessage(context.Background(), message); err == nil {
		t.Fatal("expected error, got nil")
	}
	if client.turnCalls != 0 {
		t.Fatalf("expected Turn not to be called, got %d", client.turnCalls)
	}
}

func TestHandleClaudeMessageEmptyBody(t *testing.T) {
	client := &fakeClaudeClient{result: &claude.TurnResult{Response: "hello"}}
	daemon := &Daemon{
		sdk:    SDKClaude,
		cfg:    config.Config{AgentID: uuid.MustParse(testAgentID)},
		claude: client,
	}

	message := platform.Message{ID: "msg-1", ThreadID: "thread-1"}
	if err := daemon.handleClaudeMessage(context.Background(), message); err == nil {
		t.Fatal("expected error, got nil")
	}
	if client.turnCalls != 0 {
		t.Fatalf("expected Turn not to be called, got %d", client.turnCalls)
	}
}
