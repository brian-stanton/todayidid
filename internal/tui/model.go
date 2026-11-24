// Package tui is the terminal interface. File persistence lives in journal;
// commands perform I/O and return messages to this single-threaded model.
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/brian-stanton/todayidid/internal/journal"
)

type mode int

// Bubbles currently has a fixed 10,000-line editor limit. Refuse input that
// would exceed it rather than letting the component silently truncate a note.
const maxEditorLines = 10000

const (
	writing mode = iota
	browsing
	reading
)

type action struct {
	kind     string
	notebook string
	entry    *journal.Entry
}

type loadedMsg struct {
	generation  int
	entries     []journal.Entry
	notebooks   []string
	settings    journal.NotebookSettings
	settingsErr error
	err         error
}

type savedMsg struct {
	entry journal.Entry
	err   error
}

type clipboardMsg struct {
	draftID int
	content string
	err     error
}

type fieldClipboardMsg struct {
	generation int
	target     string
	content    string
	err        error
}

type Model struct {
	store                         *journal.Store
	notebook                      string
	notebooks                     []string
	entries                       []journal.Entry
	list                          list.Model
	editor                        textarea.Model
	search                        textinput.Model
	notebookInput                 textinput.Model
	preview                       viewport.Model
	editing                       *journal.Entry
	baseline                      string
	mode                          mode
	width, height                 int
	dark                          bool
	colors                        palette
	loading, saving               bool
	generation                    int
	draftID                       int
	inputGeneration               int
	searching                     bool
	searchReturn                  mode
	searchScroll                  int
	searchBefore, searchSelection string
	showHelp, showNotebooks       bool
	showCommands                  bool
	commandIndex                  int
	notebookIndex                 int
	pending                       *action
	notice                        string
	problem                       bool
	words                         int
	settings                      journal.NotebookSettings
	settingsDraft                 journal.NotebookStyle
	showSettings, savingSettings  bool
	location, baselineLocation    string
	locationInput                 textinput.Model
	editingLocation               bool
	locationChoices               []string
	locationChoice                int
	locationGeneration            int
	locationQuery                 string
	locationError                 string
	sessionLocations              map[string]string
	historical                    bool
}

// New opens directly into an empty, focused editor. Notebook directories and
// entry files are created on the first save.
func New(store *journal.Store, notebook string) *Model {
	editor := textarea.New()
	editor.Placeholder = ""
	editor.Prompt = ""
	editor.ShowLineNumbers = false
	editor.EndOfBufferCharacter = ' '
	editor.CharLimit = 0
	editor.MaxHeight = 0
	editor.MaxWidth = 0
	editor.SetVirtualCursor(false)
	editor.Focus()
	search := textinput.New()
	search.Prompt = "/ "
	search.Placeholder = "Search entries"
	search.CharLimit = 256
	search.SetVirtualCursor(false)
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "Find or name a notebook"
	input.CharLimit = 64
	input.SetVirtualCursor(false)
	place := textinput.New()
	place.Prompt = ""
	place.Placeholder = "Location"
	place.CharLimit = 0
	place.SetVirtualCursor(false)
	items := list.New(nil, entryDelegate{}, 24, 16)
	items.SetShowTitle(false)
	items.SetShowStatusBar(false)
	items.SetShowHelp(false)
	items.SetShowPagination(false)
	items.SetFilteringEnabled(false)
	items.DisableQuitKeybindings()
	preview := viewport.New()
	preview.SoftWrap = true
	m := &Model{
		store: store, notebook: notebook, notebooks: []string{"personal", "work"},
		editor: editor, search: search, notebookInput: input, list: items, preview: preview,
		width: 100, height: 30, dark: true,
		settings:      journal.NotebookSettings{Style: journal.DefaultStyle(notebook)},
		locationInput: place, sessionLocations: make(map[string]string), locationChoice: -1,
	}
	m.applyTheme()
	m.resize()
	return m
}

// NewWithLocation starts a draft at an explicitly supplied place. Places are
// never inferred from another machine or from an older entry.
func NewWithLocation(store *journal.Store, notebook, location string) *Model {
	m := New(store, notebook)
	place, err := journal.NormalizeLocation(location)
	if err != nil {
		m.notice, m.problem = err.Error(), true
		return m
	}
	m.location = place
	m.sessionLocations[notebook] = place
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.load(), tea.RequestBackgroundColor)
}

