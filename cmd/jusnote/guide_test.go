package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The agent guide and help -json agree (Jus contract 17): every command
// help -json has is in AGENTS.md, and every command AGENTS.md names — in
// its prose, `name <args>` — is one help -json has.
func TestAgentGuideAgreesWithHelp(t *testing.T) {
	nb := t.TempDir()
	must(t, run(t, nb, nil, "", "init"))
	must(t, run(t, nb, nil, "", "agents"))
	guide, err := os.ReadFile(filepath.Join(nb, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	var help struct {
		Commands []struct {
			Name     string `json:"name"`
			Synopsis string `json:"synopsis"`
		} `json:"commands"`
	}
	decodeJSON(t, must(t, run(t, nb, nil, "", "help", "-json")).stdout, &help)
	known := map[string]bool{"help": true, "version": true, "gui": true} // the program's own, not capabilities
	for _, c := range help.Commands {
		known[c.Name] = true
		if !strings.Contains(string(guide), "`"+c.Synopsis+"`") {
			t.Errorf("AGENTS.md does not list %s", c.Synopsis)
		}
	}

	// The prose above the generated list: every `command …` it names.
	text := string(guide)
	if i := strings.Index(text, "## Every command"); i >= 0 {
		text = text[:i]
	} else {
		t.Fatal("AGENTS.md has no generated command list")
	}
	exe := strings.ReplaceAll(bin, `\`, `/`)
	word := regexp.MustCompile(`^[a-z]+$`)
	named := 0
	for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(text, -1) {
		span := strings.TrimPrefix(strings.ReplaceAll(m[1], `\`, `/`), exe+" ")
		var words []string
		for _, f := range strings.Fields(span) {
			if !word.MatchString(f) {
				break
			}
			words = append(words, f)
		}
		if len(words) == 0 || span == words[0] && !known[span] {
			continue // a lone word that is no command: a type id, a file name
		}
		name := words[0]
		if len(words) > 1 && known[words[0]+" "+words[1]] {
			name = words[0] + " " + words[1]
		}
		if !known[name] {
			t.Errorf("AGENTS.md names %q (in `%s`), which help -json does not have", name, m[1])
		}
		named++
	}
	if named < 15 {
		t.Fatalf("only %d commands found in the guide's prose: the check is not looking", named)
	}
}
