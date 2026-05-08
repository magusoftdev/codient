package codientcli

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"codient/internal/agent"
	"codient/internal/assistout"
	"codient/internal/slashcmd"
	"codient/internal/tokentracker"
	"codient/internal/tools"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// inputCloser wraps a channel with sync.Once so it can be safely closed from
// multiple goroutines (Ctrl+C in the TUI model and the error/shutdown path in run.go).
type inputCloser struct {
	ch   chan string
	once sync.Once
}

func newInputCloser() *inputCloser {
	return &inputCloser{ch: make(chan string, 1)}
}

func (ic *inputCloser) Close() {
	ic.once.Do(func() { close(ic.ch) })
}

// TUI message types.
type (
	tuiOutputMsg string // new text for the viewport
	tuiQuitMsg   struct{ exitCode int }
	tuiChromeMsg struct {
		Mode             string
		Model            string
		BackendLabel     string
		ContextWindow    int
		LastPromptTokens int64
		ContextEstimated bool // true when LastPromptTokens is heuristic (API omitted usage)
	}
	tuiWorkingMsg     bool // true = agent working, false = idle
	tuiSpinnerTickMsg time.Time
	slashCmdsMsg      *slashcmd.Registry
	tuiTranscriptMsg  struct {
		ev       agent.TranscriptEvent
		delegate bool
	}
	tuiTodosMsg struct {
		items []tools.TodoItem
	}
	// tuiUserPromptMsg appends a styled transcript block for the user's submitted prompt (TUI only).
	tuiUserPromptMsg string
)

var tuiSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// tuiModel is the Bubble Tea model for the interactive REPL.
// content is a pointer because strings.Builder must not be copied after first write,
// and Bubble Tea passes models by value through Update.
type tuiModel struct {
	viewport     viewport.Model
	input        textarea.Model
	inputCloser  *inputCloser
	content      *strings.Builder
	ready        bool
	quitting     bool
	mode         string
	plain        bool
	working      bool
	spinnerFrame int
	width        int
	height       int
	picker       slashPicker
	slashCmds    *slashcmd.Registry

	// inputHeight caches the textarea's current visual row count so the
	// model can detect height changes and re-flow the viewport when the
	// input box grows or shrinks.
	inputHeight int

	todos             []tools.TodoItem
	inReasoningStream bool
	thinkingCompact   bool // toggled with ctrl+t: shorter Thinking blocks
	onInterrupt       func() bool // returns true if a turn was cancelled

	chromeModel            string
	chromeBackendLabel     string
	chromeContextWindow    int
	chromeLastPromptTok    int64
	chromeContextEstimated bool
}

// Footer below the viewport: separator, input panel (when not plain), context hint, safety line.
// The input panel can grow vertically as the user types longer messages, so the
// total footer height is computed dynamically from the textarea's current row
// count rather than being a fixed constant.
const (
	tuiAccentColWidth     = 1
	tuiInputInnerPadH     = 1
	tuiFooterSepLines     = 1
	tuiFooterSpacerLines  = 1 // blank row between input and meta line
	tuiFooterMetaLines    = 1 // model · backend
	tuiFooterContextLines = 1 // context-hint / slash hint
	tuiFooterSafetyLines  = 1 // safety margin so the panel never collides with the viewport
	tuiInputMinRows       = 1
	tuiInputMaxRows       = 8
)

// tuiPanelBg is the background colour used for the input panel + meta row.
// Defining it once at the package level keeps the textarea Style and the
// surrounding lipgloss wrapper in sync.
var tuiPanelBg = lipgloss.AdaptiveColor{Light: "#EEEEEE", Dark: "#1A1A1A"}

