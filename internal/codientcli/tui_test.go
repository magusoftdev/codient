package codientcli

import (
	"strings"
	"testing"

	"codient/internal/agent"
	"codient/internal/slashcmd"
	"codient/internal/tools"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestTUIModel_UserPromptBlockPlain(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)
	updated, _ = m.Update(tuiUserPromptMsg("hello"))
	m = updated.(tuiModel)
	s := m.content.String()
	if !strings.Contains(s, "hello") || !strings.Contains(s, "[ask]") {
		t.Fatalf("expected plain transcript echo, got %q", s)
	}
}

func TestTUIModel_UserPromptBlockBorderWidth(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	ic := newInputCloser()
	m := newTUIModel(ic, "build", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)

	m.appendUserPromptBlock("Please create a plan to fix both bugs")
	rendered := m.content.String()
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")

	var widths []int
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		widths = append(widths, lipgloss.Width(line))
	}
	if len(widths) < 3 {
		t.Fatalf("expected at least 3 non-empty lines (top border, content, bottom border), got %d", len(widths))
	}
	maxW := 0
	for _, w := range widths {
		if w > maxW {
			maxW = w
		}
	}
	for i, w := range widths {
		if w != maxW {
			t.Fatalf("line %d has width %d, want %d (all lines should be the same width); rendered:\n%s",
				i, w, maxW, rendered)
		}
	}
}

func TestTUIModel_OutputAppendsToViewport(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)

	// Simulate window size (makes model ready).
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)
	if !m.ready {
		t.Fatal("model should be ready after WindowSizeMsg")
	}

	// Send output.
	updated, _ = m.Update(tuiOutputMsg("hello world\n"))
	m = updated.(tuiModel)

	if !strings.Contains(m.content.String(), "hello world") {
		t.Fatalf("viewport content should contain output, got %q", m.content.String())
	}
}

func TestTUIModel_TranscriptThinkingBlock(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)
	updated, _ = m.Update(tuiTranscriptMsg{ev: agent.TranscriptEvent{
		Kind:           agent.TranscriptAssistantPreface,
		AssistantProse: "Short plan.",
		ThinkingFull:   "Short plan.",
	}})
	m = updated.(tuiModel)
	s := m.content.String()
	if !strings.Contains(s, "Short plan") || !strings.Contains(s, "●") {
		t.Fatalf("expected intent-style thinking line with bullet and prose, got %q", s)
	}
	if strings.Contains(s, "Thinking:") {
		t.Fatalf("should not use legacy Thinking: header, got %q", s)
	}
}

func TestTUIModel_TodoSidebarNarrowsViewport(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(tuiModel)
	updated, _ = m.Update(tuiTodosMsg{items: []tools.TodoItem{{Content: "A", Status: "pending"}}})
	m = updated.(tuiModel)
	if m.todoSidebarWidth() == 0 {
		t.Fatal("expected sidebar width with todos on wide terminal")
	}
	if m.viewport.Width >= 100 {
		t.Fatalf("viewport should be narrower than full width, got %d", m.viewport.Width)
	}
}

func TestTUIModel_ModeChange(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)

	updated, _ := m.Update(tuiChromeMsg{
		Mode: "build", Model: "m", BackendLabel: "local", ContextWindow: 128000, LastPromptTokens: 0,
	})
	m = updated.(tuiModel)

	if m.mode != "build" {
		t.Fatalf("mode should be build, got %q", m.mode)
	}
	if !strings.Contains(m.input.Prompt, "build") {
		t.Fatalf("prompt should contain build, got %q", m.input.Prompt)
	}
}

