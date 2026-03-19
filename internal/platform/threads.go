package platform

import (
	"context"
	"fmt"
	"time"

	threadsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/threads/v1"
	"google.golang.org/grpc"
)

type Message struct {
	ID        string
	ThreadID  string
	SenderID  string
	Body      string
	FileIDs   []string
	CreatedAt time.Time
}

type Threads struct {
	client threadsv1.ThreadsServiceClient
}

func NewThreads(conn *grpc.ClientConn) *Threads {
	return &Threads{client: threadsv1.NewThreadsServiceClient(conn)}
}

func (t *Threads) GetUnackedMessages(ctx context.Context, participantID string, pageSize int32, pageToken string) ([]Message, string, error) {
	if participantID == "" {
		return nil, "", fmt.Errorf("participant id is required")
	}
	resp, err := t.client.GetUnackedMessages(ctx, &threadsv1.GetUnackedMessagesRequest{
		ParticipantId: participantID,
		PageSize:      pageSize,
		PageToken:     pageToken,
	})
	if err != nil {
		return nil, "", fmt.Errorf("get unacked messages: %w", err)
	}
	messages := make([]Message, 0, len(resp.GetMessages()))
	for _, msg := range resp.GetMessages() {
		parsed, err := messageFromProto(msg)
		if err != nil {
			return nil, "", err
		}
		messages = append(messages, parsed)
	}
	return messages, resp.GetNextPageToken(), nil
}

func (t *Threads) SendMessage(ctx context.Context, threadID, senderID, body string, fileIDs []string) (Message, error) {
	if threadID == "" {
		return Message{}, fmt.Errorf("thread id is required")
	}
	if senderID == "" {
		return Message{}, fmt.Errorf("sender id is required")
	}
	if body == "" && len(fileIDs) == 0 {
		return Message{}, fmt.Errorf("message body or file ids are required")
	}
	resp, err := t.client.SendMessage(ctx, &threadsv1.SendMessageRequest{
		ThreadId: threadID,
		SenderId: senderID,
		Body:     body,
		FileIds:  append([]string{}, fileIDs...),
	})
	if err != nil {
		return Message{}, fmt.Errorf("send message: %w", err)
	}
	return messageFromProto(resp.GetMessage())
}

func (t *Threads) AckMessages(ctx context.Context, participantID string, messageIDs []string) error {
	if participantID == "" {
		return fmt.Errorf("participant id is required")
	}
	if len(messageIDs) == 0 {
		return fmt.Errorf("message ids are required")
	}
	for _, id := range messageIDs {
		if id == "" {
			return fmt.Errorf("message id is required")
		}
	}
	_, err := t.client.AckMessages(ctx, &threadsv1.AckMessagesRequest{
		ParticipantId: participantID,
		MessageIds:    append([]string{}, messageIDs...),
	})
	if err != nil {
		return fmt.Errorf("ack messages: %w", err)
	}
	return nil
}

func messageFromProto(msg *threadsv1.Message) (Message, error) {
	if msg == nil {
		return Message{}, fmt.Errorf("message is nil")
	}
	id := msg.GetId()
	if id == "" {
		return Message{}, fmt.Errorf("message.id is required")
	}
	threadID := msg.GetThreadId()
	if threadID == "" {
		return Message{}, fmt.Errorf("message.thread_id is required")
	}
	senderID := msg.GetSenderId()
	if senderID == "" {
		return Message{}, fmt.Errorf("message.sender_id is required")
	}
	createdAt := msg.GetCreatedAt()
	if createdAt == nil {
		return Message{}, fmt.Errorf("message.created_at is required")
	}
	fileIDs := append([]string{}, msg.GetFileIds()...)
	if msg.GetBody() == "" && len(fileIDs) == 0 {
		return Message{}, fmt.Errorf("message body or file ids are required")
	}
	return Message{
		ID:        id,
		ThreadID:  threadID,
		SenderID:  senderID,
		Body:      msg.GetBody(),
		FileIDs:   fileIDs,
		CreatedAt: createdAt.AsTime(),
	}, nil
}
