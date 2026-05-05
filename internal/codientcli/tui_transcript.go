package codientcli

import (
	"strings"
	"unicode/utf8"

	"codient/internal/agent"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

var (
	tuiThinkingHeaderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#EAB308"}).
				Bold(true)
	tuiThinkingBodyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#4B5563", Dark: "#A1A1AA"}).
				Italic(true)
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
	vpHeight := max(1, m.height-tuiFooterHeight)
	m.viewport.Width = m.mainViewportWidth()
	m.viewport.Height = vpHeight
	m.syncViewport()
}

func (m *tuiModel) appendTranscriptEvent(ev agent.TranscriptEvent, delegate bool) {
	prefix := ""
	if delegate {
		prefix = "│ "
	}
	pw := max(16, m.mainViewportWidth()-2)
	if pw < 16 {
		pw = 16
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
			hdr := "Thinking (stream):"
			if !m.plain {
				hdr = tuiThinkingHeaderStyle.Render(hdr)
			}
			writePrefixed(hdr)
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
		if full == "" && ev.ToolName != "" {
			args := ev.ToolArgs
			if args == nil {
				args = []byte("null")
			}
			line := agent.FormatSyntheticIntentThinkingLine(m.plain, m.mode, ev.ToolName, args)
			if strings.TrimSpace(line) != "" {
				if m.plain {
					writePrefixed(line)
				} else {
					writePrefixed(tuiToolLineStyle.Render(line))
				}
			}
			return
		}
		if full == "" {
			return
		}
		if m.thinkingCompact && utf8.RuneCountInString(full) > 480 {
			full = string([]rune(full)[:477]) + "…"
		}
		body := wordwrap.String(full, pw)
		if !m.plain {
			hdr := tuiThinkingHeaderStyle.Render("Thinking:")
			body = tuiThinkingBodyStyle.Width(pw).Render(body)
			writePrefixed(hdr + "\n" + body)
		} else {
			writePrefixed("Thinking:\n" + body)
		}
		m.content.WriteString("\n")

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
