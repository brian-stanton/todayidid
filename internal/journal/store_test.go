package journal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "notes"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFreshStore(t *testing.T) {
	s := newTestStore(t)
	notebooks, err := s.Notebooks()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(notebooks, []string{"personal", "work"}) {
		t.Fatalf("notebooks = %v", notebooks)
	}
	entries, err := s.List("personal")
	if err != nil || len(entries) != 0 {
		t.Fatalf("fresh list = %v, %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "personal")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("listing created notebook directory: %v", err)
	}
	if info, err := os.Stat(s.Root()); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("root permissions = %v, %v", info, err)
	}
}

func TestRoundTripAndNotebookIsolation(t *testing.T) {
	s := newTestStore(t)
	content := "# Today I did\n\n- Fixed a race 🚀\n- 日本語 works\n\n"
	first, err := s.Save("work", nil, content)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first.ID, "work") || filepath.IsAbs(first.ID) || strings.Contains(first.ID, ":") {
		t.Fatalf("nonportable or nonrelative ID: %s", first.ID)
	}
	if !strings.HasPrefix(first.ID, first.CreatedAt.Format("2006/01/")) {
		t.Fatalf("ID does not match date: %s", first.ID)
	}
	path := filepath.Join(s.Root(), "work", filepath.FromSlash(first.ID))
	bytes, err := os.ReadFile(path)
	if err != nil || string(bytes) != content {
		t.Fatalf("disk content = %q, %v", bytes, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("entry permissions = %v, %v", info, err)
	}
	for dir := filepath.Dir(path); dir != s.Root(); dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("directory permissions for %s = %v, %v", dir, info, err)
		}
	}
	if _, err := s.Save("personal", nil, "A personal thought"); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List("work")
	if err != nil || len(entries) != 1 || entries[0] != first {
		t.Fatalf("roundtrip list = %+v, %v", entries, err)
	}
	updated, err := s.Save("work", &first, "# Revised\n\nBetter now.\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != first.ID || updated.CreatedAt != first.CreatedAt {
		t.Fatalf("edit changed identity: %+v", updated)
	}
	entries, err = s.List("work")
	if err != nil || len(entries) != 1 || entries[0] != updated {
		t.Fatalf("edited list = %+v, %v", entries, err)
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatalf("temporary files remained: %v, %v", files, err)
	}
}

func TestConcurrentCreationProducesDistinctEntries(t *testing.T) {
	s := newTestStore(t)
	const count = 20
	results := make(chan error, count)
	var group sync.WaitGroup
	for i := 0; i < count; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			_, err := s.Save("work", nil, fmt.Sprintf("Entry %d", i))
			results <- err
		}(i)
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := s.List("work")
	if err != nil || len(entries) != count {
		t.Fatalf("entries = %d, %v", len(entries), err)
	}
	seen := make(map[string]bool)
	for i, entry := range entries {
		if seen[entry.ID] {
			t.Fatalf("duplicate ID: %s", entry.ID)
		}
		seen[entry.ID] = true
		if i > 0 && entry.CreatedAt.After(entries[i-1].CreatedAt) {
			t.Fatal("entries are not newest first")
		}
	}
}

func TestConcurrentEditsDoNotOverwriteEachOther(t *testing.T) {
	s := newTestStore(t)
	original, err := s.Save("work", nil, "Original")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			_, err := s.Save("work", &original, fmt.Sprintf("Edit %d", i))
			results <- err
		}(i)
	}
	successes, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d", successes, conflicts)
	}
}

func TestExternalModificationAndDeletionAreConflicts(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprintf("deleted=%v", deleted), func(t *testing.T) {
			s := newTestStore(t)
			entry, err := s.Save("work", nil, "Original")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.Root(), "work", entry.ID)
			if deleted {
				err = os.Remove(path)
			} else {
				err = os.WriteFile(path, []byte("Changed by another editor"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Save("work", &entry, "My edit"); !errors.Is(err, ErrConflict) {
				t.Fatalf("expected conflict, got %v", err)
			}
			if !deleted {
				body, err := os.ReadFile(path)
				if err != nil || string(body) != "Changed by another editor" {
					t.Fatalf("external edit overwritten: %q, %v", body, err)
				}
			}
		})
	}
}

func TestRejectInvalidInputs(t *testing.T) {
	s := newTestStore(t)
	for _, name := range []string{"", ".", "..", "../escape", "/tmp", "a/b", "a\\b", ".git", "-work", "work notes", "a:b", strings.Repeat("a", 65)} {
		if _, err := s.Save(name, nil, "hello"); err == nil {
			t.Errorf("accepted notebook %q", name)
		}
		if _, err := s.List(name); err == nil {
			t.Errorf("listed notebook %q", name)
		}
	}
	for _, content := range []string{"", " \n\t", string([]byte{0xff}), strings.Repeat("x", MaxEntryBytes+1)} {
		if _, err := s.Save("work", nil, content); err == nil {
			t.Errorf("accepted invalid content of length %d", len(content))
		}
	}
	for _, id := range []string{"", "../escape.md", "a/../../escape.md", "/tmp/escape.md", "a\\escape.md", "a/.hidden.md", "a//entry.md", "a.txt", "C:/entry.md"} {
		entry := Entry{ID: id, Notebook: "work", Content: "Original"}
		if _, err := s.Save("work", &entry, "changed"); err == nil {
			t.Errorf("accepted ID %q", id)
		}
	}
	entry := Entry{ID: "entry.md", Notebook: "personal", Content: "Original"}
	if _, err := s.Save("work", &entry, "changed"); err == nil {
		t.Error("accepted cross-notebook edit")
	}
}