func (m *Model) load() tea.Cmd {
	m.loading = true
	m.generation++
	generation, notebook, store := m.generation, m.notebook, m.store
	return func() tea.Msg {
		entries, err := store.List(notebook)
		var notebooks []string
		if err == nil {
			notebooks, err = store.Notebooks()
		}
		settings, settingsErr := store.Settings(notebook)
		return loadedMsg{generation: generation, entries: entries, notebooks: notebooks, settings: settings, settingsErr: settingsErr, err: err}
	}
}

func (m *Model) dirty() bool {
	return m.editor.Value() != m.baseline || m.editing != nil && m.location != m.baselineLocation
}

func (m *Model) save() tea.Cmd {
	if m.saving {
		return nil
	}
	if !m.dirty() {
		if m.pending != nil {
			return m.performPending()
		}
		m.notice, m.problem = "No unsaved changes.", false
		return nil
	}
	if strings.TrimSpace(m.editor.Value()) == "" {
		m.notice, m.problem = "Entry is empty. Previous entry unchanged.", true
		return nil
	}
	m.saving = true
	m.notice, m.problem = "Saving…", false
	store, notebook, content, location := m.store, m.notebook, m.editor.Value(), m.location
	var original *journal.Entry
	if m.editing != nil {
		copy := *m.editing
		original = &copy
	}
	return func() tea.Msg {
		entry, err := store.SaveWithLocation(notebook, original, content, location)
		return savedMsg{entry, err}
	}
}

func (m *Model) request(a action) tea.Cmd {
	if a.kind == "edit" || a.kind == "edit-location" {
		if _, err := m.prepareEditor(a.entry.Content); err != nil {
			m.notice, m.problem = err.Error(), true
			return nil
		}
	}
	m.pending = &a
	if m.dirty() || m.saving {
		m.syncFocus()
		return nil
	}
	return m.performPending()
}

func (m *Model) performPending() tea.Cmd {
	if m.pending == nil {
		return nil
	}
	a := *m.pending
	m.pending = nil
	m.searching = false
	m.draftID++
	switch a.kind {
	case "quit":
		return tea.Quit
	case "new":
		m.editing, m.baseline = nil, ""
		m.editor.Reset()
		m.words = 0
		m.mode = writing
		m.notice, m.problem = "", false
		m.resetLocation()
	case "notebook":
		m.notebook = a.notebook
		m.editing, m.baseline = nil, ""
		m.editor.Reset()
		m.words = 0
		m.search.Reset()
		m.entries = nil
		m.refilter()
		m.mode = writing
		m.notice, m.problem = "", false
		m.settings = journal.NotebookSettings{Style: journal.DefaultStyle(m.notebook)}
		m.resetLocation()
		m.resize()
		m.syncFocus()
		return m.load()
	case "edit", "edit-location":
		// A save-and-continue may have just updated this same entry. Resolve
		// its latest in-memory version instead of reopening the stale snapshot.
		for i := range m.entries {
			if m.entries[i].ID == a.entry.ID && m.entries[i].Notebook == a.entry.Notebook {
				copy := m.entries[i]
				a.entry = &copy
				break
			}
		}
		editor, err := m.prepareEditor(a.entry.Content)
		if err != nil {
			m.notice, m.problem = err.Error(), true
			m.syncFocus()
			return nil
		}
		m.editing = a.entry
		m.baseline = a.entry.Content
		m.location, m.baselineLocation = a.entry.Location, a.entry.Location
		m.historical = true
		m.editor = editor
		m.editor.MoveToEnd()
		m.words = len(strings.Fields(a.entry.Content))
		m.mode = writing
		m.notice, m.problem = "Editing entry", false
	}
	m.resize()
	m.syncFocus()
	if a.kind == "edit-location" {
		m.beginLocation()
	}
	return nil
}

