package subscriber

import (
	"context"
	"errors"
	"io"
	"sync"
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

func (f *fakeNotifications) Subscribe(_ context.Context) (platform.SubscribeStream, error) {
	f.called++
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

func TestPayloadThreadID(t *testing.T) {
	tests := []struct {
		name    string
		payload *structpb.Struct
		want    string
		ok      bool
	}{
		{name: "nil-payload", payload: nil, want: "", ok: false},
		{name: "nil-fields", payload: &structpb.Struct{}, want: "", ok: false},
		{
			name: "missing-thread-id",
			payload: &structpb.Struct{Fields: map[string]*structpb.Value{
				"other": structpb.NewStringValue("value"),
			}},
			want: "",
			ok:   false,
		},
		{
			name: "empty-thread-id",
			payload: &structpb.Struct{Fields: map[string]*structpb.Value{
				"thread_id": structpb.NewStringValue(" "),
			}},
			want: "",
			ok:   false,
		},
		{
			name: "invalid-thread-id",
			payload: &structpb.Struct{Fields: map[string]*structpb.Value{
				"thread_id": structpb.NewStringValue("not-a-uuid"),
			}},
			want: "",
			ok:   false,
		},
		{
			name: "valid-thread-id",
			payload: &structpb.Struct{Fields: map[string]*structpb.Value{
				"thread_id": structpb.NewStringValue("  9E54A9CE-F2E1-4B5F-9C91-8F4C77554C30 "),
			}},
			want: "9e54a9ce-f2e1-4b5f-9c91-8f4c77554c30",
			ok:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := payloadThreadID(test.payload)
			if ok != test.ok {
				t.Fatalf("expected ok=%v, got %v", test.ok, ok)
			}
			if got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestSubscriberWakeOnMatchingThread(t *testing.T) {
	threadID := "9d06fe19-695b-48ac-83ba-2cd82472f7c8"
	responses := make(chan *notificationsv1.SubscribeResponse, 1)
	stream := &fakeStream{responses: responses}
	client := &fakeNotifications{stream: stream}
	sub := New(client, threadID)

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
	sub := New(client, threadID)

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

// An instance is not pinned to a thread and must wake for every one of them,
// including notifications that carry no thread at all.
func TestSubscriberWithoutThreadWakesOnAnyThread(t *testing.T) {
	responses := make(chan *notificationsv1.SubscribeResponse, 2)
	stream := &fakeStream{responses: responses}
	client := &fakeNotifications{stream: stream}
	sub := New(client, "")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sub.Run(ctx)
	}()

	responses <- makeMessageCreated("1f5a4e5c-350a-4f3a-8dda-0ef049b89d09")

	select {
	case <-sub.Wake():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected wake signal")
	}

	cancel()
	close(responses)
	waitForRun(t, done)
}

// A stream whose Recv ends with its context, as a gRPC stream's does.
type wakeStream struct {
	done      <-chan struct{}
	responses chan *notificationsv1.SubscribeResponse
}

func (s *wakeStream) Recv() (*notificationsv1.SubscribeResponse, error) {
	select {
	case <-s.done:
		return nil, context.Canceled
	case resp := <-s.responses:
		return resp, nil
	}
}

type wakeNotifications struct {
	mu       sync.Mutex
	streams  []*wakeStream
	failures int // subscribe attempts that fail before one succeeds
	attempts int
}

func (f *wakeNotifications) Subscribe(ctx context.Context) (platform.SubscribeStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.failures > 0 {
		f.failures--
		return nil, errors.New("gateway unavailable")
	}
	stream := &wakeStream{done: ctx.Done(), responses: make(chan *notificationsv1.SubscribeResponse, 1)}
	f.streams = append(f.streams, stream)
	return stream, nil
}

func (f *wakeNotifications) Streams() []*wakeStream {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*wakeStream(nil), f.streams...)
}

func (f *wakeNotifications) Attempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}

// Resubscribe ends the stream and Run reopens it at once: the drop is the
// daemon's own, after a wake from hibernation, not a failure to back off from.
func TestSubscriberResubscribeReopensStream(t *testing.T) {
	client := &wakeNotifications{}
	sub := New(client, "")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sub.Run(ctx)
	}()

	select {
	case <-sub.Ready():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected the first stream")
	}
	sub.Resubscribe()

	// Well inside the first backoff, which a deliberate drop does not wait.
	deadline := time.After(500 * time.Millisecond)
	for len(client.Streams()) < 2 {
		select {
		case <-deadline:
			t.Fatalf("expected a second stream, got %d", len(client.Streams()))
		case <-time.After(10 * time.Millisecond):
		}
	}
	streams := client.Streams()
	select {
	case <-streams[0].done:
	default:
		t.Fatal("expected the first stream to be dropped")
	}

	streams[1].responses <- makeMessageCreated("1f5a4e5c-350a-4f3a-8dda-0ef049b89d09")
	select {
	case <-sub.Wake():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected a wake from the reopened stream")
	}

	cancel()
	waitForRun(t, done)
}

// A wake can land while the subscriber is waiting out a failure. The wait is
// for the failure, and the wake is not one: Resubscribe cuts it short and the
// stream is opened at once.
func TestSubscriberResubscribeCutsTheBackoffShort(t *testing.T) {
	client := &wakeNotifications{failures: 1}
	sub := New(client, "")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- sub.Run(ctx)
	}()

	deadline := time.After(500 * time.Millisecond)
	for client.Attempts() < 1 {
		select {
		case <-deadline:
			t.Fatal("expected the first subscribe attempt")
		case <-time.After(5 * time.Millisecond):
		}
	}
	// Now inside the one-second backoff the failure earned.
	sub.Resubscribe()

	select {
	case <-sub.Ready():
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("expected the stream opened at once, got %d attempts", client.Attempts())
	}
	if got := client.Attempts(); got != 2 {
		t.Fatalf("expected the retry to be the second attempt, got %d", got)
	}

	cancel()
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
