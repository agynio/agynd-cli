package codexbridge

import (
	"context"
	"encoding/json"
	"log"
	"time"

	threadsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/threads/v1"
	"github.com/agynio/agynd-cli/pkg/codex"
)

type Bridge struct {
	ctx     context.Context
	threads threadsv1.ThreadsServiceClient
	agentID string
	mapping *ThreadMapping
}

func New(ctx context.Context, threads threadsv1.ThreadsServiceClient, agentID string, mapping *ThreadMapping) *Bridge {
	return &Bridge{ctx: ctx, threads: threads, agentID: agentID, mapping: mapping}
}

func (b *Bridge) OnTurnStarted(*codex.TurnStartedNotification) {}

func (b *Bridge) OnTurnCompleted(notification *codex.TurnCompletedNotification) {
	if notification == nil {
		return
	}
	threadID := notification.ThreadID
	platformThreadID, ok := b.mapping.PlatformForCodex(threadID)
	if !ok {
		return
	}
	message, ok := extractFinalAnswer(notification.Turn)
	if !ok || message == "" {
		return
	}
	ctx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	defer cancel()
	_, err := b.threads.SendMessage(ctx, &threadsv1.SendMessageRequest{
		ThreadId: platformThreadID,
		SenderId: b.agentID,
		Body:     message,
	})
	if err != nil {
		log.Printf("codex bridge: send message failed: %v", err)
	}
}

func (b *Bridge) OnItemStarted(*codex.ItemStartedNotification) {}

func (b *Bridge) OnItemCompleted(*codex.ItemCompletedNotification) {}

func (b *Bridge) OnAgentMessageDelta(*codex.AgentMessageDeltaNotification) {}

func (b *Bridge) OnCommandOutputDelta(*codex.CommandExecutionOutputDeltaNotification) {}

func (b *Bridge) OnFileChangeDelta(*codex.FileChangeOutputDeltaNotification) {}

func (b *Bridge) OnTokenUsageUpdated(*codex.ThreadTokenUsageUpdatedNotification) {}

func (b *Bridge) OnError(notification *codex.ErrorNotification) {
	if notification == nil {
		return
	}
	log.Printf("codex bridge: error notification: %s", notification.Error.Message)
}

func (b *Bridge) OnNotification(string, json.RawMessage) {}

func extractFinalAnswer(turn codex.Turn) (string, bool) {
	for _, item := range turn.Items {
		if item.AgentMessage == nil {
			continue
		}
		if item.AgentMessage.Phase != nil && *item.AgentMessage.Phase == codex.MessagePhaseFinalAnswer {
			return item.AgentMessage.Text, true
		}
	}
	for i := len(turn.Items) - 1; i >= 0; i-- {
		item := turn.Items[i]
		if item.AgentMessage != nil {
			return item.AgentMessage.Text, true
		}
	}
	return "", false
}
