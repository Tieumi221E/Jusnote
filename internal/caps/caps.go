// Package caps is everything Jusnote can do, registered once (Jus contract
// 17): the command line, `jusnote help -json`, the agent guide and the
// window's api/cap are all made from it. The operations are
// internal/service's, the same the editor uses.
//
// A notebook is plain Markdown on disk, so the command line works on the
// files itself, window or not; the open window sees what changed (it is
// told, events.notify) and lists it for review. Only what is the window's
// own — which note it shows, where — goes to the window.
package caps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tieumi221E/Jus/apps"
	"github.com/Tieumi221E/Jus/capreg"
	"github.com/Tieumi221E/Jus/skills"
	"github.com/Tieumi221E/Jusnote/internal/history"
	"github.com/Tieumi221E/Jusnote/internal/juslink"
	"github.com/Tieumi221E/Jusnote/internal/notebook"
	"github.com/Tieumi221E/Jusnote/internal/service"
)

// Env is where the capabilities find their notebook.
type Env struct {
	// Open is the notebook a call works on: the one the command line names
	// (-notebook), or the window's. create makes its git repository.
	Open func(ctx context.Context, create bool) (*service.Service, error)
	// Exe is the program as the agent guide names it.
	Exe string
	// Data is the app's own folder (settings, which skills the user trusts).
	Data string
}

