package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tieumi221E/Jus/conform"
)

// The CLI as an agent meets it: a built binary, run in a notebook folder.
var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "jusnote-cli")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "jusnote.exe")
	// A window of the person's own (if one is open) is not these tests' to tell.
	os.Setenv("JUSNOTE_DATA", filepath.Join(dir, "jusnote-data")) // named as a release names it (conformance checks the footprint)
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	code           int
	stdout, stderr string
}

func run(t *testing.T, nb string, env []string, stdin string, args ...string) result {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = nb
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	code := 0
	var x *exec.ExitError
	if errors.As(err, &x) {
		code = x.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result{code, o.String(), e.String()}
}

func must(t *testing.T, r result) result {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
	}
	return r
}

func decodeJSON(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
}

func TestAgentWorkflow(t *testing.T) {
	nb := t.TempDir()
	must(t, run(t, nb, nil, "", "init"))
	must(t, run(t, nb, nil, "", "write", "ideas.md", "-text", "# Ideas\n"))

	// Read, then write based on that version, without committing.
	var note struct{ Text, Version string }
	decodeJSON(t, must(t, run(t, nb, nil, "", "read", "ideas.md", "-json")).stdout, &note)
	agent := []string{"JUS_AUTHOR=agent", "JUS_MODEL=anthropic/claude", "JUS_RUN=run-7"}
	must(t, run(t, nb, agent, note.Text+"- one more\n", "write", "ideas.md", "-base", note.Version, "-no-commit", "-json"))

	// A second write from the stale version is a conflict: exit 3, JSON error.
	r := run(t, nb, agent, "overwrite\n", "write", "ideas.md", "-base", note.Version, "-json")
	if r.code != exitConflict || !strings.Contains(r.stderr, `"code":3`) {
		t.Fatalf("stale write: exit %d, stderr %s", r.code, r.stderr)
	}

	// The person sees the change with its source, accepts it.
	var changes []struct {
		Path   string
		Kind   string
		Source struct{ Author, Model, Run string }
	}
	decodeJSON(t, must(t, run(t, nb, nil, "", "status", "-json")).stdout, &changes)
	if len(changes) != 1 || changes[0].Kind != "modified" || changes[0].Source.Author != "agent" || changes[0].Source.Run != "run-7" {
		t.Fatalf("status %+v", changes)
	}
	if d := must(t, run(t, nb, nil, "", "diff", "ideas.md")).stdout; !strings.Contains(d, "+- one more") {
		t.Fatalf("diff:\n%s", d)
	}
	must(t, run(t, nb, nil, "", "commit", "ideas.md"))
	var log []struct {
		Message string
		Source  struct{ Author, Model string }
	}
	decodeJSON(t, must(t, run(t, nb, nil, "", "log", "ideas.md", "-json")).stdout, &log)
	if len(log) != 2 || log[0].Source.Author != "agent" || log[0].Source.Model != "anthropic/claude" || log[1].Source.Author != "" {
		t.Fatalf("log %+v", log)
	}
}

func TestCaptureCheckLinksRename(t *testing.T) {
	nb := t.TempDir()
	must(t, run(t, nb, nil, "", "init"))
	r := must(t, run(t, nb, nil, "", "capture", "-section", "身体", "-text", "体重：六十", "-date", "2026-09-27", "-json"))
	var cap struct {
		Path        string
		Diagnostics []struct{ Line int }
	}
	decodeJSON(t, r.stdout, &cap)
	if cap.Path != "2026-09.md" || len(cap.Diagnostics) == 0 {
		t.Fatalf("capture %+v (a non-number weight should be flagged)", cap)
	}
	var chk struct {
		Checked, OK bool
		Type        string
	}
	decodeJSON(t, must(t, run(t, nb, nil, "", "check", "2026-09.md", "-json")).stdout, &chk)
	if !chk.Checked || chk.OK || chk.Type != "daily-log" {
		t.Fatalf("check %+v", chk)
	}

	must(t, run(t, nb, nil, "", "write", "a.md", "-text", "see [[b]]\n"))
	must(t, run(t, nb, nil, "", "write", "b.md", "-text", "# B\n"))
	var ln struct{ Back []struct{ From string } }
	decodeJSON(t, must(t, run(t, nb, nil, "", "links", "b.md", "-json")).stdout, &ln)
	if len(ln.Back) != 1 || ln.Back[0].From != "a.md" {
		t.Fatalf("backlinks %+v", ln)
	}
	must(t, run(t, nb, nil, "", "rename", "b.md", "archive/b2.md"))
	if got, _ := os.ReadFile(filepath.Join(nb, "a.md")); string(got) != "see [[b2]]\n" {
		t.Fatalf("link after rename: %q", got)
	}
	if s := must(t, run(t, nb, nil, "", "status")).stdout; strings.TrimSpace(s) != "" {
		t.Fatalf("rename should commit everything it changed; status:\n%s", s)
	}
	if r := run(t, nb, nil, "", "search", "B"); r.code != 0 || !strings.Contains(r.stdout, "archive/b2.md:1: # B") {
		t.Fatalf("search: %+v", r)
	}
}

