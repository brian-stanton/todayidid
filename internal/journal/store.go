// Package journal stores notebooks as ordinary Markdown files. It has no
// dependency on the terminal UI or on Git.
package journal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// MaxEntryBytes prevents a stray binary or enormous Markdown file from
	// exhausting memory when a notebook is opened. Entries are never truncated.
	MaxEntryBytes = 1 << 20
	filenameTime  = "2006-01-02T150405.000000000Z"
)

var (
	ErrConflict   = errors.New("entry changed on disk; reopen it before saving")
	ErrEmptyEntry = errors.New("entry is empty")
)

// Entry.ID is a slash-separated path relative to its notebook. Content is the
// exact UTF-8 Markdown file body, including its whitespace.
type Entry struct {
	ID        string
	Notebook  string
	Content   string
	Location  string
	CreatedAt time.Time

	// Keep the entire original file, including metadata, for optimistic edits.
	diskContent string
	hasSnapshot bool
}

type Store struct {
	root string
	mu   sync.Mutex
}

// New opens or creates a notes directory. A symlink at root is supported so the
// directory can live in an existing private repository or submodule.
func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("notes directory must not be empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve notes directory: %w", err)
	}
	if err := os.MkdirAll(abs, 0700); err != nil {
		return nil, fmt.Errorf("create notes directory: %w", err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve notes directory: %w", err)
	}
	return &Store{root: abs}, nil
}

func (s *Store) Root() string { return s.root }

// ValidateNotebook restricts names to portable directory names, with no path
// separators or hidden directories. Notebook names are case-sensitive.
func ValidateNotebook(name string) error {
	if len(name) == 0 || len(name) > 64 {
		return errors.New("notebook name must contain 1–64 letters, numbers, hyphens, or underscores")
	}
	for i, c := range name {
		letterOrNumber := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !letterOrNumber && (i == 0 || c != '-' && c != '_') {
			return errors.New("notebook name must start with a letter or number and contain only letters, numbers, hyphens, or underscores")
		}
	}
	return nil
}

