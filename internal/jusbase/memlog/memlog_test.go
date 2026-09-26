package memlog

import (
	"fmt"
	"strings"
	"testing"
)

func TestBufferKeepsTheLastLines(t *testing.T) {
	b := New(3)
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(b, "line %d\n", i)
	}
	b.Write([]byte("par"))
	b.Write([]byte("tial"))
	if got, want := b.String(), "line 3\nline 4\nline 5\npartial"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Contains(b.String(), "line 1") {
		t.Error("oldest line kept")
	}
}
