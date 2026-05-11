package codientcli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/openai/openai-go/v3"

	"codient/internal/config"
	"codient/internal/openaiclient"
	"codient/internal/planstore"
	"codient/internal/prompt"
	"codient/internal/sessionstore"
)

// tierForResolvedMode maps a resolved internal mode (build / ask / plan) to
// the reasoning tier the orchestrator wants to drive that mode with. Plan
// (architectural design) uses the high-reasoning model; build / ask / auto
// fallthrough use the low-reasoning model.
func tierForResolvedMode(m prompt.Mode) string {
	switch m {
	case prompt.ModePlan:
		return config.TierHigh
	default:
		return config.TierLow
	}
}

// transitionToInternalMode rebuilds session state (client, registry, system
// prompt) for a new internal mode label without touching conversation
// history. This is a pure swap of runtime artifacts so the next turn runs
// against the correct tool registry, reasoning tier, and system prompt. The
// user-facing mode is always ModeAuto and we never print "switched to X
// mode" chatter. Callers (the orchestrator, structured plan execution, plan
// resume) use this between turns; the orchestrator restores ModeAuto after
// each turn so the next user prompt is re-classified.
//
// The plan -> build handoff (injecting a synthetic implementation directive)
// is the caller's responsibility — keeping it out of this helper avoids
// double-handoff bugs when the caller also wants to drive the next turn with
// its own synthetic user message.
func (s *session) transitionToInternalMode(newMode prompt.Mode) {
	if s.mode == newMode {
		return
	}
	s.setMode(newMode)
	s.client = openaiclient.NewForTier(s.cfg, tierForResolvedMode(newMode))
	s.installRegistry(buildRegistry(s.cfg, newMode, s, s.memOpts))

	if newMode == prompt.ModeBuild {
		s.warnIfNotGitRepo()
	}
}

// resetForReplan rewinds the session to a fresh plan-mode draft for the
// remaining work. Used by structured plan execution when the user asks to
// re-plan mid-flow. Filters tool messages out of the history so the planner
// sees a clean conversational record.
func (s *session) resetForReplan() {
	s.history = filterHistoryForModeSwitch(s.history)
	s.transitionToInternalMode(prompt.ModePlan)
}

// markPlanApprovedForHandoff records that the user approved the active plan via a
// plan->build mode switch. Persisted so /plan-status and resume reflect approval.
func (s *session) markPlanApprovedForHandoff() {
	plan := s.currentPlan
	if plan == nil {
		return
	}
	plan.Phase = planstore.PhaseApproved
	s.planPhase = planstore.PhaseApproved
	if plan.Approval == nil {
		recordApproval(plan, "approve", "approved by switching to build mode")
	}
	if err := planstore.Save(plan); err != nil {
		fmt.Fprintf(os.Stderr, "codient: plan save: %v\n", err)
	}
}

// filterHistoryForModeSwitch keeps user and assistant text messages, dropping tool
// messages and tool-call-only assistant messages. Assistant messages that have both
// text content and tool calls are preserved as text-only.
func filterHistoryForModeSwitch(history []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	var out []openai.ChatCompletionMessageParamUnion
	for _, m := range history {
		switch {
		case m.OfUser != nil:
			out = append(out, m)
		case m.OfAssistant != nil:
			content := extractAssistantText(m)
			if content != "" {
				out = append(out, openai.AssistantMessage(content))
			}
		case m.OfSystem != nil:
			// Drop old system messages; a new one will be prepended by the runner.
		case m.OfTool != nil:
			// Drop tool result messages.
		}
	}
	return out
}

// extractAssistantText gets the text content from an assistant message,
// ignoring any tool call data.
func extractAssistantText(m openai.ChatCompletionMessageParamUnion) string {
	if m.OfAssistant == nil {
		return ""
	}
	// Try direct content field first.
	b, err := json.Marshal(m.OfAssistant.Content)
	if err != nil {
		return ""
	}
	var s string
	if json.Unmarshal(b, &s) == nil && s != "" {
		return s
	}
	// For complex content (array of parts), extract text from the raw JSON.
	return sessionstore.MessageContent(mustMarshal(m))
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
