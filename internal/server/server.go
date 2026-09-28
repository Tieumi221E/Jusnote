package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/Tieumi221E/Jus/capreg"
	"github.com/Tieumi221E/Jusnote/internal/footprint"
	"github.com/Tieumi221E/Jusnote/internal/history"
	"github.com/Tieumi221E/Jusnote/internal/jusbase/memlog"
	"github.com/Tieumi221E/Jusnote/internal/notebook"
	"github.com/Tieumi221E/Jusnote/internal/service"
)

// Server serves one notebook at a time to the editor page. A notebook can
// be opened at runtime (the welcome screen and the notebook switcher), so
// the current one is guarded by a mutex. Everything the page can do goes
// through internal/service, which the CLI uses too.
//
// The page autosaves to disk; commits happen when the user asks (Ctrl+S),
// when they leave a note, and when the window closes (CommitPending). Only
// files the app itself wrote are committed that way: an edit made by
// another program or an agent stays in the working tree for the user to
// review (the "uncommitted" list: diff, accept, discard).
type Server struct {
	data   string // the app's own folder
	fsys   fs.FS
	prefs  *prefStore
	recent *recentStore
	http   *http.Server
	base   string
	host   string
	Log    *log.Logger
	// Version is the app's version, shown in the shortcut sheet.
	Version string
	// OnSelftest, if set, receives the report the page posts in selftest mode.
	OnSelftest func([]byte)
	mem        *memlog.Buffer

	// Caps is everything the app can do (internal/caps and RegisterWindow),
	// served at api/cap for the page and the command line.
	Caps *capreg.Registry
	live live // what the page shows, and who listens (live.go)

	mu      sync.RWMutex
	svc     *service.Service
	pending map[string]string // written by the app since their last commit: path -> version written
}

// New builds a server; dataDir holds the app's own settings.
func New(fsys fs.FS, dataDir string) *Server {
	mem := memlog.New(4000)
	return &Server{
		fsys:    fsys,
		data:    dataDir,
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
// remembers it. What the app wrote to the previous notebook is committed
// first.
func (s *Server) Open(root string) error {
	svc, err := service.Open(root, true)
	if err != nil {
		return err
	}
	s.CommitPending()
	s.mu.Lock()
	s.svc = svc
	s.pending = map[string]string{}
	s.mu.Unlock()
	s.recent.add(svc.NB.Root())
	footprint.Remember(s.data, svc.NB.Root())
	s.Log.Printf("open notebook %s", svc.NB.Root())
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

func (s *Server) current() *service.Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.svc
}

// Notebook is the open notebook's service (nil before one is open), for
// the selftest, the benchmarks and the window's capabilities.
func (s *Server) Notebook() *service.Service { return s.current() }

// commit commits the given paths — the user named them, so whoever wrote
// them — and clears them from the pending set.
func (s *Server) commit(subject string, paths ...string) (string, bool, error) {
	svc := s.current()
	if svc == nil || svc.Repo == nil || len(paths) == 0 {
		return "", false, nil
	}
	s.mu.Lock()
	for _, p := range paths {
		delete(s.pending, p)
	}
	s.mu.Unlock()
	h, did, err := svc.Commit(subject, history.Provenance{}, paths...)
	if err != nil {
		s.Log.Printf("commit %v: %v", paths, err)
	}
	return h, did, err
}

// commitMine commits what the app wrote — of the given paths, or all —
// and nothing else. A pending file that no longer holds what the app
// wrote (another program changed it since) is left for the user to
// review.
func (s *Server) commitMine(only ...string) (string, bool, error) {
	svc := s.current()
	if svc == nil || svc.NB.Settings().Commit == "manual" {
		return "", false, nil // manual: only Ctrl+S and an explicit commit commit
	}
	s.mu.Lock()
	var paths []string
	for p, v := range s.pending {
		if len(only) > 0 && !slices.Contains(only, p) {
			continue
		}
		delete(s.pending, p)
		if cur, err := svc.NB.Version(p); err == nil && (cur == v || cur == "") {
			paths = append(paths, p)
		}
	}
	s.mu.Unlock()
	if svc.Repo == nil || len(paths) == 0 {
		return "", false, nil
	}
	h, did, err := svc.Commit("", history.Provenance{}, paths...)
	if err != nil {
		s.Log.Printf("commit %v: %v", paths, err)
	}
	return h, did, err
}

// CommitPending commits what the app wrote and has not committed yet; the
// window calls it when it closes.
func (s *Server) CommitPending() {
	s.commitMine()
}

func (s *Server) markPending(rel string, data []byte) {
	s.mu.Lock()
	s.pending[rel] = notebook.VersionOf(data)
	s.mu.Unlock()
}

func (s *Server) dropPending(paths ...string) {
	s.mu.Lock()
	for _, p := range paths {
		delete(s.pending, p)
	}
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
	s.routes(mux)
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

var errNoNotebook = errors.New("no notebook is open")
