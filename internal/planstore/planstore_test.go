package planstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	plan := &Plan{
		SessionID:   "test-session",
		Workspace:   dir,
		Revision:    1,
		Phase:       PhaseDraft,
		UserRequest: "add a /version command",
		Summary:     "Add a slash command that prints the version.",
		Steps: []Step{
			{ID: "step-1", Title: "Add version const", Status: StepPending},
			{ID: "step-2", Title: "Register /version", Status: StepPending},
		},
		Verification: []string{"run /version and check output"},
	}
	if err := Save(plan); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := Path(dir, "test-session")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("plan file not created: %v", err)
	}

	loaded, err := Load(dir, "test-session")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SessionID != plan.SessionID {
		t.Errorf("SessionID = %q, want %q", loaded.SessionID, plan.SessionID)
	}
	if loaded.Revision != 1 {
		t.Errorf("Revision = %d, want 1", loaded.Revision)
	}
	if loaded.Phase != PhaseDraft {
		t.Errorf("Phase = %q, want %q", loaded.Phase, PhaseDraft)
	}
	if len(loaded.Steps) != 2 {
		t.Errorf("Steps count = %d, want 2", len(loaded.Steps))
	}
	if loaded.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

func TestLoadNonExistent(t *testing.T) {
	dir := t.TempDir()
	plan, err := Load(dir, "does-not-exist")
	if err != nil {
		t.Fatalf("Load should return nil error for missing file: %v", err)
	}
	if plan != nil {
		t.Fatal("Load should return nil plan for missing file")
	}
}

func TestIncrementRevision(t *testing.T) {
	plan := &Plan{
		Revision: 2,
		Approval: &Approval{Decision: "approve"},
	}
	IncrementRevision(plan)
	if plan.Revision != 3 {
		t.Errorf("Revision = %d, want 3", plan.Revision)
	}
	if plan.Approval != nil {
		t.Error("Approval should be cleared on revision increment")
	}
}

func TestSetStepStatus(t *testing.T) {
	plan := &Plan{
		Steps: []Step{
			{ID: "s1", Status: StepPending},
			{ID: "s2", Status: StepPending},
		},
	}
	if !SetStepStatus(plan, "s1", StepDone) {
		t.Fatal("SetStepStatus should return true for existing step")
	}
	if plan.Steps[0].Status != StepDone {
		t.Errorf("step s1 status = %q, want %q", plan.Steps[0].Status, StepDone)
	}
	if SetStepStatus(plan, "missing", StepDone) {
		t.Fatal("SetStepStatus should return false for missing step")
	}
}

func TestAllStepsDone(t *testing.T) {
	plan := &Plan{Steps: []Step{
		{ID: "s1", Status: StepDone},
		{ID: "s2", Status: StepSkipped},
	}}
	if !AllStepsDone(plan) {
		t.Error("AllStepsDone should be true when all done/skipped")
	}
	plan.Steps[0].Status = StepPending
	if AllStepsDone(plan) {
		t.Error("AllStepsDone should be false when a step is pending")
	}
}

func TestAllStepsDoneEmpty(t *testing.T) {
	plan := &Plan{}
	if AllStepsDone(plan) {
		t.Error("AllStepsDone should be false for empty steps")
	}
}

func TestStepsByPhaseGroup(t *testing.T) {
	plan := &Plan{Steps: []Step{
		{ID: "s1", PhaseGroup: 0},
		{ID: "s2", PhaseGroup: 0},
		{ID: "s3", PhaseGroup: 1},
	}}
	groups := StepsByPhaseGroup(plan)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if len(groups[0]) != 2 {
		t.Errorf("group 0 size = %d, want 2", len(groups[0]))
	}
	if len(groups[1]) != 1 {
		t.Errorf("group 1 size = %d, want 1", len(groups[1]))
	}
}

func TestPhaseGroupDone(t *testing.T) {
	steps := []Step{
		{Status: StepDone},
		{Status: StepSkipped},
	}
	if !PhaseGroupDone(steps) {
		t.Error("PhaseGroupDone should be true")
	}
	steps[0].Status = StepInProgress
	if PhaseGroupDone(steps) {
		t.Error("PhaseGroupDone should be false with in_progress")
	}
}

func TestRenderMarkdown(t *testing.T) {
	plan := &Plan{
		Revision:    1,
		Summary:     "Add version command.",
		UserRequest: "add /version",
		Steps: []Step{
			{ID: "s1", Title: "Add const", Status: StepDone},
			{ID: "s2", Title: "Register cmd", Status: StepPending},
		},
		Verification: []string{"run /version"},
	}
	md := RenderMarkdown(plan)
	if !containsAny(md, "# Plan (revision 1)") {
		t.Error("missing plan heading")
	}
	if !containsAny(md, "[x] Add const") {
		t.Error("missing done step marker")
	}
	if !containsAny(md, "[ ] Register cmd") {
		t.Error("missing pending step marker")
	}
	if !containsAny(md, "run /version") {
		t.Error("missing verification")
	}
}

