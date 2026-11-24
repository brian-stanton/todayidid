package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/brian-stanton/todayidid/internal/journal"
)

func reviewModel(t *testing.T) *Model {
	t.Helper()
	store, err := journal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := New(store, "personal")
	reviewCommand(m, m.load())
	return m
}

func reviewCommand(m *Model, cmd tea.Cmd) {
	if cmd != nil {
		m.Update(cmd())
	}
}

func reviewControl(m *Model, code rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl})
	return cmd
}

func reviewRune(m *Model, code rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code, Text: string(code)})
	return cmd
}

func reviewPaste(m *Model, text string) {
	m.Update(tea.PasteMsg{Content: text})
}

func TestNewEntryFromSearchFocusesEditor(t *testing.T) {
	m := reviewModel(t)
	m.runCommand(commandSearch)
	reviewPaste(m, "old query")
	m.runCommand(commandNew)
	reviewPaste(m, "A fresh thought")
	if m.searching || m.mode != writing || !m.editor.Focused() {
		t.Fatalf("new entry left focus in search: mode=%v searching=%v editor focus=%v", m.mode, m.searching, m.editor.Focused())
	}
	if m.editor.Value() != "A fresh thought" {
		t.Fatalf("typing went somewhere other than the editor: %q", m.editor.Value())
	}
}

func TestNotebookSwitchFromSearchFocusesEditor(t *testing.T) {
	m := reviewModel(t)
	m.runCommand(commandSearch)
	m.runCommand(commandNotebooks)
	reviewPaste(m, "work")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	reviewCommand(m, cmd)
	reviewPaste(m, "Shipped it")
	if m.notebook != "work" || m.searching || !m.editor.Focused() || m.editor.Value() != "Shipped it" {
		t.Fatalf("notebook switch retained hidden search focus: notebook=%q searching=%v text=%q", m.notebook, m.searching, m.editor.Value())
	}
}

func TestRepeatedSaveUpdatesOneEntry(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "First thought")
	reviewCommand(m, reviewControl(m, 's'))
	if m.dirty() || m.editing == nil {
		t.Fatal("successful save did not establish a clean editing baseline")
	}
	id := m.editing.ID
	if cmd := reviewControl(m, 's'); cmd != nil {
		t.Fatal("saving unchanged text scheduled another write")
	}
	reviewPaste(m, ", and a second thought")
	reviewCommand(m, reviewControl(m, 's'))
	entries, err := m.store.List("personal")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != id || entries[0].Content != "First thought, and a second thought" {
		t.Fatalf("repeat save duplicated or lost an entry: %+v", entries)
	}
}

func TestTypingDuringSaveStaysInDraft(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Saved snapshot")
	cmd := reviewControl(m, 's')
	if cmd == nil {
		t.Fatal("expected save command")
	}
	// Filesystem work can take arbitrarily long. Keystrokes arriving while
	// the command runs still belong to the live editor.
	reviewPaste(m, " plus newer typing")
	m.Update(cmd())
	if m.editor.Value() != "Saved snapshot plus newer typing" || m.baseline != "Saved snapshot" || !m.dirty() {
		t.Fatalf("save discarded newer input: text=%q baseline=%q dirty=%v", m.editor.Value(), m.baseline, m.dirty())
	}
	reviewCommand(m, reviewControl(m, 's'))
	entries, err := m.store.List("personal")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Content != "Saved snapshot plus newer typing" {
		t.Fatalf("second save failed to persist newer input: %+v", entries)
	}
}

