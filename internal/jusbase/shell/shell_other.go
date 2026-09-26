//go:build !windows

package shell

import "errors"

// Options describe the window to open.
type Options struct {
	Title         string
	URL           string
	Dark          bool
	Profile       string
	Width, Height int
}

// Window is a non-Windows stub.
type Window struct{}

// Open fails: the window needs Windows and WebView2.
func Open(Options) (*Window, error) {
	return nil, errors.New("the app window needs Windows and WebView2")
}

func (*Window) Bind(string, any) error { return nil }
func (*Window) Run()                   {}
func (*Window) Close()                 {}
