//go:build windows

package shell

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// Native dialogs, bound into the page. go-webview2 runs bound functions on
// the UI thread, so the dialogs are modal to the app window.

var (
	comdlg32             = syscall.NewLazyDLL("comdlg32.dll")
	shell32              = syscall.NewLazyDLL("shell32.dll")
	ole32                = syscall.NewLazyDLL("ole32.dll")
	procGetOpenFileName  = comdlg32.NewProc("GetOpenFileNameW")
	procSHBrowseFolder   = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDL = shell32.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
)

type openFileName struct {
	structSize    uint32
	owner         uintptr
	instance      uintptr
	filter        *uint16
	customFilter  *uint16
	maxCustFilter uint32
	filterIndex   uint32
	file          *uint16
	maxFile       uint32
	fileTitle     *uint16
	maxFileTitle  uint32
	initialDir    *uint16
	title         *uint16
	flags         uint32
	fileOffset    uint16
	fileExtension uint16
	defExt        *uint16
	custData      uintptr
	hook          uintptr
	templateName  *uint16
	reserved      uintptr
	reservedDword uint32
	flagsEx       uint32
}

const (
	ofnFileMustExist = 0x1000
	ofnPathMustExist = 0x800
	ofnNoChangeDir   = 0x8
	ofnExplorer      = 0x80000
)

func utf16z(pairs ...string) *uint16 {
	var u []uint16
	for _, s := range pairs {
		u = append(u, syscall.StringToUTF16(s)...)
	}
	u = append(u, 0)
	return &u[0]
}

// pickFile shows an open-file dialog; "" means cancelled. pattern is a
// Win32 filter pattern such as "*.md;*.markdown".
func pickFile(owner uintptr, title, pattern string) string {
	if pattern == "" {
		pattern = "*.*"
	}
	buf := make([]uint16, 4096)
	ofn := openFileName{
		owner: owner, filter: utf16z("文件", pattern, "所有文件", "*.*"), filterIndex: 1,
		file: &buf[0], maxFile: uint32(len(buf)),
		title: syscall.StringToUTF16Ptr(title),
		flags: ofnFileMustExist | ofnPathMustExist | ofnNoChangeDir | ofnExplorer,
	}
	ofn.structSize = uint32(unsafe.Sizeof(ofn))
	if r, _, _ := procGetOpenFileName.Call(uintptr(unsafe.Pointer(&ofn))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

type browseInfo struct {
	owner       uintptr
	root        uintptr
	displayName *uint16
	title       *uint16
	flags       uint32
	callback    uintptr
	param       uintptr
	image       int32
}

const (
	bifReturnOnlyFSDirs = 0x1
	bifNewDialogStyle   = 0x40
)

// pickFolder shows a folder dialog; "" means cancelled.
func pickFolder(owner uintptr, title string) string {
	name := make([]uint16, 260)
	bi := browseInfo{owner: owner, displayName: &name[0], title: syscall.StringToUTF16Ptr(title),
		flags: bifReturnOnlyFSDirs | bifNewDialogStyle}
	pidl, _, _ := procSHBrowseFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	defer procCoTaskMemFree.Call(pidl)
	path := make([]uint16, 32768)
	if r, _, _ := procSHGetPathFromIDL.Call(pidl, uintptr(unsafe.Pointer(&path[0]))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(path)
}

// reveal opens Explorer with path selected.
func reveal(path string) error {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
	return cmd.Start()
}

// openExternal opens a web URL in the user's default browser.
func openExternal(u string) error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", u).Start()
}
