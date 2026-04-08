package prompt

import (
	"fmt"
	"strings"
)

// Mode selects agent behavior (tools + system prompt).
type Mode string

const (
	ModeBuild Mode = "build"
	ModeAsk   Mode = "ask"
	ModePlan  Mode = "plan"
)

// ParseMode normalizes and validates a mode string. Empty string means build.
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "build":
		return ModeBuild, nil
	case "ask":
		return ModeAsk, nil
	case "plan", "design":
		// "design" is accepted for backward compatibility (sessions, config file).
		return ModePlan, nil
	default:
		return "", fmt.Errorf("invalid mode %q (want build, ask, or plan)", s)
	}
}
