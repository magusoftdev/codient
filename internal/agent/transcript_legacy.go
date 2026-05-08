package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// transcriptLegacyText renders a TranscriptEvent as the historical stderr progress
// lines (bullet + ▸ + summaries) so non-TUI / tests keep identical output.
func transcriptLegacyText(plain bool, mode string, ev *TranscriptEvent) string {
	if ev == nil {
		return ""
	}
	switch ev.Kind {
	case TranscriptStatus:
		s := strings.TrimSpace(ev.Text)
		if s == "" {
			return ""
		}
		if line := FormatStatusProgressLine(plain, mode, s); line != "" {
			return line + "\n"
		}
		return ""
	case TranscriptAssistantPreface:
		src := strings.TrimSpace(ev.AssistantProse)
		if src != "" {
			if line := FormatThinkingProgressLine(plain, mode, src); line != "" {
				return "\n" + line + "\n"
			}
		}
		return ""
	case TranscriptReasoningDelta:
		// Streaming reasoning: legacy path omits (TUI / dedicated pane only).
		return ""
	case TranscriptToolIntent:
		if ev.ToolName == "" {
			return ""
		}
		args := ev.ToolArgs
		if args == nil {
			args = jsonNull
		}
		return FormatToolIntentProgressLine(ev.ToolName, args) + "\n"
	case TranscriptToolResult:
		return ""
	case TranscriptRoundSummary:
		if ev.RoundLLMDur == 0 && len(ev.RoundToolParts) == 0 {
			return ""
		}
		d := formatProgressDur(ev.RoundLLMDur)
		mid := strings.Join(ev.RoundToolParts, " · ")
		if mid == "" {
			mid = "reply"
		}
		return fmt.Sprintf("%sllm %s  ·  %s%s\n", ProgressNestedIndent, d, mid, ev.RoundUsageSuf)
	case TranscriptAutoCheck:
		s := strings.TrimSpace(ev.Text)
		if s == "" {
			return ""
		}
		return ProgressNestedIndent + s + "\n"
	case TranscriptModelError:
		return FormatModelErrorProgressLine(plain, ev.RoundLLMDur, ev.Text) + "\n"
	case TranscriptPlain:
		s := strings.TrimSpace(ev.Text)
		if s == "" {
			return ""
		}
		return s + "\n"
	case TranscriptWarning:
		s := strings.TrimSpace(ev.Text)
		if s == "" {
			return ""
		}
		return "  ⚠ " + s + "\n"
	default:
		return ""
	}
}

var jsonNull = json.RawMessage(`null`)
