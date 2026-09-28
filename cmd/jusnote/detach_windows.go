package main

import (
	"os/exec"
	"syscall"
)

// detach makes cmd a process of its own: it outlives the command that
// started it, with no console (Ctrl+C in the terminal does not close it).
func detach(cmd *exec.Cmd) {
	const detachedProcess = 0x00000008
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP}
}
