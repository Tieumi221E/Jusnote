// Command jusnote is the desktop app and the external interface over the
// same files. Every command works on plain Markdown in a notebook folder
// through internal/service — the same code the editor uses — so an agent
// can do anything the app can without the app running.
//
// The commands are the capability registry (internal/caps, Jus capreg):
// the usage text, `jusnote help -json` and the agent guide are made from
// it. Commands on the notebook run here, on the files; the open window is
// told what changed (events.notify) and shows it at once. The window's own
// commands (which note it shows) go to the window.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tieumi221E/Jus/apps"
	"github.com/Tieumi221E/Jus/capreg"
	"github.com/Tieumi221E/Jus/instance"
	"github.com/Tieumi221E/Jusnote/internal/caps"
	"github.com/Tieumi221E/Jusnote/internal/footprint"
	"github.com/Tieumi221E/Jusnote/internal/jusbase/appdir"
	"github.com/Tieumi221E/Jusnote/internal/server"
	"github.com/Tieumi221E/Jusnote/internal/service"
)

// version is set by the build (-ldflags -X main.version=...).
var version = "0.0.0-dev"

// Exit codes, part of the interface (help -json lists them).
const (
	exitOK       = capreg.ExitOK
	exitFail     = capreg.ExitFail
	exitUsage    = capreg.ExitUsage
	exitConflict = capreg.ExitConflict
)

type notebookKey struct{}

// newRegistry is every command, on the command line: a command works on
// the notebook it names (-notebook, in the context), and the window's own
// commands are handed to the window.
func newRegistry(_ func() *server.Server) *capreg.Registry {
	return registry(func(ctx context.Context, create bool) (*service.Service, error) {
		dir, _ := ctx.Value(notebookKey{}).(string)
		if dir == "" {
			dir = "."
		}
		svc, err := service.Open(dir, create)
		if err == nil {
			if _, e := os.Stat(svc.NB.Vault()); e == nil {
				if d, e := dataDir(); e == nil {
					footprint.Remember(d, svc.NB.Root())
				}
			}
		}
		return svc, err
	}, nil, "")
}

// windowRegistry is every command in the window: on its open notebook.
func windowRegistry(srv *server.Server, data string) *capreg.Registry {
	return registry(func(ctx context.Context, create bool) (*service.Service, error) {
		svc := srv.Notebook()
		if svc == nil {
			return nil, errors.New("no notebook is open")
		}
		if create && svc.Repo == nil {
			return service.Open(svc.NB.Root(), true)
		}
		return svc, nil
	}, func() *server.Server { return srv }, data)
}

// registry is every command; data is the app's own folder ("": the usual one).
func registry(open func(context.Context, bool) (*service.Service, error), getServer func() *server.Server, data string) *capreg.Registry {
	r := capreg.New("jusnote", version)
	exe, err := os.Executable()
	if err != nil {
		exe = "jusnote"
	}
	if data == "" {
		data, _ = dataDir()
	}
	caps.Register(r, caps.Env{Exe: filepath.Clean(exe), Open: open, Data: data})
	r.Add(openCap())
	apps.RegisterManifest(r, func() apps.Manifest {
		return apps.Manifest{ID: "jusnote", Name: "Jusnote", Version: version, Exe: apps.Exe(),
			Links: []string{"jus://note/"},
			Formats: []apps.Format{
				{Name: "notebook", Version: 1, Doc: "Markdown files with git history; .jusnote/ beside them"},
				{Name: "notebook-settings", Version: 1, Doc: ".jusnote/settings.json"},
				{Name: "record-type", Version: 1, Doc: ".jusnote/types/*.yaml"},
				{Name: "skill", Version: 1, Doc: ".jusnote/skills/<name>/SKILL.md, skill.json"},
			},
			Footprint: footprint.Places(apps.Exe(), data)}
	})
	server.RegisterWindow(r, getServer)
	return r
}

func main() {
	defer reportStats()
	if len(os.Args) < 2 {
		if err := cmdGUI(nil); err != nil {
			fail(err)
		}
		return
	}
	switch os.Args[1] {
	case "version", "-version", "--version":
		fmt.Println("jusnote", version)
		return
	case "help", "-h", "--help", "-help":
		os.Exit(cmdHelp(os.Args[2:], os.Stdout))
	case "gui":
		if err := cmdGUI(os.Args[2:]); err != nil {
			fail(err)
		}
		return
	}
	nb, args := notebookFlag(os.Args[1:])
	if len(args) >= 2 && args[0] == "history" && (args[1] == "log" || args[1] == "status") {
		args = args[1:] // 0.1.0 spelling: history log | history status
	}
	reg := newRegistry(nil)
	c, rest, ok := reg.Resolve(args)
	if !ok {
		fmt.Fprintf(os.Stderr, "jusnote: unknown command %q (jusnote help)\n", args[0])
		os.Exit(exitUsage)
	}
	code := runCap(reg, c, rest, nb, os.Stdin, os.Stdout, os.Stderr)
	reportStats() // os.Exit skips the deferred call
	os.Exit(code)
}

