package codientcli

import (
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"

	"codient/internal/config"
	"codient/internal/planstore"
	"codient/internal/prompt"
)

// TestApplyOrchestratedMode_PlanToBuild_HandoffSeesBuildRegistry locks in the
// fix for the plan->build double-handoff / stale-tool-name bug. The orchestrator
// MUST transition the session to ModeBuild before constructing the handoff
// message so the message cites build-mode (write-enabled) tools rather than the
// plan-mode (read-only) tool list. See runOrchestratedBuildPhase.
func TestApplyOrchestratedMode_PlanToBuild_HandoffSeesBuildRegistry(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)

	if hasToolNamed(s.registry, "write_file") {
		t.Fatal("precondition: plan-mode registry should not contain write_file")
	}
	planMsg := buildPlanHandoffMessage(nil, "## Ready to implement\n- step 1\n", s.registry.Names())
	if strings.Contains(planMsg, "write_file") {
		t.Fatal("precondition: plan-mode handoff message must not cite write_file")
	}

	s.applyOrchestratedMode(prompt.ModeBuild)
	if s.mode != prompt.ModeBuild {
		t.Fatalf("expected ModeBuild after transition; got %v", s.mode)
	}
	if !hasToolNamed(s.registry, "write_file") {
		t.Fatal("expected build-mode registry to contain write_file after transition")
	}

	buildMsg := buildPlanHandoffMessage(nil, "## Ready to implement\n- step 1\n", s.registry.Names())
	if !strings.Contains(buildMsg, "write_file") {
		t.Fatalf("post-transition handoff message must cite build-mode tool list (registry=%v)\n%s",
			s.registry.Names(), buildMsg)
	}
}

// TestMarkPlanApprovedForHandoff_NilPlanNoop guards the orchestrator from
// panicking when a COMPLEX_TASK turn produced no structured plan but still
// triggered handoff via the "Ready to implement" marker.
func TestMarkPlanApprovedForHandoff_NilPlanNoop(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)
	s.currentPlan = nil
	s.markPlanApprovedForHandoff()
	if s.planPhase != "" {
		t.Fatalf("expected planPhase to remain empty for nil plan; got %q", s.planPhase)
	}
}

// TestMarkPlanApprovedForHandoff_SetsApprovedPhase verifies that an active
// plan transitions to PhaseApproved and records an approval entry. The plan
// must be associated with a real on-disk workspace+sessionID so planstore.Save
// can persist it.
func TestMarkPlanApprovedForHandoff_SetsApprovedPhase(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)
	s.sessionID = "sess_test_handoff"
	s.currentPlan = &planstore.Plan{
		SessionID: s.sessionID,
		Workspace: s.cfg.Workspace,
		Phase:     planstore.PhaseDraft,
		Summary:   "do the thing",
		Steps:     []planstore.Step{{ID: "1", Title: "x"}},
	}
	s.markPlanApprovedForHandoff()
	if s.currentPlan.Phase != planstore.PhaseApproved {
		t.Fatalf("plan phase: %q want %q", s.currentPlan.Phase, planstore.PhaseApproved)
	}
	if s.planPhase != planstore.PhaseApproved {
		t.Fatalf("session planPhase: %q want %q", s.planPhase, planstore.PhaseApproved)
	}
	if s.currentPlan.Approval == nil {
		t.Fatal("expected approval record")
	}
	if got := s.currentPlan.Approval.Decision; got != "approve" {
		t.Fatalf("approval decision: %q want %q", got, "approve")
	}
}

// TestShouldAutoBuildAfterPlan_ForceFlagBypassesPrompt verifies that the
// orchestrator's -force/-yes shortcut auto-confirms the plan->build handoff
// even when the plan has steps and no scanner is attached.
func TestShouldAutoBuildAfterPlan_ForceFlagBypassesPrompt(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)
	s.currentPlan = &planstore.Plan{Steps: []planstore.Step{{ID: "1", Title: "x"}}}
	s.orchestratorForce = true
	if !s.shouldAutoBuildAfterPlan() {
		t.Fatal("expected -force to auto-approve plan->build")
	}
}

// TestShouldAutoBuildAfterPlan_PrintModeRequiresForce ensures that headless
// -print runs do not silently chain plan->build without explicit -force/-yes
// (the user should re-run to commit, matching the print-mode pause hint).
func TestShouldAutoBuildAfterPlan_PrintModeRequiresForce(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)
	s.currentPlan = &planstore.Plan{Steps: []planstore.Step{{ID: "1", Title: "x"}}}
	s.printMode = true
	if s.shouldAutoBuildAfterPlan() {
		t.Fatal("expected print mode without -force to NOT auto-build")
	}
}

