package platform

import (
	"context"
	"fmt"

	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	notificationsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/notifications/v1"
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

func (n *Notifications) Subscribe(ctx context.Context, agentID string) (SubscribeStream, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	// NOTE: SubscribeRequest currently has no rooms field; server-side filtering
	// must ensure the agent only receives thread_participant:{agentID} events.
	return n.client.Subscribe(ctx, &notificationsv1.SubscribeRequest{})
}
