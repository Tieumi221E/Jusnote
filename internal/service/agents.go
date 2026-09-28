package service

import (
	"fmt"
	"strings"

	"github.com/Tieumi221E/Jus/skills"
)

// The notebook's agent guide. Coding agents read a project file when they
// start in a folder: AGENTS.md (Codex, Cursor, Copilot and others),
// CLAUDE.md (Claude Code), GEMINI.md (Gemini CLI). AGENTS.md holds the
// content; the other two are one-line bridges to it, the same pattern as
// the author's own agent folder. It is written only when asked (the
// notebook is the user's), never over an existing file without -force.
const agentsMarker = "<!-- written by jusnote agents -->"

// agentGuide is AGENTS.md; commands is the list of every command, made from
// the capability registry (caps.CommandList), so the guide and help -json
// cannot disagree.
func agentGuide(exe string, svc *Service, commands string) string {
	var types strings.Builder
	for _, t := range svc.Types {
		fmt.Fprintf(&types, "- `%s` (%s)", t.ID, t.Name)
		if t.File != "" {
			fmt.Fprintf(&types, ": file `%s`", t.File)
		}
		var secs []string
		for _, s := range t.Sections {
			secs = append(secs, s.Name)
		}
		if len(secs) > 0 {
			fmt.Fprintf(&types, ", sections %s", strings.Join(secs, " / "))
		}
		types.WriteString("\n")
	}
	if types.Len() == 0 {
		types.WriteString("- (none)\n")
	}
	var sk strings.Builder
	if list, err := skills.List(svc.NB.Vault(), nil); err == nil {
		for _, s := range list {
			fmt.Fprintf(&sk, "- `%s`", s.Name)
			if s.Description != "" {
				fmt.Fprintf(&sk, ": %s", s.Description)
			}
			if s.Run == nil {
				sk.WriteString(" (instructions: read its SKILL.md)")
			}
			sk.WriteString("\n")
		}
	}
	if sk.Len() == 0 {
		sk.WriteString("- (none)\n")
	}
	j := "`" + exe + "`"
	return agentsMarker + `
# Notes in this folder

This folder is a Jusnote notebook: plain Markdown files with their history in git.
A person reads and reviews everything here in the Jusnote editor. Work so that your
changes are easy for them to review.

## How to change notes (safest first)

1. **Edit the file with your own file-editing tool**, then record that you did it:
   ` + "`" + exe + ` record <path>... -author agent -model <provider>/<model> -run <session id>` + "`" + `
   Nothing passes through the shell, so no character is lost.
2. **Add to the end of a note**: ` + "`append <path> -file <file with the text>`" + ` (or pipe the text in).
   It keeps the rest of the note byte for byte. Do not rewrite a whole note to add a line.
3. **Add a log entry**: ` + "`capture -type <id> -section <name> -text \"...\"`" + `.
4. **Write a whole note** (new notes): ` + "`write <path> -file <file>`" + `. Prefer ` + "`-file`" + ` or a pipe to a long
   ` + "`-text \"...\"`" + `: PowerShell drops backticks and may drop quotes in arguments.

Every command that writes takes ` + "`-no-commit -author agent -model <provider>/<model> -run <session id>`" + `
(or set ` + "`JUS_AUTHOR=agent`, `JUS_MODEL`, `JUS_RUN`" + ` once).

## Other commands

The program is ` + j + ` (it may not be on PATH); run it from this folder or pass
` + "`-notebook <folder>`" + `. Every command takes ` + "`-json`" + `; ` + "`help -json`" + ` lists them all.

- Find: ` + "`search <text>`, `list`, `read <path>`, `links <path>`" + ` (outgoing links and backlinks).
- Check a structured note before you finish: ` + "`check <path>`" + `.
- Rename with ` + "`rename <from> <to>`" + ` (it updates the links); delete with ` + "`delete <path>`" + `.
- See what you changed: ` + "`status`, `diff <path>`" + `; history: ` + "`log <path>`, `show <commit> <path>`" + `.
- Undo everything one session did: ` + "`revert -session <id>`" + ` (without ` + "`-yes`" + ` it only says what it would do).
- Show the person a note in the editor: ` + "`open <path>[:line]`" + `; ` + "`editor status`" + ` says what they are looking at,
  ` + "`events watch`" + ` streams what happens in the window. ` + "`link <path> -line <n>`" + ` gives a ` + "`jus://`" + ` link to a place.

## Skills

A skill is a tool this notebook carries: ` + "`.jusnote/skills/<name>/SKILL.md`" + ` says what it does and how to use it;
a ` + "`skill.json`" + ` beside it (` + "`{\"run\": [\"python\", \"{skill}/make.py\"]}`" + `) makes it runnable with
` + "`skills run <name>`" + `. Its results go to ` + "`.jusnote/out/<name>/`" + ` (not in git; ` + "`last.log`" + ` is what it printed).
A skill runs a program, so the first run (and the first after any change to it) needs ` + "`-yes`" + `:
add it only when the person asked you to run that skill. ` + "`skills list`" + ` shows them.

## Rules

1. **Do not run git yourself here, and do not commit.** Leave your changes uncommitted with your
   name on them (the ways above do that); the person accepts or discards them in Jusnote, and the
   commit then records that you made them.
2. **Never edit ` + "`.jusnote/`" + `** except ` + "`.jusnote/types/*.yaml`" + ` when you are asked to change a record type,
   and ` + "`.jusnote/skills/`" + ` when you are asked to write or change a skill.
3. **Keep the notes plain Markdown.** Link notes with ` + "`[[note name]]`" + ` (or ` + "`[[folder/note]]`" + `); relative
   Markdown links also work. Images go in an ` + "`attachments/`" + ` folder next to the note.
4. **Change only what you were asked to.** Keep the author's wording, punctuation and structure;
   run ` + "`diff <path>`" + ` before you finish and make sure nothing else changed.
5. If a write fails with exit code 3 (conflict), the file changed since you read it: read it again.

## Record types in this notebook

` + types.String() + `
Their rules are in ` + "`.jusnote/types/`" + `; ` + "`" + exe + ` types` + "`" + ` prints them.

## Skills in this notebook

` + sk.String() + `
## Every command

Made from the same list as ` + "`help -json`" + ` (which also has each one's parameters):

` + commands
}

