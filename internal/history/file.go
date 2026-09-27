package history

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// Provenance says who made a change, written as git trailers so any git
// tool shows it and an agent's run can be traced (docs/architecture.md).
type Provenance struct {
	Author string `json:"author,omitempty"` // "human" or "agent"
	Model  string `json:"model,omitempty"`  // "<provider>/<model>"
	Run    string `json:"run,omitempty"`    // the agent's session or run id
}

// Message appends p to subject as trailers; an empty p changes nothing.
func (p Provenance) Message(subject string) string {
	var t []string
	if p.Author != "" {
		t = append(t, "Jus-Author: "+oneLine(p.Author))
	}
	if p.Model != "" {
		t = append(t, "Jus-Model: "+oneLine(p.Model))
	}
	if p.Run != "" {
		t = append(t, "Jus-Run: "+oneLine(p.Run))
	}
	if len(t) == 0 {
		return subject
	}
	return subject + "\n\n" + strings.Join(t, "\n")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// parseMessage splits a commit message into its subject and the Jus
// trailers of its last paragraph.
func parseMessage(msg string) (string, Provenance) {
	msg = strings.TrimRight(msg, "\r\n")
	subject, _, _ := strings.Cut(msg, "\n")
	var p Provenance
	paras := strings.Split(msg, "\n\n")
	if len(paras) > 1 {
		for _, line := range strings.Split(paras[len(paras)-1], "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			switch strings.TrimSpace(k) {
			case "Jus-Author":
				p.Author = v
			case "Jus-Model":
				p.Model = v
			case "Jus-Run":
				p.Run = v
			}
		}
	}
	return strings.TrimRight(subject, "\r"), p
}

// FileLog returns up to limit commits that changed rel, newest first. A
// commit changed rel when rel's blob differs from its first parent's (or
// appears or disappears). This looks up one path per commit; go-git's own
// LogOptions.FileName diffs whole trees and took ~300 ms on a small repo.
func (rp *Repo) FileLog(rel string, limit int) ([]Commit, error) {
	rel = filepath.ToSlash(rel)
	iter, err := rp.r.Log(&git.LogOptions{Order: git.LogOrderCommitterTime})
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, nil
		}
		return nil, err
	}
	blob := func(c *object.Commit) plumbing.Hash {
		tree, err := c.Tree()
		if err != nil {
			return plumbing.ZeroHash
		}
		e, err := tree.FindEntry(rel)
		if err != nil {
			return plumbing.ZeroHash
		}
		return e.Hash
	}
	var out []Commit
	err = iter.ForEach(func(c *object.Commit) error {
		if limit > 0 && len(out) >= limit {
			return storer.ErrStop
		}
		mine := blob(c)
		parent := plumbing.ZeroHash
		if p, err := c.Parent(0); err == nil {
			parent = blob(p)
		}
		if mine != parent {
			out = append(out, toCommit(c))
		}
		return nil
	})
	if err != nil && !errors.Is(err, storer.ErrStop) {
		return nil, err
	}
	return out, nil
}

// ContentAt is rel's text in the commit hash (a full or unique short
// hash); ok is false when the file did not exist there.
func (rp *Repo) ContentAt(hash, rel string) (string, bool, error) {
	h, err := rp.resolve(hash)
	if err != nil {
		return "", false, err
	}
	c, err := rp.r.CommitObject(h)
	if err != nil {
		return "", false, err
	}
	tree, err := c.Tree()
	if err != nil {
		return "", false, err
	}
	f, err := tree.File(filepath.ToSlash(rel))
	if err != nil {
		return "", false, nil
	}
	text, err := f.Contents()
	return text, err == nil, err
}

// HeadContent is rel's text at HEAD; ok is false when it is not there.
func (rp *Repo) HeadContent(rel string) (string, bool, error) {
	return rp.headContent(filepath.ToSlash(rel))
}

func (rp *Repo) resolve(hash string) (plumbing.Hash, error) {
	if len(hash) == 40 {
		return plumbing.NewHash(hash), nil
	}
	h, err := rp.r.ResolveRevision(plumbing.Revision(hash))
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return *h, nil
}

