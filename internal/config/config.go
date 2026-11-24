// Package config resolves local configuration without a settings file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brian-stanton/todayidid/internal/journal"
)

type Config struct {
	Dir      string
	Notebook string
}

// Resolve applies flags, environment variables, then local defaults.
// Journal data never defaults to the current working directory.
func Resolve(dir, notebook string) (Config, error) {
	return resolve(dir, notebook, os.Getenv, os.UserHomeDir)
}

func resolve(dir, notebook string, getenv func(string) string, homeDir func() (string, error)) (Config, error) {
	if dir == "" {
		dir = getenv("TODAYIDID_DIR")
	}
	if notebook == "" {
		notebook = getenv("TODAYIDID_NOTEBOOK")
	}
	if notebook == "" {
		notebook = "personal"
	}
	if err := journal.ValidateNotebook(notebook); err != nil {
		return Config{}, err
	}
	if dir == "" {
		dataHome := getenv("XDG_DATA_HOME")
		// The XDG specification requires absolute paths; ignore relative values.
		if !filepath.IsAbs(dataHome) {
			home, err := homeDir()
			if err != nil {
				return Config{}, fmt.Errorf("find home directory: %w", err)
			}
			dataHome = filepath.Join(home, ".local", "share")
		}
		dir = filepath.Join(dataHome, "todayidid")
	} else if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := homeDir()
		if err != nil {
			return Config{}, fmt.Errorf("expand notes directory: %w", err)
		}
		if dir == "~" {
			dir = home
		} else {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve notes directory: %w", err)
	}
	return Config{Dir: abs, Notebook: notebook}, nil
}
