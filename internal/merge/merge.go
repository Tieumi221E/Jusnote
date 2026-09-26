// Package merge is Jusnote's three-way merge for plain-text notes: given
// the common base and the two sides that each changed it, it combines the
// changes without git's help. Changes on different lines merge silently;
// only overlapping changes become a conflict. This is what lets two
// devices share a notebook through git and keep both edits.
//
// It works on lines (kept together with their newline) so the merged file
// is byte-for-byte what was written on either side, outside the conflicts.
package merge

import (
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Conflict is a region both sides changed differently.
type Conflict struct {
	Base   []string
	Ours   []string
	Theirs []string
}

// Result is the merged text and any conflicts (also written into Lines as
// <<<<<<< / ======= / >>>>>>> markers).
type Result struct {
	Lines     []string
	Conflicts []Conflict
}

// Bytes merges three byte slices.
func Bytes(base, ours, theirs []byte) Result {
	return Lines(splitLines(string(base)), splitLines(string(ours)), splitLines(string(theirs)))
}

// Lines merges three line slices (each line keeps its newline).
func Lines(base, ours, theirs []string) Result {
	eo, et := diffEdits(base, ours), diffEdits(base, theirs)

	var out []string
	var conflicts []Conflict
	i, j, b := 0, 0, 0
	for b <= len(base) {
		ns := -1
		if i < len(eo) {
			ns = eo[i].b0
		}
		if j < len(et) && (ns < 0 || et[j].b0 < ns) {
			ns = et[j].b0
		}
		if ns < 0 {
			out = append(out, base[b:]...)
			break
		}
		if ns > b {
			out = append(out, base[b:ns]...)
			b = ns
		}

		// Seed the cluster with the earliest change, then absorb every
		// change from either side that overlaps it.
		start := b
		end := b
		var oh, th []edit
		if i < len(eo) && (j >= len(et) || eo[i].b0 <= et[j].b0) {
			oh = append(oh, eo[i])
			end = eo[i].b1
			i++
		} else {
			th = append(th, et[j])
			end = et[j].b1
			j++
		}
		for absorb(eo, &i, start, &end, &oh) || absorb(et, &j, start, &end, &th) {
		}

		ov := version(base, start, end, oh)
		tv := version(base, start, end, th)
		switch {
		case len(oh) == 0:
			out = append(out, tv...) // only the other side changed
		case len(th) == 0:
			out = append(out, ov...) // only this side changed
		case equal(ov, tv):
			out = append(out, ov...)
		default:
			conflicts = append(conflicts, Conflict{Base: base[start:end], Ours: ov, Theirs: tv})
			out = append(out, "<<<<<<< 本机\n")
			out = append(out, ov...)
			out = append(out, "=======\n")
			out = append(out, tv...)
			out = append(out, ">>>>>>> 远端\n")
		}
		b = end
	}
	return Result{Lines: out, Conflicts: conflicts}
}

func absorb(es []edit, idx *int, start int, end *int, dst *[]edit) bool {
	moved := false
	for *idx < len(es) {
		h := es[*idx]
		if !(h.b0 < *end || h.b0 == start) {
			break
		}
		*dst = append(*dst, h)
		if h.b1 > *end {
			*end = h.b1
		}
		*idx++
		moved = true
	}
	return moved
}

// version applies a side's edits, and only those, to base[start:end).
func version(base []string, start, end int, hs []edit) []string {
	out := make([]string, 0, end-start)
	b := start
	for _, h := range hs {
		if h.b0 > b {
			out = append(out, base[b:h.b0]...)
		}
		out = append(out, h.lines...)
		if h.b1 > b {
			b = h.b1
		}
	}
	if b < end {
		out = append(out, base[b:end]...)
	}
	return out
}

// edit replaces base[b0:b1) with lines.
type edit struct {
	b0, b1 int
	lines  []string
}

func diffEdits(base, other []string) []edit {
	dmp := diffmatchpatch.New()
	r1, r2, lineArr := dmp.DiffLinesToRunes(strings.Join(base, ""), strings.Join(other, ""))
	diffs := dmp.DiffCharsToLines(dmp.DiffMainRunes(r1, r2, false), lineArr)

	var out []edit
	b, i := 0, 0
	for i < len(diffs) {
		if diffs[i].Type == diffmatchpatch.DiffEqual {
			b += len(splitLines(diffs[i].Text))
			i++
			continue
		}
		b0 := b
		var ins []string
		for i < len(diffs) && diffs[i].Type != diffmatchpatch.DiffEqual {
			if diffs[i].Type == diffmatchpatch.DiffDelete {
				b += len(splitLines(diffs[i].Text))
			} else {
				ins = append(ins, splitLines(diffs[i].Text)...)
			}
			i++
		}
		out = append(out, edit{b0: b0, b1: b, lines: ins})
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// splitLines keeps each line together with its newline; the last line may
// have none. Joining the result reproduces the input exactly.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// String joins merged lines back into text.
func (r Result) String() string { return strings.Join(r.Lines, "") }

// Format is a short description of the conflicts, for logs.
func (r Result) Format() string {
	if len(r.Conflicts) == 0 {
		return "clean"
	}
	return fmt.Sprintf("%d conflict(s)", len(r.Conflicts))
}
