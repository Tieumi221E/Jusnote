// Package sync is the sync layer spike: can two devices share a notebook
// through a git remote, keeping both sides' edits? It probes what go-git
// can do by itself (transport and merge) so we know how much of the merge
// we must own. The merge itself lives in internal/merge.
package sync
