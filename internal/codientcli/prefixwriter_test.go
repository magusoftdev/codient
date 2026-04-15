package codientcli

import (
	"bytes"
	"testing"
)

func TestPrefixWriter(t *testing.T) {
	var buf bytes.Buffer
	pw := newPrefixWriter([]byte("  | "), &buf)

	pw.Write([]byte("line1\nline2\n"))
	pw.Write([]byte("line3"))

	got := buf.String()
	want := "  | line1\n  | line2\n  | line3"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrefixWriter_SingleLine(t *testing.T) {
	var buf bytes.Buffer
	pw := newPrefixWriter([]byte("> "), &buf)

	pw.Write([]byte("hello"))
	got := buf.String()
	want := "> hello"
	if got != want {
		t.Fatalf("got: %q want: %q", got, want)
	}
}

func TestPrefixWriter_Empty(t *testing.T) {
	var buf bytes.Buffer
	pw := newPrefixWriter([]byte("| "), &buf)

	n, err := pw.Write(nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 bytes written, got %d", n)
	}
}

func TestPrefixWriter_NewlineOnly(t *testing.T) {
	var buf bytes.Buffer
	pw := newPrefixWriter([]byte("| "), &buf)

	pw.Write([]byte("\n"))
	got := buf.String()
	want := "| \n"
	if got != want {
		t.Fatalf("got: %q want: %q", got, want)
	}
}

func TestPrefixWriter_MultipleNewlines(t *testing.T) {
	var buf bytes.Buffer
	pw := newPrefixWriter([]byte("- "), &buf)

	pw.Write([]byte("\n\n\n"))
	got := buf.String()
	want := "- \n- \n- \n"
	if got != want {
		t.Fatalf("got: %q want: %q", got, want)
	}
}

func TestPrefixWriter_ByteCount(t *testing.T) {
	var buf bytes.Buffer
	pw := newPrefixWriter([]byte(">> "), &buf)

	input := []byte("hello\nworld\n")
	n, err := pw.Write(input)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(input) {
		t.Fatalf("expected %d bytes reported, got %d", len(input), n)
	}
}

func TestPrefixWriter_InterleaveWrites(t *testing.T) {
	var buf bytes.Buffer
	pw := newPrefixWriter([]byte("  │ "), &buf)

	pw.Write([]byte("a"))
	pw.Write([]byte("b\n"))
	pw.Write([]byte("c\nd"))

	got := buf.String()
	want := "  │ ab\n  │ c\n  │ d"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