func (m *Model) prepareEditor(content string) (textarea.Model, error) {
	// Existing Markdown may contain tabs, CRLF, or more lines than Bubbles
	// can represent. Preserve both the file and the current draft in that case.
	editor := textarea.New()
	editor.Placeholder = ""
	editor.Prompt = ""
	editor.ShowLineNumbers = false
	editor.EndOfBufferCharacter = ' '
	editor.CharLimit, editor.MaxHeight, editor.MaxWidth = 0, 0, 0
	editor.SetVirtualCursor(false)
	editor.SetStyles(m.editor.Styles())
	editor.SetWidth(m.editor.Width())
	editor.SetHeight(m.editor.Height())
	editor.SetValue(content)
	if editor.Value() != content {
		return textarea.Model{}, fmt.Errorf("this note needs an external editor (tabs, control characters, or over 10,000 lines); file unchanged")
	}
	return editor, nil
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case settingsSavedMsg:
		return m, m.settingsSaved(msg)
	case locationClipboardMsg:
		if msg.draftID != m.draftID || msg.generation != m.locationGeneration || !m.editingLocation {
			return m, nil
		}
		if msg.err != nil {
			m.locationError = msg.err.Error()
			return m, nil
		}
		return m, m.updateLocation(tea.PasteMsg{Content: msg.content})
	case fieldClipboardMsg:
		focused := msg.target == "search" && m.search.Focused() || msg.target == "notebook" && m.notebookInput.Focused()
		if msg.generation != m.inputGeneration || !focused {
			return m, nil
		}
		if msg.err != nil {
			m.notice, m.problem = "Could not read clipboard: "+msg.err.Error(), true
			return m, nil
		}
		return m.Update(tea.PasteMsg{Content: msg.content})
	case clipboardMsg:
		if msg.draftID != m.draftID || m.mode != writing || m.pending != nil || m.showCommands || m.showHelp || m.showNotebooks || m.showSettings || m.editingLocation {
			return m, nil
		}
		if msg.err != nil {
			m.notice, m.problem = "Could not read clipboard: "+msg.err.Error(), true
			return m, nil
		}
		return m, m.updateEditor(tea.PasteMsg{Content: msg.content})
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.applyTheme()
		return m, nil
	case loadedMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.notice, m.problem = "Could not load notes: "+msg.err.Error(), true
			return m, nil
		}
		m.entries, m.notebooks = msg.entries, msg.notebooks
		if msg.settingsErr != nil {
			m.notice, m.problem = "Notebook settings: "+msg.settingsErr.Error(), true
		} else {
			m.settings = msg.settings
		}
		if !contains(m.notebooks, m.notebook) {
			m.notebooks = append(m.notebooks, m.notebook)
			sort.Strings(m.notebooks)
		}
		m.refilter()
		if m.editingLocation {
			m.refreshLocations()
			m.locationChoice = -1
		}
		m.resize()
		return m, nil
	case savedMsg:
		m.saving = false
		if msg.err != nil && msg.entry.ID == "" {
			m.notice, m.problem = "Save failed: "+msg.err.Error(), true
			// Keep both the draft and its requested action for retry/cancel.
			return m, nil
		}
		m.editing = &msg.entry
		m.baseline = msg.entry.Content
		m.baselineLocation = msg.entry.Location
		m.notice, m.problem = "Saved to "+m.notebook+" · "+msg.entry.CreatedAt.Local().Format("3:04 PM"), false
		if m.dirty() {
			m.notice = "Saved. Newer changes are still unsaved."
		}
		if msg.err != nil {
			m.notice, m.problem = msg.err.Error(), true
		}
		// Invalidate a refresh already in flight so it cannot hide this save.
		wasLoading := m.loading
		m.generation++
		m.loading = false
		replaced := false
		for i := range m.entries {
			if m.entries[i].ID == msg.entry.ID {
				m.entries[i] = msg.entry
				replaced = true
				break
			}
		}
		if !replaced {
			m.entries = append(m.entries, msg.entry)
		}
		sort.Slice(m.entries, func(i, j int) bool { return m.entries[i].CreatedAt.After(m.entries[j].CreatedAt) })
		m.refilter()
		if m.pending != nil && !m.dirty() && msg.err == nil {
			cmd := m.performPending()
			if wasLoading && !m.loading {
				return m, tea.Batch(cmd, m.load())
			}
			return m, cmd
		}
		m.syncFocus()
		if wasLoading {
			return m, m.load()
		}
		return m, nil
	}
	if m.savingSettings {
		return m, nil
	}
	if click, ok := message.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft && m.mode == writing &&
		m.pending == nil && !m.showCommands && !m.showHelp && !m.showNotebooks && !m.showSettings && !m.editingLocation &&
		(m.settings.Style == journal.StyleJournal || m.location != "") && m.width >= 42 && m.height >= 16 {
		d := m.dimensions()
		left := d.left + 3
		if d.wide {
			left += d.sidebar + 2
		}
		if click.Y == 5 && click.X >= left && click.X < left+m.locationFieldWidth() {
			m.beginLocation()
			return m, nil
		}
	}
	if msg, ok := message.(tea.KeyPressMsg); ok {
		key := msg.String()
		if m.pending != nil {
			// The in-flight snapshot must finish before changing notebooks or
			// abandoning its editor. Escape can still cancel the pending action.
			if m.saving && key != "esc" {
				return m, nil
			}
			switch key {
			case "ctrl+s":
				return m, m.save()
			case "d":
				return m, m.performPending()
			case "esc", "enter":
				m.pending = nil
				m.syncFocus()
			}
			return m, nil
		}
		if m.showCommands {
			return m, m.updateCommands(message)
		}
		if m.showHelp {
			switch key {
			case "esc", "enter", "ctrl+g", "f1":
				m.showHelp = false
				m.syncFocus()
			case "ctrl+c":
				m.showHelp = false
				return m, m.request(action{kind: "quit"})
			}
			return m, nil
		}
		if m.showNotebooks {
			return m, m.updateNotebooks(message)
		}
		if m.showSettings {
			return m, m.updateSettings(message)
		}
		if m.editingLocation {
			return m, m.updateLocation(message)
		}
		switch key {
		case "ctrl+c":
			return m, m.request(action{kind: "quit"})
		case "ctrl+s":
			return m, m.save()
		case "ctrl+g", "f1":
			m.showCommands = true
			m.commandIndex = 0
			m.syncFocus()
			return m, nil
		}
		if m.searching {
			switch key {
			case "ctrl+v":
				return m, m.pasteField("search")
			case "esc":
				m.cancelSearch()
				return m, nil
			case "enter":
				if m.selected() == nil {
					return m, nil
				}
				m.mode = reading
				m.searching = false
				m.resize()
				m.syncFocus()
				return m, nil
			case "tab", "shift+tab":
				m.searching = false
				m.syncFocus()
				return m, nil
			case "up", "down":
				m.list, _ = m.list.Update(msg)
				m.updatePreview()
				return m, nil
			}
		} else {
			switch key {
			case "tab", "shift+tab":
				if m.mode == writing {
					m.mode = browsing
				} else {
					m.mode = writing
				}
				m.resize()
				m.syncFocus()
				return m, nil
			case "esc":
				if m.mode == reading {
					m.mode = browsing
				} else if m.mode == browsing {
					m.mode = writing
				}
				m.resize()
				m.syncFocus()
				return m, nil
			}
			if m.mode != writing {
				switch key {
				case "q":
					return m, m.request(action{kind: "quit"})
				case "n":
					return m, m.request(action{kind: "new"})
				case "/":
					m.beginSearch()
					return m, nil
				case "l":
					return m, m.runCommand(commandLocation)
				case "?":
					return m, m.runCommand(commandHelp)
				case "e":
					if entry := m.selected(); entry != nil {
						return m, m.request(action{kind: "edit", entry: entry})
					}
					return m, nil
				case "r":
					return m, m.load()
				case "enter":
					if m.selected() != nil {
						m.mode = reading
						m.resize()
						m.syncFocus()
					}
					return m, nil
				}
			}
		}
	}
	var cmd tea.Cmd
	switch {
	case m.showCommands:
		return m, m.updateCommands(message)
	case m.showSettings:
		return m, m.updateSettings(message)
	case m.editingLocation:
		return m, m.updateLocation(message)
	case m.showNotebooks:
		return m, m.updateNotebooks(message)
	case m.pending != nil || m.showHelp:
		return m, nil
	case m.searching:
		previous := m.search.Value()
		m.search, cmd = m.search.Update(message)
		if previous != m.search.Value() {
			m.refilter()
		}
	case m.mode == writing:
		return m, m.updateEditor(message)
	case m.mode == browsing:
		index := m.list.Index()
		m.list, cmd = m.list.Update(message)
		if index != m.list.Index() {
			m.updatePreview()
		}
	case m.mode == reading:
		m.preview, cmd = m.preview.Update(message)
	}
	return m, cmd
}

