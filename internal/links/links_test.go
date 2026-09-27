package links

import (
	"strings"
	"testing"
)

var notes = []string{"README.md", "notes/ideas.md", "notes/读书.md", "logs/ideas.md", "a/b/deep.md", "My Note.md"}

func TestResolveWiki(t *testing.T) {
	ix := NewIndex(notes)
	cases := []struct{ target, from, want string }{
		{"ideas", "notes/x.md", "notes/ideas.md"},        // same folder wins
		{"ideas", "logs/y.md", "logs/ideas.md"},          // same folder wins
		{"ideas", "README.md", "logs/ideas.md"},          // shortest path, then name order
		{"IDEAS", "notes/x.md", "notes/ideas.md"},        // case-insensitive
		{"读书", "README.md", "notes/读书.md"},               // CJK names
		{"notes/ideas", "a/b/deep.md", "notes/ideas.md"}, // a path
		{"a/b/deep.md", "README.md", "a/b/deep.md"},
		{"missing", "README.md", ""},
		{"My Note", "README.md", "My Note.md"},
	}
	for _, c := range cases {
		if got := ix.ResolveWiki(c.target, c.from); got != c.want {
			t.Errorf("ResolveWiki(%q from %q) = %q, want %q", c.target, c.from, got, c.want)
		}
	}
}

func TestParseSkipsCode(t *testing.T) {
	ix := NewIndex(notes)
	text := "see [[ideas|my ideas]] and [deep](../a/b/deep.md#top)\n`[[ideas]]` is code\n```\n[[ideas]]\n```\n[[missing]] [[读书#评价]]\n[web](https://x.org/a.md)"
	got := ix.Parse(text, "notes/x.md")
	var b []string
	for _, l := range got {
		b = append(b, l.Kind+":"+l.Raw+"->"+l.Target)
	}
	want := "wiki:ideas->notes/ideas.md|md:../a/b/deep.md->a/b/deep.md|wiki:missing->|wiki:读书->notes/读书.md"
	if strings.Join(b, "|") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(b, "|"), want)
	}
	if got[3].Line != 6 {
		t.Fatalf("line of [[读书]] = %d, want 6", got[3].Line)
	}
}

func TestBacklinks(t *testing.T) {
	ix := NewIndex(notes)
	texts := map[string]string{
		"README.md":      "start at [[notes/ideas]]\n",
		"notes/读书.md":    "x\n- also [[ideas]]\n",
		"logs/ideas.md":  "[[ideas]] is myself\n",
		"notes/ideas.md": "[[ideas]] self link is not a backlink\n",
	}
	bl := ix.Backlinks("notes/ideas.md", texts)
	if len(bl) != 2 || bl[0].From != "README.md" || bl[1].From != "notes/读书.md" || bl[1].Line != 2 || bl[1].Text != "- also [[ideas]]" {
		t.Fatalf("backlinks %+v", bl)
	}
}

func TestRewriteOnRename(t *testing.T) {
	before := NewIndex(notes)
	moved := []string{"README.md", "archive/old ideas.md", "notes/读书.md", "logs/ideas.md", "a/b/deep.md", "My Note.md"}
	after := NewIndex(moved)
	text := "[[ideas]] [[ideas#h|label]] [[logs/ideas]]\n[x](ideas.md) `[[ideas]]`\n"
	got, n := Rewrite(text, "notes/x.md", "notes/ideas.md", "archive/old ideas.md", before, after)
	want := "[[old ideas]] [[old ideas#h|label]] [[logs/ideas]]\n[x](../archive/old%20ideas.md) `[[ideas]]`\n"
	if got != want || n != 3 {
		t.Fatalf("got %q (%d)\nwant %q (3)", got, n, want)
	}
	// A bare name that would now resolve elsewhere becomes a path.
	after2 := NewIndex([]string{"README.md", "archive/ideas.md", "logs/ideas.md"})
	got, _ = Rewrite("[[ideas]]", "notes/x.md", "notes/ideas.md", "archive/ideas.md", before, after2)
	if got != "[[archive/ideas]]" {
		t.Fatalf("ambiguous rename: got %q, want [[archive/ideas]]", got)
	}
}

func TestRebaseMovedNote(t *testing.T) {
	ix := NewIndex(notes)
	text := "[i](ideas.md) [d](../a/b/deep.md) [[ideas]] [w](https://x/y.md)"
	got := Rebase(text, "notes/x.md", "a/b/x.md", ix)
	want := "[i](../../notes/ideas.md) [d](deep.md) [[ideas]] [w](https://x/y.md)"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}
