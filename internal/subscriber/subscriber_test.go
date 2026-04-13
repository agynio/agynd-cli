package subscriber

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	notificationsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/notifications/v1"
	"github.com/agynio/agynd-cli/internal/platform"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

type fakeNotifications struct {
	stream  platform.SubscribeStream
	agentID string
	called  int
}

func (f *fakeNotifications) Subscribe(_ context.Context, agentID string) (platform.SubscribeStream, error) {
	f.called++
	f.agentID = agentID
	return f.stream, nil
}

type fakeStream struct {
	responses chan *notificationsv1.SubscribeResponse
}

func (f *fakeStream) Recv() (*notificationsv1.SubscribeResponse, error) {
	resp, ok := <-f.responses
	if !ok {
		return nil, io.EOF
	}
	return resp, nil
}

func makeThreadPayload(threadID string) *structpb.Struct {
	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"thread_id": structpb.NewStringValue(threadID),
		},
	}
}

func makeMessageCreated(threadID string) *notificationsv1.SubscribeResponse {
	return &notificationsv1.SubscribeResponse{
		Envelope: &notificationsv1.NotificationEnvelope{
			Event:   messageCreatedEvent,
			Payload: makeThreadPayload(threadID),
		},
	}
}

func waitForRun(t *testing.T, done <-chan error) {
	t.Helper()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestSubscriberWakeOnMatchingThread(t *testing.T) {
	threadID := "9d06fe19-695b-48ac-83ba-2cd82472f7c8"
	responses := make(chan *notificationsv1.SubscribeResponse, 1)
	stream := &fakeStream{responses: responses}
	client := &fakeNotifications{stream: stream}
	sub := New(client, "agent-1", threadID)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sub.Run(ctx)
	}()

	responses <- makeMessageCreated(threadID)

	select {
	case <-sub.Wake():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected wake signal")
	}

	cancel()
	close(responses)
	waitForRun(t, done)
}

func TestSubscriberIgnoreOtherThread(t *testing.T) {
	threadID := "9d06fe19-695b-48ac-83ba-2cd82472f7c8"
	otherThreadID := "1f5a4e5c-350a-4f3a-8dda-0ef049b89d09"
	responses := make(chan *notificationsv1.SubscribeResponse, 1)
	stream := &fakeStream{responses: responses}
	client := &fakeNotifications{stream: stream}
	sub := New(client, "agent-1", threadID)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sub.Run(ctx)
	}()

	responses <- makeMessageCreated(otherThreadID)

	select {
	case <-sub.Wake():
		t.Fatal("unexpected wake signal")
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	close(responses)
	waitForRun(t, done)
}

func TestNextBackoffProgression(t *testing.T) {
	tests := []struct {
		name     string
		current  time.Duration
		expected time.Duration
	}{
		{name: "zero", current: 0, expected: time.Second},
		{name: "one-second", current: time.Second, expected: 2 * time.Second},
		{name: "two-seconds", current: 2 * time.Second, expected: 4 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := nextBackoff(test.current)
			if got != test.expected {
				t.Fatalf("expected %s, got %s", test.expected, got)
			}
		})
	}
}

func TestNextBackoffCapsAtThirtySeconds(t *testing.T) {
	if got := nextBackoff(20 * time.Second); got != 30*time.Second {
		t.Fatalf("expected cap at 30s, got %s", got)
	}
	if got := nextBackoff(30 * time.Second); got != 30*time.Second {
		t.Fatalf("expected cap at 30s, got %s", got)
	}
}
