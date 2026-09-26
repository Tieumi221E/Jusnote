package appdir

import "syscall"

// Hide sets the hidden attribute on path (a notebook's ".jusnote"), as
// Explorer does not hide dot-folders by name. Errors are ignored: hiding
// is cosmetic.
func Hide(path string) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	attrs, err := syscall.GetFileAttributes(p)
	if err != nil {
		return
	}
	syscall.SetFileAttributes(p, attrs|syscall.FILE_ATTRIBUTE_HIDDEN)
}
