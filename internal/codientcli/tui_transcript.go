package codientcli

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"codient/internal/agent"
	"codient/internal/assistout"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
	"github.com/muesli/reflow/wrap"
)

var (
	tuiReasoningStreamStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"}).
				Italic(true)
	tuiToolLineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#374151", Dark: "#D1D5DB"})
	tuiDimLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#888888"})
	tuiWarnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"})
	tuiTodoTitleStyle = lipgloss.NewStyle().Bold(true)
)

// wordwrapUserInput wraps each line / paragraph for the user prompt box.
func wordwrapUserInput(text string, width int) string {
	if width < 8 {
		width = 8
	}
	paras := strings.Split(text, "\n")
	out := make([]string, 0, len(paras))
	for _, p := range paras {
		p = strings.TrimRight(p, "\r")
		if strings.TrimSpace(p) == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wordwrap.String(strings.TrimSpace(p), width))
	}
	return strings.Join(out, "\n")
}

// appendUserPromptBlock renders the submitted user message like a chat bubble:
// rounded outline, elevated background, mode-colored vertical accent on the left.
func (m *tuiModel) appendUserPromptBlock(text string) {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return
	}
	if m.plain {
		m.content.WriteString("\n")
		m.content.WriteString(assistout.SessionPrompt(true, m.mode))
		m.content.WriteString(text)
		m.content.WriteString("\n")
		return
	}
	pw := m.mainViewportWidth()
	if pw < 16 {
		pw = m.width
		if pw < 16 {
			pw = 72
		}
	}
	innerW := pw - 8
	if innerW < 12 {
		innerW = 12
	}
	wrapped := wordwrapUserInput(text, innerW)

	accent := assistout.ModeAccentColor(m.mode)
	innerFg := lipgloss.AdaptiveColor{Light: "#18181B", Dark: "#FAFAFA"}
	innerBg := lipgloss.AdaptiveColor{Light: "#F4F4F5", Dark: "#27272A"}
	borderFg := lipgloss.AdaptiveColor{Light: "#A1A1AA", Dark: "#52525B"}

	inner := lipgloss.NewStyle().
		Foreground(innerFg).
		Background(innerBg).
		Padding(1, 2).
		Width(innerW).
		Render(wrapped)

	h := lipgloss.Height(inner)
	if h < 1 {
		h = 1
	}
	accentStyle := lipgloss.NewStyle().
		Background(accent).
		Foreground(accent)
	var barCol strings.Builder
	for i := 0; i < h; i++ {
		if i > 0 {
			barCol.WriteByte('\n')
		}
		barCol.WriteString(accentStyle.Width(2).Render("  "))
	}

	joined := lipgloss.JoinHorizontal(lipgloss.Top, barCol.String(), inner)
	boxed := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderFg).
		Render(joined)

	m.content.WriteString("\n")
	m.content.WriteString(boxed)
	m.content.WriteString("\n")
}

func (m *tuiModel) todoSidebarWidth() int {
	if len(m.todos) == 0 {
		return 0
	}
	if m.width < 50 {
		return 0
	}
	w := min(28, max(18, m.width/4))
	if m.width-w-2 < 24 {
		w = max(12, m.width/5)
	}
	if m.width-w-2 < 20 {
		return 0
	}
	return w
}

func (m *tuiModel) mainViewportWidth() int {
	tw := m.todoSidebarWidth()
	if tw == 0 {
		return m.width
	}
	w := m.width - tw - 1
	if w < 10 {
		return m.width
	}
	return w
}

func (m *tuiModel) applyViewportLayout() {
	if !m.ready {
		return
	}
	vpHeight := max(1, m.height-m.footerHeight())
	m.viewport.Width = m.mainViewportWidth()
	m.viewport.Height = vpHeight
	m.syncViewport()
}

