package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tieumi221E/Jusnote/internal/history"
	"github.com/Tieumi221E/Jusnote/internal/notebook"
	"github.com/Tieumi221E/Jusnote/internal/recordtype"
	"github.com/Tieumi221E/Jusnote/internal/service"
)

func cmdInfo(c *ctx) error {
	if err := c.parse(0, 0); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	docs, err := svc.NB.List()
	if err != nil {
		return err
	}
	info := map[string]any{"root": svc.NB.Root(), "vault": svc.NB.Vault(), "notes": len(docs), "git": svc.Repo != nil, "version": version}
	if svc.Repo != nil {
		if changed, err := svc.Repo.Status(); err == nil {
			info["changed"] = len(changed)
		}
	}
	return c.out(info, func() {
		fmt.Printf("notebook  %s\nnotes     %d\ngit       %v\n", svc.NB.Root(), len(docs), svc.Repo != nil)
		if n, ok := info["changed"]; ok {
			fmt.Printf("changed   %v\n", n)
		}
	})
}

func cmdList(c *ctx) error {
	if err := c.parse(0, 0); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	docs, err := svc.NB.List()
	if err != nil {
		return err
	}
	if docs == nil {
		docs = []notebook.Doc{}
	}
	return c.out(docs, func() {
		for _, d := range docs {
			fmt.Println(d.Rel)
		}
	})
}

func cmdRead(c *ctx) error {
	if err := c.parse(1, 1); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	data, err := svc.NB.Read(c.pos[0])
	if err != nil {
		return err
	}
	return c.out(map[string]string{"path": c.pos[0], "text": string(data), "version": notebook.VersionOf(data)}, func() {
		os.Stdout.Write(data)
	})
}

func cmdWrite(c *ctx) error {
	c.fs.String("file", "", "read the content from this file")
	text := c.fs.String("text", "", "content (otherwise standard input)")
	base := c.fs.String("base", "", "the version this was edited from (from read -json); refuse if the file changed since")
	message := c.fs.String("m", "", "commit message")
	if err := c.parse(1, 1); err != nil {
		return err
	}
	data, ok, err := c.input(text)
	if err != nil {
		return err
	}
	if !ok {
		return usageError{"write: no content: pass -text or pipe it in"}
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	var clean string
	if isSet(c, "base") {
		clean, err = svc.NB.WriteIf(c.pos[0], data, *base)
	} else {
		clean, err = svc.NB.Write(c.pos[0], data)
	}
	if err != nil {
		return err
	}
	subject := *message
	if subject == "" {
		subject = "update " + clean
	}
	hash, committed, err := c.commitOrRecord(svc, subject, clean)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"path": clean, "version": notebook.VersionOf(data), "committed": committed, "hash": hash}, func() {
		fmt.Println(wrote("wrote "+clean, committed, hash))
	})
}

