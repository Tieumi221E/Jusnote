// Package service is what Jusnote can do to a notebook, once: the editor's
// server and the command line both call it, so the app never holds a
// capability the outside lacks (Jus contract: one interface for people and
// agents). It owns no policy about *when* to commit — the server commits
// what the app wrote at natural moments, the CLI when told — only *how*.
package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tieumi221E/Jusnote/internal/history"
	"github.com/Tieumi221E/Jusnote/internal/links"
	"github.com/Tieumi221E/Jusnote/internal/notebook"
	"github.com/Tieumi221E/Jusnote/internal/recordtype"
)

// Service is one open notebook.
type Service struct {
	NB    *notebook.Notebook
	Repo  *history.Repo // nil when the folder has no git repository
	Types []*recordtype.Type

	provMu sync.Mutex

	// The notes' texts, kept while the editor runs so search and backlinks
	// read only what changed (by modification time and size).
	cacheMu sync.Mutex
	cache   map[string]cached
}

type cached struct {
	mod  time.Time
	size int64
	text string
}

// Open opens a notebook folder. With ensureRepo (the editor), a missing
// git repository is created and the vault files the app generates are
// committed; the CLI leaves a folder without git alone.
func Open(root string, ensureRepo bool) (*Service, error) {
	nb, err := notebook.Open(root)
	if err != nil {
		return nil, err
	}
	s := &Service{NB: nb}
	switch {
	case ensureRepo:
		if s.Repo, err = history.Ensure(nb.Root()); err != nil {
			return nil, err
		}
	case history.IsRepo(nb.Root()):
		if s.Repo, err = history.Open(nb.Root()); err != nil {
			return nil, err
		}
	}
	_ = recordtype.EnsureDefaults(nb.Vault())
	s.Types, _ = recordtype.Load(nb.Vault())
	if ensureRepo {
		if fresh, err := s.Repo.Untracked(notebook.VaultDir); err == nil && len(fresh) > 0 {
			if _, _, err := s.Repo.CommitPaths("jusnote: notebook metadata", fresh...); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

// ErrNoRepo is returned by history operations in a folder without git.
var ErrNoRepo = errors.New("this notebook has no git repository (run `jusnote init`)")

func (s *Service) repo() (*history.Repo, error) {
	if s.Repo == nil {
		return nil, ErrNoRepo
	}
	return s.Repo, nil
}

// Clean normalises a notebook-relative path (and refuses unsafe ones).
func (s *Service) Clean(rel string) (string, error) {
	p, err := s.NB.Path(rel)
	if err != nil {
		return "", err
	}
	r, err := filepath.Rel(s.NB.Root(), p)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(r), nil
}

// --- commits and provenance -------------------------------------------------

// Commit commits paths with the subject and provenance given. A path whose
// working file was written through the CLI with recorded provenance (see
// Record) gets that provenance when p is empty and the file is still the
// version recorded — so accepting an agent's change in the editor still
// says who made it.
func (s *Service) Commit(subject string, p history.Provenance, paths ...string) (string, bool, error) {
	repo, err := s.repo()
	if err != nil {
		return "", false, err
	}
	if len(paths) == 0 {
		return "", false, nil
	}
	sort.Strings(paths)
	if subject == "" {
		subject = "update " + paths[0]
		if len(paths) > 1 {
			subject = fmt.Sprintf("update %s and %d more", paths[0], len(paths)-1)
		}
	}
	if p == (history.Provenance{}) {
		p = s.recordedFor(paths)
	}
	h, did, err := repo.CommitPaths(p.Message(subject), paths...)
	if err == nil {
		s.forget(paths...)
	}
	return h, did, err
}

type recorded struct {
	Version string             `json:"version"`
	Source  history.Provenance `json:"source"`
	When    time.Time          `json:"when"`
}

func (s *Service) provPath() string { return filepath.Join(s.NB.Vault(), "cache", "provenance.json") }

func (s *Service) loadProv() map[string]recorded {
	m := map[string]recorded{}
	if b, err := os.ReadFile(s.provPath()); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func (s *Service) saveProv(m map[string]recorded) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	tmp := s.provPath() + ".partial"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.provPath())
}

// Record notes who wrote rel (an uncommitted write, e.g. an agent's
// `jusnote write -no-commit -author agent`), for the commit that later
// accepts it. It is machine state in the vault's cache, never committed.
func (s *Service) Record(rel string, p history.Provenance) error {
	if p == (history.Provenance{}) {
		return nil
	}
	v, err := s.NB.Version(rel)
	if err != nil {
		return err
	}
	s.provMu.Lock()
	defer s.provMu.Unlock()
	m := s.loadProv()
	m[rel] = recorded{Version: v, Source: p, When: time.Now()}
	return s.saveProv(m)
}

// Recorded is the provenance recorded for rel's current content, if any.
func (s *Service) Recorded(rel string) (history.Provenance, bool) {
	s.provMu.Lock()
	r, ok := s.loadProv()[rel]
	s.provMu.Unlock()
	if !ok {
		return history.Provenance{}, false
	}
	if v, err := s.NB.Version(rel); err != nil || v != r.Version {
		return history.Provenance{}, false
	}
	return r.Source, true
}

// recordedFor is the provenance shared by all of paths, or none.
func (s *Service) recordedFor(paths []string) history.Provenance {
	var out history.Provenance
	for i, p := range paths {
		r, ok := s.Recorded(p)
		if !ok || (i > 0 && r != out) {
			return history.Provenance{}
		}
		out = r
	}
	return out
}

func (s *Service) forget(paths ...string) {
	s.provMu.Lock()
	defer s.provMu.Unlock()
	m := s.loadProv()
	changed := false
	for _, p := range paths {
		if _, ok := m[p]; ok {
			delete(m, p)
			changed = true
		}
	}
	if changed {
		s.saveProv(m)
	}
}

// --- reading and searching ----------------------------------------------------

// Search finds lines containing q in every note and every other text file
// the editor can open, ignoring case.
func (s *Service) Search(q string, limit int) ([]notebook.Hit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	files, err := s.NB.Files()
	if err != nil {
		return nil, err
	}
	var docs []notebook.Doc
	for _, f := range files {
		if f.ReadOnly == "" {
			docs = append(docs, f.Doc)
		}
	}
	lq := strings.ToLower(q)
	texts := s.textsOf(docs, true)
	var out []notebook.Hit
	for _, d := range docs {
		if out = notebook.FindIn(out, d.Rel, texts[d.Rel], lq, limit); limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// texts is every listed note's text, from the cache when the file has not
// changed since it was read.
func (s *Service) texts(docs []notebook.Doc) map[string]string { return s.textsOf(docs, false) }

// textsOf is texts; prune (for the widest list, Search's) also drops from
// the cache the files no longer listed.
func (s *Service) textsOf(docs []notebook.Doc, prune bool) map[string]string {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cache == nil {
		s.cache = map[string]cached{}
	}
	out := make(map[string]string, len(docs))
	seen := make(map[string]bool, len(docs))
	for _, d := range docs {
		seen[d.Rel] = true
		if c, ok := s.cache[d.Rel]; ok && c.mod.Equal(d.ModTime) && c.size == d.Size {
			out[d.Rel] = c.text
			continue
		}
		b, err := s.NB.Read(d.Rel)
		if err != nil {
			continue
		}
		s.cache[d.Rel] = cached{mod: d.ModTime, size: d.Size, text: string(b)}
		out[d.Rel] = string(b)
	}
	if prune {
		for p := range s.cache {
			if !seen[p] {
				delete(s.cache, p)
			}
		}
	}
	return out
}

// Check validates text as the note rel; ok is false when rel matches no
// record type (then there is nothing to check).
func (s *Service) Check(rel string, text []byte) (typeID string, diags []recordtype.Diagnostic, ok bool) {
	t := recordtype.Match(s.Types, rel)
	if t == nil {
		return "", nil, false
	}
	return t.ID, t.Check(text), true
}

// Capture appends one entry to a record type's file for date and returns
// the file written. It does not commit.
func (s *Service) Capture(typeID, section, text string, date time.Time) (string, error) {
	var tp *recordtype.Type
	for _, t := range s.Types {
		if t.ID == typeID {
			tp = t
		}
	}
	if tp == nil {
		return "", fmt.Errorf("unknown record type %q", typeID)
	}
	rel := tp.FileName(date)
	if rel == "" {
		return "", fmt.Errorf("record type %q names no file; write the note directly", typeID)
	}
	var existing []byte
	if b, err := s.NB.Read(rel); err == nil {
		existing = b
	}
	out, err := tp.Append(existing, date, section, strings.TrimSpace(text))
	if err != nil {
		return "", err
	}
	return s.NB.Write(rel, out)
}

// --- links -----------------------------------------------------------------------

func (s *Service) notePaths() ([]string, error) {
	docs, err := s.NB.List()
	if err != nil {
		return nil, err
	}
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.Rel
	}
	return out, nil
}

func (s *Service) allTexts(paths []string) map[string]string {
	docs, err := s.NB.List()
	if err != nil {
		return map[string]string{}
	}
	return s.texts(docs)
}

// Links is a note's outgoing links and the lines elsewhere that link to it.
type Links struct {
	Out  []links.Link     `json:"out"`
	Back []links.Backlink `json:"back"`
}

// LinksOf lists rel's links; text, when non-nil, is the editor's unsaved
// version of rel.
func (s *Service) LinksOf(rel string, text []byte) (Links, error) {
	paths, err := s.notePaths()
	if err != nil {
		return Links{}, err
	}
	ix := links.NewIndex(paths)
	if text == nil {
		text, _ = s.NB.Read(rel)
	}
	out := Links{Out: ix.Parse(string(text), rel), Back: ix.Backlinks(rel, s.allTexts(paths))}
	if out.Out == nil {
		out.Out = []links.Link{}
	}
	if out.Back == nil {
		out.Back = []links.Backlink{}
	}
	return out, nil
}

// Resolve is the note a wiki link target written in from names ("" if none).
func (s *Service) Resolve(target, from string) (string, error) {
	paths, err := s.notePaths()
	if err != nil {
		return "", err
	}
	return links.NewIndex(paths).ResolveWiki(target, from), nil
}

// --- rename, delete, attach ----------------------------------------------------------

// Renamed is what a rename changed: the new path and the other notes whose
// links were rewritten.
type Renamed struct {
	Path    string   `json:"path"`
	Updated []string `json:"updated"`
}

// Rename moves a note; with updateLinks, links to it in other notes are
// rewritten to the new place. It does not commit.
func (s *Service) Rename(from, to string, updateLinks bool) (Renamed, error) {
	from, err := s.Clean(from)
	if err != nil {
		return Renamed{}, err
	}
	before, err := s.notePaths()
	if err != nil {
		return Renamed{}, err
	}
	texts := map[string]string{}
	if updateLinks {
		texts = s.allTexts(before)
	}
	newRel, err := s.NB.Rename(from, to)
	if err != nil {
		return Renamed{}, err
	}
	out := Renamed{Path: newRel, Updated: []string{}}
	if !updateLinks {
		return out, nil
	}
	after := make([]string, 0, len(before))
	for _, p := range before {
		if p == from {
			p = newRel
		}
		after = append(after, p)
	}
	ixBefore, ixAfter := links.NewIndex(before), links.NewIndex(after)
	for p, text := range texts {
		at := p
		if p == from {
			at = newRel // the renamed note's own relative links move with it
		}
		next, n := links.Rewrite(text, p, from, newRel, ixBefore, ixAfter)
		if at != p {
			next = links.Rebase(next, p, at, ixBefore)
		}
		if n == 0 && at == p {
			continue
		}
		if next == text {
			continue
		}
		if _, err := s.NB.Write(at, []byte(next)); err != nil {
			return out, err
		}
		if at != newRel {
			out.Updated = append(out.Updated, at)
		}
	}
	sort.Strings(out.Updated)
	return out, nil
}

// Delete removes a note (its last content stays in the vault's backup).
func (s *Service) Delete(rel string) (string, error) {
	rel, err := s.Clean(rel)
	if err != nil {
		return "", err
	}
	return rel, s.NB.Delete(rel)
}

// Attach stores data as a file next to the note rel, in an "attachments"
// folder, under name (made unique), and returns the file's notebook path
// and the link to write into the note.
func (s *Service) Attach(note, name string, data []byte) (file, link string, err error) {
	note, err = s.Clean(note)
	if err != nil {
		return "", "", err
	}
	name = safeName(name)
	dir := path.Join(path.Dir(note), "attachments")
	if path.Dir(note) == "." {
		dir = "attachments"
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	file = path.Join(dir, name)
	for i := 2; s.NB.Exists(file); i++ {
		file = path.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
	}
	if _, err := s.NB.Write(file, data); err != nil {
		return "", "", err
	}
	link = strings.TrimPrefix(file, path.Dir(note)+"/")
	return file, strings.ReplaceAll(link, " ", "%20"), nil
}

func safeName(n string) string {
	n = path.Base(filepath.ToSlash(strings.TrimSpace(n)))
	n = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '-'
		}
		return r
	}, n)
	if n == "" || n == "." || n == ".." {
		n = "file"
	}
	return n
}

// --- review and history ------------------------------------------------------------------

// Change is one uncommitted file.
type Change struct {
	Path    string             `json:"path"`
	Kind    string             `json:"kind"` // "new", "modified" or "deleted"
	Source  history.Provenance `json:"source"`
	Tracked bool               `json:"tracked"`
}

// Changes lists the uncommitted files with what kind of change each is and
// who is recorded as having made it.
func (s *Service) Changes() ([]Change, error) {
	repo, err := s.repo()
	if err != nil {
		return nil, err
	}
	paths, err := repo.Status()
	if err != nil {
		return nil, err
	}
	out := make([]Change, 0, len(paths))
	for _, p := range paths {
		c := Change{Path: p, Tracked: repo.Tracked(p)}
		switch {
		case !s.onDisk(p):
			c.Kind = "deleted"
		case !c.Tracked:
			c.Kind = "new"
		default:
			c.Kind = "modified"
		}
		c.Source, _ = s.Recorded(p)
		out = append(out, c)
	}
	return out, nil
}

// onDisk reports whether a path from git status is on disk. Not NB.Exists:
// that refuses the vault's paths, and vault files show up in status too.
func (s *Service) onDisk(rel string) bool {
	_, err := os.Stat(filepath.Join(s.NB.Root(), filepath.FromSlash(rel)))
	return err == nil
}

// FileDiff is a file's changes, and whether they are only line endings.
type FileDiff struct {
	Hunks   []history.Hunk `json:"hunks"`
	EOLOnly bool           `json:"eolOnly"`
}

// Diff is rel's working file against HEAD.
func (s *Service) Diff(rel string) (FileDiff, error) {
	repo, err := s.repo()
	if err != nil {
		return FileDiff{}, err
	}
	rel, err = s.Clean(rel)
	if err != nil {
		return FileDiff{}, err
	}
	hs, eol, err := repo.WorkingDiff(rel)
	if hs == nil {
		hs = []history.Hunk{}
	}
	return FileDiff{Hunks: hs, EOLOnly: eol}, err
}

// DiffAt is rel as it was in a commit against its working file now.
func (s *Service) DiffAt(hash, rel string) (FileDiff, error) {
	old, err := s.Show(hash, rel)
	if err != nil {
		return FileDiff{}, err
	}
	now, _ := s.NB.Read(rel)
	hs := history.Diff(old, string(now))
	if hs == nil {
		hs = []history.Hunk{}
	}
	return FileDiff{Hunks: hs, EOLOnly: history.EOLOnly(old, string(now))}, nil
}

// Discard puts rel back as it is at HEAD: a changed file gets its committed
// content, a new one is removed (both keep the discarded content in the
// vault's backup), a deleted one comes back.
func (s *Service) Discard(rel string) error {
	repo, err := s.repo()
	if err != nil {
		return err
	}
	rel, err = s.Clean(rel)
	if err != nil {
		return err
	}
	head, ok, err := repo.HeadContent(rel)
	if err != nil {
		return err
	}
	defer s.forget(rel)
	if !ok {
		if !s.NB.Exists(rel) {
			return nil
		}
		return s.NB.Delete(rel)
	}
	_, err = s.NB.Write(rel, []byte(head))
	return err
}

// Log is the commits that changed rel (or all commits when rel is "").
func (s *Service) Log(rel string, limit int) ([]history.Commit, error) {
	repo, err := s.repo()
	if err != nil {
		return nil, err
	}
	var cs []history.Commit
	if rel == "" {
		cs, err = repo.Log(limit)
	} else {
		if rel, err = s.Clean(rel); err != nil {
			return nil, err
		}
		cs, err = repo.FileLog(rel, limit)
	}
	if cs == nil {
		cs = []history.Commit{}
	}
	return cs, err
}

// Show is rel's text in a commit.
func (s *Service) Show(hash, rel string) (string, error) {
	repo, err := s.repo()
	if err != nil {
		return "", err
	}
	rel, err = s.Clean(rel)
	if err != nil {
		return "", err
	}
	text, ok, err := repo.ContentAt(hash, rel)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("%s did not exist in %s", rel, short(hash))
	}
	return text, nil
}

// Restore writes rel's text from a commit as its current content. History
// is not rewritten: the restore is a new change (commit it as usual).
func (s *Service) Restore(hash, rel string) (string, error) {
	text, err := s.Show(hash, rel)
	if err != nil {
		return "", err
	}
	return s.NB.Write(rel, []byte(text))
}

func short(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}
