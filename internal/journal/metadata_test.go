package journal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeLocation(t *testing.T) {
	for _, location := range []string{"", "Home", "Café / 東京 📍", strings.Repeat("界", MaxLocationRunes)} {
		got, err := NormalizeLocation(" \u2003" + location + " \u2003")
		if err != nil || got != location {
			t.Errorf("NormalizeLocation(%q) = %q, %v", location, got, err)
		}
	}
	for _, location := range []string{
		"Two\nlines", "Trailing newline\n", "\tTabbed", "Carriage\rreturn", "ESC\x1b[2J",
		"Null\x00byte", "Delete\x7f", "Next\u0085line", "Line\u2028separator", "Paragraph\u2029separator",
		string([]byte{0xff}), strings.Repeat("界", MaxLocationRunes+1),
	} {
		if _, err := NormalizeLocation(location); err == nil {
			t.Errorf("accepted invalid location %q", location)
		}
	}
}

func TestLocationLifecyclePreservesBody(t *testing.T) {
	s := newTestStore(t)
	body := "---\ntitle: A real journal\n---\n\n# Today\n\n  Spaces stay.\n東京 📍\n\n"
	entry, err := s.SaveWithLocation("personal", nil, body, "  Café / 東京  ")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Location != "Café / 東京" || entry.Content != body {
		t.Fatalf("saved entry = %+v", entry)
	}
	path := filepath.Join(s.Root(), "personal", entry.ID)
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != `<!-- todayidid: {"version":1,"location":"Café / 東京"} -->`+"\n\n"+body {
		t.Fatalf("saved file = %q, %v", raw, err)
	}
	listed, err := s.List("personal")
	if err != nil || len(listed) != 1 || listed[0] != entry {
		t.Fatalf("location roundtrip = %+v, %v", listed, err)
	}
	editedBody := body + "Another thought.\n"
	entry, err = s.Save("personal", &listed[0], editedBody)
	if err != nil || entry.Location != "Café / 東京" || entry.Content != editedBody {
		t.Fatalf("Save did not preserve location: %+v, %v", entry, err)
	}
	entry, err = s.SaveWithLocation("personal", &entry, editedBody, "Office")
	if err != nil || entry.Location != "Office" {
		t.Fatalf("location update = %+v, %v", entry, err)
	}
	entry, err = s.SaveWithLocation("personal", &entry, editedBody, "")
	if err != nil || entry.Location != "" {
		t.Fatalf("location removal = %+v, %v", entry, err)
	}
	raw, err = os.ReadFile(path)
	if err != nil || string(raw) != editedBody {
		t.Fatalf("removing location did not restore ordinary Markdown: %q, %v", raw, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("metadata file permissions = %v, %v", info, err)
	}
}

func TestMetadataCommentCannotBeClosedByLocation(t *testing.T) {
	s := newTestStore(t)
	location := "Home --> <script>&stuff</script>"
	entry, err := s.SaveWithLocation("personal", nil, "A thought", location)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Root(), "personal", entry.ID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "-->") != 1 || strings.Contains(string(raw), "<script>") {
		t.Fatalf("location escaped its comment: %q", raw)
	}
	entries, err := s.List("personal")
	if err != nil || len(entries) != 1 || entries[0].Location != location {
		t.Fatalf("escaped location roundtrip = %+v, %v", entries, err)
	}
}

func TestReservedPrefixBodyRoundTripsWithoutInterpretation(t *testing.T) {
	for _, body := range []string{
		`<!-- todayidid: {"version":1,"location":"This is body text"} -->` + "\n\nBody\n",
		"<!-- todayidid: this is not metadata, just literal source",
		`<!-- todayidid: {"version":999} -->` + "\n\nFuture format example\n",
	} {
		s := newTestStore(t)
		entry, err := s.Save("work", nil, body)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := s.List("work")
		if err != nil || len(entries) != 1 || entries[0].Content != body || entries[0].Location != "" {
			t.Fatalf("reserved-prefix roundtrip = %+v, %v", entries, err)
		}
		if _, err := s.Save("work", &entry, body+"\nOne more line"); err != nil {
			t.Fatalf("reserved-prefix edit: %v", err)
		}
	}
}

