package tui

import (
	"fmt"
	"image/color"
	"io"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/brian-stanton/todayidid/internal/journal"
	"github.com/charmbracelet/x/ansi"
)

type palette struct{ ink, muted, accent, green, border, selected, warning color.Color }

func (m *Model) applyTheme() {
	choose := lipgloss.LightDark(m.dark)
	m.colors = palette{
		ink:      choose(lipgloss.Color("#30323A"), lipgloss.Color("#E8E5DF")),
		muted:    choose(lipgloss.Color("#727680"), lipgloss.Color("#92959F")),
		accent:   choose(lipgloss.Color("#7151AF"), lipgloss.Color("#C6AFF4")),
		green:    choose(lipgloss.Color("#38715A"), lipgloss.Color("#A8CFB1")),
		border:   choose(lipgloss.Color("#D2D0D8"), lipgloss.Color("#40414D")),
		selected: choose(lipgloss.Color("#EDE8F5"), lipgloss.Color("#302B3D")),
		warning:  choose(lipgloss.Color("#A24525"), lipgloss.Color("#F0AC87")),
	}
	s := textarea.DefaultStyles(m.dark)
	s.Focused.Text = lipgloss.NewStyle().Foreground(m.colors.ink)
	s.Focused.CursorLine = lipgloss.NewStyle()
	s.Focused.Placeholder = lipgloss.NewStyle().Foreground(m.colors.muted)
	s.Blurred = s.Focused
	s.Cursor.Color = m.colors.accent
	s.Cursor.Shape = tea.CursorBar
	s.Cursor.Blink = true
	m.editor.SetStyles(s)
	m.search.SetStyles(textinput.DefaultStyles(m.dark))
	m.notebookInput.SetStyles(textinput.DefaultStyles(m.dark))
	m.styleLocationInput()
	m.list.SetDelegate(entryDelegate{m.colors})
	m.preview.Style = lipgloss.NewStyle().Foreground(m.colors.ink)
}

type dimensions struct {
	width, bodyHeight, sidebar, pane, left int
	wide                                   bool
}

func (m *Model) dimensions() dimensions {
	w := max(1, min(124, m.width-4))
	focused := m.settings.Style == journal.StyleJournal && m.mode != browsing
	if focused {
		w = min(w, 96)
	}
	d := dimensions{width: w, bodyHeight: max(6, m.height-6), pane: w, left: max(0, (m.width-w)/2), wide: w >= 86 && !focused}
	if d.wide {
		d.sidebar = 30
		d.pane = w - d.sidebar - 2
	}
	return d
}

func (m *Model) resize() {
	d := m.dimensions()
	listWidth := d.width
	if d.wide {
		listWidth = d.sidebar
	}
	m.list.SetSize(max(1, listWidth-4), max(1, d.bodyHeight-8))
	m.editor.SetWidth(max(1, d.pane-6))
	m.editor.SetHeight(max(1, d.bodyHeight-6))
	m.preview.SetWidth(max(1, d.pane-6))
	m.preview.SetHeight(max(1, d.bodyHeight-6))
	m.search.SetWidth(max(1, listWidth-6))
	m.notebookInput.SetWidth(max(1, min(48, d.width-12)))
	m.locationInput.SetWidth(max(1, m.locationFieldWidth()-1))
}

