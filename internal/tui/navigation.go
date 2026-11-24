package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/brian-stanton/todayidid/internal/journal"
)

// Application actions share one path whether chosen by mnemonic or from a
// history view. Destructive transitions always go through request.
func (m *Model) runCommand(id commandID) tea.Cmd {
	switch id {
	case commandSave:
		return m.save()
	case commandNew:
		return m.request(action{kind: "new"})
	case commandLocation:
		if m.mode == writing {
			m.beginLocation()
		} else if entry := m.selected(); entry != nil {
			return m.request(action{kind: "edit-location", entry: entry})
		}
	case commandNotebooks:
		m.inputGeneration++
		m.showNotebooks = true
		m.notebookInput.Reset()
		m.notebookIndex = 0
		for i, name := range m.notebooks {
			if name == m.notebook {
				m.notebookIndex = i
				break
			}
		}
	case commandStyle:
		m.showSettings = true
		m.settingsDraft = m.settings.Style
	case commandSearch:
		m.beginSearch()
	case commandReload:
		return m.load()
	case commandEdit:
		if entry := m.selected(); entry != nil {
			return m.request(action{kind: "edit", entry: entry})
		}
	case commandSelectAll:
		if m.mode == writing {
			m.syncFocus()
			var cmd tea.Cmd
			m.editor, cmd = m.editor.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
			return cmd
		}
	case commandHelp:
		m.showHelp = true
	case commandQuit:
		return m.request(action{kind: "quit"})
	}
	m.syncFocus()
	return nil
}

func (m *Model) beginSearch() {
	if !m.searching {
		m.inputGeneration++
		m.searchReturn = m.mode
		m.searchScroll = m.preview.YOffset()
		m.searchBefore = m.search.Value()
		m.searchSelection = ""
		if entry := m.selected(); entry != nil {
			m.searchSelection = entry.ID
		}
	}
	m.mode = browsing
	m.searching = true
	m.resize()
	m.syncFocus()
}

func (m *Model) cancelSearch() {
	m.search.SetValue(m.searchBefore)
	m.refilter()
	for i, item := range m.list.Items() {
		if item.(entryItem).entry.ID == m.searchSelection {
			m.list.Select(i)
			break
		}
	}
	m.updatePreview()
	m.mode = m.searchReturn
	m.searching = false
	m.resize()
	m.preview.SetYOffset(m.searchScroll)
	m.syncFocus()
}

// A clipboard read belongs to the field that requested it. A late response
// must never become input in a different field, or in a reopened picker.
func (m *Model) pasteField(target string) tea.Cmd {
	generation := m.inputGeneration
	return func() tea.Msg {
		content, err := clipboard.ReadAll()
		return fieldClipboardMsg{generation: generation, target: target, content: content, err: err}
	}
}

type notebookChoice struct {
	name   string
	create bool
}

func (m *Model) notebookChoices() []notebookChoice {
	query := strings.TrimSpace(m.notebookInput.Value())
	folded := strings.ToLower(query)
	var choices []notebookChoice
	exact := false
	for _, name := range m.notebooks {
		if name == query {
			choices = append(choices, notebookChoice{name: name})
			exact = true
		}
	}
	for _, name := range m.notebooks {
		if name != query && strings.Contains(strings.ToLower(name), folded) {
			choices = append(choices, notebookChoice{name: name})
		}
	}
	if !exact && journal.ValidateNotebook(query) == nil {
		choices = append(choices, notebookChoice{name: query, create: true})
	}
	return choices
}
