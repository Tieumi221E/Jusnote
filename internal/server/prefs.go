package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Prefs are the interface choices shared with the page: language, colour
// theme, editor font size, line wrapping and rainbow indent guides. They
// are loaded as a blocking script (prefs.js) in <head> so the first paint
// is already right; the loopback port, and with it the page origin, changes
// every run, so localStorage cannot do this.
type Prefs struct {
	Lang    string `json:"lang"`    // "zh" or "ja"
	Theme   string `json:"theme"`   // "dark" or "light"
	Font    string `json:"font"`    // "sans" (Jus Sans) or "mono" (a monospace face, Jus Sans for CJK)
	Size    int    `json:"size"`    // editor font size in px
	Wrap    bool   `json:"wrap"`    // soft wrap long lines
	Guides  bool   `json:"guides"`  // rainbow indent guides
	Numbers bool   `json:"numbers"` // line numbers
	Files   string `json:"files"`   // the file list: "all" text files, or "notes" only
}

const (
	sizeMin     = 11
	sizeMax     = 28
	sizeDefault = 15
)

var prefChoices = map[string][]string{
	"lang":  {"zh", "ja"},
	"theme": {"dark", "light"},
	"font":  {"sans", "mono"},
	"files": {"all", "notes"},
}

func (p *Prefs) field(k string) *string {
	switch k {
	case "lang":
		return &p.Lang
	case "theme":
		return &p.Theme
	case "font":
		return &p.Font
	case "files":
		return &p.Files
	}
	return nil
}

func allowed(k, v string) bool {
	for _, c := range prefChoices[k] {
		if c == v {
			return true
		}
	}
	return false
}

func (p *Prefs) normalize() {
	for k, cs := range prefChoices {
		if f := p.field(k); !allowed(k, *f) {
			*f = cs[0]
		}
	}
	if p.Size < sizeMin {
		p.Size = sizeDefault
	}
	if p.Size > sizeMax {
		p.Size = sizeMax
	}
}

type prefStore struct {
	path string
	mu   sync.Mutex
	p    Prefs
}

func defaults() Prefs {
	return Prefs{Lang: "zh", Theme: "dark", Font: "sans", Size: sizeDefault, Wrap: true, Guides: true, Numbers: true, Files: "all"}
}

func openPrefs(path string) *prefStore {
	s := &prefStore{path: path, p: defaults()}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			json.Unmarshal(b, &s.p)
		}
	}
	s.p.normalize()
	return s
}

func (s *prefStore) get() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.p
}

// update applies the keys present in b; an unknown key or value is an error
// and changes nothing.
func (s *prefStore) update(b []byte) (Prefs, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return Prefs{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.p
	for k, raw := range m {
		switch k {
		case "lang", "theme", "font", "files":
			var v string
			if err := json.Unmarshal(raw, &v); err != nil || !allowed(k, v) {
				return Prefs{}, fmt.Errorf("bad preference %s=%s", k, raw)
			}
			*next.field(k) = v
		case "size":
			var v int
			if err := json.Unmarshal(raw, &v); err != nil {
				return Prefs{}, fmt.Errorf("bad preference %s=%s (a size in px)", k, raw)
			}
			if v < sizeMin {
				v = sizeMin
			}
			if v > sizeMax {
				v = sizeMax
			}
			next.Size = v
		case "wrap":
			if err := json.Unmarshal(raw, &next.Wrap); err != nil {
				return Prefs{}, fmt.Errorf("bad preference %s=%s (true or false)", k, raw)
			}
		case "guides":
			if err := json.Unmarshal(raw, &next.Guides); err != nil {
				return Prefs{}, fmt.Errorf("bad preference %s=%s (true or false)", k, raw)
			}
		case "numbers":
			if err := json.Unmarshal(raw, &next.Numbers); err != nil {
				return Prefs{}, fmt.Errorf("bad preference %s=%s (true or false)", k, raw)
			}
		default:
			return Prefs{}, fmt.Errorf("unknown preference %q", k)
		}
	}
	if s.path != "" {
		out, _ := json.Marshal(next)
		if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
			return Prefs{}, err
		}
		tmp := s.path + ".partial"
		if err := os.WriteFile(tmp, out, 0o644); err != nil {
			return Prefs{}, err
		}
		if err := os.Rename(tmp, s.path); err != nil {
			return Prefs{}, errors.Join(err, os.Remove(tmp))
		}
	}
	s.p = next
	return next, nil
}

func (s *Server) handlePrefsGet(w http.ResponseWriter, r *http.Request) {
	ok(w, s.prefs.get())
}

func (s *Server) handlePrefsPost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		fail(w, err)
		return
	}
	p, err := s.prefs.update(body)
	if err != nil {
		fail(w, err)
		return
	}
	ok(w, p)
}

// handlePrefsJS is prefs.js: it sets <html data-theme lang> and the editor
// font size before the body is parsed, and preloads the font faces.
func (s *Server) handlePrefsJS(w http.ResponseWriter, r *http.Request) {
	p := s.prefs.get()
	j, _ := json.Marshal(p)
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	v, _ := json.Marshal(s.Version)
	fmt.Fprintf(w, "window.jusVersion=%s;window.jusPrefs=%s;document.documentElement.dataset.theme=jusPrefs.theme;"+
		"document.documentElement.lang=jusPrefs.lang===\"ja\"?\"ja\":\"zh-CN\";"+
		"document.documentElement.style.setProperty(\"--editor-size\",jusPrefs.size+\"px\");document.documentElement.dataset.font=jusPrefs.font;"+
		"for(var w of[400,700]){var l=document.createElement(\"link\");l.rel=\"preload\";l.as=\"font\";"+
		"l.type=\"font/woff2\";l.crossOrigin=\"anonymous\";l.href=\"fonts/jus-sans-\"+w+\".woff2\";"+
		"document.head.appendChild(l)}\n", v, j)
}