func newTUIModel(ic *inputCloser, mode string, plain bool) tuiModel {
	ti := textarea.New()
	ti.CharLimit = 0
	ti.MaxHeight = tuiInputMaxRows
	ti.ShowLineNumbers = false
	ti.EndOfBufferCharacter = ' '
	// Plain Enter submits the message; multi-line input is inserted with
	// ctrl+j (universal "line feed") or alt+enter / shift+enter on terminals
	// that distinguish them. The outer model intercepts plain Enter before
	// the textarea sees it, so InsertNewline must not re-bind "enter" here.
	ti.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("ctrl+j", "alt+enter", "shift+enter"),
		key.WithHelp("ctrl+j", "insert newline"),
	)
	ti.SetWidth(40)
	ti.SetHeight(tuiInputMinRows)
	ti.Focus()

	m := tuiModel{
		input:       ti,
		inputCloser: ic,
		content:     &strings.Builder{},
		mode:        mode,
		plain:       plain,
		inputHeight: tuiInputMinRows,
	}
	m.applyInputPrompt()
	m.applyInputStyle()
	return m
}

func (m *tuiModel) applyInputPrompt() {
	var rendered string
	if m.plain {
		rendered = assistout.SessionPrompt(true, m.mode)
	} else {
		mode := strings.ToLower(strings.TrimSpace(m.mode))
		if mode == "" {
			mode = "build"
		}
		accent := assistout.ModeAccentColor(m.mode)
		dim := lipgloss.AdaptiveColor{Light: "#525252", Dark: "#A3A3A3"}
		// Bake the panel background into each segment so the rendered prompt
		// does not contain a mid-string SGR reset that would clear the panel
		// colour. Without this, the area "behind" the prompt characters
		// renders with the terminal's default background while the empty
		// padding cells render with panelBg.
		rendered = lipgloss.NewStyle().
			Foreground(accent).
			Background(tuiPanelBg).
			Bold(true).
			Render(mode) +
			lipgloss.NewStyle().
				Foreground(dim).
				Background(tuiPanelBg).
				Render(" > ")
	}
	m.input.Prompt = rendered
	promptW := lipgloss.Width(rendered)
	if promptW < 1 {
		promptW = 1
	}
	pad := strings.Repeat(" ", promptW)
	if !m.plain {
		// Continuation rows in the panel still need panelBg under the leading
		// space gutter so the wrapped text appears flush against a uniform
		// box.
		pad = lipgloss.NewStyle().Background(tuiPanelBg).Render(pad)
	}
	m.input.SetPromptFunc(promptW, func(line int) string {
		if line == 0 {
			return rendered
		}
		return pad
	})
}

// applyInputStyle configures the textarea's Style so the entire content area
// (cursor row, non-cursor rows, value text, end-of-buffer fill) shares the
// panel background. Setting Base.Background and letting the other style slots
// inherit it is enough because textarea always renders via Inherit(Base).
func (m *tuiModel) applyInputStyle() {
	if m.plain {
		focused, blurred := textarea.DefaultStyles()
		m.input.FocusedStyle = focused
		m.input.BlurredStyle = blurred
		m.input.Cursor.TextStyle = lipgloss.NewStyle()
		return
	}
	base := lipgloss.NewStyle().Background(tuiPanelBg)
	styled := textarea.Style{
		Base:             base,
		CursorLine:       base,
		CursorLineNumber: base,
		// EndOfBuffer needs Foreground == Background so the gutter character
		// is invisible on the panel background.
		EndOfBuffer: base.Foreground(tuiPanelBg),
		LineNumber:  base,
		Placeholder: base,
		Prompt:      base,
		Text:        base,
	}
	m.input.FocusedStyle = styled
	m.input.BlurredStyle = styled
	// The hidden (blink-off) cursor cell uses TextStyle; the visible cursor
	// uses Style.Reverse(true), which inverts foreground/background and
	// looks correct against panelBg without further configuration.
	m.input.Cursor.TextStyle = base
}

