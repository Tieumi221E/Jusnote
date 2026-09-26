package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/Tieumi221E/Jusnote/internal/merge"
)

func sig() *object.Signature {
	return &object.Signature{Name: "Tester", Email: "t@example.com", When: time.Now()}
}

func commitFile(t *testing.T, repo *git.Repository, name, content, msg string) {
	t.Helper()
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Filesystem.Root(), name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(name); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit(msg, &git.CommitOptions{Author: sig()}); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestProbeLogsGoGitBehaviour answers, with the git binary available for
// the local "file" transport: can go-git push/clone a local remote, and
// what does worktree.Pull do when the branches have diverged?
func TestProbeLogsGoGitBehaviour(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	if _, err := git.PlainInit(origin, true); err != nil {
		t.Fatal(err)
	}

	aDir := filepath.Join(root, "a")
	a, err := git.PlainInit(aDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{origin}}); err != nil {
		t.Fatal(err)
	}
	commitFile(t, a, "note.md", "a\nb\nc\n", "init")
	if err := a.Push(&git.PushOptions{RemoteName: "origin"}); err != nil {
		t.Skipf("local push failed (file transport needs git?): %v", err)
	}
	t.Logf("push to local bare remote: ok")

	bDir := filepath.Join(root, "b")
	b, err := git.PlainClone(bDir, false, &git.CloneOptions{URL: origin})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	t.Logf("clone from local bare remote: ok")

	// A edits line 1 and pushes.
	commitFile(t, a, "note.md", "A\nb\nc\n", "a-edit")
	if err := a.Push(&git.PushOptions{RemoteName: "origin"}); err != nil {
		t.Fatalf("push2: %v", err)
	}

	// B edits line 3 (a different line) and commits, then pulls.
	commitFile(t, b, "note.md", "a\nb\nC\n", "b-edit")
	bwt, err := b.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	err = bwt.Pull(&git.PullOptions{RemoteName: "origin"})
	t.Logf("pull on divergence (different lines): err=%v", err)
	if err == nil {
		t.Logf("  merged content = %q", readFile(t, bDir, "note.md"))
	}

	// Same line on both sides.
	commitFile(t, a, "note.md", "A\nX\nc\n", "a-same")
	if err := a.Push(&git.PushOptions{RemoteName: "origin", Force: true}); err != nil {
		t.Fatalf("push3: %v", err)
	}
	commitFile(t, b, "note.md", "A\nY\nc\n", "b-same")
	err = bwt.Pull(&git.PullOptions{RemoteName: "origin"})
	t.Logf("pull on divergence (same line): err=%v", err)
	if err == nil {
		t.Logf("  content = %q", readFile(t, bDir, "note.md"))
	}
}

// TestMergeCommitAndPush proves the whole local flow works without git's
// merge: fetch, merge with internal/merge, write a two-parent merge commit
// with go-git, push, and a fresh clone sees the combined file.
func TestMergeCommitAndPush(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	if _, err := git.PlainInit(origin, true); err != nil {
		t.Fatal(err)
	}
	aDir := filepath.Join(root, "a")
	a, err := git.PlainInit(aDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{origin}}); err != nil {
		t.Fatal(err)
	}
	commitFile(t, a, "note.md", "a\nb\nc\n", "init")
	if err := a.Push(&git.PushOptions{RemoteName: "origin"}); err != nil {
		t.Skipf("local push needs git file transport: %v", err)
	}
	bDir := filepath.Join(root, "b")
	b, err := git.PlainClone(bDir, false, &git.CloneOptions{URL: origin})
	if err != nil {
		t.Fatal(err)
	}

	commitFile(t, a, "note.md", "A\nb\nc\n", "a-edit")
	if err := a.Push(&git.PushOptions{RemoteName: "origin"}); err != nil {
		t.Fatal(err)
	}
	commitFile(t, b, "note.md", "a\nb\nC\n", "b-edit")

	if err := b.Fetch(&git.FetchOptions{RemoteName: "origin"}); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	var theirs plumbing.Hash
	refs, _ := b.References()
	refs.ForEach(func(r *plumbing.Reference) error {
		s := r.Name().String()
		if strings.HasPrefix(s, "refs/remotes/origin/") && !strings.HasSuffix(s, "HEAD") {
			theirs = r.Hash()
		}
		return nil
	})
	if theirs.IsZero() {
		t.Fatal("no remote branch found after fetch")
	}
	head, _ := b.Head()
	ours := head.Hash()

	// Merge the content ourselves.
	base := "a\nb\nc\n"
	res := merge.Bytes([]byte(base), []byte(readFile(t, bDir, "note.md")), []byte("A\nb\nc\n"))
	if len(res.Conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", res.Conflicts)
	}
	if err := os.WriteFile(filepath.Join(bDir, "note.md"), []byte(res.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	// A temporary commit gives us the merged tree; the real commit is a
	// two-parent merge of ours and theirs.
	wt, _ := b.Worktree()
	if _, err := wt.Add("."); err != nil {
		t.Fatal(err)
	}
	temp, err := wt.Commit("merged tree", &git.CommitOptions{Author: sig()})
	if err != nil {
		t.Fatal(err)
	}
	tempObj, err := b.CommitObject(temp)
	if err != nil {
		t.Fatal(err)
	}
	mc := &object.Commit{
		Author:       *sig(),
		Committer:    *sig(),
		Message:      "merge remote",
		TreeHash:     tempObj.TreeHash,
		ParentHashes: []plumbing.Hash{ours, theirs},
	}
	obj := b.Storer.NewEncodedObject()
	if err := mc.Encode(obj); err != nil {
		t.Fatal(err)
	}
	mh, err := b.Storer.SetEncodedObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	headRef, err := b.Reference(plumbing.HEAD, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Storer.SetReference(plumbing.NewHashReference(headRef.Target(), mh)); err != nil {
		t.Fatal(err)
	}
	if err := b.Push(&git.PushOptions{RemoteName: "origin"}); err != nil {
		t.Fatalf("push merge: %v", err)
	}

	// A fresh clone must see the combined file.
	cDir := filepath.Join(root, "c")
	if _, err := git.PlainClone(cDir, false, &git.CloneOptions{URL: origin}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, cDir, "note.md"); got != "A\nb\nC\n" {
		t.Fatalf("remote content = %q, want %q", got, "A\nb\nC\n")
	}
	t.Logf("two-parent merge commit pushed; fresh clone sees %q", readFile(t, cDir, "note.md"))
}
