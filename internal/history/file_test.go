package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffHunks(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n"
	new := "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\n"
	hunks := Diff(old, new)
	if len(hunks) != 2 {
		t.Fatalf("got %d hunks, want 2 (changes 10 lines apart): %+v", len(hunks), hunks)
	}
	h := hunks[0]
	if h.OldStart != 1 || h.NewStart != 1 || h.OldLines != 5 || h.NewLines != 5 {
		t.Fatalf("first hunk header %+v, want -1,5 +1,5", h)
	}
	var ops []string
	for _, l := range h.Lines {
		ops = append(ops, l.Op+l.Text)
	}
	if got, want := strings.Join(ops, "|"), " a|-b|+B| c| d| e"; got != want {
		t.Fatalf("first hunk %q, want %q", got, want)
	}
	h = hunks[1]
	if h.OldStart != 10 || h.NewStart != 10 || h.OldLines != 3 || h.NewLines != 4 {
		t.Fatalf("second hunk header %+v, want -10,3 +10,4", h)
	}
	if last := h.Lines[len(h.Lines)-1]; last.Op != "+" || last.Text != "m" {
		t.Fatalf("second hunk ends with %+v, want +m", last)
	}
	if len(Diff("same\n", "same\n")) != 0 {
		t.Fatal("equal texts should have no hunks")
	}
	if hs := Diff("", "x\ny\n"); len(hs) != 1 || hs[0].OldLines != 0 || hs[0].NewLines != 2 {
		t.Fatalf("new file: %+v", hs)
	}
}

// Changes close together share one hunk.
func TestDiffMergesNearbyChanges(t *testing.T) {
	old := "1\n2\n3\n4\n5\n6\n7\n8\n"
	new := "1\nX\n3\n4\n5\nY\n7\n8\n"
	if hs := Diff(old, new); len(hs) != 1 {
		t.Fatalf("got %d hunks, want 1", len(hs))
	}
}

func TestProvenanceRoundTrip(t *testing.T) {
	p := Provenance{Author: "agent", Model: "anthropic/claude", Run: "sess 42\nx"}
	msg := p.Message("update a.md")
	subject, got := parseMessage(msg)
	if subject != "update a.md" || got.Author != "agent" || got.Model != "anthropic/claude" || got.Run != "sess 42 x" {
		t.Fatalf("subject %q, provenance %+v", subject, got)
	}
	if (Provenance{}).Message("plain") != "plain" {
		t.Fatal("empty provenance must not change the message")
	}
	if _, got := parseMessage("a: b\n\nnot a trailer"); got != (Provenance{}) {
		t.Fatalf("spurious provenance %+v", got)
	}
}

func TestFileLogAndContentAt(t *testing.T) {
	root := t.TempDir()
	repo, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.md", "one\n")
	repo.CommitPaths("a1", "a.md")
	write("b.md", "other\n")
	repo.CommitPaths("b1", "b.md")
	write("a.md", "two\n")
	repo.CommitPaths(Provenance{Author: "agent"}.Message("a2"), "a.md")

	log, err := repo.FileLog("a.md", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].Message != "a2" || log[1].Message != "a1" {
		t.Fatalf("file log %+v, want a2, a1", log)
	}
	if log[0].Source.Author != "agent" || log[1].Source.Author != "" {
		t.Fatalf("sources %+v / %+v", log[0].Source, log[1].Source)
	}
	text, ok, err := repo.ContentAt(log[1].Hash[:8], "a.md")
	if err != nil || !ok || text != "one\n" {
		t.Fatalf("content at a1: %q %v %v", text, ok, err)
	}
	if _, ok, _ := repo.ContentAt(log[1].Hash, "b.md"); ok {
		t.Fatal("b.md did not exist at a1")
	}
	write("a.md", "three\n")
	hs, _, err := repo.WorkingDiff("a.md")
	if err != nil || len(hs) != 1 || hs[0].Lines[0].Text != "two" || hs[0].Lines[1].Text != "three" {
		t.Fatalf("working diff %+v %v", hs, err)
	}
}

// A file saved with CRLF where it had LF is not "every line changed".
func TestDiffIgnoresLineEndings(t *testing.T) {
	old := "a\nb\nc\n"
	crlf := "a\r\nb\r\nc\r\n"
	if hs := Diff(old, crlf); len(hs) != 0 {
		t.Fatalf("CRLF-only change gave hunks %+v", hs)
	}
	if !EOLOnly(old, crlf) || EOLOnly(old, old) || EOLOnly(old, "a\r\nB\r\nc\r\n") {
		t.Fatal("EOLOnly wrong")
	}
	hs := Diff(old, "a\r\nB\r\nc\r\n")
	if len(hs) != 1 || hs[0].OldLines != 3 || hs[0].Lines[1].Text != "b" || hs[0].Lines[2].Text != "B" {
		t.Fatalf("one real change among CRLF lines: %+v", hs)
	}
	root := t.TempDir()
	repo, _ := Init(root)
	os.WriteFile(filepath.Join(root, "a.md"), []byte(old), 0o644)
	repo.CommitPaths("base", "a.md")
	os.WriteFile(filepath.Join(root, "a.md"), []byte("a\r\nB\r\nc\r\n"), 0o644)
	if g, _ := repo.Gutter("a.md"); len(g) != 1 || g[0] != (Change{Line: 2, Kind: "modify"}) {
		t.Fatalf("gutter over CRLF: %+v, want only line 2 modified", g)
	}
}