func TestHelpJSONAndUsage(t *testing.T) {
	var h struct {
		Commands []struct {
			Name   string
			Writes bool
		}
		Exit map[string]string
	}
	decodeJSON(t, must(t, run(t, t.TempDir(), nil, "", "help", "-json")).stdout, &h)
	names := map[string]bool{}
	for _, c := range h.Commands {
		names[c.Name] = true
	}
	for _, want := range []string{"search", "check", "capture", "rename", "delete", "status", "diff", "discard", "commit", "log", "show", "restore", "links", "agents"} {
		if !names[want] {
			t.Errorf("help -json lacks %q", want)
		}
	}
	if h.Exit["3"] == "" {
		t.Error("help -json lacks the exit codes")
	}
	if r := run(t, t.TempDir(), nil, "", "read"); r.code != exitUsage {
		t.Fatalf("missing argument: exit %d, want %d", r.code, exitUsage)
	}
	if r := run(t, t.TempDir(), nil, "", "nosuch"); r.code != exitUsage {
		t.Fatalf("unknown command: exit %d", r.code)
	}
}

func TestAgentsKeepsTheUsersFiles(t *testing.T) {
	nb := t.TempDir()
	must(t, run(t, nb, nil, "", "init"))
	os.WriteFile(filepath.Join(nb, "CLAUDE.md"), []byte("my own rules\n"), 0o644)
	var res struct {
		Files []struct{ Path, Action string }
	}
	decodeJSON(t, must(t, run(t, nb, nil, "", "agents", "-json")).stdout, &res)
	got := map[string]string{}
	for _, f := range res.Files {
		got[f.Path] = f.Action
	}
	if got["AGENTS.md"] != "written" || got["GEMINI.md"] != "written" || got["CLAUDE.md"] != "kept" {
		t.Fatalf("agents: %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(nb, "CLAUDE.md")); string(b) != "my own rules\n" {
		t.Fatal("the user's CLAUDE.md was overwritten")
	}
	guide, _ := os.ReadFile(filepath.Join(nb, "AGENTS.md"))
	for _, want := range []string{"-no-commit", "-author agent", "help -json", "daily-log", bin} {
		if !strings.Contains(string(guide), want) {
			t.Errorf("AGENTS.md lacks %q", want)
		}
	}
	decodeJSON(t, must(t, run(t, nb, nil, "", "agents", "-json")).stdout, &res)
	for _, f := range res.Files {
		if f.Path == "AGENTS.md" && f.Action != "unchanged" {
			t.Fatalf("second run: AGENTS.md %s", f.Action)
		}
	}
}

// Agents' shells often run commands with an empty, non-terminal stdin: that
// is no input, never "write an empty note" or "check an empty text".
func TestEmptyStdinIsNoInput(t *testing.T) {
	nb := t.TempDir()
	must(t, run(t, nb, nil, "", "init"))
	must(t, run(t, nb, nil, "", "write", "a.md", "-text", "keep\n"))
	if r := run(t, nb, nil, "", "write", "a.md"); r.code != exitUsage {
		t.Fatalf("write with empty stdin: exit %d, want %d", r.code, exitUsage)
	}
	if b, _ := os.ReadFile(filepath.Join(nb, "a.md")); string(b) != "keep\n" {
		t.Fatalf("note changed by an empty stdin: %q", b)
	}
	must(t, run(t, nb, nil, "", "write", "empty.md", "-text", ""))
	if b, err := os.ReadFile(filepath.Join(nb, "empty.md")); err != nil || len(b) != 0 {
		t.Fatalf("explicit -text \"\" should write an empty note: %q %v", b, err)
	}
	if s := must(t, run(t, nb, nil, "", "status")).stdout; strings.TrimSpace(s) != "" {
		t.Fatalf("init + committed writes should leave status clean:\n%s", s)
	}
}

// append keeps the note byte for byte (the failure a cold-start agent hit:
// rewriting a whole note through PowerShell lost its quotes and backticks);
// -file carries any bytes; record puts the agent's name on its own edits.
func TestAppendFileAndRecord(t *testing.T) {
	nb := t.TempDir()
	must(t, run(t, nb, nil, "", "init"))
	orig := "# Ideas\n\n- 把\"记录类型\"做成 `jus://` 配置"
	os.WriteFile(filepath.Join(nb, "ideas.md"), []byte(orig), 0o644)
	must(t, run(t, nb, nil, "", "commit", "ideas.md"))

	tmp := filepath.Join(t.TempDir(), "add.txt")
	os.WriteFile(tmp, []byte("- 新的 `想法` \"引号\""), 0o644)
	agent := []string{"JUS_AUTHOR=agent", "JUS_MODEL=test/model", "JUS_RUN=r1"}
	must(t, run(t, nb, agent, "", "append", "ideas.md", "-file", tmp, "-no-commit"))
	got, _ := os.ReadFile(filepath.Join(nb, "ideas.md"))
	if want := orig + "\n- 新的 `想法` \"引号\"\n"; string(got) != want {
		t.Fatalf("append:\n got %q\nwant %q", got, want)
	}

	// An edit made with the agent's own tools, then recorded.
	os.WriteFile(filepath.Join(nb, "own.md"), []byte("edited directly\n"), 0o644)
	if r := run(t, nb, nil, "", "record", "own.md"); r.code != exitUsage {
		t.Fatalf("record without -author: exit %d", r.code)
	}
	must(t, run(t, nb, agent, "", "record", "own.md"))
	var cs []struct {
		Path   string
		Source struct{ Author, Run string }
	}
	decodeJSON(t, must(t, run(t, nb, nil, "", "status", "-json")).stdout, &cs)
	for _, c := range cs {
		if c.Source.Author != "agent" || c.Source.Run != "r1" {
			t.Fatalf("status %+v: every change should carry the agent's name", cs)
		}
	}
	must(t, run(t, nb, nil, "", "commit", "-all"))
	var log []struct{ Source struct{ Author string } }
	decodeJSON(t, must(t, run(t, nb, nil, "", "log", "own.md", "-json")).stdout, &log)
	if len(log) == 0 || log[0].Source.Author != "agent" {
		t.Fatalf("accepted commit of own.md lacks the agent: %+v", log)
	}
}

// The remaining commands, as an agent would chain them.
func TestDeleteDiscardShowRestoreTypesAttach(t *testing.T) {
	nb := t.TempDir()
	must(t, run(t, nb, nil, "", "init"))
	must(t, run(t, nb, nil, "", "write", "a.md", "-text", "v1\n"))
	must(t, run(t, nb, nil, "", "write", "a.md", "-text", "v2\n"))

	var log []struct{ Hash string }
	decodeJSON(t, must(t, run(t, nb, nil, "", "log", "a.md", "-json")).stdout, &log)
	if len(log) != 2 {
		t.Fatalf("log %+v", log)
	}
	if s := must(t, run(t, nb, nil, "", "show", log[1].Hash[:8], "a.md")).stdout; s != "v1\n" {
		t.Fatalf("show: %q", s)
	}
	must(t, run(t, nb, nil, "", "restore", log[1].Hash, "a.md"))
	if b, _ := os.ReadFile(filepath.Join(nb, "a.md")); string(b) != "v1\n" {
		t.Fatalf("restore: %q", b)
	}

	os.WriteFile(filepath.Join(nb, "a.md"), []byte("unwanted\n"), 0o644)
	must(t, run(t, nb, nil, "", "discard", "a.md"))
	if b, _ := os.ReadFile(filepath.Join(nb, "a.md")); string(b) != "v1\n" {
		t.Fatalf("discard: %q", b)
	}

	must(t, run(t, nb, nil, "", "delete", "a.md"))
	if _, err := os.Stat(filepath.Join(nb, "a.md")); !os.IsNotExist(err) {
		t.Fatal("delete left the file")
	}

	var types []struct{ ID string }
	decodeJSON(t, must(t, run(t, nb, nil, "", "types", "-json")).stdout, &types)
	if len(types) < 2 {
		t.Fatalf("types %+v", types)
	}

	img := filepath.Join(t.TempDir(), "photo 1.png")
	os.WriteFile(img, []byte("\x89PNG"), 0o644)
	must(t, run(t, nb, nil, "", "write", "notes/trip.md", "-text", "# Trip\n"))
	var at struct{ Path, Markdown string }
	decodeJSON(t, must(t, run(t, nb, nil, "", "attach", "notes/trip.md", img, "-json")).stdout, &at)
	if at.Path != "notes/attachments/photo 1.png" || at.Markdown != "![photo 1.png](attachments/photo%201.png)" {
		t.Fatalf("attach %+v", at)
	}
	if s := must(t, run(t, nb, nil, "", "status")).stdout; strings.TrimSpace(s) != "" {
		t.Fatalf("every command above commits what it changed; status:\n%s", s)
	}
}

// Two agent sessions and a person edit the notebook; one session is undone:
// its changes go back, the other session's stay, and a file the person
// changed since is reported, not overwritten.
func TestRevertASession(t *testing.T) {
	nb := t.TempDir()
	must(t, run(t, nb, nil, "", "init"))
	must(t, run(t, nb, nil, "", "write", "a.md", "-text", "a0\n"))
	must(t, run(t, nb, nil, "", "write", "b.md", "-text", "b0\n"))
	must(t, run(t, nb, nil, "", "write", "c.md", "-text", "c0\n"))
	as := func(session string) []string {
		return []string{"JUS_AUTHOR=agent", "JUS_HARNESS=test-harness", "JUS_RUN=" + session}
	}
	must(t, run(t, nb, as("A"), "", "write", "a.md", "-text", "a1 by A\n"))
	must(t, run(t, nb, as("A"), "", "write", "new.md", "-text", "made by A\n"))
	must(t, run(t, nb, as("B"), "", "write", "b.md", "-text", "b1 by B\n"))
	must(t, run(t, nb, as("A"), "", "write", "c.md", "-text", "c1 by A\n"))
	must(t, run(t, nb, nil, "", "write", "c.md", "-text", "c2 by the person\n"))

	var cs []struct {
		Source struct{ Harness, Run string }
	}
	decodeJSON(t, must(t, run(t, nb, nil, "", "log", "-session", "A", "-json")).stdout, &cs)
	if len(cs) != 3 || cs[0].Source.Harness != "test-harness" {
		t.Fatalf("log -session A: %+v", cs)
	}
	// The plan first; then for real.
	r := run(t, nb, nil, "", "revert", "-session", "A", "-json")
	if r.code != exitUsage || !strings.Contains(r.stderr, `"plan"`) {
		t.Fatalf("revert without -yes: %d %s", r.code, r.stderr)
	}
	var res struct {
		Steps     []struct{ Path, State string }
		Committed bool
	}
	decodeJSON(t, must(t, run(t, nb, nil, "", "revert", "-session", "A", "-yes", "-json")).stdout, &res)
	states := map[string]string{}
	for _, s := range res.Steps {
		states[s.Path] = s.State
	}
	if states["a.md"] != "done" || states["new.md"] != "done" || states["c.md"] != "conflict" || !res.Committed {
		t.Fatalf("revert steps %+v", res)
	}
	read := func(p string) string { b, _ := os.ReadFile(filepath.Join(nb, p)); return string(b) }
	if read("a.md") != "a0\n" || read("b.md") != "b1 by B\n" || read("c.md") != "c2 by the person\n" {
		t.Fatalf("after revert: a=%q b=%q c=%q", read("a.md"), read("b.md"), read("c.md"))
	}
	if _, err := os.Stat(filepath.Join(nb, "new.md")); !os.IsNotExist(err) {
		t.Fatal("the note session A made is still there")
	}
	if s := must(t, run(t, nb, nil, "", "status")).stdout; strings.TrimSpace(s) != "" {
		t.Fatalf("the revert is committed; status:\n%s", s)
	}
}

// A notebook's skill: listed, refused until confirmed (the plan says what
// it runs), then run in the notebook; what it writes through jusnote is
// marked as the skill's.
func TestSkills(t *testing.T) {
	nb := t.TempDir()
	must(t, run(t, nb, nil, "", "init"))
	dir := filepath.Join(nb, ".jusnote", "skills", "digest")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: digest\ndescription: writes a digest note\n---\n"), 0o644)
	spec, _ := json.Marshal(map[string]any{"run": []string{bin, "write", "digest.md", "-text", "# Digest\n"}})
	os.WriteFile(filepath.Join(dir, "skill.json"), spec, 0o644)

	var list []struct {
		Name    string `json:"name"`
		Trusted bool   `json:"trusted"`
	}
	decodeJSON(t, must(t, run(t, nb, nil, "", "skills", "list", "-json")).stdout, &list)
	if len(list) != 1 || list[0].Name != "digest" || list[0].Trusted {
		t.Fatalf("skills list: %+v", list)
	}
	r := run(t, nb, nil, "", "skills", "run", "digest", "-json")
	var refusal struct {
		Kind string `json:"kind"`
		Plan struct {
			Command []string `json:"command"`
		} `json:"plan"`
	}
	decodeJSON(t, r.stderr, &refusal)
	if r.code == 0 || refusal.Kind != "confirm" || len(refusal.Plan.Command) == 0 || refusal.Plan.Command[0] != bin {
		t.Fatalf("unconfirmed run: exit %d %s", r.code, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(nb, "digest.md")); err == nil {
		t.Fatal("the skill ran without -yes")
	}
	var res struct {
		Exit int `json:"exit"`
	}
	decodeJSON(t, must(t, run(t, nb, nil, "", "skills", "run", "digest", "-yes", "-json")).stdout, &res)
	if res.Exit != 0 {
		t.Fatalf("skill exit %d", res.Exit)
	}
	must(t, run(t, nb, nil, "", "skills", "run", "digest", "-json")) // confirmed: no -yes needed now
	// A skill that fails is a failed call.
	bad := filepath.Join(nb, ".jusnote", "skills", "bad")
	os.MkdirAll(bad, 0o755)
	os.WriteFile(filepath.Join(bad, "SKILL.md"), []byte("---\nname: bad\n---\n"), 0o644)
	spec, _ = json.Marshal(map[string]any{"run": []string{bin, "no-such-command"}})
	os.WriteFile(filepath.Join(bad, "skill.json"), spec, 0o644)
	if r := run(t, nb, nil, "", "skills", "run", "bad", "-yes", "-json"); r.code != 1 || !strings.Contains(r.stderr, "exited 2") {
		t.Fatalf("failing skill: exit %d, stderr %s", r.code, r.stderr)
	}
	log := must(t, run(t, nb, nil, "", "log", "-json")).stdout
	if !strings.Contains(log, "skill/digest") {
		t.Fatalf("the skill's commit is not marked as its own:\n%s", log)
	}
	if _, err := os.Stat(filepath.Join(nb, ".jusnote", "out", "digest", "last.log")); err != nil {
		t.Fatal("no output folder:", err)
	}
	if st := must(t, run(t, nb, nil, "", "status", "-json")).stdout; strings.Contains(st, "out/") {
		t.Fatalf("the output folder shows as a change: %s", st)
	}
}

// link: the editor's jus:// link, from the command line.
func TestLink(t *testing.T) {
	nb := t.TempDir()
	for args, want := range map[[3]string]string{
		{"link", "读书/某 书.md", "-line=1"}: "jus://note/%E8%AF%BB%E4%B9%A6/%E6%9F%90%20%E4%B9%A6.md",
		{"link", "a.md", "-line=12"}:     "jus://note/a.md?line=12",
	} {
		if got := strings.TrimSpace(must(t, run(t, nb, nil, "", args[:]...)).stdout); got != want {
			t.Errorf("%v = %s, want %s", args, got, want)
		}
	}
}

// The Jus conformance test (the Jus module's conform), on this build.
func TestConformance(t *testing.T) {
	rep := conform.Run(context.Background(), bin, t.TempDir(), nil)
	for _, c := range rep.Checks {
		if !c.OK {
			t.Errorf("%s: %s", c.Name, c.Detail)
		}
	}
	if len(rep.Checks) < 10 {
		t.Fatalf("only %d checks ran", len(rep.Checks))
	}
}

// link preview: a note here, and another app's link when that app is not
// installed (it says so; it is not an error).
func TestLinkPreview(t *testing.T) {
	nb := t.TempDir()
	must(t, run(t, nb, nil, "", "write", "a.md", "-text", "# Title A\n\ntext\n"))
	var p map[string]any
	decodeJSON(t, must(t, run(t, nb, nil, "", "link", "preview", "jus://note/a.md?line=2", "-json")).stdout, &p)
	if p["title"] != "Title A" || p["exists"] != true || p["line"] != float64(2) {
		t.Fatalf("note preview: %v", p)
	}
	decodeJSON(t, must(t, run(t, nb, []string{"JUS_JUSZZZ_EXE="}, "", "link", "preview", "jus://zzz/x", "-json")).stdout, &p)
	if p["app"] != "juszzz" || p["installed"] != false {
		t.Fatalf("missing app: %v", p)
	}
	if r := run(t, nb, nil, "", "link", "preview", "https://example.com"); r.code != 2 {
		t.Fatalf("not a jus link: exit %d", r.code)
	}
}
