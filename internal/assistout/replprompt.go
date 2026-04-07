package assistout

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

var modeColors = map[string]lipgloss.AdaptiveColor{
	"build": {Light: "#C2410C", Dark: "#FB923C"}, // orange
	"plan":  {Light: "#0369A1", Dark: "#7DD3FC"}, // blue
	"ask":   {Light: "#15803D", Dark: "#4ADE80"}, // green
}

// SessionPrompt returns a styled REPL prompt showing the current mode, e.g. "[build] > ".
// Used for all modes as the default input prompt between turns.
func SessionPrompt(plain bool, mode string) string {
	label := fmt.Sprintf("[%s] > ", mode)
	color, ok := modeColors[mode]
	if !ok {
		color = modeColors["build"]
	}
	return styledLabel(plain, label, color, true)
}

// ModeHint returns a short description of the mode and how to use it.
func ModeHint(plain bool, mode string) string {
	var text string
	switch mode {
	case "build":
		text = "Build mode — full read/write tools. Ask the agent to implement, refactor, or fix code."
	case "plan":
		text = "Plan mode — read-only tools. Describe what you want built and the agent will draft an implementation design, asking clarifying questions along the way."
	case "ask":
		text = "Ask mode — read-only tools. Ask questions about your codebase, libraries, or concepts."
	default:
		return ""
	}
	color, ok := modeColors[mode]
	if !ok {
		color = modeColors["build"]
	}
	return styledLabel(plain, text, color, false)
}

// PlanAnswerPrefix returns text to print before stdin when the assistant is
// blocking on a clarifying answer ("Answer: ").
func PlanAnswerPrefix(plain bool) string {
	return styledLabel(plain, "Answer: ", lipgloss.AdaptiveColor{Light: "#0B6E99", Dark: "#7FD7FF"}, true)
}

func styledLabel(plain bool, label string, fg lipgloss.AdaptiveColor, bold bool) string {
	if plain {
		return label
	}
	st, err := os.Stderr.Stat()
	if err != nil || (st.Mode()&os.ModeCharDevice) == 0 {
		return label
	}
	s := lipgloss.NewStyle().Foreground(fg)
	if bold {
		s = s.Bold(true)
	}
	return s.Render(label)
}
