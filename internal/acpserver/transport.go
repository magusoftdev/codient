package acpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
)

const jsonRPCVersion = "2.0"

// WireMsg is a minimal superset for NDJSON lines on stdio.
type WireMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Transport implements NDJSON JSON-RPC on os.Stdin/os.Stdout for ACP stdio.
type Transport struct {
	in  *bufio.Reader
	out io.Writer
	log io.Writer

	mu       sync.Mutex
	nextID   atomic.Int32
	pending  sync.Map // int32 -> chan WireMsg
	readErr atomic.Value // stores error
}

// NewTransport returns an ACP stdio transport using os.Stdin/os.Stdout/os.Stderr.
func NewTransport() *Transport {
	return &Transport{
		in:  bufio.NewReader(os.Stdin),
		out: os.Stdout,
		log: os.Stderr,
	}
}

func (t *Transport) writeRawJSONLine(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	for _, c := range b {
		if c == '\n' || c == '\r' {
			return fmt.Errorf("acp: JSON line must not contain embedded newlines")
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, err := t.out.Write(b); err != nil {
		return err
	}
	_, err := t.out.Write([]byte{'\n'})
	return err
}

func (t *Transport) writeEnvelope(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return t.writeRawJSONLine(b)
}

func (t *Transport) WriteResult(id int, result any) error {
	return t.writeEnvelope(map[string]any{
		"jsonrpc": jsonRPCVersion,
		"id":      id,
		"result":  result,
	})
}

func (t *Transport) WriteError(id int, code int, message string) error {
	return t.writeEnvelope(map[string]any{
		"jsonrpc": jsonRPCVersion,
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (t *Transport) SendNotification(method string, params any) error {
	return t.writeEnvelope(map[string]any{
		"jsonrpc": jsonRPCVersion,
		"method":  method,
		"params":  params,
	})
}

// callClient sends a JSON-RPC request to the client (editor) and waits for the matching response id.
func (t *Transport) CallClient(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := int(t.nextID.Add(1))
	ch := make(chan WireMsg, 1)
	t.pending.Store(int32(id), ch)
	defer t.pending.Delete(int32(id))

	req := map[string]any{
		"jsonrpc": jsonRPCVersion,
		"id":      id,
		"method":  method,
		"params":  params,
	}
	if err := t.writeEnvelope(req); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg := <-ch:
		if msg.Error != nil {
			return nil, fmt.Errorf("json-rpc error %d: %s", msg.Error.Code, msg.Error.Message)
		}
		return msg.Result, nil
	}
}

func (t *Transport) dispatchResponse(msg WireMsg) bool {
	if msg.ID == nil {
		return false
	}
	if msg.Result == nil && msg.Error == nil {
		return false
	}
	v, ok := t.pending.Load(int32(*msg.ID))
	if !ok {
		return false
	}
	ch := v.(chan WireMsg)
	select {
	case ch <- msg:
	default:
	}
	return true
}

func (t *Transport) ReadLoop(ctx context.Context, out chan<- WireMsg) {
	defer close(out)
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		line, err := t.in.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if err != nil {
			if err != io.EOF {
				t.readErr.Store(err)
			}
			return
		}
		if len(line) == 0 {
			continue
		}
		var msg WireMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if t.dispatchResponse(msg) {
			continue
		}
		select {
		case out <- msg:
		case <-ctx.Done():
			return
		}
	}
}