func isSet(c *ctx, name string) bool {
	set := false
	c.fs.Visit(func(f *flagT) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func wrote(what string, committed bool, hash string) string {
	if committed {
		return what + " (committed " + short(hash) + ")"
	}
	return what + " (not committed)"
}

func cmdCapture(c *ctx) error {
	c.fs.String("file", "", "read the content from this file")
	typ := c.fs.String("type", "daily-log", "record type id (jusnote types)")
	section := c.fs.String("section", "", "section to append to")
	text := c.fs.String("text", "", "the entry (otherwise standard input)")
	date := c.fs.String("date", "", "YYYY-MM-DD (default today)")
	if err := c.parse(0, 0); err != nil {
		return err
	}
	entry, ok, err := c.input(text)
	if err != nil {
		return err
	}
	if !ok || strings.TrimSpace(string(entry)) == "" || *section == "" {
		return usageError{"capture: need -section and -text (or stdin)"}
	}
	day := time.Now()
	if *date != "" {
		if day, err = time.ParseInLocation("2006-01-02", *date, time.Local); err != nil {
			return usageError{"capture: -date must be YYYY-MM-DD"}
		}
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	rel, err := svc.Capture(*typ, *section, string(entry), day)
	if err != nil {
		return err
	}
	_, diags, _ := svc.Check(rel, mustRead(svc.NB, rel))
	hash, committed, err := c.commitOrRecord(svc, "log: "+rel, rel)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"path": rel, "committed": committed, "hash": hash, "diagnostics": orEmpty(diags)}, func() {
		fmt.Println(wrote("added to "+rel+" · "+*section, committed, hash))
		printDiags(rel, diags)
	})
}

func mustRead(nb *notebook.Notebook, rel string) []byte {
	b, _ := nb.Read(rel)
	return b
}

func cmdSearch(c *ctx) error {
	limit := c.fs.Int("limit", 100, "maximum hits")
	if err := c.parse(1, -1); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	hits, err := svc.Search(strings.Join(c.pos, " "), *limit)
	if err != nil {
		return err
	}
	return c.out(orEmpty(hits), func() {
		for _, h := range hits {
			fmt.Printf("%s:%d: %s\n", h.Rel, h.Line, h.Text)
		}
	})
}

func cmdCheck(c *ctx) error {
	c.fs.String("file", "", "read the content from this file")
	text := c.fs.String("text", "", "content to check instead of the file")
	if err := c.parse(1, 1); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	data, ok, err := c.input(text)
	if err != nil {
		return err
	}
	if !ok {
		if data, err = svc.NB.Read(c.pos[0]); err != nil {
			return err
		}
	}
	id, diags, matched := svc.Check(c.pos[0], data)
	return c.out(map[string]any{"path": c.pos[0], "type": id, "checked": matched, "ok": len(diags) == 0, "diagnostics": orEmpty(diags)}, func() {
		switch {
		case !matched:
			fmt.Println(c.pos[0] + ": no record type applies; nothing to check")
		case len(diags) == 0:
			fmt.Println(c.pos[0] + ": ok (" + id + ")")
		default:
			printDiags(c.pos[0], diags)
		}
	})
}

func printDiags(rel string, ds []recordtype.Diagnostic) {
	for _, d := range ds {
		fmt.Printf("%s:%d: %s: %s\n", rel, d.Line, d.Severity, d.Message)
	}
}

func cmdTypes(c *ctx) error {
	if err := c.parse(0, 0); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	types := svc.Types
	if types == nil {
		types = []*recordtype.Type{}
	}
	return c.out(types, func() {
		for _, t := range types {
			fmt.Printf("%s (%s)", t.ID, t.Name)
			if t.File != "" {
				fmt.Printf("  file %s", t.File)
			}
			if t.Unit != "" {
				fmt.Printf("  unit %q", t.Unit)
			}
			fmt.Println()
			for _, s := range t.Sections {
				fmt.Printf("  - %s [%s]", s.Name, s.Kind)
				var keys []string
				for _, f := range s.Fields {
					k := f.Key
					if f.Type != "" {
						k += ":" + f.Type
					}
					keys = append(keys, k)
				}
				if len(keys) > 0 {
					fmt.Printf(" %s", strings.Join(keys, ", "))
				}
				fmt.Println()
			}
		}
		fmt.Println("\nRules: .jusnote/types/*.yaml. Check a note with `jusnote check <path>`.")
	})
}

func cmdLinks(c *ctx) error {
	if err := c.parse(1, 1); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	rel, err := svc.Clean(c.pos[0])
	if err != nil {
		return err
	}
	l, err := svc.LinksOf(rel, nil)
	if err != nil {
		return err
	}
	return c.out(l, func() {
		fmt.Println("links from " + rel + ":")
		for _, x := range l.Out {
			t := x.Target
			if t == "" {
				t = "(no such note)"
			}
			fmt.Printf("  %d: %s -> %s\n", x.Line, x.Raw, t)
		}
		fmt.Println("links to " + rel + ":")
		for _, b := range l.Back {
			fmt.Printf("  %s:%d: %s\n", b.From, b.Line, b.Text)
		}
	})
}

func cmdRename(c *ctx) error {
	noLinks := c.fs.Bool("no-links", false, "do not rewrite links to the note")
	if err := c.parse(2, 2); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	from, err := svc.Clean(c.pos[0])
	if err != nil {
		return err
	}
	res, err := svc.Rename(from, c.pos[1], !*noLinks)
	if err != nil {
		return err
	}
	hash, committed, err := c.commitOrRecord(svc, "rename "+from+" to "+res.Path, append([]string{from, res.Path}, res.Updated...)...)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"path": res.Path, "updated": res.Updated, "committed": committed, "hash": hash}, func() {
		line := "renamed " + from + " -> " + res.Path
		if len(res.Updated) > 0 {
			line += fmt.Sprintf("; links updated in %d notes", len(res.Updated))
		}
		fmt.Println(wrote(line, committed, hash))
	})
}

