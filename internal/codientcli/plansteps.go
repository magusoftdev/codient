package codientcli

import (
	"fmt"
	"strings"

	"codient/internal/planstore"
	"codient/internal/tools"
)

func (s *session) applyCompleteStep(req tools.CompleteStepRequest) (string, error) {
	plan := s.currentPlan
	if plan == nil {
		return "", fmt.Errorf("no active structured plan")
	}
	idx := -1
	for i := range plan.Steps {
		if plan.Steps[i].ID == req.StepID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", fmt.Errorf("unknown plan step %q", req.StepID)
	}
	st := &plan.Steps[idx]
	if s.planActiveGroupLimited && st.PhaseGroup != s.planActiveGroup {
		return "", fmt.Errorf("step %q belongs to phase group %d, but phase group %d is active", req.StepID, st.PhaseGroup+1, s.planActiveGroup+1)
	}
	switch req.Outcome {
	case "done":
		st.Status = planstore.StepDone
	case "skipped":
		st.Status = planstore.StepSkipped
	default:
		return "", fmt.Errorf("invalid outcome %q", req.Outcome)
	}
	st.Note = strings.TrimSpace(req.Note)
	if err := planstore.Save(plan); err != nil {
		return "", err
	}
	s.planPhase = plan.Phase
	if s.cfg != nil {
		s.autoSave()
	}
	return "complete_step: updated plan progress\n\n" + planProgressSummary(plan, s.planActiveGroup, s.planActiveGroupLimited), nil
}

func planProgressSummary(plan *planstore.Plan, activeGroup int, limited bool) string {
	if plan == nil {
		return "No active plan."
	}
	var b strings.Builder
	done, total := 0, len(plan.Steps)
	for _, st := range plan.Steps {
		if st.Status == planstore.StepDone || st.Status == planstore.StepSkipped {
			done++
		}
	}
	fmt.Fprintf(&b, "Plan progress: %d/%d steps complete\n", done, total)
	for _, st := range plan.Steps {
		if limited && st.PhaseGroup != activeGroup {
			continue
		}
		status := string(st.Status)
		if status == "" {
			status = string(planstore.StepPending)
		}
		fmt.Fprintf(&b, "- [%s] %s %s", status, st.ID, st.Title)
		if st.Note != "" {
			fmt.Fprintf(&b, " — %s", st.Note)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func phaseGroupComplete(plan *planstore.Plan, groupIdx int) bool {
	if plan == nil {
		return false
	}
	seen := false
	for _, st := range plan.Steps {
		if st.PhaseGroup != groupIdx {
			continue
		}
		seen = true
		if st.Status != planstore.StepDone && st.Status != planstore.StepSkipped {
			return false
		}
	}
	return seen
}
