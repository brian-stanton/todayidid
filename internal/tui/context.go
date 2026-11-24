package tui

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/brian-stanton/todayidid/internal/journal"
)

type settingsSavedMsg struct {
	notebook string
	settings journal.NotebookSettings
	err      error
}

type locationClipboardMsg struct {
	draftID    int
	generation int
	content    string
	err        error
}

func (m *Model) resetLocation() {
	m.location = m.sessionLocations[m.notebook]
	m.baselineLocation = ""
	m.historical = false
	m.editingLocation = false
	m.locationError = ""
}

func (m *Model) beginLocation() {
	m.locationGeneration++
	m.editingLocation = true
	m.locationInput.SetValue(m.location)
	if m.locationInput.Value() != m.location {
		m.editingLocation = false
		m.notice, m.problem = "This location needs an external editor; entry unchanged.", true
		return
	}
	m.locationInput.CursorEnd()
	m.locationQuery = ""
	m.locationChoice = -1
	m.locationError = ""
	m.refreshLocations()
	m.syncFocus()
}

func (m *Model) refreshLocations() {
	m.locationChoices = nil
	seen := make(map[string]bool)
	for _, entry := range m.entries {
		place := entry.Location
		if strings.ContainsRune(place, utf8.RuneError) {
			continue
		}
		key := strings.ToLower(place)
		if place != "" && !seen[key] && strings.Contains(key, strings.ToLower(m.locationQuery)) {
			m.locationChoices = append(m.locationChoices, place)
			seen[key] = true
		}
	}
}

func (m *Model) applyLocation() bool {
	place, err := journal.NormalizeLocation(m.locationInput.Value())
	if err != nil {
		m.locationError = err.Error()
		return false
	}
	m.location = place
	if !m.historical {
		m.sessionLocations[m.notebook] = place
	}
	m.editingLocation = false
	m.locationError = ""
	m.notice, m.problem = "", false
	m.syncFocus()
	return true
}

func (m *Model) updateLocation(message tea.Msg) tea.Cmd {
	if key, ok := message.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.editingLocation = false
			m.locationError = ""
			m.syncFocus()
			return nil
		case "enter":
			m.applyLocation()
			return nil
		case "ctrl+u":
			m.locationInput.Reset()
			m.locationQuery = ""
			m.locationChoice = -1
			m.locationError = ""
			m.refreshLocations()
			return nil
		case "ctrl+s":
			if m.applyLocation() {
				return m.save()
			}
			return nil
		case "ctrl+c":
			if m.applyLocation() {
				return m.request(action{kind: "quit"})
			}
			return nil
		case "up", "down", "tab", "shift+tab":
			if len(m.locationChoices) > 0 {
				step := 1
				if key.String() == "up" || key.String() == "shift+tab" {
					step = -1
				}
				if m.locationChoice < 0 && step < 0 {
					m.locationChoice = 0
				}
				m.locationChoice = (m.locationChoice + step + len(m.locationChoices)) % len(m.locationChoices)
				m.locationInput.SetValue(m.locationChoices[m.locationChoice])
				m.locationInput.CursorEnd()
			}
			return nil
		case "ctrl+v":
			draftID := m.draftID
			generation := m.locationGeneration
			return func() tea.Msg {
				content, err := clipboard.ReadAll()
				return locationClipboardMsg{draftID, generation, content, err}
			}
		}
	}
	if paste, ok := message.(tea.PasteMsg); ok {
		// Reject rather than silently flatten a multiline clipboard location.
		if _, err := journal.NormalizeLocation(paste.Content); err != nil {
			m.locationError = err.Error()
			return nil
		}
		if strings.ContainsRune(paste.Content, utf8.RuneError) {
			m.locationError = "Clipboard text contains an unsupported character."
			return nil
		}
	}
	previous := m.locationInput.Value()
	var cmd tea.Cmd
	m.locationInput, cmd = m.locationInput.Update(message)
	if previous != m.locationInput.Value() {
		m.locationChoice = -1
		m.locationQuery = m.locationInput.Value()
		m.refreshLocations()
		m.locationError = ""
	}
	return cmd
}

func (m *Model) updateSettings(message tea.Msg) tea.Cmd {
	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch key.String() {
	case "esc":
		m.showSettings = false
		m.syncFocus()
	case "ctrl+c":
		m.showSettings = false
		return m.request(action{kind: "quit"})
	case "left", "right", "up", "down", "tab", "shift+tab", "space":
		if m.settingsDraft == journal.StyleJournal {
			m.settingsDraft = journal.StyleNotes
		} else {
			m.settingsDraft = journal.StyleJournal
		}
	case "j":
		m.settingsDraft = journal.StyleJournal
	case "n":
		m.settingsDraft = journal.StyleNotes
	case "enter", "ctrl+s":
		m.savingSettings = true
		store, notebook := m.store, m.notebook
		settings := journal.NotebookSettings{Style: m.settingsDraft}
		return func() tea.Msg { return settingsSavedMsg{notebook, settings, store.SaveSettings(notebook, settings)} }
	}
	return nil
}

func (m *Model) settingsSaved(msg settingsSavedMsg) tea.Cmd {
	m.savingSettings = false
	if msg.notebook != m.notebook {
		return nil
	}
	if msg.err != nil {
		m.notice, m.problem = "Notebook settings: "+msg.err.Error(), true
		return nil
	}
	m.settings = msg.settings
	m.showSettings = false
	m.notice, m.problem = "", false
	m.resize()
	m.syncFocus()
	// Supersede any startup or refresh response containing the old preference.
	return m.load()
}

func (m *Model) styleLocationInput() {
	s := textinput.DefaultStyles(m.dark)
	s.Focused.Text = m.headingStyle()
	m.locationInput.SetStyles(s)
}
