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
	dropMu    sync.Mutex
	drop      context.CancelFunc
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
		// A context per attempt, so Resubscribe can end the stream it carries,
		// or the wait before the next one, without ending Run.
		attemptCtx, endAttempt := context.WithCancel(ctx)
		s.setDrop(endAttempt)
		stream, err := s.client.Subscribe(attemptCtx)
		if err != nil {
			if attemptCtx.Err() == nil {
				log.Printf("subscriber: subscribe failed: %v", err)
			}
			backoff, err = s.waitOut(ctx, attemptCtx, backoff)
			endAttempt()
			if err != nil {
				return err
			}
			continue
		}
		s.readyOnce.Do(func() { close(s.ready) })
		backoff = time.Second

		for {
			resp, err := stream.Recv()
			if err != nil {
				if ctx.Err() != nil {
					endAttempt()
					return ctx.Err()
				}
				if attemptCtx.Err() != nil {
					// The daemon's own doing, not a failure: reopen without
					// the backoff a failure earns.
					log.Printf("subscriber: stream dropped; resubscribing")
					endAttempt()
					break
				}
				if errors.Is(err, io.EOF) {
					log.Printf("subscriber: stream closed")
				} else {
					log.Printf("subscriber: stream recv failed: %v", err)
				}
				backoff, err = s.waitOut(ctx, attemptCtx, backoff)
				endAttempt()
				if err != nil {
					return err
				}
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

// waitOut sleeps out a failure's backoff under the attempt's context and
// returns the backoff the next failure gets: doubled, or back at the start when
// Resubscribe cut the wait short, because a wake is not another failure. The
// error is ctx's own, once Run itself is done.
func (s *Subscriber) waitOut(ctx, attemptCtx context.Context, backoff time.Duration) (time.Duration, error) {
	err := waitWithBackoff(attemptCtx, backoff)
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if err != nil {
		log.Printf("subscriber: resubscribing")
		return time.Second, nil
	}
	return nextBackoff(backoff), nil
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

// Resubscribe ends the open stream so Run reopens it, or ends the wait after
// a failure so Run opens one now. After a wake from hibernation the far side
// of the stream is gone, and Recv would otherwise wait on it until TCP gave
// up.
func (s *Subscriber) Resubscribe() {
	s.dropMu.Lock()
	defer s.dropMu.Unlock()
	if s.drop != nil {
		s.drop()
	}
}

func (s *Subscriber) setDrop(drop context.CancelFunc) {
	s.dropMu.Lock()
	defer s.dropMu.Unlock()
	s.drop = drop
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