// applyInputChromeLayout sizes the textarea to match the current terminal
// width and computes its desired visual row count from the user's text. The
// computed height is clamped so the input never collapses below one row or
// grows past tuiInputMaxRows; longer text scrolls inside the textarea's own
// viewport.
func (m *tuiModel) applyInputChromeLayout() {
	if !m.ready {
		return
	}
	totalW := m.inputTotalWidth()
	if totalW < 4 {
		totalW = 4
	}
	m.input.SetWidth(totalW)

	rows := wrappedRowCount(m.input.Value(), m.input.Width())
	if rows < tuiInputMinRows {
		rows = tuiInputMinRows
	}
	if rows > tuiInputMaxRows {
		rows = tuiInputMaxRows
	}
	m.input.SetHeight(rows)
	m.inputHeight = rows
}

// inputTotalWidth returns the outer width the textarea is allowed to occupy,
// which is the inner panel width in non-plain mode (terminal width minus the
// accent column and horizontal padding) and the full terminal width in plain
// mode. The result is clamped to the terminal width so degenerate sizes never
// produce a textarea wider than the screen.
func (m *tuiModel) inputTotalWidth() int {
	if m.plain {
		return m.width
	}
	contentW := m.width - tuiAccentColWidth
	if contentW < 8 {
		contentW = m.width
	}
	inner := contentW - 2*tuiInputInnerPadH
	if inner < 10 {
		inner = max(10, contentW-1)
	}
	if m.width > 0 && inner > m.width {
		inner = m.width
	}
	return inner
}

// footerHeight returns the number of rows reserved below the viewport,
// including the dynamic input box.
func (m *tuiModel) footerHeight() int {
	rows := tuiInputMinRows
	if m.ready {
		rows = m.input.Height()
	}
	if rows < tuiInputMinRows {
		rows = tuiInputMinRows
	}
	if m.plain {
		return tuiFooterSepLines + rows + tuiFooterSafetyLines
	}
	return tuiFooterSepLines + rows + tuiFooterSpacerLines + tuiFooterMetaLines + tuiFooterContextLines + tuiFooterSafetyLines
}

// resetInputAfterSubmit clears the textarea and snaps the input box back to
// a single row, re-flowing the viewport if the panel shrank.
func (m *tuiModel) resetInputAfterSubmit() {
	m.input.Reset()
	if !m.ready {
		return
	}
	prev := m.input.Height()
	m.input.SetHeight(tuiInputMinRows)
	m.inputHeight = tuiInputMinRows
	if prev != m.input.Height() {
		m.applyViewportLayout()
	}
}

// wrappedRowCount computes how many visual rows a string occupies when
// word-wrapped at `width`. The algorithm mirrors charmbracelet/bubbles
// textarea's internal wrap() so the row count stays in sync with what the
// textarea actually renders. Each "\n" begins a new logical line; empty
// logical lines still count as one visible row.
func wrappedRowCount(s string, width int) int {
	if width <= 0 {
		return 1
	}
	total := 0
	for _, line := range strings.Split(s, "\n") {
		total += wordWrapLineCount(line, width)
	}
	if total == 0 {
		return 1
	}
	return total
}

// wordWrapLineCount counts the visual rows a single logical line occupies
// when word-wrapped at width, matching the textarea's wrap() behaviour:
// break at space boundaries; force-break a word that would exceed width.
func wordWrapLineCount(s string, width int) int {
	runes := []rune(s)
	if len(runes) == 0 {
		return 1
	}
	var (
		rows   = 1
		lineW  int
		word   []rune
		spaces int
	)
	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}
		if spaces > 0 {
			ww := lipgloss.Width(string(word))
			if lineW+ww+spaces > width {
				rows++
				lineW = ww + spaces
			} else {
				lineW += ww + spaces
			}
			spaces = 0
			word = nil
		} else if len(word) > 0 {
			ww := lipgloss.Width(string(word))
			lastW := lipgloss.Width(string(word[len(word)-1:]))
			if ww+lastW > width {
				if lineW > 0 {
					rows++
				}
				lineW = ww
				word = nil
			}
		}
	}
	if len(word) > 0 || spaces > 0 {
		ww := lipgloss.Width(string(word))
		if lineW+ww+spaces >= width {
			rows++
		}
	}
	return rows
}

