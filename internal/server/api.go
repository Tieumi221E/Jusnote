package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Tieumi221E/Jusnote/internal/history"
	"github.com/Tieumi221E/Jusnote/internal/notebook"
	"github.com/Tieumi221E/Jusnote/internal/recordtype"
	"github.com/Tieumi221E/Jusnote/internal/service"
)

func (s *Server) routes(mux *http.ServeMux) {
	nb := func(h func(http.ResponseWriter, *http.Request, *service.Service)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			svc := s.current()
			if svc == nil {
				fail(w, errNoNotebook)
				return
			}
			h(w, r, svc)
		}
	}
	mux.HandleFunc("POST /api/state", s.handleState)
	caps := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Caps == nil {
			http.NotFound(w, r)
			return
		}
		s.Caps.ServeHTTP(w, r)
	})
	mux.Handle("GET /api/cap", caps)   // help
	mux.Handle("GET /api/cap/", caps)  // streams (events.watch)
	mux.Handle("POST /api/cap/", caps) // calls
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /api/recent", s.handleRecent)
	mux.HandleFunc("POST /api/open", s.handleOpen)
	mux.HandleFunc("GET /api/list", s.handleList)
	mux.HandleFunc("GET /api/note", nb(s.handleNoteGet))
	mux.HandleFunc("PUT /api/note", nb(s.handleNotePut))
	mux.HandleFunc("GET /api/version", nb(s.handleVersion))
	mux.HandleFunc("GET /api/raw", nb(s.handleRaw))
	mux.HandleFunc("POST /api/commit", nb(s.handleCommit))
	mux.HandleFunc("POST /api/rename", nb(s.handleRename))
	mux.HandleFunc("POST /api/delete", nb(s.handleDelete))
	mux.HandleFunc("POST /api/attach", nb(s.handleAttach))
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/changes", nb(s.handleChanges))
	mux.HandleFunc("GET /api/diff", nb(s.handleDiff))
	mux.HandleFunc("POST /api/discard", nb(s.handleDiscard))
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/show", nb(s.handleShow))
	mux.HandleFunc("POST /api/restore", nb(s.handleRestore))
	mux.HandleFunc("GET /api/gutter", s.handleGutter)
	mux.HandleFunc("POST /api/links", nb(s.handleLinks))
	mux.HandleFunc("GET /api/resolve", nb(s.handleResolve))
	mux.HandleFunc("GET /api/types", s.handleTypes)
	mux.HandleFunc("POST /api/check", s.handleCheck)
	mux.HandleFunc("POST /api/capture", nb(s.handleCapture))
	mux.HandleFunc("GET /api/session", s.handleSessionGet)
	mux.HandleFunc("POST /api/session", s.handleSessionPost)
	mux.HandleFunc("GET /api/prefs", s.handlePrefsGet)
	mux.HandleFunc("POST /api/prefs", s.handlePrefsPost)
	mux.HandleFunc("GET /api/log", s.handleLogGet)
	mux.HandleFunc("POST /api/log", s.handleLogPost)
	mux.HandleFunc("POST /api/selftest", s.handleSelftest)
	mux.HandleFunc("POST /api/selftest/outside", nb(s.handleSelftestOutside))
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	svc := s.current()
	if svc == nil {
		ok(w, map[string]any{"open": false})
		return
	}
	docs, err := svc.NB.List()
	if err != nil {
		fail(w, err)
		return
	}
	info := map[string]any{"open": true, "root": svc.NB.Root(), "vault": svc.NB.Vault(), "notes": len(docs), "git": svc.Repo != nil}
	if svc.Repo != nil {
		if changed, err := svc.Repo.Status(); err == nil {
			info["changed"] = len(changed)
		}
	}
	ok(w, info)
}

func (s *Server) handleRecent(w http.ResponseWriter, r *http.Request) {
	ok(w, s.recent.all())
}

