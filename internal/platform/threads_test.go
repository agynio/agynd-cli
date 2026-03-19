package platform

import (
	"reflect"
	"testing"
	"time"

	threadsv1 "github.com/agynio/agynd-cli/.gen/go/agynio/api/threads/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestMessageFromProtoValid(t *testing.T) {
	createdAt := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	proto := &threadsv1.Message{
		Id:        "msg-1",
		ThreadId:  "thread-1",
		SenderId:  "sender-1",
		Body:      "hello",
		FileIds:   []string{"file-a"},
		CreatedAt: timestamppb.New(createdAt),
	}
	message, err := messageFromProto(proto)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if message.ID != "msg-1" {
		t.Fatalf("unexpected id: %s", message.ID)
	}
	if message.ThreadID != "thread-1" {
		t.Fatalf("unexpected thread id: %s", message.ThreadID)
	}
	if message.SenderID != "sender-1" {
		t.Fatalf("unexpected sender id: %s", message.SenderID)
	}
	if message.Body != "hello" {
		t.Fatalf("unexpected body: %s", message.Body)
	}
	if !reflect.DeepEqual(message.FileIDs, []string{"file-a"}) {
		t.Fatalf("unexpected file ids: %v", message.FileIDs)
	}
	if !message.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected created time: %s", message.CreatedAt)
	}
}

func TestMessageFromProtoNil(t *testing.T) {
	_, err := messageFromProto(nil)
	if err == nil {
		t.Fatal("expected error for nil message")
	}
}

func TestMessageFromProtoMissingFields(t *testing.T) {
	createdAt := timestamppb.New(time.Now())
	valid := &threadsv1.Message{
		Id:        "msg-1",
		ThreadId:  "thread-1",
		SenderId:  "sender-1",
		Body:      "hello",
		CreatedAt: createdAt,
	}
	tests := []struct {
		name string
		msg  *threadsv1.Message
	}{
		{
			name: "missing-id",
			msg: &threadsv1.Message{
				ThreadId:  valid.ThreadId,
				SenderId:  valid.SenderId,
				Body:      valid.Body,
				CreatedAt: valid.CreatedAt,
			},
		},
		{
			name: "missing-thread-id",
			msg: &threadsv1.Message{
				Id:        valid.Id,
				SenderId:  valid.SenderId,
				Body:      valid.Body,
				CreatedAt: valid.CreatedAt,
			},
		},
		{
			name: "missing-sender-id",
			msg: &threadsv1.Message{
				Id:        valid.Id,
				ThreadId:  valid.ThreadId,
				Body:      valid.Body,
				CreatedAt: valid.CreatedAt,
			},
		},
		{
			name: "missing-created-at",
			msg: &threadsv1.Message{
				Id:       valid.Id,
				ThreadId: valid.ThreadId,
				SenderId: valid.SenderId,
				Body:     valid.Body,
			},
		},
		{
			name: "missing-body-and-files",
			msg: &threadsv1.Message{
				Id:        valid.Id,
				ThreadId:  valid.ThreadId,
				SenderId:  valid.SenderId,
				CreatedAt: valid.CreatedAt,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := messageFromProto(test.msg)
			if err == nil {
				t.Fatal("expected error for missing field")
			}
		})
	}
}
