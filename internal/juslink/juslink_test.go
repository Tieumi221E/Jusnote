package juslink

import "testing"

func TestRoundTrip(t *testing.T) {
	cases := []string{
		"jus://note/2026-09.md?line=42",
		"jus://note/%E6%97%A5%E8%AE%B0/2026-09.md",
		"jus://play/so46814148?t=12%3A34",
		"jus://read/abc?page=120",
	}
	for _, in := range cases {
		l, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", in, err)
		}
		if got := l.String(); got != in {
			t.Errorf("round trip %q -> %q", in, got)
		}
	}
}

func TestParseBare(t *testing.T) {
	l, err := Parse("note:2026-09.md")
	if err != nil {
		t.Fatal(err)
	}
	if l.Kind != Note || l.Target != "2026-09.md" {
		t.Fatalf("got %+v", l)
	}
}

func TestNoteAndLine(t *testing.T) {
	s := NoteLink("sub/日记.md", 7)
	l, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	if l.Target != "sub/日记.md" || l.Line() != 7 {
		t.Fatalf("got target=%q line=%d", l.Target, l.Line())
	}
}

func TestRejectsForeign(t *testing.T) {
	for _, in := range []string{"https://example.com", "", "jus://"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) should fail", in)
		}
	}
}