func TestTUIModel_ChromeFooterShowsModelAndContext(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", false)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(tuiModel)
	updated, _ = m.Update(tuiChromeMsg{
		Mode: "build", Model: "test-model-x", BackendLabel: "local",
		ContextWindow: 100000, LastPromptTokens: 169800,
	})
	m = updated.(tuiModel)
	view := m.View()
	if !strings.Contains(view, "build") {
		t.Fatalf("view should include mode before prompt, got:\n%s", view)
	}
	if !strings.Contains(view, "test-model-x") {
		t.Fatalf("view should list model, got:\n%s", view)
	}
	if !strings.Contains(view, "169") {
		t.Fatalf("view should list prompt token hint, got:\n%s", view)
	}

	updated, _ = m.Update(tuiChromeMsg{
		Mode: "build", Model: "m", BackendLabel: "local",
		ContextWindow: 100000, LastPromptTokens: 5000, ContextEstimated: true,
	})
	m = updated.(tuiModel)
	view = m.View()
	if !strings.Contains(view, "~") {
		t.Fatalf("view should mark estimated context with ~, got:\n%s", view)
	}
	if !strings.Contains(view, "type / for commands") {
		t.Fatalf("view should list slash hint, got:\n%s", view)
	}
}

func TestTUIModel_WorkingStatus(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)

	// Make model ready so the spinner renders in the viewport.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)

	updated, cmd := m.Update(tuiWorkingMsg(true))
	m = updated.(tuiModel)
	if !m.working {
		t.Fatal("working should be true")
	}
	if cmd == nil {
		t.Fatal("should return a tick command when working starts")
	}
	if !strings.Contains(m.viewportContent(), "Agent is working") {
		t.Fatal("viewport content should contain spinner text while working")
	}
	if m.input.Focused() {
		t.Fatal("input should be blurred while agent is working")
	}

	updated, _ = m.Update(tuiWorkingMsg(false))
	m = updated.(tuiModel)
	if m.working {
		t.Fatal("working should be false")
	}
	if strings.Contains(m.viewportContent(), "Agent is working") {
		t.Fatal("viewport content should not contain spinner text when idle")
	}
	if !m.input.Focused() {
		t.Fatal("input should be focused when agent is idle")
	}
}

func TestTUIModel_EnterSendsInput(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)

	// Type some text then press Enter.
	m.input.SetValue("test input")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)

	select {
	case got := <-ic.ch:
		if got != "test input" {
			t.Fatalf("got %q, want %q", got, "test input")
		}
	default:
		t.Fatal("expected input on channel")
	}

	if m.input.Value() != "" {
		t.Fatal("input should be cleared after Enter")
	}
}

func TestTUIModel_QuitMessage(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)

	updated, cmd := m.Update(tuiQuitMsg{exitCode: 0})
	m = updated.(tuiModel)
	if !m.quitting {
		t.Fatal("should be quitting")
	}
	if cmd == nil {
		t.Fatal("should return tea.Quit cmd")
	}
}

func TestTUIWriter_SendsOutput(t *testing.T) {
	// tuiWriter.Write should not panic with a nil prog (graceful no-op).
	w := &tuiWriter{}
	n, err := w.Write([]byte("test"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("got %d, want 4", n)
	}
}

func TestTUIModel_SpinnerTickAdvancesFrame(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)

	updated, _ = m.Update(tuiWorkingMsg(true))
	m = updated.(tuiModel)

	content0 := m.viewportContent()

	// Simulate a spinner tick.
	updated, cmd := m.Update(tuiSpinnerTickMsg{})
	m = updated.(tuiModel)

	content1 := m.viewportContent()
	if content0 == content1 {
		t.Fatal("spinner tick should change the viewport content")
	}
	if cmd == nil {
		t.Fatal("tick should schedule another tick while working")
	}

	// Tick while not working should be a no-op.
	updated, _ = m.Update(tuiWorkingMsg(false))
	m = updated.(tuiModel)
	_, cmd = m.Update(tuiSpinnerTickMsg{})
	if cmd != nil {
		t.Fatal("tick while not working should not schedule another tick")
	}
}

func TestSanitizePipeOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain text unchanged",
			in:   "hello world\n",
			want: "hello world\n",
		},
		{
			name: "strips ESC[K",
			in:   "before\x1b[Kafter\n",
			want: "beforeafter\n",
		},
		{
			name: "strips ESC[0K",
			in:   "text\x1b[0Kmore\n",
			want: "textmore\n",
		},
		{
			name: "carriage return keeps last segment",
			in:   "old text\rnew text\n",
			want: "new text\n",
		},
		{
			name: "combined CR and ESC[K",
			in:   "\r\x1b[K⠋ spinner\r\x1b[K⠙ spinner\r\x1b[K\n",
			want: "\n",
		},
		{
			name: "CR before LF treated as overwrite",
			in:   "old\rnew\n",
			want: "new\n",
		},
		{
			name: "multiple lines with mixed sequences",
			in:   "clean\n\r\x1b[K⠋ working\nend\n",
			want: "clean\n⠋ working\nend\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizePipeOutput(tt.in)
			if got != tt.want {
				t.Errorf("sanitizePipeOutput(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTUIModel_ViewContainsInput(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", true)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)

	view := m.View()
	if !strings.Contains(view, "[build] > ") {
		t.Fatalf("view should contain prompt, got:\n%s", view)
	}
}

// TestTUIModel_InputGrowsWithMultilineText verifies the input box expands
// vertically as the user adds text that wraps past the inner width, and
// shrinks back to a single row after the message is submitted.
func TestTUIModel_InputGrowsWithMultilineText(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", false)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m = updated.(tuiModel)

	if got := m.input.Height(); got != tuiInputMinRows {
		t.Fatalf("initial input height = %d, want %d", got, tuiInputMinRows)
	}
	initialFooter := m.footerHeight()
	initialVPHeight := m.viewport.Height

	// Inject a value long enough to wrap to several visual rows, then send a
	// no-op key so the model's post-update layout pass picks up the change.
	long := strings.Repeat("abcdefghij ", 30)
	m.input.SetValue(long)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(tuiModel)

	if m.input.Height() <= tuiInputMinRows {
		t.Fatalf("input should have grown past %d rows for wrapped text, got %d",
			tuiInputMinRows, m.input.Height())
	}
	if m.input.Height() > tuiInputMaxRows {
		t.Fatalf("input height %d exceeds max %d", m.input.Height(), tuiInputMaxRows)
	}
	if m.footerHeight() <= initialFooter {
		t.Fatalf("footer height should grow with input, was %d now %d",
			initialFooter, m.footerHeight())
	}
	if m.viewport.Height >= initialVPHeight {
		t.Fatalf("viewport height should shrink as input grows, was %d now %d",
			initialVPHeight, m.viewport.Height)
	}

	// Submitting via Enter should reset the input back to a single row and
	// give the viewport its space back.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)
	<-ic.ch // drain submitted message

	if m.input.Height() != tuiInputMinRows {
		t.Fatalf("input height after submit = %d, want %d",
			m.input.Height(), tuiInputMinRows)
	}
	if m.viewport.Height != initialVPHeight {
		t.Fatalf("viewport height should restore after submit, was %d want %d",
			m.viewport.Height, initialVPHeight)
	}
}