func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	var in struct{ Folder string }
	if !decode(w, r, &in) {
		return
	}
	if in.Folder == "" {
		fail(w, errors.New("no folder chosen"))
		return
	}
	if err := s.Open(in.Folder); err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]any{"root": s.current().NB.Root()})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	svc := s.current()
	if svc == nil {
		ok(w, []notebook.Doc{})
		return
	}
	// The notes and the other text files (notebook.Files): the page shows
	// what the files preference says; links and completion use the notes.
	files, err := svc.NB.Files()
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, orEmpty(files))
}

func (s *Server) handleNoteGet(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	path := r.URL.Query().Get("path")
	text, err := svc.NB.Read(path)
	if err != nil {
		fail(w, err)
		return
	}
	// Text not in UTF-8 (an old GBK or Shift-JIS file) is read in its own
	// encoding, for showing: the editor opens it read-only (notebook.Files).
	shown, enc := notebook.TextOf(text)
	ok(w, map[string]any{
		"path":     path,
		"text":     shown,
		"encoding": enc,
		"version":  notebook.VersionOf(text),
		"tracked":  svc.Repo != nil && svc.Repo.Tracked(path),
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	v, err := svc.NB.Version(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]string{"version": v})
}

// handleNotePut saves the editor's text. Base is the version the editor
// loaded; when the file has changed on disk since, nothing is written and
// the answer is 409, so the page can ask what to do. Commit asks for the
// save to be committed at once (Ctrl+S); otherwise it is pending.
func (s *Server) handleNotePut(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	var in struct {
		Path   string  `json:"path"`
		Text   string  `json:"text"`
		Base   *string `json:"base"`
		Commit bool    `json:"commit"`
	}
	if !decode(w, r, &in) {
		return
	}
	var clean string
	var err error
	if in.Base != nil {
		clean, err = svc.NB.WriteIf(in.Path, []byte(in.Text), *in.Base)
	} else {
		clean, err = svc.NB.Write(in.Path, []byte(in.Text))
	}
	if errors.Is(err, notebook.ErrChanged) {
		conflict(w, err)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	s.markPending(clean, []byte(in.Text))
	out := map[string]any{"path": clean, "version": notebook.VersionOf([]byte(in.Text)), "committed": false, "hash": ""}
	if in.Commit {
		h, did, err := s.commit("", clean)
		if err != nil {
			fail(w, err)
			return
		}
		out["committed"], out["hash"] = did, h
	}
	ok(w, out)
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	full, err := svc.NB.Path(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	data, err := os.ReadFile(full)
	if err != nil {
		fail(w, err)
		return
	}
	http.ServeContent(w, r, filepath.Base(full), time.Time{}, bytes.NewReader(data))
}

// handleCommit commits the named paths — app-written or not; this is how
// the user accepts outside changes — or, with all, every change. With
// mine, only what the app wrote to paths (leaving a note).
func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	var in struct {
		Paths []string `json:"paths"`
		All   bool     `json:"all"`
		Mine  bool     `json:"mine"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.All && svc.Repo != nil {
		if changed, err := svc.Repo.Status(); err == nil {
			in.Paths = changed
		}
	}
	var h string
	var did bool
	var err error
	switch {
	case in.Mine && len(in.Paths) == 0:
	case in.Mine:
		h, did, err = s.commitMine(in.Paths...)
	default:
		h, did, err = s.commit("", in.Paths...)
	}
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]any{"committed": did, "hash": h})
}

// handleRename moves a note and, with links, rewrites the links to it; the
// rename and the rewritten notes are committed together at once (a move
// is otherwise a deletion plus an untracked file).
func (s *Server) handleRename(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	var in struct {
		From, To string
		Links    bool
	}
	if !decode(w, r, &in) {
		return
	}
	from, err := svc.Clean(in.From)
	if err != nil {
		fail(w, err)
		return
	}
	s.commitMine(from) // the editor's last autosave goes in under the old name
	res, err := svc.Rename(from, in.To, in.Links)
	if err != nil {
		fail(w, err)
		return
	}
	paths := append([]string{from, res.Path}, res.Updated...)
	s.dropPending(paths...)
	subject := "rename " + from + " to " + res.Path
	if len(res.Updated) > 0 {
		subject += " (links in " + strconv.Itoa(len(res.Updated)) + " notes)"
	}
	h, did, err := svc.Commit(subject, history.Provenance{}, paths...)
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]any{"path": res.Path, "updated": res.Updated, "committed": did, "hash": h})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	var in struct{ Path string }
	if !decode(w, r, &in) {
		return
	}
	rel, err := svc.Delete(in.Path)
	if err != nil {
		fail(w, err)
		return
	}
	s.dropPending(rel)
	h, did, err := svc.Commit("delete "+rel, history.Provenance{}, rel)
	if err != nil && !errors.Is(err, service.ErrNoRepo) {
		fail(w, err)
		return
	}
	ok(w, map[string]any{"committed": did, "hash": h})
}

// handleAttach stores a pasted or dropped file next to a note. The body is
// JSON (the write guard), so the bytes come base64-encoded.
func (s *Server) handleAttach(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	var in struct{ Note, Name, Data string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(&in); err != nil {
		fail(w, err)
		return
	}
	data, err := base64.StdEncoding.DecodeString(in.Data)
	if err != nil {
		fail(w, err)
		return
	}
	file, link, err := svc.Attach(in.Note, in.Name, data)
	if err != nil {
		fail(w, err)
		return
	}
	s.markPending(file, data)
	ok(w, map[string]string{"path": file, "link": link})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	svc := s.current()
	if svc == nil {
		ok(w, []notebook.Hit{})
		return
	}
	hits, err := svc.Search(r.URL.Query().Get("q"), 300)
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, orEmpty(hits))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	svc := s.current()
	if svc == nil || svc.Repo == nil {
		ok(w, []string{})
		return
	}
	changed, err := svc.Repo.Status()
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, orEmpty(changed))
}

func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	if svc.Repo == nil {
		ok(w, []service.Change{})
		return
	}
	cs, err := svc.Changes()
	if err != nil {
		fail(w, err)
		return
	}
	// Mine: the editor wrote it and will commit it itself; not for review.
	type change struct {
		service.Change
		Mine bool `json:"mine"`
	}
	out := make([]change, len(cs))
	s.mu.RLock()
	for i, c := range cs {
		_, mine := s.pending[c.Path]
		out[i] = change{Change: c, Mine: mine}
	}
	s.mu.RUnlock()
	ok(w, out)
}

// handleDiff is a file against HEAD, or with hash, a commit's version of
// it against the file now.
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	q := r.URL.Query()
	var hs service.FileDiff
	var err error
	if h := q.Get("hash"); h != "" {
		hs, err = svc.DiffAt(h, q.Get("path"))
	} else {
		hs, err = svc.Diff(q.Get("path"))
	}
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, hs)
}

func (s *Server) handleDiscard(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	var in struct{ Path string }
	if !decode(w, r, &in) {
		return
	}
	if err := svc.Discard(in.Path); err != nil {
		fail(w, err)
		return
	}
	if rel, err := svc.Clean(in.Path); err == nil {
		s.dropPending(rel)
	}
	ok(w, map[string]bool{"ok": true})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	svc := s.current()
	if svc == nil || svc.Repo == nil {
		ok(w, []history.Commit{})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	cs, err := svc.Log(r.URL.Query().Get("path"), limit)
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, cs)
}

func (s *Server) handleShow(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	q := r.URL.Query()
	text, err := svc.Show(q.Get("hash"), q.Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]string{"text": text})
}

// handleRestore makes an old version the current content and commits that
// at once ("restore … to …"), so the history reads the way it happened.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	var in struct{ Hash, Path string }
	if !decode(w, r, &in) {
		return
	}
	rel, err := svc.Restore(in.Hash, in.Path)
	if err != nil {
		fail(w, err)
		return
	}
	s.dropPending(rel)
	h, did, err := svc.Commit("restore "+rel+" to "+shortHash(in.Hash), history.Provenance{}, rel)
	if err != nil {
		fail(w, err)
		return
	}
	v, _ := svc.NB.Version(rel)
	ok(w, map[string]any{"path": rel, "version": v, "committed": did, "hash": h})
}

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

func (s *Server) handleGutter(w http.ResponseWriter, r *http.Request) {
	svc := s.current()
	if svc == nil || svc.Repo == nil {
		ok(w, []history.Change{})
		return
	}
	changes, err := svc.Repo.Gutter(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, orEmpty(changes))
}

// handleLinks lists a note's links and backlinks; Text is the editor's
// current (maybe unsaved) text.
func (s *Server) handleLinks(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	var in struct {
		Path string  `json:"path"`
		Text *string `json:"text"`
	}
	if !decode(w, r, &in) {
		return
	}
	var text []byte
	if in.Text != nil {
		text = []byte(*in.Text)
	}
	l, err := svc.LinksOf(in.Path, text)
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, l)
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	q := r.URL.Query()
	p, err := svc.Resolve(q.Get("target"), q.Get("from"))
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]string{"path": p})
}

func (s *Server) handleTypes(w http.ResponseWriter, r *http.Request) {
	svc := s.current()
	if svc == nil || svc.Types == nil {
		ok(w, []*recordtype.Type{})
		return
	}
	ok(w, svc.Types)
}

// handleCheck validates the editor's current text against the record type
// its path matches; a path that matches no type is left alone.
func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	var in struct{ Path, Text string }
	if !decode(w, r, &in) {
		return
	}
	svc := s.current()
	if svc == nil {
		ok(w, []recordtype.Diagnostic{})
		return
	}
	_, diags, _ := svc.Check(in.Path, []byte(in.Text))
	ok(w, orEmpty(diags))
}

// handleCapture appends one entry to a record type's file for a date
// (today by default) and commits that file.
func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	var in struct{ Type, Section, Text, Date string }
	if !decode(w, r, &in) {
		return
	}
	date := time.Now()
	if in.Date != "" {
		if d, err := time.ParseInLocation("2006-01-02", in.Date, time.Local); err == nil {
			date = d
		}
	}
	rel, err := svc.Capture(in.Type, in.Section, in.Text, date)
	if err != nil {
		fail(w, err)
		return
	}
	h, did, err := s.commit("log: "+rel, rel)
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]any{"path": rel, "committed": did, "hash": h})
}

// handleLogGet is the in-memory log, for "copy diagnostic log".
func (s *Server) handleLogGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, s.mem.String())
}

// handleLogPost records an error the page ran into.
func (s *Server) handleLogPost(w http.ResponseWriter, r *http.Request) {
	var in struct{ Message string }
	if !decode(w, r, &in) {
		return
	}
	m := in.Message
	if len(m) > 4000 {
		m = m[:4000]
	}
	s.Log.Printf("page: %s", m)
	ok(w, map[string]bool{"ok": true})
}

// handleSelftest hands the page's selftest report to the process.
func (s *Server) handleSelftest(w http.ResponseWriter, r *http.Request) {
	if s.OnSelftest == nil {
		http.NotFound(w, r)
		return
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
	go s.OnSelftest(b)
}

// handleSelftestOutside plays another program (or an agent) writing a
// note: straight to disk, not through the editor, so it shows up for
// review. Only in selftest mode.
func (s *Server) handleSelftestOutside(w http.ResponseWriter, r *http.Request, svc *service.Service) {
	if s.OnSelftest == nil {
		http.NotFound(w, r)
		return
	}
	var in struct {
		Path, Text string
		Source     history.Provenance
	}
	if !decode(w, r, &in) {
		return
	}
	full, err := svc.NB.Path(in.Path)
	if err != nil {
		fail(w, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		fail(w, err)
		return
	}
	if err := os.WriteFile(full, []byte(in.Text), 0o644); err != nil {
		fail(w, err)
		return
	}
	if err := svc.Record(in.Path, in.Source); err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func ok(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func conflict(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "changed": true})
}

// decode reads a JSON body (at most 16 MiB) into v, answering the error
// itself when it cannot.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(v); err != nil {
		fail(w, err)
		return false
	}
	return true
}

// orEmpty makes a nil slice encode as [] (the page iterates it).
func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}
