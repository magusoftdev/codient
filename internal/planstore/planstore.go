// Package planstore persists structured execution plans to disk as JSON,
// with a markdown rendering for user-facing display.
package planstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Phase tracks where a plan is in the plan-approve-execute-review lifecycle.
type Phase string

const (
	PhaseDraft            Phase = "draft"
	PhaseAwaitingApproval Phase = "awaiting_approval"
	PhaseApproved         Phase = "approved"
	PhaseExecuting        Phase = "executing"
	PhaseReview           Phase = "review"
	PhaseDone             Phase = "done"
)

// StepStatus tracks the state of an individual plan step.
type StepStatus string

const (
	StepPending    StepStatus = "pending"
	StepInProgress StepStatus = "in_progress"
	StepDone       StepStatus = "done"
	StepSkipped    StepStatus = "skipped"
)

// Plan is the structured execution plan artifact persisted to disk.
type Plan struct {
	SessionID     string    `json:"session_id"`
	Workspace     string    `json:"workspace"`
	Revision      int       `json:"revision"`
	Phase         Phase     `json:"phase"`
	UserRequest   string    `json:"user_request"`
	Summary       string    `json:"summary"`
	Assumptions   []string  `json:"assumptions,omitempty"`
	OpenQuestions []string  `json:"open_questions,omitempty"`
	FilesToModify []string  `json:"files_to_modify,omitempty"`
	Steps         []Step    `json:"steps"`
	Verification  []string  `json:"verification,omitempty"`
	Approval      *Approval `json:"approval,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// RawMarkdown preserves the agent's original markdown output for display
	// when structured parsing could not extract all sections.
	RawMarkdown string `json:"raw_markdown,omitempty"`
}

// Step is a single implementation step within a plan.
type Step struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	PhaseGroup  int        `json:"phase_group"`
	Status      StepStatus `json:"status"`
}

// Approval records the user's decision on a plan.
type Approval struct {
	Decision  string `json:"decision"` // "approve", "reject", "edit"
	Feedback  string `json:"feedback,omitempty"`
	Timestamp string `json:"timestamp"`
}

// Dir returns the plan storage directory for a session within a workspace.
func Dir(workspace, sessionID string) string {
	base := strings.TrimSpace(workspace)
	if base == "" {
		base = "."
	}
	dir := filepath.Join(base, ".codient", "plans")
	if sid := strings.TrimSpace(sessionID); sid != "" {
		dir = filepath.Join(dir, sid)
	}
	return dir
}

// Path returns the JSON plan file path for a session.
func Path(workspace, sessionID string) string {
	return filepath.Join(Dir(workspace, sessionID), "plan.json")
}

// Save writes the plan atomically (write tmp + rename).
func Save(plan *Plan) error {
	plan.UpdatedAt = time.Now().UTC()
	if plan.CreatedAt.IsZero() {
		plan.CreatedAt = plan.UpdatedAt
	}

	dir := Dir(plan.Workspace, plan.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("planstore: mkdir: %w", err)
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("planstore: marshal: %w", err)
	}

	path := Path(plan.Workspace, plan.SessionID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("planstore: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("planstore: rename: %w", err)
	}
	return nil
}

// Load reads a plan from the standard path for a session.
// Returns nil, nil if no plan file exists.
func Load(workspace, sessionID string) (*Plan, error) {
	path := Path(workspace, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("planstore: read: %w", err)
	}
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("planstore: unmarshal: %w", err)
	}
	return &p, nil
}

// IncrementRevision bumps the revision and resets approval.
func IncrementRevision(plan *Plan) {
	plan.Revision++
	plan.Approval = nil
}

// SetStepStatus updates the status of a step by ID.
// Returns false if the step was not found.
func SetStepStatus(plan *Plan, stepID string, status StepStatus) bool {
	for i := range plan.Steps {
		if plan.Steps[i].ID == stepID {
			plan.Steps[i].Status = status
			return true
		}
	}
	return false
}

// AllStepsDone returns true when every step is done or skipped.
func AllStepsDone(plan *Plan) bool {
	if len(plan.Steps) == 0 {
		return false
	}
	for _, s := range plan.Steps {
		if s.Status != StepDone && s.Status != StepSkipped {
			return false
		}
	}
	return true
}

// StepsByPhaseGroup returns steps grouped by PhaseGroup number.
// Groups are returned in ascending order.
func StepsByPhaseGroup(plan *Plan) [][]Step {
	if len(plan.Steps) == 0 {
		return nil
	}
	maxGroup := 0
	for _, s := range plan.Steps {
		if s.PhaseGroup > maxGroup {
			maxGroup = s.PhaseGroup
		}
	}
	groups := make([][]Step, maxGroup+1)
	for _, s := range plan.Steps {
		groups[s.PhaseGroup] = append(groups[s.PhaseGroup], s)
	}
	// Remove empty groups.
	var out [][]Step
	for _, g := range groups {
		if len(g) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// PhaseGroupDone returns true if all steps in the given group are done or skipped.
func PhaseGroupDone(steps []Step) bool {
	for _, s := range steps {
		if s.Status != StepDone && s.Status != StepSkipped {
			return false
		}
	}
	return len(steps) > 0
}

// CheckpointSummary renders a progress summary after completing a phase group.
func CheckpointSummary(plan *Plan, completedGroup int) string {
	var b strings.Builder
	groups := StepsByPhaseGroup(plan)

	doneCount, totalCount := 0, 0
	for _, s := range plan.Steps {
		totalCount++
		if s.Status == StepDone || s.Status == StepSkipped {
			doneCount++
		}
	}

	fmt.Fprintf(&b, "Checkpoint: phase group %d complete (%d/%d steps done)\n\n", completedGroup+1, doneCount, totalCount)

	fmt.Fprintf(&b, "Completed:\n")
	for gi, g := range groups {
		if gi > completedGroup {
			break
		}
		for _, s := range g {
			if s.Status == StepDone {
				fmt.Fprintf(&b, "  [done]    %s\n", s.Title)
			} else if s.Status == StepSkipped {
				fmt.Fprintf(&b, "  [skipped] %s\n", s.Title)
			}
		}
	}

	remaining := false
	for gi, g := range groups {
		if gi <= completedGroup {
			continue
		}
		if !remaining {
			fmt.Fprintf(&b, "\nRemaining:\n")
			remaining = true
		}
		for _, s := range g {
			fmt.Fprintf(&b, "  [pending] %s\n", s.Title)
		}
	}

	return b.String()
}

// RenderMarkdown produces a user-readable markdown representation of the plan.
func RenderMarkdown(plan *Plan) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Plan (revision %d)\n\n", plan.Revision)

	if plan.Summary != "" {
		fmt.Fprintf(&b, "## Summary\n\n%s\n\n", plan.Summary)
	}

	if plan.UserRequest != "" {
		fmt.Fprintf(&b, "**Request:** %s\n\n", plan.UserRequest)
	}

	if len(plan.Assumptions) > 0 {
		fmt.Fprintf(&b, "## Assumptions\n\n")
		for _, a := range plan.Assumptions {
			fmt.Fprintf(&b, "- %s\n", a)
		}
		b.WriteString("\n")
	}

	if len(plan.FilesToModify) > 0 {
		fmt.Fprintf(&b, "## Files to modify\n\n")
		for _, f := range plan.FilesToModify {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
		b.WriteString("\n")
	}

	if len(plan.Steps) > 0 {
		fmt.Fprintf(&b, "## Steps\n\n")
		for i, s := range plan.Steps {
			status := "[ ]"
			switch s.Status {
			case StepDone:
				status = "[x]"
			case StepInProgress:
				status = "[>]"
			case StepSkipped:
				status = "[-]"
			}
			fmt.Fprintf(&b, "%d. %s %s\n", i+1, status, s.Title)
			if s.Description != "" {
				fmt.Fprintf(&b, "   %s\n", s.Description)
			}
		}
		b.WriteString("\n")
	}

	if len(plan.Verification) > 0 {
		fmt.Fprintf(&b, "## Verification\n\n")
		for _, v := range plan.Verification {
			fmt.Fprintf(&b, "- %s\n", v)
		}
		b.WriteString("\n")
	}

	if len(plan.OpenQuestions) > 0 {
		fmt.Fprintf(&b, "## Open questions\n\n")
		for _, q := range plan.OpenQuestions {
			fmt.Fprintf(&b, "- %s\n", q)
		}
		b.WriteString("\n")
	}

	if plan.Approval != nil {
		fmt.Fprintf(&b, "## Approval\n\n")
		fmt.Fprintf(&b, "- Decision: %s\n", plan.Approval.Decision)
		if plan.Approval.Feedback != "" {
			fmt.Fprintf(&b, "- Feedback: %s\n", plan.Approval.Feedback)
		}
		if plan.Approval.Timestamp != "" {
			fmt.Fprintf(&b, "- At: %s\n", plan.Approval.Timestamp)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "**Phase:** %s\n", plan.Phase)

	return b.String()
}
