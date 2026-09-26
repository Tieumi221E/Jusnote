package main

import (
	"os"
	"syscall"
)

// The Windows build is a GUI program (go build -ldflags=-H=windowsgui), so
// opening it from Explorer shows no console window. Run from a terminal,
// the command-line subcommands still print there: the process attaches to
// its parent's console and writes to it. Output that is already redirected
// (a pipe or a file, as when run from a script) is left as it is.

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
)

const attachParentProcess = ^uintptr(0) // ATTACH_PARENT_PROCESS (-1)

func init() {
	if usable(os.Stdout) && usable(os.Stderr) {
		return
	}
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return // started from Explorer: no console to write to
	}
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	if !usable(os.Stdout) {
		os.Stdout = out
	}
	if !usable(os.Stderr) {
		os.Stderr = out
	}
}

// usable reports whether f is a real handle (a GUI program gets none unless
// its parent redirected it).
func usable(f *os.File) bool {
	h := syscall.Handle(f.Fd())
	if h == 0 || h == syscall.InvalidHandle {
		return false
	}
	_, err := syscall.GetFileType(h)
	return err == nil
}