func formatTUIBackendLabel(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "—"
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return truncateRunes(baseURL, 28)
	}
	host := u.Hostname()
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		return "local"
	}
	return truncateRunes(host, 28)
}

func truncateRunes(s string, max int) string {
	if max < 1 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

func (m *tuiModel) metaLineStyled(innerW int) string {
	dim := lipgloss.AdaptiveColor{Light: "#737373", Dark: "#A3A3A3"}
	modelFg := lipgloss.AdaptiveColor{Light: "#262626", Dark: "#FAFAFA"}

	model := strings.TrimSpace(m.chromeModel)
	if model == "" {
		model = "—"
	}
	backend := strings.TrimSpace(m.chromeBackendLabel)
	if backend == "" {
		backend = "—"
	}

	// Embed the panel background in every segment so the meta row's text
	// cells share the same background as the surrounding padding.
	sep := lipgloss.NewStyle().Foreground(dim).Background(tuiPanelBg).Render(" · ")
	modelSt := lipgloss.NewStyle().Foreground(modelFg).Background(tuiPanelBg).Render(truncateRunes(model, 56))
	backSt := lipgloss.NewStyle().Foreground(dim).Background(tuiPanelBg).Render(truncateRunes(backend, 28))

	line := lipgloss.JoinHorizontal(lipgloss.Top, modelSt, sep, backSt)
	return lipgloss.NewStyle().MaxWidth(innerW).Render(line)
}

func (m *tuiModel) inputFooterView() string {
	if m.plain {
		return m.input.View()
	}

	contentW := m.width - tuiAccentColWidth
	if contentW < 8 {
		contentW = m.width
	}
	innerW := contentW - 2*tuiInputInnerPadH
	if innerW < 4 {
		innerW = max(4, contentW-1)
	}

	inputLine := lipgloss.NewStyle().
		Width(contentW).
		Padding(0, tuiInputInnerPadH).
		Background(tuiPanelBg).
		Render(m.input.View())

	spacer := lipgloss.NewStyle().
		Width(contentW).
		Height(1).
		Background(tuiPanelBg).
		Render(" ")

	metaLine := lipgloss.NewStyle().
		Width(contentW).
		Padding(0, tuiInputInnerPadH).
		Background(tuiPanelBg).
		Render(m.metaLineStyled(innerW))

	innerCol := lipgloss.JoinVertical(lipgloss.Left, inputLine, spacer, metaLine)
	h := max(1, lipgloss.Height(innerCol))
	accentColor := assistout.ModeAccentColor(m.mode)
	accentCell := lipgloss.NewStyle().
		Background(accentColor).
		Foreground(accentColor).
		Width(tuiAccentColWidth).
		Render(" ")
	var accentCol string
	for i := 0; i < h; i++ {
		if i > 0 {
			accentCol += "\n"
		}
		accentCol += accentCell
	}

	panel := lipgloss.JoinHorizontal(lipgloss.Top, accentCol, innerCol)

	ctxHint := tokentracker.FormatPromptContextHint(m.chromeLastPromptTok, m.chromeContextWindow)
	if m.chromeContextEstimated && ctxHint != "—" {
		ctxHint = "~" + ctxHint
	}
	ctxLine := lipgloss.NewStyle().
		Width(m.width).
		Align(lipgloss.Right).
		Foreground(lipgloss.AdaptiveColor{Light: "#737373", Dark: "#888888"}).
		Render(ctxHint + "  ·  type / for commands  ·  ctrl+j newline")

	return panel + "\n" + ctxLine
}

func (m tuiModel) Init() tea.Cmd {
	return textarea.Blink
}

// tuiRecover logs a panic with its stack trace to a temp file and re-panics.
// This lets us capture the real cause since Bubble Tea's built-in recovery
// discards the stack.
func tuiRecover() {
	if r := recover(); r != nil {
		f, err := os.CreateTemp("", "codient-panic-*.txt")
		if err == nil {
			fmt.Fprintf(f, "panic: %v\n\n", r)
			// Capture stack by re-panicking inside a nested recover.
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, false)
			f.Write(buf[:n])
			f.Close()
		}
		panic(r) // re-panic so Bubble Tea sees it
	}
}

