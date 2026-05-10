package platform

import (
	"context"

	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	notificationsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/notifications/v1"
)

const threadParticipantSelfRoom = "thread_participant:me"

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
	request := &notificationsv1.SubscribeRequest{
		Rooms: []string{threadParticipantSelfRoom},
	}
	return n.client.Subscribe(ctx, request)
}