func TestCheckpointSummary(t *testing.T) {
	plan := &Plan{Steps: []Step{
		{ID: "s1", Title: "Step A", PhaseGroup: 0, Status: StepDone},
		{ID: "s2", Title: "Step B", PhaseGroup: 0, Status: StepDone},
		{ID: "s3", Title: "Step C", PhaseGroup: 1, Status: StepPending},
	}}
	summary := CheckpointSummary(plan, 0)
	if !containsAny(summary, "phase group 1 complete") {
		t.Error("missing phase group label")
	}
	if !containsAny(summary, "2/3 steps done") {
		t.Error("missing progress count")
	}
	if !containsAny(summary, "[done]    Step A") {
		t.Error("missing completed step")
	}
	if !containsAny(summary, "[pending] Step C") {
		t.Error("missing remaining step")
	}
}

func TestRenderMarkdownAllBranches(t *testing.T) {
	plan := &Plan{
		Revision:      2,
		Summary:       "Full plan.",
		UserRequest:   "do everything",
		Assumptions:   []string{"Go 1.22+", "git available"},
		FilesToModify: []string{"main.go", "util.go"},
		Steps: []Step{
			{ID: "s1", Title: "Step one", Description: "desc1", Status: StepDone},
			{ID: "s2", Title: "Step two", Status: StepInProgress},
			{ID: "s3", Title: "Step three", Status: StepSkipped},
			{ID: "s4", Title: "Step four", Status: StepPending},
		},
		Verification:  []string{"run tests"},
		OpenQuestions: []string{"deploy strategy?"},
		Approval:      &Approval{Decision: "approve", Feedback: "looks good", Timestamp: "2026-04-14T00:00:00Z"},
	}
	md := RenderMarkdown(plan)
	for _, want := range []string{
		"# Plan (revision 2)",
		"## Assumptions",
		"Go 1.22+",
		"`main.go`",
		"[x] Step one",
		"[>] Step two",
		"[-] Step three",
		"[ ] Step four",
		"desc1",
		"## Open questions",
		"deploy strategy?",
		"## Approval",
		"approve",
		"looks good",
		"2026-04-14T00:00:00Z",
		"## Verification",
	} {
		if !containsAny(md, want) {
			t.Errorf("RenderMarkdown missing %q", want)
		}
	}
}

func TestParseFromMarkdown(t *testing.T) {
	md := `## Goal

Add a /version command to the CLI.

## Implementation steps

1. Define a version constant in main.go
2. Register a /version slash command
3. Write tests for the new command

## Testing strategy

- Run /version and check output
- Unit test the handler

## Risks or open points

- None
`
	plan := ParseFromMarkdown(md, "add /version")
	if plan.Summary == "" {
		t.Error("Summary should be populated")
	}
	if plan.UserRequest != "add /version" {
		t.Errorf("UserRequest = %q, want %q", plan.UserRequest, "add /version")
	}
	if len(plan.Steps) != 3 {
		t.Errorf("Steps = %d, want 3", len(plan.Steps))
	}
	if plan.Steps[0].Title == "" {
		t.Error("first step title should not be empty")
	}
	if len(plan.Verification) != 2 {
		t.Errorf("Verification = %d, want 2", len(plan.Verification))
	}
	if plan.Phase != PhaseDraft {
		t.Errorf("Phase = %q, want %q", plan.Phase, PhaseDraft)
	}
}

func TestParseFromMarkdownStarBullets(t *testing.T) {
	md := `## Assumptions

* Git is installed
* Go 1.22+

## Implementation steps

* Add handler
* Write test
`
	plan := ParseFromMarkdown(md, "test")
	if len(plan.Assumptions) != 2 {
		t.Errorf("Assumptions = %d, want 2", len(plan.Assumptions))
	}
	if len(plan.Steps) != 2 {
		t.Errorf("Steps = %d, want 2", len(plan.Steps))
	}
}

func TestParseFromMarkdownWithDescriptions(t *testing.T) {
	md := `## Steps

1. Add the constant
   Define VERSION = "1.0" in version.go
2. Register the command
   Wire it into the slash command registry
`
	plan := ParseFromMarkdown(md, "")
	if len(plan.Steps) != 2 {
		t.Fatalf("Steps = %d, want 2", len(plan.Steps))
	}
	if plan.Steps[0].Description == "" {
		t.Error("first step should have a description")
	}
}

