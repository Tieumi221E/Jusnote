// Command jusnote is the desktop app and the external interface over the
// same files. Every command works on plain Markdown in a notebook folder
// through internal/service — the same code the editor uses — so an agent
// can do anything the app can without the app running.
//
// The command table below is the single description of the interface: the
// usage text and `jusnote help -json` (for agents) are both made from it.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Tieumi221E/Jusnote/internal/history"
	"github.com/Tieumi221E/Jusnote/internal/notebook"
	"github.com/Tieumi221E/Jusnote/internal/service"
)

// version is set by the build (-ldflags -X main.version=...).
var version = "0.0.0-dev"

// Exit codes, part of the interface (help -json lists them).
const (
	exitOK       = 0
	exitFail     = 1
	exitUsage    = 2
	exitConflict = 3
)

type command struct {
	Name    string   `json:"name"`
	Args    string   `json:"args,omitempty"`
	Summary string   `json:"summary"`
	Flags   []string `json:"flags,omitempty"`
	Writes  bool     `json:"writes"` // changes files in the notebook
	run     func(c *ctx) error
}

// Flags shared by the commands that write.
var provFlags = []string{"-author human|agent", "-model <provider>/<model>", "-run <session id>"}

func commands() []command {
	return []command{
		{Name: "gui", Summary: "open the editor window (the default with no command)", Flags: []string{"-serve", "-selftest"}, run: nil},
		{Name: "info", Summary: "the notebook and its git state", run: cmdInfo},
		{Name: "list", Summary: "list notes (path, size, modified)", run: cmdList},
		{Name: "read", Args: "<path>", Summary: "print a note; -json adds its version for write -base", run: cmdRead},
		{Name: "write", Args: "<path>", Summary: "write a note from -text or stdin (atomic), then commit it", Writes: true,
			Flags: append([]string{"-text <content>", "-file <path>", "-base <version>", "-m <message>", "-no-commit"}, provFlags...), run: cmdWrite},
		{Name: "append", Args: "<path>", Summary: "add text at the end of a note (the rest is left byte for byte); creates the note if missing", Writes: true,
			Flags: append([]string{"-text <content>", "-file <path>", "-no-commit"}, provFlags...), run: cmdAppend},
		{Name: "attach", Args: "<note> <file>", Summary: "copy a file (e.g. an image) next to a note, into attachments/; prints the Markdown to insert", Writes: true,
			Flags: append([]string{"-name <file name>", "-no-commit"}, provFlags...), run: cmdAttach},
		{Name: "record", Args: "<paths...>", Summary: "after editing files with your own tools: record who changed them, for the commit that accepts the change",
			Flags: provFlags, run: cmdRecord},
		{Name: "capture", Summary: "append one entry to a record type's file (e.g. today's log)", Writes: true,
			Flags: append([]string{"-type <id>", "-section <name>", "-text <entry>", "-file <path>", "-date YYYY-MM-DD", "-no-commit"}, provFlags...), run: cmdCapture},
		{Name: "search", Args: "<query>", Summary: "lines containing the query in every note (ignoring case)", Flags: []string{"-limit <n>"}, run: cmdSearch},
		{Name: "check", Args: "<path>", Summary: "check a note against its record type (the file, or -text / -file / stdin)", Flags: []string{"-text <content>", "-file <path>"}, run: cmdCheck},
		{Name: "types", Summary: "the record types of this notebook: files, sections and rules", run: cmdTypes},
		{Name: "links", Args: "<path>", Summary: "a note's links and the notes that link to it", run: cmdLinks},
		{Name: "rename", Args: "<from> <to>", Summary: "move a note and update the links to it", Writes: true,
			Flags: append([]string{"-no-links", "-no-commit"}, provFlags...), run: cmdRename},
		{Name: "delete", Args: "<path>", Summary: "delete a note (its last content stays in .jusnote/backup)", Writes: true,
			Flags: append([]string{"-no-commit"}, provFlags...), run: cmdDelete},
		{Name: "status", Summary: "uncommitted files, with kind and recorded source", run: cmdStatus},
		{Name: "diff", Args: "<path>", Summary: "a file against its last commit", run: cmdDiff},
		{Name: "discard", Args: "<path>", Summary: "put a file back as it was at the last commit", Writes: true, run: cmdDiscard},
		{Name: "commit", Args: "[paths...]", Summary: "commit the named files (or -all)", Writes: true,
			Flags: append([]string{"-all", "-m <message>"}, provFlags...), run: cmdCommit},
		{Name: "log", Args: "[path]", Summary: "recent commits (of one note when a path is given)", Flags: []string{"-limit <n>"}, run: cmdLog},
		{Name: "show", Args: "<commit> <path>", Summary: "a note as it was in a commit", run: cmdShow},
		{Name: "restore", Args: "<commit> <path>", Summary: "make a note's old version its current content, then commit", Writes: true,
			Flags: append([]string{"-no-commit"}, provFlags...), run: cmdRestore},
		{Name: "agents", Summary: "write AGENTS.md (and CLAUDE.md, GEMINI.md bridges) so coding agents know how to work here", Writes: true,
			Flags: []string{"-force"}, run: cmdAgents},
		{Name: "init", Summary: "create the notebook's git repository", Writes: true, run: cmdInit},
		{Name: "help", Summary: "this list; -json for machines", run: nil},
		{Name: "version", Summary: "print the version", run: nil},
	}
}