// TestTUIModel_InsertNewlineWithCtrlJ verifies that ctrl+j inserts a newline
// inside the textarea and grows the input panel without submitting.
func TestTUIModel_InsertNewlineWithCtrlJ(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", false)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m = updated.(tuiModel)

	m.input.SetValue("first")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated.(tuiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second")})
	m = updated.(tuiModel)

	if !strings.Contains(m.input.Value(), "\n") {
		t.Fatalf("ctrl+j should insert a newline into the textarea, got %q", m.input.Value())
	}
	if m.input.Height() < 2 {
		t.Fatalf("input should be at least 2 rows after newline, got %d", m.input.Height())
	}

	select {
	case got := <-ic.ch:
		t.Fatalf("ctrl+j should not submit, but channel received %q", got)
	default:
	}
}

// TestTUIModel_InputBackgroundUniform verifies that the panel background
// SGR sequence is present in the row that contains the user's typed text,
// not just in the empty padding cells next to it. This guards against the
// regression where a styled prompt with an embedded reset code clobbered
// the panel background once the user began typing.
func TestTUIModel_InputBackgroundUniform(t *testing.T) {
	// Force colour rendering even in non-TTY environments (CI) so the test
	// can inspect the background SGR codes regardless of where it runs.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	ic := newInputCloser()
	m := newTUIModel(ic, "build", false)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m = updated.(tuiModel)

	emptyView := m.inputFooterView()
	emptyFirstLine := strings.SplitN(emptyView, "\n", 2)[0]
	if !strings.Contains(emptyFirstLine, "\x1b[48") {
		t.Fatalf("empty input row should already carry a panel-background SGR; got %q", emptyFirstLine)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello world")})
	m = updated.(tuiModel)

	typedView := m.inputFooterView()
	typedFirstLine := strings.SplitN(typedView, "\n", 2)[0]

	// After typing, the row that contains the user's text must still have
	// at least as many background-colour sequences as the empty view. If
	// the background were lost mid-row (the original bug) we'd expect
	// fewer or zero "\x1b[48" sequences in the affected segment.
	bgEscapeEmpty := strings.Count(emptyFirstLine, "\x1b[48")
	bgEscapeTyped := strings.Count(typedFirstLine, "\x1b[48")
	if bgEscapeTyped < bgEscapeEmpty {
		t.Fatalf("typed input should keep the panel background; bg-seqs went from %d → %d\nempty: %q\ntyped: %q",
			bgEscapeEmpty, bgEscapeTyped, emptyFirstLine, typedFirstLine)
	}
	if !strings.Contains(typedFirstLine, "hello world") {
		t.Fatalf("typed input view should contain the typed text, got %q", typedFirstLine)
	}
}

// TestWrappedRowCount exercises the helper used to size the input panel.
func TestWrappedRowCount(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  int
	}{
		{"empty", "", 10, 1},
		{"single line short", "hello", 10, 1},
		{"single line exact", "0123456789", 10, 1},
		{"wraps to two", "01234567890", 10, 2},
		{"wraps to four", strings.Repeat("a", 35), 10, 4},
		{"explicit newlines", "a\nb\nc", 10, 3},
		{"empty trailing line", "a\n", 10, 2},
		{"width <= 0 defaults to one", "anything", 0, 1},

		// Word-wrap cases: words that don't fit on the current line
		// push to the next, producing more rows than character-level
		// division would predict.
		{"word wrap three rows", "aaaa bbbbb cccc", 10, 3},
		{"word wrap repeated", "word word word", 8, 3},
		{"word wrap with newlines", "aaaa bbbbb cccc\ndd ee", 10, 4},
		{"words fit just under", "aaa bbb c", 10, 1},
		{"single space boundary", "abcde fghij", 10, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wrappedRowCount(tt.text, tt.width); got != tt.want {
				t.Fatalf("wrappedRowCount(%q, %d) = %d, want %d", tt.text, tt.width, got, tt.want)
			}
		})
	}
}

func TestTUIModel_ViewportContentWrapsLongLines(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m = updated.(tuiModel)

	longLine := strings.Repeat("x", 80)
	updated, _ = m.Update(tuiOutputMsg(longLine))
	m = updated.(tuiModel)

	content := m.viewportContent()
	for i, line := range strings.Split(content, "\n") {
		w := lipgloss.Width(line)
		if w > 40 {
			t.Fatalf("line %d exceeds viewport width 40: width=%d, content=%q", i, w, line)
		}
	}
}

func TestTUIModel_ViewportContentWordWrapsAtBoundaries(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 24})
	m = updated.(tuiModel)

	updated, _ = m.Update(tuiOutputMsg("hello world this is a long sentence for wrapping"))
	m = updated.(tuiModel)

	content := m.viewportContent()
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines after wrapping at width 20, got %d: %q", len(lines), content)
	}
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w > 20 {
			t.Fatalf("line %d exceeds viewport width 20: width=%d, content=%q", i, w, line)
		}
	}
}

func TestTUIModel_ScrollUpPreservesPosition(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 6})
	m = updated.(tuiModel)

	var buf strings.Builder
	for i := 0; i < 50; i++ {
		buf.WriteString("line\n")
	}
	updated, _ = m.Update(tuiOutputMsg(buf.String()))
	m = updated.(tuiModel)

	if !m.viewport.AtBottom() {
		t.Fatal("should auto-scroll to bottom on new content")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(tuiModel)
	if m.viewport.AtBottom() {
		t.Fatal("PgUp should scroll away from bottom")
	}

	// New content while scrolled up must not snap back to bottom.
	updated, _ = m.Update(tuiOutputMsg("new content\n"))
	m = updated.(tuiModel)
	if m.viewport.AtBottom() {
		t.Fatal("new content should not auto-scroll when user has scrolled up")
	}
}

