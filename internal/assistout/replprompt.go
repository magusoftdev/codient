package assistout

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

func stderrIsInteractive() bool {
	if o := tuiOverride.Load(); o != nil {
		return o.StderrInteractive
	}
	st, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (st.Mode() & os.ModeCharDevice) != 0
}

// UserMessageAccentColor returns the accent color used for rendered user
// chat entries in the transcript (bubble border, input panel accent strip,
// REPL "> " prompt). We use the codient brand sky-blue — the same hue as
// the leftmost stop of the welcome logo gradient — so user-authored content
// is visually distinct from agent-side framing, which uses
// [AgentAccentColor] (purple, the rightmost / magenta-leaning stop).
func UserMessageAccentColor() lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: "#0284C7", Dark: "#38BDF8"}
}

// AgentAccentColor returns the accent color used for codient's own agent
// framing in the transcript and chrome: the welcome banner border, the
// "● " progress / intent bullets, and any other place that should read as
// "the codient agent speaking". It pairs with [UserMessageAccentColor]
// (blue) to color-code the two voices in the REPL.
func AgentAccentColor() lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C084FC"}
}

// ProgressIntentBulletPrefix is the leading indent and bullet for assistant
// thinking / intent prose on stderr. The bullet uses the agent accent
// (purple) so the "codient is speaking" voice is consistent regardless of
// which internal mode the orchestrator picked for the current turn. The
// mode parameter is kept for API stability but no longer influences color.
func ProgressIntentBulletPrefix(plain bool, _ string) string {
	return progressBulletPrefix(plain, AgentAccentColor())
}

func progressBulletPrefix(plain bool, fg lipgloss.AdaptiveColor) string {
	const bullet = "●"
	if plain || !stderrIsInteractive() {
		return "  " + bullet + " "
	}
	b := lipgloss.NewStyle().Foreground(fg).Render(bullet)
	return "  " + b + " "
}

// UserPrompt returns the styled REPL prompt for the orchestrator-driven default
// experience: a single chevron in the user-message accent color, no mode label.
// The orchestrator picks an internal mode per turn so we no longer surface a
// mode label at the input line.
func UserPrompt(plain bool) string {
	return styledLabel(plain, "> ", UserMessageAccentColor(), true)
}

// PlanAnswerPrefix returns text to print before stdin when the assistant is
// blocking on a clarifying answer ("Answer: ").
func PlanAnswerPrefix(plain bool) string {
	return styledLabel(plain, "Answer: ", lipgloss.AdaptiveColor{Light: "#0B6E99", Dark: "#7FD7FF"}, true)
}

func styledLabel(plain bool, label string, fg lipgloss.AdaptiveColor, bold bool) string {
	if plain || !stderrIsInteractive() {
		return label
	}
	s := lipgloss.NewStyle().Foreground(fg)
	if bold {
		s = s.Bold(true)
	}
	return s.Render(label)
}
