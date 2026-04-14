package codientcli

import (
	"bytes"
	"sync"
	"testing"
)

func TestFirstWriteStop(t *testing.T) {
	var buf bytes.Buffer
	var n int
	var once sync.Once
	stop := func() {
		once.Do(func() { n++ })
	}
	w := &firstWriteStop{w: &buf, stop: stop}
	if _, err := w.Write([]byte{}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("empty write should not stop: n=%d", n)
	}
	if _, err := w.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("first non-empty write should stop once: n=%d", n)
	}
	if buf.String() != "hi" {
		t.Fatalf("forwarded: %q", buf.String())
	}
	if _, err := w.Write([]byte("!")); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("stop should run once: n=%d", n)
	}
	if buf.String() != "hi!" {
		t.Fatalf("second write: %q", buf.String())
	}
}
