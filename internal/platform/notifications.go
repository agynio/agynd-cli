package platform

import (
	"context"
	"fmt"

	notificationsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/notifications/v1"
	"google.golang.org/grpc"
)

type SubscribeStream interface {
	Recv() (*notificationsv1.SubscribeResponse, error)
}

type Notifications struct {
	client notificationsv1.NotificationsServiceClient
}

func NewNotifications(conn *grpc.ClientConn) *Notifications {
	return &Notifications{client: notificationsv1.NewNotificationsServiceClient(conn)}
}

func (n *Notifications) Subscribe(ctx context.Context, agentID string) (SubscribeStream, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	// NOTE: SubscribeRequest currently has no rooms field; server-side filtering
	// must ensure the agent only receives thread_participant:{agentID} events.
	return n.client.Subscribe(ctx, &notificationsv1.SubscribeRequest{})
}
