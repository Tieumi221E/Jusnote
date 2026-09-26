package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Recent is one remembered notebook.
type Recent struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// recentStore is the small list of notebooks the app has opened, most
// recent first, kept in the app's data folder.
type recentStore struct {
	path string
	mu   sync.Mutex
	list []Recent
}

const recentMax = 10

func openRecent(path string) *recentStore {
	s := &recentStore{path: path}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			json.Unmarshal(b, &s.list)
		}
	}
	return s
}

func (s *recentStore) all() []Recent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Recent, len(s.list))
	copy(out, s.list)
	return out
}

// add moves root to the front, remembering its base name.
func (s *recentStore) add(root string) {
	name := filepath.Base(root)
	if name == "." || name == string(filepath.Separator) {
		name = root
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := []Recent{{Path: root, Name: name}}
	for _, r := range s.list {
		if r.Path != root {
			next = append(next, r)
		}
	}
	if len(next) > recentMax {
		next = next[:recentMax]
	}
	s.list = next
	s.save()
}

func (s *recentStore) save() {
	if s.path == "" {
		return
	}
	out, _ := json.MarshalIndent(s.list, "", "  ")
	os.MkdirAll(filepath.Dir(s.path), 0o755)
	tmp := s.path + ".partial"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return
	}
	os.Rename(tmp, s.path)
}
