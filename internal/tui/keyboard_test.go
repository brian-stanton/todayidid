package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestStandardEditorControlKeysKeepEditing(t *testing.T) {
	t.Run("delete previous word", func(t *testing.T) {
		m := reviewModel(t)
		reviewPaste(m, "first second")
		reviewControl(m, 'w')
		if m.editor.Value() != "first " || m.showNotebooks || !m.editor.Focused() {
			t.Fatal("Ctrl+W opened application UI instead of deleting the previous word")
		}
	})
	t.Run("next and previous line", func(t *testing.T) {
		m := reviewModel(t)
		reviewPaste(m, "one\ntwo\nthree")
		reviewControl(m, 'p')
		if m.editor.Line() != 1 {
			t.Fatal("Ctrl+P did not move to the previous editor line")
		}
		reviewControl(m, 'p')
		reviewControl(m, 'n')
		if m.editor.Line() != 1 || m.editor.Value() != "one\ntwo\nthree" || m.pending != nil {
			t.Fatal("Ctrl+N replaced the entry instead of moving to the next line")
		}
	})
	t.Run("character forward", func(t *testing.T) {
		m := reviewModel(t)
		reviewPaste(m, "abcd")
		m.editor.CursorStart()
		reviewControl(m, 'f')
		if m.editor.Column() != 1 || m.mode != writing || m.searching {
			t.Fatal("Ctrl+F opened history search instead of moving the editor cursor")
		}
	})
}

func TestEditorEscapeKeepsFocusAndDraft(t *testing.T) {
	m := reviewModel(t)
	for _, body := range []string{"", "An unfinished entry"} {
		m.editor.SetValue(body)
		m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		if m.mode != writing || !m.editor.Focused() || m.editor.Value() != body || m.pending != nil {
			t.Fatal("Escape in the editor navigated away or changed the draft")
		}
	}
}

