package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/Tieumi221E/Jusnote/internal/jusbase/appdir"
	"github.com/Tieumi221E/Jusnote/internal/jusbase/shell"
	"github.com/Tieumi221E/Jusnote/internal/server"
	"github.com/Tieumi221E/Jusnote/internal/ui"
)

// cmdGUI opens the editor window. With no -notebook it reopens the last
// notebook used, or shows the welcome screen when there is none; opening a
// notebook also creates its git repository, because the history is the
// point.
//
// -selftest runs the page's scripted checks and timings in the real
// window (web/src/selftest.ts) against the notebook given — use a copy,
// it is edited — prints the report as JSON on stdout and exits.
func cmdGUI(args []string) error {
	fs := flag.NewFlagSet("gui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("notebook", "", "notebook folder (default: the last one used)")
	serve := fs.Bool("serve", false, "serve without opening a window and keep running")
	selftest := fs.Bool("selftest", false, "run the page selftest, print the report, exit")
	dataDir := fs.String("data", "", "the app's settings folder (default: next to the exe)")
	if err := fs.Parse(args); err != nil {
		return usageError{"gui: " + err.Error()}
	}
	if fs.NArg() > 0 {
		return usageError{"gui: unexpected argument " + fs.Arg(0)}
	}
	if *selftest && *dir == "" {
		return usageError{"gui -selftest needs -notebook (a copy: the selftest edits it)"}
	}
	if !ui.Built() {
		return fmt.Errorf("the editor page is not built: run `npm --prefix web run build` first")
	}
	data := *dataDir
	if data == "" {
		d, _, err := appdir.Dir("jusnote")
		if err != nil {
			return err
		}
		data = d
	}
	srv := server.New(ui.FS(), data)
	srv.Version = version
	if err := srv.OpenDefault(*dir); err != nil {
		return err
	}
	done := make(chan struct{})
	if *selftest {
		start := time.Now()
		srv.OnSelftest = func(report []byte) {
			os.Stdout.Write(report)
			fmt.Fprintf(os.Stderr, "selftest: report after %d ms\n", time.Since(start).Milliseconds())
			close(done)
		}
	}
	base, err := srv.Start()
	if err != nil {
		return err
	}
	defer srv.Close()
	url := base
	if *selftest {
		url += "?selftest=1"
	}

	if *serve {
		fmt.Println(url)
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)
		select {
		case <-stop:
		case <-done:
		}
		srv.CommitPending()
		return nil
	}

	temp, err := appdir.Temp(data)
	if err != nil {
		return err
	}
	win, err := shell.Open(shell.Options{
		Title:   "Jusnote",
		URL:     url,
		Dark:    srv.Dark(),
		Profile: filepath.Join(temp, fmt.Sprintf("webview2-%d", os.Getpid())),
	})
	if err != nil {
		return err
	}
	if *selftest {
		go func() {
			select {
			case <-done:
			case <-time.After(5 * time.Minute):
				fmt.Fprintln(os.Stderr, "selftest: no report in time")
			}
			win.Close()
		}()
	}
	win.Run()
	// The page autosaves as you type; what it wrote is committed now.
	srv.CommitPending()
	appdir.ClearTemp(temp)
	return nil
}
