package codientcli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"codient/internal/prompt"
)

func TestResolvePrompt_FromFlag(t *testing.T) {
	s, err := resolvePrompt("  hello  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(s) != "hello" {
		t.Fatalf("got %q", s)
	}
}

func TestResolvePrompt_EmptyFlag(t *testing.T) {
	s, err := resolvePrompt("")
	if err != nil {
		t.Fatal(err)
	}
	if s != "" {
		t.Fatalf("expected empty when no stdin pipe; got %q (TTY?)", s)
	}
}

func TestResolveProgressOut_Flag(t *testing.T) {
	if w := resolveProgressOut(true, false); w != os.Stderr {
		t.Fatalf("progress flag: got %v want stderr", w)
	}
	if w := resolveProgressOut(false, true); w != os.Stderr {
		t.Fatalf("log requested: got %v want stderr", w)
	}
}

func TestStreamWriterForTurn_PlanRichOnlyAfterBlockingQuestion(t *testing.T) {
	waiting := "Q?\n\n**Waiting for your answer**"
	if w := streamWriterForTurn(true, true, prompt.ModePlan, true, waiting); w != nil {
		t.Fatalf("plan+rich after blocking question: expected buffered glamour, got %v", w)
	}
	if w := streamWriterForTurn(true, true, prompt.ModePlan, true, "Ready to implement."); w != nil {
		t.Fatal("plan+rich follow-up: expected no stdout streaming (glamour at end)")
	}
	if w := streamWriterForTurn(true, true, prompt.ModePlan, false, waiting); w == nil {
		t.Fatal("plan+plain after question: expected streaming")
	}
	if w := streamWriterForTurn(true, true, prompt.ModeBuild, true, waiting); w != nil {
		t.Fatal("build+rich: expected no stdout streaming (glamour at end)")
	}
	if w := streamWriterForTurn(true, true, prompt.ModeBuild, false, waiting); w == nil {
		t.Fatal("build+plain: expected streaming")
	}
}

func TestWritePlanDraftPreamble_AfterBlockingQuestion(t *testing.T) {
	var buf bytes.Buffer
	writePlanDraftPreamble(&buf, prompt.ModeBuild, "x **Waiting for your answer**")
	if buf.Len() != 0 {
		t.Fatalf("non-plan: expected no preamble, got %q", buf.String())
	}
	buf.Reset()
	writePlanDraftPreamble(&buf, prompt.ModePlan, "no wait")
	if buf.Len() != 0 {
		t.Fatalf("no wait phrase: expected empty, got %q", buf.String())
	}
	buf.Reset()
	writePlanDraftPreamble(&buf, prompt.ModePlan, "Q?\n\n**Waiting for your answer**")
	s := buf.String()
	if !strings.Contains(s, "Building the implementation plan") {
		t.Fatalf("expected status line: %q", s)
	}
}

func TestResolveProgressOut_StderrTTYFallback(t *testing.T) {
	st, err := os.Stderr.Stat()
	if err != nil {
		t.Fatal(err)
	}
	isTTY := (st.Mode() & os.ModeCharDevice) != 0
	got := resolveProgressOut(false, false)
	if isTTY && got != os.Stderr {
		t.Fatalf("interactive stderr: got %v want stderr", got)
	}
	if !isTTY && got != nil {
		t.Fatalf("non-interactive stderr: got %v want nil", got)
	}
}

func TestIsInterruptErr(t *testing.T) {
	if !isInterruptErr(context.Canceled) {
		t.Fatal("context.Canceled should be an interrupt error")
	}
	if isInterruptErr(context.DeadlineExceeded) {
		t.Fatal("context.DeadlineExceeded should not be an interrupt error")
	}
	if isInterruptErr(nil) {
		t.Fatal("nil should not be an interrupt error")
	}
	wrapped := fmt.Errorf("wrapped: %w", context.Canceled)
	if !isInterruptErr(wrapped) {
		t.Fatal("wrapped context.Canceled should be an interrupt error")
	}
}

func TestSession_InterruptTurn(t *testing.T) {
	s := &session{}
	// No turn in progress — interruptTurn returns false.
	if s.interruptTurn() {
		t.Fatal("interruptTurn should return false with no turn in progress")
	}
	if s.isRunning() {
		t.Fatal("isRunning should be false with no turn")
	}

	// Simulate a turn starting by setting turnCancel.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.turnCancelMu.Lock()
	s.turnCancel = cancel
	s.turnCancelMu.Unlock()

	if !s.isRunning() {
		t.Fatal("isRunning should be true with a turn in progress")
	}

	if !s.interruptTurn() {
		t.Fatal("interruptTurn should return true with a turn in progress")
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("context should be cancelled after interruptTurn")
	}
}
