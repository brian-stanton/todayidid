package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/brian-stanton/todayidid/internal/journal"
	"github.com/charmbracelet/x/ansi"
)

func TestLocationHeaderAndSettingsFitTerminal(t *testing.T) {
	for _, size := range [][2]int{{42, 16}, {64, 20}, {100, 30}, {140, 50}} {
		for _, style := range []journal.NotebookStyle{journal.StyleJournal, journal.StyleNotes} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], style), func(t *testing.T) {
				m := reviewModel(t)
				m.settings.Style = style
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				contextLocation(t, m, strings.Repeat("渋谷", 60))
				for _, screen := range []string{"location", "settings"} {
					if screen == "location" {
						m.beginLocation()
					} else {
						m.editingLocation = false
						m.runCommand(commandStyle)
					}
					v := m.View()
					if lipgloss.Width(v.Content) > size[0] || lipgloss.Height(v.Content) > size[1] {
						t.Fatalf("%s overflowed terminal", screen)
					}
					if v.Cursor != nil && (v.Cursor.X < 0 || v.Cursor.X >= size[0] || v.Cursor.Y < 0 || v.Cursor.Y >= size[1]) {
						t.Fatalf("%s cursor outside terminal: %+v", screen, v.Cursor)
					}
				}
			})
		}
	}
}

func TestRecentLocationsExcludeUneditableText(t *testing.T) {
	m := reviewModel(t)
	m.entries = []journal.Entry{{Location: "A\uFFFDplace"}, {Location: "Portland"}}
	m.beginLocation()
	if len(m.locationChoices) != 1 || m.locationChoices[0] != "Portland" {
		t.Fatalf("unsafe recent location: %v", m.locationChoices)
	}
}

func TestJournalHeaderCanBeClickedToEditLocation(t *testing.T) {
	m := reviewModel(t)
	d := m.dimensions()
	m.Update(tea.MouseClickMsg{X: d.left + 3, Y: 5, Button: tea.MouseLeft})
	if !m.editingLocation || !m.locationInput.Focused() {
		t.Fatal("location label did not open its input")
	}
}

