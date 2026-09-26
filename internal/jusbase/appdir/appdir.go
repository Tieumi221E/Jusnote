// Package appdir says where a Jus app keeps its own few files: the
// "<app>-data" folder next to the executable when that is writable, and
// %AppData%\<app> otherwise. Everything that belongs to a notebook lives
// inside the notebook itself; this is only the app's own settings and
// scratch space.
//
// Temp is for what must be on disk while the app runs and nowhere after:
// caches, indexes and window profiles of files outside any notebook. It
// is emptied when the app starts and when it closes.
package appdir

import (
	"errors"
	"os"
	"path/filepath"
)

// Dir is the app's data folder; portable is false when it fell back to
// the user profile.
func Dir(app string) (dir string, portable bool, err error) {
	if exe, err := os.Executable(); err == nil {
		d := filepath.Join(filepath.Dir(exe), app+"-data")
		if writable(d) {
			return d, true, nil
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", false, err
	}
	d := filepath.Join(base, app)
	if !writable(d) {
		return "", false, errors.New("no writable folder for the app's settings (" + d + ")")
	}
	return d, false, nil
}

// writable creates d if needed and checks that a file can be made in it.
func writable(d string) bool {
	if err := os.MkdirAll(d, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(d, ".probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// Temp is dir's temporary folder, emptied now (what a crashed run left).
func Temp(dir string) (string, error) {
	t := filepath.Join(dir, "temp")
	ClearTemp(t)
	return t, os.MkdirAll(t, 0o755)
}

// ClearTemp removes t and everything in it; files still held by a process
// that has not quite exited are left for the next start.
func ClearTemp(t string) {
	os.RemoveAll(t)
}
