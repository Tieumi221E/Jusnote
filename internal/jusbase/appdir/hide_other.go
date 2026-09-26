//go:build !windows

package appdir

// Hide does nothing: a leading dot already hides the folder.
func Hide(string) {}
