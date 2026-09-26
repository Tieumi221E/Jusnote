// Package memlog keeps an app's log in memory: what was opened and what
// went wrong, for the "copy diagnostic log" button, gone when the app
// closes. Only a debug run also writes it to a file.
package memlog

import (
	"bytes"
	"sync"
)

// Buffer is an io.Writer holding the last Max lines.
type Buffer struct {
	Max   int
	mu    sync.Mutex
	lines [][]byte
	part  []byte
}

// New keeps the last max lines.
func New(max int) *Buffer { return &Buffer{Max: max} }

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.part = append(b.part, p...)
	for {
		i := bytes.IndexByte(b.part, '\n')
		if i < 0 {
			break
		}
		b.lines = append(b.lines, append([]byte(nil), b.part[:i+1]...))
		b.part = b.part[i+1:]
	}
	if over := len(b.lines) - b.Max; over > 0 {
		b.lines = append([][]byte(nil), b.lines[over:]...)
	}
	return len(p), nil
}

// String is the kept lines, oldest first.
func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.Join(b.lines, nil)) + string(b.part)
}