func TestHangingIndentWidth(t *testing.T) {
	tests := []struct {
		name string
		line string
		want int
	}{
		{"empty", "", 0},
		{"no indent", "hello world", 0},
		{"spaces", "  hello", 2},
		{"bullet", "● hello", 2},
		{"indented bullet", "  ▸ hello", 4},
		{"pipe prefix", "│ hello", 2},
		{"spinner", "⠋ working", 2},
		{"ansi styled", "\x1b[38;2;107;114;128m  ▸ hello\x1b[0m", 4},
		{"ansi then text", "\x1b[31mhello\x1b[0m", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hangingIndentWidth(tt.line); got != tt.want {
				t.Fatalf("hangingIndentWidth(%q) = %d, want %d", tt.line, got, tt.want)
			}
		})
	}
}

func TestIndentAwareWrap_ContinuationIndent(t *testing.T) {
	s := "● " + strings.Repeat("word ", 20)
	wrapped := indentAwareWrap(s, 40)
	lines := strings.Split(wrapped, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines, got %d: %q", len(lines), wrapped)
	}
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w > 40 {
			t.Fatalf("line %d exceeds width 40: width=%d, content=%q", i, w, line)
		}
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("continuation should be indented by 2 (matching bullet prefix), got %q", line)
		}
	}
}

func TestIndentAwareWrap_NoIndent(t *testing.T) {
	s := strings.Repeat("x", 60)
	wrapped := indentAwareWrap(s, 40)
	for i, line := range strings.Split(wrapped, "\n") {
		w := lipgloss.Width(line)
		if w > 40 {
			t.Fatalf("line %d exceeds width 40: width=%d", i, w)
		}
	}
}

func TestIndentAwareWrap_ShortLineUnchanged(t *testing.T) {
	s := "  ▸ short"
	if got := indentAwareWrap(s, 80); got != s {
		t.Fatalf("short line should be unchanged, got %q", got)
	}
}

