// Package juslink is the addressing scheme shared by the Jus apps, so that
// a note, a video position and a book page can point at one another from
// day one. Links are stable text and are meant to be written into notes,
// exchanged with external agents, and opened by any Jus app.
//
// The form is "jus://<kind>/<target>?<query>":
//
//	jus://note/2026-09.md?line=42
//	jus://play/so46814148?t=12:34
//	jus://read/<id>?page=120
//	jus://talk/<id>?at=<message>
//
// Only the note side lives here for now; the other kinds are parsed and
// formatted so Jusnote can already recognise and round-trip them.
package juslink

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Kind is the app a link points into.
type Kind string

const (
	Note Kind = "note"
	Play Kind = "play"
	Read Kind = "read"
	Talk Kind = "talk"
)

// Scheme is the URL scheme for every Jus link.
const Scheme = "jus"

// Link is a parsed jus:// link. Target is the part after the kind (a path
// for notes, an id for the other apps); Query keeps any extra parameters.
type Link struct {
	Kind   Kind
	Target string
	Query  url.Values
}

// Parse reads a jus:// link. It also accepts bare refs like
// "note:2026-09.md" so callers can be lenient.
func Parse(s string) (*Link, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") && strings.Contains(s, ":") {
		s = Scheme + "://" + strings.Replace(s, ":", "/", 1)
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("juslink: %w", err)
	}
	if !strings.EqualFold(u.Scheme, Scheme) {
		return nil, fmt.Errorf("juslink: not a %s link: %q", Scheme, s)
	}
	kind := Kind(strings.ToLower(u.Host))
	if kind == "" {
		return nil, fmt.Errorf("juslink: missing kind in %q", s)
	}
	target := strings.TrimPrefix(u.Path, "/")
	if u.Host == "" && target == "" {
		return nil, fmt.Errorf("juslink: empty link %q", s)
	}
	return &Link{Kind: kind, Target: target, Query: u.Query()}, nil
}

// String renders the link back to text.
func (l *Link) String() string {
	u := url.URL{Scheme: Scheme, Host: string(l.Kind), Path: "/" + l.Target}
	if len(l.Query) > 0 {
		u.RawQuery = l.Query.Encode()
	}
	return u.String()
}

// NoteLink builds a link to a notebook-relative note path, optionally to
// a 1-based line.
func NoteLink(rel string, line int) string {
	l := &Link{Kind: Note, Target: strings.TrimPrefix(strings.ReplaceAll(rel, "\\", "/"), "/")}
	if line > 0 {
		l.Query = url.Values{"line": {strconv.Itoa(line)}}
	}
	return l.String()
}

// Line is the 1-based line of a note link, or 0.
func (l *Link) Line() int {
	n, err := strconv.Atoi(l.Query.Get("line"))
	if err != nil || n < 1 {
		return 0
	}
	return n
}
