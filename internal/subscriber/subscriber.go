package subscriber

import (
	"context"
	"errors"
	"io"
	"log"
	"time"

	"github.com/agynio/agynd-cli/internal/platform"
)

const messageCreatedEvent = "message.created"

type Subscriber struct {
	client  *platform.Notifications
	agentID string
	wake    chan struct{}
}

func New(client *platform.Notifications, agentID string) *Subscriber {
	return &Subscriber{client: client, agentID: agentID, wake: make(chan struct{}, 1)}
}

func (s *Subscriber) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stream, err := s.client.Subscribe(ctx, s.agentID)
		if err != nil {
			log.Printf("subscriber: subscribe failed: %v", err)
			if err := waitWithBackoff(ctx, backoff); err != nil {
				return err
			}
			backoff = nextBackoff(backoff)
			continue
		}
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
			if envelope.GetEvent() == messageCreatedEvent {
				select {
				case s.wake <- struct{}{}:
				default:
				}
			}
		}
	}
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