func TestMetadataOnlyExternalChangesCauseConflict(t *testing.T) {
	for _, replacement := range []string{
		`<!-- todayidid: {"version":1,"location":"Office"} -->` + "\n\nSame body",
		`<!-- todayidid: { "version": 1, "location": "Home" } -->` + "\n\nSame body",
	} {
		s := newTestStore(t)
		entry, err := s.SaveWithLocation("work", nil, "Same body", "Home")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(s.Root(), "work", entry.ID)
		if err := os.WriteFile(path, []byte(replacement), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveWithLocation("work", &entry, "My changed body", "Elsewhere"); !errors.Is(err, ErrConflict) {
			t.Fatalf("metadata change did not cause conflict: %v", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != replacement {
			t.Fatalf("external metadata overwritten: %q, %v", raw, err)
		}
	}
}

func TestMetadataReadsCRLFAndKeepsExactConflictSnapshot(t *testing.T) {
	s := newTestStore(t)
	entry, err := s.Save("work", nil, "Old body")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Root(), "work", entry.ID)
	body := "First\r\nSecond\r\n\r\n"
	raw := `<!-- todayidid: { "location": " Home ", "version": 1 } -->` + "\r\n\r\n" + body
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := s.List("work")
	if err != nil || len(entries) != 1 || entries[0].Content != body || entries[0].Location != "Home" {
		t.Fatalf("CRLF metadata = %+v, %v", entries, err)
	}
	if _, err := s.Save("work", &entries[0], body+"Changed"); err != nil {
		t.Fatalf("unchanged noncanonical metadata caused conflict: %v", err)
	}
}

func TestMalformedOrUnsupportedMetadataIsNotSilentlyRemoved(t *testing.T) {
	for _, raw := range []string{
		`<!-- todayidid: {oops} -->` + "\n\nBody",
		`<!-- todayidid: {"version":2,"location":"Home"} -->` + "\n\nBody",
		`<!-- todayidid: {"location":"Home"} -->` + "\n\nBody",
		`<!-- todayidid: {"version":1,"future":"data"} -->` + "\n\nBody",
		`<!-- todayidid: {"version":1} {} -->` + "\n\nBody",
		`<!-- todayidid: {"version":1,"location":"Two\nlines"} -->` + "\n\nBody",
		`<!-- todayidid: {"version":1} -->` + "\nMissing blank separator",
		`<!-- todayidid: {"version":1} -->`,
	} {
		s := newTestStore(t)
		entry, err := s.Save("work", nil, "Original")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(s.Root(), "work", entry.ID)
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.List("work"); err == nil {
			t.Errorf("accepted malformed metadata %q", raw)
		}
		if disk, err := os.ReadFile(path); err != nil || string(disk) != raw {
			t.Fatalf("malformed metadata was changed: %q, %v", disk, err)
		}
	}
}

func TestMetadataSizeBudgetKeepsFullBodyCapacity(t *testing.T) {
	s := newTestStore(t)
	body := strings.Repeat("x", MaxEntryBytes)
	entry, err := s.SaveWithLocation("work", nil, body, strings.Repeat("<", MaxLocationRunes))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := s.List("work")
	if err != nil || len(entries) != 1 || entries[0] != entry {
		t.Fatalf("maximum-size metadata entry failed to roundtrip: %v", err)
	}
	if _, err := s.SaveWithLocation("work", &entry, body+"x", "Home"); err == nil {
		t.Fatal("oversized body was accepted with metadata")
	}
}

func TestInvalidLocationDoesNotCreateNotebook(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SaveWithLocation("new", nil, "Body", "Two\nlines"); err == nil {
		t.Fatal("invalid location accepted")
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "new")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid location created a notebook: %v", err)
	}
}
