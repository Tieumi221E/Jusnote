package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Tieumi221E/Jusnote/internal/notebook"
)

func start(t *testing.T) (*Server, string) {
	t.Helper()
	s := New(fstest.MapFS{"index.html": {Data: []byte("page")}}, t.TempDir())
	if err := s.Open(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	base, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, base
}

func call(t *testing.T, method, url, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// The API answers only under the token, only to its own Host, and takes
// writes only as JSON.
func TestGuard(t *testing.T) {
	_, base := start(t)
	if code, _ := call(t, "GET", base+"api/info", ""); code != 200 {
		t.Fatalf("with token: %d", code)
	}
	root := base[:strings.Index(base[len("http://"):], "/")+len("http://")+1]
	if code, _ := call(t, "GET", root+"api/info", ""); code == 200 {
		t.Fatal("reachable without the token")
	}
	req, _ := http.NewRequest("GET", base+"api/info", nil)
	req.Host = "evil.example:80"
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign Host: %v %v, want 403", resp.StatusCode, err)
	}
	form, _ := http.NewRequest("POST", base+"api/open", strings.NewReader(`{"folder":"C:\\"}`))
	form.Header.Set("Content-Type", "text/plain")
	if resp, err := http.DefaultClient.Do(form); err != nil || resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain write: %v %v, want 415", resp.StatusCode, err)
	}
}

// A save based on an old version is refused with 409 and writes nothing;
// autosaves stay uncommitted until a commit, which leaves outside edits
// alone.
func TestSaveConflictAndPendingCommit(t *testing.T) {
	s, base := start(t)
	root := s.nb.Root()
	if code, out := call(t, "PUT", base+"api/note", `{"path":"a.md","text":"one\n","base":""}`); code != 200 || out["committed"] != false {
		t.Fatalf("autosave: %d %v", code, out)
	}
	os.WriteFile(filepath.Join(root, "b.md"), []byte("agent\n"), 0o644)
	os.WriteFile(filepath.Join(root, "a.md"), []byte("outside\n"), 0o644)
	if code, out := call(t, "PUT", base+"api/note", `{"path":"a.md","text":"mine\n","base":"`+versionOf("one\n")+`"}`); code != http.StatusConflict || out["changed"] != true {
		t.Fatalf("stale save: %d %v, want 409", code, out)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "a.md")); string(got) != "outside\n" {
		t.Fatalf("stale save overwrote the file: %q", got)
	}
	// c.md: autosaved and untouched since, so the pending commit takes it.
	call(t, "PUT", base+"api/note", `{"path":"c.md","text":"c\n","base":""}`)
	s.CommitPending()
	changed, _ := s.repo.Status()
	if strings.Join(changed, ",") != "a.md,b.md" {
		t.Fatalf("after commit: %v, want a.md (changed outside after the app wrote it) and b.md (outside) left for review", changed)
	}
	if !s.repo.Tracked("c.md") {
		t.Fatal("the app's own autosave was not committed")
	}
}

func versionOf(s string) string { return notebook.VersionOf([]byte(s)) }

// Leaving a note commits only what the app wrote to it: an untracked file
// the app merely showed stays uncommitted.
func TestCommitMineSkipsUnwritten(t *testing.T) {
	s, base := start(t)
	os.WriteFile(filepath.Join(s.nb.Root(), "shown.md"), []byte("never edited\n"), 0o644)
	if code, out := call(t, "POST", base+"api/commit", `{"paths":["shown.md"],"mine":true}`); code != 200 || out["committed"] != false {
		t.Fatalf("mine commit of an unwritten file: %d %v", code, out)
	}
	call(t, "PUT", base+"api/note", `{"path":"edited.md","text":"x\n","base":""}`)
	if code, out := call(t, "POST", base+"api/commit", `{"paths":["edited.md"],"mine":true}`); code != 200 || out["committed"] != true {
		t.Fatalf("mine commit of an edited file: %d %v", code, out)
	}
	if s.repo.Tracked("shown.md") || !s.repo.Tracked("edited.md") {
		t.Fatal("want edited.md committed and shown.md not")
	}
}
