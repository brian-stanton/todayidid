package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type commandID uint8

const (
	commandSave commandID = iota
	commandNew
	commandLocation
	commandNotebooks
	commandStyle
	commandSearch
	commandReload
	commandEdit
	commandSelectAll
	commandHelp
	commandQuit
)

type command struct {
	id    commandID
	key   string
	label string
}

// The menu and keyboard help share this registry. Ordinary editing shortcuts
// stay with the text controls; application actions use a mnemonic after Ctrl+g.
var commandRegistry = [...]command{
	{commandSave, "s", "Save entry"},
	{commandNew, "n", "New entry"},
	{commandLocation, "l", "Location"},
	{commandNotebooks, "b", "Notebooks"},
	{commandStyle, "o", "Notebook style"},
	{commandSearch, "/", "Search history"},
	{commandReload, "r", "Reload history"},
	{commandEdit, "e", "Edit entry"},
	{commandSelectAll, "a", "Select all"},
	{commandHelp, "?", "Keyboard shortcuts"},
	{commandQuit, "q", "Quit"},
}

func (m *Model) availableCommands() []command {
	commands := make([]command, 0, len(commandRegistry))
	for _, item := range commandRegistry {
		switch item.id {
		case commandSave:
			if m.mode != writing {
				item.label = "Save draft"
			}
		case commandLocation:
			if m.mode != writing {
				if m.selected() == nil {
					continue
				}
				item.label = "Edit location"
			}
		case commandEdit:
			if m.mode == writing || m.selected() == nil {
				continue
			}
		case commandSelectAll:
			if m.mode != writing {
				continue
			}
		}
		commands = append(commands, item)
	}
	return commands
}

func (m *Model) updateCommands(message tea.Msg) tea.Cmd {
	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	commands := m.availableCommands()
	m.commandIndex = min(max(0, m.commandIndex), len(commands)-1)
	run := func(id commandID) tea.Cmd {
		m.showCommands = false
		m.syncFocus()
		return m.runCommand(id)
	}
	switch key.String() {
	case "esc", "ctrl+g", "f1":
		m.showCommands = false
		m.syncFocus()
		return nil
	case "ctrl+c":
		return run(commandQuit)
	case "ctrl+s":
		return run(commandSave)
	case "up", "k":
		m.commandIndex = (m.commandIndex - 1 + len(commands)) % len(commands)
		return nil
	case "down", "j":
		m.commandIndex = (m.commandIndex + 1) % len(commands)
		return nil
	case "enter":
		return run(commands[m.commandIndex].id)
	}
	for _, item := range commands {
		if strings.ToLower(key.String()) == item.key {
			return run(item.id)
		}
	}
	return nil
}

// commandsView returns a complete card; the parent view centers it. Selection
// scrolls within the available height, so the bottom border is always visible.
func (m *Model) commandsView(width int) string {
	width = min(width, 52)
	inner := max(1, width-6)
	commands := m.availableCommands()
	index := min(max(0, m.commandIndex), len(commands)-1)
	visible := min(len(commands), max(1, m.height-10))
	start := max(0, index-visible+1)
	lines := []string{
		spread(m.heading("Commands"), m.muted(fmt.Sprintf("%d / %d", index+1, len(commands))), inner),
		"",
	}
	for i := start; i < min(len(commands), start+visible); i++ {
		item := commands[i]
		prefix := " "
		style := lipgloss.NewStyle().Foreground(m.colors.ink).Width(inner)
		if i == index {
			prefix = "›"
			style = style.Foreground(m.colors.accent).Background(m.colors.selected)
		}
		lines = append(lines, style.Render(fmt.Sprintf("%s %-2s %s", prefix, item.key, item.label)))
	}
	lines = append(lines, "", m.muted(fitHints(inner,
		keyHint{"↑↓ choose", "↑↓ choose"},
		keyHint{"Enter run", "Enter run"},
		keyHint{"Esc close", "Esc close"},
	)))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(m.colors.accent).
		Padding(1, 2).Width(width).Render(strings.Join(lines, "\n"))
}

