package platform

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type Consumer struct {
	threads        *Threads
	pageSize       int32
	requestTimeout time.Duration
}

func NewConsumer(threads *Threads, pageSize int32, requestTimeout time.Duration) *Consumer {
	return &Consumer{threads: threads, pageSize: pageSize, requestTimeout: requestTimeout}
}

func (c *Consumer) Sync(ctx context.Context, participantID string, handle func(Message) error) error {
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
		messages, nextToken, err := c.threads.GetUnackedMessages(pageCtx, participantID, c.pageSize, pageToken)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			return err
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
