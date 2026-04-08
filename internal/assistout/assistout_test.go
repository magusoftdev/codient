package assistout

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrepareAssistantText_PlanMode(t *testing.T) {
	in := "Pick one?\n\nA) a B) b\n\nWaiting for your answer"
	got := PrepareAssistantText(in, true)
	if !strings.Contains(got, "- A) a") || strings.Contains(got, "a B)") {
		t.Fatalf("expected list normalization: %q", got)
	}
	if PrepareAssistantText("x", false) != "x" {
		t.Fatal("non-plan should trim only")
	}
}

func TestWriteAssistant_Plain(t *testing.T) {
	var buf bytes.Buffer
	err := WriteAssistant(&buf, "# Title\n\nHello", false, false)
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "# Title") {
		t.Fatalf("expected raw markdown, got %q", s)
	}
}

func TestWriteAssistant_EmptyPlain(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteAssistant(&buf, "", false, false); err != nil {
		t.Fatal(err)
	}
}

func TestWriteWelcome_Plain(t *testing.T) {
	var buf bytes.Buffer
	WriteWelcome(&buf, WelcomeParams{
		Plain:     true,
		Repl:      true,
		Mode:      "plan",
		Workspace: "/tmp/ws",
		Model:     "m1",
	})
	s := buf.String()
	if !strings.Contains(s, "codient") || !strings.Contains(s, "Session") || !strings.Contains(s, "plan") {
		t.Fatalf("unexpected welcome: %q", s)
	}
}

func TestWriteWelcome_Quiet(t *testing.T) {
	var buf bytes.Buffer
	WriteWelcome(&buf, WelcomeParams{Quiet: true, Plain: true, Mode: "build"})
	if buf.Len() != 0 {
		t.Fatalf("expected empty, got %q", buf.String())
	}
}

func TestSessionPrompt_Plain(t *testing.T) {
	p := SessionPrompt(true, "build")
	if !strings.HasPrefix(p, "[build] > ") {
		t.Fatalf("unexpected prompt: %q", p)
	}
	p = SessionPrompt(true, "plan")
	if !strings.HasPrefix(p, "[plan] > ") {
		t.Fatalf("unexpected prompt: %q", p)
	}
}

func TestPlanAnswerPrefix_Plain(t *testing.T) {
	p := PlanAnswerPrefix(true)
	if !strings.HasPrefix(p, "Answer:") {
		t.Fatalf("unexpected prefix: %q", p)
	}
}

func TestProgressIntentBulletPrefix_Plain(t *testing.T) {
	want := "  ● "
	if got := ProgressIntentBulletPrefix(true, "plan"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
