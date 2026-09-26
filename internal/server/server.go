package server

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tieumi221E/Jusnote/internal/history"
	"github.com/Tieumi221E/Jusnote/internal/jusbase/memlog"
	"github.com/Tieumi221E/Jusnote/internal/notebook"
	"github.com/Tieumi221E/Jusnote/internal/recordtype"
)

// Server serves one or more notebooks to the editor page. A notebook can
// be opened at runtime (the welcome screen and the notebook switcher), so
// the current one is guarded by a mutex.
//
// The page autosaves to disk; commits happen when the user asks (Ctrl+S),
// when they leave a note, and when the window closes (CommitPending). Only
// files the app itself wrote are committed that way: an edit made by
// another program or an agent stays in the working tree for the user to
// review and commit.
type Server struct {
	fsys   fs.FS
	prefs  *prefStore
	recent *recentStore
	http   *http.Server
	base   string
	host   string
	Log    *log.Logger
	// Version is the app's version, shown in the shortcut sheet.
	Version string
	mem     *memlog.Buffer

	mu      sync.RWMutex
	nb      *notebook.Notebook
	repo    *history.Repo
	types   []*recordtype.Type
	pending map[string]string // written by the app since their last commit: path -> version written
}

// New builds a server; dataDir holds the app's own settings.
func New(fsys fs.FS, dataDir string) *Server {
	mem := memlog.New(4000)
	return &Server{
		fsys:    fsys,
		prefs:   openPrefs(filepath.Join(dataDir, "ui.json")),
		recent:  openRecent(filepath.Join(dataDir, "notebooks.json")),
		mem:     mem,
		Log:     log.New(mem, "", log.LstdFlags|log.Lmicroseconds),
		pending: map[string]string{},
	}
}

// Dark reports whether the saved theme is the night one, for the window
// frame's first paint.
func (s *Server) Dark() bool { return s.prefs.get().Theme != "light" }

// Open opens a notebook folder, creating its git repository if needed, and
// remembers it. Changes the app made to the previous notebook are
// committed first.
func (s *Server) Open(root string) error {
	nb, err := notebook.Open(root)
	if err != nil {
		return err
	}
	repo, err := history.Ensure(nb.Root())
	if err != nil {
		return err
	}
	_ = recordtype.EnsureDefaults(nb.Vault())
	types, _ := recordtype.Load(nb.Vault())
	// The vault files the app just created (built-in record types, the
	// self-ignoring cache folders) are committed at once, so they do not
	// sit in the "uncommitted" count; ones the user changed are left alone.
	if fresh, err := repo.Untracked(notebook.VaultDir); err == nil && len(fresh) > 0 {
		if _, _, err := repo.CommitPaths("jusnote: notebook metadata", fresh...); err != nil {
			return err
		}
	}

	s.CommitPending()
	s.mu.Lock()
	s.nb, s.repo, s.types = nb, repo, types
	s.pending = map[string]string{}
	s.mu.Unlock()
	s.recent.add(nb.Root())
	s.Log.Printf("open notebook %s", nb.Root())
	return nil
}

// OpenDefault opens explicit when given; otherwise the most recent notebook
// that still exists; otherwise leaves the server without a notebook (the
// page then shows the welcome screen).
func (s *Server) OpenDefault(explicit string) error {
	if explicit != "" {
		return s.Open(explicit)
	}
	for _, r := range s.recent.all() {
		if st, err := os.Stat(r.Path); err == nil && st.IsDir() {
			return s.Open(r.Path)
		}
	}
	return nil
}

// Recent lists remembered notebooks, most recent first.
func (s *Server) Recent() []Recent { return s.recent.all() }

func (s *Server) current() (*notebook.Notebook, *history.Repo, []*recordtype.Type) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nb, s.repo, s.types
}

// commit commits the given paths, or with none the pending ones, and
// clears them from the pending set.
func (s *Server) commit(paths ...string) (string, bool, error) {
	if len(paths) == 0 {
		return s.commitMine()
	}
	s.mu.Lock()
	for _, p := range paths {
		delete(s.pending, p)
	}
	s.mu.Unlock()
	return s.commitNow(paths)
}