func TestQuitDuringSaveProtectsNewerTyping(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Saved snapshot")
	cmd := reviewControl(m, 's')
	reviewPaste(m, " and a newer thought")
	reviewControl(m, 'c')
	reviewCommand(m, cmd)
	if m.pending == nil || m.pending.kind != "quit" || !m.dirty() {
		t.Fatal("quit after a save discarded text typed during the write")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !m.editor.Focused() || m.editor.Value() != "Saved snapshot and a newer thought" {
		t.Fatal("canceling quit did not restore the complete draft")
	}
}

func TestClipboardResultCannotLeakIntoAnotherNotebook(t *testing.T) {
	m := reviewModel(t)
	draftID := m.draftID
	reviewCommand(m, m.request(action{kind: "notebook", notebook: "work"}))
	m.Update(clipboardMsg{draftID: draftID, content: "private personal text"})
	if m.editor.Value() != "" {
		t.Fatal("stale clipboard response entered another notebook")
	}
	m.Update(clipboardMsg{draftID: m.draftID, content: strings.Repeat("x\n", 10001)})
	if m.editor.Value() != "" || !m.problem {
		t.Fatal("clipboard paste bypassed editor limits")
	}
}

func TestPublishedSaveWarningDoesNotCreateDuplicateOnRetry(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "A thought worth keeping")
	m.request(action{kind: "quit"})
	m.save()
	entry, err := m.store.Save("personal", nil, m.editor.Value())
	if err != nil {
		t.Fatal(err)
	}
	m.Update(savedMsg{entry: entry, err: errors.New("entry was written, but directory sync failed")})
	if m.editing == nil || m.editing.ID != entry.ID || m.dirty() || !m.problem || m.pending == nil {
		t.Fatal("published warning lost the saved baseline or dismissed the warning")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd := reviewControl(m, 's'); cmd != nil {
		t.Fatal("retry would create a duplicate entry")
	}
}

func TestSaveFailureKeepsDraftAndPendingAction(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Please keep this")
	reviewControl(m, 'c')
	if m.pending == nil || m.pending.kind != "quit" {
		t.Fatal("quitting discarded the draft without asking")
	}
	if cmd := reviewControl(m, 's'); cmd == nil {
		t.Fatal("save confirmation did not schedule a save")
	}
	m.Update(savedMsg{err: errors.New("disk full")})
	if m.saving || !m.problem || !m.dirty() || m.editor.Value() != "Please keep this" || m.pending == nil {
		t.Fatalf("failed save lost draft or pending action: %+v", m)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.pending != nil || !m.editor.Focused() || m.editor.Value() != "Please keep this" {
		t.Fatal("cancel after failed save did not return to the intact draft")
	}
}

func TestEditingSameEntryAfterSaveUsesLatestContent(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Original")
	reviewCommand(m, reviewControl(m, 's'))
	reviewPaste(m, " plus changes")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	reviewRune(m, 'e')
	if m.pending == nil {
		t.Fatal("expected unsaved changes confirmation")
	}
	reviewCommand(m, reviewControl(m, 's'))
	if m.editor.Value() != "Original plus changes" || m.baseline != "Original plus changes" || m.dirty() {
		t.Fatalf("pending edit reopened a stale snapshot: text=%q baseline=%q dirty=%v", m.editor.Value(), m.baseline, m.dirty())
	}
}

func TestStaleLoadCannotReplaceNewNotebook(t *testing.T) {
	m := reviewModel(t)
	oldCommand := m.load()
	oldResult := oldCommand()
	reviewCommand(m, m.request(action{kind: "notebook", notebook: "work"}))
	reviewPaste(m, "Work entry")
	reviewCommand(m, reviewControl(m, 's'))
	m.Update(oldResult)
	if m.notebook != "work" || len(m.entries) != 1 || m.entries[0].Content != "Work entry" {
		t.Fatalf("stale load replaced current notebook: notebook=%q entries=%+v", m.notebook, m.entries)
	}
}

func TestSavingDuringInitialLoadStillLoadsExistingHistory(t *testing.T) {
	m := reviewModel(t)
	if _, err := m.store.Save("personal", nil, "Existing history"); err != nil {
		t.Fatal(err)
	}
	// Capture the startup result, then let a quick save complete before the
	// result is delivered. The save must not permanently hide older entries.
	startupResult := m.load()()
	reviewPaste(m, "Quick new thought")
	cmd := reviewControl(m, 's')
	if cmd == nil {
		t.Fatal("expected save command")
	}
	_, followup := m.Update(cmd())
	m.Update(startupResult)
	if followup != nil {
		reviewCommand(m, followup)
	}
	if len(m.entries) != 2 {
		t.Fatalf("save during startup hid history: %+v", m.entries)
	}
}

func TestSearchMatchesBodyAndRetainsDraft(t *testing.T) {
	m := reviewModel(t)
	if _, err := m.store.Save("personal", nil, "# Monday\n\nFinished the migration"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.Save("personal", nil, "# Tuesday\n\nWent hiking"); err != nil {
		t.Fatal(err)
	}
	reviewCommand(m, m.load())
	reviewPaste(m, "Unfinished draft")
	m.runCommand(commandSearch)
	reviewPaste(m, "MIGRATION")
	if len(m.list.Items()) != 1 || !strings.Contains(m.selected().Content, "migration") {
		t.Fatal("search did not match entry body case-insensitively")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.editor.Value() != "Unfinished draft" || !m.dirty() || !m.editor.Focused() || m.mode != writing || m.search.Value() != "" {
		t.Fatal("browsing/searching lost the current draft")
	}
}

func TestOversizedLineCountCannotBeSilentlyEdited(t *testing.T) {
	m := reviewModel(t)
	content := strings.Repeat("A line\n", 10001) + "The irreplaceable last line"
	entry, err := m.store.Save("personal", nil, content)
	if err != nil {
		t.Fatal(err)
	}
	m.request(action{kind: "edit", entry: &entry})
	// Bubbles caps textarea content at 10,000 lines. The app must either
	// preserve every byte or refuse editing instead of offering to save a
	// truncated version over the source file.
	if m.editor.Value() != content && (m.editing != nil || !m.problem) {
		t.Fatal("long entry was silently truncated into an editable draft")
	}
}

func TestUnrepresentableEntryRefusedBeforeReplacingDraft(t *testing.T) {
	for name, content := range map[string]string{
		"tabs":             "A\tB",
		"CRLF":             "First\r\nSecond\r\n",
		"replacement rune": "Literal \uFFFD text",
	} {
		t.Run(name, func(t *testing.T) {
			m := reviewModel(t)
			entry, err := m.store.Save("personal", nil, content)
			if err != nil {
				t.Fatal(err)
			}
			reviewPaste(m, "My unsaved draft")
			m.request(action{kind: "edit", entry: &entry})
			if m.editor.Value() != "My unsaved draft" || m.editing != nil || !m.problem || m.pending != nil {
				t.Fatalf("unsupported edit did not preserve draft and report error: text=%q problem=%v pending=%+v", m.editor.Value(), m.problem, m.pending)
			}
		})
	}
}

func TestOversizedPastePreservesDraft(t *testing.T) {
	for name, content := range map[string]string{
		"line count": strings.Repeat("A\n", 10001),
		"bytes":      strings.Repeat("A", journal.MaxEntryBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			m := reviewModel(t)
			reviewPaste(m, "My unsaved draft")
			reviewPaste(m, content)
			if m.editor.Value() != "My unsaved draft" || !m.problem {
				t.Fatal("oversized paste silently changed or truncated the draft")
			}
		})
	}
}

func TestCRLFPasteDoesNotLoseItsTail(t *testing.T) {
	m := reviewModel(t)
	reviewPaste(m, "Draft prefix\n")
	content := strings.Repeat("Windows clipboard line\r\n", 6000) + "Irreplaceable tail"
	reviewPaste(m, content)
	// Normalizing CRLF is fine for newly pasted text. If it cannot be
	// represented, rejecting the paste is also safe. Truncation is neither.
	if !strings.HasSuffix(m.editor.Value(), "Irreplaceable tail") && (m.editor.Value() != "Draft prefix\n" || !m.problem) {
		t.Fatal("CRLF paste was silently truncated by the textarea line cap")
	}
}

func TestViewFitsTerminal(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {42, 12}, {42, 16}, {80, 24}, {140, 50}, {39, 8}} {
		for _, activeMode := range []mode{writing, browsing, reading} {
			t.Run(fmt.Sprintf("%dx%d/mode%d", size[0], size[1], activeMode), func(t *testing.T) {
				m := reviewModel(t)
				m.mode = activeMode
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.syncFocus()
				view := m.View()
				if lipgloss.Width(view.Content) > size[0] || lipgloss.Height(view.Content) > size[1] {
					t.Fatalf("view exceeds terminal: got %dx%d, terminal %dx%d", lipgloss.Width(view.Content), lipgloss.Height(view.Content), size[0], size[1])
				}
				if view.Cursor != nil && (view.Cursor.X < 0 || view.Cursor.X >= size[0] || view.Cursor.Y < 0 || view.Cursor.Y >= size[1]) {
					t.Fatalf("native cursor lies outside terminal: %+v", view.Cursor)
				}
			})
		}
	}
}
