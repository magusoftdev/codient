package acpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestTransport_dispatchResponse(t *testing.T) {
	tr := NewTransport()
	id := 7
	ch := make(chan WireMsg, 1)
	tr.pending.Store(int32(id), ch)

	msg := WireMsg{JSONRPC: "2.0", ID: intPtr(7), Result: json.RawMessage(`{"x":1}`)}
	if !tr.dispatchResponse(msg) {
		t.Fatal("expected dispatch")
	}
	got := <-ch
	if string(got.Result) != `{"x":1}` {
		t.Fatalf("got %q", got.Result)
	}
}

func intPtr(i int) *int { return &i }

func TestTransport_writeRawJSONLine_rejectsEmbeddedNewline(t *testing.T) {
	tr := &Transport{out: &bytes.Buffer{}}
	err := tr.writeRawJSONLine([]byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.writeRawJSONLine([]byte("bad\n")); err == nil {
		t.Fatal("expected error")
	}
}

func TestTransport_ConsumeInbound_skipsChannel(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	tr := &Transport{in: bufio.NewReader(pr), out: &bytes.Buffer{}}
	out := make(chan WireMsg, 4)
	var cancelSeen int
	tr.ConsumeInbound = func(msg WireMsg) bool {
		if msg.Method == "session/cancel" {
			cancelSeen++
			return true
		}
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go tr.ReadLoop(ctx, out)

	go func() {
		_, _ = fmt.Fprintf(pw, `{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":"s1"}}`+"\n")
		_, _ = fmt.Fprintf(pw, `{"jsonrpc":"2.0","id":7,"method":"initialize","params":{"protocolVersion":1}}`+"\n")
		_ = pw.Close()
	}()

	got, ok := <-out
	if !ok {
		t.Fatal("channel closed before forwarded message")
	}
	if got.Method != "initialize" || got.ID == nil || *got.ID != 7 {
		t.Fatalf("expected initialize id=7 after consumed cancel, got method=%q id=%v", got.Method, got.ID)
	}
	if cancelSeen != 1 {
		t.Fatalf("expected one consumed cancel, got %d", cancelSeen)
	}
}
