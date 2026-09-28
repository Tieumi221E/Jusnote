//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach makes cmd a process of its own, outliving the command that started it.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