func (m tuiModel) Update(msg tea.Msg) (_ tea.Model, _ tea.Cmd) {
	defer tuiRecover()
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			// If picker is visible and has selection, check if the typed text
			// already exactly matches a command. If so, just append a space
			// and submit. Otherwise, use the picker to replace/complete.
			if m.picker.visible && m.picker.SelectedName() != "" {
				current := m.input.Value()
				slashIdx := strings.LastIndex(current, "/")
				if slashIdx >= 0 {
					typed := current[slashIdx+1:]
					selected := m.picker.SelectedName()
					// If typed exactly matches the selected command, submit directly.
					if typed == selected {
						newValue := current[:slashIdx+1] + selected + " "
						m.input.SetValue(newValue)
						m.resetInputAfterSubmit()
						if m.inputCloser != nil {
							m.inputCloser.ch <- newValue
						}
						return m, nil
					}
					// Partial match: complete the command and submit it.
					if typed != "" {
						newValue := current[:slashIdx+1] + selected + " "
						m.input.SetValue(newValue)
						text := m.input.Value()
						m.resetInputAfterSubmit()
						if m.inputCloser != nil {
							m.inputCloser.ch <- text
						}
						return m, nil
					}
				}
				m.picker.hide()
				return m, nil
			}
			text := m.input.Value()
			m.resetInputAfterSubmit()
			if m.inputCloser != nil {
				m.inputCloser.ch <- text
			}
			return m, nil
		case tea.KeyCtrlT:
			m.thinkingCompact = !m.thinkingCompact
			return m, nil
		case tea.KeyCtrlC:
			if m.working && m.onInterrupt != nil {
				if m.onInterrupt() {
					return m, nil
				}
			}
			if m.inputCloser != nil {
				m.inputCloser.Close()
				m.inputCloser = nil
			}
			m.quitting = true
			return m, tea.Quit
		case tea.KeyPgUp:
			if m.ready {
				m.viewport.HalfPageUp()
			}
			return m, nil
		case tea.KeyPgDown:
			if m.ready {
				m.viewport.HalfPageDown()
			}
			return m, nil
		case tea.KeyUp:
			if m.picker.visible {
				m.picker.selectUp()
				return m, nil
			}
			// Hold Alt+Up as a viewport scroll shortcut so users can still
			// scroll history without leaving the input. Plain Up navigates
			// inside the textarea (line previous) when content spans
			// multiple visual rows.
			if msg.Alt && m.ready {
				m.viewport.ScrollUp(3)
				return m, nil
			}
		case tea.KeyDown:
			if m.picker.visible {
				m.picker.selectDown()
				return m, nil
			}
			if msg.Alt && m.ready {
				m.viewport.ScrollDown(3)
				return m, nil
			}
		case tea.KeyEscape:
			if m.picker.visible {
				m.picker.hide()
				return m, nil
			}
			if m.working && m.onInterrupt != nil {
				m.onInterrupt()
				return m, nil
			}

		}

	case tea.WindowSizeMsg:
		firstResize := !m.ready
		m.width = msg.Width
		m.height = msg.Height
		// Mark the model ready before computing the input chrome layout so
		// applyInputChromeLayout can call SetWidth/SetHeight on the textarea.
		// The footer height depends on the textarea's row count, so the
		// chrome must be applied before the viewport is sized.
		m.ready = true
		m.applyInputChromeLayout()
		vpHeight := max(1, msg.Height-m.footerHeight())
		mainW := m.mainViewportWidth()
		if firstResize {
			m.viewport = viewport.New(mainW, vpHeight)
			m.viewport.KeyMap = disabledViewportKeyMap()
			m.viewport.SetContent(m.content.String())
		} else {
			m.viewport.Width = mainW
			m.viewport.Height = vpHeight
			m.syncViewport()
		}

	case tuiOutputMsg:
		m.content.WriteString(string(msg))
		if m.ready {
			m.syncViewport()
		}

	case tuiUserPromptMsg:
		m.appendUserPromptBlock(string(msg))
		if m.ready {
			m.syncViewport()
		}

	case tuiChromeMsg:
		m.mode = msg.Mode
		m.chromeModel = msg.Model
		m.chromeBackendLabel = msg.BackendLabel
		m.chromeContextWindow = msg.ContextWindow
		m.chromeLastPromptTok = msg.LastPromptTokens
		m.chromeContextEstimated = msg.ContextEstimated
		m.applyInputPrompt()
		// Restyle the textarea too so the panel background reflects the new
		// mode / chrome state.
		m.applyInputStyle()
		m.applyInputChromeLayout()
		// applyInputChromeLayout may have grown or shrunk the input panel
		// because the prompt width can change with the mode label; re-flow
		// the viewport so it doesn't overlap or leave a gap.
		m.applyViewportLayout()

	case tuiWorkingMsg:
		m.working = bool(msg)
		if m.working {
			m.spinnerFrame = 0
			if m.ready {
				m.syncViewport()
			}
			cmds = append(cmds, m.spinnerTick())
		} else if m.ready {
			m.syncViewport()
		}

	case tuiSpinnerTickMsg:
		if m.working {
			m.spinnerFrame++
			if m.ready {
				m.syncViewport()
			}
			cmds = append(cmds, m.spinnerTick())
		}

	case slashCmdsMsg:
		m.slashCmds = msg

	case tuiTranscriptMsg:
		m.appendTranscriptEvent(msg.ev, msg.delegate)
		if m.ready {
			m.syncViewport()
		}

	case tuiTodosMsg:
		m.todos = append([]tools.TodoItem(nil), msg.items...)
		m.applyViewportLayout()

	case tuiQuitMsg:
		m.quitting = true
		return m, tea.Quit
	}

	var cmd tea.Cmd
	prevInputRows := m.input.Height()
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	// Re-flow the viewport when the textarea's visual row count changes so
	// the input panel can grow or shrink with the user's content. Skip the
	// recomputation until the model has been ready'd by an initial
	// WindowSizeMsg, otherwise SetHeight/SetWidth would clobber tests that
	// drive the model without sending a resize.
	if m.ready {
		desired := wrappedRowCount(m.input.Value(), max(1, m.input.Width()))
		if desired < tuiInputMinRows {
			desired = tuiInputMinRows
		}
		if desired > tuiInputMaxRows {
			desired = tuiInputMaxRows
		}
		if desired != m.input.Height() {
			growing := desired > m.input.Height()
			m.input.SetHeight(desired)
			if growing {
				// The textarea's repositionView (called inside its
				// Update) only guarantees the cursor row is visible —
				// it does not scroll back to show line 0 (the prompt).
				// When the input grows, re-set the same value: SetValue
				// calls Reset → viewport.GotoTop, which zeroes YOffset
				// so the prompt stays visible. The cursor ends up at
				// the end of the text, which is where it already is
				// since growth is caused by typing.
				m.input.SetValue(m.input.Value())
			}
		}
		if m.input.Height() != prevInputRows {
			m.inputHeight = m.input.Height()
			m.applyViewportLayout()
		}
	}

	// Check for slash command prefix and show picker.
	if m.slashCmds != nil {
		val := m.input.Value()
		prefix := ""
		// Only trigger on / at the start of the line or after whitespace.
		if strings.HasPrefix(val, "/") {
			prefix = val[1:]
		} else {
			lastSlash := strings.LastIndex(val, "/")
			if lastSlash >= 0 {
				// Check if preceded by whitespace or newline.
				if lastSlash == 0 || val[lastSlash-1] == ' ' || val[lastSlash-1] == '\t' || val[lastSlash-1] == '\n' {
					prefix = val[lastSlash+1:]
				}
			}
		}
		if prefix != "" {
			m.picker.show(m.slashCmds, prefix, m.width)
		} else if val == "/" {
			// Bare / triggers picker with empty prefix.
			m.picker.show(m.slashCmds, "", m.width)
		} else if m.picker.visible {
			m.picker.hide()
		}
	} else if m.picker.visible {
		m.picker.hide()
	}

	// Only forward non-key messages to the viewport (window size, mouse scroll).
	// Key events go exclusively to the textinput to avoid the viewport's
	// default bindings (b=PageUp, f=PageDown, etc.) stealing typed characters.
	if m.ready {
		if _, isKey := msg.(tea.KeyMsg); !isKey {
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

var statusBarStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"})

// syncViewport updates the viewport content from the builder plus any spinner
// suffix, and auto-scrolls if the viewport was already at the bottom.
func (m *tuiModel) syncViewport() {
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.viewportContent())
	if atBottom {
		m.viewport.GotoBottom()
	}
}

// viewportContent returns the accumulated output plus an animated spinner line
// when the agent is working. The result is word-wrapped (then hard-wrapped as
// a fallback for tokens wider than the viewport) so content never overflows
// the terminal width.
func (m *tuiModel) viewportContent() string {
	s := m.content.String()
	if m.working {
		frame := tuiSpinnerFrames[m.spinnerFrame%len(tuiSpinnerFrames)]
		line := frame + " Agent is working…"
		if strings.TrimSpace(s) != "" {
			s += "\n\n" + line
		} else {
			s += line
		}
	}
	if w := m.viewport.Width; w > 0 {
		s = indentAwareWrap(s, w)
	}
	return s
}

func (m tuiModel) spinnerTick() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(t time.Time) tea.Msg {
		return tuiSpinnerTickMsg(t)
	})
}

