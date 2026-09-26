// Package notebook is Jusnote's storage core: one folder of plain Markdown
// that the user owns. It reads and writes files atomically, keeps the
// app's own metadata in a hidden ".jusnote" folder, and knows nothing
// about what kind of notes these are — record types and rules are a layer
// above it.
//
// A notebook is portable: copy the folder and the notes are there. The
// ".jusnote" folder holds only what the app adds (templates, rules, caches
// and last-version backups); the ".md" files stay plain and readable by
// any editor or agent.
package notebook

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tieumi221E/Jusnote/internal/jusbase/appdir"
	"github.com/Tieumi221E/Jusnote/internal/jusbase/safeswap"
	"github.com/Tieumi221E/Jusnote/internal/juslink"
)

// VaultDir is the hidden folder a notebook keeps its own files in.
const VaultDir = ".jusnote"

// noteExts are the files a note list offers. Anything else (images, PDFs,
// attachments) can still be read and written by path.
var noteExts = map[string]bool{".md": true, ".markdown": true}

// Notebook is an open notebook folder.
type Notebook struct {
	root  string
	vault string
	swap  safeswap.NS
	mu    sync.Mutex
}

// Doc is one note file as the list shows it.
type Doc struct {
	Rel     string    `json:"rel"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// Open prepares a notebook folder. It creates the folder and its
// ".jusnote" metadata if they are missing and recovers any write that a
// crash left half finished. It does not touch the notes themselves.
func Open(root string) (*Notebook, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("notebook: empty root")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	nb := &Notebook{root: abs, vault: filepath.Join(abs, VaultDir), swap: safeswap.NS{Dot: VaultDir}}
	if err := nb.EnsureLayout(); err != nil {
		return nil, err
	}
	if err := nb.Recover(); err != nil {
		return nil, err
	}
	return nb, nil
}

// Root is the notebook folder.
func (nb *Notebook) Root() string { return nb.root }

// Vault is the notebook's hidden ".jusnote" folder.
func (nb *Notebook) Vault() string { return nb.vault }

// Path is the absolute path of a notebook-relative file, refusing anything
// that would escape the notebook or write into the vault.
func (nb *Notebook) Path(rel string) (string, error) {
	clean, err := nb.cleanRel(rel)
	if err != nil {
		return "", err
	}
	return filepath.Join(nb.root, filepath.FromSlash(clean)), nil
}

// Exists reports whether a note file is present.
func (nb *Notebook) Exists(rel string) bool {
	p, err := nb.Path(rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Read returns a note's bytes.
func (nb *Notebook) Read(rel string) ([]byte, error) {
	p, err := nb.Path(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// Write stores data as rel atomically and returns the cleaned relative
// path. The previous version is copied into ".jusnote/backup" first, so a
// bad write can be undone even without git.
func (nb *Notebook) Write(rel string, data []byte) (string, error) {
	return nb.write(rel, data, nil)
}

func (nb *Notebook) write(rel string, data []byte, base *string) (string, error) {
	clean, err := nb.cleanRel(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(nb.root, filepath.FromSlash(clean))

	nb.mu.Lock()
	defer nb.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if _, err := nb.swap.Recover(full); err != nil {
		return "", err
	}
	if base != nil {
		cur := ""
		if old, err := os.ReadFile(full); err == nil {
			cur = VersionOf(old)
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if cur != *base {
			return "", ErrChanged
		}
	}
	if err := nb.backup(clean, full); err != nil {
		return "", err
	}
	tmp := nb.swap.Temp(full)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := nb.swap.Swap(tmp, full); err != nil {
		return "", err
	}
	return clean, nil
}

// List returns every note file, sorted by path.
func (nb *Notebook) List() ([]Doc, error) {
	var out []Doc
	err := filepath.WalkDir(nb.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != nb.root && skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !noteExts[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(nb.root, p)
		if err != nil {
			return err
		}
		out = append(out, Doc{Rel: filepath.ToSlash(rel), Size: info.Size(), ModTime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// skipDir: folders a note list does not descend into — hidden ones (the
// vault, .git, other apps' metadata such as .obsidian) and node_modules.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

// Version identifies a file's current content (a hash; "" when the file
// does not exist). A writer passes the version it started from, so a save
// never silently replaces an edit made meanwhile by another program.
func (nb *Notebook) Version(rel string) (string, error) {
	data, err := nb.Read(rel)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return VersionOf(data), nil
}

// VersionOf is the version of the given content.
func VersionOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

// ErrChanged is returned by WriteIf when the file is no longer the version
// the writer started from.
var ErrChanged = errors.New("notebook: the file was changed by another program")

// WriteIf writes like Write, but only when the file is still at version
// base ("" meaning it must not exist yet).
func (nb *Notebook) WriteIf(rel string, data []byte, base string) (string, error) {
	return nb.write(rel, data, &base)
}

// Rename moves a note to a new path (creating folders as needed). It
// refuses to replace an existing file.
func (nb *Notebook) Rename(from, to string) (string, error) {
	src, err := nb.Path(from)
	if err != nil {
		return "", err
	}
	clean, err := nb.cleanRel(to)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(nb.root, filepath.FromSlash(clean))
	nb.mu.Lock()
	defer nb.mu.Unlock()
	if _, err := os.Stat(src); err != nil {
		return "", err
	}
	if _, err := os.Stat(dst); err == nil && !strings.EqualFold(src, dst) {
		return "", fmt.Errorf("notebook: %q already exists", clean)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return clean, nil
}

// Delete removes a note. Its last content is kept in ".jusnote/backup"
// (and in git history once committed), so a deletion can be undone.
func (nb *Notebook) Delete(rel string) error {
	clean, err := nb.cleanRel(rel)
	if err != nil {
		return err
	}
	full := filepath.Join(nb.root, filepath.FromSlash(clean))
	nb.mu.Lock()
	defer nb.mu.Unlock()
	if err := nb.backup(clean, full); err != nil {
		return err
	}
	return os.Remove(full)
}

// Hit is one line that matches a search.
type Hit struct {
	Rel  string `json:"rel"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Search finds the lines of every note that contain q, ignoring case, up
// to limit hits. Lines are cut to a readable length around the match.
func (nb *Notebook) Search(q string, limit int) ([]Hit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	lq := strings.ToLower(q)
	docs, err := nb.List()
	if err != nil {
		return nil, err
	}
	var out []Hit
	for _, d := range docs {
		data, err := nb.Read(d.Rel)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			at := strings.Index(strings.ToLower(line), lq)
			if at < 0 {
				continue
			}
			out = append(out, Hit{Rel: d.Rel, Line: i + 1, Text: excerpt(strings.TrimRight(line, "\r"), at)})
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// excerpt keeps about 120 characters of line, starting a little before
// byte offset at.
func excerpt(line string, at int) string {
	r := []rune(line)
	start := len([]rune(line[:min(at, len(line))])) - 30
	if start < 0 {
		start = 0
	}
	end := min(start+120, len(r))
	s := strings.TrimSpace(string(r[start:end]))
	if start > 0 {
		s = "…" + s
	}
	if end < len(r) {
		s += "…"
	}
	return s
}

// Link is the stable jus:// address of a note, optionally to a 1-based
// line, for use in notes and by external agents.
func (nb *Notebook) Link(rel string, line int) string {
	clean, err := nb.cleanRel(rel)
	if err != nil {
		clean = strings.ReplaceAll(rel, "\\", "/")
	}
	return juslink.NoteLink(clean, line)
}

// EnsureLayout creates the vault and its self-ignoring cache and backup
// folders, and hides the vault in Explorer. Templates and rules added
// later live in the vault and are tracked by git; the caches are not.
func (nb *Notebook) EnsureLayout() error {
	if err := os.MkdirAll(nb.vault, 0o755); err != nil {
		return err
	}
	for _, d := range []string{"cache", "backup"} {
		p := filepath.Join(nb.vault, d)
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
		gi := filepath.Join(p, ".gitignore")
		if _, err := os.Stat(gi); os.IsNotExist(err) {
			if err := os.WriteFile(gi, []byte("*\n!.gitignore\n"), 0o644); err != nil {
				return err
			}
		}
	}
	appdir.Hide(nb.vault)
	return nil
}

// Recover puts right any write that was interrupted by a crash.
func (nb *Notebook) Recover() error {
	suffix := nb.swap.Dot + "-old"
	return filepath.WalkDir(nb.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != nb.root && d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), suffix) {
			orig := strings.TrimSuffix(p, suffix)
			if _, err := nb.swap.Recover(orig); err != nil {
				return err
			}
		}
		return nil
	})
}

// backup copies the current version of full into the backup tree.
func (nb *Notebook) backup(clean, full string) error {
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	dst := filepath.Join(nb.vault, "backup", filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// cleanRel turns a user-supplied path into a safe notebook-relative path.
func (nb *Notebook) cleanRel(rel string) (string, error) {
	r := strings.TrimSpace(rel)
	if r == "" {
		return "", fmt.Errorf("notebook: empty path")
	}
	slash := filepath.ToSlash(r)
	if path.IsAbs(slash) || strings.HasPrefix(slash, "/") {
		return "", fmt.Errorf("notebook: absolute path not allowed: %q", rel)
	}
	clean := path.Clean(slash)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("notebook: path escapes the notebook: %q", rel)
	}
	// Case-insensitive: on Windows ".JUSNOTE" is the same folder.
	if first, _, _ := strings.Cut(clean, "/"); strings.EqualFold(first, VaultDir) || strings.EqualFold(first, ".git") {
		return "", fmt.Errorf("notebook: %q is reserved for the app", VaultDir)
	}
	return clean, nil
}