// notebookFlag takes -notebook <dir> (or -notebook=<dir>) out of args;
// "" when it is not given (the commands then use the current folder).
func notebookFlag(args []string) (string, []string) {
	dir := ""
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-notebook" || a == "--notebook":
			if i+1 < len(args) {
				dir = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-notebook=") || strings.HasPrefix(a, "--notebook="):
			_, dir, _ = strings.Cut(a, "=")
		default:
			rest = append(rest, a)
		}
	}
	return dir, rest
}

// dataDir is the app's own folder, where a running window announces itself.
func dataDir() (string, error) {
	if d := os.Getenv("JUSNOTE_DATA"); d != "" {
		return filepath.Abs(d)
	}
	d, _, err := appdir.Dir("jusnote")
	return d, err
}

// runCap runs one command and returns the exit code.
func runCap(reg *capreg.Registry, c *capreg.Cap, args []string, nb string, stdin *os.File, stdout, stderr io.Writer) int {
	inv, err := c.ParseCLI(args, filepath.Abs)
	if err != nil {
		return report(err, inv.JSON, stderr)
	}
	if err := c.FillStdin(inv.Params, stdin, "file"); err != nil {
		return report(err, inv.JSON, stderr)
	}
	ctx := capreg.WithSource(context.WithValue(context.Background(), notebookKey{}, nb), inv.Source)
	if c.Window || c.Streams {
		return runInWindow(c, inv, stdout, stderr)
	}
	b, err := json.Marshal(inv.Params)
	if err != nil {
		return report(err, inv.JSON, stderr)
	}
	v, err := reg.Call(ctx, c.ID, b)
	if err != nil {
		return report(err, inv.JSON, stderr)
	}
	if c.Writes {
		notifyWindow(nb, c.ID, inv.Source)
	}
	c.PrintResult(stdout, v, inv.JSON)
	return exitOK
}

// runInWindow hands a window command to the running window.
func runInWindow(c *capreg.Cap, inv capreg.Invocation, stdout, stderr io.Writer) int {
	dir, err := dataDir()
	if err != nil {
		return report(err, inv.JSON, stderr)
	}
	rm, _, ok := instance.Find(dir)
	if !ok {
		return report(errors.New("this needs the Jusnote window, and none is open (jusnote gui)"), inv.JSON, stderr)
	}
	rm.Source = inv.Source
	if c.Streams {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		if err := rm.Stream(context.Background(), c.ID, inv.Params, func(v any) error { return enc.Encode(v) }); err != nil {
			return report(err, inv.JSON, stderr)
		}
		return exitOK
	}
	v, err := rm.Call(context.Background(), c.ID, inv.Params)
	if err != nil {
		return report(err, inv.JSON, stderr)
	}
	c.PrintResult(stdout, v, inv.JSON)
	return exitOK
}

// notifyWindow tells an open window that the command line changed its
// notebook, so it shows the change (and who made it) at once.
func notifyWindow(nb, id string, src capreg.Source) {
	dir, err := dataDir()
	if err != nil {
		return
	}
	rm, _, ok := instance.Find(dir)
	if !ok {
		return
	}
	root, err := filepath.Abs(nb)
	if err != nil {
		return
	}
	rm.Source = src
	rm.Call(context.Background(), "events.notify", map[string]any{"notebook": root, "cap": id})
}

// report prints err (with -json as {"error", "kind", "code"} on stderr)
// and returns its exit code; without a console it also shows a message box.
func report(err error, asJSON bool, stderr io.Writer) int {
	if asJSON {
		json.NewEncoder(stderr).Encode(capreg.ErrorOut(err))
	} else {
		e := capreg.AsError(err)
		fmt.Fprintln(stderr, "jusnote:", e.Msg)
		if e.Plan != nil {
			b, _ := json.MarshalIndent(e.Plan, "", "  ")
			fmt.Fprintf(stderr, "it would do:\n%s\n", b)
		}
		showError(err)
	}
	return capreg.ExitCode(err)
}

// fail reports an error of the window (gui) and exits.
func fail(err error) {
	os.Exit(report(err, false, os.Stderr))
}

const guideNotes = `Every command: -notebook DIR (default "."), -json (machine-readable output).
Who is writing: -author human|agent -harness <name> -model <provider/model> -run <session id> (or JUS_AUTHOR, JUS_HARNESS, JUS_MODEL, JUS_RUN);
with -no-commit the source is remembered and used when the user accepts the change.
Content: -file <path> (or a pipe) is safest; long -text through a shell may lose quotes and backticks.
The window: (no arguments) | gui [-notebook DIR] opens the editor.
open <file>[:line] | open <folder> shows it in the window (starting one, or switching its notebook to the one the file is in).

`

func cmdHelp(args []string, stdout io.Writer) int {
	reg := newRegistry(nil)
	for _, a := range args {
		if a == "-json" || a == "--json" {
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			enc.SetEscapeHTML(false)
			enc.Encode(reg.Help(
				"Plain-Markdown notebook with git history. Every command takes -notebook DIR (default: the current folder).",
				"A commit made with -author/-harness/-model/-run carries them as Jus-* trailers; log -session <id> lists one session's, revert -session <id> -yes undoes them without undoing anyone else's.",
				"The notebook's own guide for agents is AGENTS.md (jusnote agents writes it).",
			))
			return exitOK
		}
	}
	reg.Usage(stdout, guideNotes)
	return exitOK
}
