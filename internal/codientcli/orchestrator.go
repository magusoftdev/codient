package codientcli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/openai/openai-go/v3"

	"codient/internal/config"
	"codient/internal/intent"
	"codient/internal/openaiclient"
	"codient/internal/prompt"
)

// orchestratedTurn is the per-turn entry point for ModeAuto: classify the user's
// prompt with the low-reasoning supervisor, switch the session into the chosen
// mode for this turn, run executeTurn, and (for COMPLEX_TASK) optionally chain a
// build phase using the existing plan->build handoff machinery. The session is
// restored to ModeAuto when the turn returns so the next prompt is re-classified.
func (s *session) orchestratedTurn(ctx context.Context, userMsg openai.ChatCompletionMessageParamUnion, userText string) (string, error) {
	id := s.identifyIntent(ctx, userText)
	s.printIntentStatus(id)

	target := mapCategoryToMode(id.Category)
	autoMode := s.mode

	defer func() {
		if autoMode == prompt.ModeAuto && s.mode != prompt.ModeAuto {
			// Restore the auto sentinel so the next turn re-classifies.
			s.setMode(prompt.ModeAuto)
		}
	}()

	s.applyOrchestratedMode(target)
	s.lastTurnMode = target
	runner := s.newRunner()
	reply, err := s.executeTurn(ctx, runner, userMsg)
	if err != nil || target != prompt.ModePlan || id.Category != intent.CategoryComplexTask {
		return reply, err
	}

	// Plan turn finished. Decide whether to auto-continue into build.
	if !s.shouldAutoBuildAfterPlan() {
		s.printPlanPauseHint()
		return reply, nil
	}
	return s.runOrchestratedBuildPhase(ctx, reply)
}

// applyOrchestratedMode swaps mode-specific session state (mode, client,
// registry, system prompt) before running a turn in target mode. Delegates to
// the shared transitionToInternalMode helper so structured plan execution and
// the orchestrator stay in sync.
func (s *session) applyOrchestratedMode(target prompt.Mode) {
	s.transitionToInternalMode(target)
}

// runOrchestratedBuildPhase transitions into build mode and runs a second
// turn with a synthetic handoff message as the user input. The transition
// happens FIRST so the handoff text references the build-mode tool registry
// rather than the plan-mode one. Returns the build reply (or planReply on
// error so callers still have something).
func (s *session) runOrchestratedBuildPhase(ctx context.Context, planReply string) (string, error) {
	s.markPlanApprovedForHandoff()
	s.applyOrchestratedMode(prompt.ModeBuild)
	s.lastTurnMode = prompt.ModeBuild

	handoffText := buildPlanHandoffMessage(s.currentPlan, s.lastReply, s.registry.Names())

	if !s.printMode {
		fmt.Fprintln(os.Stderr, "codient: implementing approved plan…")
	}

	runner := s.newRunner()
	buildReply, err := s.executeTurn(ctx, runner, openai.UserMessage(handoffText))
	if err != nil {
		return planReply, err
	}
	return buildReply, nil
}

// identifyIntent runs the low-tier supervisor classifier. Errors are logged but
// not returned: IdentifyIntent always yields a populated Identification (the
// fallback path returns CategoryQuery, which is the safest read-only mode).
func (s *session) identifyIntent(ctx context.Context, userText string) intent.Identification {
	cli := openaiclient.NewForTier(s.cfg, config.TierLow)
	id, err := intent.IdentifyIntent(ctx, cli, userText, intent.Options{Tracker: s.tokenTracker})
	if err != nil && s.cfg.Verbose {
		fmt.Fprintf(s.intentStatusWriter(), "codient: orchestrator: supervisor error: %v (falling back to %s)\n", err, id.Category)
	}
	return id
}

// mapCategoryToMode routes the supervisor's category into a concrete agent mode.
//   - QUERY        -> Ask  (read-only Q&A)
//   - DESIGN       -> Plan (read-only architectural design)
//   - SIMPLE_FIX   -> Build (write-enabled, single-shot)
//   - COMPLEX_TASK -> Plan first; auto-build follows when approved.
func mapCategoryToMode(c intent.Category) prompt.Mode {
	switch c {
	case intent.CategoryDesign, intent.CategoryComplexTask:
		return prompt.ModePlan
	case intent.CategorySimpleFix:
		return prompt.ModeBuild
	default:
		return prompt.ModeAsk
	}
}