func TestSlashPicker_ShowAndSelect(t *testing.T) {
	cmds := &slashcmd.Registry{}
	cmds.Register(slashcmd.Command{Name: "build", Aliases: []string{"b"}, Description: "switch to build mode"})
	cmds.Register(slashcmd.Command{Name: "plan", Aliases: []string{"p"}, Description: "switch to plan mode"})
	cmds.Register(slashcmd.Command{Name: "ask", Aliases: []string{"a"}, Description: "switch to ask mode"})
	cmds.Register(slashcmd.Command{Name: "help", Aliases: []string{"h"}, Description: "show available commands"})
	cmds.Register(slashcmd.Command{Name: "exit", Aliases: []string{"q"}, Description: "quit the session"})
	cmds.Register(slashcmd.Command{Name: "branches", Aliases: []string{"cbranch"}, Description: "list conversation branches"})
	cmds.Register(slashcmd.Command{Name: "branch", Aliases: []string{}, Description: "show/switch branch"})
	cmds.Register(slashcmd.Command{Name: "checkpoint", Aliases: []string{"cp"}, Description: "save snapshot"})
	cmds.Register(slashcmd.Command{Name: "config", Description: "view/set configuration"})
	cmds.Register(slashcmd.Command{Name: "cost", Aliases: []string{"tokens"}, Description: "show token usage"})
	cmds.Register(slashcmd.Command{Name: "clear", Description: "reset conversation history"})
	cmds.Register(slashcmd.Command{Name: "compact", Description: "summarize conversation"})
	cmds.Register(slashcmd.Command{Name: "tools", Description: "list available tools"})
	cmds.Register(slashcmd.Command{Name: "mcp", Description: "list MCP servers"})
	cmds.Register(slashcmd.Command{Name: "model", Description: "switch model"})
	cmds.Register(slashcmd.Command{Name: "workspace", Description: "change workspace"})
	cmds.Register(slashcmd.Command{Name: "log", Description: "show/change log path"})
	cmds.Register(slashcmd.Command{Name: "undo", Description: "undo last build turn"})
	cmds.Register(slashcmd.Command{Name: "diff", Description: "show git diff"})
	cmds.Register(slashcmd.Command{Name: "pr", Description: "open GitHub PR"})
	cmds.Register(slashcmd.Command{Name: "checkpoints", Aliases: []string{"cps"}, Description: "list checkpoints"})
	cmds.Register(slashcmd.Command{Name: "rollback", Aliases: []string{"rb"}, Description: "restore to checkpoint"})
	cmds.Register(slashcmd.Command{Name: "fork", Description: "fork checkpoint + new branch"})
	cmds.Register(slashcmd.Command{Name: "hooks", Description: "list lifecycle hooks"})
	cmds.Register(slashcmd.Command{Name: "image", Description: "attach image"})
	cmds.Register(slashcmd.Command{Name: "setup", Description: "guided setup wizard"})
	cmds.Register(slashcmd.Command{Name: "skills", Description: "list discovered skills"})
	cmds.Register(slashcmd.Command{Name: "status", Description: "show session state"})
	cmds.Register(slashcmd.Command{Name: "plan-status", Aliases: []string{"ps"}, Description: "show plan phase"})
	cmds.Register(slashcmd.Command{Name: "memory", Aliases: []string{"mem"}, Description: "view/edit memory"})
	cmds.Register(slashcmd.Command{Name: "new", Aliases: []string{"n"}, Description: "start new session"})
	cmds.Register(slashcmd.Command{Name: "create-skill", Description: "create agent skill"})
	cmds.Register(slashcmd.Command{Name: "create-rule", Description: "create Cursor-style rule"})

	p := newPicker()

	// Initially invisible
	if p.visible {
		t.Fatal("picker should be invisible initially")
	}

	// Show with empty prefix
	p.show(cmds, "", 80)
	if !p.visible {
		t.Fatal("picker should be visible after show")
	}
	if len(p.commands) == 0 {
		t.Fatal("should have commands")
	}
	if p.selected != 0 {
		t.Fatal("selected should be 0")
	}

	// Show with prefix
	p.show(cmds, "b", 80)
	if len(p.commands) == 0 {
		t.Fatal("should have matches for 'b'")
	}

	// Navigate
	p.selectDown()
	if p.selected != 1 {
		t.Fatalf("selected should be 1, got %d", p.selected)
	}

	p.selectUp()
	if p.selected != 0 {
		t.Fatalf("selected should be 0, got %d", p.selected)
	}

	// Select name
	name := p.SelectedName()
	if name == "" {
		t.Fatal("selected name should not be empty")
	}

	// Hide
	p.hide()
	if p.visible {
		t.Fatal("picker should be hidden")
	}
}

func TestSlashPicker_View(t *testing.T) {
	cmds := &slashcmd.Registry{}
	cmds.Register(slashcmd.Command{Name: "build", Description: "switch to build mode"})
	cmds.Register(slashcmd.Command{Name: "plan", Description: "switch to plan mode"})

	p := newPicker()
	p.show(cmds, "b", 80)

	view := p.View()
	if view == "" {
		t.Fatal("view should not be empty")
	}
	if !strings.Contains(view, "build") {
		t.Fatalf("view should contain 'build', got:\n%s", view)
	}
}

func TestSlashPicker_OffsetUnchangedOnRepeatShow(t *testing.T) {
	cmds := &slashcmd.Registry{}
	for _, name := range []string{"a0", "a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9"} {
		cmds.Register(slashcmd.Command{Name: name, Description: "x"})
	}
	p := newPicker()
	p.show(cmds, "", 80)
	for i := 0; i < 8; i++ {
		p.selectDown()
	}
	wantOff, wantSel := p.offset, p.selected
	if wantSel < 5 {
		t.Fatalf("setup: want selected>=5 after scrolling, got %d", wantSel)
	}
	if wantOff == 0 {
		t.Fatalf("setup: want non-zero scroll offset, got 0 (selected=%d)", wantSel)
	}
	// Same as a cursor-blink tick: show() again with an unchanged filter prefix.
	p.show(cmds, "", 80)
	if p.offset != wantOff {
		t.Fatalf("repeat show reset offset: was %d, now %d", wantOff, p.offset)
	}
	if p.selected != wantSel {
		t.Fatalf("repeat show changed selection: was %d, now %d", wantSel, p.selected)
	}
}