func main() {
	defer reportStats()
	if len(os.Args) < 2 {
		if err := cmdGUI(nil); err != nil {
			fail(err, false)
		}
		return
	}
	name, args := os.Args[1], os.Args[2:]
	switch name {
	case "version", "-version", "--version":
		fmt.Println("jusnote", version)
		return
	case "help", "-h", "--help", "-help":
		help(hasFlag(args, "-json"))
		return
	case "gui":
		if err := cmdGUI(args); err != nil {
			fail(err, false)
		}
		return
	case "history": // 0.1.0 spelling: history log | history status
		if len(args) > 0 && (args[0] == "log" || args[0] == "status") {
			name, args = args[0], args[1:]
		}
	}
	for _, c := range commands() {
		if c.Name == name && c.run != nil {
			x := newCtx(c.Name, args)
			if err := c.run(x); err != nil {
				reportStats() // fail exits without running defers
				fail(err, *x.json)
			}
			return
		}
	}
	fmt.Fprintf(os.Stderr, "jusnote: unknown command %q (jusnote help)\n", name)
	os.Exit(exitUsage)
}

func hasFlag(args []string, f string) bool {
	for _, a := range args {
		if a == f || a == "-"+f {
			return true
		}
	}
	return false
}

func help(asJSON bool) {
	cs := commands()
	if asJSON {
		printJSON(map[string]any{
			"name":     "jusnote",
			"version":  version,
			"about":    "Plain-Markdown notebook with git history. Every command takes -notebook DIR (default: the current folder) and -json.",
			"commands": cs,
			"exit":     map[string]string{"0": "ok", "1": "failed", "2": "usage error", "3": "conflict: the file changed since -base"},
			"env":      map[string]string{"JUS_AUTHOR": "default for -author", "JUS_MODEL": "default for -model", "JUS_RUN": "default for -run"},
			"errors":   "with -json, a failure prints {\"error\": \"…\", \"code\": n} on stderr",
		})
		return
	}
	var b strings.Builder
	b.WriteString("jusnote — plain-Markdown notes with git history.\n\nUsage:\n  jusnote <command> [args] [flags]\n\nCommands:\n")
	for _, c := range cs {
		fmt.Fprintf(&b, "  %-24s %s\n", strings.TrimSpace(c.Name+" "+c.Args), c.Summary)
		if len(c.Flags) > 0 {
			fmt.Fprintf(&b, "  %-24s %s\n", "", strings.Join(c.Flags, "  "))
		}
	}
	b.WriteString("\nEvery command: -notebook DIR (default \".\"), -json (machine-readable output).\n")
	b.WriteString("Writes by an agent: pass -author agent -model … -run … (or set JUS_AUTHOR/JUS_MODEL/JUS_RUN);\n")
	b.WriteString("with -no-commit the source is remembered and used when the user accepts the change.\n")
	b.WriteString("Content: -file <path> (or a pipe) is safest; long -text through a shell may lose quotes and backticks.\n")
	b.WriteString("Exit codes: 0 ok, 1 failed, 2 usage, 3 conflict.\n")
	fmt.Print(b.String())
}

