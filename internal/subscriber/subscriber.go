package subscriber

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/agynio/agynd-cli/internal/platform"
	"github.com/google/uuid"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

const messageCreatedEvent = "message.created"

type Subscriber struct {
	client    notificationsClient
	threadID  string
	started   chan struct{}
	startOnce sync.Once
	ready     chan struct{}
	readyOnce sync.Once
	wake      chan struct{}
}

type notificationsClient interface {
	Subscribe(ctx context.Context) (platform.SubscribeStream, error)
}

func New(client notificationsClient, threadID string) *Subscriber {
	return &Subscriber{client: client, threadID: threadID, started: make(chan struct{}), ready: make(chan struct{}), wake: make(chan struct{}, 1)}
}

func (s *Subscriber) Run(ctx context.Context) error {
	s.startOnce.Do(func() { close(s.started) })
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stream, err := s.client.Subscribe(ctx)
		if err != nil {
			log.Printf("subscriber: subscribe failed: %v", err)
			if err := waitWithBackoff(ctx, backoff); err != nil {
				return err
			}
			backoff = nextBackoff(backoff)
			continue
		}
		s.readyOnce.Do(func() { close(s.ready) })
		backoff = time.Second

		for {
			resp, err := stream.Recv()
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if errors.Is(err, io.EOF) {
					log.Printf("subscriber: stream closed")
				} else {
					log.Printf("subscriber: stream recv failed: %v", err)
				}
				if err := waitWithBackoff(ctx, backoff); err != nil {
					return err
				}
				backoff = nextBackoff(backoff)
				break
			}
			envelope := resp.GetEnvelope()
			if envelope == nil {
				continue
			}
			if envelope.GetEvent() != messageCreatedEvent {
				continue
			}
			// An instance has no fixed thread and must wake for every one of
			// them; only a thread-scoped daemon filters.
			payloadThreadID, ok := payloadThreadID(envelope.GetPayload())
			if s.threadID != "" && (!ok || payloadThreadID != s.threadID) {
				continue
			}
			select {
			case s.wake <- struct{}{}:
			default:
			}
		}
	}
}

func (s *Subscriber) Started() <-chan struct{} {
	return s.started
}

func (s *Subscriber) Ready() <-chan struct{} {
	return s.ready
}

func (s *Subscriber) Wake() <-chan struct{} {
	return s.wake
}

func waitWithBackoff(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return time.Second
	}
	next := current * 2
	if next > 30*time.Second {
		return 30 * time.Second
	}
	return next
}

func payloadThreadID(payload *structpb.Struct) (string, bool) {
	if payload == nil {
		return "", false
	}
	fields := payload.GetFields()
	if fields == nil {
		return "", false
	}
	value, ok := fields["thread_id"]
	if !ok || value == nil {
		return "", false
	}
	raw := strings.TrimSpace(value.GetStringValue())
	if raw == "" {
		return "", false
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return "", false
	}
	return parsed.String(), true
}
