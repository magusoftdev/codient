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
		m.content.WriteString(assistout.UserPrompt(true))
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

	accent := assistout.UserMessageAccentColor()
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
		Width(innerW + 4).
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

// isDecoratedLine reports whether line has a non-whitespace prefix decoration
// (bullet, box-drawing, spinner, warning) that marks it as a structured TUI
// element rather than plain prose. Decorated lines must not be joined with
// adjacent lines during the re-flow pre-pass.
func isDecoratedLine(line string) bool {
	visible := reAnsiCSI.ReplaceAllString(line, "")
	for _, r := range visible {
		if r == ' ' || r == '\t' {
			continue
		}
		return isPrefixRune(r)
	}
	return false
}

// looksLikeCodeFence returns true when the visible content of line starts
// with ``` or ~~~, indicating a fenced code block boundary.
func looksLikeCodeFence(line string) bool {
	visible := strings.TrimLeft(reAnsiCSI.ReplaceAllString(line, ""), " \t")
	return strings.HasPrefix(visible, "```") || strings.HasPrefix(visible, "~~~")
}

// looksLikeTableRow returns true when the visible content of line looks like
// a Markdown table row (contains a | character that is not part of a box
// border used as a delegation prefix).
func looksLikeTableRow(line string) bool {
	visible := reAnsiCSI.ReplaceAllString(line, "")
	// A delegation prefix is "│ " at the very start. Table rows have | in
	// the middle of the content.
	trimmed := strings.TrimLeft(visible, " \t")
	if strings.HasPrefix(trimmed, "│ ") || strings.HasPrefix(trimmed, "| ") {
		// Could be a delegation prefix — check if there is another | later.
		rest := trimmed[2:]
		return strings.ContainsRune(rest, '|') || strings.ContainsRune(rest, '│')
	}
	return strings.ContainsRune(trimmed, '|') || strings.ContainsRune(trimmed, '│')
}

// looksLikeHeading returns true for lines that look like markdown headings
// (leading # after stripping ANSI) or glamour-rendered headings (lines that
// are entirely bold/upper-cased surrounded by blank lines). We use a simple
// heuristic: visible content starts with '#'.
func looksLikeHeading(line string) bool {
	visible := strings.TrimLeft(reAnsiCSI.ReplaceAllString(line, ""), " \t")
	return strings.HasPrefix(visible, "#")
}

// isJoinable reports whether line is plain prose that may be joined with
// adjacent lines during the re-flow pre-pass. A line is joinable when it:
//   - is non-empty
//   - carries no leading decoration (bullet, box-drawing, etc.)
//   - is not a code fence
//   - is not a table row
//   - is not a heading
//   - has no leading indentation beyond a single space (indented blocks such
//     as blockquotes and code-without-fence are left alone)
func isJoinable(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	if isDecoratedLine(line) {
		return false
	}
	if looksLikeCodeFence(line) {
		return false
	}
	if looksLikeTableRow(line) {
		return false
	}
	if looksLikeHeading(line) {
		return false
	}
	// Lines with 2+ leading spaces are treated as intentionally indented
	// (blockquotes, code, etc.) and are not joined.
	visible := reAnsiCSI.ReplaceAllString(line, "")
	return !strings.HasPrefix(visible, "  ")
}

// endsWithParagraphTerminator reports whether the visible content of line
// ends with punctuation that signals the end of a logical message or
// paragraph: ".", "!", "?", or ")". The check ignores trailing whitespace
// and ANSI escape codes so styled status lines are treated the same as
// raw ones.
func endsWithParagraphTerminator(line string) bool {
	visible := strings.TrimRight(reAnsiCSI.ReplaceAllString(line, ""), " \t")
	if visible == "" {
		return false
	}
	last := visible[len(visible)-1]
	switch last {
	case '.', '!', '?', ')':
		return true
	}
	return false
}

// looksLikeStatusMessage reports whether the visible content of line begins
// with a known status prefix that always identifies a discrete CLI message
// (today: "codient:"). Such lines should never be joined with whatever
// preceded them in the viewport.
func looksLikeStatusMessage(line string) bool {
	visible := strings.TrimLeft(reAnsiCSI.ReplaceAllString(line, ""), " \t")
	return strings.HasPrefix(visible, "codient:")
}

