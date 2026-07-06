package codexbridge

import (
	"encoding/json"
	"errors"
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

var ErrMissingAgentMessage = errors.New("missing agent message")

type ErrorNotificationError struct {
	ThreadID  string
	TurnID    string
	Message   string
	WillRetry bool
	Details   string
}

func (e *ErrorNotificationError) Error() string {
	base := fmt.Sprintf("codex error notification for turn %s on thread %s: %s", e.TurnID, e.ThreadID, e.Message)
	if e.Details == "" {
		return base
	}
	return base + ": " + e.Details
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
	message, err := ExtractFinalAnswer(turn)
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
	details := ""
	if notification.Error.AdditionalDetails != nil {
		details = *notification.Error.AdditionalDetails
	}
	log.Printf(
		"codex bridge: error notification: thread_id=%s turn_id=%s will_retry=%t message=%s details=%s",
		notification.ThreadID,
		notification.TurnID,
		notification.WillRetry,
		notification.Error.Message,
		details,
	)
	if notification.WillRetry || notification.TurnID == "" {
		return
	}
	b.tracker.Notify(TurnResult{
		ThreadID: notification.ThreadID,
		TurnID:   notification.TurnID,
		Err: &ErrorNotificationError{
			ThreadID:  notification.ThreadID,
			TurnID:    notification.TurnID,
			Message:   notification.Error.Message,
			WillRetry: notification.WillRetry,
			Details:   details,
		},
	})
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

func ExtractFinalAnswer(turn codex.Turn) (string, error) {
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
	return "", fmt.Errorf("turn %s: %w", turn.ID, ErrMissingAgentMessage)
}
