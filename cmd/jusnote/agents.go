package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type flagT = flag.Flag

// cmdAgents writes the notebook's agent guide (internal/service/agents.go):
// AGENTS.md plus the CLAUDE.md / GEMINI.md bridges, naming this exe.
func cmdAgents(c *ctx) error {
	force := c.fs.Bool("force", false, "replace existing files")
	if err := c.parse(0, 0); err != nil {
		return err
	}
	svc, err := c.open()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "jusnote"
	}
	out, written, err := svc.WriteAgentGuide(filepath.Clean(exe), *force)
	if err != nil {
		return err
	}
	hash, committed := "", false
	if len(written) > 0 && svc.Repo != nil && !*c.noCommit {
		if hash, committed, err = svc.Commit("jusnote: agent guide", c.prov(), written...); err != nil {
			return err
		}
	}
	return c.out(map[string]any{"files": out, "committed": committed, "hash": hash}, func() {
		for _, r := range out {
			note := ""
			if r.Action == "kept" {
				note = " (already there and not written by jusnote; -force replaces it)"
			}
			fmt.Printf("%-9s %s%s\n", r.Action, r.Path, note)
		}
	})
}