func TestParseFromMarkdownManyStepsPhaseGroups(t *testing.T) {
	md := `## Implementation steps

1. Step A
2. Step B
3. Step C
4. Step D
5. Step E
6. Step F
`
	plan := ParseFromMarkdown(md, "")
	if len(plan.Steps) != 6 {
		t.Fatalf("Steps = %d, want 6", len(plan.Steps))
	}
	// Steps should be distributed across phase groups (groups of 4).
	if plan.Steps[4].PhaseGroup != 1 {
		t.Errorf("Step 5 PhaseGroup = %d, want 1", plan.Steps[4].PhaseGroup)
	}
}

func TestParseFromMarkdownFallback(t *testing.T) {
	md := "Just some prose without any headings or steps."
	plan := ParseFromMarkdown(md, "do something")
	if len(plan.Steps) != 1 {
		t.Fatalf("fallback should produce 1 step, got %d", len(plan.Steps))
	}
	if plan.Steps[0].Title != "Implement plan" {
		t.Errorf("fallback step title = %q", plan.Steps[0].Title)
	}
}

func TestParseFromMarkdownSummaryFallback(t *testing.T) {
	md := `## Some heading

First paragraph of text that describes the goal.

More text here.
`
	plan := ParseFromMarkdown(md, "")
	if plan.Summary == "" {
		t.Error("Summary should fall back to first paragraph")
	}
}

func TestParseFromMarkdownFilesToModify(t *testing.T) {
	md := `## Proposed structure

- cmd/main.go
- internal/handler.go
`
	plan := ParseFromMarkdown(md, "")
	if len(plan.FilesToModify) != 2 {
		t.Errorf("FilesToModify = %d, want 2", len(plan.FilesToModify))
	}
}

func TestExtractFirstParagraphLong(t *testing.T) {
	long := strings.Repeat("word ", 200)
	md := "## Title\n\n" + long
	plan := ParseFromMarkdown(md, "")
	if len(plan.Summary) > 510 {
		t.Errorf("Summary too long: %d chars", len(plan.Summary))
	}
}

func TestParseFromMarkdownNumberedAssumptions(t *testing.T) {
	md := `## Assumptions

1. Go is installed
2. Git is available
3. Tests exist
`
	plan := ParseFromMarkdown(md, "")
	if len(plan.Assumptions) != 3 {
		t.Errorf("Assumptions = %d, want 3", len(plan.Assumptions))
	}
	if plan.Assumptions[0] != "Go is installed" {
		t.Errorf("Assumptions[0] = %q", plan.Assumptions[0])
	}
}

func TestCheckpointSummaryWithSkipped(t *testing.T) {
	plan := &Plan{Steps: []Step{
		{ID: "s1", Title: "Step A", PhaseGroup: 0, Status: StepSkipped},
		{ID: "s2", Title: "Step B", PhaseGroup: 1, Status: StepPending},
	}}
	summary := CheckpointSummary(plan, 0)
	if !containsAny(summary, "[skipped] Step A") {
		t.Error("missing skipped step in summary")
	}
}

func TestExtractStepsWithBullets(t *testing.T) {
	md := `## Steps

- Create the file
- Add the handler
- Write tests
`
	plan := ParseFromMarkdown(md, "")
	if len(plan.Steps) != 3 {
		t.Errorf("Steps = %d, want 3 (from bullet list)", len(plan.Steps))
	}
}

func TestDirEmptyWorkspace(t *testing.T) {
	d := Dir("", "sid")
	if d == "" {
		t.Error("Dir should return non-empty even for empty workspace")
	}
}

func TestStepsByPhaseGroupEmpty(t *testing.T) {
	plan := &Plan{}
	groups := StepsByPhaseGroup(plan)
	if groups != nil {
		t.Errorf("expected nil for empty steps, got %v", groups)
	}
}

func TestAtomicSave(t *testing.T) {
	dir := t.TempDir()
	plan := &Plan{
		SessionID: "atomic-test",
		Workspace: dir,
		Phase:     PhaseDraft,
		Steps:     []Step{{ID: "s1", Title: "test", Status: StepPending}},
	}
	if err := Save(plan); err != nil {
		t.Fatalf("Save: %v", err)
	}
	tmp := Path(dir, "atomic-test") + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("tmp file should be cleaned up after save")
	}
	if _, err := os.Stat(filepath.Join(dir, ".codient", "plans", "atomic-test", "plan.json")); err != nil {
		t.Errorf("plan.json not at expected path: %v", err)
	}
}