func (m *Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.WindowTitle = "todayidid · " + m.notebook
	v.MouseMode = tea.MouseModeCellMotion
	if m.width < 42 || m.height < 16 {
		content := "todayidid\n\nTerminal too small.\nMinimum: 42 columns × 16 rows\n\nCtrl+s save · Ctrl+c quit"
		if m.pending != nil {
			content = "Unsaved entry\n\nCtrl+s save & continue\nd discard · Esc cancel"
		}
		if m.problem {
			content += "\n" + safeText(m.notice)
		}
		v.SetContent(lipgloss.NewStyle().MaxWidth(max(1, m.width)).MaxHeight(max(1, m.height)).Render(content))
		return v
	}
	d := m.dimensions()
	if m.showCommands {
		v.SetContent(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.commandsView(d.width)))
		return v
	}
	if m.pending != nil || m.showHelp || m.showNotebooks || m.showSettings {
		content := m.dialog(d.width)
		v.SetContent(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content))
		return v
	}
	brand := lipgloss.NewStyle().Bold(true).Foreground(m.colors.ink).Render("today") + lipgloss.NewStyle().Bold(true).Foreground(m.colors.accent).Render("idid")
	badge := lipgloss.NewStyle().Foreground(m.colors.green).Render("  / " + m.notebook)
	header := spread(brand+badge, m.muted(time.Now().Format("Mon, Jan 02")), d.width)
	var body string
	if d.wide {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.historyPanel(d.sidebar, d.bodyHeight), "  ", m.notePanel(d.pane, d.bodyHeight))
	} else if m.mode == browsing {
		body = m.historyPanel(d.width, d.bodyHeight)
	} else {
		body = m.notePanel(d.width, d.bodyHeight)
	}
	status := m.status(d.width)
	hints := m.footerHints(d.width)
	content := header + "\n\n" + body + "\n" + status + "\n" + m.muted(ansi.Truncate(hints, d.width, ""))
	v.SetContent(lipgloss.NewStyle().Padding(1, d.left, 1, d.left).Render(content))
	if m.editingLocation {
		if cursor := m.locationInput.Cursor(); cursor != nil {
			cursor.X += d.left + 3
			if d.wide {
				cursor.X += d.sidebar + 2
			}
			cursor.Y += 5
			v.Cursor = cursor
		}
	} else if m.mode == writing {
		if cursor := m.editor.Cursor(); cursor != nil {
			cursor.X += d.left + 3
			if d.wide {
				cursor.X += d.sidebar + 2
			}
			cursor.Y += 7
			v.Cursor = cursor
		}
	} else if m.searching {
		if cursor := m.search.Cursor(); cursor != nil {
			cursor.X += d.left + 2
			cursor.Y += 7
			v.Cursor = cursor
		}
	}
	return v
}