func TestOrdinaryMarkdownAndHiddenFiles(t *testing.T) {
	s := newTestStore(t)
	base := filepath.Join(s.Root(), "ideas")
	if err := os.MkdirAll(filepath.Join(base, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"ordinary.md": "An imported file\n", "UPPER.MD": "Another file\n", "ignore.txt": "Not a note",
		".hidden.md": "Hidden", ".git/internal.md": "Git metadata",
	} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	when := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(base, "ordinary.md"), when, when); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List("ideas")
	if err != nil || len(entries) != 2 {
		t.Fatalf("ordinary files: %+v, %v", entries, err)
	}
	if entries[1].ID != "ordinary.md" || !entries[1].CreatedAt.Equal(when) {
		t.Fatalf("mtime fallback: %+v", entries[1])
	}
	if _, err := s.Save("ideas", &entries[1], "Edited imported file\n"); err != nil {
		t.Fatal(err)
	}
	notebooks, err := s.Notebooks()
	if err != nil || !reflect.DeepEqual(notebooks, []string{"ideas", "personal", "work"}) {
		t.Fatalf("notebooks = %v, %v", notebooks, err)
	}
}

func TestSymlinkRootSupportedButNestedLinksRejected(t *testing.T) {
	actual := t.TempDir()
	link := filepath.Join(t.TempDir(), "notes")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	s, err := New(link)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(actual, "work")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("work", nil, "Must not escape"); err == nil {
		t.Error("saved through notebook symlink")
	}
	if _, err := s.List("work"); err == nil {
		t.Error("listed through notebook symlink")
	}
	entry, err := s.Save("personal", nil, "Original")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(actual, "personal", entry.ID)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	externalFile := filepath.Join(outside, "private.md")
	if err := os.WriteFile(externalFile, []byte("Original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(externalFile, path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save("personal", &entry, "Must not overwrite"); err == nil {
		t.Error("edited through file symlink")
	}
	entries, err := s.List("personal")
	if err != nil || len(entries) != 0 {
		t.Fatalf("followed nested link: %v, %v", entries, err)
	}
}

func TestOversizeFileReturnsAnErrorWithoutTruncating(t *testing.T) {
	s := newTestStore(t)
	if err := os.Mkdir(filepath.Join(s.Root(), "work"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Root(), "work", "large.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxEntryBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List("work"); err == nil {
		t.Error("oversized file did not return an error")
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != MaxEntryBytes+1 {
		t.Fatalf("file was changed: %v, %v", info, err)
	}
}

func TestAtomicCreateNeverOverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry.md")
	if err := os.WriteFile(path, []byte("Original"), 0600); err != nil {
		t.Fatal(err)
	}
	if published, err := writeAtomic(path, "Collision", true, syncDirectory); published || !errors.Is(err, os.ErrExist) {
		t.Fatalf("collision: published = %v, error = %v", published, err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "Original" {
		t.Fatalf("collision overwrote data: %q, %v", body, err)
	}
}

func TestAtomicWriteReportsPublishedContentWhenDirectorySyncFails(t *testing.T) {
	for _, create := range []bool{true, false} {
		t.Run(fmt.Sprintf("create=%v", create), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "entry.md")
			if !create {
				if err := os.WriteFile(path, []byte("Original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			syncErr := errors.New("injected directory sync failure")
			published, err := writeAtomic(path, "Saved content", create, func(directory string) error {
				if directory != filepath.Dir(path) {
					t.Fatalf("sync directory = %q", directory)
				}
				return syncErr
			})
			if !published || !errors.Is(err, syncErr) {
				t.Fatalf("published = %v, error = %v", published, err)
			}
			body, err := os.ReadFile(path)
			if err != nil || string(body) != "Saved content" {
				t.Fatalf("published content = %q, error = %v", body, err)
			}
			files, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(files) != 1 {
				t.Fatalf("temporary files remained after sync failure: %v, %v", files, err)
			}
		})
	}
}

func TestNotebookMustBeADirectory(t *testing.T) {
	s := newTestStore(t)
	if err := os.WriteFile(filepath.Join(s.Root(), "work"), []byte("A file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List("work"); err == nil {
		t.Error("a notebook file was silently treated as an empty directory")
	}
	if _, err := s.Save("work", nil, "Entry"); err == nil {
		t.Error("saved into a notebook file")
	}
}