func contextLocation(t *testing.T, m *Model, location string) {
	t.Helper()
	m.runCommand(commandLocation)
	if !m.editingLocation || !m.locationInput.Focused() {
		t.Fatal("Location command did not focus the location input")
	}
	reviewControl(m, 'u')
	if location != "" {
		reviewPaste(m, location)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.editingLocation || m.location != location || !m.editor.Focused() {
		t.Fatalf("location was not applied: got=%q want=%q editing=%v error=%q", m.location, location, m.editingLocation, m.locationError)
	}
}

func contextStyle(t *testing.T, m *Model, style journal.NotebookStyle) {
	t.Helper()
	m.runCommand(commandStyle)
	if !m.showSettings {
		t.Fatal("Style command did not open notebook settings")
	}
	if style == journal.StyleJournal {
		reviewRune(m, 'j')
	} else {
		reviewRune(m, 'n')
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("confirming style did not schedule persistence")
	}
	_, reload := m.Update(cmd())
	reviewCommand(m, reload)
	if m.settings.Style != style || m.showSettings || m.savingSettings {
		t.Fatalf("style not applied: got=%s want=%s", m.settings.Style, style)
	}
}

func TestLocationPersistsWithEntryAndAppearsInHistory(t *testing.T) {
	m := reviewModel(t)
	contextLocation(t, m, "Portland, OR")
	reviewPaste(m, "A walk along the river.")
	reviewCommand(m, reviewControl(m, 's'))
	entries, err := m.store.List("personal")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Location != "Portland, OR" || entries[0].Content != "A walk along the river." || m.dirty() {
		t.Fatalf("body and location did not save together: %+v", entries)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(ansi.Strip(m.View().Content), "Portland, OR") {
		t.Fatal("saved location is missing from the reading view")
	}
	reopened := New(m.store, "personal")
	reviewCommand(reopened, reopened.load())
	if reopened.location != "" || reopened.entries[0].Location != "Portland, OR" {
		t.Fatal("reopening inferred the current place from a historical entry")
	}
}

func TestLocationOnlyEditAndClearPreserveBodyAndEntry(t *testing.T) {
	m := reviewModel(t)
	contextLocation(t, m, "Seattle")
	reviewPaste(m, "Original body")
	reviewCommand(m, reviewControl(m, 's'))
	id := m.editing.ID
	for _, location := range []string{"Vancouver", ""} {
		contextLocation(t, m, location)
		if !m.dirty() {
			t.Fatal("changing only a saved entry's location did not mark it dirty")
		}
		reviewCommand(m, reviewControl(m, 's'))
		entries, err := m.store.List("personal")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].ID != id || entries[0].Content != "Original body" || entries[0].Location != location || m.dirty() {
			t.Fatalf("location edit duplicated entry or changed body: %+v", entries)
		}
	}
}

func TestBlankDraftWithLocationCanQuitWithoutSaveTrap(t *testing.T) {
	m := reviewModel(t)
	contextLocation(t, m, "Portland")
	if m.dirty() {
		t.Fatal("a location-only empty draft should not need an empty entry saved")
	}
	cmd := reviewControl(m, 'c')
	if m.pending != nil || cmd == nil {
		t.Fatal("blank draft with a place opened an unsaved-changes dialog")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("blank draft quit did not terminate normally")
	}
	entries, err := m.store.List("personal")
	if err != nil || len(entries) != 0 {
		t.Fatalf("blank draft created an entry: entries=%+v err=%v", entries, err)
	}
}

func TestLocationCancelPreservesDraftAndCurrentPlace(t *testing.T) {
	m := reviewModel(t)
	contextLocation(t, m, "Portland")
	reviewPaste(m, "Unfinished body")
	m.runCommand(commandLocation)
	reviewControl(m, 'u')
	reviewPaste(m, "Discard this location")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.location != "Portland" || m.sessionLocations["personal"] != "Portland" || m.editor.Value() != "Unfinished body" || !m.editor.Focused() || m.editingLocation {
		t.Fatal("canceling the location edit changed the draft or its current place")
	}
}

func TestCanceledLocationClipboardCannotPasteIntoReopenedField(t *testing.T) {
	m := reviewModel(t)
	m.runCommand(commandLocation)
	oldPaste := locationClipboardMsg{draftID: m.draftID, generation: m.locationGeneration, content: "Old location"}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.runCommand(commandLocation)
	reviewPaste(m, "New")
	m.Update(oldPaste)
	if m.locationInput.Value() != "New" {
		t.Fatal("clipboard result from canceled input changed the reopened location field")
	}
	m.Update(locationClipboardMsg{draftID: m.draftID, generation: m.locationGeneration, content: " location"})
	if m.locationInput.Value() != "New location" {
		t.Fatal("clipboard result for the current location edit was not accepted")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.location != "New location" {
		t.Fatal("current location clipboard text was not applied")
	}
}

func TestUnrepresentableHistoricalLocationIsNotChangedByOpeningField(t *testing.T) {
	m := reviewModel(t)
	entry, err := m.store.SaveWithLocation("personal", nil, "Body", "Literal \uFFFD place")
	if err != nil {
		t.Fatal(err)
	}
	m.request(action{kind: "edit", entry: &entry})
	m.runCommand(commandLocation)
	if m.editingLocation || !m.problem || m.location != entry.Location || m.dirty() || m.editor.Value() != "Body" {
		t.Fatal("opening an unrepresentable historical place silently changed the entry")
	}
}

func TestInvalidLocationDoesNotAlterEntryOrFlattenInput(t *testing.T) {
	m := reviewModel(t)
	contextLocation(t, m, "Portland")
	reviewPaste(m, "Untouched draft")
	m.runCommand(commandLocation)
	reviewPaste(m, "Seattle\nOffice")
	if m.locationInput.Value() != "Portland" || m.locationError == "" || m.location != "Portland" {
		t.Fatal("multiline location paste was silently flattened or applied")
	}
	reviewControl(m, 'u')
	for range journal.MaxLocationRunes + 1 {
		reviewRune(m, 'a')
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.editingLocation || m.locationError == "" || m.location != "Portland" || m.editor.Value() != "Untouched draft" {
		t.Fatal("oversized location changed the entry instead of leaving a validation error")
	}
}

func TestSessionLocationsStayWithinTheirNotebook(t *testing.T) {
	m := reviewModel(t)
	contextLocation(t, m, "Home")
	reviewCommand(m, m.request(action{kind: "notebook", notebook: "work"}))
	if m.location != "" {
		t.Fatal("personal location leaked into work")
	}
	contextLocation(t, m, "Office")
	reviewCommand(m, m.request(action{kind: "notebook", notebook: "personal"}))
	if m.location != "Home" {
		t.Fatal("personal notebook lost its explicitly selected session place")
	}
	reviewCommand(m, m.request(action{kind: "notebook", notebook: "work"}))
	if m.location != "Office" {
		t.Fatal("work notebook lost its own session place")
	}
}

func TestHistoricalLocationEditDoesNotSetNextDraftLocation(t *testing.T) {
	m := reviewModel(t)
	entry, err := m.store.SaveWithLocation("personal", nil, "An older trip", "Paris")
	if err != nil {
		t.Fatal(err)
	}
	reviewCommand(m, m.load())
	contextLocation(t, m, "Home")
	m.request(action{kind: "edit", entry: &entry})
	if m.location != "Paris" || !m.historical {
		t.Fatal("historical edit did not load the entry's recorded place")
	}
	contextLocation(t, m, "Lyon")
	reviewCommand(m, reviewControl(m, 's'))
	m.runCommand(commandNew)
	if m.location != "Home" || m.historical || m.editor.Value() != "" {
		t.Fatalf("historical place leaked into new draft: location=%q historical=%v", m.location, m.historical)
	}
}

func TestRecentLocationsComeFromCurrentNotebookAndAreSelectable(t *testing.T) {
	m := reviewModel(t)
	for _, location := range []string{"Portland", "Seattle", "portland"} {
		if _, err := m.store.SaveWithLocation("personal", nil, "Personal entry", location); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.store.SaveWithLocation("work", nil, "Work entry", "Office"); err != nil {
		t.Fatal(err)
	}
	reviewCommand(m, m.load())
	m.runCommand(commandLocation)
	if len(m.locationChoices) != 2 || contains(m.locationChoices, "Office") {
		t.Fatalf("recent locations were not notebook-local and deduplicated: %+v", m.locationChoices)
	}
	wanted := m.locationChoices[0]
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.location != wanted {
		t.Fatalf("recent location could not be selected: got=%q want=%q", m.location, wanted)
	}
}

func TestNotebookStyleDefaultsAndPersistence(t *testing.T) {
	m := reviewModel(t)
	if m.settings.Style != journal.StyleJournal || m.dimensions().wide {
		t.Fatal("personal journal should open with a focused writing layout")
	}
	contextStyle(t, m, journal.StyleNotes)
	reopened := New(m.store, "personal")
	reviewCommand(reopened, reopened.load())
	if reopened.settings.Style != journal.StyleNotes || !reopened.dimensions().wide {
		t.Fatal("explicit notebook style did not persist across reopen")
	}
	work := New(m.store, "work")
	reviewCommand(work, work.load())
	if work.settings.Style != journal.StyleNotes {
		t.Fatal("work notebook should default to notes style")
	}
}

func TestChangingStyleKeepsDraftAndLocation(t *testing.T) {
	m := reviewModel(t)
	contextLocation(t, m, "Portland")
	reviewPaste(m, "Draft stays intact")
	for _, style := range []journal.NotebookStyle{journal.StyleNotes, journal.StyleJournal} {
		contextStyle(t, m, style)
		if m.editor.Value() != "Draft stays intact" || m.location != "Portland" || !m.dirty() || !m.editor.Focused() {
			t.Fatal("changing layout altered the unsaved draft or its place")
		}
	}
}

func TestNotesLayoutHidesEmptyLocationButShowsSavedPlace(t *testing.T) {
	m := reviewModel(t)
	reviewCommand(m, m.request(action{kind: "notebook", notebook: "work"}))
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "Location") || !strings.Contains(view, "RECENT ENTRIES") {
		t.Fatal("notes style should retain history without an empty location field")
	}
	contextLocation(t, m, "Office")
	reviewPaste(m, "Shipped the release")
	reviewCommand(m, reviewControl(m, 's'))
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(ansi.Strip(m.View().Content), "Office") {
		t.Fatal("notes style hid an entry's saved location")
	}
}

func TestLateLoadKeepsActiveLocationInput(t *testing.T) {
	m := reviewModel(t)
	if _, err := m.store.SaveWithLocation("personal", nil, "Earlier entry", "Seattle"); err != nil {
		t.Fatal(err)
	}
	load := m.load()
	m.runCommand(commandLocation)
	reviewPaste(m, "Portland")
	reviewCommand(m, load)
	if !m.editingLocation || !m.locationInput.Focused() || m.locationInput.Value() != "Portland" {
		t.Fatal("late history load replaced the active location input")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.location != "Portland" {
		t.Fatal("location typed during load was not applied")
	}
}

func TestLateLoadPopulatesRecentLocationsWithoutReopening(t *testing.T) {
	m := reviewModel(t)
	if _, err := m.store.SaveWithLocation("personal", nil, "Earlier entry", "Seattle"); err != nil {
		t.Fatal(err)
	}
	load := m.load()
	m.runCommand(commandLocation)
	reviewPaste(m, "Sea")
	reviewCommand(m, load)
	if !contains(m.locationChoices, "Seattle") || m.locationInput.Value() != "Sea" || !m.locationInput.Focused() {
		t.Fatal("late history load did not refresh recent places while preserving the typed query")
	}
}

func TestLateLoadCannotUndoNewStyle(t *testing.T) {
	m := reviewModel(t)
	staleResult := m.load()()
	contextStyle(t, m, journal.StyleNotes)
	m.Update(staleResult)
	if m.settings.Style != journal.StyleNotes {
		t.Fatal("old load response overwrote the newly saved style")
	}
}

func TestSaveFailureKeepsLocationChange(t *testing.T) {
	m := reviewModel(t)
	contextLocation(t, m, "Portland")
	reviewPaste(m, "Body")
	reviewCommand(m, reviewControl(m, 's'))
	contextLocation(t, m, "Seattle")
	if cmd := reviewControl(m, 's'); cmd == nil {
		t.Fatal("location-only edit did not schedule save")
	}
	m.Update(savedMsg{err: errors.New("disk full")})
	if !m.dirty() || m.location != "Seattle" || m.baselineLocation != "Portland" || m.editor.Value() != "Body" {
		t.Fatal("failed save lost the unsaved location or changed its baseline")
	}
}

func TestTypingDuringLocationSaveStaysInDraft(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Saved body")
	reviewCommand(m, reviewControl(m, 's'))
	contextLocation(t, m, "Portland")
	cmd := reviewControl(m, 's')
	if cmd == nil {
		t.Fatal("location change did not schedule save")
	}
	reviewPaste(m, " and newer text")
	m.Update(cmd())
	if m.location != "Portland" || m.baselineLocation != "Portland" || m.editor.Value() != "Saved body and newer text" || !m.dirty() {
		t.Fatal("saving a location change discarded later body input")
	}
}
