// Package links finds the links between notes: wiki links ([[name]],
// [[name|label]], [[name#heading]]) and relative Markdown links
// ([label](other.md)). It resolves them to notebook paths, lists
// backlinks, and rewrites links when a note is renamed, so a rename never
// leaves links dangling.
//
// Resolution of a wiki link, as in other Markdown notebooks: a target with
// a "/" is a path from the notebook root; otherwise it is a note's file
// name (without ".md", ignoring case), and when several notes share it,
// the one in the linking note's folder, then the shortest path, wins.
package links

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Link is one link in a note.
type Link struct {
	Line   int    `json:"line"`   // 1-based
	Kind   string `json:"kind"`   // "wiki" or "md"
	Raw    string `json:"raw"`    // the target as written
	Target string `json:"target"` // resolved notebook path; "" when it names no note
}

var (
	wikiRE = regexp.MustCompile(`\[\[([^\[\]|#\n]+)(#[^\[\]|\n]*)?(\|[^\[\]\n]*)?\]\]`)
	mdRE   = regexp.MustCompile(`\]\(([^()\s]+\.(?:md|markdown))(#[^()\s]*)?\)`)
)

// Index maps names to notes for resolution.
type Index struct {
	paths  map[string]string   // lower-case path -> path
	byName map[string][]string // lower-case base name without extension -> paths
}

// NewIndex indexes the notebook's note paths (slash-separated).
func NewIndex(notes []string) *Index {
	ix := &Index{paths: map[string]string{}, byName: map[string][]string{}}
	for _, p := range notes {
		ix.paths[strings.ToLower(p)] = p
		n := strings.ToLower(stem(p))
		ix.byName[n] = append(ix.byName[n], p)
	}
	return ix
}

func stem(p string) string {
	b := path.Base(p)
	return strings.TrimSuffix(strings.TrimSuffix(b, ".md"), ".markdown")
}

// ResolveWiki resolves a wiki target written in the note from.
func (ix *Index) ResolveWiki(target, from string) string {
	t := strings.TrimSpace(target)
	if t == "" {
		return ""
	}
	if strings.Contains(t, "/") {
		p := strings.TrimPrefix(path.Clean(t), "/")
		for _, c := range []string{p, p + ".md", p + ".markdown"} {
			if hit, ok := ix.paths[strings.ToLower(c)]; ok {
				return hit
			}
		}
		return ""
	}
	cands := ix.byName[strings.ToLower(strings.TrimSuffix(t, ".md"))]
	if len(cands) == 0 {
		return ""
	}
	dir := path.Dir(from)
	best := ""
	for _, c := range cands {
		if path.Dir(c) == dir {
			return c
		}
		if best == "" || len(c) < len(best) || (len(c) == len(best) && c < best) {
			best = c
		}
	}
	return best
}

// ResolveMD resolves a relative Markdown link target written in from.
func (ix *Index) ResolveMD(target, from string) string {
	if strings.Contains(target, "://") || strings.HasPrefix(target, "/") {
		return ""
	}
	t := unescape(target)
	p := path.Clean(path.Join(path.Dir(from), t))
	if strings.HasPrefix(p, "../") {
		return ""
	}
	return ix.paths[strings.ToLower(p)]
}

// unescape undoes the %20-style escapes a Markdown link may use.
func unescape(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, ok := hex2(s[i+1], s[i+2]); ok {
				b.WriteByte(v)
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func hex2(a, b byte) (byte, bool) {
	h := func(c byte) (byte, bool) {
		switch {
		case '0' <= c && c <= '9':
			return c - '0', true
		case 'a' <= c && c <= 'f':
			return c - 'a' + 10, true
		case 'A' <= c && c <= 'F':
			return c - 'A' + 10, true
		}
		return 0, false
	}
	x, ok1 := h(a)
	y, ok2 := h(b)
	return x<<4 | y, ok1 && ok2
}

// Parse lists the links in text (the note at path from). Links inside
// fenced code blocks and inline code are not links.
func (ix *Index) Parse(text, from string) []Link {
	var out []Link
	fence := ""
	for i, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trim, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			fence = trim[:3]
			continue
		}
		line = blankInlineCode(line)
		for _, m := range wikiRE.FindAllStringSubmatch(line, -1) {
			out = append(out, Link{Line: i + 1, Kind: "wiki", Raw: m[1], Target: ix.ResolveWiki(m[1], from)})
		}
		for _, m := range mdRE.FindAllStringSubmatch(line, -1) {
			if strings.Contains(m[1], "://") {
				continue // a web address that happens to end in .md
			}
			out = append(out, Link{Line: i + 1, Kind: "md", Raw: m[1], Target: ix.ResolveMD(m[1], from)})
		}
	}
	return out
}

