package platform

import (
	"context"
	"fmt"

	agentsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/agents/v1"
	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
)

type Agents struct {
	client gatewayv1.AgentsGatewayClient
}

func NewAgents(client gatewayv1.AgentsGatewayClient) *Agents {
	return &Agents{client: client}
}

func (a *Agents) GetUnackedInboxItems(ctx context.Context, agentInstanceID string, pageSize int32, pageToken string) ([]Message, string, error) {
	if agentInstanceID == "" {
		return nil, "", fmt.Errorf("agent instance id is required")
	}
	resp, err := a.client.GetUnackedInboxItems(ctx, &agentsv1.GetUnackedInboxItemsRequest{
		AgentInstanceId: agentInstanceID,
		PageSize:        pageSize,
		PageToken:       pageToken,
	})
	if err != nil {
		return nil, "", fmt.Errorf("get unacked inbox items: %w", err)
	}
	messages := make([]Message, 0, len(resp.GetItems()))
	for _, item := range resp.GetItems() {
		parsed, err := inboxItemFromProto(item)
		if err != nil {
			return nil, "", err
		}
		messages = append(messages, parsed)
	}
	return messages, resp.GetNextPageToken(), nil
}

func (a *Agents) AckInboxItems(ctx context.Context, agentInstanceID string, itemIDs []string) error {
	if agentInstanceID == "" {
		return fmt.Errorf("agent instance id is required")
	}
	if len(itemIDs) == 0 {
		return fmt.Errorf("item ids are required")
	}
	for _, id := range itemIDs {
		if id == "" {
			return fmt.Errorf("item id is required")
		}
	}
	_, err := a.client.AckInboxItems(ctx, &agentsv1.AckInboxItemsRequest{
		AgentInstanceId: agentInstanceID,
		ItemIds:         append([]string{}, itemIDs...),
	})
	if err != nil {
		return fmt.Errorf("ack inbox items: %w", err)
	}
	return nil
}

// inboxItemFromProto flattens an inbox item into the Message the daemon already
// handles. Thread-sourced items carry the thread and the originating message id;
// direct items carry neither, so they keep an empty ThreadID and are identified
// by the item id alone. InboxItemID is always the item, because that -- not the
// message -- is what the ack addresses.
func inboxItemFromProto(item *agentsv1.InboxItem) (Message, error) {
	if item == nil {
		return Message{}, fmt.Errorf("inbox item is nil")
	}
	id := item.GetId()
	if id == "" {
		return Message{}, fmt.Errorf("inbox item.id is required")
	}
	senderID := item.GetSenderId()
	if senderID == "" {
		return Message{}, fmt.Errorf("inbox item.sender_id is required")
	}
	acceptedAt := item.GetAcceptedAt()
	if acceptedAt == nil {
		return Message{}, fmt.Errorf("inbox item.accepted_at is required")
	}
	fileIDs := append([]string{}, item.GetFileIds()...)
	if item.GetBody() == "" && len(fileIDs) == 0 {
		return Message{}, fmt.Errorf("inbox item body or file ids are required")
	}

	messageID := id
	threadID := ""
	switch kind := item.GetSourceKind(); kind {
	case agentsv1.InboxItemSourceKind_INBOX_ITEM_SOURCE_KIND_THREAD:
		threadID = item.GetThreadId()
		if threadID == "" {
			return Message{}, fmt.Errorf("inbox item.thread_id is required for thread-sourced items")
		}
		if item.GetMessageId() != "" {
			messageID = item.GetMessageId()
		}
	case agentsv1.InboxItemSourceKind_INBOX_ITEM_SOURCE_KIND_DIRECT:
	default:
		return Message{}, fmt.Errorf("inbox item.source_kind %s is not supported", kind)
	}

	return Message{
		ID:          messageID,
		InboxItemID: id,
		ThreadID:    threadID,
		SenderID:    senderID,
		Body:        item.GetBody(),
		FileIDs:     fileIDs,
		CreatedAt:   acceptedAt.AsTime(),
	}, nil
}