// TestShouldAutoBuildAfterPlan_ACPNoScannerAutoBuilds ensures that the ACP /
// sub-agent path (no stdin scanner attached) auto-builds when a plan applies.
// Unity drives the picker, so the orchestrator should not block on stdin.
func TestShouldAutoBuildAfterPlan_ACPNoScannerAutoBuilds(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)
	s.currentPlan = &planstore.Plan{Steps: []planstore.Step{{ID: "1", Title: "x"}}}
	s.printMode = false
	s.scanner = nil
	if !s.shouldAutoBuildAfterPlan() {
		t.Fatal("ACP / sub-agent path should auto-build when plan applies")
	}
}

func TestCapturePlanReplyForHandoff_ParsesFreshReadyPlan(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)
	s.sessionID = "sess_ready_plan"
	reply := `## Goal

Do the work.

## Implementation steps

1. Update the runner.
2. Add tests.

## Ready to implement

- Start now.`

	s.capturePlanReplyForHandoff(reply, "fix the runner")
	if s.lastReply == "" {
		t.Fatal("expected lastReply to be captured")
	}
	if s.currentPlan == nil {
		t.Fatal("expected currentPlan to be parsed")
	}
	if len(s.currentPlan.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(s.currentPlan.Steps))
	}
	if s.currentPlan.UserRequest != "fix the runner" {
		t.Fatalf("user request = %q", s.currentPlan.UserRequest)
	}
}

func TestCapturePlanReplyForHandoff_ClearsStalePlanWhenNotReady(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)
	s.currentPlan = &planstore.Plan{Steps: []planstore.Step{{ID: "old", Title: "stale"}}}
	s.planPhase = planstore.PhaseDraft

	s.capturePlanReplyForHandoff("## Question\n\nA) Pick one\n\n**Waiting for your answer**", "new task")
	if s.currentPlan != nil {
		t.Fatalf("expected stale plan to be cleared, got %#v", s.currentPlan)
	}
	if s.planPhase != "" {
		t.Fatalf("expected planPhase cleared, got %q", s.planPhase)
	}
	if s.shouldAutoBuildAfterPlan() {
		t.Fatal("not-ready reply must not auto-build via stale plan state")
	}
}

// TestApplyOrchestratedMode_ResetTransition verifies that going back to a
// resolved mode (e.g. Plan -> Build -> Plan within a single orchestrator chain)
// continues to rebuild the registry to the correct contents. Used as a safety
// net against future "skip if same external mode" optimizations that might
// regress the orchestrator's auto-routing per turn.
func TestApplyOrchestratedMode_ResetTransition(t *testing.T) {
	s := newTestSessionForModeSwitch(t, prompt.ModePlan)
	if hasToolNamed(s.registry, "write_file") {
		t.Fatal("plan registry should not have write_file")
	}
	s.applyOrchestratedMode(prompt.ModeBuild)
	if !hasToolNamed(s.registry, "write_file") {
		t.Fatal("build registry should have write_file")
	}
	s.applyOrchestratedMode(prompt.ModePlan)
	if hasToolNamed(s.registry, "write_file") {
		t.Fatal("plan registry (after build) should not have write_file")
	}
}

// TestACPApplyACPMode_SwapsRegistryForResolvedMode is the ACP-side analogue of
// TestApplyOrchestratedMode_PlanToBuild_HandoffSeesBuildRegistry: applyACPMode
// must install build-mode artifacts before orchestrateACPTurn constructs the
// handoff user message. See acpServer.orchestrateACPTurn.
func TestACPApplyACPMode_SwapsRegistryForResolvedMode(t *testing.T) {
	srv, sess := newTestACPServerWithStub(t)

	srv.applyACPMode(sess, prompt.ModePlan)
	if hasToolNamed(sess.registry, "write_file") {
		t.Fatal("plan-mode session must not register write_file")
	}
	if sess.systemPrompt == "" {
		t.Fatal("expected non-empty plan-mode system prompt")
	}

	srv.applyACPMode(sess, prompt.ModeBuild)
	if !hasToolNamed(sess.registry, "write_file") {
		t.Fatalf("build-mode session should register write_file; got tools=%v", sess.registry.Names())
	}
	msg := buildPlanHandoffMessage(nil, "## Ready to implement\n- foo\n", sess.registry.Names())
	if !strings.Contains(msg, "write_file") {
		t.Fatalf("ACP handoff message after build swap must cite write_file:\n%s", msg)
	}
}

