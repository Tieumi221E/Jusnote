package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommitAndLog(t *testing.T) {
	root := t.TempDir()
	repo, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	if !IsRepo(root) {
		t.Fatal("IsRepo false after Init")
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := repo.Status()
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != "a.md" {
		t.Fatalf("status %v", changed)
	}

	hash, ok, err := repo.CommitAll("first")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || hash == "" {
		t.Fatalf("commit ok=%v hash=%q", ok, hash)
	}

	if _, ok, err := repo.CommitAll("again"); err != nil || ok {
		t.Fatalf("clean commit should be a no-op: ok=%v err=%v", ok, err)
	}

	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := repo.CommitAll("second"); err != nil || !ok {
		t.Fatalf("second commit ok=%v err=%v", ok, err)
	}

	commits, err := repo.Log(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].Message != "second" || commits[1].Message != "first" {
		t.Fatalf("log %+v", commits)
	}
	if commits[0].Author == "" || commits[0].When.IsZero() {
		t.Fatalf("commit metadata missing: %+v", commits[0])
	}

	limited, err := repo.Log(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].Message != "second" {
		t.Fatalf("limited log %+v", limited)
	}
}

// CommitPaths commits the named files only; an unrelated edit (say, an
// agent's) stays in the working tree.
func TestCommitPathsLeavesOthers(t *testing.T) {
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
	write("a.md", "a\n")
	write("b.md", "b\n")
	if _, ok, err := repo.CommitPaths("a only", "a.md"); err != nil || !ok {
		t.Fatalf("commit a: ok=%v err=%v", ok, err)
	}
	changed, _ := repo.Status()
	if len(changed) != 1 || changed[0] != "b.md" {
		t.Fatalf("status after committing a: %v, want [b.md]", changed)
	}
	if !repo.Tracked("a.md") || repo.Tracked("b.md") {
		t.Fatal("Tracked: want a.md yes, b.md no")
	}
	if _, ok, err := repo.CommitPaths("again", "a.md"); err != nil || ok {
		t.Fatalf("unchanged path should not commit: ok=%v err=%v", ok, err)
	}

	// A removed tracked file is recorded as removed.
	if err := os.Remove(filepath.Join(root, "a.md")); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := repo.CommitPaths("remove a", "a.md"); err != nil || !ok {
		t.Fatalf("commit removal: ok=%v err=%v", ok, err)
	}
	if repo.Tracked("a.md") {
		t.Fatal("a.md still in HEAD after its removal was committed")
	}
	// A never-committed file that is gone is nothing to record.
	if _, ok, err := repo.CommitPaths("ghost", "ghost.md"); err != nil || ok {
		t.Fatalf("ghost: ok=%v err=%v", ok, err)
	}
}

func TestLogOnEmptyIsEmpty(t *testing.T) {
	repo, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	commits, err := repo.Log(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 0 {
		t.Fatalf("expected no commits, got %+v", commits)
	}
}
