package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/Tieumi221E/Jusnote/internal/jusbase/appdir"
	"github.com/Tieumi221E/Jusnote/internal/jusbase/shell"
	"github.com/Tieumi221E/Jusnote/internal/server"
	"github.com/Tieumi221E/Jusnote/internal/ui"
)

// cmdGUI opens the editor window. With no -notebook it reopens the last
// notebook used, or shows the welcome screen when there is none; opening a
// notebook also creates its git repository, because auto-commit is the
// point.
func cmdGUI(args []string) error {
	fs := flag.NewFlagSet("gui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("notebook", "", "notebook folder (default: the last one used)")
	serve := fs.Bool("serve", false, "serve without opening a window and keep running")
	if _, err := parse(fs, args, 0); err != nil {
		return err
	}
	if !ui.Built() {
		return fmt.Errorf("the editor page is not built: run `npm --prefix web run build` first")
	}
	data, _, err := appdir.Dir("jusnote")
	if err != nil {
		return err
	}
	srv := server.New(ui.FS(), data)
	srv.Version = version
	if err := srv.OpenDefault(*dir); err != nil {
		return err
	}
	base, err := srv.Start()
	if err != nil {
		return err
	}
	defer srv.Close()

	if *serve {
		fmt.Println(base)
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)
		<-stop
		srv.CommitPending()
		return nil
	}

	temp, err := appdir.Temp(data)
	if err != nil {
		return err
	}
	win, err := shell.Open(shell.Options{
		Title:   "Jusnote",
		URL:     base,
		Dark:    srv.Dark(),
		Profile: filepath.Join(temp, fmt.Sprintf("webview2-%d", os.Getpid())),
	})
	if err != nil {
		return err
	}
	win.Run()
	// The page autosaves as you type; what it wrote is committed now.
	srv.CommitPending()
	appdir.ClearTemp(temp)
	return nil
}
