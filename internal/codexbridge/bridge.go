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
	items   map[string]*turnItems
}

type turnItems struct {
	items         []codex.ThreadItem
	agentMessages map[string]string
}

func New(tracker *TurnTracker) *Bridge {
	return &Bridge{
		tracker: tracker,
		items:   make(map[string]*turnItems),
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
	if accumulated != nil {
		turn.Items = mergeTurnItems(turn.Items, accumulated)
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
	items := b.itemsForTurn(notification.TurnID)
	items.items = append(items.items, notification.Item)
	b.mu.Unlock()
}

func (b *Bridge) OnAgentMessageDelta(notification *codex.AgentMessageDeltaNotification) {
	if notification == nil || notification.TurnID == "" || notification.ItemID == "" || notification.Delta == "" {
		return
	}
	b.mu.Lock()
	items := b.itemsForTurn(notification.TurnID)
	items.agentMessages[notification.ItemID] += notification.Delta
	b.mu.Unlock()
}

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

func (b *Bridge) itemsForTurn(turnID string) *turnItems {
	items := b.items[turnID]
	if items == nil {
		items = &turnItems{agentMessages: make(map[string]string)}
		b.items[turnID] = items
	}
	return items
}

func mergeTurnItems(items []codex.ThreadItem, accumulated *turnItems) []codex.ThreadItem {
	if len(items) == 0 && len(accumulated.items) > 0 {
		items = accumulated.items
	}
	if len(accumulated.agentMessages) == 0 {
		return items
	}
	merged := append([]codex.ThreadItem(nil), items...)
	seen := make(map[string]struct{}, len(merged))
	for _, item := range merged {
		if item.AgentMessage != nil && item.AgentMessage.ID != "" {
			seen[item.AgentMessage.ID] = struct{}{}
		}
	}
	for itemID, text := range accumulated.agentMessages {
		if _, ok := seen[itemID]; ok {
			continue
		}
		merged = append(merged, codex.ThreadItem{
			AgentMessage: &codex.AgentMessageThreadItem{
				ID:   itemID,
				Type: codex.ThreadItemTypeAgentMessage,
				Text: text,
			},
		})
	}
	return merged
}

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
