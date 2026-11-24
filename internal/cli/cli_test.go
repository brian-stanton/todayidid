package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brian-stanton/todayidid/internal/journal"
)

func TestAddAndImport(t *testing.T) {
	root := filepath.Join(t.TempDir(), "journal")
	legacy := filepath.Join(t.TempDir(), "old.txt")
	legacyBody := "  Old journal\n\n- Kept the original spacing.\n"
	if err := os.WriteFile(legacy, []byte(legacyBody), 0600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args     []string
		in, want string
	}{
		{args: []string{"add", "Shipped", "the", "thing"}, want: "Shipped the thing"},
		{args: []string{"add", "-"}, in: "A note\nwith two lines\n", want: "A note\nwith two lines\n"},
		{args: []string{"import", legacy}, want: legacyBody},
	}
	for _, tt := range tests {
		var out, errOut bytes.Buffer
		args := append([]string{"--dir", root, "--notebook", "work"}, tt.args...)
		if code := Run(args, strings.NewReader(tt.in), &out, &errOut); code != 0 {
			t.Fatalf("Run(%v): %s", tt.args, errOut.String())
		}
		file := strings.TrimSpace(out.String())
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(file, filepath.Join(resolvedRoot, "work")+string(filepath.Separator)) {
			t.Fatalf("saved outside notebook: %q", file)
		}
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tt.want {
			t.Fatalf("body = %q, want %q", got, tt.want)
		}
	}
	got, err := os.ReadFile(legacy)
	if err != nil || string(got) != legacyBody {
		t.Fatalf("import modified original: %q, %v", got, err)
	}
}

func TestInformationalCommandsDoNotCreateNotes(t *testing.T) {
	for _, command := range []string{"path", "--help", "--version", "unknown-command"} {
		t.Run(command, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "must-not-exist")
			var out, errOut bytes.Buffer
			Run([]string{"--dir", root, command}, strings.NewReader(""), &out, &errOut)
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatalf("command created data directory: %v", err)
			}
			if command == "path" && strings.TrimSpace(out.String()) != root {
				t.Fatalf("path = %q", out.String())
			}
		})
	}
}

func TestInvalidInputDoesNotSave(t *testing.T) {
	for _, args := range [][]string{{"add"}, {"import"}, {"path", "extra"}, {"add", "   "}, {"import", "/missing/file"}} {
		var out, errOut bytes.Buffer
		root := t.TempDir()
		if code := Run(append([]string{"--dir", root}, args...), strings.NewReader(""), &out, &errOut); code == 0 {
			t.Fatalf("expected error for %v", args)
		}
		if out.Len() != 0 {
			t.Fatalf("unexpected success output for %v: %q", args, out.String())
		}
	}
}

func TestReadInputRejectsOversize(t *testing.T) {
	if _, err := readInput(strings.NewReader(strings.Repeat("x", journal.MaxEntryBytes+1))); err == nil {
		t.Fatal("expected oversized input error")
	}
}

func TestCaptureWithLocation(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "old.txt")
	body := "\tAn existing note\r\n\r\nWith its spacing intact.  \r\n"
	if err := os.WriteFile(legacy, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, flag, input, want string
		args                    []string
	}{
		{name: "add", flag: "--location", args: []string{"add", "Walked", "home."}, want: "Walked home."},
		{name: "stdin", flag: "-l", args: []string{"add", "-"}, input: body, want: body},
		{name: "import", flag: "--location", args: []string{"import", legacy}, want: body},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			var out, errOut bytes.Buffer
			args := append([]string{"--dir", root, "--notebook", "personal", tt.flag, "  Montréal  "}, tt.args...)
			if code := Run(args, strings.NewReader(tt.input), &out, &errOut); code != 0 {
				t.Fatalf("capture failed: %s", errOut.String())
			}
			store, err := journal.New(root)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := store.List("personal")
			if err != nil || len(entries) != 1 {
				t.Fatalf("entries = %v, error = %v", entries, err)
			}
			if entries[0].Location != "Montréal" || entries[0].Content != tt.want {
				t.Fatalf("entry location/body = %q / %q, want %q / %q", entries[0].Location, entries[0].Content, "Montréal", tt.want)
			}
			if _, err := os.Stat(strings.TrimSpace(out.String())); err != nil {
				t.Fatalf("output path does not identify the saved file: %v", err)
			}
		})
	}
	got, err := os.ReadFile(legacy)
	if err != nil || string(got) != body {
		t.Fatalf("import modified original: %q, %v", got, err)
	}
}

func TestLocationDoesNotCarryBetweenCommands(t *testing.T) {
	root := t.TempDir()
	for _, flags := range [][]string{{"--location", "Paris"}, nil} {
		var out, errOut bytes.Buffer
		args := append([]string{"--dir", root, "--notebook", "personal"}, flags...)
		args = append(args, "add", "A note.")
		if code := Run(args, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatalf("capture failed: %s", errOut.String())
		}
		if len(flags) == 0 {
			body, err := os.ReadFile(strings.TrimSpace(out.String()))
			if err != nil || string(body) != "A note." {
				t.Fatalf("default capture inherited metadata: %q, %v", body, err)
			}
		}
	}
}

func TestInvalidLocationDoesNotCreateNotes(t *testing.T) {
	for _, location := range []string{"Paris\nFrance", "\x1b[31mParis", strings.Repeat("é", journal.MaxLocationRunes+1)} {
		root := filepath.Join(t.TempDir(), "must-not-exist")
		var out, errOut bytes.Buffer
		if code := Run([]string{"--dir", root, "--location", location, "add", "A note."}, strings.NewReader(""), &out, &errOut); code == 0 {
			t.Fatalf("expected invalid location error for %q", location)
		}
		if out.Len() != 0 {
			t.Fatalf("unexpected success output: %q", out.String())
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatalf("invalid location created data directory: %v", err)
		}
	}
}