// printIntentStatus writes the supervisor decision to the user via stderr (or
// the TUI viewport when active). Suppressed in headless -print non-text output.
func (s *session) printIntentStatus(id intent.Identification) {
	out := s.intentStatusWriter()
	if out == nil {
		return
	}
	cat := string(id.Category)
	reason := strings.TrimSpace(id.Reasoning)
	suffix := ""
	if id.Fallback {
		suffix = " (fallback)"
	}
	if reason == "" {
		fmt.Fprintf(out, "codient: intent: %s%s\n", cat, suffix)
		return
	}
	fmt.Fprintf(out, "codient: intent: %s%s — %s\n", cat, suffix, reason)
}

// intentStatusWriter returns the writer for orchestrator status lines, or nil
// when output should be suppressed (e.g. -print json/stream-json).
func (s *session) intentStatusWriter() io.Writer {
	if s.printMode && (s.outputFormat == "json" || s.outputFormat == "stream-json") {
		return nil
	}
	return os.Stderr
}

// shouldAutoBuildAfterPlan decides whether a COMPLEX_TASK plan should be
// followed by an immediate build phase.
//
//	-force / -yes        -> always yes
//	headless -print      -> no (user should re-run with -force to commit)
//	ACP / non-interactive without scanner -> yes (Unity drives the picker)
//	Interactive REPL/TUI -> prompt the user (Y/n)
//
// The function never returns true when the supervisor produced no actionable
// plan signal (planHandoffApplies). That matches the contract of the removed
// manual /build path.
func (s *session) shouldAutoBuildAfterPlan() bool {
	if !planHandoffApplies(s.currentPlan, s.lastReply) {
		return false
	}
	if s.orchestratorForce {
		return true
	}
	if s.printMode {
		return false
	}
	if s.scanner == nil {
		// ACP / sub-agent path: no interactive prompt available; fall through to auto-build.
		return true
	}
	return s.promptUserForBuildApproval()
}

// promptUserForBuildApproval blocks on stdin (or the TUI input channel) for a
// single-line answer. Empty / Y / yes / go / implement = yes; everything else =
// no. Used in interactive REPL when the orchestrator wants to chain plan->build.
func (s *session) promptUserForBuildApproval() bool {
	if s.scanner == nil {
		return false
	}
	w := s.intentStatusWriter()
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprint(w, "codient: implement this plan in build mode now? [Y/n] ")
	if !s.scanner.Scan() {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(s.scanner.Text()))
	switch answer {
	case "", "y", "yes", "go", "implement", "build":
		return true
	default:
		return false
	}
}

// printPlanPauseHint is shown when COMPLEX_TASK produced a plan but auto-build
// was declined (or unavailable headless). Mirrors the legacy "type 'go' to
// implement" hint from the old REPL plan->build transition.
func (s *session) printPlanPauseHint() {
	out := s.intentStatusWriter()
	if out == nil {
		return
	}
	if s.printMode {
		fmt.Fprintln(out, "codient: plan complete — re-run with -force / -yes to auto-implement the plan.")
		return
	}
	fmt.Fprintln(out, "codient: plan complete — type a follow-up message (or 'go') to implement; the orchestrator will route it to build mode.")
}

// orchestratorTurnDescription produces a short human-readable label like
// "auto -> plan (DESIGN)" used by the status / verbose paths. Exposed for tests.
func orchestratorTurnDescription(id intent.Identification, target prompt.Mode) string {
	if !target.IsResolved() {
		target = prompt.ModeBuild
	}
	reason := strings.TrimSpace(id.Reasoning)
	if reason == "" {
		return fmt.Sprintf("auto -> %s (%s)", target, id.Category)
	}
	return fmt.Sprintf("auto -> %s (%s: %s)", target, id.Category, reason)
}
