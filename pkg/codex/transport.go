package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"sync"
)

type jsonlTransport struct {
	reader *bufio.Reader
	writer *bufio.Writer
	mu     sync.Mutex
}

func newJSONLTransport(r io.Reader, w io.Writer) *jsonlTransport {
	return &jsonlTransport{
		reader: bufio.NewReader(r),
		writer: bufio.NewWriter(w),
	}
}

func (t *jsonlTransport) ReadMessage() ([]byte, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		if len(line) == 0 {
			return nil, err
		}
	}
	line = bytes.TrimSpace(line)
	return line, err
}

func (t *jsonlTransport) WriteMessage(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, err := t.writer.Write(data); err != nil {
		return err
	}
	if err := t.writer.WriteByte('\n'); err != nil {
		return err
	}
	return t.writer.Flush()
}