// blankInlineCode replaces `code` spans with spaces (keeping columns).
func blankInlineCode(line string) string {
	if !strings.Contains(line, "`") {
		return line
	}
	b := []byte(line)
	in := false
	for i := range b {
		if b[i] == '`' {
			in = !in
			continue
		}
		if in {
			b[i] = ' '
		}
	}
	return string(b)
}

// Backlink is a line in another note that links to a note.
type Backlink struct {
	From string `json:"from"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Backlinks lists the links to target in notes (path -> text).
func (ix *Index) Backlinks(target string, notes map[string]string) []Backlink {
	var out []Backlink
	for from, text := range notes {
		if from == target {
			continue
		}
		lines := strings.Split(text, "\n")
		for _, l := range ix.Parse(text, from) {
			if l.Target == target {
				out = append(out, Backlink{From: from, Line: l.Line, Text: strings.TrimSpace(strings.TrimRight(lines[l.Line-1], "\r"))})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// Rewrite returns text (the note at from) with every link that resolves
// to oldPath pointing at newPath instead, and how many it changed. Wiki
// links keep their form: a path stays a path, a bare name stays a bare
// name when it still resolves to the note (the new index is passed) and
// becomes a path otherwise. Headings and labels are kept.
func Rewrite(text, from, oldPath, newPath string, before, after *Index) (string, int) {
	n := 0
	fence := ""
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trim, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			fence = trim[:3]
			continue
		}
		code := blankInlineCode(line)
		var b strings.Builder
		last := 0
		for _, m := range wikiRE.FindAllStringSubmatchIndex(code, -1) {
			raw := line[m[2]:m[3]]
			if before.ResolveWiki(raw, from) != oldPath {
				continue
			}
			repl := stem(newPath)
			if strings.Contains(raw, "/") || after.ResolveWiki(repl, from) != newPath {
				repl = strings.TrimSuffix(strings.TrimSuffix(newPath, ".md"), ".markdown")
			}
			b.WriteString(line[last:m[2]])
			b.WriteString(repl)
			last = m[3]
			n++
		}
		if last > 0 {
			b.WriteString(line[last:])
			line = b.String()
			code = blankInlineCode(line)
		}
		b.Reset()
		last = 0
		for _, m := range mdRE.FindAllStringSubmatchIndex(code, -1) {
			raw := line[m[2]:m[3]]
			if before.ResolveMD(raw, from) != oldPath {
				continue
			}
			b.WriteString(line[last:m[2]])
			b.WriteString(relPath(from, newPath))
			last = m[3]
			n++
		}
		if last > 0 {
			b.WriteString(line[last:])
			line = b.String()
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n"), n
}

// relPath is target relative to the folder of from, with spaces escaped
// so the Markdown link stays one token.
func relPath(from, target string) string {
	fromDir := strings.Split(path.Dir(from), "/")
	if path.Dir(from) == "." {
		fromDir = nil
	}
	to := strings.Split(target, "/")
	i := 0
	for i < len(fromDir) && i < len(to)-1 && fromDir[i] == to[i] {
		i++
	}
	parts := make([]string, 0, len(fromDir)-i+len(to)-i)
	for range fromDir[i:] {
		parts = append(parts, "..")
	}
	parts = append(parts, to[i:]...)
	return strings.ReplaceAll(strings.Join(parts, "/"), " ", "%20")
}

// Rebase keeps a moved note's relative Markdown links pointing at the same
// notes from its new place (oldAt -> newAt). Wiki links need nothing: they
// name notes, not paths relative to the linking note.
func Rebase(text, oldAt, newAt string, ix *Index) string {
	if path.Dir(oldAt) == path.Dir(newAt) {
		return text
	}
	lines := strings.Split(text, "\n")
	fence := ""
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trim, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			fence = trim[:3]
			continue
		}
		code := blankInlineCode(line)
		var b strings.Builder
		last := 0
		for _, m := range mdRE.FindAllStringSubmatchIndex(code, -1) {
			target := ix.ResolveMD(line[m[2]:m[3]], oldAt)
			if target == "" {
				continue
			}
			b.WriteString(line[last:m[2]])
			b.WriteString(relPath(newAt, target))
			last = m[3]
		}
		if last > 0 {
			b.WriteString(line[last:])
			lines[i] = b.String()
		}
	}
	return strings.Join(lines, "\n")
}
