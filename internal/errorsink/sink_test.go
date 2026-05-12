package errorsink

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
)

func TestDisabled(t *testing.T) {
	t.Setenv(EnvDisable, "0")
	if !Disabled() {
		t.Fatal("expected disabled")
	}
	t.Setenv(EnvDisable, "")
	if Disabled() {
		t.Fatal("expected not disabled when unset")
	}
}

func TestSink_OpenAndLogPanic(t *testing.T) {
	dir := t.TempDir()
	s, path, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if path == "" || !strings.HasPrefix(filepath.Base(path), "errors-") {
		t.Fatalf("unexpected path %q", path)
	}
	s.SetSessionID("sess-1")
	stack := debug.Stack()
	s.LogPanic("test panic", stack)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "panic: test panic") {
		t.Fatalf("missing panic line: %q", text)
	}
	if !strings.Contains(text, "[session_id=sess-1]") {
		t.Fatalf("missing session prefix: %q", text)
	}
	if !strings.Contains(text, "TestSink_OpenAndLogPanic") {
		t.Fatalf("expected stack trace substring: %q", text)
	}
}

func TestSink_ConcurrentLogf(t *testing.T) {
	dir := t.TempDir()
	s, _, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Logf("msg-%d", i)
		}(i)
	}
	wg.Wait()
}

func TestSink_NilNoOp(t *testing.T) {
	var s *Sink
	s.Logf("x")
	s.LogError("c", os.ErrInvalid)
	s.LogPanic("p", nil)
	s.SetSessionID("x")
	_ = s.Close()
}