func (m tuiModel) View() (_ string) {
	defer tuiRecover()
	if !m.ready {
		return "Initializing..."
	}
	sep := statusBarStyle.Render(strings.Repeat("─", m.width))
	if m.picker.visible {
		// Render the picker as an overlay on the viewport, not below it.
		pickerView := m.picker.View()
		if pickerView != "" {
			overlay := m.viewportContentWithOverlay(m.viewport.View(), pickerView)
			return m.composeMainRow(overlay) + "\n" + sep + "\n" + m.inputFooterView()
		}
	}
	return m.composeMainRow(m.viewport.View()) + "\n" + sep + "\n" + m.inputFooterView()
}

// viewportContentWithOverlay renders the viewport content with the picker
// overlaid on the bottom, replacing the last N lines.
func (m tuiModel) viewportContentWithOverlay(vpContent, overlay string) string {
	vpLines := strings.Split(vpContent, "\n")
	pickerLines := strings.Split(overlay, "\n")
	if len(pickerLines) == 0 {
		return vpContent
	}
	// Replace the last len(pickerLines) lines of the viewport with the overlay.
	if len(vpLines) <= len(pickerLines) {
		return overlay
	}
	return strings.Join(append(vpLines[:len(vpLines)-len(pickerLines)], pickerLines...), "\n")
}