func (m *tuiModel) appendTranscriptEvent(ev agent.TranscriptEvent, delegate bool) {
	prefix := ""
	if delegate {
		prefix = "│ "
	}

	flushStream := func() {
		if m.inReasoningStream {
			m.content.WriteString("\n")
			m.inReasoningStream = false
		}
	}

	writePrefixed := func(s string) {
		for _, line := range strings.Split(s, "\n") {
			m.content.WriteString(prefix)
			m.content.WriteString(line)
			m.content.WriteString("\n")
		}
	}

	switch ev.Kind {
	case agent.TranscriptReasoningDelta:
		if strings.TrimSpace(ev.Text) == "" {
			return
		}
		if !m.inReasoningStream {
			m.content.WriteString("\n")
			m.content.WriteString(prefix)
			m.content.WriteString(assistout.ProgressIntentBulletPrefix(m.plain, m.mode))
			m.inReasoningStream = true
		}
		frag := ev.Text
		if !m.plain {
			frag = tuiReasoningStreamStyle.Render(ev.Text)
		}
		m.content.WriteString(prefix)
		m.content.WriteString(frag)
		return

	case agent.TranscriptAssistantPreface:
		flushStream()
		full := strings.TrimSpace(ev.ThinkingFull)
		if full == "" {
			full = strings.TrimSpace(agent.FormatFullThinkingProse(ev.AssistantProse))
		}
		if full == "" {
			return
		}
		if m.thinkingCompact && utf8.RuneCountInString(full) > 480 {
			full = string([]rune(full)[:477]) + "…"
		}
		line := agent.FormatThinkingProgressLine(m.plain, m.mode, full)
		if strings.TrimSpace(line) == "" {
			return
		}
		m.content.WriteString("\n")
		if m.plain {
			writePrefixed(line)
		} else {
			writePrefixed(tuiToolLineStyle.Render(line))
		}

	case agent.TranscriptToolIntent:
		flushStream()
		if ev.ToolName == "" {
			return
		}
		args := ev.ToolArgs
		if args == nil {
			args = []byte("null")
		}
		line := agent.FormatToolIntentProgressLine(ev.ToolName, args)
		if m.plain {
			writePrefixed(line)
		} else {
			writePrefixed(tuiToolLineStyle.Render(line))
		}

	case agent.TranscriptToolResult:
		flushStream()
		s := strings.TrimRight(ev.Text, "\n")
		if s == "" {
			return
		}
		if m.plain {
			writePrefixed(s)
		} else {
			writePrefixed(tuiDimLineStyle.Render(s))
		}

	case agent.TranscriptRoundSummary:
		flushStream()
		mid := strings.Join(ev.RoundToolParts, " · ")
		if mid == "" {
			mid = "reply"
		}
		line := agent.FormatProgressRoundLineForTranscript(ev.RoundLLMDur, mid, ev.RoundUsageSuf)
		if line != "" {
			if m.plain {
				writePrefixed(line)
			} else {
				writePrefixed(tuiDimLineStyle.Render(line))
			}
		}

	case agent.TranscriptAutoCheck:
		flushStream()
		if strings.TrimSpace(ev.Text) != "" {
			line := agent.ProgressNestedIndent + strings.TrimSpace(ev.Text)
			if m.plain {
				writePrefixed(line)
			} else {
				writePrefixed(tuiDimLineStyle.Render(line))
			}
		}

	case agent.TranscriptStatus:
		flushStream()
		if strings.TrimSpace(ev.Text) == "" {
			return
		}
		line := agent.FormatStatusProgressLine(m.plain, m.mode, strings.TrimSpace(ev.Text))
		if line != "" {
			if m.plain {
				writePrefixed(line)
			} else {
				writePrefixed(tuiDimLineStyle.Render(line))
			}
		}

	case agent.TranscriptPlain:
		flushStream()
		if strings.TrimSpace(ev.Text) == "" {
			return
		}
		writePrefixed(strings.TrimSpace(ev.Text))

	case agent.TranscriptWarning:
		flushStream()
		if strings.TrimSpace(ev.Text) == "" {
			return
		}
		s := "⚠ " + strings.TrimSpace(ev.Text)
		if m.plain {
			writePrefixed(s)
		} else {
			writePrefixed(tuiWarnStyle.Render(s))
		}

	case agent.TranscriptModelError:
		flushStream()
		line := agent.FormatModelErrorProgressLine(m.plain, ev.RoundLLMDur, ev.Text)
		if line != "" {
			if m.plain {
				writePrefixed(line)
			} else {
				writePrefixed(tuiWarnStyle.Render(line))
			}
		}
	}
}