func TestCommandMenuKeysDoNotLeakIntoEditor(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Draft")
	reviewControl(m, 'g')
	reviewRune(m, 'x') // Not a command; must not type into the journal.
	m.Update(tea.PasteMsg{Content: "Clipboard text"})
	m.Update(clipboardMsg{draftID: m.draftID, content: "Late clipboard result"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.editor.Value() != "Draft" || !m.editor.Focused() {
		t.Fatal("command menu input leaked into the editor or failed to return focus")
	}
	reviewControl(m, 'g')
	reviewRune(m, 'l')
	if !m.editingLocation || !m.locationInput.Focused() || m.locationInput.Value() != "" || m.editor.Value() != "Draft" {
		t.Fatal("location command mnemonic was typed into an input instead of opening it")
	}
}

func TestCommandMenuSelectAllKeepsSelectionAvailable(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Select this draft")
	reviewControl(m, 'g')
	reviewRune(m, 'a')
	if !m.editor.HasSelection() || !m.editor.Focused() || m.editor.Value() != "Select this draft" {
		t.Fatal("command menu did not provide the editor's select-all action")
	}
}

func TestCommandMenuNewEntryProtectsUnsavedDraft(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Unsaved draft")
	reviewControl(m, 'g')
	reviewRune(m, 'n')
	if m.pending == nil || m.pending.kind != "new" || m.editor.Value() != "Unsaved draft" {
		t.Fatal("new-entry menu command discarded the unsaved draft")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.pending != nil || !m.editor.Focused() || m.editor.Value() != "Unsaved draft" {
		t.Fatal("canceling new-entry confirmation failed to restore the draft")
	}
}

func TestSearchEnterReadsAndEscapeReturnsThroughHistory(t *testing.T) {
	m := reviewModel(t)
	for _, body := range []string{"Finished the migration", "Went for a walk"} {
		if _, err := m.store.Save("personal", nil, body); err != nil {
			t.Fatal(err)
		}
	}
	reviewCommand(m, m.load())
	reviewPaste(m, "Preserved live draft")
	m.runCommand(commandSearch)
	reviewPaste(m, "migration")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.mode != reading || m.searching || m.selected() == nil || !strings.Contains(m.selected().Content, "migration") {
		t.Fatal("Enter in search did not read the highlighted matching entry")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.mode != browsing || m.search.Value() != "migration" || len(m.list.Items()) != 1 {
		t.Fatal("Escape from reading lost the filtered history context")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.mode != writing || !m.editor.Focused() || m.editor.Value() != "Preserved live draft" {
		t.Fatal("Escape from history did not return to the preserved editor draft")
	}
}

func TestCancelSearchRestoresPreviousModeQueryAndSelection(t *testing.T) {
	for _, previousMode := range []mode{writing, browsing, reading} {
		t.Run(map[mode]string{writing: "writing", browsing: "browsing", reading: "reading"}[previousMode], func(t *testing.T) {
			m := reviewModel(t)
			for _, body := range []string{"river walk", "river book", "mountain"} {
				if _, err := m.store.Save("personal", nil, body); err != nil {
					t.Fatal(err)
				}
			}
			reviewCommand(m, m.load())
			reviewPaste(m, "Live draft")
			m.search.SetValue("river")
			m.refilter()
			m.list.Select(1)
			m.updatePreview()
			selectedID := m.selected().ID
			m.mode = previousMode
			m.resize()
			m.syncFocus()
			m.runCommand(commandSearch)
			reviewControl(m, 'u')
			reviewPaste(m, "mountain")
			m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
			if m.searching || m.mode != previousMode || m.search.Value() != "river" || m.selected() == nil || m.selected().ID != selectedID {
				t.Fatalf("canceling search lost previous context: mode=%v query=%q selected=%+v", m.mode, m.search.Value(), m.selected())
			}
			if m.editor.Value() != "Live draft" || previousMode == writing && !m.editor.Focused() {
				t.Fatal("canceling search changed the draft or failed to restore editor focus")
			}
		})
	}
}

func TestSearchEnterWithNoResultsKeepsInputActive(t *testing.T) {
	m := reviewModel(t)
	m.runCommand(commandSearch)
	for _, query := range []string{"", "no matching entries"} {
		m.search.SetValue(query)
		m.refilter()
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if !m.searching || !m.search.Focused() || m.mode != browsing {
			t.Fatal("Enter with no results left the active search field")
		}
	}
}

func TestLocationMenuWorksDuringSaveWithoutLosingDraftInput(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Snapshot")
	cmd := reviewControl(m, 's')
	if cmd == nil {
		t.Fatal("expected save command")
	}
	reviewPaste(m, " and newer text")
	reviewControl(m, 'g')
	reviewRune(m, 'l')
	if !m.editingLocation || !m.locationInput.Focused() {
		t.Fatal("location menu command was swallowed while saving")
	}
	reviewPaste(m, "Portland")
	m.Update(cmd())
	if !m.editingLocation || m.locationInput.Value() != "Portland" || !m.locationInput.Focused() {
		t.Fatal("save completion replaced the active location edit")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.location != "Portland" || m.editor.Value() != "Snapshot and newer text" || m.baseline != "Snapshot" || !m.dirty() {
		t.Fatal("save completion lost newer body or location input")
	}
}

func TestNewEntryCannotDiscardBeforeInFlightSaveCompletes(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Original")
	reviewCommand(m, reviewControl(m, 's'))
	reviewControl(m, 'u')
	reviewPaste(m, "Snapshot")
	save := reviewControl(m, 's')
	if save == nil {
		t.Fatal("expected in-flight save")
	}
	// The live editor can return to the old saved baseline before the new
	// snapshot finishes. A navigation command must still wait for that write.
	reviewControl(m, 'u')
	reviewPaste(m, "Original")
	if m.dirty() {
		t.Fatal("test setup did not restore the old saved baseline")
	}
	reviewControl(m, 'g')
	reviewRune(m, 'n')
	if m.pending == nil || m.pending.kind != "new" || m.editor.Value() != "Original" {
		t.Fatal("new-entry command ran before the in-flight save completed")
	}
	reviewRune(m, 'd')
	if m.pending == nil || m.editor.Value() != "Original" || !m.saving {
		t.Fatal("discard confirmation ran before save completion")
	}
	m.Update(save())
	if m.pending == nil || !m.dirty() || m.editor.Value() != "Original" || m.baseline != "Snapshot" {
		t.Fatal("snapshot completion discarded newer editor input")
	}
	reviewRune(m, 'd')
	if m.pending != nil || m.editing != nil || m.editor.Value() != "" {
		t.Fatal("confirmed new entry did not open after the save completed")
	}
	entries, err := m.store.List("personal")
	if err != nil || len(entries) != 1 || entries[0].Content != "Snapshot" {
		t.Fatalf("new-entry transition corrupted the saved snapshot: entries=%+v err=%v", entries, err)
	}
}

func TestNotebookFilteredSelectionOverridesTypedPrefix(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Unsaved personal note")
	m.runCommand(commandNotebooks)
	reviewPaste(m, "wo")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.pending == nil || m.pending.kind != "notebook" || m.pending.notebook != "work" {
		t.Fatalf("Enter did not choose the highlighted matching notebook: pending=%+v", m.pending)
	}
	if m.editor.Value() != "Unsaved personal note" || m.notebook != "personal" {
		t.Fatal("choosing another notebook discarded the current draft before confirmation")
	}
}

func TestNotebookExactMatchIsPreferredOverOtherSubstringMatches(t *testing.T) {
	m := reviewModel(t)
	if _, err := m.store.Save("a-work", nil, "Another notebook"); err != nil {
		t.Fatal(err)
	}
	reviewCommand(m, m.load())
	m.runCommand(commandNotebooks)
	reviewPaste(m, "work")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	reviewCommand(m, cmd)
	if m.notebook != "work" {
		t.Fatalf("typed exact name selected another substring match: %q", m.notebook)
	}
}

func TestNotebookCanCreateNameThatMatchesPartOfExistingName(t *testing.T) {
	m := reviewModel(t)
	if _, err := m.store.Save("work-log", nil, "Existing notebook"); err != nil {
		t.Fatal(err)
	}
	reviewCommand(m, m.load())
	m.runCommand(commandNotebooks)
	reviewPaste(m, "log")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // Explicit Create log row.
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	reviewCommand(m, cmd)
	if m.notebook != "log" {
		t.Fatalf("could not create a notebook whose name matches an existing substring: %q", m.notebook)
	}
}

func TestNotebookPickerTypingAndCancelPreserveDraft(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Original draft")
	m.runCommand(commandNotebooks)
	reviewPaste(m, "first second")
	reviewControl(m, 'w')
	if !m.showNotebooks || m.notebookInput.Value() != "first " {
		t.Fatal("Ctrl+W closed the notebook picker instead of deleting its previous word")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.showNotebooks || m.notebook != "personal" || m.editor.Value() != "Original draft" || !m.editor.Focused() {
		t.Fatal("canceling notebook input changed the current draft or focus")
	}
}

func TestFieldClipboardCannotCrossIntoAnotherInput(t *testing.T) {
	m := reviewModel(t)
	m.runCommand(commandNotebooks)
	oldPaste := fieldClipboardMsg{generation: m.inputGeneration, target: "notebook", content: "Private notebook name"}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.runCommand(commandLocation)
	m.Update(oldPaste)
	if m.locationInput.Value() != "" || !m.locationInput.Focused() {
		t.Fatal("notebook clipboard result leaked into the location input")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.runCommand(commandSearch)
	m.Update(oldPaste)
	if m.search.Value() != "" || !m.search.Focused() {
		t.Fatal("notebook clipboard result leaked into the search input")
	}
	m.Update(fieldClipboardMsg{generation: m.inputGeneration, target: "search", content: "Current search"})
	if m.search.Value() != "Current search" {
		t.Fatal("clipboard result for the current search input was not accepted")
	}
}

func TestCanceledFieldClipboardCannotPasteIntoReopenedField(t *testing.T) {
	for _, field := range []struct {
		name string
		id   commandID
	}{
		{name: "search", id: commandSearch},
		{name: "notebook", id: commandNotebooks},
	} {
		t.Run(field.name, func(t *testing.T) {
			m := reviewModel(t)
			m.runCommand(field.id)
			oldPaste := fieldClipboardMsg{generation: m.inputGeneration, target: field.name, content: "Old clipboard text"}
			m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
			m.runCommand(field.id)
			reviewPaste(m, "New")
			m.Update(oldPaste)
			value := m.search.Value()
			if field.name == "notebook" {
				value = m.notebookInput.Value()
			}
			if value != "New" {
				t.Fatalf("canceled clipboard result changed reopened %s input: %q", field.name, value)
			}
			m.Update(fieldClipboardMsg{generation: m.inputGeneration, target: field.name, content: " current"})
			value = m.search.Value()
			if field.name == "notebook" {
				value = m.notebookInput.Value()
			}
			if value != "New current" {
				t.Fatalf("current clipboard result was rejected for %s: %q", field.name, value)
			}
		})
	}
}

func TestUnmatchedNotebookNameCanBeCreatedAfterArrowInput(t *testing.T) {
	m := reviewModel(t)
	m.runCommand(commandNotebooks)
	reviewPaste(m, "travel")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	reviewCommand(m, cmd)
	if m.notebook != "travel" || m.showNotebooks || !m.editor.Focused() {
		t.Fatal("unmatched notebook name was lost when navigating an empty filtered list")
	}
}
