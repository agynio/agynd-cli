package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	threadsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/threads/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeThreadsClient struct {
	// Embedded so the fake satisfies the client without restating it. Adding an
	// RPC to the API cannot break this build; calling one the test did not
	// override panics, which is what a test wants.
	gatewayv1.ThreadsGatewayClient

	responses []*threadsv1.GetUnackedMessagesResponse
	index     int
	requests  []*threadsv1.GetUnackedMessagesRequest
	err       error
}

func (f *fakeThreadsClient) GetUnackedMessages(ctx context.Context, in *threadsv1.GetUnackedMessagesRequest, opts ...grpc.CallOption) (*threadsv1.GetUnackedMessagesResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.index >= len(f.responses) {
		return nil, fmt.Errorf("unexpected GetUnackedMessages call")
	}
	f.requests = append(f.requests, in)
	resp := f.responses[f.index]
	f.index++
	return resp, nil
}

func TestConsumerSyncWrapsPageFetchError(t *testing.T) {
	fetchErr := fmt.Errorf("rpc failed")
	fake := &fakeThreadsClient{err: fetchErr}
	consumer := NewConsumer(&Threads{client: fake}, 100, 0)

	err := consumer.Sync(context.Background(), "participant-1", "thread-1", func(message Message) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var pageFetchErr *PageFetchError
	if !errors.As(err, &pageFetchErr) {
		t.Fatalf("expected PageFetchError, got %T", err)
	}
	if !errors.Is(err, fetchErr) {
		t.Fatalf("expected wrapped fetch error, got %v", err)
	}
}

func (f *fakeThreadsClient) GetUnackedMessageCounts(ctx context.Context, in *threadsv1.GetUnackedMessageCountsRequest, opts ...grpc.CallOption) (*threadsv1.GetUnackedMessageCountsResponse, error) {
	return nil, fmt.Errorf("GetUnackedMessageCounts not implemented")
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
	err := consumer.Sync(context.Background(), "participant-1", "thread-1", func(message Message) error {
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
	if len(fake.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(fake.requests))
	}
	if fake.requests[0].ThreadId == nil || *fake.requests[0].ThreadId != "thread-1" {
		t.Fatalf("expected thread id filter to be set")
	}
}

func TestConsumerSyncNoThreadFilter(t *testing.T) {
	fake := &fakeThreadsClient{
		responses: []*threadsv1.GetUnackedMessagesResponse{{}},
	}
	threads := &Threads{client: fake}
	consumer := NewConsumer(threads, 100, 0)

	called := false
	err := consumer.Sync(context.Background(), "participant-1", "", func(message Message) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if called {
		t.Fatal("expected no messages to be handled")
	}
	if len(fake.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(fake.requests))
	}
	if fake.requests[0].ThreadId != nil {
		t.Fatalf("expected no thread filter, got %q", fake.requests[0].GetThreadId())
	}
}
