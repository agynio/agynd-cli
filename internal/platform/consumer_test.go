package platform

import (
	"context"
	"fmt"
	"testing"
	"time"

	threadsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/threads/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeThreadsClient struct {
	responses []*threadsv1.GetUnackedMessagesResponse
	index     int
}

func (f *fakeThreadsClient) CreateThread(ctx context.Context, in *threadsv1.CreateThreadRequest, opts ...grpc.CallOption) (*threadsv1.CreateThreadResponse, error) {
	return nil, fmt.Errorf("CreateThread not implemented")
}

func (f *fakeThreadsClient) ArchiveThread(ctx context.Context, in *threadsv1.ArchiveThreadRequest, opts ...grpc.CallOption) (*threadsv1.ArchiveThreadResponse, error) {
	return nil, fmt.Errorf("ArchiveThread not implemented")
}

func (f *fakeThreadsClient) AddParticipant(ctx context.Context, in *threadsv1.AddParticipantRequest, opts ...grpc.CallOption) (*threadsv1.AddParticipantResponse, error) {
	return nil, fmt.Errorf("AddParticipant not implemented")
}

func (f *fakeThreadsClient) SendMessage(ctx context.Context, in *threadsv1.SendMessageRequest, opts ...grpc.CallOption) (*threadsv1.SendMessageResponse, error) {
	return nil, fmt.Errorf("SendMessage not implemented")
}

func (f *fakeThreadsClient) GetThreads(ctx context.Context, in *threadsv1.GetThreadsRequest, opts ...grpc.CallOption) (*threadsv1.GetThreadsResponse, error) {
	return nil, fmt.Errorf("GetThreads not implemented")
}

func (f *fakeThreadsClient) GetMessages(ctx context.Context, in *threadsv1.GetMessagesRequest, opts ...grpc.CallOption) (*threadsv1.GetMessagesResponse, error) {
	return nil, fmt.Errorf("GetMessages not implemented")
}

func (f *fakeThreadsClient) GetUnackedMessages(ctx context.Context, in *threadsv1.GetUnackedMessagesRequest, opts ...grpc.CallOption) (*threadsv1.GetUnackedMessagesResponse, error) {
	if f.index >= len(f.responses) {
		return nil, fmt.Errorf("unexpected GetUnackedMessages call")
	}
	resp := f.responses[f.index]
	f.index++
	return resp, nil
}

func (f *fakeThreadsClient) AckMessages(ctx context.Context, in *threadsv1.AckMessagesRequest, opts ...grpc.CallOption) (*threadsv1.AckMessagesResponse, error) {
	return nil, fmt.Errorf("AckMessages not implemented")
}

func TestConsumerSyncSortsMessages(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	messageA := &threadsv1.Message{
		Id:        "b",
		ThreadId:  "thread-1",
		SenderId:  "sender-1",
		Body:      "hello",
		CreatedAt: timestamppb.New(base.Add(2 * time.Second)),
	}
	messageB := &threadsv1.Message{
		Id:        "c",
		ThreadId:  "thread-1",
		SenderId:  "sender-1",
		Body:      "hello",
		CreatedAt: timestamppb.New(base),
	}
	messageC := &threadsv1.Message{
		Id:        "a",
		ThreadId:  "thread-1",
		SenderId:  "sender-1",
		Body:      "hello",
		CreatedAt: timestamppb.New(base),
	}

	fake := &fakeThreadsClient{
		responses: []*threadsv1.GetUnackedMessagesResponse{{
			Messages: []*threadsv1.Message{messageA, messageB, messageC},
		}},
	}
	threads := &Threads{client: fake}
	consumer := NewConsumer(threads, 100, 0)

	var got []Message
	err := consumer.Sync(context.Background(), "participant-1", func(message Message) error {
		got = append(got, message)
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "c" || got[2].ID != "b" {
		t.Fatalf("unexpected sort order: %q, %q, %q", got[0].ID, got[1].ID, got[2].ID)
	}
}
