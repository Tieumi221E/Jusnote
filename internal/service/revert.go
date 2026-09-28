package service

import (
	"strings"
	"time"

	"github.com/Tieumi221E/Jusnote/internal/history"
)

// Undoing one agent session (Jus contract 5): what the commits carrying
// Jus-Run: <run> changed goes back, newest first, each file only while it
// is still as that commit left it — a file someone changed since is
// reported, not overwritten. What the session wrote without committing
// (recorded with -no-commit) and is still as it wrote it is discarded.

// RevertStep is one file of the session: put back, or why not.
type RevertStep struct {
	Commit  string    `json:"commit,omitempty"` // "" for an uncommitted change
	Subject string    `json:"subject,omitempty"`
	When    time.Time `json:"when"`
	Path    string    `json:"path"`
	// State: "ready" (plan), "done", or "conflict".
	State string `json:"state"`
	Why   string `json:"why,omitempty"`
}

// sessionLimit bounds how far back a session's commits are looked for.
const sessionLimit = 5000

// SessionCommits are the commits of session run, newest first.
func (s *Service) SessionCommits(run string) ([]history.Commit, error) {
	repo, err := s.repo()
	if err != nil {
		return nil, err
	}
	all, err := repo.Log(sessionLimit)
	if err != nil {
		return nil, err
	}
	out := []history.Commit{}
	for _, c := range all {
		if c.Source.Run == run {
			out = append(out, c)
		}
	}
	return out, nil
}

// RevertSession undoes session run (apply false: only says what it would
// do). It returns the steps and the files it changed, for the caller to
// commit.
func (s *Service) RevertSession(run string, apply bool) ([]RevertStep, []string, error) {
	repo, err := s.repo()
	if err != nil {
		return nil, nil, err
	}
	commits, err := s.SessionCommits(run)
	if err != nil {
		return nil, nil, err
	}
	steps := []RevertStep{}
	var changed []string
	// What each file is now, as the steps so far leave it (nil: missing).
	now := map[string]*string{}
	current := func(rel string) *string {
		if v, ok := now[rel]; ok {
			return v
		}
		b, err := s.NB.Read(rel)
		if err != nil {
			now[rel] = nil
			return nil
		}
		t := string(b)
		now[rel] = &t
		return &t
	}
	// First what the session left uncommitted: it came after its commits.
	for rel, r := range s.loadProv() {
		if r.Source.Run != run {
			continue
		}
		st := RevertStep{Path: rel, When: r.When, State: "ready", Subject: "uncommitted"}
		if v, err := s.NB.Version(rel); err != nil || v != r.Version {
			st.State, st.Why = "conflict", "changed since the session wrote it"
		} else if apply {
			if err := s.Discard(rel); err != nil {
				st.State, st.Why = "conflict", err.Error()
			} else {
				st.State = "done"
				delete(now, rel)
			}
		}
		steps = append(steps, st)
	}
	for _, c := range commits {
		paths, parent, err := repo.Changed(c.Hash)
		if err != nil {
			return nil, nil, err
		}
		for _, rel := range paths {
			st := RevertStep{Commit: c.Hash, Subject: c.Message, When: c.When, Path: rel, State: "ready"}
			after, afterOK, err := repo.ContentAt(c.Hash, rel)
			if err != nil {
				return nil, nil, err
			}
			before, beforeOK := "", false
			if parent != "" {
				if before, beforeOK, err = repo.ContentAt(parent, rel); err != nil {
					return nil, nil, err
				}
			}
			cur := current(rel)
			switch {
			case afterOK != (cur != nil) || (afterOK && !sameText(*cur, after)):
				st.State, st.Why = "conflict", "changed since (by someone else, or later in this session)"
			default:
				if beforeOK {
					t := before
					now[rel] = &t
				} else {
					now[rel] = nil
				}
				if apply {
					var err error
					if beforeOK {
						_, err = s.NB.Write(rel, []byte(before))
					} else {
						_, err = s.Delete(rel)
					}
					if err != nil {
						st.State, st.Why = "conflict", err.Error()
						break
					}
					st.State = "done"
					changed = append(changed, rel)
				}
			}
			steps = append(steps, st)
		}
	}
	return steps, changed, nil
}

// sameText compares two texts ignoring line endings (a file saved with
// CRLF is the text git keeps with LF).
func sameText(a, b string) bool {
	return strings.ReplaceAll(a, "\r\n", "\n") == strings.ReplaceAll(b, "\r\n", "\n")
}