// commitMine commits what the app wrote — of the given paths, or all —
// and nothing else. A pending file that no longer holds what the app
// wrote (another program changed it since) is left for the user to
// review.
func (s *Server) commitMine(only ...string) (string, bool, error) {
	s.mu.Lock()
	nb := s.nb
	var paths []string
	for p, v := range s.pending {
		if len(only) > 0 && !slices.Contains(only, p) {
			continue
		}
		delete(s.pending, p)
		if cur, err := nb.Version(p); err == nil && (cur == v || cur == "") {
			paths = append(paths, p)
		}
	}
	s.mu.Unlock()
	return s.commitNow(paths)
}

func (s *Server) commitNow(paths []string) (string, bool, error) {
	_, repo, _ := s.current()
	if repo == nil || len(paths) == 0 {
		return "", false, nil
	}
	sort.Strings(paths)
	msg := "update " + paths[0]
	if len(paths) > 1 {
		msg = fmt.Sprintf("update %s and %d more", paths[0], len(paths)-1)
	}
	h, did, err := repo.CommitPaths(msg, paths...)
	if err != nil {
		s.Log.Printf("commit %v: %v", paths, err)
	}
	return h, did, err
}

// CommitPending commits what the app wrote and has not committed yet; the
// window calls it when it closes.
func (s *Server) CommitPending() {
	s.commit()
}

func (s *Server) markPending(rel string, data []byte) {
	s.mu.Lock()
	s.pending[rel] = notebook.VersionOf(data)
	s.mu.Unlock()
}

// Start listens on a loopback port and returns the page's URL. Everything
// is served under a random path token and only to requests addressed to
// the loopback listener, so other local programs and web pages (DNS
// rebinding) cannot reach the notes.
func (s *Server) Start() (string, error) {
	var tok [16]byte
	if _, err := rand.Read(tok[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tok[:])

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /api/list", s.handleList)
	mux.HandleFunc("GET /api/note", s.handleNoteGet)
	mux.HandleFunc("PUT /api/note", s.handleNotePut)
	mux.HandleFunc("GET /api/version", s.handleVersion)
	mux.HandleFunc("POST /api/commit", s.handleCommit)
	mux.HandleFunc("POST /api/rename", s.handleRename)
	mux.HandleFunc("POST /api/delete", s.handleDelete)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/raw", s.handleRaw)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/gutter", s.handleGutter)
	mux.HandleFunc("GET /api/types", s.handleTypes)
	mux.HandleFunc("POST /api/check", s.handleCheck)
	mux.HandleFunc("POST /api/capture", s.handleCapture)
	mux.HandleFunc("GET /api/recent", s.handleRecent)
	mux.HandleFunc("POST /api/open", s.handleOpen)
	mux.HandleFunc("GET /api/session", s.handleSessionGet)
	mux.HandleFunc("POST /api/session", s.handleSessionPost)
	mux.HandleFunc("GET /api/prefs", s.handlePrefsGet)
	mux.HandleFunc("POST /api/prefs", s.handlePrefsPost)
	mux.HandleFunc("GET /api/log", s.handleLogGet)
	mux.HandleFunc("POST /api/log", s.handleLogPost)
	mux.HandleFunc("GET /prefs.js", s.handlePrefsJS)
	mux.Handle("GET /", http.FileServerFS(s.fsys))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	s.host = ln.Addr().String()
	s.base = "http://" + s.host + "/" + token + "/"
	s.http = &http.Server{Handler: s.guard(http.StripPrefix("/"+token, mux))}
	go s.http.Serve(ln)
	return s.base, nil
}

// guard rejects requests not addressed to the listener, and writes that do
// not carry JSON (a cross-site form cannot send it without a preflight,
// which is refused).
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.host {
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead &&
			!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "JSON only", http.StatusUnsupportedMediaType)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// Base is the page's URL after Start.
func (s *Server) Base() string { return s.base }

