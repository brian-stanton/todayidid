package journal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

type NotebookStyle string

const (
	StyleJournal     NotebookStyle = "journal"
	StyleNotes       NotebookStyle = "notes"
	settingsFile                   = ".todayidid.json"
	maxSettingsBytes               = 4096
)

type NotebookSettings struct {
	Style NotebookStyle `json:"style"`
}

func DefaultStyle(name string) NotebookStyle {
	if strings.EqualFold(name, "personal") || strings.EqualFold(name, "journal") {
		return StyleJournal
	}
	return StyleNotes
}

// Settings reads a notebook's presentation preferences. Missing preferences
// use the notebook's default style without creating any directory or file.
func (s *Store) Settings(notebook string) (NotebookSettings, error) {
	if err := ValidateNotebook(notebook); err != nil {
		return NotebookSettings{}, err
	}
	path, err := s.safePath(filepath.Join(notebook, settingsFile), false)
	if err != nil {
		return NotebookSettings{}, err
	}
	fields, err := readSettings(path)
	if err != nil {
		return NotebookSettings{}, err
	}
	settings := NotebookSettings{Style: DefaultStyle(notebook)}
	if rawStyle, ok := fields["style"]; ok {
		var style NotebookStyle
		if err := json.Unmarshal(rawStyle, &style); err != nil {
			return NotebookSettings{}, fmt.Errorf("invalid notebook style: %w", err)
		}
		if err := validateStyle(style); err != nil {
			return NotebookSettings{}, err
		}
		settings.Style = style
	}
	return settings, nil
}

// SaveSettings atomically writes notebook preferences, preserving any unknown
// JSON fields. New files are owner-only. A sync failure after publication is
// returned as a warning explaining that the file has already been written.
func (s *Store) SaveSettings(notebook string, settings NotebookSettings) error {
	if err := ValidateNotebook(notebook); err != nil {
		return err
	}
	if err := validateStyle(settings.Style); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.safePath(filepath.Join(notebook, settingsFile), true)
	if err != nil {
		return err
	}
	fields, err := readSettings(path)
	if err != nil {
		return err
	}
	create := fields == nil
	if create {
		fields = make(map[string]json.RawMessage)
	}
	style, err := json.Marshal(settings.Style)
	if err != nil {
		return err
	}
	fields["style"] = style
	body, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if len(body) > maxSettingsBytes {
		return fmt.Errorf("notebook settings exceed the %d byte limit", maxSettingsBytes)
	}
	if _, err := writeAtomic(path, string(body), create, syncDirectory); err != nil {
		return fmt.Errorf("save notebook settings: %w", err)
	}
	return nil
}

func validateStyle(style NotebookStyle) error {
	if style != StyleJournal && style != StyleNotes {
		return fmt.Errorf("unsupported notebook style %q; use journal or notes", style)
	}
	return nil
}

func readSettings(path string) (map[string]json.RawMessage, error) {
	raw, err := readTextFile(path, maxSettingsBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read notebook settings: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, fmt.Errorf("invalid notebook settings: %w", err)
	}
	if fields == nil {
		return nil, errors.New("notebook settings must be a JSON object")
	}
	return fields, nil
}
