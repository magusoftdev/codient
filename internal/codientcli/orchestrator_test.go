package codientcli

import (
	"testing"

	"codient/internal/intent"
	"codient/internal/prompt"
)

func TestMapCategoryToMode(t *testing.T) {
	cases := []struct {
		in   intent.Category
		want prompt.Mode
	}{
		{intent.CategoryQuery, prompt.ModeAsk},
		{intent.CategoryDesign, prompt.ModePlan},
		{intent.CategorySimpleFix, prompt.ModeBuild},
		{intent.CategoryComplexTask, prompt.ModePlan},
		{"WAT", prompt.ModeAsk}, // unknown / malformed -> safest path
	}
	for _, tc := range cases {
		got := mapCategoryToMode(tc.in)
		if got != tc.want {
			t.Errorf("mapCategoryToMode(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestOrchestratorTurnDescription(t *testing.T) {
	id := intent.Identification{Category: intent.CategoryComplexTask, Reasoning: "multi-file refactor"}
	got := orchestratorTurnDescription(id, prompt.ModePlan)
	want := "auto -> plan (COMPLEX_TASK: multi-file refactor)"
	if got != want {
		t.Fatalf("description = %q want %q", got, want)
	}
	idShort := intent.Identification{Category: intent.CategoryQuery}
	got = orchestratorTurnDescription(idShort, prompt.ModeAsk)
	want = "auto -> ask (QUERY)"
	if got != want {
		t.Fatalf("short description = %q want %q", got, want)
	}
	// Unresolved target should fall back to build.
	got = orchestratorTurnDescription(idShort, prompt.ModeAuto)
	if got != "auto -> build (QUERY)" {
		t.Fatalf("unresolved fallback: got %q", got)
	}
}

func TestPromptUserForBuildApproval_NoScannerReturnsFalse(t *testing.T) {
	s := &session{}
	if s.promptUserForBuildApproval() {
		t.Fatal("expected false when scanner is nil")
	}
}

func TestShouldAutoBuildAfterPlan_NoPlanFalse(t *testing.T) {
	s := &session{}
	if s.shouldAutoBuildAfterPlan() {
		t.Fatal("expected false when no plan handoff applies")
	}
}
