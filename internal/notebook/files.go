package notebook

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tieumi221E/Jus/textenc"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// Beside the notes, a notebook folder may hold other text files (a config,
// a script, a log): the editor opens them as plain text, with the same
// history, review and backups; Markdown's own features stay with the
// notes. What is a text file is decided by the content, not the name: no
// NUL in the first bytes. One that is not UTF-8, or is very large, opens
// read-only. Files git ignores are left out, as the hidden folders are.

// MaxEditable is the largest file the editor edits; larger ones open
// read-only (a long log would slow the window down).
const MaxEditable = 4 << 20

// File is a note or another text file, as the file list shows it.
type File struct {
	Doc
	// Note: a Markdown note (links, preview, record types apply).
	Note bool `json:"note"`
	// ReadOnly, when set, is why the editor only shows it: "encoding" (not
	// UTF-8) or "size" (over MaxEditable).
	ReadOnly string `json:"readOnly,omitempty"`
}

type sniffed struct {
	size int64
	mod  time.Time
	text bool
	utf8 bool
}

// sniffLen is how much of a file is read to tell text from binary.
const sniffLen = 8 << 10

func sniff(path string) (text, isUTF8 bool) {
	f, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer f.Close()
	buf := make([]byte, sniffLen)
	n, _ := io.ReadFull(f, buf)
	b := buf[:n]
	if bytes.IndexByte(b, 0) >= 0 {
		return false, false
	}
	// The sample may end inside a character.
	for i := 0; i < 3 && len(b) > 0 && !utf8.Valid(b); i++ {
		b = b[:len(b)-1]
	}
	return true, utf8.Valid(b)
}

// Files lists the notes and the other text files, sorted by path.
func (nb *Notebook) Files() ([]File, error) {
	var ignore gitignore.Matcher
	if ps, err := gitignore.ReadPatterns(osfs.New(nb.root), nil); err == nil && len(ps) > 0 {
		ignore = gitignore.NewMatcher(ps)
	}
	var out []File
	seen := map[string]bool{}
	err := filepath.WalkDir(nb.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(nb.root, p)
		if rerr != nil || rel == "." {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if d.IsDir() {
			if skipDir(d.Name()) || (ignore != nil && ignore.Match(parts, true)) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || (ignore != nil && ignore.Match(parts, false)) {
			return nil // hidden, as the dot folders are, or ignored by git
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		f := File{Doc: Doc{Rel: filepath.ToSlash(rel), Size: info.Size(), ModTime: info.ModTime()}}
		f.Note = noteExts[strings.ToLower(filepath.Ext(p))]
		if !f.Note {
			text, isUTF8 := nb.sniffCached(f.Rel, p, info)
			seen[f.Rel] = true
			if !text {
				return nil
			}
			if !isUTF8 {
				f.ReadOnly = "encoding"
			}
		}
		if f.ReadOnly == "" && info.Size() > MaxEditable {
			f.ReadOnly = "size"
		}
		out = append(out, f)
		return nil
	})
	if err != nil {
		return nil, err
	}
	nb.mu.Lock()
	for rel := range nb.sniffs {
		if !seen[rel] {
			delete(nb.sniffs, rel)
		}
	}
	nb.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// sniffCached is sniff, kept per file while its size and time stay.
func (nb *Notebook) sniffCached(rel, path string, info fs.FileInfo) (bool, bool) {
	nb.mu.Lock()
	if s, ok := nb.sniffs[rel]; ok && s.size == info.Size() && s.mod.Equal(info.ModTime()) {
		nb.mu.Unlock()
		return s.text, s.utf8
	}
	nb.mu.Unlock()
	text, isUTF8 := sniff(path)
	nb.mu.Lock()
	if nb.sniffs == nil {
		nb.sniffs = map[string]sniffed{}
	}
	nb.sniffs[rel] = sniffed{size: info.Size(), mod: info.ModTime(), text: text, utf8: isUTF8}
	nb.mu.Unlock()
	return text, isUTF8
}

// TextOf is b as text, and its encoding ("utf-8", or the one it is in:
// gb18030, shift_jis, big5, utf-16le …).
func TextOf(b []byte) (string, string) {
	if utf8.Valid(b) {
		return string(b), "utf-8"
	}
	return textenc.DecodeName(b)
}

// IsNote reports whether rel is a Markdown note.
func IsNote(rel string) bool { return noteExts[strings.ToLower(filepath.Ext(rel))] }
