package codexbridge

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	codex "github.com/agynio/codex-sdk-go"
)

type Bridge struct {
	tracker *TurnTracker
	mu      sync.Mutex
	items   map[string][]codex.ThreadItem
}

func New(tracker *TurnTracker) *Bridge {
	return &Bridge{
		tracker: tracker,
		items:   make(map[string][]codex.ThreadItem),
	}
}

func (b *Bridge) OnTurnStarted(*codex.TurnStartedNotification) {}

func (b *Bridge) OnTurnCompleted(notification *codex.TurnCompletedNotification) {
	if notification == nil {
		return
	}
	turnID := notification.Turn.ID

	b.mu.Lock()
	accumulated := b.items[turnID]
	delete(b.items, turnID)
	b.mu.Unlock()

	turn := notification.Turn
	if len(turn.Items) == 0 && len(accumulated) > 0 {
		turn.Items = accumulated
	}

	result := TurnResult{
		ThreadID: notification.ThreadID,
		TurnID:   turnID,
	}
	message, err := extractFinalAnswer(turn)
	if err != nil {
		result.Err = err
	} else {
		result.Message = message
	}
	b.tracker.Notify(result)
}

func (b *Bridge) OnItemStarted(*codex.ItemStartedNotification) {}

func (b *Bridge) OnItemCompleted(notification *codex.ItemCompletedNotification) {
	if notification == nil {
		return
	}
	b.mu.Lock()
	b.items[notification.TurnID] = append(b.items[notification.TurnID], notification.Item)
	b.mu.Unlock()
}

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

func extractFinalAnswer(turn codex.Turn) (string, error) {
	for _, item := range turn.Items {
		if item.AgentMessage == nil {
			continue
		}
		if item.AgentMessage.Phase != nil && *item.AgentMessage.Phase == codex.MessagePhaseFinalAnswer {
			return item.AgentMessage.Text, nil
		}
	}
	for i := len(turn.Items) - 1; i >= 0; i-- {
		item := turn.Items[i]
		if item.AgentMessage != nil {
			return item.AgentMessage.Text, nil
		}
	}
	return "", fmt.Errorf("turn %s missing agent message", turn.ID)
}