func (m *Model) panel(width, height int, active bool, content string, horizontalPadding int) string {
	border := m.colors.border
	if active {
		border = m.colors.accent
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(1, horizontalPadding).Width(width).Height(height).
		MaxWidth(width).MaxHeight(height).Render(content)
}

func (m *Model) historyPanel(width, height int) string {
	title := "RECENT ENTRIES"
	if m.search.Value() != "" {
		title = "SEARCH RESULTS"
	}
	count := entryCount(len(m.list.Items()))
	if m.loading {
		count = "Loading…"
	}
	top := lipgloss.NewStyle().Bold(true).Foreground(m.colors.accent).Render(title) + "\n" + m.muted(count) + "\n" + m.search.View() + "\n\n"
	content := m.list.View()
	if len(m.list.Items()) == 0 {
		if m.search.Value() != "" {
			content = m.muted("No matching entries.")
		} else {
			content = ""
		}
	}
	return m.panel(width, height, m.mode == browsing, top+content, 1)
}

func (m *Model) notePanel(width, height int) string {
	label := "TODAY, I DID…"
	meta := "New entry"
	place := m.location
	content := m.editor.View()
	if m.mode == writing {
		if m.editing != nil {
			meta = m.noteDate(m.editing.CreatedAt)
		} else if m.settings.Style == journal.StyleJournal {
			meta = m.noteDate(time.Now())
		}
	} else {
		label = "ENTRY"
		meta = "No entry selected"
		place = ""
		content = m.preview.View()
		if entry := m.selected(); entry != nil {
			meta = m.noteDate(entry.CreatedAt)
			place = entry.Location
		} else {
			content = ""
		}
	}
	left := m.heading(label)
	if place != "" {
		left = m.heading(safeText(place))
	} else if m.mode == writing && m.settings.Style == journal.StyleJournal {
		left = m.muted("Location")
	}
	if m.editingLocation {
		left = m.locationInput.View()
	}
	inner := max(1, width-6)
	// Give location the left edge without allowing long names to erase dates.
	leftWidth := max(1, inner-lipgloss.Width(meta)-2)
	if m.editingLocation {
		left = ansi.Truncate(left, leftWidth, "")
	} else {
		left = ansi.Truncate(left, leftWidth, "…")
	}
	top := spread(left, m.muted(meta), inner)
	return m.panel(width, height, m.mode == writing || m.mode == reading, top+"\n\n"+content, 2)
}

func (m *Model) status(width int) string {
	if m.editingLocation {
		if m.locationError != "" {
			return lipgloss.NewStyle().Foreground(m.colors.warning).Render(ansi.Truncate(m.locationError, width, "…"))
		}
		if len(m.locationChoices) > 0 {
			return m.muted(ansi.Truncate("Recent: "+strings.Join(m.locationChoices[:min(3, len(m.locationChoices))], " · "), width, "…"))
		}
		return ""
	}
	state := "Ready"
	if m.editing != nil {
		state = "Saved"
	}
	if m.dirty() {
		state = "● Unsaved"
	}
	if m.saving {
		state = "Saving…"
	}
	left := m.notice
	if left == "" {
		left = state
	}
	style := lipgloss.NewStyle().Foreground(m.colors.green)
	if m.dirty() {
		style = style.Foreground(m.colors.accent)
	}
	if m.problem {
		style = style.Foreground(m.colors.warning)
	}
	right := fmt.Sprintf("%d words", m.words)
	if m.mode != writing {
		right = entryCount(len(m.entries))
		if m.dirty() {
			right += " · unsaved draft"
		}
	}
	return spread(style.Render(safeText(left)), m.muted(right), width)
}

func (m *Model) dialog(width int) string {
	width = min(width, 68)
	var content string
	if m.pending != nil {
		content = m.heading("Unsaved changes") + "\n\nNotebook: " + m.notebook + "\n\n" +
			"ctrl+s   Save and continue\n" + "d        Discard changes and continue\n" + "esc      Cancel"
		if m.saving {
			content = m.heading("Saving…") + "\n\n" + m.muted("Esc cancel pending action")
		}
		if m.problem {
			content += "\n\n" + lipgloss.NewStyle().Foreground(m.colors.warning).Render(safeText(m.notice))
		}
	} else if m.showHelp {
		content = m.keyboardHelp(width - 8)
	} else if m.showSettings {
		journalLabel, notesLabel := "Journal", "Notes"
		if m.settingsDraft == journal.StyleJournal {
			journalLabel = m.heading("› Journal")
		} else {
			notesLabel = m.heading("› Notes")
		}
		content = m.heading("Notebook style") + "\n" + m.muted(ansi.Truncate(m.notebook, width-8, "…")) + "\n\n" +
			journalLabel + "\n" + m.muted("Location · focused writing") + "\n\n" +
			notesLabel + "\n" + m.muted("History beside the editor") + "\n\n" +
			m.muted("↑↓ choose · Enter apply · Esc cancel")
		if m.savingSettings {
			content += "\nSaving…"
		} else if m.problem {
			content += "\n" + m.muted(safeText(m.notice))
		}
	} else {
		content = m.heading("Notebooks") + "\n\n"
		choices := m.notebookChoices()
		// Keep long notebook lists within the terminal; selection scrolls.
		visible := max(1, min(8, m.height-14))
		start := max(0, m.notebookIndex-visible+1)
		for i := start; i < min(len(choices), start+visible); i++ {
			prefix := "  "
			if i == m.notebookIndex {
				prefix = "› "
			}
			line := prefix + choices[i].name
			if choices[i].create {
				line = prefix + "Create " + choices[i].name
			} else if choices[i].name == m.notebook {
				line += "  · current"
			}
			line = ansi.Truncate(line, width-8, "…")
			if i == m.notebookIndex {
				content += m.heading(line) + "\n"
			} else {
				content += m.muted(line) + "\n"
			}
		}
		// The centered picker uses a steady virtual cursor. The main editor
		// uses a native cursor, so typing needs no periodic redraw timer.
		input := m.notebookInput
		input.SetVirtualCursor(true)
		styles := input.Styles()
		styles.Cursor.Blink = false
		input.SetStyles(styles)
		enter := "Enter open"
		if len(choices) > 0 && choices[min(m.notebookIndex, len(choices)-1)].create {
			enter = "Enter create"
		}
		hint := fitHints(width-8, keyHint{enter, enter}, keyHint{"Esc cancel", "Esc cancel"}, keyHint{"↑↓ choose", "↑↓ choose"})
		content += "\n" + input.View() + "\n\n" + m.muted(hint)
		if m.problem {
			content += "\n" + lipgloss.NewStyle().Foreground(m.colors.warning).Render(ansi.Truncate(safeText(m.notice), width-8, "…"))
		}
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(m.colors.accent).
		Padding(1, 3).Width(width).MaxHeight(m.height).Render(content)
}

func (m *Model) heading(value string) string {
	return m.headingStyle().Render(value)
}

func (m *Model) headingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(m.colors.accent)
}

func (m *Model) locationFieldWidth() int {
	d := m.dimensions()
	meta := m.noteDate(time.Now())
	if m.editing != nil {
		meta = m.noteDate(m.editing.CreatedAt)
	} else if m.settings.Style != journal.StyleJournal {
		meta = "New entry"
	}
	return max(1, d.pane-6-lipgloss.Width(meta)-2)
}

func (m *Model) noteDate(created time.Time) string {
	if m.dimensions().pane < 64 {
		return created.Local().Format("Jan 02, 2006")
	}
	return dateLabel(created)
}
func (m *Model) muted(value string) string {
	return lipgloss.NewStyle().Foreground(m.colors.muted).Render(value)
}

func spread(left, right string, width int) string {
	rightWidth := lipgloss.Width(right)
	if lipgloss.Width(left)+rightWidth+2 > width {
		return ansi.Truncate(left, width, "…")
	}
	return left + strings.Repeat(" ", max(0, width-lipgloss.Width(left)-rightWidth)) + right
}

// Notes may be edited outside this program. Never interpret terminal escape
// sequences from their content; keep the stored bytes untouched.
func safeText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(value))
}

