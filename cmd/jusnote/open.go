package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Tieumi221E/Jus/capreg"
	"github.com/Tieumi221E/Jus/instance"
	"github.com/Tieumi221E/Jusnote/internal/juslink"
)

// openCap is `jusnote open <file>[:line]` (or a folder, or a jus://note
// link): show it in the window, starting one when none is open and
// switching the open one's notebook when the file is in another. The
// notebook is -notebook when given, else the nearest folder above the file
// that is one (has .jusnote or .git), else the file's own folder.
func openCap() capreg.Cap {
	return capreg.Cap{ID: "open", Summary: "show a file (at :line), a folder, or a jus://note link in the editor: starts the window when none is open, switches its notebook when the file is in another",
		Params: []capreg.Param{{Name: "target", Kind: capreg.String, Required: true, Positional: true, Doc: "a file, file:line, a folder, or jus://note/<path>?line=<n>"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			nb, _ := ctx.Value(notebookKey{}).(string) // "": not given
			return openTarget(ctx, a.String("target"), nb)
		},
		Text: func(w io.Writer, v any) {
			m := v.(map[string]any)
			if p, ok := m["path"]; ok {
				fmt.Fprintf(w, "opened %v in %v\n", p, m["notebook"])
			} else {
				fmt.Fprintf(w, "opened %v\n", m["notebook"])
			}
		}}
}

func openTarget(ctx context.Context, target, nb string) (map[string]any, error) {
	explicit := nb != ""
	if strings.HasPrefix(strings.ToLower(target), "jus://note/") {
		l, err := juslink.Parse(target)
		if err != nil {
			return nil, capreg.Usagef("open: %v", err)
		}
		base := nb
		if !explicit {
			base = "."
			if rm, _, ok := findWindow(); ok {
				if v, err := rm.Call(ctx, "info", nil); err == nil {
					if m, ok := v.(map[string]any); ok && m["root"] != nil {
						base, explicit = fmt.Sprint(m["root"]), true
					}
				}
			}
		}
		nb = base
		target = filepath.Join(base, filepath.FromSlash(l.Target))
		if n := l.Line(); n > 0 {
			target += ":" + strconv.Itoa(n)
		}
	}
	file, line := splitLine(target)
	file, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(file)
	if err != nil {
		return nil, capreg.NotFoundf("no file %s", file)
	}
	var root string
	switch {
	case explicit:
		root, _ = filepath.Abs(nb)
	case fi.IsDir():
		root = file
	default:
		root = notebookOf(filepath.Dir(file))
	}
	rel := ""
	if !fi.IsDir() {
		r, err := filepath.Rel(root, file)
		if err != nil || strings.HasPrefix(r, "..") {
			return nil, capreg.Usagef("open: %s is not in the notebook %s", file, root)
		}
		rel = filepath.ToSlash(r)
	}

	rm, started, err := windowFor(root)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"notebook": root, "started": started}
	if !started {
		v, err := rm.Call(ctx, "notebook.open", map[string]any{"folder": root})
		if err != nil {
			return nil, err
		}
		out["switched"] = v.(map[string]any)["switched"]
	}
	if rel != "" {
		p := map[string]any{"path": rel}
		if line > 0 {
			p["line"] = line
		}
		// A window just started may not have its page listening yet.
		deadline := time.Now().Add(15 * time.Second)
		for {
			v, err := rm.Call(ctx, "editor.open", p)
			if err == nil {
				out["state"] = v
				break
			}
			if !started || time.Now().After(deadline) || !strings.Contains(err.Error(), "no window page") {
				return nil, err
			}
			time.Sleep(150 * time.Millisecond)
		}
		out["path"] = rel
	}
	return out, nil
}

func notebookGiven(args []string) bool {
	for _, a := range args {
		if a == "-notebook" || a == "--notebook" || strings.HasPrefix(a, "-notebook=") || strings.HasPrefix(a, "--notebook=") {
			return true
		}
	}
	return false
}

// findWindow is the running window, if one is.
func findWindow() (capreg.Remote, instance.Info, bool) {
	dir, err := dataDir()
	if err != nil {
		return capreg.Remote{}, instance.Info{}, false
	}
	return instance.Find(dir)
}

// lineSuffix is a trailing ":<line>" (a drive's "C:" is not one: it has no digits after it).
var lineSuffix = regexp.MustCompile(`^(.+):(\d+)$`)

// splitLine reads "notes/a.md:12" as the file and line 12 (0: no line).
func splitLine(s string) (string, int) {
	if m := lineSuffix.FindStringSubmatch(s); m != nil {
		if _, err := os.Stat(s); err != nil { // not a file whose name ends so
			n, _ := strconv.Atoi(m[2])
			return m[1], n
		}
	}
	return s, 0
}

// notebookOf is the nearest folder from dir up that is a notebook (.jusnote)
// or a repository (.git), else dir itself.
func notebookOf(dir string) string {
	for d := dir; ; {
		for _, mark := range []string{".jusnote", ".git"} {
			if fi, err := os.Stat(filepath.Join(d, mark)); err == nil && (fi.IsDir() || mark == ".git") {
				return d
			}
		}
		up := filepath.Dir(d)
		if up == d {
			return dir
		}
		d = up
	}
}

// windowFor is the running window, or a new one started on root (started).
func windowFor(root string) (capreg.Remote, bool, error) {
	dir, err := dataDir()
	if err != nil {
		return capreg.Remote{}, false, err
	}
	if rm, _, ok := instance.Find(dir); ok {
		return rm, false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return capreg.Remote{}, false, err
	}
	cmd := exec.Command(exe, "gui", "-notebook", root, "-data", dir)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return capreg.Remote{}, false, fmt.Errorf("could not start the window: %w", err)
	}
	cmd.Process.Release()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if rm, _, ok := instance.Find(dir); ok {
			return rm, true, nil
		}
	}
	return capreg.Remote{}, false, errors.New("the window did not start in time")
}
