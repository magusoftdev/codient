package agent

import (
	"encoding/json"
	"time"
)

// TranscriptKind classifies structured agent output for UIs (TUI, future ACP rich views).
type TranscriptKind int

const (
	TranscriptStatus TranscriptKind = iota
	TranscriptAssistantPreface
	TranscriptReasoningDelta
	TranscriptToolIntent
	// TranscriptToolResult is emitted after each tool finishes (compact summary line).
	// Legacy stderr output omits per-tool lines and keeps the single round summary only.
	TranscriptToolResult
	TranscriptRoundSummary
	TranscriptAutoCheck
	TranscriptModelError
	TranscriptPlain
	TranscriptWarning
)

// TranscriptEvent is a single unit of progress / chain-of-thought / tool lifecycle.
// Plain REPL with -progress renders via transcriptLegacyText; the Bubble Tea TUI
// uses OnTranscriptEvent with structured formatting instead of raw Progress lines.
type TranscriptEvent struct {
	Kind TranscriptKind

	// Status, Plain, Warning, ModelError, AutoCheck, ReasoningDelta
	Text string

	// AssistantPreface: raw assistant prose before tools (may include XML tool markup).
	// UIs show ThinkingFull (or derived) for OpenCode-style blocks; legacy line uses formatThinkingLine.
	AssistantProse string
	ThinkingFull   string // stripped of tool markup; empty means derive from AssistantProse

	ToolName    string
	ToolArgs    json.RawMessage
	ToolSummary string

	RoundLLMDur    time.Duration
	RoundToolParts []string
	RoundUsageSuf  string
}
