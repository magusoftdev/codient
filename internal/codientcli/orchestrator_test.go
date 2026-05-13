package codientcli

import (
	"context"
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
	if s.shouldAutoBuildAfterPlan(context.Background()) {
		t.Fatal("expected false when no plan handoff applies")
	}
}

// TestStartSupervisorIndicator_NoTUI_NoTTY_ReturnsNoopStop verifies the
// indicator helper is safe to call in non-interactive runs (no TUI, no
// stderr TTY): it must return a non-nil stop function that is idempotent and
// must not panic. This is the path executeTurn relies on for headless
// `-print` runs and CI.
func TestStartSupervisorIndicator_NoTUI_NoTTY_ReturnsNoopStop(t *testing.T) {
	s := &session{}
	stop := s.startSupervisorIndicator()
	if stop == nil {
		t.Fatal("startSupervisorIndicator must return a non-nil stop function")
	}
	// Idempotent: calling stop multiple times must not panic.
	stop()
	stop()
}

// TestFormatIntentStatusLine covers the on-screen `codient: intent: …`
// formatting for each Source / Fallback combination, including the new
// `(heuristic)` tag for the pre-LLM fast path.
func TestFormatIntentStatusLine(t *testing.T) {
	cases := []struct {
		name string
		id   intent.Identification
		want string
	}{
		{
			name: "supervisor with reason",
			id:   intent.Identification{Category: intent.CategoryQuery, Reasoning: "explain", Source: intent.SourceSupervisor},
			want: "codient: intent: QUERY — explain",
		},
		{
			name: "supervisor without reason",
			id:   intent.Identification{Category: intent.CategorySimpleFix, Source: intent.SourceSupervisor},
			want: "codient: intent: SIMPLE_FIX",
		},
		{
			name: "heuristic fast-path tagged",
			id:   intent.Identification{Category: intent.CategoryDesign, Reasoning: "design phrase: create a plan", Source: intent.SourceHeuristic},
			want: "codient: intent: DESIGN (heuristic) — design phrase: create a plan",
		},
		{
			name: "fallback wins over heuristic tag",
			id:   intent.Identification{Category: intent.CategoryQuery, Reasoning: "supervisor: parse error", Fallback: true, Source: intent.SourceHeuristicFallback},
			want: "codient: intent: QUERY (fallback) — supervisor: parse error",
		},
		{
			name: "empty source falls through (no tag)",
			id:   intent.Identification{Category: intent.CategoryComplexTask, Reasoning: "x"},
			want: "codient: intent: COMPLEX_TASK — x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatIntentStatusLine(tc.id)
			if got != tc.want {
				t.Fatalf("formatIntentStatusLine = %q, want %q", got, tc.want)
			}
		})
	}
}
