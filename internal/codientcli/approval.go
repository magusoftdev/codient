package codientcli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"codient/internal/planstore"
)

// recordApproval sets the approval metadata on the plan and saves it.
func recordApproval(plan *planstore.Plan, decision, feedback string) {
	plan.Approval = &planstore.Approval{
		Decision:  decision,
		Feedback:  feedback,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// editPlanInExternalEditor opens the rendered plan markdown in $EDITOR (or $VISUAL,
// notepad on Windows, vi elsewhere). When the user saves edits, the markdown is
// re-parsed back into the structured plan and the revision is bumped. Returns true
// when the plan was actually changed and persisted.
func (s *session) editPlanInExternalEditor(plan *planstore.Plan) (bool, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		if runtime.GOOS == "windows" {
			editor = "notepad"
		} else {
			editor = "vi"
		}
	}

	md := planstore.RenderMarkdown(plan)
	tmpFile, err := os.CreateTemp("", "codient-plan-*.md")
	if err != nil {
		return false, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(md); err != nil {
		tmpFile.Close()
		return false, fmt.Errorf("write temp: %w", err)
	}
	tmpFile.Close()

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("editor exited with error: %w", err)
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return false, fmt.Errorf("read edited file: %w", err)
	}
	editedMd := string(edited)
	if editedMd == md {
		return false, nil
	}

	reparsed := planstore.ParseFromMarkdown(editedMd, plan.UserRequest)
	plan.Summary = reparsed.Summary
	plan.Steps = reparsed.Steps
	plan.Assumptions = reparsed.Assumptions
	plan.OpenQuestions = reparsed.OpenQuestions
	plan.FilesToModify = reparsed.FilesToModify
	plan.Verification = reparsed.Verification
	plan.RawMarkdown = reparsed.RawMarkdown
	planstore.IncrementRevision(plan)

	if err := planstore.Save(plan); err != nil {
		return false, fmt.Errorf("plan save: %w", err)
	}
	return true, nil
}