func (m *Model) updateEditor(message tea.Msg) tea.Cmd {
	if paste, ok := message.(tea.PasteMsg); ok {
		// Normalize clipboard line endings before counting. Bubbles otherwise
		// treats CR and LF separately, doubling lines in Windows clipboard text.
		paste.Content = strings.ReplaceAll(paste.Content, "\r\n", "\n")
		paste.Content = strings.ReplaceAll(paste.Content, "\r", "\n")
		paste.Content = strings.ReplaceAll(paste.Content, "\t", "    ")
		if safeText(paste.Content) != paste.Content || strings.ContainsRune(paste.Content, utf8.RuneError) {
			m.notice, m.problem = "Paste contains unsupported control characters. Use import to preserve the original text.", true
			return nil
		}
		if len(m.editor.Value())+len(paste.Content) > journal.MaxEntryBytes || m.editor.LineCount()+strings.Count(paste.Content, "\n") > maxEditorLines {
			m.notice, m.problem = "Paste too large for the editor (1 MiB / 10,000 lines). Use import for longer files.", true
			return nil
		}
		message = paste
	}
	if key, ok := message.(tea.KeyPressMsg); ok {
		if key.String() == "ctrl+v" {
			draftID := m.draftID
			return func() tea.Msg { content, err := clipboard.ReadAll(); return clipboardMsg{draftID, content, err} }
		}
		if (key.String() == "enter" || key.String() == "ctrl+m") && m.editor.LineCount() >= maxEditorLines {
			m.notice, m.problem = "Editor limit reached (10,000 lines). Save and start a new entry.", true
			return nil
		}
	}
	previous := m.editor.Value()
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(message)
	if previous != m.editor.Value() {
		m.words = len(strings.Fields(m.editor.Value()))
		if !m.problem {
			m.notice = ""
		}
	}
	return cmd
}

