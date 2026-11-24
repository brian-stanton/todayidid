package journal

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSettingsDefaultsDoNotCreateNotebooks(t *testing.T) {
	s := newTestStore(t)
	for name, style := range map[string]NotebookStyle{
		"personal": StyleJournal, "journal": StyleJournal, "Personal": StyleJournal,
		"work": StyleNotes, "ideas": StyleNotes,
	} {
		got, err := s.Settings(name)
		if err != nil || got.Style != style {
			t.Errorf("%s settings = %+v, %v", name, got, err)
		}
		if _, err := os.Stat(filepath.Join(s.Root(), name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("reading settings created %s: %v", name, err)
		}
	}
}

func TestNotebookSettingsPersistPrivatelyAndIndependently(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSettings("work", NotebookSettings{Style: StyleJournal}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings("personal", NotebookSettings{Style: StyleNotes}); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	for name, style := range map[string]NotebookStyle{"work": StyleJournal, "personal": StyleNotes} {
		settings, err := reopened.Settings(name)
		if err != nil || settings.Style != style {
			t.Fatalf("%s settings = %+v, %v", name, settings, err)
		}
		path := filepath.Join(s.Root(), name, settingsFile)
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("config permissions = %v, %v", info, err)
		}
		if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("notebook permissions = %v, %v", info, err)
		}
		entries, err := reopened.List(name)
		if err != nil || len(entries) != 0 {
			t.Fatalf("config appeared as an entry: %+v, %v", entries, err)
		}
	}
	if err := reopened.SaveSettings("work", NotebookSettings{Style: StyleNotes}); err != nil {
		t.Fatal(err)
	}
	settings, err := s.Settings("work")
	if err != nil || settings.Style != StyleNotes {
		t.Fatalf("changed settings not visible to original store: %+v, %v", settings, err)
	}
	files, err := os.ReadDir(filepath.Join(s.Root(), "work"))
	if err != nil || len(files) != 1 || files[0].Name() != settingsFile {
		t.Fatalf("settings left temporary files: %v, %v", files, err)
	}
}

func TestSavingSettingsPreservesUnknownFields(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSettings("work", NotebookSettings{Style: StyleNotes}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Root(), "work", settingsFile)
	raw := `{"style":"notes","future":{"nested":[1,"two",true]},"label":"東京"}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings("work", NotebookSettings{Style: StyleJournal}); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var before, after map[string]any
	if err := json.Unmarshal([]byte(raw), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bytes, &after); err != nil {
		t.Fatal(err)
	}
	before["style"] = "journal"
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("unknown fields lost: %s", bytes)
	}
}

func TestInvalidSettingsAreRejectedWithoutOverwriting(t *testing.T) {
	for _, raw := range []string{"", "{broken", "null", "[]", strings.Repeat("x", maxSettingsBytes+1)} {
		s := newTestStore(t)
		if err := s.SaveSettings("work", NotebookSettings{Style: StyleNotes}); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(s.Root(), "work", settingsFile)
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Settings("work"); err == nil {
			t.Errorf("read invalid settings %q", raw)
		}
		if err := s.SaveSettings("work", NotebookSettings{Style: StyleJournal}); err == nil {
			t.Errorf("overwrote invalid settings %q", raw)
		}
		if bytes, err := os.ReadFile(path); err != nil || string(bytes) != raw {
			t.Fatal("invalid settings were modified")
		}
	}
	s := newTestStore(t)
	for _, style := range []NotebookStyle{"", "calendar", "Journal"} {
		if err := s.SaveSettings("new", NotebookSettings{Style: style}); err == nil {
			t.Errorf("accepted invalid style %q", style)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "new")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid style created a notebook: %v", err)
	}
}

func TestSettingsRequireAKnownStyleString(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSettings("work", NotebookSettings{Style: StyleNotes}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Root(), "work", settingsFile)
	for _, raw := range []string{`{"style":null}`, `{"style":123}`, `{"style":"future"}`} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Settings("work"); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if err := os.WriteFile(path, []byte(`{"future":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := s.Settings("work")
	if err != nil || settings.Style != StyleNotes {
		t.Fatalf("missing style did not use notebook default: %+v, %v", settings, err)
	}
}

func TestSettingsRejectSymlinksAndInvalidNotebookPaths(t *testing.T) {
	s := newTestStore(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.Root(), "linked")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../outside", "/tmp", "linked"} {
		if _, err := s.Settings(name); err == nil {
			t.Errorf("read unsafe notebook %q", name)
		}
		if err := s.SaveSettings(name, NotebookSettings{Style: StyleJournal}); err == nil {
			t.Errorf("wrote unsafe notebook %q", name)
		}
	}
	if err := os.Mkdir(filepath.Join(s.Root(), "work"), 0700); err != nil {
		t.Fatal(err)
	}
	externalFile := filepath.Join(outside, "config.json")
	if err := os.WriteFile(externalFile, []byte(`{"style":"notes"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalFile, filepath.Join(s.Root(), "work", settingsFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Settings("work"); err == nil {
		t.Fatal("read settings through symlink")
	}
	if err := s.SaveSettings("work", NotebookSettings{Style: StyleJournal}); err == nil {
		t.Fatal("wrote settings through symlink")
	}
}
