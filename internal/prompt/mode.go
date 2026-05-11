package prompt

import (
	"fmt"
	"strings"
)

// Mode selects agent behavior (tools + system prompt).
//
// ModeAuto is the orchestrator sentinel: the supervisor classifies each user
// prompt and routes it to one of the resolved modes (Ask / Plan / Build). The
// sentinel must be resolved by the orchestrator before reaching code that
// depends on a concrete tool registry or system prompt (see IsResolved).
type Mode string

const (
	ModeAuto  Mode = "auto"
	ModeBuild Mode = "build"
	ModeAsk   Mode = "ask"
	ModePlan  Mode = "plan"
)

// ParseMode normalizes and validates a mode string.
// Empty string returns ModeAuto so the orchestrator picks per-turn; explicit
// values continue to work as before. "design" stays as a backward-compatible
// alias for "plan" (sessions, config file).
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return ModeAuto, nil
	case "build":
		return ModeBuild, nil
	case "ask":
		return ModeAsk, nil
	case "plan", "design":
		return ModePlan, nil
	default:
		return "", fmt.Errorf("invalid mode %q (want auto, build, ask, or plan)", s)
	}
}

// IsResolved reports whether m is a concrete execution mode (not the
// orchestrator sentinel). buildRegistry / buildAgentSystemPromptEx call this
// to fail fast if they ever receive ModeAuto.
func (m Mode) IsResolved() bool {
	switch m {
	case ModeBuild, ModeAsk, ModePlan:
		return true
	default:
		return false
	}
}
