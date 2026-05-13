package codientcli

import (
	"fmt"
	"strings"

	"codient/internal/planstore"
)

func summarizePlanForIntent(plan *planstore.Plan, phase planstore.Phase) string {
	if plan == nil {
		return ""
	}
	var b strings.Builder
	if phase != "" {
		fmt.Fprintf(&b, "phase=%s", phase)
	} else if plan.Phase != "" {
		fmt.Fprintf(&b, "phase=%s", plan.Phase)
	}
	if plan.Summary != "" {
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "summary=%s", compactIntentText(plan.Summary, 400))
	}
	done, total := 0, len(plan.Steps)
	for _, st := range plan.Steps {
		if st.Status == planstore.StepDone || st.Status == planstore.StepSkipped {
			done++
		}
	}
	if total > 0 {
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "steps=%d/%d done", done, total)
		b.WriteString("; next=")
		wrote := 0
		for _, st := range plan.Steps {
			if st.Status == planstore.StepDone || st.Status == planstore.StepSkipped {
				continue
			}
			if wrote > 0 {
				b.WriteString(" | ")
			}
			title := strings.TrimSpace(st.Title)
			if title == "" {
				title = st.ID
			}
			fmt.Fprintf(&b, "%s:%s", st.ID, compactIntentText(title, 120))
			wrote++
			if wrote >= 4 {
				break
			}
		}
		if wrote == 0 {
			b.WriteString("(none)")
		}
	}
	return b.String()
}

func compactIntentText(s string, max int) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
