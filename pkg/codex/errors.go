package codex

import (
	"encoding/json"
	"fmt"
)

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return "rpc error"
	}
	if len(e.Data) == 0 {
		return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("rpc error %d: %s (%s)", e.Code, e.Message, string(e.Data))
}

type ProcessError struct {
	ExitCode int
	Stderr   string
}

func (e *ProcessError) Error() string {
	if e == nil {
		return "process error"
	}
	if e.Stderr == "" {
		return fmt.Sprintf("process exited with code %d", e.ExitCode)
	}
	return fmt.Sprintf("process exited with code %d: %s", e.ExitCode, e.Stderr)
}
