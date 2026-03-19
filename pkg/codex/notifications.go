package codex

import "encoding/json"

type NotificationHandler interface {
	OnTurnStarted(*TurnStartedNotification)
	OnTurnCompleted(*TurnCompletedNotification)
	OnItemStarted(*ItemStartedNotification)
	OnItemCompleted(*ItemCompletedNotification)
	OnAgentMessageDelta(*AgentMessageDeltaNotification)
	OnCommandOutputDelta(*CommandExecutionOutputDeltaNotification)
	OnFileChangeDelta(*FileChangeOutputDeltaNotification)
	OnTokenUsageUpdated(*ThreadTokenUsageUpdatedNotification)
	OnError(*ErrorNotification)
	OnNotification(method string, raw json.RawMessage)
}

type NopNotificationHandler struct{}

func (NopNotificationHandler) OnTurnStarted(*TurnStartedNotification)             {}
func (NopNotificationHandler) OnTurnCompleted(*TurnCompletedNotification)         {}
func (NopNotificationHandler) OnItemStarted(*ItemStartedNotification)             {}
func (NopNotificationHandler) OnItemCompleted(*ItemCompletedNotification)         {}
func (NopNotificationHandler) OnAgentMessageDelta(*AgentMessageDeltaNotification) {}
func (NopNotificationHandler) OnCommandOutputDelta(*CommandExecutionOutputDeltaNotification) {
}
func (NopNotificationHandler) OnFileChangeDelta(*FileChangeOutputDeltaNotification)     {}
func (NopNotificationHandler) OnTokenUsageUpdated(*ThreadTokenUsageUpdatedNotification) {}
func (NopNotificationHandler) OnError(*ErrorNotification)                               {}
func (NopNotificationHandler) OnNotification(string, json.RawMessage)                   {}