// Register adds Jusnote's capabilities to r.
func Register(r *capreg.Registry, env Env) {
	open := func(ctx context.Context) (*service.Service, error) { return env.Open(ctx, false) }
	noCommit := capreg.Param{Name: "no-commit", Kind: capreg.Bool, Doc: "write but do not commit: the change waits for review in the editor (who wrote it is remembered)"}
	path := func(doc string) capreg.Param {
		return capreg.Param{Name: "path", Kind: capreg.String, Required: true, Positional: true, Doc: doc}
	}
	text := capreg.Param{Name: "text", Kind: capreg.String, Stdin: true, Doc: "the content (or -file, or piped in; long text through a shell may lose quotes, prefer -file)"}
	file := capreg.Param{Name: "file", Kind: capreg.Path, Doc: "read the content from this file"}
	add := func(c capreg.Cap) {
		run := c.Run
		c.Run = func(ctx context.Context, a capreg.Args) (any, error) {
			if err := checkSource(ctx); err != nil {
				return nil, err
			}
			v, err := run(ctx, a)
			return v, kindOf(err)
		}
		r.Add(c)
	}

	add(capreg.Cap{ID: "info", Summary: "the notebook and its git state",
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			docs, err := svc.NB.List()
			if err != nil {
				return nil, err
			}
			info := map[string]any{"root": svc.NB.Root(), "vault": svc.NB.Vault(), "notes": len(docs), "git": svc.Repo != nil, "version": r.Version}
			if svc.Repo != nil {
				if changed, err := svc.Repo.Status(); err == nil {
					info["changed"] = len(changed)
				}
			}
			return info, nil
		},
		Text: func(w io.Writer, v any) {
			m := v.(map[string]any)
			fmt.Fprintf(w, "notebook  %v\nnotes     %v\ngit       %v\n", m["root"], m["notes"], m["git"])
			if n, ok := m["changed"]; ok {
				fmt.Fprintf(w, "changed   %v\n", n)
			}
		}})

	add(capreg.Cap{ID: "list", Summary: "list notes (path, size, modified); -all also the other text files (config, scripts, logs), as the editor's file list shows them",
		Params: []capreg.Param{{Name: "all", Kind: capreg.Bool, Doc: "also the other text files, each marked note or not, and why one opens read-only"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			if a.Bool("all") {
				files, err := svc.NB.Files()
				if files == nil {
					files = []notebook.File{}
				}
				return files, err
			}
			docs, err := svc.NB.List()
			if docs == nil {
				docs = []notebook.Doc{}
			}
			return docs, err
		},
		Text: func(w io.Writer, v any) { eachField(w, v, "rel") }})

	add(capreg.Cap{ID: "read", Summary: "print a note; -json adds its version for write -base",
		Params: []capreg.Param{path("the note")},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			data, err := svc.NB.Read(a.String("path"))
			if err != nil {
				return nil, err
			}
			text, enc := notebook.TextOf(data)
			return map[string]string{"path": a.String("path"), "text": text, "encoding": enc, "version": notebook.VersionOf(data)}, nil
		},
		Text: func(w io.Writer, v any) { fmt.Fprint(w, v.(map[string]any)["text"]) }})

	add(capreg.Cap{ID: "write", Summary: "write a note (atomic), then commit it", Writes: true,
		Params: []capreg.Param{path("the note"), text, file,
			{Name: "base", Kind: capreg.String, Doc: "the version this was edited from (read -json); refused with exit 3 if the file changed since"},
			{Name: "m", Kind: capreg.String, Doc: "commit message"}, noCommit},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			data, ok, err := content(a)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, capreg.Usagef("write: no content: pass -text, -file or pipe it in")
			}
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			var clean string
			if a.Has("base") {
				clean, err = svc.NB.WriteIf(a.String("path"), data, a.String("base"))
			} else {
				clean, err = svc.NB.Write(a.String("path"), data)
			}
			if err != nil {
				return nil, err
			}
			subject := a.String("m")
			if subject == "" {
				subject = "update " + clean
			}
			hash, committed, err := commitOrRecord(ctx, svc, a, subject, clean)
			return map[string]any{"path": clean, "version": notebook.VersionOf(data), "committed": committed, "hash": hash}, err
		},
		Text: wroteText("wrote")})

	add(capreg.Cap{ID: "append", Summary: "add text at the end of a note (the rest is left byte for byte); creates the note if missing", Writes: true,
		Params: []capreg.Param{path("the note"), text, file, noCommit},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			more, ok, err := content(a)
			if err != nil {
				return nil, err
			}
			if !ok || len(more) == 0 {
				return nil, capreg.Usagef("append: no text: pass -text, -file or pipe it in")
			}
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			old, rerr := svc.NB.Read(a.String("path"))
			if rerr != nil && !os.IsNotExist(rerr) {
				return nil, rerr
			}
			next := append([]byte{}, old...)
			if len(next) > 0 && next[len(next)-1] != '\n' {
				next = append(next, '\n')
			}
			next = append(next, more...)
			if next[len(next)-1] != '\n' {
				next = append(next, '\n')
			}
			base := ""
			if rerr == nil {
				base = notebook.VersionOf(old)
			}
			clean, err := svc.NB.WriteIf(a.String("path"), next, base)
			if err != nil {
				return nil, err
			}
			hash, committed, err := commitOrRecord(ctx, svc, a, "update "+clean, clean)
			return map[string]any{"path": clean, "version": notebook.VersionOf(next), "committed": committed, "hash": hash}, err
		},
		Text: wroteText("appended to")})

	add(capreg.Cap{ID: "attach", Summary: "copy a file (e.g. an image) next to a note, into attachments/; prints the Markdown to insert", Writes: true,
		Params: []capreg.Param{path("the note"),
			{Name: "source", Kind: capreg.Path, Required: true, Positional: true, Doc: "the file to attach"},
			{Name: "name", Kind: capreg.String, Doc: "file name to store it under (default: the file's own)"}, noCommit},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			data, err := os.ReadFile(a.String("source"))
			if err != nil {
				return nil, err
			}
			name := a.String("name")
			if name == "" {
				name = filepath.Base(a.String("source"))
			}
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			f, link, err := svc.Attach(a.String("path"), name, data)
			if err != nil {
				return nil, err
			}
			md := "[" + strings.NewReplacer("[", "", "]", "").Replace(name) + "](" + link + ")"
			switch strings.ToLower(filepath.Ext(name)) {
			case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".avif":
				md = "!" + md
			}
			hash, committed, err := commitOrRecord(ctx, svc, a, "attach "+f, f)
			return map[string]any{"path": f, "link": link, "markdown": md, "committed": committed, "hash": hash}, err
		},
		Text: func(w io.Writer, v any) { fmt.Fprintln(w, v.(map[string]any)["markdown"]) }})

	add(capreg.Cap{ID: "record", Summary: "after editing files with your own tools: record who changed them, for the commit that accepts the change",
		Params: []capreg.Param{{Name: "paths", Kind: capreg.Strings, Required: true, Positional: true, Doc: "the files changed"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			p := provenance(ctx)
			if p.Author == "" {
				return nil, capreg.Usagef("record: say who made the change: -author agent (and -harness, -model, -run), or JUS_AUTHOR")
			}
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			done := []string{}
			for _, x := range a.Strings("paths") {
				rel, err := svc.Clean(x)
				if err != nil {
					return nil, err
				}
				if err := svc.Record(rel, p); err != nil {
					return nil, fmt.Errorf("%s: %w", rel, err)
				}
				done = append(done, rel)
			}
			return map[string]any{"recorded": done, "source": p}, nil
		},
		Text: func(w io.Writer, v any) {
			m := v.(map[string]any)
			fmt.Fprintf(w, "recorded %d files\n", len(m["recorded"].([]any)))
		}})

	add(capreg.Cap{ID: "capture", Summary: "append one entry to a record type's file (e.g. today's log)", Writes: true,
		Params: []capreg.Param{
			{Name: "type", Kind: capreg.String, Default: "daily-log", Doc: "record type id (jusnote types)"},
			{Name: "section", Kind: capreg.String, Required: true, Doc: "section to append to"},
			text, file,
			{Name: "date", Kind: capreg.String, Doc: "YYYY-MM-DD (default today)"}, noCommit},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			entry, ok, err := content(a)
			if err != nil {
				return nil, err
			}
			if !ok || strings.TrimSpace(string(entry)) == "" {
				return nil, capreg.Usagef("capture: need -section and -text (or -file, or stdin)")
			}
			day := time.Now()
			if a.Has("date") {
				if day, err = time.ParseInLocation("2006-01-02", a.String("date"), time.Local); err != nil {
					return nil, capreg.Usagef("capture: -date must be YYYY-MM-DD")
				}
			}
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			rel, err := svc.Capture(a.String("type"), a.String("section"), string(entry), day)
			if err != nil {
				return nil, err
			}
			b, _ := svc.NB.Read(rel)
			_, diags, _ := svc.Check(rel, b)
			hash, committed, err := commitOrRecord(ctx, svc, a, "log: "+rel, rel)
			return map[string]any{"path": rel, "committed": committed, "hash": hash, "diagnostics": orEmpty(diags)}, err
		},
		Text: wroteText("added to")})

	add(capreg.Cap{ID: "search", Summary: "lines containing the query in every note and other text file (ignoring case)",
		Params: []capreg.Param{{Name: "query", Kind: capreg.Strings, Required: true, Positional: true, Doc: "what to look for (words are joined with spaces)"},
			{Name: "limit", Kind: capreg.Int, Default: int64(100), Doc: "maximum hits"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			hits, err := svc.Search(strings.Join(a.Strings("query"), " "), int(a.Int("limit")))
			return orEmpty(hits), err
		},
		Text: func(w io.Writer, v any) {
			for _, x := range v.([]any) {
				h := x.(map[string]any)
				fmt.Fprintf(w, "%v:%v: %v\n", h["rel"], h["line"], h["text"])
			}
		}})

	add(capreg.Cap{ID: "check", Summary: "check a note against its record type (the file, or -text / -file / stdin)",
		Params: []capreg.Param{path("the note"), text, file},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			data, ok, err := content(a)
			if err != nil {
				return nil, err
			}
			if !ok {
				if data, err = svc.NB.Read(a.String("path")); err != nil {
					return nil, err
				}
			}
			id, diags, matched := svc.Check(a.String("path"), data)
			return map[string]any{"path": a.String("path"), "type": id, "checked": matched, "ok": len(diags) == 0, "diagnostics": orEmpty(diags)}, nil
		},
		Text: func(w io.Writer, v any) {
			m := v.(map[string]any)
			switch {
			case m["checked"] != true:
				fmt.Fprintf(w, "%v: no record type applies; nothing to check\n", m["path"])
			case m["ok"] == true:
				fmt.Fprintf(w, "%v: ok (%v)\n", m["path"], m["type"])
			default:
				printDiags(w, m["path"], m["diagnostics"])
			}
		}})

	add(capreg.Cap{ID: "types", Summary: "the record types of this notebook: files, sections and rules",
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			return orEmpty(svc.Types), nil
		}})

	add(capreg.Cap{ID: "links", Summary: "a note's links and the notes that link to it",
		Params: []capreg.Param{path("the note")},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			rel, err := svc.Clean(a.String("path"))
			if err != nil {
				return nil, err
			}
			return svc.LinksOf(rel, nil)
		}})

	add(capreg.Cap{ID: "rename", Summary: "move a note and update the links to it", Writes: true,
		Params: []capreg.Param{path("the note"), {Name: "to", Kind: capreg.String, Required: true, Positional: true, Doc: "its new path"},
			{Name: "no-links", Kind: capreg.Bool, Doc: "do not rewrite links to the note"}, noCommit},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			from, err := svc.Clean(a.String("path"))
			if err != nil {
				return nil, err
			}
			res, err := svc.Rename(from, a.String("to"), !a.Bool("no-links"))
			if err != nil {
				return nil, err
			}
			hash, committed, err := commitOrRecord(ctx, svc, a, "rename "+from+" to "+res.Path, append([]string{from, res.Path}, res.Updated...)...)
			return map[string]any{"path": res.Path, "updated": orEmpty(res.Updated), "committed": committed, "hash": hash}, err
		},
		Text: wroteText("renamed to")})

	add(capreg.Cap{ID: "delete", Summary: "delete a note (its last content stays in .jusnote/backup)", Writes: true,
		Params: []capreg.Param{path("the note"), noCommit},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			rel, err := svc.Delete(a.String("path"))
			if err != nil {
				return nil, err
			}
			hash, committed, err := commitOrRecord(ctx, svc, a, "delete "+rel, rel)
			return map[string]any{"path": rel, "committed": committed, "hash": hash}, err
		},
		Text: wroteText("deleted")})

	add(capreg.Cap{ID: "status", Summary: "uncommitted files, with kind and recorded source",
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			cs, err := svc.Changes()
			if cs == nil {
				cs = []service.Change{}
			}
			return cs, err
		},
		Text: func(w io.Writer, v any) {
			for _, x := range v.([]any) {
				c := x.(map[string]any)
				src := sourceText(c["source"])
				fmt.Fprintf(w, "%-9v %v%s\n", c["kind"], c["path"], src)
			}
		}})

	add(capreg.Cap{ID: "diff", Summary: "a file against its last commit",
		Params: []capreg.Param{path("the file")},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			return svc.Diff(a.String("path"))
		},
		Text: func(w io.Writer, v any) {
			m := v.(map[string]any)
			if m["eolOnly"] == true {
				fmt.Fprintln(w, "only the line endings changed (CRLF / LF)")
			}
			hunks, _ := m["hunks"].([]any)
			for _, x := range hunks {
				h := x.(map[string]any)
				fmt.Fprintf(w, "@@ -%v,%v +%v,%v @@\n", h["oldStart"], h["oldLines"], h["newStart"], h["newLines"])
				lines, _ := h["lines"].([]any)
				for _, y := range lines {
					l := y.(map[string]any)
					fmt.Fprintf(w, "%v%v\n", l["op"], l["text"])
				}
			}
		}})

	add(capreg.Cap{ID: "discard", Summary: "put a file back as it was at the last commit", Writes: true,
		Params: []capreg.Param{path("the file")},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			if err := svc.Discard(a.String("path")); err != nil {
				return nil, err
			}
			return map[string]any{"path": a.String("path"), "discarded": true}, nil
		},
		Text: func(w io.Writer, v any) { fmt.Fprintf(w, "discarded changes to %v\n", v.(map[string]any)["path"]) }})

	add(capreg.Cap{ID: "commit", Summary: "commit the named files (or -all)", Writes: true,
		Params: []capreg.Param{{Name: "paths", Kind: capreg.Strings, Positional: true, Doc: "the files"},
			{Name: "all", Kind: capreg.Bool, Doc: "every uncommitted file"},
			{Name: "m", Kind: capreg.String, Doc: "commit message"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			if svc.Repo == nil {
				return nil, service.ErrNoRepo
			}
			paths := a.Strings("paths")
			if a.Bool("all") {
				if paths, err = svc.Repo.Status(); err != nil {
					return nil, err
				}
			} else {
				for i, p := range paths {
					if paths[i], err = svc.Clean(p); err != nil {
						return nil, err
					}
				}
			}
			if len(paths) == 0 {
				return nil, capreg.Usagef("commit: name the files, or -all")
			}
			hash, committed, err := svc.Commit(a.String("m"), provenance(ctx), paths...)
			return map[string]any{"paths": paths, "committed": committed, "hash": hash}, err
		},
		Text: func(w io.Writer, v any) {
			m := v.(map[string]any)
			if m["committed"] == true {
				fmt.Fprintf(w, "committed %s (%d files)\n", short(fmt.Sprint(m["hash"])), len(m["paths"].([]any)))
			} else {
				fmt.Fprintln(w, "nothing to commit")
			}
		}})

	add(capreg.Cap{ID: "log", Summary: "recent commits (of one note when a path is given; of one agent session with -session)",
		Params: []capreg.Param{{Name: "path", Kind: capreg.String, Positional: true, Doc: "only this note's"},
			{Name: "limit", Kind: capreg.Int, Default: int64(20), Doc: "maximum commits"},
			{Name: "session", Kind: capreg.String, Doc: "only this session's (the -run / JUS_RUN the commits were made with)"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			if a.Has("session") {
				cs, err := svc.SessionCommits(a.String("session"))
				if err != nil || a.Int("limit") <= 0 || int64(len(cs)) <= a.Int("limit") {
					return cs, err
				}
				return cs[:a.Int("limit")], nil
			}
			return svc.Log(a.String("path"), int(a.Int("limit")))
		},
		Text: func(w io.Writer, v any) {
			for _, x := range v.([]any) {
				c := x.(map[string]any)
				when, _ := time.Parse(time.RFC3339, fmt.Sprint(c["when"]))
				fmt.Fprintf(w, "%s  %s  %v%s\n", short(fmt.Sprint(c["hash"])), when.Local().Format("2006-01-02 15:04"), c["message"], sourceText(c["source"]))
			}
		}})

	add(capreg.Cap{ID: "revert", Summary: "undo one agent session: what its commits changed goes back (newest first), each file only if nothing changed it since; without -yes, says what it would do",
		Writes: true, Confirm: true,
		Params: []capreg.Param{{Name: "session", Kind: capreg.String, Required: true, Doc: "the session id (log -json shows it: source.run)"},
			{Name: "m", Kind: capreg.String, Doc: "commit message (default: revert session <id>)"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			run := a.String("session")
			if !a.Confirmed() {
				plan, _, err := svc.RevertSession(run, false)
				if err != nil {
					return nil, err
				}
				return nil, capreg.NeedConfirm("revert puts these back; add -yes", plan)
			}
			steps, changed, err := svc.RevertSession(run, true)
			if err != nil {
				return nil, err
			}
			hash, committed := "", false
			if len(changed) > 0 {
				msg := a.String("m")
				if msg == "" {
					msg = "revert session " + run
				}
				if hash, committed, err = svc.Commit(msg, provenance(ctx), changed...); err != nil {
					return nil, err
				}
			}
			return map[string]any{"steps": steps, "committed": committed, "hash": hash}, nil
		}})

	add(capreg.Cap{ID: "show", Summary: "a note as it was in a commit",
		Params: []capreg.Param{{Name: "commit", Kind: capreg.String, Required: true, Positional: true, Doc: "the commit (hash, or a unique prefix)"}, path("the note")},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			text, err := svc.Show(a.String("commit"), a.String("path"))
			return map[string]string{"commit": a.String("commit"), "path": a.String("path"), "text": text}, err
		},
		Text: func(w io.Writer, v any) { fmt.Fprint(w, v.(map[string]any)["text"]) }})

	add(capreg.Cap{ID: "restore", Summary: "make a note's old version its current content, then commit", Writes: true,
		Params: []capreg.Param{{Name: "commit", Kind: capreg.String, Required: true, Positional: true, Doc: "the commit"}, path("the note"), noCommit},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			rel, err := svc.Restore(a.String("commit"), a.String("path"))
			if err != nil {
				return nil, err
			}
			hash, committed, err := commitOrRecord(ctx, svc, a, "restore "+rel+" to "+short(a.String("commit")), rel)
			return map[string]any{"path": rel, "committed": committed, "hash": hash}, err
		},
		Text: wroteText("restored")})

	add(capreg.Cap{ID: "agents", Summary: "write AGENTS.md (and CLAUDE.md, GEMINI.md bridges) so coding agents know how to work here", Writes: true,
		Params: []capreg.Param{{Name: "force", Kind: capreg.Bool, Doc: "replace files not written by jusnote"}, noCommit},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			out, written, err := svc.WriteAgentGuide(env.Exe, a.Bool("force"), CommandList(r))
			if err != nil {
				return nil, err
			}
			hash, committed := "", false
			if len(written) > 0 && svc.Repo != nil && !a.Bool("no-commit") {
				if hash, committed, err = svc.Commit("jusnote: agent guide", provenance(ctx), written...); err != nil {
					return nil, err
				}
			}
			return map[string]any{"files": out, "committed": committed, "hash": hash}, nil
		},
		Text: func(w io.Writer, v any) {
			files, _ := v.(map[string]any)["files"].([]any)
			for _, x := range files {
				f := x.(map[string]any)
				note := ""
				if f["action"] == "kept" {
					note = " (already there and not written by jusnote; -force replaces it)"
				}
				fmt.Fprintf(w, "%-9v %v%s\n", f["action"], f["path"], note)
			}
		}})

	add(capreg.Cap{ID: "config.get", Summary: "the notebook's settings (kept in it, .jusnote/settings.json)",
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			return svc.NB.Settings(), nil
		}})

	add(capreg.Cap{ID: "config.set", Summary: "change the notebook's settings: -commit manual keeps the editor from committing on its own (only Ctrl+S and commit do)", Writes: true,
		Params: []capreg.Param{{Name: "commit", Kind: capreg.String, Required: true, Enum: []string{"auto", "manual"}, Doc: "auto: commit what the editor wrote when a note is left and the window closes; manual: only when asked"}, noCommit},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			st := svc.NB.Settings()
			st.Commit = a.String("commit")
			if err := svc.NB.SetSettings(st); err != nil {
				return nil, capreg.Usagef("%v", err)
			}
			rel := ".jusnote/settings.json"
			hash, committed, err := commitOrRecord(ctx, svc, a, "jusnote: settings", rel)
			return map[string]any{"settings": st, "committed": committed, "hash": hash}, err
		}})

	add(capreg.Cap{ID: "link", Summary: "the jus:// link to a note (at -line), as the editor copies it; any Jus app opens it",
		Params: []capreg.Param{path("the note"), {Name: "line", Kind: capreg.Int, Doc: "the line (1 is the first)"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			rel, err := svc.Clean(a.String("path"))
			if err != nil {
				return nil, capreg.Usagef("%v", err)
			}
			line := a.Int("line")
			if line <= 1 {
				line = 0 // the top of the note is the note
			}
			return map[string]any{"link": juslink.NoteLink(rel, int(line)), "path": rel}, nil
		},
		Text: func(w io.Writer, v any) { fmt.Fprintln(w, v.(map[string]any)["link"]) }})

	add(capreg.Cap{ID: "link.preview", Summary: "what a jus:// link points to, for showing it: a note here, or — asked of the Jus app it belongs to (its link info) — say a moment of a video; says when that app is not installed",
		Params: []capreg.Param{{Name: "link", Kind: capreg.String, Required: true, Positional: true, Doc: "a jus:// link"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			link := a.String("link")
			l, err := juslink.Parse(link)
			if err != nil || !strings.HasPrefix(strings.ToLower(link), "jus://") {
				return nil, capreg.Usagef("not a jus:// link: %s", link)
			}
			if l.Kind == juslink.Note {
				svc, err := open(ctx)
				if err != nil {
					return nil, err
				}
				rel, err := svc.Clean(l.Target)
				if err != nil {
					return nil, capreg.Usagef("%v", err)
				}
				out := map[string]any{"app": "jusnote", "installed": true, "path": rel, "exists": svc.NB.Exists(rel), "title": noteTitle(svc, rel)}
				if n := l.Line(); n > 0 {
					out["line"] = n
				}
				return out, nil
			}
			id := "jus" + string(l.Kind)
			exe := apps.Find(id)
			if exe == "" {
				return map[string]any{"app": id, "installed": false}, nil
			}
			b, err := apps.Call(ctx, exe, "link", "info", link)
			if err != nil {
				return map[string]any{"app": id, "installed": true, "problem": err.Error()}, nil
			}
			out := map[string]any{}
			if err := json.Unmarshal(b, &out); err != nil {
				return map[string]any{"app": id, "installed": true, "problem": "its link info is not JSON"}, nil
			}
			out["app"], out["installed"] = id, true
			return out, nil
		}})

	trust := skills.OpenTrust(env.Data)
	add(capreg.Cap{ID: "skills.list", Summary: "the notebook's own tools (.jusnote/skills/<name>: SKILL.md, and skill.json to run it); results go to .jusnote/out/<name>",
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			return skills.List(svc.NB.Vault(), trust)
		},
		Text: func(w io.Writer, v any) {
			ss := capreg.As[[]skills.Skill](v)
			if len(ss) == 0 {
				fmt.Fprintln(w, "no skills (.jusnote/skills/<name>/SKILL.md)")
			}
			for _, s := range ss {
				state := "instructions only"
				switch {
				case s.Problem != "":
					state = s.Problem
				case s.Run != nil && s.Trusted:
					state = "runs"
				case s.Run != nil:
					state = "runs after you confirm it"
				}
				fmt.Fprintf(w, "%-20s %s  (%s)\n", s.Name, s.Description, state)
			}
		}})

	add(capreg.Cap{ID: "skills.run", Summary: "run one of the notebook's skills (its skill.json command, in the notebook); the first time, and after its files change, it needs -yes: without, says what it would run",
		Writes: true, Confirm: true,
		Params: []capreg.Param{{Name: "name", Kind: capreg.String, Required: true, Positional: true, Doc: "the skill (skills list)"},
			{Name: "args", Kind: capreg.Strings, Doc: "more arguments for its command"}},
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			svc, err := open(ctx)
			if err != nil {
				return nil, err
			}
			s, err := skills.Load(svc.NB.Vault(), a.String("name"), trust)
			if err != nil {
				return nil, capreg.NotFoundf("%v", err)
			}
			if s.Problem != "" || s.Run == nil {
				_, err := skills.Run(ctx, s, svc.NB.Root(), nil, nil)
				return nil, capreg.Usagef("%v", err)
			}
			if !s.Trusted {
				if !a.Confirmed() {
					return nil, capreg.NeedConfirm("skill "+s.Name+" runs a program that came with this notebook; add -yes to run it (asked again if its files change)",
						map[string]any{"skill": s.Name, "command": append(s.Command(svc.NB.Root()), a.Strings("args")...), "dir": s.Dir, "out": s.Out, "hash": s.Hash})
				}
				if err := trust.Add(svc.NB.Vault(), s.Name, s.Hash); err != nil {
					return nil, err
				}
				s.Trusted = true
			}
			res, err := skills.Run(ctx, s, svc.NB.Root(), []string{ // Jusnote's own names, beside the series'
				"JUSNOTE_NOTEBOOK=" + svc.NB.Root(), "JUSNOTE_OUT=" + s.Out, "JUSNOTE_SKILL=" + s.Dir, "JUSNOTE_EXE=" + env.Exe,
			}, a.Strings("args"))
			if err == nil && (res.Exit != 0 || res.TimedOut) {
				// A skill that failed is a failed call (exit 1), saying what it last printed.
				last := strings.TrimSpace(res.Tail)
				if i := strings.LastIndexByte(last, '\n'); i >= 0 {
					last = last[i+1:]
				}
				if res.TimedOut {
					return nil, fmt.Errorf("skill %s ran out of time (%s)", s.Name, res.Log)
				}
				return nil, fmt.Errorf("skill %s exited %d: %s (all it printed: %s)", s.Name, res.Exit, last, res.Log)
			}
			return res, err
		},
		Text: func(w io.Writer, v any) {
			r := capreg.As[skills.Result](v)
			fmt.Fprint(w, r.Tail)
			if r.Tail != "" && !strings.HasSuffix(r.Tail, "\n") {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "%s: exit %d in %d ms; %d file(s) in %s\n", r.Skill, r.Exit, r.Ms, len(r.Files), r.Out)
		}})

	add(capreg.Cap{ID: "init", Summary: "create the notebook's git repository", Writes: true,
		Run: func(ctx context.Context, a capreg.Args) (any, error) {
			// As the editor does: create the repository and commit the
			// metadata Jusnote generates, so "status" starts clean.
			svc, err := env.Open(ctx, true)
			if err != nil {
				return nil, err
			}
			return map[string]string{"root": svc.Repo.Root()}, nil
		},
		Text: func(w io.Writer, v any) { fmt.Fprintln(w, "repository ready at", v.(map[string]any)["root"]) }})
}

