// Package ui embeds the editor page built from web/ (npm --prefix web run
// build).
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built page. Without a build it holds only .gitkeep and
// Built reports false.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// Built reports whether the page has been built into the binary.
func Built() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
