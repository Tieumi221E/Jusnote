package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tieumi221E/Jusnote/internal/history"
)

func open(t *testing.T) *Service {
	t.Helper()
	s, err := Open(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func put(t *testing.T, s *Service, rel, text string) {
	t.Helper()
	if _, err := s.NB.Write(rel, []byte(text)); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, s *Service, rel string) string {
	t.Helper()
	b, err := s.NB.Read(rel)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRenameUpdatesLinks(t *testing.T) {
	s := open(t)
	put(t, s, "notes/ideas.md", "# Ideas\nsee [deep](../a/deep.md)\n")
	put(t, s, "a/deep.md", "back to [[ideas]]\n")
	put(t, s, "README.md", "start: [[notes/ideas]] and [i](notes/ideas.md)\n")
	put(t, s, "other.md", "no links here\n")

	r, err := s.Rename("notes/ideas.md", "archive/idea list.md", true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != "archive/idea list.md" || strings.Join(r.Updated, ",") != "README.md,a/deep.md" {
		t.Fatalf("renamed %+v", r)
	}
	if got := read(t, s, "README.md"); got != "start: [[archive/idea list]] and [i](archive/idea%20list.md)\n" {
		t.Fatalf("README: %q", got)
	}
	if got := read(t, s, "a/deep.md"); got != "back to [[idea list]]\n" {
		t.Fatalf("deep: %q", got)
	}
	// The moved note's own relative link still reaches a/deep.md.
	if got := read(t, s, "archive/idea list.md"); got != "# Ideas\nsee [deep](../a/deep.md)\n" {
		t.Fatalf("moved note: %q", got)
	}
	if got := read(t, s, "other.md"); got != "no links here\n" {
		t.Fatalf("untouched note changed: %q", got)
	}
}

// An agent's uncommitted write keeps its provenance until it is accepted;
// a later edit by someone else drops it.
func TestRecordedProvenanceReachesTheCommit(t *testing.T) {
	s := open(t)
	put(t, s, "a.md", "by agent\n")
	agent := history.Provenance{Author: "agent", Model: "openai/codex", Run: "r-1"}
	if err := s.Record("a.md", agent); err != nil {
		t.Fatal(err)
	}
	cs, _ := s.Changes()
	if len(cs) != 1 || cs[0].Kind != "new" || cs[0].Source != agent {
		t.Fatalf("changes %+v", cs)
	}
	if _, ok, err := s.Commit("", history.Provenance{}, "a.md"); err != nil || !ok {
		t.Fatalf("accept: %v %v", ok, err)
	}
	log, _ := s.Log("a.md", 1)
	if log[0].Source != agent {
		t.Fatalf("commit source %+v, want %+v", log[0].Source, agent)
	}
	if _, ok := s.Recorded("a.md"); ok {
		t.Fatal("provenance kept after its commit")
	}

	put(t, s, "b.md", "agent\n")
	s.Record("b.md", agent)
	put(t, s, "b.md", "then a person\n")
	s.Commit("", history.Provenance{}, "b.md")
	if log, _ := s.Log("b.md", 1); log[0].Source != (history.Provenance{}) {
		t.Fatalf("stale provenance used: %+v", log[0].Source)
	}
}

func TestDiscardAndRestore(t *testing.T) {
	s := open(t)
	put(t, s, "a.md", "v1\n")
	put(t, s, "gone.md", "keep me\n")
	s.Commit("base", history.Provenance{}, "a.md", "gone.md")
	put(t, s, "a.md", "v2\n")
	h2, _, _ := s.Commit("second", history.Provenance{}, "a.md")

	put(t, s, "a.md", "unwanted\n")
	put(t, s, "new.md", "unwanted too\n")
	os.Remove(filepath.Join(s.NB.Root(), "gone.md"))
	cs, _ := s.Changes()
	kinds := map[string]string{}
	for _, c := range cs {
		kinds[c.Path] = c.Kind
	}
	if kinds["a.md"] != "modified" || kinds["new.md"] != "new" || kinds["gone.md"] != "deleted" {
		t.Fatalf("kinds %v", kinds)
	}
	if d, _ := s.Diff("a.md"); len(d.Hunks) != 1 {
		t.Fatalf("diff %+v", d)
	}
	for _, p := range []string{"a.md", "new.md", "gone.md"} {
		if err := s.Discard(p); err != nil {
			t.Fatalf("discard %s: %v", p, err)
		}
	}
	if cs, _ := s.Changes(); len(cs) != 0 {
		t.Fatalf("after discard: %+v", cs)
	}
	if read(t, s, "a.md") != "v2\n" || read(t, s, "gone.md") != "keep me\n" || s.NB.Exists("new.md") {
		t.Fatal("discard did not put the files back as at HEAD")
	}
	if b, err := os.ReadFile(filepath.Join(s.NB.Vault(), "backup", "new.md")); err != nil || string(b) != "unwanted too\n" {
		t.Fatalf("discarded new file not in backup: %q %v", b, err)
	}

	log, _ := s.Log("a.md", 0)
	if len(log) != 2 || log[0].Hash != h2 {
		t.Fatalf("log %+v", log)
	}
	if _, err := s.Restore(log[1].Hash, "a.md"); err != nil {
		t.Fatal(err)
	}
	if read(t, s, "a.md") != "v1\n" {
		t.Fatal("restore did not bring v1 back")
	}
	if _, err := s.Show(log[1].Hash, "new.md"); err == nil {
		t.Fatal("show of a file absent from the commit should fail")
	}
}

func TestAttach(t *testing.T) {
	s := open(t)
	put(t, s, "notes/trip.md", "# Trip\n")
	f1, l1, err := s.Attach("notes/trip.md", "photo 1.png", []byte("png"))
	if err != nil {
		t.Fatal(err)
	}
	f2, l2, _ := s.Attach("notes/trip.md", "photo 1.png", []byte("png2"))
	if f1 != "notes/attachments/photo 1.png" || l1 != "attachments/photo%201.png" || f2 != "notes/attachments/photo 1-2.png" || l2 != "attachments/photo%201-2.png" {
		t.Fatalf("attach: %q %q / %q %q", f1, l1, f2, l2)
	}
	if f, _, _ := s.Attach("top.md", `..\evil:name?.png`, []byte("x")); f != "attachments/evil-name-.png" {
		t.Fatalf("unsafe name: %q", f)
	}
}

func TestNoRepoCLI(t *testing.T) {
	s, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if s.Repo != nil {
		t.Fatal("the CLI must not create a repository")
	}
	if _, err := s.Changes(); err != ErrNoRepo {
		t.Fatalf("got %v, want ErrNoRepo", err)
	}
}
