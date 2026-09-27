//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// With JUSNOTE_STATS set, the process reports its peak memory on stderr as
// it exits, for tools/bench.ps1: a short-lived process's peak cannot be
// read from outside after it has gone.
func reportStats() {
	if os.Getenv("JUSNOTE_STATS") == "" {
		return
	}
	type counters struct {
		Cb                         uint32
		PageFaultCount             uint32
		PeakWorkingSetSize         uintptr
		WorkingSetSize             uintptr
		QuotaPeakPagedPoolUsage    uintptr
		QuotaPagedPoolUsage        uintptr
		QuotaPeakNonPagedPoolUsage uintptr
		QuotaNonPagedPoolUsage     uintptr
		PagefileUsage              uintptr
		PeakPagefileUsage          uintptr
	}
	var c counters
	c.Cb = uint32(unsafe.Sizeof(c))
	h, _ := syscall.GetCurrentProcess()
	proc := syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	if r, _, _ := proc.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.Cb)); r != 0 {
		fmt.Fprintf(os.Stderr, "jusnote-stats peak-working-set %d\n", c.PeakWorkingSetSize)
	}
}
