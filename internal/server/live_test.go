package server

import (
	"context"
	"testing"

	"github.com/Tieumi221E/Jus/capreg"
)

// prefs.set: typed values from the command line's strings, refused when
// wrong (nothing changes), kept.
func TestPrefsSet(t *testing.T) {
	s, _ := start(t)
	r := capreg.New("jusnote", "test")
	RegisterWindow(r, func() *Server { return s })
	ctx := context.Background()
	for _, kv := range [][2]string{{"theme", "light"}, {"size", "19"}, {"wrap", "false"}, {"files", "notes"}} {
		if _, err := r.Call(ctx, "prefs.set", []byte(`{"key":"`+kv[0]+`","value":"`+kv[1]+`"}`)); err != nil {
			t.Fatalf("prefs.set %s %s: %v", kv[0], kv[1], err)
		}
	}
	for _, kv := range [][2]string{{"wrap", "maybe"}, {"theme", "blue"}, {"size", "big"}} {
		if _, err := r.Call(ctx, "prefs.set", []byte(`{"key":"`+kv[0]+`","value":"`+kv[1]+`"}`)); capreg.AsError(err).Kind != capreg.KindUsage {
			t.Fatalf("prefs.set %s %s: %v, want a usage error", kv[0], kv[1], err)
		}
	}
	p := s.prefs.get()
	if p.Theme != "light" || p.Size != 19 || p.Wrap || p.Files != "notes" {
		t.Fatalf("prefs after set: %+v", p)
	}
}
