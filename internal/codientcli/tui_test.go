package codientcli

import (
	"strings"
	"testing"

	"codient/internal/slashcmd"

	tea "github.com/charmbracelet/bubbletea"
)

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

func TestTUIModel_ModeChange(t *testing.T) {
	ic := newInputCloser()
	m := newTUIModel(ic, "ask", true)

	updated, _ := m.Update(tuiModeMsg("build"))
	m = updated.(tuiModel)

	if m.mode != "build" {
		t.Fatalf("mode should be build, got %q", m.mode)
	}
	if !strings.Contains(m.input.Prompt, "build") {
		t.Fatalf("prompt should contain build, got %q", m.input.Prompt)
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

	updated, _ = m.Update(tuiWorkingMsg(false))
	m = updated.(tuiModel)
	if m.working {
		t.Fatal("working should be false")
	}
	if strings.Contains(m.viewportContent(), "Agent is working") {
		t.Fatal("viewport content should not contain spinner text when idle")
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

	// Type / to trigger picker (via textinput update)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(tuiModel)

	// Picker should be visible after typing /
	if !m.picker.visible {
		t.Fatal("picker should be visible after typing /")
	}

	// Press Enter to select
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(tuiModel)

	// Input should have the selected command
	val := m.input.Value()
	if !strings.HasPrefix(val, "/build ") {
		t.Fatalf("input should be '/build ...', got %q", val)
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
