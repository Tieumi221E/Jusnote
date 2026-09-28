// Package footprint is every place Jusnote writes (Jus contract 12), for
// its manifest and `jus uninstall`: the program, its data folder, and in
// each notebook it has worked in, its .jusnote folder — the caches and the
// skills' output there are the app's, the rest (record types, skills,
// settings, backups) is the user's and stays.
//
// The notebooks are remembered in the data folder (notebooks-known.json)
// the first time the app works in each, from the window or the command
// line, so the footprint does not depend on the recent list.
package footprint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Tieumi221E/Jus/apps"
)

const file = "notebooks-known.json"

var mu sync.Mutex

func read(data string) []string {
	var list []string
	if b, err := os.ReadFile(filepath.Join(data, file)); err == nil {
		json.Unmarshal(b, &list)
	}
	return list
}

// Remember adds root to the notebooks the app has worked in (a no-op when
// it is known; errors are ignored: this is bookkeeping, not the work).
func Remember(data, root string) {
	if data == "" || root == "" {
		return
	}
	root = filepath.Clean(root)
	mu.Lock()
	defer mu.Unlock()
	list := read(data)
	for _, p := range list {
		if strings.EqualFold(p, root) {
			return
		}
	}
	list = append(list, root)
	sort.Strings(list)
	b, _ := json.MarshalIndent(list, "", "  ")
	if os.MkdirAll(data, 0o755) != nil {
		return
	}
	tmp := filepath.Join(data, file+".partial")
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, filepath.Join(data, file))
	}
}

// Places are Jusnote's places on this computer now.
func Places(exe, data string) []apps.Place {
	out := []apps.Place{}
	if exe != "" {
		out = append(out, apps.Place{Path: exe, Kind: apps.Program, What: "the program"})
	}
	if data != "" {
		out = append(out, apps.Place{Path: data, Kind: apps.Data, What: "settings, the notebooks list, trusted skills, window profile"})
	}
	for _, root := range read(data) {
		vault := filepath.Join(root, ".jusnote")
		if _, err := os.Stat(vault); err != nil {
			continue // gone, or never made
		}
		out = append(out,
			apps.Place{Path: filepath.Join(vault, "cache"), Kind: apps.Cache, What: "made again when needed"},
			apps.Place{Path: filepath.Join(vault, "out"), Kind: apps.Cache, What: "what the notebook's skills made (runs again make it)"},
			apps.Place{Path: vault, Kind: apps.User, What: "record types, skills, settings and backups of " + root},
		)
	}
	return out
}
