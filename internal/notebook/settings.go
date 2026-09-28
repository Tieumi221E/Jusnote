package notebook

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Settings is how this notebook behaves, kept in it (.jusnote/settings.json,
// tracked by git: it travels with the notebook). A folder that is also
// something else — a code repository, say — may not want the editor
// committing on its own.
type Settings struct {
	Version int `json:"version"`
	// Commit: "auto" (the editor commits what it wrote when you leave a
	// note and when the window closes) or "manual" (only Ctrl+S and an
	// explicit commit do).
	Commit string `json:"commit"`
}

func (nb *Notebook) settingsFile() string { return filepath.Join(nb.vault, "settings.json") }

// Settings are the notebook's settings (the defaults when it has none).
func (nb *Notebook) Settings() Settings {
	s := Settings{Version: 1, Commit: "auto"}
	b, err := os.ReadFile(nb.settingsFile())
	if err != nil {
		return s
	}
	var f Settings
	if json.Unmarshal(b, &f) == nil && (f.Commit == "auto" || f.Commit == "manual") {
		s.Commit = f.Commit
	}
	return s
}

// SetSettings keeps s (atomically).
func (nb *Notebook) SetSettings(s Settings) error {
	if s.Commit != "auto" && s.Commit != "manual" {
		return errors.New("commit must be auto or manual")
	}
	s.Version = 1
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := nb.settingsFile() + ".partial"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, nb.settingsFile()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