// ---- helpers ----

// checkSource refuses an -author that is neither human nor agent.
func checkSource(ctx context.Context) error {
	if a := capreg.SourceOf(ctx).Author; a != "" && a != "human" && a != "agent" {
		return capreg.Usagef("-author must be human or agent")
	}
	return nil
}

// provenance is who is calling, as the commit's trailers say it.
func provenance(ctx context.Context) history.Provenance {
	s := capreg.SourceOf(ctx)
	return history.Provenance{Author: s.Author, Harness: s.Harness, Model: s.Model, Run: s.Run}
}

// kindOf gives the service's errors their kind: a changed file is a conflict.
func kindOf(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, notebook.ErrChanged):
		return capreg.Conflictf("%v", err)
	case os.IsNotExist(err):
		return capreg.NotFoundf("%v", err)
	}
	return err
}

// content is -file, else -text (also from standard input).
func content(a capreg.Args) ([]byte, bool, error) {
	if a.Has("file") {
		b, err := os.ReadFile(a.String("file"))
		return b, err == nil, err
	}
	if a.Has("text") {
		return []byte(a.String("text")), true, nil
	}
	return nil, false, nil
}

// commitOrRecord commits paths now, or with -no-commit (or no repository)
// remembers who wrote them for the commit that later accepts the change.
func commitOrRecord(ctx context.Context, svc *service.Service, a capreg.Args, subject string, paths ...string) (string, bool, error) {
	p := provenance(ctx)
	if a.Bool("no-commit") || svc.Repo == nil {
		for _, x := range paths {
			if svc.NB.Exists(x) {
				if err := svc.Record(x, p); err != nil {
					return "", false, err
				}
			}
		}
		return "", false, nil
	}
	return svc.Commit(subject, p, paths...)
}