// Close stops the server.
func (s *Server) Close() error {
	if s.http == nil {
		return nil
	}
	return s.http.Close()
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	nb, repo, _ := s.current()
	if nb == nil {
		ok(w, map[string]any{"open": false})
		return
	}
	docs, err := nb.List()
	if err != nil {
		fail(w, err)
		return
	}
	info := map[string]any{
		"open":  true,
		"root":  nb.Root(),
		"vault": nb.Vault(),
		"notes": len(docs),
		"git":   repo != nil,
	}
	if repo != nil {
		if changed, err := repo.Status(); err == nil {
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
	nb, _, _ := s.current()
	ok(w, map[string]any{"root": nb.Root()})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	nb, _, _ := s.current()
	if nb == nil {
		ok(w, []notebook.Doc{})
		return
	}
	docs, err := nb.List()
	if err != nil {
		fail(w, err)
		return
	}
	if docs == nil {
		docs = []notebook.Doc{}
	}
	ok(w, docs)
}

func (s *Server) handleNoteGet(w http.ResponseWriter, r *http.Request) {
	nb, repo, _ := s.current()
	if nb == nil {
		fail(w, errNoNotebook)
		return
	}
	path := r.URL.Query().Get("path")
	text, err := nb.Read(path)
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]any{
		"path":    path,
		"text":    string(text),
		"version": notebook.VersionOf(text),
		"tracked": repo != nil && repo.Tracked(path),
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	nb, _, _ := s.current()
	if nb == nil {
		fail(w, errNoNotebook)
		return
	}
	v, err := nb.Version(r.URL.Query().Get("path"))
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
func (s *Server) handleNotePut(w http.ResponseWriter, r *http.Request) {
	nb, _, _ := s.current()
	if nb == nil {
		fail(w, errNoNotebook)
		return
	}
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
		clean, err = nb.WriteIf(in.Path, []byte(in.Text), *in.Base)
	} else {
		clean, err = nb.Write(in.Path, []byte(in.Text))
	}
	if errors.Is(err, notebook.ErrChanged) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "changed": true})
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	s.markPending(clean, []byte(in.Text))
	out := map[string]any{"path": clean, "version": notebook.VersionOf([]byte(in.Text)), "committed": false, "hash": ""}
	if in.Commit {
		h, did, err := s.commit(clean)
		if err != nil {
			fail(w, err)
			return
		}
		out["committed"], out["hash"] = did, h
	}
	ok(w, out)
}

// handleCommit commits the named paths — app-written or not; this is how
// the user accepts outside changes — or, with none, what the app wrote.
func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Paths []string `json:"paths"`
		All   bool     `json:"all"`  // every change in the working tree (the user accepts them)
		Mine  bool     `json:"mine"` // only what the app wrote to Paths (leaving a note)
	}
	if !decode(w, r, &in) {
		return
	}
	_, repo, _ := s.current()
	if in.All && repo != nil {
		if changed, err := repo.Status(); err == nil {
			in.Paths = changed
		}
	}
	var h string
	var did bool
	var err error
	if in.Mine {
		if len(in.Paths) == 0 {
			ok(w, map[string]any{"committed": false, "hash": ""})
			return
		}
		h, did, err = s.commitMine(in.Paths...)
	} else {
		h, did, err = s.commit(in.Paths...)
	}
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]any{"committed": did, "hash": h})
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	nb, _, _ := s.current()
	if nb == nil {
		fail(w, errNoNotebook)
		return
	}
	var in struct{ From, To string }
	if !decode(w, r, &in) {
		return
	}
	to, err := nb.Rename(in.From, in.To)
	if err != nil {
		fail(w, err)
		return
	}
	from := filepath.ToSlash(filepath.Clean(in.From))
	h, did, err := s.commitMove(from, to)
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]any{"path": to, "committed": did, "hash": h})
}