// tuiWriter is an io.Writer that sends each Write to the Bubble Tea program
// as a tuiOutputMsg. It is safe for concurrent use.
type tuiWriter struct {
	prog *tea.Program
	mu   sync.Mutex
}

func (w *tuiWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.prog != nil {
		w.prog.Send(tuiOutputMsg(string(p)))
	}
	return len(p), nil
}

// tuiSetup holds all state needed to run the Bubble Tea TUI session.
type tuiSetup struct {
	prog     *tea.Program
	input    *inputCloser
	origOut  *os.File
	origErr  *os.File
	stdoutR  *os.File
	stdoutW  *os.File
	stderrR  *os.File
	stderrW  *os.File
	exitCode int
	done     chan struct{} // closed when the session goroutine exits
}

// initTUI creates the Bubble Tea program and redirects stdout/stderr into it.
// The caller must run the returned setup's start method in a goroutine to pump
// pipe output into the TUI, then call prog.Run() on the main goroutine.
// onInterrupt is called when Ctrl+C is pressed while the agent is working;
// it should return true if a turn was successfully cancelled.
func initTUI(mode string, plain bool, onInterrupt func() bool) (*tuiSetup, error) {
	origOut := os.Stdout
	origErr := os.Stderr

	// Cache terminal state before redirecting file descriptors.
	stdoutTTY := isFileTTY(origOut)
	stderrTTY := isFileTTY(origErr)
	width := getTermWidth(origErr)
	darkBg := lipgloss.HasDarkBackground()

	// Create pipes to capture stdout/stderr.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	assistout.SetTUIOverride(assistout.NewTUIOverrideValues(stdoutTTY, stderrTTY, width, darkBg))
	tuiModeActive.Store(true)

	os.Stdout = stdoutW
	os.Stderr = stderrW

	ic := newInputCloser()
	model := newTUIModel(ic, mode, plain)
	model.onInterrupt = onInterrupt
	prog := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithOutput(origErr),
	)

	ts := &tuiSetup{
		prog:    prog,
		input:   ic,
		origOut: origOut,
		origErr: origErr,
		stdoutR: stdoutR,
		stdoutW: stdoutW,
		stderrR: stderrR,
		stderrW: stderrW,
		done:    make(chan struct{}),
	}
	return ts, nil
}