func TestTUIModel_SlashPickerEnterSelects(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)

	// Set slash commands
	cmds := &slashcmd.Registry{}
	cmds.Register(slashcmd.Command{Name: "build", Description: "switch to build mode"})
	cmds.Register(slashcmd.Command{Name: "plan", Description: "switch to plan mode"})
	m.slashCmds = cmds

	// Type /b to trigger picker
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(tuiModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = updated.(tuiModel)

	// Picker should be visible after typing /b
	if !m.picker.visible {
		t.Fatal("picker should be visible after typing /b")
	}

	// Press Enter to complete and submit
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)

	// The command should have been submitted to the channel.
	select {
	case got := <-ic.ch:
		if !strings.HasPrefix(got, "/build ") {
			t.Fatalf("channel should have '/build ...', got %q", got)
		}
	default:
		t.Fatal("expected input on channel")
	}
}

func TestTUIModel_SlashPickerEscapeDismisses(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)

	// Set slash commands
	cmds := &slashcmd.Registry{}
	cmds.Register(slashcmd.Command{Name: "build", Description: "switch to build mode"})
	m.slashCmds = cmds

	// Type / to show picker (via textinput update)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(tuiModel)

	if !m.picker.visible {
		t.Fatal("picker should be visible")
	}

	// Escape to dismiss
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(tuiModel)

	if m.picker.visible {
		t.Fatal("picker should be hidden after escape")
	}
}

func TestTUIModel_CtrlC_InterruptsWhileWorking(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", true)

	interrupted := false
	m.onInterrupt = func() bool {
		interrupted = true
		return true
	}

	// Put the model into working state.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)
	updated, _ = m.Update(tuiWorkingMsg(true))
	m = updated.(tuiModel)

	// Ctrl+C while working should call onInterrupt and not quit.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(tuiModel)

	if !interrupted {
		t.Fatal("onInterrupt should have been called")
	}
	if m.quitting {
		t.Fatal("should not be quitting when interrupt succeeds")
	}
	if cmd != nil {
		t.Fatal("should not return tea.Quit cmd when interrupt succeeds")
	}
}

func TestTUIModel_CtrlC_QuitsWhenIdle(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", true)

	interrupted := false
	m.onInterrupt = func() bool {
		interrupted = true
		return true
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)

	// Not working — Ctrl+C should quit normally.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(tuiModel)

	if interrupted {
		t.Fatal("onInterrupt should not be called when not working")
	}
	if !m.quitting {
		t.Fatal("should be quitting when not working")
	}
	if cmd == nil {
		t.Fatal("should return tea.Quit cmd when quitting")
	}
}

func TestTUIModel_Escape_InterruptsWhileWorking(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", true)

	interrupted := false
	m.onInterrupt = func() bool {
		interrupted = true
		return true
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)
	updated, _ = m.Update(tuiWorkingMsg(true))
	m = updated.(tuiModel)

	// Escape while working should call onInterrupt.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(tuiModel)

	if !interrupted {
		t.Fatal("onInterrupt should have been called on Escape while working")
	}
	if m.quitting {
		t.Fatal("should not be quitting on Escape")
	}
}

func TestTUIModel_Escape_NoopWhenIdle(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "build", true)

	interrupted := false
	m.onInterrupt = func() bool {
		interrupted = true
		return true
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(tuiModel)

	// Escape while idle and no picker — should be a no-op.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(tuiModel)

	if interrupted {
		t.Fatal("onInterrupt should not be called when not working")
	}
	if m.quitting {
		t.Fatal("should not be quitting on idle Escape")
	}
}
