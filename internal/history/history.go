// Package history is the notebook's storage layer seen through git: every
// save becomes a commit, so any line can be traced, compared and undone,
// and two devices that exchange git history can merge instead of
// overwriting each other.
//
// It is deliberately a small interface over go-git (pure Go, so the app
// still runs on a machine with no git installed): init, status, commit
// everything, and read the log. Merging and remotes are added later behind
// the same type.
package history

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// Commit is one entry of the log.
type Commit struct {
	Hash    string     `json:"hash"`
	Message string     `json:"message"` // the subject line
	When    time.Time  `json:"when"`
	Author  string     `json:"author"`
	Source  Provenance `json:"source"` // from the Jus-* trailers; empty when there are none
}

// Repo is an open git repository.
type Repo struct {
	root string
	r    *git.Repository
}

// IsRepo reports whether root already holds a git repository.
func IsRepo(root string) bool {
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return true
	}
	return false
}

// Open opens the repository at root (which must already exist).
func Open(root string) (*Repo, error) {
	r, err := git.PlainOpen(root)
	if err != nil {
		return nil, err
	}
	return &Repo{root: root, r: r}, nil
}

// Init creates a repository at root with "main" as its branch.
func Init(root string) (*Repo, error) {
	r, err := git.PlainInit(root, false)
	if err != nil {
		return nil, err
	}
	_ = r.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main")))
	return &Repo{root: root, r: r}, nil
}

// Ensure opens the repository at root, creating it when missing.
func Ensure(root string) (*Repo, error) {
	if IsRepo(root) {
		return Open(root)
	}
	return Init(root)
}

// Root is the repository folder.
func (rp *Repo) Root() string { return rp.root }

// Status lists changed paths (relative, slash-separated), sorted.
func (rp *Repo) Status() ([]string, error) {
	wt, err := rp.r.Worktree()
	if err != nil {
		return nil, err
	}
	st, err := wt.Status()
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(st))
	for p := range st {
		paths = append(paths, filepath.ToSlash(p))
	}
	sort.Strings(paths)
	return paths, nil
}

// CommitAll stages every change and commits it. It returns the commit hash
// and whether a commit was actually made (false when the tree was clean).
func (rp *Repo) CommitAll(message string) (hash string, committed bool, err error) {
	wt, err := rp.r.Worktree()
	if err != nil {
		return "", false, err
	}
	st, err := wt.Status()
	if err != nil {
		return "", false, err
	}
	if st.IsClean() {
		return "", false, nil
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return "", false, err
	}
	h, err := wt.Commit(message, &git.CommitOptions{Author: rp.signature()})
	if err != nil {
		return "", false, err
	}
	return h.String(), true, nil
}

// CommitPaths commits only the given paths (relative, slash-separated):
// present files are staged, missing ones recorded as removed. Other
// changes in the working tree, such as an agent's or another editor's,
// stay uncommitted for the user to review. It returns false when none of
// the paths had changed.
func (rp *Repo) CommitPaths(message string, paths ...string) (hash string, committed bool, err error) {
	wt, err := rp.r.Worktree()
	if err != nil {
		return "", false, err
	}
	st, err := wt.Status()
	if err != nil {
		return "", false, err
	}
	staged := 0
	for _, p := range paths {
		p = filepath.ToSlash(p)
		fs, ok := st[p]
		if !ok || (fs.Worktree == git.Unmodified && fs.Staging == git.Unmodified) {
			continue
		}
		if _, err := os.Stat(filepath.Join(rp.root, filepath.FromSlash(p))); os.IsNotExist(err) {
			if fs.Staging == git.Added || fs.Worktree == git.Untracked {
				continue // never committed and now gone: nothing to record
			}
			if _, err := wt.Remove(p); err != nil {
				return "", false, err
			}
		} else if err := wt.AddWithOptions(&git.AddOptions{Path: p, SkipStatus: true}); err != nil {
			// SkipStatus: plain Add runs a full status per call, which made
			// committing n files O(n²) (minutes for 5 000 notes). The status
			// is already taken above, and only paths it lists (never ignored
			// ones) get here.
			return "", false, err
		}
		staged++
	}
	if staged == 0 {
		return "", false, nil
	}
	h, err := wt.Commit(message, &git.CommitOptions{Author: rp.signature()})
	if err != nil {
		return "", false, err
	}
	return h.String(), true, nil
}

// Untracked lists the untracked files under dir (slash-separated, "" for
// the whole tree).
func (rp *Repo) Untracked(dir string) ([]string, error) {
	wt, err := rp.r.Worktree()
	if err != nil {
		return nil, err
	}
	st, err := wt.Status()
	if err != nil {
		return nil, err
	}
	var out []string
	for p, fs := range st {
		p = filepath.ToSlash(p)
		if fs.Worktree == git.Untracked && (dir == "" || strings.HasPrefix(p, dir+"/")) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Tracked reports whether rel exists in the HEAD commit.
func (rp *Repo) Tracked(rel string) bool {
	_, ok, err := rp.headContent(filepath.ToSlash(rel))
	return err == nil && ok
}

// Log returns up to limit commits, newest first (limit <= 0 means all).
func (rp *Repo) Log(limit int) ([]Commit, error) {
	iter, err := rp.r.Log(&git.LogOptions{})
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var out []Commit
	err = iter.ForEach(func(c *object.Commit) error {
		if limit > 0 && len(out) >= limit {
			return storer.ErrStop
		}
		out = append(out, toCommit(c))
		return nil
	})
	if err != nil && !errors.Is(err, storer.ErrStop) {
		return nil, err
	}
	return out, nil
}

// signature is the repository's configured author, or a neutral Jusnote
// identity so commits work on a machine that never set up git.
func (rp *Repo) signature() *object.Signature {
	if cfg, err := rp.r.Config(); err == nil && cfg.User.Name != "" {
		email := cfg.User.Email
		if email == "" {
			email = "jusnote@localhost"
		}
		return &object.Signature{Name: cfg.User.Name, Email: email, When: time.Now()}
	}
	return &object.Signature{Name: "Jusnote", Email: "jusnote@localhost", When: time.Now()}
}
