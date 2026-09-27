package history

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// Change is one line of the working file marked against HEAD, for the
// editor's git gutter. Line is 1-based in the working file; for "delete"
// it is the line the removed text sat after (so the editor can draw a
// marker there).
type Change struct {
	Line int    `json:"line"`
	Kind string `json:"kind"` // "add", "modify" or "delete"
}

// Gutter compares the working copy of rel with its content at HEAD and
// returns the line-level changes. A file not yet in HEAD (new, or a
// repository with no commits) has no markers, as in other editors: every
// line would be "added", which says nothing.
func (rp *Repo) Gutter(rel string) ([]Change, error) {
	rel = filepath.ToSlash(rel)
	head, ok, err := rp.headContent(rel)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	work, err := os.ReadFile(filepath.Join(rp.root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	return diffChanges(normalizeEOL(head), normalizeEOL(string(work))), nil
}

// headContent is the file's text at HEAD; ok is false when there is no
// commit yet or the file did not exist.
func (rp *Repo) headContent(rel string) (text string, ok bool, err error) {
	head, err := rp.r.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	commit, err := rp.r.CommitObject(head.Hash())
	if err != nil {
		return "", false, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", false, err
	}
	f, err := tree.File(rel)
	if err != nil {
		return "", false, nil // not in this commit
	}
	text, err = f.Contents()
	if err != nil {
		return "", false, err
	}
	return text, true, nil
}

func diffChanges(head, work string) []Change {
	dmp := diffmatchpatch.New()
	a, b, lines := dmp.DiffLinesToRunes(head, work)
	diffs := dmp.DiffMainRunes(a, b, false)
	return classify(dmp.DiffCharsToLines(diffs, lines))
}

// classify turns the line-level diff into per-line changes. A hunk that
// both removes and adds lines is a modification; extra additions are adds,
// extra removals a single delete marker.
func classify(diffs []diffmatchpatch.Diff) []Change {
	var out []Change
	newLine := 1
	for i := 0; i < len(diffs); {
		if diffs[i].Type == diffmatchpatch.DiffEqual {
			newLine += countLines(diffs[i].Text)
			i++
			continue
		}
		var del, ins strings.Builder
		for i < len(diffs) && diffs[i].Type != diffmatchpatch.DiffEqual {
			switch diffs[i].Type {
			case diffmatchpatch.DiffDelete:
				del.WriteString(diffs[i].Text)
			case diffmatchpatch.DiffInsert:
				ins.WriteString(diffs[i].Text)
			}
			i++
		}
		dl, il := countLines(del.String()), countLines(ins.String())
		switch {
		case il == 0:
			out = append(out, Change{Line: newLine, Kind: "delete"})
		case dl == 0:
			for k := 0; k < il; k++ {
				out = append(out, Change{Line: newLine + k, Kind: "add"})
			}
			newLine += il
		default:
			m := dl
			if il < m {
				m = il
			}
			for k := 0; k < m; k++ {
				out = append(out, Change{Line: newLine + k, Kind: "modify"})
			}
			for k := m; k < il; k++ {
				out = append(out, Change{Line: newLine + k, Kind: "add"})
			}
			if dl > il {
				out = append(out, Change{Line: newLine + il, Kind: "delete"})
			}
			newLine += il
		}
	}
	return out
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}