// joinProseLines merges adjacent joinable lines that are shorter than width
// into a single line so that a subsequent wrap pass can re-flow them at the
// current terminal width. This is the inverse of word-wrapping: it undoes
// hard newlines that were inserted by a previous wrap at a narrower width.
//
// Two consecutive non-blank joinable lines are joined with a single space.
// A blank line (or any non-joinable line) resets the accumulator so that
// paragraph boundaries, decorated lines, code blocks, and table rows are
// always preserved.
func joinProseLines(lines []string, width int) []string {
	out := make([]string, 0, len(lines))
	acc := ""        // current accumulated joinable run
	inCode := false  // inside a fenced code block

	flush := func() {
		if acc != "" {
			out = append(out, acc)
			acc = ""
		}
	}

	for _, line := range lines {
		// Track code fence state on the raw line.
		if looksLikeCodeFence(line) {
			flush()
			out = append(out, line)
			inCode = !inCode
			continue
		}
		if inCode {
			flush()
			out = append(out, line)
			continue
		}

		blank := strings.TrimSpace(reAnsiCSI.ReplaceAllString(line, "")) == ""
		if blank {
			flush()
			out = append(out, line)
			continue
		}

		if !isJoinable(line) {
			flush()
			out = append(out, line)
			continue
		}

		// Plain prose: join with previous accumulator if both are short
		// enough that they came from a previous wrap pass (heuristic:
		// the accumulated line is shorter than width-1).
		//
		// Skip the join when:
		//   - acc ends with sentence-terminating or closing punctuation
		//     (".", "!", "?", ")"). Word-wrapped continuations of a
		//     single paragraph almost never end that way (wraps break at
		//     inter-word spaces, not after sentence-final punctuation),
		//     but mode hints and most "codient:" status lines do.
		//   - line is a status message (starts with "codient:"). These are
		//     always discrete CLI events and must not be glued onto a
		//     previous line.
		// Both signals keep distinct stderr writes on distinct lines while
		// still allowing genuine wrap continuations to re-flow.
		if acc == "" {
			acc = line
		} else if endsWithParagraphTerminator(acc) || looksLikeStatusMessage(line) {
			flush()
			acc = line
		} else if lipgloss.Width(acc) < width-1 {
			sep := " "
			if strings.HasSuffix(acc, " ") || strings.HasPrefix(line, " ") {
				sep = ""
			}
			acc = acc + sep + line
		} else {
			flush()
			acc = line
		}
	}
	flush()
	return out
}

// collapseBlankLines reduces runs of more than maxRun consecutive blank lines
// to exactly maxRun blank lines. This prevents the "extra spaces between
// paragraphs" artifact that arises when word-wrapping inserts newlines into
// content that already has newline terminators.
func collapseBlankLines(lines []string, maxRun int) []string {
	out := make([]string, 0, len(lines))
	run := 0
	for _, line := range lines {
		if strings.TrimSpace(reAnsiCSI.ReplaceAllString(line, "")) == "" {
			run++
			if run <= maxRun {
				out = append(out, line)
			}
		} else {
			run = 0
			out = append(out, line)
		}
	}
	return out
}

// indentAwareWrap word-wraps s at width, preserving leading indentation on
// continuation lines so wrapped text stays visually aligned with its parent
// line's content rather than resetting to column 0.
//
// Before wrapping, a join pre-pass merges adjacent short prose lines that
// were produced by a previous wrap at a narrower width. This allows the
// viewport to re-flow correctly when the terminal is widened. A blank-line
// collapse post-pass then ensures no more than one consecutive blank line
// appears between paragraphs, preventing the extra-spacing artefact.
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

	// Pre-pass: re-join prose lines that were split by a previous narrower wrap.
	lines = joinProseLines(lines, width)

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

	// Post-pass: collapse runs of blank lines to at most 1 consecutive blank.
	out = collapseBlankLines(out, 1)

	return strings.Join(out, "\n")
}