func (m *tuiModel) renderTodoPanel(sideWidth int) string {
	if len(m.todos) == 0 {
		return ""
	}
	var b strings.Builder
	title := "Todo"
	if !m.plain {
		title = tuiTodoTitleStyle.Render(title)
	}
	b.WriteString(title)
	b.WriteString("\n")
	for _, t := range m.todos {
		mark := "○"
		switch strings.ToLower(strings.TrimSpace(t.Status)) {
		case "in_progress":
			mark = "◐"
		case "completed":
			mark = "✓"
		case "cancelled":
			mark = "⊘"
		}
		line := mark + " " + strings.TrimSpace(t.Content)
		if sideWidth > 3 {
			if utf8.RuneCountInString(line) > sideWidth-1 {
				line = string([]rune(line)[:max(1, sideWidth-4)]) + "…"
			}
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	s := strings.TrimSuffix(b.String(), "\n")
	if m.plain {
		return s
	}
	return lipgloss.NewStyle().Width(sideWidth).Render(s)
}

func (m *tuiModel) composeMainRow(vpView string) string {
	tw := m.todoSidebarWidth()
	if tw == 0 {
		return vpView
	}
	mainW := m.mainViewportWidth()
	right := m.renderTodoPanel(tw)
	if !m.plain {
		right = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#4B5563"}).
			PaddingLeft(1).
			Width(tw + 2).
			Render(right)
	} else {
		right = "| " + right
	}
	left := lipgloss.NewStyle().Width(mainW).Render(vpView)
	if m.plain {
		return left + right
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// ---------------------------------------------------------------------------
// Indent-aware viewport wrapping
// ---------------------------------------------------------------------------

// reAnsiCSI matches ANSI CSI escape sequences (SGR colour codes, cursor
// movement, etc.) so they can be stripped for visible-width measurement.
var reAnsiCSI = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// isPrefixRune returns true for characters commonly used as line-start
// decoration in the TUI: bullets, borders, spinners, and warning signs.
func isPrefixRune(r rune) bool {
	switch r {
	case '●', '◐', '▸', '▹', '•', '·', '○', '✓', '⊘',
		'│', '─',
		'⚠',
		'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏':
		return true
	}
	return false
}

// hangingIndentWidth measures the visible width of the leading prefix
// characters (whitespace, bullets, box-drawing, spinners) in a line that
// may contain ANSI escape codes.
func hangingIndentWidth(line string) int {
	visible := reAnsiCSI.ReplaceAllString(line, "")
	var prefix []rune
	for _, r := range visible {
		if r == ' ' || r == '\t' || isPrefixRune(r) {
			prefix = append(prefix, r)
		} else {
			break
		}
	}
	if len(prefix) == 0 {
		return 0
	}
	return lipgloss.Width(string(prefix))
}

// indentAwareWrap word-wraps s at width, preserving leading indentation on
// continuation lines so wrapped text stays visually aligned with its parent
// line's content rather than resetting to column 0.
//
// The first line of each paragraph uses the full width. Continuation lines
// receive a hanging indent equal to the parent's prefix (whitespace +
// bullets / box-drawing characters), with their content re-wrapped at
// width − indentWidth so the total never exceeds the viewport.
func indentAwareWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if lipgloss.Width(line) <= width {
			out = append(out, line)
			continue
		}
		indentW := hangingIndentWidth(line)
		innerW := width - indentW
		if indentW == 0 || innerW < 10 {
			out = append(out, strings.Split(
				wrap.String(wordwrap.String(line, width), width), "\n")...)
			continue
		}
		wrapped := wrap.String(wordwrap.String(line, width), width)
		subs := strings.Split(wrapped, "\n")
		out = append(out, subs[0])
		pad := strings.Repeat(" ", indentW)
		for _, sub := range subs[1:] {
			indented := pad + sub
			if lipgloss.Width(indented) <= width {
				out = append(out, indented)
			} else {
				rewrapped := wrap.String(wordwrap.String(sub, innerW), innerW)
				for _, rs := range strings.Split(rewrapped, "\n") {
					out = append(out, pad+rs)
				}
			}
		}
	}
	return strings.Join(out, "\n")
}
