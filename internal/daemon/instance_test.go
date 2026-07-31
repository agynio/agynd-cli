package daemon

import (
	"context"
	"testing"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	"github.com/agynio/agynd-cli/internal/config"
	"github.com/agynio/agynd-cli/internal/platform"
	claude "github.com/agynio/claude-sdk-go"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

const testAgentInstanceID = "1c2f0f4e-8b9d-4a5b-9b3c-1d2e3f4a5b6c"

type fakeAgentsInboxClient struct {
	// Embedded so the fake satisfies the client without restating it. Adding an
	// RPC to the API cannot break this build; calling one the test did not
	// override panics, which is what a test wants.
	gatewayv1.AgentsGatewayClient

	ackRequests []*agentsv1.AckInboxItemsRequest
}

func (f *fakeAgentsInboxClient) AckInboxItems(ctx context.Context, in *agentsv1.AckInboxItemsRequest, opts ...grpc.CallOption) (*agentsv1.AckInboxItemsResponse, error) {
	f.ackRequests = append(f.ackRequests, in)
	return &agentsv1.AckInboxItemsResponse{}, nil
}

// An instance speaks as itself, not as its agent class: the class names every
// instance at once, so a reply attributed to it cannot be traced back.
func TestInstanceRepliesAndAcksAsItself(t *testing.T) {
	client := &fakeClaudeClient{result: &claude.TurnResult{Response: "hello"}}
	threadsClient := &fakeClaudeThreadsClient{}
	inboxClient := &fakeAgentsInboxClient{}
	instanceID := uuid.MustParse(testAgentInstanceID)
	daemon := &Daemon{
		sdk: SDKClaude,
		cfg: config.Config{
			AgentID:         uuid.MustParse(testAgentID),
			AgentInstanceID: instanceID,
		},
		threads:    platform.NewThreads(threadsClient),
		agentInbox: platform.NewAgents(inboxClient),
		claude:     client,
	}

	message := platform.Message{
		ID:          "msg-1",
		InboxItemID: "item-1",
		ThreadID:    "thread-1",
		Body:        "hello",
	}
	if err := daemon.handleClaudeMessage(context.Background(), message); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(threadsClient.sendRequests) != 1 {
		t.Fatalf("expected one SendMessage, got %d", len(threadsClient.sendRequests))
	}
	if got := threadsClient.sendRequests[0].GetSenderId(); got != instanceID.String() {
		t.Fatalf("expected sender %q, got %q", instanceID, got)
	}

	// The item, not the message, is what the inbox tracks: acking the thread
	// message would leave it outstanding and the instance would re-read it.
	if len(threadsClient.ackRequests) != 0 {
		t.Fatalf("expected no thread acks, got %d", len(threadsClient.ackRequests))
	}
	if len(inboxClient.ackRequests) != 1 {
		t.Fatalf("expected one inbox ack, got %d", len(inboxClient.ackRequests))
	}
	ack := inboxClient.ackRequests[0]
	if ack.GetAgentInstanceId() != instanceID.String() {
		t.Fatalf("expected instance %q, got %q", instanceID, ack.GetAgentInstanceId())
	}
	if len(ack.GetItemIds()) != 1 || ack.GetItemIds()[0] != "item-1" {
		t.Fatalf("unexpected acked item ids: %#v", ack.GetItemIds())
	}
}

// Messages that did not arrive through an inbox still ack on the thread.
func TestThreadScopedDaemonStillAcksOnTheThread(t *testing.T) {
	client := &fakeClaudeClient{result: &claude.TurnResult{Response: "hello"}}
	threadsClient := &fakeClaudeThreadsClient{}
	inboxClient := &fakeAgentsInboxClient{}
	agentID := uuid.MustParse(testAgentID)
	daemon := &Daemon{
		sdk:        SDKClaude,
		cfg:        config.Config{AgentID: agentID},
		threads:    platform.NewThreads(threadsClient),
		agentInbox: platform.NewAgents(inboxClient),
		claude:     client,
	}

	message := platform.Message{ID: "msg-1", ThreadID: "thread-1", Body: "hello"}
	if err := daemon.handleClaudeMessage(context.Background(), message); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(inboxClient.ackRequests) != 0 {
		t.Fatalf("expected no inbox acks, got %d", len(inboxClient.ackRequests))
	}
	if len(threadsClient.ackRequests) != 1 {
		t.Fatalf("expected one thread ack, got %d", len(threadsClient.ackRequests))
	}
	if got := threadsClient.sendRequests[0].GetSenderId(); got != agentID.String() {
		t.Fatalf("expected sender %q, got %q", agentID, got)
	}
}

func TestSelfIDPrefersTheInstance(t *testing.T) {
	agentID := uuid.MustParse(testAgentID)
	instanceID := uuid.MustParse(testAgentInstanceID)

	withInstance := &Daemon{cfg: config.Config{AgentID: agentID, AgentInstanceID: instanceID}}
	if got := withInstance.selfID(); got != instanceID.String() {
		t.Fatalf("expected the instance id, got %q", got)
	}

	withoutInstance := &Daemon{cfg: config.Config{AgentID: agentID}}
	if got := withoutInstance.selfID(); got != agentID.String() {
		t.Fatalf("expected the agent id, got %q", got)
	}
}
