package acpserver

import (
	"bytes"
	"encoding/json"
	"testing"
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
