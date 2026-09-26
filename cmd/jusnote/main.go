// Command jusnote is the desktop app and the external interface (CLI, and
// later MCP) over the same files. Every command works on plain Markdown in
// a notebook folder, so an agent can do anything the app can without the
// app running: read, append, check and commit.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Tieumi221E/Jusnote/internal/history"
	"github.com/Tieumi221E/Jusnote/internal/notebook"
)

// version is set by the build (-ldflags -X main.version=...).
var version = "0.0.0-dev"

func main() {
	if len(os.Args) < 2 {
		if err := cmdGUI(nil); err != nil {
			fail(err)
		}
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "version", "-version", "--version":
		fmt.Println("jusnote", version)
		return
	case "help", "-h", "--help":
		usage()
		return
	case "gui":
		err = cmdGUI(args)
	case "info":
		err = cmdInfo(args)
	case "list":
		err = cmdList(args)
	case "read":
		err = cmdRead(args)
	case "write":
		err = cmdWrite(args)
	case "init":
		err = cmdInit(args)
	case "history":
		err = cmdHistory(args)
	default:
		fmt.Fprintf(os.Stderr, "jusnote: unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fail(err)
	}
}

// fail reports err where it can be seen (stderr, or a message box when
// started without a console) and exits.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "jusnote:", err)
	showError(err)
	os.Exit(1)
}

func usage() {
	fmt.Fprint(os.Stderr, `jusnote — plain-text notes, backed by git.

Usage:
  jusnote <command> [flags] [args]

Commands:
  gui              open the editor window (default with no command)
  info             show the notebook and its git state
  list             list notes
  read <path>      print a note
  write <path>     write a note (content from -text or stdin), then commit
  init             create the notebook's git repository
  history log      show recent commits
  history status   show changed notes
  version          print the version

Common flags:
  -notebook DIR    notebook folder (default ".")
  -json            machine-readable output
`)
}

// common holds the flags every command shares.
type common struct {
	fs       *flag.FlagSet
	notebook *string
	json     *bool
}

func newFlags(name string) *common {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return &common{
		fs:       fs,
		notebook: fs.String("notebook", ".", "notebook folder"),
		json:     fs.Bool("json", false, "print machine-readable JSON"),
	}
}

// parse allows flags before, between or after positional arguments, the
// way a person is likely to type them.
func parse(fs *flag.FlagSet, args []string, npos int) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
	if len(pos) != npos {
		return nil, fmt.Errorf("want %d argument(s), got %d", npos, len(pos))
	}
	return pos, nil
}

func cmdInfo(args []string) error {
	c := newFlags("info")
	if _, err := parse(c.fs, args, 0); err != nil {
		return err
	}
	nb, err := notebook.Open(*c.notebook)
	if err != nil {
		return err
	}
	docs, err := nb.List()
	if err != nil {
		return err
	}
	isRepo := history.IsRepo(nb.Root())
	info := map[string]any{
		"root":  nb.Root(),
		"vault": nb.Vault(),
		"notes": len(docs),
		"git":   isRepo,
	}
	if isRepo {
		if repo, err := history.Open(nb.Root()); err == nil {
			if changed, err := repo.Status(); err == nil {
				info["changed"] = len(changed)
			}
		}
	}
	if *c.json {
		return printJSON(info)
	}
	fmt.Printf("notebook  %s\n", nb.Root())
	fmt.Printf("vault     %s\n", nb.Vault())
	fmt.Printf("notes     %d\n", len(docs))
	fmt.Printf("git       %v\n", isRepo)
	if n, ok := info["changed"]; ok {
		fmt.Printf("changed   %v\n", n)
	}
	return nil
}

func cmdList(args []string) error {
	c := newFlags("list")
	if _, err := parse(c.fs, args, 0); err != nil {
		return err
	}
	nb, err := notebook.Open(*c.notebook)
	if err != nil {
		return err
	}
	docs, err := nb.List()
	if err != nil {
		return err
	}
	if *c.json {
		return printJSON(docs)
	}
	for _, d := range docs {
		fmt.Println(d.Rel)
	}
	return nil
}