// commitMove records a rename or deletion right away: a moved file is
// otherwise a deleted one plus an untracked one in the working tree.
func (s *Server) commitMove(paths ...string) (string, bool, error) {
	_, repo, _ := s.current()
	if repo == nil {
		return "", false, nil
	}
	s.mu.Lock()
	for _, p := range paths {
		delete(s.pending, p)
	}
	s.mu.Unlock()
	msg := "delete " + paths[0]
	if len(paths) == 2 {
		msg = "rename " + paths[0] + " to " + paths[1]
	}
	return repo.CommitPaths(msg, paths...)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	nb, _, _ := s.current()
	if nb == nil {
		fail(w, errNoNotebook)
		return
	}
	var in struct{ Path string }
	if !decode(w, r, &in) {
		return
	}
	if err := nb.Delete(in.Path); err != nil {
		fail(w, err)
		return
	}
	h, did, err := s.commitMove(filepath.ToSlash(filepath.Clean(in.Path)))
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]any{"committed": did, "hash": h})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	nb, _, _ := s.current()
	if nb == nil {
		ok(w, []notebook.Hit{})
		return
	}
	hits, err := nb.Search(r.URL.Query().Get("q"), 300)
	if err != nil {
		fail(w, err)
		return
	}
	if hits == nil {
		hits = []notebook.Hit{}
	}
	ok(w, hits)
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	nb, _, _ := s.current()
	if nb == nil {
		fail(w, errNoNotebook)
		return
	}
	full, err := nb.Path(r.URL.Query().Get("path"))
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

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	_, repo, _ := s.current()
	if repo == nil {
		ok(w, []history.Commit{})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	commits, err := repo.Log(limit)
	if err != nil {
		fail(w, err)
		return
	}
	if commits == nil {
		commits = []history.Commit{}
	}
	ok(w, commits)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	_, repo, _ := s.current()
	if repo == nil {
		ok(w, []string{})
		return
	}
	changed, err := repo.Status()
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, changed)
}

func (s *Server) handleGutter(w http.ResponseWriter, r *http.Request) {
	_, repo, _ := s.current()
	if repo == nil {
		ok(w, []history.Change{})
		return
	}
	changes, err := repo.Gutter(r.URL.Query().Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	if changes == nil {
		changes = []history.Change{}
	}
	ok(w, changes)
}

func (s *Server) handleTypes(w http.ResponseWriter, r *http.Request) {
	_, _, types := s.current()
	if types == nil {
		ok(w, []*recordtype.Type{})
		return
	}
	ok(w, types)
}

// handleCheck validates the editor's current text against the record type
// its path matches; a path that matches no type is left alone.
func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	var in struct{ Path, Text string }
	if !decode(w, r, &in) {
		return
	}
	_, _, types := s.current()
	tp := recordtype.Match(types, in.Path)
	if tp == nil {
		ok(w, []recordtype.Diagnostic{})
		return
	}
	diags := tp.Check([]byte(in.Text))
	if diags == nil {
		diags = []recordtype.Diagnostic{}
	}
	ok(w, diags)
}

// handleCapture appends one entry to a record type's file for a date
// (today by default) and commits that file.
func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request) {
	nb, _, types := s.current()
	if nb == nil {
		fail(w, errNoNotebook)
		return
	}
	var in struct{ Type, Section, Text, Date string }
	if !decode(w, r, &in) {
		return
	}
	var tp *recordtype.Type
	for _, t := range types {
		if t.ID == in.Type {
			tp = t
		}
	}
	if tp == nil {
		fail(w, fmt.Errorf("unknown record type %q", in.Type))
		return
	}
	date := time.Now()
	if in.Date != "" {
		if d, err := time.ParseInLocation("2006-01-02", in.Date, time.Local); err == nil {
			date = d
		}
	}
	rel := tp.FileName(date)
	if rel == "" {
		fail(w, errors.New("this record type names no file"))
		return
	}
	var existing []byte
	if b, err := nb.Read(rel); err == nil {
		existing = b
	}
	out, err := tp.Append(existing, date, in.Section, in.Text)
	if err != nil {
		fail(w, err)
		return
	}
	clean, err := nb.Write(rel, out)
	if err != nil {
		fail(w, err)
		return
	}
	s.markPending(clean, out)
	h, did, err := s.commit(clean)
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]any{"path": clean, "committed": did, "hash": h})
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

func ok(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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

var errNoNotebook = errors.New("no notebook is open")