func cmdDelete(c *ctx) error {
	if err := c.parse(1, 1); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	rel, err := svc.Delete(c.pos[0])
	if err != nil {
		return err
	}
	hash, committed, err := c.commitOrRecord(svc, "delete "+rel, rel)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"path": rel, "committed": committed, "hash": hash}, func() {
		fmt.Println(wrote("deleted "+rel, committed, hash))
	})
}

func needRepo(svcRepo *history.Repo) error {
	if svcRepo == nil {
		return errors.New("this notebook has no git repository (run `jusnote init`)")
	}
	return nil
}

func cmdStatus(c *ctx) error {
	if err := c.parse(0, 0); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	if err := needRepo(svc.Repo); err != nil {
		return err
	}
	cs, err := svc.Changes()
	if err != nil {
		return err
	}
	return c.out(cs, func() {
		for _, ch := range cs {
			src := ""
			if ch.Source.Author != "" {
				src = "  [" + strings.TrimSpace(ch.Source.Author+" "+ch.Source.Model) + "]"
			}
			fmt.Printf("%-9s %s%s\n", ch.Kind, ch.Path, src)
		}
	})
}

func cmdDiff(c *ctx) error {
	if err := c.parse(1, 1); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	d, err := svc.Diff(c.pos[0])
	if err != nil {
		return err
	}
	return c.out(d, func() {
		if d.EOLOnly {
			fmt.Println("only the line endings changed (CRLF / LF)")
		}
		for _, h := range d.Hunks {
			fmt.Printf("@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
			for _, l := range h.Lines {
				fmt.Println(l.Op + l.Text)
			}
		}
	})
}

func cmdDiscard(c *ctx) error {
	if err := c.parse(1, 1); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	if err := svc.Discard(c.pos[0]); err != nil {
		return err
	}
	return c.out(map[string]any{"path": c.pos[0], "discarded": true}, func() {
		fmt.Println("discarded changes to " + c.pos[0])
	})
}

func cmdCommit(c *ctx) error {
	all := c.fs.Bool("all", false, "commit every uncommitted file")
	message := c.fs.String("m", "", "commit message")
	if err := c.parse(0, -1); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	if err := needRepo(svc.Repo); err != nil {
		return err
	}
	paths := c.pos
	if *all {
		if paths, err = svc.Repo.Status(); err != nil {
			return err
		}
	} else {
		for i, p := range paths {
			if paths[i], err = svc.Clean(p); err != nil {
				return err
			}
		}
	}
	if len(paths) == 0 {
		return usageError{"commit: name the files, or -all"}
	}
	hash, committed, err := svc.Commit(*message, c.prov(), paths...)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"paths": paths, "committed": committed, "hash": hash}, func() {
		if committed {
			fmt.Printf("committed %s (%d files)\n", short(hash), len(paths))
		} else {
			fmt.Println("nothing to commit")
		}
	})
}

func cmdLog(c *ctx) error {
	limit := c.fs.Int("limit", 20, "maximum commits")
	if err := c.parse(0, 1); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	if err := needRepo(svc.Repo); err != nil {
		return err
	}
	rel := ""
	if len(c.pos) == 1 {
		rel = c.pos[0]
	}
	cs, err := svc.Log(rel, *limit)
	if err != nil {
		return err
	}
	return c.out(cs, func() {
		for _, cm := range cs {
			src := ""
			if cm.Source.Author == "agent" {
				src = "  [agent " + cm.Source.Model + "]"
			}
			fmt.Printf("%s  %s  %s%s\n", short(cm.Hash), cm.When.Format("2006-01-02 15:04"), cm.Message, src)
		}
	})
}