// DiffLine is one line of a hunk: ' ' context, '-' removed, '+' added.
type DiffLine struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

// Hunk is a run of changes with up to three lines of context, numbered
// as in a unified diff (1-based; Old* in the old text, New* in the new).
type Hunk struct {
	OldStart int        `json:"oldStart"`
	OldLines int        `json:"oldLines"`
	NewStart int        `json:"newStart"`
	NewLines int        `json:"newLines"`
	Lines    []DiffLine `json:"lines"`
}

const diffContext = 3

// Diff compares two texts line by line. Line endings do not count: a file
// that an editor saved with CRLF instead of LF would otherwise show every
// line as changed (see EOLOnly).
func Diff(old, new string) []Hunk {
	old, new = normalizeEOL(old), normalizeEOL(new)
	dmp := diffmatchpatch.New()
	a, b, lines := dmp.DiffLinesToRunes(old, new)
	diffs := dmp.DiffCharsToLines(dmp.DiffMainRunes(a, b, false), lines)

	// Flatten into single lines with their ops.
	var all []DiffLine
	for _, d := range diffs {
		op := " "
		switch d.Type {
		case diffmatchpatch.DiffDelete:
			op = "-"
		case diffmatchpatch.DiffInsert:
			op = "+"
		}
		for _, l := range splitKeep(d.Text) {
			all = append(all, DiffLine{Op: op, Text: strings.TrimRight(l, "\r\n")})
		}
	}

	var hunks []Hunk
	oldN, newN := 1, 1
	for i := 0; i < len(all); {
		if all[i].Op == " " {
			oldN++
			newN++
			i++
			continue
		}
		// A change starts at i: back up for context, then run forward until
		// more than 2*context unchanged lines separate it from the next.
		start := max(0, i-diffContext)
		h := Hunk{OldStart: oldN - (i - start), NewStart: newN - (i - start)}
		for k := start; k < i; k++ {
			h.Lines = append(h.Lines, all[k])
			h.OldLines++
			h.NewLines++
		}
		for i < len(all) {
			if all[i].Op == " " {
				run := 0
				for i+run < len(all) && all[i+run].Op == " " {
					run++
				}
				if i+run == len(all) || run > 2*diffContext {
					for k := i; k < i+min(run, diffContext); k++ {
						h.Lines = append(h.Lines, all[k])
						h.OldLines++
						h.NewLines++
					}
					break
				}
				for k := i; k < i+run; k++ {
					h.Lines = append(h.Lines, all[k])
					h.OldLines++
					h.NewLines++
				}
				oldN += run
				newN += run
				i += run
				continue
			}
			h.Lines = append(h.Lines, all[i])
			if all[i].Op == "-" {
				h.OldLines++
				oldN++
			} else {
				h.NewLines++
				newN++
			}
			i++
		}
		hunks = append(hunks, h)
	}
	return hunks
}

func normalizeEOL(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// EOLOnly reports whether two texts differ only in their line endings.
func EOLOnly(old, new string) bool {
	return old != new && normalizeEOL(old) == normalizeEOL(new)
}

// splitKeep splits s into lines, each keeping its newline.
func splitKeep(s string) []string {
	var out []string
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// WorkingDiff compares rel at HEAD with the working file. A file not in
// HEAD diffs against empty; a deleted file diffs to empty. eolOnly is true
// when the only difference is the line endings.
func (rp *Repo) WorkingDiff(rel string) (hunks []Hunk, eolOnly bool, err error) {
	head, _, err := rp.headContent(filepath.ToSlash(rel))
	if err != nil {
		return nil, false, err
	}
	work, err := os.ReadFile(filepath.Join(rp.root, filepath.FromSlash(rel)))
	if err != nil && !os.IsNotExist(err) {
		return nil, false, err
	}
	return Diff(head, string(work)), EOLOnly(head, string(work)), nil
}

func toCommit(c *object.Commit) Commit {
	subject, p := parseMessage(c.Message)
	return Commit{
		Hash:    c.Hash.String(),
		Message: subject,
		When:    c.Author.When,
		Author:  c.Author.Name,
		Source:  p,
	}
}
