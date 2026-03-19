package codexbridge

import (
	"encoding/json"
	"fmt"
	"log"

	codex "github.com/agynio/codex-sdk-go"
)

type Bridge struct {
	tracker *TurnTracker
}

func New(tracker *TurnTracker) *Bridge {
	return &Bridge{tracker: tracker}
}

func (b *Bridge) OnTurnStarted(*codex.TurnStartedNotification) {}

func (b *Bridge) OnTurnCompleted(notification *codex.TurnCompletedNotification) {
	if notification == nil {
		return
	}
	turnID := notification.Turn.ID
	result := TurnResult{
		ThreadID: notification.ThreadID,
		TurnID:   turnID,
	}
	message, err := extractFinalAnswer(notification.Turn)
	if err != nil {
		result.Err = err
	} else {
		result.Message = message
	}
	b.tracker.Notify(result)
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
