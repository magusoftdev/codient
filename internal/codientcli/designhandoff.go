package codientcli

import (
	"fmt"
	"regexp"
	"strings"

	"codient/internal/planstore"
)

// buildPlanHandoffMessage produces the synthetic user message injected on a plan->build
// transition. When plan is non-nil it is rendered in structured form (steps, files, verification)
// so the model receives precise scope; otherwise fallbackMarkdown (e.g. the assistant's last
// plan-mode reply) is appended verbatim, with any legacy "run codient -mode build" / "switch
// modes" instructions removed because the session is already in build mode.
func buildPlanHandoffMessage(plan *planstore.Plan, fallbackMarkdown string, toolNames []string) string {
	var b strings.Builder
	b.WriteString("This session is already in build mode. Available tools: ")
	b.WriteString(strings.Join(toolNames, ", "))
	b.WriteString(". Only use tools from this list.\n\n")
	b.WriteString("The user just chose to implement the design from the previous turn. Do not ask whether to proceed, whether they want you to build, or for confirmation—they already confirmed. Do not treat the design as a proposal to discuss: start implementing now using tools.\n\n")
	b.WriteString("The design was produced by a language model in a read-only session. " +
		"Before implementing each step, verify its premise using tools " +
		"(e.g. read the files it references, run existing tests). " +
		"If a step's premise is wrong—for example it claims something is broken but it is not—skip that step and briefly note why.\n\n")
	b.WriteString("Ignore any line in the design below that says to run codient or switch modes elsewhere; you are in the correct mode here.\n\n")
	b.WriteString("---\n\n")

	body := stripRelaunchLines(fallbackMarkdown)
	if plan != nil {
		structured := renderHandoffPlan(plan)
		if structured != "" {
			body = structured
		}
	}
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}

// reRelaunch matches lines that tell the user to run/relaunch codient in a different
// mode. They are stripped from handoff bodies because the session is already in build mode.
var reRelaunch = regexp.MustCompile(`(?im)^.*\b(run|invoke|launch|start|use)\b[^\n]*\bcodient\b[^\n]*(-mode|--mode|build mode|in build|switch modes?|switching modes?).*$`)

// reSwitchMode catches "switch to build mode" / "switch modes" / "now switch to build" without "codient".
var reSwitchMode = regexp.MustCompile(`(?im)^.*\b(switch|switching)\b[^\n]*\bmodes?\b.*$`)

// stripRelaunchLines drops lines from a plan markdown that instruct the user to run codient
// elsewhere or switch modes. Such lines confuse the model after we have already switched.
func stripRelaunchLines(md string) string {
	if md == "" {
		return ""
	}
	cleaned := reRelaunch.ReplaceAllString(md, "")
	cleaned = reSwitchMode.ReplaceAllString(cleaned, "")
	// Collapse the blank lines left behind so the output stays readable.
	for strings.Contains(cleaned, "\n\n\n") {
		cleaned = strings.ReplaceAll(cleaned, "\n\n\n", "\n\n")
	}
	return cleaned
}

// renderHandoffPlan formats a structured plan for inclusion in the handoff message.
// Returns "" when the plan has no usable content; callers fall back to raw markdown.
func renderHandoffPlan(plan *planstore.Plan) string {
	if plan == nil {
		return ""
	}
	if len(plan.Steps) == 0 && plan.Summary == "" && len(plan.FilesToModify) == 0 && len(plan.Verification) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Approved plan\n\n")
	if plan.Summary != "" {
		fmt.Fprintf(&b, "## Summary\n\n%s\n\n", strings.TrimSpace(plan.Summary))
	}
	if plan.UserRequest != "" {
		fmt.Fprintf(&b, "**Original request:** %s\n\n", strings.TrimSpace(plan.UserRequest))
	}
	if len(plan.FilesToModify) > 0 {
		b.WriteString("## Files to modify\n\n")
		for _, f := range plan.FilesToModify {
			fmt.Fprintf(&b, "- `%s`\n", strings.TrimSpace(f))
		}
		b.WriteString("\n")
	}
	if len(plan.Steps) > 0 {
		b.WriteString("## Implementation steps\n\n")
		for i, step := range plan.Steps {
			title := strings.TrimSpace(step.Title)
			if title == "" {
				title = fmt.Sprintf("Step %d", i+1)
			}
			fmt.Fprintf(&b, "%d. %s\n", i+1, title)
			if d := strings.TrimSpace(step.Description); d != "" {
				fmt.Fprintf(&b, "   %s\n", d)
			}
		}
		b.WriteString("\n")
	}
	if len(plan.Verification) > 0 {
		b.WriteString("## Verification\n\n")
		for _, v := range plan.Verification {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(v))
		}
		b.WriteString("\n")
	}
	if len(plan.Assumptions) > 0 {
		b.WriteString("## Assumptions to verify\n\n")
		for _, a := range plan.Assumptions {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(a))
		}
		b.WriteString("\n")
	}
	return b.String()
}