type entryItem struct{ entry journal.Entry }

func (item entryItem) FilterValue() string { return item.entry.Content }

type entryDelegate struct{ colors palette }

func (entryDelegate) Height() int                         { return 3 }
func (entryDelegate) Spacing() int                        { return 1 }
func (entryDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d entryDelegate) Render(w io.Writer, model list.Model, index int, value list.Item) {
	item, ok := value.(entryItem)
	if !ok {
		return
	}
	title, summary := "Untitled", ""
	found := false
	for line := range strings.Lines(item.entry.Content) {
		line = strings.TrimSpace(safeText(line))
		if line == "" {
			continue
		}
		if !found {
			title = strings.TrimLeft(line, "# ")
			found = true
		} else {
			summary = line
			break
		}
	}
	if item.entry.Location != "" {
		summary = safeText(item.entry.Location)
	}
	width := max(1, model.Width()-2)
	style := lipgloss.NewStyle().Width(model.Width()).Padding(0, 1)
	titleStyle := lipgloss.NewStyle().Foreground(d.colors.ink)
	metaStyle := lipgloss.NewStyle().Foreground(d.colors.muted)
	if index == model.Index() {
		style = style.Background(d.colors.selected)
		titleStyle = titleStyle.Foreground(d.colors.accent).Bold(true)
	}
	text := titleStyle.Render(ansi.Truncate(title, width, "…")) + "\n" +
		metaStyle.Render(ansi.Truncate(item.entry.CreatedAt.Local().Format("Jan 02 · 3:04 PM"), width, "")) + "\n" +
		metaStyle.Render(ansi.Truncate(summary, width, "…"))
	fmt.Fprint(w, style.Render(text))
}
