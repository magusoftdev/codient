package prompt

import (
	"fmt"
	"os"
	"strings"
)

// Mode selects agent behavior (tools + system prompt). See -mode and CODIENT_MODE.
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
		// "design" is accepted for backward compatibility (sessions, CODIENT_MODE).
		return ModePlan, nil
	default:
		return "", fmt.Errorf("invalid mode %q (want build, ask, or plan)", s)
	}
}

// ResolveMode uses flagValue when non-empty; otherwise CODIENT_MODE; default build.
func ResolveMode(flagValue string) (Mode, error) {
	s := strings.TrimSpace(flagValue)
	if s == "" {
		s = strings.TrimSpace(os.Getenv("CODIENT_MODE"))
	}
	return ParseMode(s)
}
