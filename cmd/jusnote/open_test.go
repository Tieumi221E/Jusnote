package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitLine(t *testing.T) {
	dir := t.TempDir()
	odd := filepath.Join(dir, "odd:7") // a name that ends like a line (not on Windows)
	os.WriteFile(odd, nil, 0o644)
	for in, want := range map[string]struct {
		f string
		n int
	}{
		`notes\a.md:12`: {`notes\a.md`, 12},
		`C:\n\a.md`:     {`C:\n\a.md`, 0},
		`C:\n\a.md:3`:   {`C:\n\a.md`, 3},
		`a.md`:          {`a.md`, 0},
		`a.md:x`:        {`a.md:x`, 0},
	} {
		if f, n := splitLine(in); f != want.f || n != want.n {
			t.Errorf("splitLine(%q) = %q, %d; want %q, %d", in, f, n, want.f, want.n)
		}
	}
	if _, err := os.Stat(odd); err == nil {
		if f, n := splitLine(odd); f != odd || n != 0 {
			t.Errorf("an existing file named like a line was split: %q %d", f, n)
		}
	}
}

func TestNotebookOf(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "nb", "a", "b")
	os.MkdirAll(deep, 0o755)
	os.MkdirAll(filepath.Join(root, "nb", ".jusnote"), 0o755)
	if got := notebookOf(deep); got != filepath.Join(root, "nb") {
		t.Fatalf("notebookOf = %s, want the folder with .jusnote", got)
	}
	plain := filepath.Join(root, "plain")
	os.MkdirAll(plain, 0o755)
	if got := notebookOf(plain); got != plain { // (the temp folder is in no repository)
		t.Fatalf("notebookOf(plain) = %s", got)
	}
}