// Notebooks includes the two starter notebooks without creating empty folders.
// Hidden directories and symlinks inside the notes directory are not followed.
func (s *Store) Notebooks() ([]string, error) {
	items, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("read notebooks: %w", err)
	}
	names := map[string]bool{"personal": true, "work": true}
	for _, item := range items {
		if item.IsDir() && ValidateNotebook(item.Name()) == nil {
			names[item.Name()] = true
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

// List returns newest entries first. Any non-hidden .md file in the notebook
// can be read and edited. Files created outside todayidid use modification time
// when their filename does not contain a todayidid timestamp.
func (s *Store) List(notebook string) ([]Entry, error) {
	if err := ValidateNotebook(notebook); err != nil {
		return nil, err
	}
	base, err := s.safePath(notebook, false)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0)
	err = filepath.WalkDir(base, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == base && errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if path == base {
			if !item.IsDir() {
				return fmt.Errorf("notebook path is not a directory: %s", base)
			}
			return nil
		}
		if strings.HasPrefix(item.Name(), ".") {
			if item.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !item.Type().IsRegular() || !strings.EqualFold(filepath.Ext(item.Name()), ".md") {
			return nil
		}
		raw, err := readContent(path)
		if err != nil {
			return err
		}
		body, location, err := decodeEntry(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		id, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		var created time.Time
		if len(item.Name()) > len(filenameTime) && item.Name()[len(filenameTime)] == '-' {
			if parsed, err := time.Parse(filenameTime, item.Name()[:len(filenameTime)]); err == nil {
				created = parsed
			}
		}
		if created.IsZero() {
			info, err := item.Info()
			if err != nil {
				return err
			}
			created = info.ModTime()
		}
		entries = append(entries, Entry{
			ID: filepath.ToSlash(id), Notebook: notebook, Content: body, Location: location,
			CreatedAt: created, diskContent: raw, hasSnapshot: true,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read %s notebook: %w", notebook, err)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].ID > entries[j].ID
		}
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})
	return entries, nil
}

// Save creates an entry without a location when original is nil, or updates an
// entry while preserving its location. See SaveWithLocation for save semantics.
func (s *Store) Save(notebook string, original *Entry, content string) (Entry, error) {
	location := ""
	if original != nil {
		location = original.Location
	}
	return s.SaveWithLocation(notebook, original, content, location)
}

// SaveWithLocation creates or updates an entry and its optional location after
// checking that the full original file has not changed on disk. Passing an
// empty location removes it. Writes are staged, synced, and
// atomically published. Failures before publication leave the previous file
// intact; a directory-sync error explicitly reports that the file was written.
// In that case Save returns both the published Entry and the error. Callers must
// retain that Entry as their saved baseline to avoid duplicating it on retry.
// Every error before publication returns a zero Entry.
//
// Conflict detection protects against edits made since List. Like most text
// editors it cannot prevent an unrelated process writing in the brief interval
// between the comparison and rename. Git operations should run after saving.
func (s *Store) SaveWithLocation(notebook string, original *Entry, content, location string) (Entry, error) {
	if err := ValidateNotebook(notebook); err != nil {
		return Entry{}, err
	}
	if strings.TrimSpace(content) == "" {
		return Entry{}, ErrEmptyEntry
	}
	if len(content) > MaxEntryBytes {
		return Entry{}, fmt.Errorf("entry exceeds the %d MiB limit", MaxEntryBytes>>20)
	}
	if !utf8.ValidString(content) {
		return Entry{}, errors.New("entry must contain valid UTF-8 text")
	}
	location, err := NormalizeLocation(location)
	if err != nil {
		return Entry{}, err
	}
	raw, err := encodeEntry(content, location)
	if err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var entry Entry
	if original != nil {
		if original.Notebook != notebook {
			return Entry{}, errors.New("entry belongs to a different notebook")
		}
		if err := validateID(original.ID); err != nil {
			return Entry{}, err
		}
		entry = *original
	} else {
		now := time.Now().UTC()
		random := make([]byte, 8)
		if _, err := rand.Read(random); err != nil {
			return Entry{}, fmt.Errorf("create entry identifier: %w", err)
		}
		entry = Entry{
			Notebook:  notebook,
			CreatedAt: now,
			ID:        now.Format("2006/01/") + now.Format(filenameTime) + "-" + hex.EncodeToString(random) + ".md",
		}
	}
	target, err := s.safePath(filepath.Join(notebook, filepath.FromSlash(entry.ID)), original == nil)
	if err != nil {
		return Entry{}, err
	}
	if original != nil {
		existing, err := readContent(target)
		if errors.Is(err, fs.ErrNotExist) {
			return Entry{}, ErrConflict
		}
		if err != nil {
			return Entry{}, fmt.Errorf("check existing entry: %w", err)
		}
		baseline := original.diskContent
		if !original.hasSnapshot {
			// Keep compatibility with callers holding an ordinary Markdown entry
			// assembled before reading through this store.
			baseline, err = encodeEntry(original.Content, original.Location)
			if err != nil {
				return Entry{}, err
			}
		}
		if existing != baseline {
			return Entry{}, ErrConflict
		}
	}
	published, err := writeAtomic(target, raw, original == nil, syncDirectory)
	entry.Content = content
	entry.Location = location
	entry.diskContent, entry.hasSnapshot = raw, true
	if err != nil {
		if !published {
			entry = Entry{}
		}
		return entry, fmt.Errorf("save entry: %w", err)
	}
	return entry, nil
}

func validateID(id string) error {
	if id == "" || strings.Contains(id, "\\") || strings.HasPrefix(id, "/") || !strings.EqualFold(filepath.Ext(id), ".md") {
		return errors.New("invalid entry path")
	}
	for _, part := range strings.Split(id, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.Contains(part, ":") {
			return errors.New("invalid entry path")
		}
	}
	return nil
}

// safePath refuses symlinks below the root, including the destination itself.
// Missing parent directories are only created for a new entry.
func (s *Store) safePath(relative string, createParents bool) (string, error) {
	parts := strings.Split(relative, string(filepath.Separator))
	current := s.root
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			if createParents && i < len(parts)-1 {
				if err := os.Mkdir(current, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
					return "", fmt.Errorf("create notebook directory: %w", err)
				}
				// Recheck to reject a symlink created by another process.
				info, err = os.Lstat(current)
			} else {
				continue
			}
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("notes path contains a symlink: %s", current)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("notes path is not a directory: %s", current)
		}
		if i == len(parts)-1 && !info.IsDir() && !info.Mode().IsRegular() {
			return "", fmt.Errorf("notes path is not a regular file: %s", current)
		}
	}
	return current, nil
}

func readContent(path string) (string, error) {
	return readTextFile(path, MaxEntryBytes+maxMetadataBytes)
}

func readTextFile(path string, maxBytes int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxBytes {
		return "", fmt.Errorf("%s exceeds the %d byte file limit", filepath.Base(path), maxBytes)
	}
	if !utf8.Valid(body) {
		return "", fmt.Errorf("%s is not valid UTF-8 text", filepath.Base(path))
	}
	return string(body), nil
}

func writeAtomic(target, content string, create bool, syncDir func(string) error) (published bool, err error) {
	directory := filepath.Dir(target)
	f, err := os.CreateTemp(directory, ".todayidid-*")
	if err != nil {
		return false, err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err := io.WriteString(f, content); err != nil {
		f.Close()
		return false, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	if create {
		// A hard link publishes the complete file atomically without replacing
		// another entry, even in the exceedingly unlikely event of an ID clash.
		err = os.Link(temp, target)
	} else {
		err = os.Rename(temp, target)
	}
	if err != nil {
		return false, err
	}
	if err := syncDir(directory); err != nil {
		return true, fmt.Errorf("file was written, but syncing its directory failed: %w", err)
	}
	return true, nil
}

func syncDirectory(directory string) error {
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