func (m *Model) updateNotebooks(message tea.Msg) tea.Cmd {
	choices := m.notebookChoices()
	m.notebookIndex = min(max(0, m.notebookIndex), max(0, len(choices)-1))
	if msg, ok := message.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "ctrl+v":
			return m.pasteField("notebook")
		case "esc":
			m.showNotebooks = false
			m.syncFocus()
			return nil
		case "ctrl+c":
			m.showNotebooks = false
			return m.request(action{kind: "quit"})
		case "up", "shift+tab":
			if len(choices) > 0 {
				m.notebookIndex = (m.notebookIndex + len(choices) - 1) % len(choices)
			}
			return nil
		case "down", "tab":
			if len(choices) > 0 {
				m.notebookIndex = (m.notebookIndex + 1) % len(choices)
			}
			return nil
		case "enter":
			name := strings.TrimSpace(m.notebookInput.Value())
			if len(choices) > 0 {
				name = choices[m.notebookIndex].name
			}
			if err := journal.ValidateNotebook(name); err != nil {
				m.notice, m.problem = err.Error(), true
				return nil
			}
			m.showNotebooks = false
			if name == m.notebook {
				m.syncFocus()
				return nil
			}
			return m.request(action{kind: "notebook", notebook: name})
		}
	}
	var cmd tea.Cmd
	previous := m.notebookInput.Value()
	m.notebookInput, cmd = m.notebookInput.Update(message)
	if previous != m.notebookInput.Value() {
		m.notebookIndex = 0
	}
	return cmd
}

func (m *Model) syncFocus() {
	m.editor.Blur()
	m.search.Blur()
	m.notebookInput.Blur()
	m.locationInput.Blur()
	if m.pending != nil || m.showCommands || m.showHelp || m.showSettings {
		return
	}
	if m.showNotebooks {
		m.notebookInput.Focus()
		return
	}
	if m.editingLocation {
		m.locationInput.Focus()
		return
	}
	if m.searching {
		m.search.Focus()
		return
	}
	if m.mode == writing {
		m.editor.Focus()
	}
}

func (m *Model) selected() *journal.Entry {
	if item, ok := m.list.SelectedItem().(entryItem); ok {
		copy := item.entry
		return &copy
	}
	return nil
}

func (m *Model) refilter() {
	id := ""
	if entry := m.selected(); entry != nil {
		id = entry.ID
	}
	terms := strings.Fields(strings.ToLower(m.search.Value()))
	items := make([]list.Item, 0, len(m.entries))
	selected := 0
	for _, entry := range m.entries {
		searchable := strings.ToLower(entry.Content + " " + entry.Location + " " + entry.CreatedAt.Local().Format("2006-01-02 Monday January"))
		match := true
		for _, term := range terms {
			if !strings.Contains(searchable, term) {
				match = false
				break
			}
		}
		if match {
			if entry.ID == id {
				selected = len(items)
			}
			items = append(items, entryItem{entry})
		}
	}
	m.list.SetItems(items)
	m.list.Select(selected)
	m.updatePreview()
}

func (m *Model) updatePreview() {
	if entry := m.selected(); entry != nil {
		m.preview.SetContent(safeText(entry.Content))
	} else {
		m.preview.SetContent("")
	}
	m.preview.GotoTop()
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func dateLabel(t time.Time) string {
	t = t.Local()
	if t.Format("2006-01-02") == time.Now().Format("2006-01-02") {
		return "Today · " + t.Format("3:04 PM")
	}
	return t.Format("Jan 02, 2006 · 3:04 PM")
}

func entryCount(count int) string {
	if count == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", count)
}
