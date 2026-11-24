package journal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// benchmarkNotebook models roughly three years of daily notes. Fixture writes
// are excluded from timings; Save benchmarks include the real fsync operations.
func benchmarkNotebook(b *testing.B) (*Store, string, Entry) {
	b.Helper()
	s, err := New(filepath.Join(b.TempDir(), "notes"))
	if err != nil {
		b.Fatal(err)
	}
	body := "# Today I did\n\n" + strings.Repeat("- Reviewed a change, wrote down the tradeoffs, and made the next step smaller.\n", 10)
	start := time.Date(2024, 1, 1, 9, 30, 0, 0, time.UTC)
	var last Entry
	for i := 0; i < 1000; i++ {
		created := start.AddDate(0, 0, i)
		id := created.Format("2006/01/") + created.Format(filenameTime) + fmt.Sprintf("-%016x.md", i)
		path := filepath.Join(s.Root(), "work", filepath.FromSlash(id))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			b.Fatal(err)
		}
		last = Entry{ID: id, Notebook: "work", Content: body, CreatedAt: created}
	}
	return s, body, last
}

func BenchmarkList(b *testing.B) {
	s, body, _ := benchmarkNotebook(b)
	b.ReportAllocs()
	b.SetBytes(int64(1000 * len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entries, err := s.List("work")
		if err != nil {
			b.Fatal(err)
		}
		if len(entries) != 1000 {
			b.Fatalf("listed %d entries; want 1000", len(entries))
		}
	}
	b.StopTimer()
	b.ReportMetric(1000, "entries/op")
}

func BenchmarkSave(b *testing.B) {
	b.Run("Create", func(b *testing.B) {
		s, body, _ := benchmarkNotebook(b)
		b.ReportAllocs()
		b.SetBytes(int64(len(body)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := s.Save("work", nil, body); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Edit", func(b *testing.B) {
		s, body, entry := benchmarkNotebook(b)
		versions := [2]string{body + "\nA\n", body + "\nB\n"}
		b.ReportAllocs()
		b.SetBytes(int64(len(versions[0])))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var err error
			entry, err = s.Save("work", &entry, versions[i%2])
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
