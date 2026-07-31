package platform

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	gatewayv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/gateway/v1"
	notificationsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/notifications/v1"
	"google.golang.org/grpc"
)

type fakeNotificationsGatewayClient struct {
	request *notificationsv1.SubscribeRequest
	err     error
}

var _ gatewayv1.NotificationsGatewayClient = (*fakeNotificationsGatewayClient)(nil)

func (f *fakeNotificationsGatewayClient) Subscribe(ctx context.Context, in *notificationsv1.SubscribeRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[notificationsv1.SubscribeResponse], error) {
	f.request = in
	if f.err != nil {
		return nil, f.err
	}
	return &fakeSubscribeStream{}, nil
}

type fakeSubscribeStream struct {
	grpc.ClientStream
}

func (f *fakeSubscribeStream) Recv() (*notificationsv1.SubscribeResponse, error) {
	return nil, fmt.Errorf("Recv not implemented")
}

func TestNotificationsSubscribe(t *testing.T) {
	fake := &fakeNotificationsGatewayClient{}
	client := NewNotifications(fake)
	stream, err := client.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if stream == nil {
		t.Fatal("expected stream")
	}
	want := []string{threadParticipantSelfRoom, agentInstanceSelfRoom}
	if fake.request == nil || !reflect.DeepEqual(fake.request.GetRooms(), want) {
		t.Fatalf("expected rooms %v, got %+v", want, fake.request)
	}
}

func TestNotificationsSubscribeWrapsError(t *testing.T) {
	fake := &fakeNotificationsGatewayClient{err: fmt.Errorf("rpc failed")}
	client := NewNotifications(fake)
	_, err := client.Subscribe(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "subscribe notifications") {
		t.Fatalf("missing operation context: %v", err)
	}
}
