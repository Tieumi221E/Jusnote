// Command winres writes cmd/jusnote/rsrc_windows_amd64.syso: the app
// icon (all sizes made by assets/make_icon.py) as icon group 1, which the Go
// linker picks up for the Windows build and Explorer shows, and the night
// icon as group 2, which the shell puts on the window in the dark theme.
//
//	cd tools/winres && go run .
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/tc-hib/winres"
)

var sizes = []int{16, 20, 24, 32, 40, 48, 64, 96, 128, 256}

func load(root, prefix string) *winres.Icon {
	var imgs []image.Image
	for _, n := range sizes {
		f, err := os.Open(filepath.Join(root, "assets", fmt.Sprintf("%s-%d.png", prefix, n)))
		if err != nil {
			fail(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			fail(err)
		}
		imgs = append(imgs, img)
	}
	icon, err := winres.NewIconFromImages(imgs)
	if err != nil {
		fail(err)
	}
	return icon
}

func main() {
	root := filepath.Join("..", "..")
	rs := &winres.ResourceSet{}
	if err := rs.SetIcon(winres.ID(1), load(root, "icon")); err != nil {
		fail(err)
	}
	if err := rs.SetIcon(winres.ID(2), load(root, "icon-dark")); err != nil {
		fail(err)
	}
	out, err := os.Create(filepath.Join(root, "cmd", "jusnote", "rsrc_windows_amd64.syso"))
	if err != nil {
		fail(err)
	}
	defer out.Close()
	if err := rs.WriteObject(out, winres.ArchAMD64); err != nil {
		fail(err)
	}
	fmt.Println("wrote", out.Name())
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
