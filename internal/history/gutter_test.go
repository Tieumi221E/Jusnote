package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGutterNewFile(t *testing.T) {
	root := t.TempDir()
	repo, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := repo.Gutter("a.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("a file not in HEAD should have no markers, got %+v", changes)
	}
}

func TestGutterKinds(t *testing.T) {
	root := t.TempDir()
	repo, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "a.md")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.CommitAll("base"); err != nil {
		t.Fatal(err)
	}

	// Clean tree: no changes.
	if changes, err := repo.Gutter("a.md"); err != nil || len(changes) != 0 {
		t.Fatalf("clean gutter = %+v err=%v", changes, err)
	}

	// Modify line 2, add line 4.
	if err := os.WriteFile(path, []byte("a\nB\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := repo.Gutter("a.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes %+v", changes)
	}
	if changes[0] != (Change{Line: 2, Kind: "modify"}) || changes[1] != (Change{Line: 4, Kind: "add"}) {
		t.Fatalf("changes %+v", changes)
	}

	// Delete line 2.
	if err := os.WriteFile(path, []byte("a\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err = repo.Gutter("a.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != "delete" {
		t.Fatalf("delete changes %+v", changes)
	}
}
