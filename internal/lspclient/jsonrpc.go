// Package lspclient manages LSP (Language Server Protocol) client connections.
// It connects to configured language servers, manages their lifecycle, and
// dispatches LSP requests on behalf of agent tools.
package lspclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// jsonrpcRequest is a JSON-RPC 2.0 request message.
type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonrpcNotification is a JSON-RPC 2.0 notification (no id).
type jsonrpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonrpcResponse is a JSON-RPC 2.0 response message.
type jsonrpcResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      int64            `json:"id"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *jsonrpcError    `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *jsonrpcError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("LSP error %d: %s (%s)", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("LSP error %d: %s", e.Code, e.Message)
}

// conn wraps a Content-Length framed JSON-RPC 2.0 connection over stdio.
type conn struct {
	w      io.WriteCloser
	reader *bufio.Reader
	wmu    sync.Mutex
	nextID atomic.Int64

	// pending tracks in-flight requests by ID.
	pendingMu sync.Mutex
	pending   map[int64]chan jsonrpcResponse
	closed    chan struct{}
}

func newConn(r io.ReadCloser, w io.WriteCloser) *conn {
	c := &conn{
		w:       w,
		reader:  bufio.NewReaderSize(r, 64*1024),
		pending: make(map[int64]chan jsonrpcResponse),
		closed:  make(chan struct{}),
	}
	go c.readLoop(r)
	return c
}

// call sends a request and waits for the response.
func (c *conn) call(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
	ch := make(chan jsonrpcResponse, 1)

	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	req := jsonrpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	if err := c.write(req); err != nil {
		return fmt.Errorf("write %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return fmt.Errorf("connection closed")
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	}
}

// notify sends a notification (no response expected).
func (c *conn) notify(method string, params any) error {
	n := jsonrpcNotification{JSONRPC: "2.0", Method: method, Params: params}
	return c.write(n)
}

func (c *conn) write(msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))

	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := io.WriteString(c.w, header); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

func (c *conn) readLoop(r io.ReadCloser) {
	defer close(c.closed)
	defer r.Close()
	for {
		msg, err := c.readMessage()
		if err != nil {
			return
		}
		var resp jsonrpcResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			continue
		}
		if resp.ID == 0 && resp.Result == nil && resp.Error == nil {
			continue // notification from server; ignore
		}
		c.pendingMu.Lock()
		ch, ok := c.pending[resp.ID]
		c.pendingMu.Unlock()
		if ok {
			ch <- resp
		}
	}
}

func (c *conn) readMessage() ([]byte, error) {
	var contentLen int
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if strings.HasPrefix(line, "Content-Length:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length: %w", err)
			}
			contentLen = n
		}
	}
	if contentLen == 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	body := make([]byte, contentLen)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *conn) close() error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.w.Close()
}
