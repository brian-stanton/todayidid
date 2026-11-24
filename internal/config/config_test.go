package config

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	tests := []struct {
		name, dir, notebook, wantDir, wantNotebook string
		env                                        map[string]string
	}{
		{name: "defaults outside working directory", wantDir: filepath.Join(home, ".local", "share", "todayidid"), wantNotebook: "personal"},
		{name: "xdg", env: map[string]string{"XDG_DATA_HOME": "/custom/data"}, wantDir: "/custom/data/todayidid", wantNotebook: "personal"},
		{name: "relative xdg ignored", env: map[string]string{"XDG_DATA_HOME": "relative"}, wantDir: filepath.Join(home, ".local", "share", "todayidid"), wantNotebook: "personal"},
		{name: "environment", env: map[string]string{"TODAYIDID_DIR": "/notes", "TODAYIDID_NOTEBOOK": "work"}, wantDir: "/notes", wantNotebook: "work"},
		{name: "flags override environment", dir: "/flag-notes", notebook: "project", env: map[string]string{"TODAYIDID_DIR": "/notes", "TODAYIDID_NOTEBOOK": "work"}, wantDir: "/flag-notes", wantNotebook: "project"},
		{name: "tilde", dir: "~/notes", wantDir: filepath.Join(home, "notes"), wantNotebook: "personal"},
		{name: "home tilde", dir: "~", wantDir: home, wantNotebook: "personal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolve(tt.dir, tt.notebook, func(key string) string { return tt.env[key] }, func() (string, error) { return home, nil })
			if err != nil {
				t.Fatal(err)
			}
			if got.Dir != filepath.FromSlash(tt.wantDir) || got.Notebook != tt.wantNotebook {
				t.Fatalf("got %+v, want dir %q notebook %q", got, tt.wantDir, tt.wantNotebook)
			}
		})
	}
}

func TestResolveRejectsInvalidNotebook(t *testing.T) {
	_, err := resolve("/notes", "../escape", func(string) string { return "" }, func() (string, error) { return "/home", nil })
	if err == nil {
		t.Fatal("expected invalid notebook error")
	}
}

func TestResolveHomeError(t *testing.T) {
	_, err := resolve("", "", func(string) string { return "" }, func() (string, error) { return "", errors.New("unavailable") })
	if err == nil {
		t.Fatal("expected home directory error")
	}
}
