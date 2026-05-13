package codientcli

import (
	"strings"
	"testing"

	"codient/internal/planstore"
)

var testTools = []string{"read_file", "write_file", "str_replace", "list_dir"}

func TestBuildPlanHandoffMessage_FallbackIncludesDesignBody(t *testing.T) {
	const body = "UNIQUE_DESIGN_MARKER_9a2f"
	out := buildPlanHandoffMessage(nil, "## Goal\n"+body, testTools)
	if !strings.Contains(out, body) {
		t.Fatalf("expected design body preserved; got:\n%s", out)
	}
}

func TestBuildPlanHandoffMessage_FallbackDirectives(t *testing.T) {
	out := buildPlanHandoffMessage(nil, "# Ready to implement\n\n- [ ] step", testTools)
	lower := strings.ToLower(out)
	for _, phrase := range []string{
		"build mode",
		"do not ask",
		"confirmed",
		"start implementing",
		"verify",
		"ignore any line",
		"run codient",
	} {
		if !strings.Contains(lower, strings.ToLower(phrase)) {
			t.Errorf("expected message to contain %q\n---\n%s", phrase, out)
		}
	}
}

func TestBuildPlanHandoffMessage_FallbackListsTools(t *testing.T) {
	out := buildPlanHandoffMessage(nil, "design", testTools)
	for _, tool := range testTools {
		if !strings.Contains(out, tool) {
			t.Errorf("expected tool %q in message", tool)
		}
	}
	if strings.Contains(out, "run_command") {
		t.Error("should not mention run_command when not in tool list")
	}
}

func TestBuildPlanHandoffMessage_FallbackTrimsDesignWhitespace(t *testing.T) {
	out := buildPlanHandoffMessage(nil, "  \n  hello  \n  ", testTools)
	if !strings.Contains(out, "hello") {
		t.Fatal(out)
	}
}

func TestBuildPlanHandoffMessage_StructuredPlanReplacesFallback(t *testing.T) {
	plan := &planstore.Plan{
		Summary:     "Add a TODO CLI",
		UserRequest: "design a Go TODO app",
		Steps: []planstore.Step{
			{ID: "step-1", Title: "Create cmd/todo main entrypoint", Description: "Wire cobra"},
			{ID: "step-2", Title: "Add JSON storage layer"},
		},
		FilesToModify: []string{"cmd/todo/main.go", "internal/store/store.go"},
		Verification:  []string{"go test ./...", "manual smoke test"},
	}
	const fallback = "raw markdown FALLBACK_MARKER"
	out := buildPlanHandoffMessage(plan, fallback, testTools)
	for _, want := range []string{
		"Approved plan",
		"Add a TODO CLI",
		"design a Go TODO app",
		"cmd/todo/main.go",
		"internal/store/store.go",
		"Create cmd/todo main entrypoint",
		"Wire cobra",
		"Add JSON storage layer",
		"go test ./...",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected structured handoff to contain %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "FALLBACK_MARKER") {
		t.Errorf("structured plan should replace fallback markdown, but it leaked into output\n---\n%s", out)
	}
}

func TestBuildPlanHandoffMessage_FallbackWhenPlanEmpty(t *testing.T) {
	const fallback = "## Ready to implement\n\nFALLBACK_BODY_zz"
	out := buildPlanHandoffMessage(&planstore.Plan{}, fallback, testTools)
	if !strings.Contains(out, "FALLBACK_BODY_zz") {
		t.Fatalf("expected fallback to be used when plan empty\n---\n%s", out)
	}
}

func TestStripRelaunchLines(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		mustDrop  []string
		mustKeep  []string
		mustExist bool
	}{
		{
			name: "run codient with -mode build",
			input: "Step 1: write tests.\n" +
				"When ready, run codient with -mode build to implement this plan.\n" +
				"Step 2: write code.\n",
			mustDrop: []string{"run codient", "-mode build"},
			mustKeep: []string{"Step 1", "Step 2"},
		},
		{
			name:     "switch to build mode prose",
			input:    "Plan summary.\n\nNow switch to build mode and continue.\n\nVerification: tests pass.\n",
			mustDrop: []string{"switch to build mode"},
			mustKeep: []string{"Plan summary", "Verification"},
		},
		{
			name:     "no instructions to strip",
			input:    "Step 1.\nStep 2.\nStep 3.\n",
			mustKeep: []string{"Step 1.", "Step 2.", "Step 3."},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := stripRelaunchLines(tc.input)
			lower := strings.ToLower(out)
			for _, drop := range tc.mustDrop {
				if strings.Contains(lower, strings.ToLower(drop)) {
					t.Errorf("expected %q to be removed; got:\n%s", drop, out)
				}
			}
			for _, keep := range tc.mustKeep {
				if !strings.Contains(out, keep) {
					t.Errorf("expected %q to remain; got:\n%s", keep, out)
				}
			}
		})
	}
}

func TestPlanHandoffApplies(t *testing.T) {
	if !planHandoffApplies(&planstore.Plan{Steps: []planstore.Step{{ID: "1", Title: "x"}}}, "") {
		t.Error("expected handoff to apply when plan has steps")
	}
	if !planHandoffApplies(nil, "## Ready to implement\n- foo") {
		t.Error("expected handoff to apply when reply contains 'Ready to implement'")
	}
	if planHandoffApplies(nil, "just chatting") {
		t.Error("expected no handoff when there is no plan and no ready marker")
	}
	if planHandoffApplies(&planstore.Plan{}, "") {
		t.Error("expected no handoff when plan has no steps")
	}
}

func TestDeterministicPlanReadiness(t *testing.T) {
	readyPlan := &planstore.Plan{
		Summary: "Add feature.",
		Steps: []planstore.Step{
			{ID: "step-1", Title: "Update command"},
		},
		Verification: []string{"go test ./..."},
	}
	if got := deterministicPlanReadiness(readyPlan, "## Ready to implement"); !got.Ready {
		t.Fatalf("expected ready plan, got %+v", got)
	}

	blocked := deterministicPlanReadiness(readyPlan, "## Question\n\nA or B?\n\n**Waiting for your answer**")
	if blocked.Ready {
		t.Fatalf("expected blocking question to reject readiness")
	}

	fallback := &planstore.Plan{
		Steps: []planstore.Step{{
			ID:          "step-1",
			Title:       "Implement plan",
			Description: "Execute the full plan as described in the design document.",
		}},
		Verification: []string{"go test ./..."},
	}
	if got := deterministicPlanReadiness(fallback, "## Ready to implement"); got.Ready {
		t.Fatalf("expected fallback-only plan to reject readiness")
	}

	noVerification := &planstore.Plan{Steps: []planstore.Step{{ID: "step-1", Title: "Do work"}}}
	if got := deterministicPlanReadiness(noVerification, "## Ready to implement"); got.Ready {
		t.Fatalf("expected missing verification to reject readiness")
	}
}