func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func short(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func wroteText(what string) func(io.Writer, any) {
	return func(w io.Writer, v any) {
		m := v.(map[string]any)
		line := fmt.Sprintf("%s %v", what, m["path"])
		if m["committed"] == true {
			line += " (committed " + short(fmt.Sprint(m["hash"])) + ")"
		} else {
			line += " (not committed)"
		}
		fmt.Fprintln(w, line)
		if d, ok := m["diagnostics"]; ok {
			printDiags(w, m["path"], d)
		}
	}
}

func printDiags(w io.Writer, path, diags any) {
	ds, _ := diags.([]any)
	for _, x := range ds {
		d := x.(map[string]any)
		fmt.Fprintf(w, "%v:%v: %v: %v\n", path, d["line"], d["severity"], d["message"])
	}
}

func eachField(w io.Writer, v any, field string) {
	xs, _ := v.([]any)
	for _, x := range xs {
		fmt.Fprintln(w, x.(map[string]any)[field])
	}
}

func sourceText(v any) string {
	m, _ := v.(map[string]any)
	if m == nil || m["author"] == nil {
		return ""
	}
	parts := []string{fmt.Sprint(m["author"])}
	for _, k := range []string{"harness", "model", "run"} {
		if s, ok := m[k].(string); ok && s != "" {
			parts = append(parts, s)
		}
	}
	return "  [" + strings.Join(parts, " ") + "]"
}

// noteTitle is a note's first heading, or its name.
func noteTitle(svc *service.Service, rel string) string {
	if b, err := os.ReadFile(filepath.Join(svc.NB.Root(), filepath.FromSlash(rel))); err == nil {
		for _, line := range strings.SplitN(string(b), "\n", 40) {
			if t, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok && strings.TrimSpace(t) != "" {
				return strings.TrimSpace(t)
			}
		}
	}
	return strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
}

// CommandList is every command of r as the agent guide lists them: one
// line each, its synopsis and what it does; window ones say so.
func CommandList(r *capreg.Registry) string {
	var b strings.Builder
	for _, c := range r.All() {
		note := ""
		if c.Window {
			note = " (in the open window)"
		}
		fmt.Fprintf(&b, "- `%s` — %s%s\n", c.Synopsis(), c.Summary, note)
	}
	return b.String()
}
