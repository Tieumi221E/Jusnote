//go:build windows

package shell

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFindAppAndLinks(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "jusplay.exe")
	os.WriteFile(fake, []byte("not a program"), 0o644)
	t.Setenv("JUS_JUSPLAY_EXE", fake)
	if got := FindApp("jusplay"); got != fake {
		t.Fatalf("FindApp: %q", got)
	}
	t.Setenv("JUS_JUSPLAY_EXE", filepath.Join(t.TempDir(), "missing.exe"))
	t.Setenv("PATH", t.TempDir())
	if got := FindApp("jusplay"); got != "" && filepath.Dir(got) != filepath.Dir(os.Args[0]) {
		t.Fatalf("FindApp found %q with nothing installed", got)
	}
	// Not another app's link: refused; an app that is not here: said so.
	for _, l := range []string{"jus://note/a.md", "https://example.org", "jus:///x"} {
		if err := OpenJusLink(l); err == nil || errors.Is(err, ErrNotInstalled) {
			t.Errorf("%s: %v", l, err)
		}
	}
	if err := OpenJusLink("jus://read/x/y.epub"); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("jus://read with no Jusread: %v", err)
	}
}