func cmdRead(args []string) error {
	c := newFlags("read")
	pos, err := parse(c.fs, args, 1)
	if err != nil {
		return err
	}
	nb, err := notebook.Open(*c.notebook)
	if err != nil {
		return err
	}
	data, err := nb.Read(pos[0])
	if err != nil {
		return err
	}
	if *c.json {
		return printJSON(map[string]string{"rel": pos[0], "text": string(data)})
	}
	_, err = os.Stdout.Write(data)
	return err
}

func cmdWrite(args []string) error {
	c := newFlags("write")
	text := c.fs.String("text", "", "content to write (otherwise read standard input)")
	message := c.fs.String("m", "", "commit message")
	noCommit := c.fs.Bool("no-commit", false, "do not commit after writing")
	pos, err := parse(c.fs, args, 1)
	if err != nil {
		return err
	}
	rel := pos[0]

	var data []byte
	if *text != "" {
		data = []byte(*text)
	} else {
		piped, err := isPiped(os.Stdin)
		if err != nil {
			return err
		}
		if !piped {
			return fmt.Errorf("no content: pass -text or pipe it in")
		}
		if data, err = io.ReadAll(os.Stdin); err != nil {
			return err
		}
	}

	nb, err := notebook.Open(*c.notebook)
	if err != nil {
		return err
	}
	clean, err := nb.Write(rel, data)
	if err != nil {
		return err
	}

	committed, hash := false, ""
	if !*noCommit && history.IsRepo(nb.Root()) {
		repo, err := history.Open(nb.Root())
		if err != nil {
			return err
		}
		msg := *message
		if msg == "" {
			msg = "update " + clean
		}
		h, ok, err := repo.CommitPaths(msg, clean)
		if err != nil {
			return err
		}
		committed, hash = ok, h
	}

	if *c.json {
		return printJSON(map[string]any{"rel": clean, "committed": committed, "hash": hash})
	}
	line := "wrote " + clean
	if committed {
		line += " (committed " + short(hash) + ")"
	}
	fmt.Println(line)
	return nil
}

func cmdInit(args []string) error {
	c := newFlags("init")
	if _, err := parse(c.fs, args, 0); err != nil {
		return err
	}
	nb, err := notebook.Open(*c.notebook)
	if err != nil {
		return err
	}
	repo, err := history.Ensure(nb.Root())
	if err != nil {
		return err
	}
	if *c.json {
		return printJSON(map[string]string{"root": repo.Root()})
	}
	fmt.Println("repository ready at", repo.Root())
	return nil
}

func cmdHistory(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("history needs a subcommand: log or status")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "log":
		return cmdHistoryLog(rest)
	case "status":
		return cmdHistoryStatus(rest)
	default:
		return fmt.Errorf("unknown history subcommand %q", sub)
	}
}

func cmdHistoryLog(args []string) error {
	c := newFlags("history log")
	limit := c.fs.Int("limit", 20, "maximum commits to show")
	if _, err := parse(c.fs, args, 0); err != nil {
		return err
	}
	if !history.IsRepo(*c.notebook) {
		return fmt.Errorf("no git repository in %s", *c.notebook)
	}
	repo, err := history.Open(*c.notebook)
	if err != nil {
		return err
	}
	commits, err := repo.Log(*limit)
	if err != nil {
		return err
	}
	if *c.json {
		return printJSON(commits)
	}
	for _, cm := range commits {
		fmt.Printf("%s  %s  %s\n", short(cm.Hash), cm.When.Format("2006-01-02 15:04"), cm.Message)
	}
	return nil
}

func cmdHistoryStatus(args []string) error {
	c := newFlags("history status")
	if _, err := parse(c.fs, args, 0); err != nil {
		return err
	}
	if !history.IsRepo(*c.notebook) {
		return fmt.Errorf("no git repository in %s", *c.notebook)
	}
	repo, err := history.Open(*c.notebook)
	if err != nil {
		return err
	}
	changed, err := repo.Status()
	if err != nil {
		return err
	}
	if *c.json {
		return printJSON(changed)
	}
	for _, p := range changed {
		fmt.Println(p)
	}
	return nil
}

func isPiped(f *os.File) (bool, error) {
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeCharDevice == 0, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func short(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}