// Claude Code reads AGENTS.md itself only when no CLAUDE.md exists in the
// folder or above it (code.claude.com/docs/en/memory) — a parent folder
// often has one — so the bridge imports it.
const claudeBridge = agentsMarker + "\n@AGENTS.md\n"

// Gemini CLI reads GEMINI.md, not AGENTS.md, by default, and expands
// @file imports (geminicli.com/docs/cli/gemini-md). A sentence asking it to
// read AGENTS.md would only work if it chose to open the file.
const geminiBridge = agentsMarker + "\n@AGENTS.md\n"

// GuideFile is one file of the agent guide and what writing it did.
type GuideFile struct {
	Path   string `json:"path"`
	Action string `json:"action"` // "written", "updated", "unchanged" or "kept" (the user's own file)
}

// WriteAgentGuide writes AGENTS.md and the CLAUDE.md / GEMINI.md bridges
// into the notebook root, naming exe as the program to run. A file that is
// there and was not written by Jusnote is kept unless force. It returns
// what it did to each file and the paths it wrote (for the caller to
// commit).
func (s *Service) WriteAgentGuide(exe string, force bool, commands string) ([]GuideFile, []string, error) {
	files := map[string]string{
		"AGENTS.md": agentGuide(exe, s, commands),
		"CLAUDE.md": claudeBridge,
		"GEMINI.md": geminiBridge,
	}
	var out []GuideFile
	var written []string
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"} {
		old, err := s.NB.Read(name)
		switch {
		case err == nil && !force && !strings.HasPrefix(string(old), agentsMarker):
			out = append(out, GuideFile{name, "kept"})
			continue
		case err == nil && string(old) == files[name]:
			out = append(out, GuideFile{name, "unchanged"})
			continue
		}
		action := "written"
		if err == nil {
			action = "updated"
		}
		if _, err := s.NB.Write(name, []byte(files[name])); err != nil {
			return out, written, err
		}
		written = append(written, name)
		out = append(out, GuideFile{name, action})
	}
	return out, written, nil
}
