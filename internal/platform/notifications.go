package platform

import (
	"context"
	"fmt"

	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	notificationsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/notifications/v1"
)

const (
	threadParticipantSelfRoom = "thread_participant:me"
	agentInstanceSelfRoom     = "agent_instance:me"
)

type SubscribeStream interface {
	Recv() (*notificationsv1.SubscribeResponse, error)
}

type Notifications struct {
	client gatewayv1.NotificationsGatewayClient
}

func NewNotifications(client gatewayv1.NotificationsGatewayClient) *Notifications {
	return &Notifications{client: client}
}

func (n *Notifications) Subscribe(ctx context.Context) (SubscribeStream, error) {
	// Both rooms: the identity is an agent instance and gets woken through its
	// inbox, but it is still a participant in the threads it was handed.
	request := &notificationsv1.SubscribeRequest{
		Rooms: []string{threadParticipantSelfRoom, agentInstanceSelfRoom},
	}
	stream, err := n.client.Subscribe(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("subscribe notifications: %w", err)
	}
	return stream, nil
}