// fail reports err where it can be seen — JSON on stderr for machines, a
// line on stderr, or a message box when started without a console — and
// exits with the error's code.
func fail(err error, asJSON bool) {
	code := exitFail
	var u usageError
	switch {
	case errors.As(err, &u):
		code = exitUsage
	case errors.Is(err, notebook.ErrChanged):
		code = exitConflict
	}
	if asJSON {
		json.NewEncoder(os.Stderr).Encode(map[string]any{"error": err.Error(), "code": code})
	} else {
		fmt.Fprintln(os.Stderr, "jusnote:", err)
		showError(err)
	}
	os.Exit(code)
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// ctx is one command's flags and the notebook they point at.
type ctx struct {
	fs       *flag.FlagSet
	name     string
	args     []string
	notebook *string
	json     *bool
	author   *string
	model    *string
	run      *string
	noCommit *bool
	pos      []string
}

func newCtx(name string, args []string) *ctx {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return &ctx{
		fs: fs, name: name, args: args,
		notebook: fs.String("notebook", ".", "notebook folder"),
		json:     fs.Bool("json", false, "machine-readable output"),
		author:   fs.String("author", os.Getenv("JUS_AUTHOR"), "who writes: human or agent"),
		model:    fs.String("model", os.Getenv("JUS_MODEL"), "the agent's model"),
		run:      fs.String("run", os.Getenv("JUS_RUN"), "the agent's session or run id"),
		noCommit: fs.Bool("no-commit", false, "write but do not commit"),
	}
}

// parse reads the flags (anywhere among the arguments) and wants between
// min and max positional arguments (max < 0: any number).
func (c *ctx) parse(min, max int) error {
	args := c.args
	for {
		if err := c.fs.Parse(args); err != nil {
			return usageError{c.name + ": " + err.Error()}
		}
		args = c.fs.Args()
		if len(args) == 0 {
			break
		}
		c.pos = append(c.pos, args[0])
		args = args[1:]
	}
	if len(c.pos) < min || (max >= 0 && len(c.pos) > max) {
		return usageError{fmt.Sprintf("%s: wrong number of arguments (jusnote help)", c.name)}
	}
	if a := *c.author; a != "" && a != "human" && a != "agent" {
		return usageError{"-author must be human or agent"}
	}
	return nil
}

func (c *ctx) prov() history.Provenance {
	return history.Provenance{Author: *c.author, Model: *c.model, Run: *c.run}
}

func (c *ctx) open() (*service.Service, error) {
	return service.Open(*c.notebook, false)
}

// out prints v as JSON with -json, and text otherwise.
func (c *ctx) out(v any, text func()) error {
	if *c.json {
		return printJSON(v)
	}
	text()
	return nil
}

// commitOrRecord commits paths now, or with -no-commit remembers who wrote
// them for the commit that later accepts the change.
func (c *ctx) commitOrRecord(svc *service.Service, subject string, paths ...string) (hash string, committed bool, err error) {
	if *c.noCommit || svc.Repo == nil {
		for _, p := range paths {
			if svc.NB.Exists(p) {
				if err := svc.Record(p, c.prov()); err != nil {
					return "", false, err
				}
			}
		}
		return "", false, nil
	}
	return svc.Commit(subject, c.prov(), paths...)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func short(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

func isPiped(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice == 0
}

// input is the -text flag when it was given (even empty), otherwise what
// is piped into standard input — but an empty pipe is no input: agents'
// shells often run commands with a closed or empty stdin, and that must
// never turn into "write an empty note".
func (c *ctx) input(text *string) ([]byte, bool, error) {
	if isSet(c, "file") {
		b, err := os.ReadFile(c.fs.Lookup("file").Value.String())
		return b, err == nil, err
	}
	if isSet(c, "text") {
		return []byte(*text), true, nil
	}
	if !isPiped(os.Stdin) {
		return nil, false, nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil || len(b) == 0 {
		return nil, false, err
	}
	return b, true, nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