func cmdShow(c *ctx) error {
	if err := c.parse(2, 2); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	text, err := svc.Show(c.pos[0], c.pos[1])
	if err != nil {
		return err
	}
	return c.out(map[string]string{"commit": c.pos[0], "path": c.pos[1], "text": text}, func() {
		fmt.Print(text)
	})
}

func cmdRestore(c *ctx) error {
	if err := c.parse(2, 2); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	rel, err := svc.Restore(c.pos[0], c.pos[1])
	if err != nil {
		return err
	}
	hash, committed, err := c.commitOrRecord(svc, "restore "+rel+" to "+short(c.pos[0]), rel)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"path": rel, "committed": committed, "hash": hash}, func() {
		fmt.Println(wrote("restored "+rel+" to "+short(c.pos[0]), committed, hash))
	})
}

func cmdInit(c *ctx) error {
	if err := c.parse(0, 0); err != nil {
		return err
	}
	// As the editor does: create the repository and commit the metadata
	// Jusnote generates, so "status" starts clean.
	svc, err := service.Open(*c.notebook, true)
	if err != nil {
		return err
	}
	return c.out(map[string]string{"root": svc.Repo.Root()}, func() {
		fmt.Println("repository ready at", svc.Repo.Root())
	})
}

func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

func cmdAppend(c *ctx) error {
	c.fs.String("file", "", "read the text from this file")
	text := c.fs.String("text", "", "the text to add (otherwise -file or standard input)")
	if err := c.parse(1, 1); err != nil {
		return err
	}
	add, ok, err := c.input(text)
	if err != nil {
		return err
	}
	if !ok || len(add) == 0 {
		return usageError{"append: no text: pass -text, -file or pipe it in"}
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	old, err := svc.NB.Read(c.pos[0])
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	next := append([]byte{}, old...)
	if len(next) > 0 && next[len(next)-1] != '\n' {
		next = append(next, '\n')
	}
	next = append(next, add...)
	if next[len(next)-1] != '\n' {
		next = append(next, '\n')
	}
	clean, err := svc.NB.WriteIf(c.pos[0], next, versionOrEmpty(old, err))
	if err != nil {
		return err
	}
	hash, committed, err := c.commitOrRecord(svc, "update "+clean, clean)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"path": clean, "version": notebook.VersionOf(next), "committed": committed, "hash": hash}, func() {
		fmt.Println(wrote("appended to "+clean, committed, hash))
	})
}

// versionOrEmpty is the version to write over: the one read, or "" (must
// not exist) when the note was missing.
func versionOrEmpty(data []byte, readErr error) string {
	if readErr != nil {
		return ""
	}
	return notebook.VersionOf(data)
}

func cmdRecord(c *ctx) error {
	if err := c.parse(1, -1); err != nil {
		return err
	}
	if *c.author == "" {
		return usageError{"record: say who made the change: -author agent (and -model, -run), or JUS_AUTHOR"}
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	var done []string
	for _, p := range c.pos {
		rel, err := svc.Clean(p)
		if err != nil {
			return err
		}
		if err := svc.Record(rel, c.prov()); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		done = append(done, rel)
	}
	return c.out(map[string]any{"recorded": done, "source": c.prov()}, func() {
		fmt.Printf("recorded %d files as %s\n", len(done), strings.TrimSpace(*c.author+" "+*c.model))
	})
}

func cmdAttach(c *ctx) error {
	name := c.fs.String("name", "", "file name to store it under (default: the file's own)")
	if err := c.parse(2, 2); err != nil {
		return err
	}
	data, err := os.ReadFile(c.pos[1])
	if err != nil {
		return err
	}
	n := *name
	if n == "" {
		n = filepath.Base(c.pos[1])
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	file, link, err := svc.Attach(c.pos[0], n, data)
	if err != nil {
		return err
	}
	md := "[" + strings.NewReplacer("[", "", "]", "").Replace(n) + "](" + link + ")"
	switch strings.ToLower(filepath.Ext(n)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".avif":
		md = "!" + md
	}
	hash, committed, err := c.commitOrRecord(svc, "attach "+file, file)
	if err != nil {
		return err
	}
	return c.out(map[string]any{"path": file, "link": link, "markdown": md, "committed": committed, "hash": hash}, func() {
		fmt.Println(md)
	})
}
