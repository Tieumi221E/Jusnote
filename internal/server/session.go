package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Session is where the user was in a notebook — the open note and each
// note's cursor — so reopening lands on the same line. It belongs to the
// notebook folder (like Jusplay's per-folder records), in the vault's
// cache: machine state, never committed.
type Session struct {
	File    string         `json:"file"`
	Cursors map[string]int `json:"cursors"`
}

const sessionMaxCursors = 200

func (s *Server) sessionPath() string {
	svc := s.current()
	if svc == nil {
		return ""
	}
	return filepath.Join(svc.NB.Vault(), "cache", "session.json")
}

func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	out := Session{Cursors: map[string]int{}}
	if p := s.sessionPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			json.Unmarshal(b, &out)
		}
	}
	if out.Cursors == nil {
		out.Cursors = map[string]int{}
	}
	ok(w, out)
}

func (s *Server) handleSessionPost(w http.ResponseWriter, r *http.Request) {
	p := s.sessionPath()
	if p == "" {
		fail(w, errNoNotebook)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		fail(w, err)
		return
	}
	var in Session
	if err := json.Unmarshal(body, &in); err != nil {
		fail(w, err)
		return
	}
	if len(in.Cursors) > sessionMaxCursors {
		keep := map[string]int{}
		if c, ok := in.Cursors[in.File]; ok {
			keep[in.File] = c
		}
		in.Cursors = keep
	}
	out, _ := json.Marshal(in)
	tmp := p + ".partial"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		fail(w, err)
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		fail(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
