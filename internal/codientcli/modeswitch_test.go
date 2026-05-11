package codientcli

import (
	"testing"

	"github.com/openai/openai-go/v3"

	"codient/internal/config"
	"codient/internal/planstore"
	"codient/internal/prompt"
	"codient/internal/tools"
)

// helperPlanWithSteps returns a non-empty plan suitable for testing handoff
// rendering. It exists in this file rather than designhandoff_test.go because
// modeswitch tests use it for transition-related assertions.
func helperPlanWithSteps(workspace, sid string) *planstore.Plan {
	return &planstore.Plan{
		SessionID: sid,
		Workspace: workspace,
		Phase:     planstore.PhaseDraft,
		Summary:   "implement TODO list",
		Steps: []planstore.Step{
			{ID: "s1", Title: "Create cmd/todo main"},
			{ID: "s2", Title: "Add JSON store"},
		},
		Verification: []string{"go test ./..."},
	}
}

func newTestSessionForModeSwitch(t *testing.T, mode prompt.Mode) *session {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{Workspace: tmp, Plain: true, Model: "gpt-4o-mini"}
	s := &session{cfg: cfg, mode: mode}
	s.installRegistry(buildRegistry(cfg, mode, s, nil))
	return s
}

// transitionToInternalMode is a pure runtime artifact swap — it must not
// mutate conversation history. The orchestrator's plan -> build chain owns
// the handoff message injection so it can build it AFTER the transition (with
// the correct build-mode tool registry). See runOrchestratedBuildPhase.
func TestTransitionToInternalMode_PlanToBuild_NeverMutatesHistory(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)
	s.history = []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("design a TODO CLI"),
		openai.AssistantMessage("## Plan\nReady to implement"),
	}
	s.lastReply = "## Plan\nReady to implement"
	s.currentPlan = helperPlanWithSteps(s.cfg.Workspace, "test-sid")
	s.sessionID = "test-sid"
	startLen := len(s.history)

	s.transitionToInternalMode(prompt.ModeBuild)

	if s.mode != prompt.ModeBuild {
		t.Fatalf("expected build mode, got %v", s.mode)
	}
	if len(s.history) != startLen {
		t.Fatalf("transition should not mutate history; len=%d want=%d", len(s.history), startLen)
	}
}

// When plan -> build is requested without an active plan, transitionToInternalMode
// should still switch the mode and rebuild artifacts but leave history untouched.
func TestTransitionToInternalMode_PlanToBuild_NoHandoffWhenNoPlan(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)
	s.history = []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("design something"),
		openai.AssistantMessage("Hmm, let me think about it."),
	}
	s.lastReply = "Hmm, let me think about it."

	startLen := len(s.history)
	s.transitionToInternalMode(prompt.ModeBuild)

	if s.mode != prompt.ModeBuild {
		t.Fatalf("expected build mode, got %v", s.mode)
	}
	if len(s.history) != startLen {
		t.Errorf("expected history len unchanged, got %d (was %d)", len(s.history), startLen)
	}
}

// build -> plan must not mutate history either.
func TestTransitionToInternalMode_BuildToPlan_DoesNotMutateHistory(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModeBuild)
	s.history = []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hi"),
	}
	startLen := len(s.history)

	s.transitionToInternalMode(prompt.ModePlan)

	if s.mode != prompt.ModePlan {
		t.Fatalf("expected plan mode, got %v", s.mode)
	}
	if len(s.history) != startLen {
		t.Errorf("history len changed after build->plan transition: %d -> %d", startLen, len(s.history))
	}
}

// Re-entering the same mode is a no-op (no registry rebuild, no client swap).
func TestTransitionToInternalMode_SameMode_NoOp(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModeBuild)
	regBefore := s.registry

	s.transitionToInternalMode(prompt.ModeBuild)

	if s.registry != regBefore {
		t.Fatal("expected registry pointer to remain stable for same-mode transition")
	}
}

// transitionToInternalMode must rebuild the registry to match the new mode.
// Plan / Ask are read-only and must not expose write tools; Build does.
func TestTransitionToInternalMode_RebuildsRegistryForMode(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)
	if hasToolNamed(s.registry, "write_file") {
		t.Fatal("plan registry should not include write_file")
	}
	s.transitionToInternalMode(prompt.ModeBuild)
	if !hasToolNamed(s.registry, "write_file") {
		t.Fatal("expected build registry to include write_file after transition from plan")
	}
	s.transitionToInternalMode(prompt.ModeAsk)
	if hasToolNamed(s.registry, "write_file") {
		t.Fatal("ask registry should not include write_file after transition from build")
	}
}

func hasToolNamed(reg *tools.Registry, name string) bool {
	if reg == nil {
		return false
	}
	for _, n := range reg.Names() {
		if n == name {
			return true
		}
	}
	return false
}
