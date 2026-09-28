package notebook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesBesideTheNotes(t *testing.T) {
	root := t.TempDir()
	nb, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	put := func(rel string, b []byte) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, b, 0o644)
	}
	put("a.md", []byte("# A\n"))
	put("config.yaml", []byte("key: value\n"))
	put("tools/run.sh", []byte("#!/bin/sh\necho 雨\n"))
	put("photo.png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"))
	put("legacy.txt", []byte{0xbd, 0xf1, 0xcc, 0xec, '\n'}) // GBK "今天"
	put("big.log", []byte(strings.Repeat("x", MaxEditable+1)))
	put(".gitignore", []byte("build/\n*.tmp\n"))
	put("build/out.txt", []byte("generated\n"))
	put("scratch.tmp", []byte("scratch\n"))
	put("node_modules/x/index.js", []byte("x\n"))

	fs, err := nb.Files()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]File{}
	for _, f := range fs {
		got[f.Rel] = f
	}
	for _, want := range []string{"a.md", "config.yaml", "tools/run.sh", "legacy.txt", "big.log"} {
		if _, ok := got[want]; !ok {
			t.Errorf("%s missing (have %v)", want, keys(got))
		}
	}
	for _, not := range []string{"photo.png", "build/out.txt", "scratch.tmp", "node_modules/x/index.js", ".gitignore"} {
		if _, ok := got[not]; ok {
			t.Errorf("%s listed", not)
		}
	}
	if !got["a.md"].Note || got["config.yaml"].Note {
		t.Error("notes are the Markdown files")
	}
	if got["legacy.txt"].ReadOnly != "encoding" || got["big.log"].ReadOnly != "size" || got["config.yaml"].ReadOnly != "" {
		t.Errorf("read-only: legacy %q big %q config %q", got["legacy.txt"].ReadOnly, got["big.log"].ReadOnly, got["config.yaml"].ReadOnly)
	}
	// List is still the notes only (links, search and record types use it).
	docs, _ := nb.List()
	if len(docs) != 1 || docs[0].Rel != "a.md" {
		t.Errorf("List: %+v", docs)
	}
	// A file that turns binary is left out at the next listing.
	put("config.yaml", []byte("k\x00v"))
	fs, _ = nb.Files()
	for _, f := range fs {
		if f.Rel == "config.yaml" {
			t.Error("a file that became binary is still listed")
		}
	}
}

func keys(m map[string]File) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