// keyboardHelp returns inner dialog content. The compact form keeps essential
// editing keys visible; taller terminals also show the current command menu.
func (m *Model) keyboardHelp(width int) string {
	lines := []string{
		m.heading("Keyboard shortcuts"),
		"Ctrl+g  Commands (F1 alias)",
		"Ctrl+s  Save · Tab switch",
		"Ctrl+c  Quit · Esc close",
		"Home/End  Line start/end",
		"Alt+←/→  Move by word",
		"Ctrl+w  Delete word",
		"Ctrl+n/p  Next/previous line",
		"Ctrl+f  Forward character",
	}
	for _, item := range commandRegistry {
		if item.id == commandSelectAll {
			lines = append(lines, "Editor: Ctrl+g, "+item.key+"  "+item.label)
			break
		}
	}
	maxLines := max(1, m.height-6)
	commands := m.availableCommands()
	rows := commandHelpRows(commands, width)
	if len(lines)+2+len(rows) <= maxLines {
		lines = append(lines, "", m.heading("Commands: Ctrl+g, then a key"))
		lines = append(lines, rows...)
	}
	if len(lines)+4 <= maxLines {
		lines = append(lines, "", m.heading("History"), "↑↓ / j/k choose · Enter read", "/ search · e edit · r reload")
	}
	return strings.Join(lines[:min(len(lines), maxLines)], "\n")
}

func commandHelpRows(commands []command, width int) []string {
	rows := make([]string, 0, len(commands))
	column := 0
	for _, item := range commands {
		row := item.key + "  " + item.label
		rows = append(rows, row)
		column = max(column, lipgloss.Width(row))
	}
	if column*2+3 > width {
		return rows
	}
	paired := make([]string, 0, (len(rows)+1)/2)
	for i := 0; i < len(rows); i += 2 {
		row := rows[i]
		if i+1 < len(rows) {
			row += strings.Repeat(" ", column-lipgloss.Width(row)+3) + rows[i+1]
		}
		paired = append(paired, row)
	}
	return paired
}

type keyHint struct {
	full, compact string
}

// fitHints keeps complete hints in priority order, using compact key notation
// when necessary. It never leaves a partial key or action at the right edge.
func fitHints(width int, hints ...keyHint) string {
	full := make([]string, len(hints))
	for i, hint := range hints {
		full[i] = hint.full
	}
	if joined := strings.Join(full, "  "); lipgloss.Width(joined) <= width {
		return joined
	}
	var result string
	for _, hint := range hints {
		candidate := hint.compact
		if result != "" {
			candidate = result + "  " + candidate
		}
		if lipgloss.Width(candidate) > width {
			break
		}
		result = candidate
	}
	return result
}

func (m *Model) footerHints(width int) string {
	var hints []keyHint
	switch {
	case m.pending != nil:
		hints = []keyHint{{"Ctrl+s save", "^s save"}, {"d discard", "d discard"}, {"Esc cancel", "Esc cancel"}}
	case m.showCommands:
		hints = []keyHint{{"↑↓ choose", "↑↓ choose"}, {"Enter run", "Enter run"}, {"Esc close", "Esc close"}}
	case m.showHelp:
		hints = []keyHint{{"Esc close", "Esc close"}}
	case m.showSettings:
		hints = []keyHint{{"Enter apply", "Enter apply"}, {"Esc cancel", "Esc cancel"}, {"↑↓ style", "↑↓ style"}}
	case m.showNotebooks:
		hints = []keyHint{{"Enter open", "Enter open"}, {"Esc cancel", "Esc cancel"}, {"↑↓ choose", "↑↓ choose"}}
	case m.editingLocation:
		hints = []keyHint{{"Enter apply", "Enter apply"}, {"Esc cancel", "Esc cancel"}, {"↑↓ recent", "↑↓ recent"}, {"Ctrl+u clear", "^u clear"}}
	case m.searching:
		hints = []keyHint{{"Enter read", "Enter read"}, {"Esc cancel", "Esc cancel"}, {"↑↓ choose", "↑↓ choose"}, {"Tab results", "Tab results"}}
	case m.mode == browsing:
		hints = []keyHint{{"Enter read", "Enter read"}, {"Ctrl+g commands", "^g commands"}, {"/ search", "/ search"}, {"↑↓ choose", "↑↓ choose"}, {"Tab draft", "Tab draft"}}
	case m.mode == reading:
		hints = []keyHint{{"Esc history", "Esc back"}, {"Ctrl+g commands", "^g commands"}, {"↑↓ scroll", "↑↓ scroll"}, {"e edit", "e edit"}, {"Tab draft", "Tab draft"}}
	default:
		hints = []keyHint{{"Ctrl+s save", "^s save"}, {"Tab history", "Tab history"}, {"Ctrl+g commands", "^g commands"}, {"Ctrl+c quit", "^c quit"}}
	}
	return fitHints(width, hints...)
}
