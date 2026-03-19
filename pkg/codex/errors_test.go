package codex

import (
	"encoding/json"
	"testing"
)

func TestRPCError(t *testing.T) {
	err := (&RPCError{Code: -32000, Message: "bad"}).Error()
	if err != "rpc error -32000: bad" {
		t.Fatalf("unexpected error string: %s", err)
	}
	err = (&RPCError{Code: 1, Message: "bad", Data: json.RawMessage(`{"detail":"oops"}`)}).Error()
	if err != "rpc error 1: bad ({\"detail\":\"oops\"})" {
		t.Fatalf("unexpected error string: %s", err)
	}
}

func TestProcessError(t *testing.T) {
	err := (&ProcessError{ExitCode: 2}).Error()
	if err != "process exited with code 2" {
		t.Fatalf("unexpected error string: %s", err)
	}
	err = (&ProcessError{ExitCode: 3, Stderr: "boom"}).Error()
	if err != "process exited with code 3: boom" {
		t.Fatalf("unexpected error string: %s", err)
	}
}
