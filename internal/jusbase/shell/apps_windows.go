//go:build windows

package shell

import (
	"errors"
	"net/url"
	"os/exec"

	"github.com/Tieumi221E/Jus/apps"
)

// Links to the other Jus apps (Jus contract 19): jus://<app>/… is handed to
// that app's program, which opens it in its window. The program is found
// by JUS_<APP>_EXE, beside this one, in a folder beside this one's (the
// release zips unpacked side by side), or on PATH; when it is not there,
// ErrNotInstalled says so and nothing else happens.

// ErrNotInstalled: the app a link belongs to is not on this computer.
var ErrNotInstalled = errors.New("not installed")

// FindApp is the program of Jus app name ("jusplay"), or "" (the Jus
// module's apps.Find: JUS_<APP>_EXE, beside, a folder beside, PATH).
func FindApp(name string) string { return apps.Find(name) }

// OpenJusLink hands link (jus://<app>/…) to its app.
func OpenJusLink(link string) error {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "jus" || u.Host == "" || u.Host == "note" {
		return errors.New("not a link of another Jus app")
	}
	app := "jus" + u.Host // jus://play/… → jusplay
	exe := FindApp(app)
	if exe == "" {
		return ErrNotInstalled
	}
	cmd := exec.Command(exe, link)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

var (
	procIsIconic            = user32.NewProc("IsIconic")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

// front brings the window to the front (restored if minimised).
func (win *Window) front() {
	const swRestore = 9
	if r, _, _ := procIsIconic.Call(win.hwnd); r != 0 {
		procShowWindow.Call(win.hwnd, swRestore)
	}
	procSetForegroundWindow.Call(win.hwnd)
}
