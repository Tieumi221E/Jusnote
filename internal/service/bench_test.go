package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tieumi221E/Jusnote/internal/history"
)

// Benchmarks on synthetic notebooks of 100, 1 000 and 5 000 notes (about
// 2 KB each, one wiki link per note, all committed). tools/bench.ps1 runs
// them with the CLI timings and records the machine; results stay local
// (bench/). A run: go test ./internal/service -run - -bench . -benchmem

var sizes = []int{100, 1000, 5000}

var fixtures = map[int]*Service{}

func fixture(b *testing.B, n int) *Service {
	if s, ok := fixtures[n]; ok {
		return s
	}
	b.Helper()
	dir, err := os.MkdirTemp("", fmt.Sprintf("jusnote-bench-%d-", n))
	if err != nil {
		b.Fatal(err)
	}
	body := strings.Repeat("这是一段合成的笔记内容，用来测量。Synthetic text for timing, nothing real.\n", 20)
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		rel := fmt.Sprintf("folder-%02d/note-%05d.md", i%50, i)
		text := fmt.Sprintf("# Note %d\n\n%slink to [[note-%05d]]\n", i, body, (i*7+1)%n)
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(text), 0o644); err != nil {
			b.Fatal(err)
		}
		paths = append(paths, rel)
	}
	s, err := Open(dir, true)
	if err != nil {
		b.Fatal(err)
	}
	if _, _, err := s.Commit("fixture", history.Provenance{}, paths...); err != nil {
		b.Fatal(err)
	}
	fixtures[n] = s
	return s
}

func each(b *testing.B, fn func(b *testing.B, s *Service)) {
	for _, n := range sizes {
		b.Run(fmt.Sprintf("notes=%d", n), func(b *testing.B) {
			s := fixture(b, n)
			b.ResetTimer()
			fn(b, s)
		})
	}
}

func BenchmarkList(b *testing.B) {
	each(b, func(b *testing.B, s *Service) {
		for i := 0; i < b.N; i++ {
			if _, err := s.NB.List(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkSearch(b *testing.B) {
	each(b, func(b *testing.B, s *Service) {
		for i := 0; i < b.N; i++ {
			if _, err := s.Search("nothing real", 300); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkSearchMiss(b *testing.B) {
	each(b, func(b *testing.B, s *Service) {
		for i := 0; i < b.N; i++ {
			if _, err := s.Search("没有这个词", 300); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkStatusClean(b *testing.B) {
	each(b, func(b *testing.B, s *Service) {
		for i := 0; i < b.N; i++ {
			if _, err := s.Repo.Status(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// A save as the editor makes it: a versioned write, then a commit of that file.
func BenchmarkWriteCommit(b *testing.B) {
	each(b, func(b *testing.B, s *Service) {
		rel := "folder-00/note-00000.md"
		for i := 0; i < b.N; i++ {
			v, _ := s.NB.Version(rel)
			if _, err := s.NB.WriteIf(rel, []byte(fmt.Sprintf("# Note 0\n\nedit %d\n", i)), v); err != nil {
				b.Fatal(err)
			}
			if _, _, err := s.Commit("", history.Provenance{}, rel); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkLinksAndBacklinks(b *testing.B) {
	each(b, func(b *testing.B, s *Service) {
		for i := 0; i < b.N; i++ {
			if _, err := s.LinksOf("folder-01/note-00001.md", nil); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkFileLog(b *testing.B) {
	each(b, func(b *testing.B, s *Service) {
		for i := 0; i < b.N; i++ {
			if _, err := s.Log("folder-02/note-00002.md", 50); err != nil {
				b.Fatal(err)
			}
		}
	})
}
