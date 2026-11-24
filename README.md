# todayidid

A keyboard-first journal for the terminal.

Built in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), and [Lip Gloss](https://github.com/charmbracelet/lipgloss). Notes are ordinary Markdown files, grouped into notebooks such as `personal` and `work`.

## Start writing

Requires Go 1.26 or newer.

```sh
go build -trimpath -o bin/todayidid ./cmd/todayidid
./bin/todayidid
```

Or run `go run ./cmd/todayidid`. The original `go run ./cmd/main` entry point also works. Put the built binary on your `PATH` to use `todayidid` from anywhere.

The editor opens ready to type. Press **Ctrl+S** to save. New installations need no setup or existing notes directory.

**Ctrl+G** opens the command menu. Release the keys, then press a displayed letter, or choose with arrows and Enter. F1 is an alias; function keys are optional. The app keeps ordinary text controls such as Ctrl+W, Ctrl+N/P, and Ctrl+F and leaves your terminal and tmux configuration alone.

| Key | Action |
| --- | --- |
| Ctrl+S | Save the current note |
| Ctrl+G, n | Start a new note |
| Tab | Switch between writing and history |
| Ctrl+G, / | Search history |
| Ctrl+G, b | Choose a notebook |
| Ctrl+G, l | Set or clear the current or selected note's location |
| Ctrl+G, o | Change the notebook's writing style |
| Ctrl+G, a | Select all text in the editor |
| Ctrl+G, ? | Show keyboard shortcuts |
| Ctrl+G / F1 | Open or close the command menu |
| Ctrl+C | Quit |
| Esc | Go back / cancel |
| ↑ / ↓ or k / j | Move through history |
| Enter | Read the selected note |
| e | Edit the selected note in history |
| / | Search from history |
| r | Reload history after external edits or a Git pull |
| q | Quit from history |

The notebook picker filters as you type and puts an exact name match first. **Enter** opens the highlighted notebook. To add one, choose the **Create** row; its directory is created on the first save.

Changing notebooks, starting a new note, or quitting with unsaved changes asks what to do: **Ctrl+S** saves and continues, **d** discards, and **Esc** cancels the action. A failed save keeps the note in the editor.

Your draft stays in the editor while you browse history. Ctrl+S saves that draft; use **e** to open a historical note for editing first.

In search, **Enter** reads the selected match, **Tab** accepts the query and shows the results list, and **Esc** cancels the search and restores the previous view. In the editor, Home/End move within a line, Alt+arrows move by word, Ctrl+W deletes the previous word, Ctrl+N/P move between lines, and Ctrl+F moves forward one character. Select all is available through the command menu because Ctrl+G opens that menu.

## Quick capture

```sh
todayidid --notebook work add "Shipped the smaller, simpler implementation."
todayidid --notebook personal --location "Montréal" add "Walked along the canal."
printf '%s\n' '- Went for a walk.' '- Finished the chapter.' | todayidid add -
todayidid --notebook personal
```

Options go before the command. Run `todayidid --help` for the complete CLI.

## Notebook styles and locations

Each notebook has a writing style. `personal` and `journal` default to **journal**, a focused writing and reading view with the location at the upper left and the date at the right. `work` and other notebooks default to **notes**, which keeps history alongside the editor on wider terminals. History remains available in either style with Tab. Press **Ctrl+G**, then **o**, to change the current notebook's style. The setting is stored in that notebook's `.todayidid.json` and can travel with your notes in Git. Changing style does not rewrite existing entries.

Locations are optional, entered manually. Press **Ctrl+G**, then **l**, while writing, or click the location in the header, to edit it in place. Type a place, use arrows or Tab to cycle through recent places in the same notebook, or press **Ctrl+U** to clear the field. **Enter** applies the change; **Esc** cancels it. The current place carries forward to new notes in that notebook during the session. It is not automatically restored when you reopen the app or move to another computer. Existing entries retain their own location regardless of notebook style; editing an older entry does not change the current place for new entries. History search matches locations as well as text and dates.

Use `--location PLACE` or `-l PLACE` for an initial draft, quick capture, or import:

```sh
todayidid --location "Portland"
todayidid -n personal -l "Portland" import /path/to/note.md
```

Locations are trimmed and limited to 120 characters on one line. No location is inferred from your computer or network.

## Where notes live

By default, notes live in `$XDG_DATA_HOME/todayidid`, or `~/.local/share/todayidid` when XDG data home is unset. The location is independent of the application checkout and your current directory.

Choose a directory and default notebook with environment variables:

```sh
export TODAYIDID_DIR="$HOME/journals"
export TODAYIDID_NOTEBOOK=personal
```

Or override them for one invocation:

```sh
todayidid --dir "$HOME/work-journal" --notebook work
```

`-d` and `-n` are short forms. `todayidid path` prints the resolved directory without creating it. Precedence is flags, then environment, then defaults.

`personal` and `work` are available immediately. Opening another valid notebook with `--notebook NAME` creates its directory on the first save. Each notebook is isolated under the selected root:

```text
journals/
  personal/
    2026/11/
      2026-11-23T151234.000000000Z-a1b2c3d4.md
  work/
    2026/11/
      2026-11-23T153000.000000000Z-e5f6a7b8.md
```

Each capture is one UTF-8 Markdown file. Its timestamp and random suffix are in the filename; the body is exactly what you wrote. Separate files make captures from different computers easy to merge. Existing Markdown files in a notebook are also readable; filenames outside the app's format use the file's modification time for ordering. Entries are limited to 1 MiB, with an explicit error for larger input.

When a note has a location, a small HTML comment on the first line records it, followed by a blank line and the original body:

```markdown
<!-- todayidid: {"version":1,"location":"Montréal"} -->

Walked along the canal.
```

The comment is hidden in normal Markdown rendering and stays in the same file as the note. Existing files need no conversion; notes without a location remain plain Markdown. The app reserves the `<!-- todayidid: ` first-line prefix and protects imported text beginning with that prefix so its body is still preserved.

The built-in editor supports up to 10,000 lines. Files containing text the editor cannot preserve exactly (such as tabs or CRLF line endings) remain readable, but must be edited externally. The app refuses to silently rewrite or truncate them. Import and quick capture preserve the original UTF-8 text. The terminal layout needs at least 42 columns and 16 rows.

When pasting new text into the editor, Windows line endings become newlines and tabs become four spaces. Use `import` or `add -` when exact source bytes matter.

Saves use temporary files and atomic replacement. New files have owner-only permissions. If an entry changed on disk since you opened it, the app refuses to overwrite that newer content. Reload history before editing again. Keep unsaved text before reloading or resolving the conflict.

## Store notes in a private Git repository

Point `TODAYIDID_DIR` into a **private** notes repository. The public application repository should hold code, not journal content. The app does not run Git commands or contact a server.

Create a private repository for your notes and clone it outside the application checkout. For example, with a notes repository at `~/journals`:

```sh
export TODAYIDID_DIR="$HOME/journals"
```

Use Git directly to transfer notes between computers:

```sh
git -C "$TODAYIDID_DIR" pull --rebase
# Write some notes.
git -C "$TODAYIDID_DIR" add .
git -C "$TODAYIDID_DIR" commit -m "Journal updates"
git -C "$TODAYIDID_DIR" push
```

Clone the private repository on each computer and set `TODAYIDID_DIR` to its local path. Work and personal notebooks can share one private repository, or use separate roots and repositories when they need different access.

Independent new entries normally merge without conflict. Editing the same existing entry on two computers can still create a normal Git conflict; resolve it in your editor and reload history.

The application ignores common local data directories (`notes/`, `journal/`, `data/`, and `out/`) in its code checkout. This is a convenience, not a substitute for choosing a private location; arbitrary paths inside a public repository can still be tracked.

## Import existing notes

Import a UTF-8 Markdown or text file:

```sh
todayidid --notebook personal import /path/to/note.md
```

This copies the whole text into one new entry, preserving its content and leaving the original file intact. Import does not infer entry boundaries or timestamps from the text. It is not deduplicated; run it once for each source file.

## Development

```sh
make build
make check
```

`make check` runs `go vet ./...` and the race-enabled tests. CI checks macOS and Linux.

The code has a few direct boundaries:

- `internal/journal`: files, notebooks, validation, and conflict-aware saves; standard library only.
- `internal/tui`: Bubble Tea state, editing, history, and terminal layout.
- `internal/config`: path and notebook selection.
- `internal/cli`: argument handling, quick capture, and explicit import.

Storage remains independent of the UI. Future exports, tags, or summaries can use the same files without a database migration. Add an index only if real journal sizes demonstrate the need.
