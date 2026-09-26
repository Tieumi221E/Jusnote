package main

import (
	"strings"
	"syscall"
	"unsafe"
)

// showError shows err in a message box when there is nowhere else to see
// it: the exe is a GUI program, so started from Explorer it has no console
// and stderr goes nowhere.
func showError(err error) {
	if hasStderr() {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "WebView2") {
		msg += "\n\nJusnote 需要 Microsoft Edge WebView2 运行时（Windows 11 自带）。Windows 10 上请先安装它。" +
			"\nJusnote には Microsoft Edge WebView2 ランタイムが必要です（Windows 11 は標準搭載）。"
	}
	text, _ := syscall.UTF16PtrFromString(msg)
	title, _ := syscall.UTF16PtrFromString("Jusnote")
	const mbIconError = 0x10
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), mbIconError)
}

// hasStderr reports whether stderr leads somewhere (a console, a pipe or a
// file), as when run from a terminal or a script.
func hasStderr() bool {
	h, err := syscall.GetStdHandle(syscall.STD_ERROR_HANDLE)
	if err != nil || h == syscall.InvalidHandle || h == 0 {
		return false
	}
	t, err := syscall.GetFileType(h)
	return err == nil && t != 0 // FILE_TYPE_UNKNOWN
}
