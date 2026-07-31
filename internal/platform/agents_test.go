package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeAgentsClient struct {
	// Embedded so the fake satisfies the client without restating it. Adding an
	// RPC to the API cannot break this build; calling one the test did not
	// override panics, which is what a test wants.
	gatewayv1.AgentsGatewayClient

	inboxReq  *agentsv1.GetUnackedInboxItemsRequest
	inboxResp *agentsv1.GetUnackedInboxItemsResponse
	inboxErr  error

	ackReq *agentsv1.AckInboxItemsRequest
	ackErr error
}

func (f *fakeAgentsClient) GetUnackedInboxItems(ctx context.Context, in *agentsv1.GetUnackedInboxItemsRequest, opts ...grpc.CallOption) (*agentsv1.GetUnackedInboxItemsResponse, error) {
	f.inboxReq = in
	if f.inboxErr != nil {
		return nil, f.inboxErr
	}
	return f.inboxResp, nil
}

func (f *fakeAgentsClient) AckInboxItems(ctx context.Context, in *agentsv1.AckInboxItemsRequest, opts ...grpc.CallOption) (*agentsv1.AckInboxItemsResponse, error) {
	f.ackReq = in
	if f.ackErr != nil {
		return nil, f.ackErr
	}
	return &agentsv1.AckInboxItemsResponse{}, nil
}

func strPtr(value string) *string { return &value }

func TestAgentsGetUnackedInboxItems(t *testing.T) {
	accepted := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	fake := &fakeAgentsClient{inboxResp: &agentsv1.GetUnackedInboxItemsResponse{
		Items: []*agentsv1.InboxItem{{
			Id:              "item-1",
			AgentInstanceId: "instance-1",
			SourceKind:      agentsv1.InboxItemSourceKind_INBOX_ITEM_SOURCE_KIND_THREAD,
			ThreadId:        strPtr("thread-1"),
			MessageId:       strPtr("message-1"),
			SenderId:        "sender-1",
			Body:            "hello",
			AcceptedAt:      timestamppb.New(accepted),
		}},
		NextPageToken: "next",
	}}

	messages, token, err := NewAgents(fake).GetUnackedInboxItems(context.Background(), "instance-1", 25, "page")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if token != "next" {
		t.Fatalf("expected next page token, got %q", token)
	}
	want := []Message{{
		ID:          "message-1",
		InboxItemID: "item-1",
		ThreadID:    "thread-1",
		SenderID:    "sender-1",
		Body:        "hello",
		FileIDs:     []string{},
		CreatedAt:   accepted,
	}}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("expected %+v, got %+v", want, messages)
	}
	if fake.inboxReq.GetAgentInstanceId() != "instance-1" || fake.inboxReq.GetPageSize() != 25 || fake.inboxReq.GetPageToken() != "page" {
		t.Fatalf("unexpected request: %+v", fake.inboxReq)
	}
}

// A direct item has no thread and no originating message, so the item id is
// what identifies it and the thread stays empty.
func TestAgentsGetUnackedInboxItemsDirect(t *testing.T) {
	fake := &fakeAgentsClient{inboxResp: &agentsv1.GetUnackedInboxItemsResponse{
		Items: []*agentsv1.InboxItem{{
			Id:         "item-1",
			SourceKind: agentsv1.InboxItemSourceKind_INBOX_ITEM_SOURCE_KIND_DIRECT,
			SenderId:   "sender-1",
			Body:       "hello",
			AcceptedAt: timestamppb.Now(),
		}},
	}}

	messages, _, err := NewAgents(fake).GetUnackedInboxItems(context.Background(), "instance-1", 25, "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %d", len(messages))
	}
	if messages[0].ID != "item-1" || messages[0].InboxItemID != "item-1" || messages[0].ThreadID != "" {
		t.Fatalf("unexpected message: %+v", messages[0])
	}
}

