package notebook

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadList(t *testing.T) {
	nb, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clean, err := nb.Write("a/b.md", []byte("hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	if clean != "a/b.md" {
		t.Fatalf("clean = %q", clean)
	}
	data, err := nb.Read("a/b.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("read %q", data)
	}
	docs, err := nb.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Rel != "a/b.md" {
		t.Fatalf("list %+v", docs)
	}
}

func TestPathSafety(t *testing.T) {
	nb, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"../x.md", "..\\x.md", "a/../../x.md", "/abs.md", "", ".jusnote/x.md", ".jusnote"} {
		if _, err := nb.Write(rel, []byte("x")); err == nil {
			t.Errorf("Write(%q) should be refused", rel)
		}
	}
}

func TestBackupKeepsPrevious(t *testing.T) {
	nb, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nb.Write("a.md", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := nb.Write("a.md", []byte("two")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(nb.Vault(), "backup", "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "one" {
		t.Fatalf("backup = %q", got)
	}
	now, _ := nb.Read("a.md")
	if string(now) != "two" {
		t.Fatalf("current = %q", now)
	}
}

func TestLink(t *testing.T) {
	nb, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := nb.Link("a/b.md", 3), "jus://note/a/b.md?line=3"; got != want {
		t.Fatalf("Link = %q, want %q", got, want)
	}
}

func TestRecoverOnOpen(t *testing.T) {
	root := t.TempDir()
	nb, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	marker := ".jusnote-old"
	if _, err := nb.Write("a.md", []byte("keep")); err != nil {
		t.Fatal(err)
	}
	// Pretend a swap died after moving the original aside and losing the
	// new file: only the "-old" copy is left.
	if err := os.Rename(filepath.Join(root, "a.md"), filepath.Join(root, "a.md"+marker)); err != nil {
		t.Fatal(err)
	}
	nb2, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := nb2.Read("a.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("after recover = %q", data)
	}
}

func TestPathSafetyIgnoresCase(t *testing.T) {
	nb, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".JUSNOTE/x.md", ".Jusnote/types/a.yaml", ".git/config", ".GIT/hooks/x"} {
		if _, err := nb.Write(rel, []byte("x")); err == nil {
			t.Errorf("Write(%q) should be refused", rel)
		}
	}
}

// WriteIf refuses to replace content that changed since the writer read it.
func TestWriteIfDetectsOutsideEdit(t *testing.T) {
	nb, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nb.WriteIf("a.md", []byte("one"), ""); err != nil {
		t.Fatalf("create with empty base: %v", err)
	}
	if _, err := nb.WriteIf("a.md", []byte("again"), ""); err != ErrChanged {
		t.Fatalf("create over an existing file: got %v, want ErrChanged", err)
	}
	base, _ := nb.Version("a.md")
	if err := os.WriteFile(filepath.Join(nb.Root(), "a.md"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := nb.WriteIf("a.md", []byte("mine"), base); err != ErrChanged {
		t.Fatalf("got %v, want ErrChanged", err)
	}
	if got, _ := nb.Read("a.md"); string(got) != "outside" {
		t.Fatalf("outside edit lost: %q", got)
	}
	cur, _ := nb.Version("a.md")
	if _, err := nb.WriteIf("a.md", []byte("mine"), cur); err != nil {
		t.Fatalf("write at current version: %v", err)
	}
}

func TestRenameDeleteSearch(t *testing.T) {
	nb, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nb.Write("a.md", []byte("# Title\nfind ME here\n"))
	nb.Write("b.md", []byte("nothing\n"))
	nb.Write(".obsidian/c.md", []byte("find me\n")) // refused? no: only the vault is reserved
	if _, err := nb.Rename("a.md", "b.md"); err == nil {
		t.Fatal("rename over an existing note should be refused")
	}
	clean, err := nb.Rename("a.md", "dir/a2.md")
	if err != nil || clean != "dir/a2.md" {
		t.Fatalf("rename: %q %v", clean, err)
	}
	hits, err := nb.Search("find me", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Rel != "dir/a2.md" || hits[0].Line != 2 || hits[0].Text != "find ME here" {
		t.Fatalf("hits %+v (hidden folders must not be searched)", hits)
	}
	if err := nb.Delete("dir/a2.md"); err != nil {
		t.Fatal(err)
	}
	if nb.Exists("dir/a2.md") {
		t.Fatal("still there after Delete")
	}
	if got, err := os.ReadFile(filepath.Join(nb.Vault(), "backup", "dir", "a2.md")); err != nil || string(got) != "# Title\nfind ME here\n" {
		t.Fatalf("deleted note not kept in backup: %q %v", got, err)
	}
}
