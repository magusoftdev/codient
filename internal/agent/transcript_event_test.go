package agent

import (
	"strings"
	"testing"
	"time"
)

func TestTranscriptLegacy_roundSummaryReplyOnly(t *testing.T) {
	s := transcriptLegacyText(true, "build", &TranscriptEvent{
		Kind:          TranscriptRoundSummary,
		RoundLLMDur:   100 * time.Millisecond,
		RoundUsageSuf: " · ctx 1%",
	})
	if !strings.Contains(s, "reply") || !strings.Contains(s, "llm") {
		t.Fatalf("got %q", s)
	}
}

func TestTranscriptLegacy_toolIntent(t *testing.T) {
	s := transcriptLegacyText(true, "build", &TranscriptEvent{
		Kind:     TranscriptToolIntent,
		ToolName: "read_file",
		ToolArgs: []byte(`{"path":"a.go"}`),
	})
	if !strings.Contains(s, "▸") || !strings.Contains(s, "a.go") {
		t.Fatalf("got %q", s)
	}
}

func TestTranscriptLegacy_toolResultSilent(t *testing.T) {
	s := transcriptLegacyText(true, "build", &TranscriptEvent{
		Kind: TranscriptToolResult,
		Text: ProgressNestedIndent + "read_file a.go 1ms",
	})
	if s != "" {
		t.Fatalf("legacy should omit per-tool result lines, got %q", s)
	}
}

func TestFormatFullThinkingProse_stripsMarkup(t *testing.T) {
	in := "Plan.\n<function=read_file><parameter=path>x.go</parameter></function>"
	got := FormatFullThinkingProse(in)
	if !strings.Contains(got, "Plan") || strings.Contains(got, "function=") {
		t.Fatalf("got %q", got)
	}
}
