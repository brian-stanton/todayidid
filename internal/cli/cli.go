// Package cli implements the command line around the journal and terminal UI.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/brian-stanton/todayidid/internal/config"
	"github.com/brian-stanton/todayidid/internal/journal"
	"github.com/brian-stanton/todayidid/internal/tui"

	tea "charm.land/bubbletea/v2"
)

// Version may be set at build time with -ldflags '-X github.com/brian-stanton/todayidid/internal/cli.Version=...'.
var Version = "dev"

const usage = `todayidid — terminal journal

Usage:
  todayidid [options]               Open the journal
  todayidid [options] add TEXT       Save a quick note
  todayidid [options] add -          Save a note from standard input
  todayidid [options] import FILE    Copy an existing text file into one entry
  todayidid [options] path           Print the notes directory

Options (place before the command):
  -d, --dir PATH        Notes directory (TODAYIDID_DIR)
  -n, --notebook NAME   Notebook (TODAYIDID_NOTEBOOK; default: personal)
  -l, --location PLACE  Location for this note (optional)
  -h, --help            Show this help
      --version         Show the version

Notes default to $XDG_DATA_HOME/todayidid or ~/.local/share/todayidid.
Notes are local; Git operations are manual.
`

// Run returns an exit status. It never exits the process, so commands are testable.
func Run(args []string, in io.Reader, out, errOut io.Writer) int {
	if err := run(args, in, out, errOut); err != nil {
		fmt.Fprintln(errOut, "todayidid:", err)
		return 1
	}
	return 0
}

func run(args []string, in io.Reader, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("todayidid", flag.ContinueOnError)
	flags.SetOutput(errOut)
	flags.Usage = func() { fmt.Fprint(out, usage) }
	var dir, notebook, location string
	var version bool
	flags.StringVar(&dir, "dir", "", "notes directory")
	flags.StringVar(&dir, "d", "", "notes directory")
	flags.StringVar(&notebook, "notebook", "", "notebook")
	flags.StringVar(&notebook, "n", "", "notebook")
	flags.StringVar(&location, "location", "", "location for this note")
	flags.StringVar(&location, "l", "", "location for this note")
	flags.BoolVar(&version, "version", false, "show version")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if version {
		fmt.Fprintln(out, "todayidid", Version)
		return nil
	}
	args = flags.Args()
	// Validate before opening a store so a typo has no filesystem side effects.
	if len(args) > 0 {
		switch args[0] {
		case "path":
			if len(args) != 1 {
				return errors.New("path takes no arguments")
			}
		case "add":
			if len(args) < 2 {
				return errors.New("add needs text, or '-' to read standard input")
			}
		case "import":
			if len(args) != 2 {
				return errors.New("import needs exactly one file path")
			}
		default:
			return fmt.Errorf("unknown command %q; use --help for usage", args[0])
		}
	}
	cfg, err := config.Resolve(dir, notebook)
	if err != nil {
		return err
	}
	location, err = journal.NormalizeLocation(location)
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "path" {
		fmt.Fprintln(out, cfg.Dir)
		return nil
	}
	if len(args) > 0 {
		var content string
		if args[0] == "import" {
			f, err := os.Open(args[1])
			if err != nil {
				return fmt.Errorf("read import: %w", err)
			}
			defer f.Close()
			content, err = readInput(f)
			if err != nil {
				return err
			}
		} else if len(args) == 2 && args[1] == "-" {
			content, err = readInput(in)
			if err != nil {
				return err
			}
		} else {
			content = strings.Join(args[1:], " ")
		}
		store, err := journal.New(cfg.Dir)
		if err != nil {
			return err
		}
		entry, err := store.SaveWithLocation(cfg.Notebook, nil, content, location)
		if entry.ID != "" {
			fmt.Fprintln(out, filepath.Join(store.Root(), entry.Notebook, entry.ID))
		}
		return err
	}
	store, err := journal.New(cfg.Dir)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(tui.NewWithLocation(store, cfg.Notebook, location), tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}

func readInput(in io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(in, journal.MaxEntryBytes+1))
	if err != nil {
		return "", fmt.Errorf("read note: %w", err)
	}
	if len(data) > journal.MaxEntryBytes {
		return "", fmt.Errorf("note exceeds %d MiB", journal.MaxEntryBytes>>20)
	}
	return string(data), nil
}
