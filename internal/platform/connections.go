package platform

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
)

type Connections struct {
	Threads       *grpc.ClientConn
	Notifications *grpc.ClientConn
	Teams         *grpc.ClientConn
}

func DialConnections(ctx context.Context, threadsAddr, notificationsAddr, teamsAddr string) (*Connections, error) {
	threadsConn, err := Dial(ctx, threadsAddr)
	if err != nil {
		return nil, fmt.Errorf("dial threads: %w", err)
	}
	notificationsConn, err := Dial(ctx, notificationsAddr)
	if err != nil {
		_ = threadsConn.Close()
		return nil, fmt.Errorf("dial notifications: %w", err)
	}
	teamsConn, err := Dial(ctx, teamsAddr)
	if err != nil {
		_ = threadsConn.Close()
		_ = notificationsConn.Close()
		return nil, fmt.Errorf("dial teams: %w", err)
	}
	return &Connections{
		Threads:       threadsConn,
		Notifications: notificationsConn,
		Teams:         teamsConn,
	}, nil
}

func (c *Connections) Close() {
	if c == nil {
		return
	}
	if c.Threads != nil {
		_ = c.Threads.Close()
	}
	if c.Notifications != nil {
		_ = c.Notifications.Close()
	}
	if c.Teams != nil {
		_ = c.Teams.Close()
	}
}
