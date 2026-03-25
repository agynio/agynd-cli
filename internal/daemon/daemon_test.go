package daemon

import (
	"context"
	"strings"
	"testing"

	threadsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/threads/v1"
)

func TestHandleMessageDispatchesAgn(t *testing.T) {
	d := &Daemon{sdk: "agn"}
	message := &threadsv1.Message{Id: "msg-1"}
	err := d.handleMessage(context.Background(), message)
	if err == nil {
		t.Fatalf("expected error")
	}
	if strings.Contains(err.Error(), "unknown sdk") {
		t.Fatalf("expected agn branch error, got: %v", err)
	}
}
