package platform

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type Consumer struct {
	threads        *Threads
	agents         agentsInboxClient
	pageSize       int32
	requestTimeout time.Duration
}

// agentsInboxClient is the instance-inbox half of the gateway's agents API.
// Naming it here keeps the consumer independent of how the client is built.
type agentsInboxClient interface {
	GetUnackedInboxItems(ctx context.Context, agentInstanceID string, pageSize int32, pageToken string) ([]Message, string, error)
}

type PageFetchError struct {
	Err error
}

func (e *PageFetchError) Error() string {
	return e.Err.Error()
}

func (e *PageFetchError) Unwrap() error {
	return e.Err
}

func NewConsumer(threads *Threads, pageSize int32, requestTimeout time.Duration) *Consumer {
	return &Consumer{threads: threads, pageSize: pageSize, requestTimeout: requestTimeout}
}

// NewInboxConsumer reads an agent instance's inbox instead of a thread
// participant's unacked messages. The participant id passed to Sync is then the
// instance id, and the thread id is ignored: an inbox spans threads.
func NewInboxConsumer(agents agentsInboxClient, pageSize int32, requestTimeout time.Duration) *Consumer {
	return &Consumer{agents: agents, pageSize: pageSize, requestTimeout: requestTimeout}
}

func (c *Consumer) Sync(ctx context.Context, participantID string, threadID string, handle func(Message) error) error {
	if handle == nil {
		return fmt.Errorf("handle function is required")
	}
	pageToken := ""
	for {
		pageCtx := ctx
		var cancel context.CancelFunc
		if c.requestTimeout > 0 {
			pageCtx, cancel = context.WithTimeout(ctx, c.requestTimeout)
		}
		messages, nextToken, err := c.fetchPage(pageCtx, participantID, threadID, pageToken)
		if err != nil && pageCtx.Err() != nil {
			err = pageCtx.Err()
		}
		if cancel != nil {
			cancel()
		}
		if err != nil {
			return &PageFetchError{Err: err}
		}
		if len(messages) > 1 {
			sort.Slice(messages, func(i, j int) bool {
				if messages[i].CreatedAt.Equal(messages[j].CreatedAt) {
					return messages[i].ID < messages[j].ID
				}
				return messages[i].CreatedAt.Before(messages[j].CreatedAt)
			})
		}
		for _, message := range messages {
			if err := handle(message); err != nil {
				return err
			}
		}
		if nextToken == "" {
			return nil
		}
		pageToken = nextToken
	}
}

func (c *Consumer) fetchPage(ctx context.Context, participantID string, threadID string, pageToken string) ([]Message, string, error) {
	if c.agents != nil {
		return c.agents.GetUnackedInboxItems(ctx, participantID, c.pageSize, pageToken)
	}
	return c.threads.GetUnackedMessages(ctx, participantID, threadID, c.pageSize, pageToken)
}