// reEraseLine matches ANSI "erase in line" sequences (ESC [ … K).
var reEraseLine = regexp.MustCompile(`\x1b\[\d*K`)

// sanitizePipeOutput strips terminal cursor-control sequences that would
// corrupt the viewport. Bare \r (carriage return) without a following \n
// means "overwrite current line"; we handle this by keeping only the text
// after the last \r on each visual line.
func sanitizePipeOutput(s string) string {
	s = reEraseLine.ReplaceAllString(s, "")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if idx := strings.LastIndex(line, "\r"); idx >= 0 {
			lines[i] = line[idx+1:]
		}
	}
	return strings.Join(lines, "\n")
}

// startPipeReaders launches goroutines that read from the captured stdout/stderr
// pipes and forward content to the TUI viewport. Call this before prog.Run().
func (ts *tuiSetup) startPipeReaders() {
	pump := func(r io.Reader) {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				clean := sanitizePipeOutput(string(buf[:n]))
				if clean != "" {
					ts.prog.Send(tuiOutputMsg(clean))
				}
			}
			if err != nil {
				return
			}
		}
	}
	go pump(ts.stdoutR)
	go pump(ts.stderrR)
}

// cleanup restores original stdout/stderr and closes pipes.
// Safe to call multiple times.
func (ts *tuiSetup) cleanup() {
	os.Stdout = ts.origOut
	os.Stderr = ts.origErr
	// Close all pipe ends (Close on an already-closed *os.File is harmless).
	ts.stdoutW.Close()
	ts.stderrW.Close()
	ts.stdoutR.Close()
	ts.stderrR.Close()
	assistout.SetTUIOverride(nil)
	tuiModeActive.Store(false)
}

// disabledViewportKeyMap returns a KeyMap with all bindings disabled so the
// viewport never intercepts keystrokes meant for the text input.
func disabledViewportKeyMap() viewport.KeyMap {
	disabled := func() key.Binding { return key.NewBinding(key.WithDisabled()) }
	return viewport.KeyMap{
		PageDown:     disabled(),
		PageUp:       disabled(),
		HalfPageUp:   disabled(),
		HalfPageDown: disabled(),
		Down:         disabled(),
		Up:           disabled(),
		Left:         disabled(),
		Right:        disabled(),
	}
}

func isFileTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func getTermWidth(f *os.File) int {
	fd := int(f.Fd())
	if !term.IsTerminal(fd) {
		return 80
	}
	w, _, err := term.GetSize(fd)
	if err != nil || w < 20 {
		return 80
	}
	return w
}