func TestAgentsGetUnackedInboxItemsRejectsInvalidItems(t *testing.T) {
	tests := map[string]*agentsv1.InboxItem{
		"missing id": {
			SourceKind: agentsv1.InboxItemSourceKind_INBOX_ITEM_SOURCE_KIND_DIRECT,
			SenderId:   "sender-1",
			Body:       "hello",
			AcceptedAt: timestamppb.Now(),
		},
		"missing sender": {
			Id:         "item-1",
			SourceKind: agentsv1.InboxItemSourceKind_INBOX_ITEM_SOURCE_KIND_DIRECT,
			Body:       "hello",
			AcceptedAt: timestamppb.Now(),
		},
		"missing accepted at": {
			Id:         "item-1",
			SourceKind: agentsv1.InboxItemSourceKind_INBOX_ITEM_SOURCE_KIND_DIRECT,
			SenderId:   "sender-1",
			Body:       "hello",
		},
		"empty body and files": {
			Id:         "item-1",
			SourceKind: agentsv1.InboxItemSourceKind_INBOX_ITEM_SOURCE_KIND_DIRECT,
			SenderId:   "sender-1",
			AcceptedAt: timestamppb.Now(),
		},
		"thread sourced without thread": {
			Id:         "item-1",
			SourceKind: agentsv1.InboxItemSourceKind_INBOX_ITEM_SOURCE_KIND_THREAD,
			SenderId:   "sender-1",
			Body:       "hello",
			AcceptedAt: timestamppb.Now(),
		},
		"unspecified source kind": {
			Id:         "item-1",
			SenderId:   "sender-1",
			Body:       "hello",
			AcceptedAt: timestamppb.Now(),
		},
	}
	for name, item := range tests {
		t.Run(name, func(t *testing.T) {
			fake := &fakeAgentsClient{inboxResp: &agentsv1.GetUnackedInboxItemsResponse{Items: []*agentsv1.InboxItem{item}}}
			if _, _, err := NewAgents(fake).GetUnackedInboxItems(context.Background(), "instance-1", 25, ""); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestAgentsGetUnackedInboxItemsValidation(t *testing.T) {
	if _, _, err := NewAgents(&fakeAgentsClient{}).GetUnackedInboxItems(context.Background(), "", 25, ""); err == nil {
		t.Fatal("expected an error for a missing instance id")
	}
}

func TestAgentsGetUnackedInboxItemsWrapsError(t *testing.T) {
	fake := &fakeAgentsClient{inboxErr: errors.New("rpc failed")}
	_, _, err := NewAgents(fake).GetUnackedInboxItems(context.Background(), "instance-1", 25, "")
	if err == nil || !errors.Is(err, fake.inboxErr) {
		t.Fatalf("expected the rpc error to be wrapped, got %v", err)
	}
}

func TestAgentsAckInboxItems(t *testing.T) {
	fake := &fakeAgentsClient{}
	if err := NewAgents(fake).AckInboxItems(context.Background(), "instance-1", []string{"item-1", "item-2"}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if fake.ackReq.GetAgentInstanceId() != "instance-1" {
		t.Fatalf("unexpected instance id: %+v", fake.ackReq)
	}
	if !reflect.DeepEqual(fake.ackReq.GetItemIds(), []string{"item-1", "item-2"}) {
		t.Fatalf("unexpected item ids: %+v", fake.ackReq.GetItemIds())
	}
}

func TestAgentsAckInboxItemsValidation(t *testing.T) {
	agents := NewAgents(&fakeAgentsClient{})
	if err := agents.AckInboxItems(context.Background(), "", []string{"item-1"}); err == nil {
		t.Fatal("expected an error for a missing instance id")
	}
	if err := agents.AckInboxItems(context.Background(), "instance-1", nil); err == nil {
		t.Fatal("expected an error for missing item ids")
	}
	if err := agents.AckInboxItems(context.Background(), "instance-1", []string{""}); err == nil {
		t.Fatal("expected an error for an empty item id")
	}
}

func TestAgentsAckInboxItemsWrapsError(t *testing.T) {
	fake := &fakeAgentsClient{ackErr: errors.New("rpc failed")}
	err := NewAgents(fake).AckInboxItems(context.Background(), "instance-1", []string{"item-1"})
	if err == nil || !errors.Is(err, fake.ackErr) {
		t.Fatalf("expected the rpc error to be wrapped, got %v", err)
	}
}

// The inbox consumer must read the inbox, not the thread participant's
// messages: an instance has no single thread to read.
func TestInboxConsumerSyncReadsTheInbox(t *testing.T) {
	fake := &fakeAgentsClient{inboxResp: &agentsv1.GetUnackedInboxItemsResponse{
		Items: []*agentsv1.InboxItem{{
			Id:         "item-1",
			SourceKind: agentsv1.InboxItemSourceKind_INBOX_ITEM_SOURCE_KIND_THREAD,
			ThreadId:   strPtr("thread-1"),
			MessageId:  strPtr("message-1"),
			SenderId:   "sender-1",
			Body:       "hello",
			AcceptedAt: timestamppb.Now(),
		}},
	}}

	var handled []Message
	consumer := NewInboxConsumer(NewAgents(fake), 10, time.Second)
	if err := consumer.Sync(context.Background(), "instance-1", "", func(message Message) error {
		handled = append(handled, message)
		return nil
	}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(handled) != 1 || handled[0].InboxItemID != "item-1" || handled[0].ThreadID != "thread-1" {
		t.Fatalf("unexpected handled messages: %+v", handled)
	}
	if fake.inboxReq.GetAgentInstanceId() != "instance-1" {
		t.Fatalf("expected the instance id as the participant, got %+v", fake.inboxReq)
	}
}
