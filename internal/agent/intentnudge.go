package agent

import "strings"

// toolIntentContinueMessage is injected once when the model returned intent-like prose
// but no tool calls while tools were available — common with some OpenAI-compatible locals.
const toolIntentContinueMessage = `You outlined what to do but did not call any tools. Invoke the appropriate tools now (use native function/tool_calls in the API response), run the searches or file reads you described, then answer with what you found. Do not reply with intent-only prose.`

// shouldNudgeIncompleteToolIntent reports whether assistant text is likely a preamble
// that should have been followed by tool_calls. Conservative to avoid nudging real short answers.
func shouldNudgeIncompleteToolIntent(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.Contains(s, "```") {
		return false
	}
	r := []rune(s)
	if len(r) > 512 {
		return false
	}
	paras := strings.Split(s, "\n\n")
	if len(paras) >= 2 && len(strings.TrimSpace(paras[1])) > 150 {
		return false
	}
	lower := strings.ToLower(s)
	firstLine := lower
	if idx := strings.IndexByte(lower, '\n'); idx >= 0 {
		firstLine = lower[:idx]
	}
	switch {
	case strings.HasPrefix(firstLine, "let me "):
		return true
	case strings.HasPrefix(firstLine, "i need to "):
		return true
	case strings.HasPrefix(firstLine, "i'm going to "):
		return true
	case strings.HasPrefix(firstLine, "first,"):
		return true
	case strings.HasPrefix(firstLine, "to investigate"):
		return true
	case strings.HasPrefix(firstLine, "to answer"):
		return true
	case strings.HasPrefix(firstLine, "i'll start"):
		return true
	case strings.HasPrefix(firstLine, "i will search"):
		return true
	case strings.HasPrefix(firstLine, "i will read"):
		return true
	case strings.HasPrefix(firstLine, "i will check"):
		return true
	case strings.HasPrefix(firstLine, "i will look"):
		return true
	case strings.HasPrefix(firstLine, "i will grep"):
		return true
	}
	if strings.HasSuffix(strings.TrimSpace(s), ":") && len(r) <= 320 {
		return true
	}
	return false
}