// TestACPApplyACPMode_RestoreToAuto verifies that the orchestrator can return
// the ACP session to ModeAuto between turns (handleSessionPrompt's deferred
// restore). The registry then reflects the default-mode artifacts.
func TestACPApplyACPMode_RestoreToAuto(t *testing.T) {
	srv, sess := newTestACPServerWithStub(t)

	srv.applyACPMode(sess, prompt.ModeBuild)
	srv.applyACPMode(sess, prompt.ModeAuto)
	if sess.mode != prompt.ModeAuto {
		t.Fatalf("expected sess.mode == ModeAuto, got %v", sess.mode)
	}
	if sess.registry == nil {
		t.Fatal("expected non-nil registry after auto restore")
	}
}

// newTestACPServerWithStub constructs a minimal acpServer + session pair
// suitable for testing mode-swap helpers without spinning up the full stdio
// transport. The server's cached registry/system prompt are pre-populated so
// modeArtifactsFor(ModeAuto) returns immediately.
func newTestACPServerWithStub(t *testing.T) (*acpServer, *acpChatSession) {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{Workspace: tmp, Plain: true, Model: "gpt-4o-mini"}
	stub := &session{cfg: cfg, mode: prompt.ModeAuto, acpNoDelegate: true}
	stub.installRegistry(buildRegistry(cfg, prompt.ModeAuto, stub, nil))
	srv := &acpServer{
		cfg:      cfg,
		mode:     prompt.ModeAuto,
		stub:     stub,
		sessions: map[string]*acpChatSession{},
	}
	sess := &acpChatSession{
		id:            "sess_test",
		mode:          prompt.ModeAuto,
		systemPrompt:  stub.systemPrompt,
		registry:      stub.registry,
		workspaceRoot: tmp,
	}
	return srv, sess
}

// TestACPApplyACPMode_PreservesHistory verifies that swapping modes for an ACP
// session never mutates the per-session history (parity with the CLI fix).
func TestACPApplyACPMode_PreservesHistory(t *testing.T) {
	srv, sess := newTestACPServerWithStub(t)
	sess.history = []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hi"),
		openai.AssistantMessage("hello"),
	}
	startLen := len(sess.history)

	srv.applyACPMode(sess, prompt.ModeBuild)
	srv.applyACPMode(sess, prompt.ModePlan)
	srv.applyACPMode(sess, prompt.ModeAsk)

	if len(sess.history) != startLen {
		t.Fatalf("ACP applyACPMode mutated history; len=%d want=%d", len(sess.history), startLen)
	}
}

// TestPrintPlanPauseHint_NoLegacyModeFlags guards against regressions where
// the user-facing plan-pause hint reintroduces the long-removed `-mode build`
// flag or `/build` slash command. Both have been replaced by the orchestrator
// auto-routing, so the hint must only mention `-force` / `-yes` and the REPL
// follow-up flow.
func TestPrintPlanPauseHint_NoLegacyModeFlags(t *testing.T) {
	t.Run("interactive", func(t *testing.T) {
		s := newTestSessionForModeSwitch(t, prompt.ModePlan)
		s.printMode = false
		out := captureStderr(t, s.printPlanPauseHint)
		if strings.Contains(out, "-mode build") {
			t.Errorf("hint must not mention `-mode build`: %q", out)
		}
		if strings.Contains(out, "/build") {
			t.Errorf("hint must not mention `/build`: %q", out)
		}
		if !strings.Contains(out, "plan complete") {
			t.Errorf("expected `plan complete` in hint: %q", out)
		}
	})

	t.Run("print mode", func(t *testing.T) {
		s := newTestSessionForModeSwitch(t, prompt.ModePlan)
		s.printMode = true
		out := captureStderr(t, s.printPlanPauseHint)
		if strings.Contains(out, "-mode build") {
			t.Errorf("print-mode hint must not mention `-mode build`: %q", out)
		}
		if !strings.Contains(out, "-force") && !strings.Contains(out, "-yes") {
			t.Errorf("print-mode hint should mention -force / -yes: %q", out)
		}
	})
}
