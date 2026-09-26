package merge

import "testing"

func TestDifferentLinesMerge(t *testing.T) {
	r := Bytes([]byte("a\nb\nc\n"), []byte("A\nb\nc\n"), []byte("a\nb\nC\n"))
	if len(r.Conflicts) != 0 {
		t.Fatalf("conflicts: %+v", r.Conflicts)
	}
	if got, want := r.String(), "A\nb\nC\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSameLineConflicts(t *testing.T) {
	r := Bytes([]byte("a\nb\n"), []byte("X\nb\n"), []byte("Y\nb\n"))
	if len(r.Conflicts) != 1 {
		t.Fatalf("want 1 conflict, got %+v", r.Conflicts)
	}
	if r.Conflicts[0].Ours[0] != "X\n" || r.Conflicts[0].Theirs[0] != "Y\n" {
		t.Fatalf("conflict sides wrong: %+v", r.Conflicts[0])
	}
}

func TestSameChangeMerges(t *testing.T) {
	r := Bytes([]byte("a\n"), []byte("b\n"), []byte("b\n"))
	if len(r.Conflicts) != 0 || r.String() != "b\n" {
		t.Fatalf("got %+v %q", r.Conflicts, r.String())
	}
}

func TestInsertAndAppend(t *testing.T) {
	// Ours inserts a line in the middle; theirs appends at the end.
	r := Bytes([]byte("a\nc\n"), []byte("a\nb\nc\n"), []byte("a\nc\nd\n"))
	if len(r.Conflicts) != 0 {
		t.Fatalf("conflicts: %+v", r.Conflicts)
	}
	if got, want := r.String(), "a\nb\nc\nd\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestOursOnlyAndTheirsOnly(t *testing.T) {
	r := Bytes([]byte("a\nb\nc\n"), []byte("a\nB\nc\n"), []byte("a\nb\nc\n"))
	if len(r.Conflicts) != 0 || r.String() != "a\nB\nc\n" {
		t.Fatalf("ours-only got %+v %q", r.Conflicts, r.String())
	}
	r = Bytes([]byte("a\nb\nc\n"), []byte("a\nb\nc\n"), []byte("a\nb\nC\n"))
	if len(r.Conflicts) != 0 || r.String() != "a\nb\nC\n" {
		t.Fatalf("theirs-only got %+v %q", r.Conflicts, r.String())
	}
}

func TestAddsAtDifferentPlaces(t *testing.T) {
	base := "# Log\n\n## 2026-01-01\n- a\n- b\n\n## 2026-01-02\n- c\n"
	ours := "# Log\n\n## 2026-01-01\n- a\n- b\n- ours\n\n## 2026-01-02\n- c\n"
	theirs := "# Log\n\n## 2026-01-01\n- a\n- b\n\n## 2026-01-02\n- c\n- theirs\n"
	r := Bytes([]byte(base), []byte(ours), []byte(theirs))
	if len(r.Conflicts) != 0 {
		t.Fatalf("conflicts in different-day edits: %+v", r.Conflicts)
	}
	got := r.String()
	if !contains(got, "- ours\n") || !contains(got, "- theirs\n") {
		t.Fatalf("both additions should survive:\n%s", got)
	}
}

func TestNoTrailingNewline(t *testing.T) {
	r := Bytes([]byte("a\nb"), []byte("a\nB"), []byte("a\nb"))
	if r.String() != "a\nB" {
		t.Fatalf("got %q", r.String())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
