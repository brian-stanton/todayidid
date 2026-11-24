package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func openCommandMenu(t *testing.T, m *Model) {
	t.Helper()
	reviewControl(m, 'g')
	if !m.showCommands || m.editor.Focused() {
		t.Fatal("command menu did not take focus")
	}
}

func TestCommandMnemonicCreatesNewEntry(t *testing.T) {
	m := reviewModel(t)
	before := m.draftID
	openCommandMenu(t, m)
	m.commandIndex = len(m.availableCommands()) - 1
	reviewRune(m, 'n')
	if m.showCommands || m.draftID == before || m.mode != writing || !m.editor.Focused() {
		t.Fatalf("n did not create a focused new entry: menu=%v draft=%d mode=%v focus=%v", m.showCommands, m.draftID, m.mode, m.editor.Focused())
	}
}

func TestCommandMenuSavesAndProtectsUnsavedQuit(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "A useful note.")
	openCommandMenu(t, m)
	reviewCommand(m, reviewRune(m, 's'))
	entries, err := m.store.List(m.notebook)
	if err != nil || len(entries) != 1 || entries[0].Content != "A useful note." || m.showCommands {
		t.Fatalf("menu save failed: entries=%v err=%v menu=%v", entries, err, m.showCommands)
	}
	reviewPaste(m, " More to keep.")
	openCommandMenu(t, m)
	reviewRune(m, 'q')
	if m.showCommands || m.pending == nil || m.pending.kind != "quit" || !m.dirty() {
		t.Fatal("menu quit bypassed unsaved changes")
	}
}

func TestCommandMenuNavigationAndCancel(t *testing.T) {
	m := reviewModel(t)
	openCommandMenu(t, m)
	reviewRune(m, 'j')
	if m.commandIndex != 1 {
		t.Fatalf("j selected %d, want 1", m.commandIndex)
	}
	reviewRune(m, 'k')
	if m.commandIndex != 0 {
		t.Fatalf("k selected %d, want 0", m.commandIndex)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.commandIndex != len(m.availableCommands())-1 {
		t.Fatal("up did not wrap to the final command")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.showCommands || !m.editor.Focused() {
		t.Fatal("escape did not return focus to the draft")
	}
}

func TestCommandsFollowCurrentContext(t *testing.T) {
	m := reviewModel(t)
	contains := func(id commandID) bool {
		for _, item := range m.availableCommands() {
			if item.id == id {
				return true
			}
		}
		return false
	}
	if !contains(commandSelectAll) || !contains(commandLocation) || contains(commandEdit) {
		t.Fatal("editor has the wrong command set")
	}
	m.mode = browsing
	if contains(commandSelectAll) || contains(commandLocation) || contains(commandEdit) {
		t.Fatal("empty history exposes commands requiring an entry or editor")
	}
	if _, err := m.store.Save(m.notebook, nil, "Saved note"); err != nil {
		t.Fatal(err)
	}
	reviewCommand(m, m.load())
	if !contains(commandLocation) || !contains(commandEdit) || contains(commandSelectAll) {
		t.Fatal("selected history entry has the wrong command set")
	}
}

func TestCommandSurfacesFitTerminal(t *testing.T) {
	for _, size := range [][2]int{{42, 16}, {80, 24}, {120, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := reviewModel(t)
			m.width, m.height = size[0], size[1]
			m.resize()
			m.showCommands = true
			for i := range m.availableCommands() {
				m.commandIndex = i
				card := m.commandsView(m.dimensions().width)
				if lipgloss.Width(card) > m.width || lipgloss.Height(card) > m.height {
					t.Fatalf("command card exceeds terminal: %dx%d", lipgloss.Width(card), lipgloss.Height(card))
				}
				lines := strings.Split(ansi.Strip(card), "\n")
				if !strings.HasPrefix(lines[0], "╭") || !strings.HasSuffix(lines[0], "╮") || !strings.HasPrefix(lines[len(lines)-1], "╰") || !strings.HasSuffix(lines[len(lines)-1], "╯") {
					t.Fatalf("card lost its border: %q", card)
				}
			}
			inner := min(m.dimensions().width, 68) - 8
			help := m.keyboardHelp(inner)
			if lipgloss.Width(help) > inner || lipgloss.Height(help) > m.height-6 {
				t.Fatalf("keyboard help exceeds its inner bounds: %dx%d", lipgloss.Width(help), lipgloss.Height(help))
			}
		})
	}
}

func TestNarrowEditorHintsKeepWholeActions(t *testing.T) {
	m := reviewModel(t)
	got := m.footerHints(38)
	if got != "^s save  Tab history  ^g commands" {
		t.Fatalf("narrow footer lost a primary action or cut a hint: %q", got)
	}
	for width := 1; width < 100; width++ {
		got := fitHints(width, keyHint{"Ctrl+s save", "^s save"}, keyHint{"Ctrl+g commands", "^g commands"})
		if lipgloss.Width(got) > width {
			t.Fatalf("footer width %d exceeds %d", lipgloss.Width(got), width)
		}
		for _, part := range strings.Split(got, "  ") {
			if part != "" && part != "Ctrl+s save" && part != "^s save" && part != "Ctrl+g commands" && part != "^g commands" {
				t.Fatalf("partial hint at width %d: %q", width, part)
			}
		}
	}
}
