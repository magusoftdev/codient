package codientcli

import (
	"strings"

	"codient/internal/slashcmd"

	"github.com/charmbracelet/lipgloss"
)

// slashPicker displays a dropdown of slash command suggestions above the input.
type slashPicker struct {
	visible    bool
	commands   []slashcmd.CommandMatch
	selected   int
	inputValue string
	width      int
	offset     int // scroll offset into the command list
}

// pickerStyles holds the styling for the slash picker dropdown.
var pickerStyles = struct {
	box       lipgloss.Style
	header    lipgloss.Style
	selected  lipgloss.Style
	regular   lipgloss.Style
}{
	box: lipgloss.NewStyle().
		Background(lipgloss.AdaptiveColor{Light: "#F0F0F0", Dark: "#2A2A2A"}).
		Border(lipgloss.Border{
			Top:         "─",
			Bottom:      "─",
			Left:        "│",
			Right:       "│",
			TopLeft:     "┌",
			TopRight:    "┐",
			BottomLeft:  "└",
			BottomRight: "┘",
		}).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#CCCCCC", Dark: "#555555"}).
		PaddingTop(1).
		PaddingBottom(1).
		MarginTop(1),
	header: lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"}).
		Italic(true),
	selected: lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#000000", Dark: "#FFFFFF"}).
		Background(lipgloss.AdaptiveColor{Light: "#CCCCCC", Dark: "#444444"}).
		PaddingLeft(1).
		PaddingRight(1),
	regular: lipgloss.NewStyle().
		PaddingLeft(2),
}

// newPicker creates an invisible picker.
func newPicker() slashPicker {
	return slashPicker{}
}

// show populates and displays the picker for the given registry and input.
func (p *slashPicker) show(cmds *slashcmd.Registry, inputValue string, width int) {
	p.width = width
	// Extract the part after the last / for matching.
	prefix := inputValue
	if idx := strings.LastIndex(inputValue, "/"); idx >= 0 {
		prefix = inputValue[idx+1:]
	}
	p.inputValue = prefix
	p.commands = cmds.Lookup(prefix)
	if len(p.commands) == 0 {
		p.visible = false
		return
	}
	p.visible = true
	// Clamp selection to the new result set.
	if p.selected >= len(p.commands) {
		p.selected = len(p.commands) - 1
	}
	p.offset = 0
}

// hide hides the picker.
func (p *slashPicker) hide() {
	p.visible = false
	p.commands = nil
	p.selected = 0
	p.offset = 0
}

// selectUp moves selection up by one.
func (p *slashPicker) selectUp() {
	if !p.visible || len(p.commands) == 0 {
		return
	}
	p.selected--
	if p.selected < 0 {
		p.selected = len(p.commands) - 1
	}
	p.clampOffset()
}

// selectDown moves selection down by one.
func (p *slashPicker) selectDown() {
	if !p.visible || len(p.commands) == 0 {
		return
	}
	p.selected++
	if p.selected >= len(p.commands) {
		p.selected = 0
	}
	p.clampOffset()
}

// SelectedName returns the name of the currently selected command, or empty.
func (p slashPicker) SelectedName() string {
	if !p.visible || len(p.commands) == 0 {
		return ""
	}
	return p.commands[p.selected].Name
}

// clampOffset keeps the scroll offset valid for the current selection.
func (p *slashPicker) clampOffset() {
	maxOffset := max(0, len(p.commands)-5)
	if p.offset > maxOffset {
		p.offset = maxOffset
	}
	// Keep selected item in view when possible.
	if p.selected < p.offset {
		p.offset = p.selected
	}
	if p.selected >= p.offset+5 {
		p.offset = p.selected - 4
	}
}

// View renders the picker dropdown.
func (p slashPicker) View() string {
	if !p.visible || len(p.commands) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, pickerStyles.header.Render("  slash commands"))

	// Items (limit to 5 to save screen space, using offset for scrolling).
	for i := 0; i < 5; i++ {
		idx := p.offset + i
		if idx >= len(p.commands) {
			break
		}
		line := p.renderItem(p.commands[idx], idx == p.selected)
		lines = append(lines, line)
	}

	// Wrap in a box with background and border.
	return pickerStyles.box.Render(strings.Join(lines, "\n"))
}

func (p slashPicker) renderItem(cmd slashcmd.CommandMatch, selected bool) string {
	// Use the primary command name (or first alias if it's shorter).
	nameStr := cmd.Name
	if len(cmd.Aliases) > 0 {
		// Show "name (alias)" only when the alias is useful for matching.
		nameStr = cmd.Name
	}

	desc := cmd.Description
	if cmd.Usage != "" {
		desc = cmd.Usage + " — " + cmd.Description
	}

	// Truncate description to fit available width.
	maxDesc := p.width - 35
	if maxDesc < 20 {
		maxDesc = 20
	}
	if len(desc) > maxDesc {
		desc = desc[:maxDesc-1] + "…"
	}

	var line string
	if selected {
		line = pickerStyles.selected.Render(nameStr + "  " + desc)
	} else {
		line = pickerStyles.regular.Render(nameStr + "  " + desc)
	}
	return line
}
