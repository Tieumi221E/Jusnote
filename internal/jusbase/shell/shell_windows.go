//go:build windows

// Package shell opens an app page in a WebView2 window. It is the Jus base
// shell: DPI awareness, a title bar that follows the page's theme, a
// fullscreen toggle and native file/folder dialogs, all bound into the
// page under generic names so every Jus app can reuse it.
package shell

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

// Options describe the window to open.
type Options struct {
	Title         string
	URL           string
	Dark          bool
	Profile       string // WebView2 data folder (history/caches); empty = default
	Width, Height int    // in 96-DPI units; zero picks 1280x760
}

// Window is an open app window.
type Window struct {
	w      webview2.WebView
	hwnd   uintptr
	saved  windowPlacement
	style  uintptr
	isFull bool
	icons  [2]uintptr
}

var (
	procSetDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForSystem        = user32.NewProc("GetDpiForSystem")
)

// dpiAwarenessPerMonitorV2 is DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2.
const dpiAwarenessPerMonitorV2 = ^uintptr(3) // -4

// Without this the process is DPI-unaware: Windows renders the window at
// 96 DPI and stretches it, so text is blurred on a scaled display.
func init() {
	procSetDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)
}

func scaled(v int) int {
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		return v
	}
	return v * int(dpi) / 96
}

// Open creates the window; call Run to show it.
func Open(o Options) (*Window, error) {
	if o.Profile != "" {
		// Also as the environment variable, which WebView2 prefers to the
		// argument: go-webview2 passes the argument as a temporary UTF-16
		// buffer that nothing keeps alive while WebView2 reads it on another
		// thread.
		os.Setenv("WEBVIEW2_USER_DATA_FOLDER", o.Profile)
	}
	wpx, hpx := o.Width, o.Height
	if wpx == 0 {
		wpx = 1280
	}
	if hpx == 0 {
		hpx = 760
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  o.Profile,
		WindowOptions: webview2.WindowOptions{
			Title: o.Title, Width: uint(scaled(wpx)), Height: uint(scaled(hpx)), Center: true,
			IconId: appIcon,
		},
	})
	if w == nil {
		return nil, errNoWebView
	}
	win := &Window{w: w, hwnd: uintptr(w.Window())}
	win.setIcons(o.Dark)
	win.setDarkFrame(o.Dark)

	if err := win.bindGeneric(); err != nil {
		w.Destroy()
		return nil, err
	}
	w.Navigate(o.URL)
	return win, nil
}

// Bind makes a Go function callable from the page.
func (win *Window) Bind(name string, fn any) error { return win.w.Bind(name, fn) }

// bindGeneric wires the bindings every Jus app shares.
func (win *Window) bindGeneric() error {
	binds := map[string]any{
		"kpFullscreen": func(on bool) error {
			win.w.Dispatch(func() { win.setFullscreen(on) })
			return nil
		},
		"kpTheme": func(dark bool) error {
			win.w.Dispatch(func() {
				win.setDarkFrame(dark)
				win.setIcons(dark)
			})
			return nil
		},
		"kpTitle": func(t string) error {
			win.w.Dispatch(func() { win.w.SetTitle(t) })
			return nil
		},
		"kpPickFile": func(title, pattern string) string {
			return pickFile(win.hwnd, title, pattern)
		},
		"kpPickFolder": func(title string) string {
			return pickFolder(win.hwnd, title)
		},
		"kpReveal":   func(path string) error { return reveal(path) },
		"kpTerminal": func(dir string) error { return terminal(dir) },
		// A link of another Jus app (jus://play/…): to that app ("notinstalled" when it is not here).
		"kpOpenJus": func(link string) (string, error) {
			if err := OpenJusLink(link); errors.Is(err, ErrNotInstalled) {
				return "notinstalled", nil
			} else if err != nil {
				return "", err
			}
			return "opened", nil
		},
		// To the front: a command from the command line (editor open) wants the window seen.
		"kpFront": func() error {
			win.w.Dispatch(win.front)
			return nil
		},
		"kpOpenExternal": func(u string) error {
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "mailto:") {
				return errors.New("refusing to open a non-web URL")
			}
			return openExternal(u)
		},
	}
	for name, f := range binds {
		if err := win.w.Bind(name, f); err != nil {
			return err
		}
	}
	return nil
}

// Run blocks until the window closes.
func (win *Window) Run() {
	defer win.w.Destroy()
	win.w.Run()
}

// Close ends Run from any goroutine.
func (win *Window) Close() {
	win.w.Dispatch(win.w.Terminate)
}

type errString string

func (e errString) Error() string { return string(e) }

const errNoWebView = errString("could not create a WebView2 window (is the WebView2 runtime installed?)")

// appIcon and nightIcon are icon group ids in the executable's resources,
// when the app ships a .syso; LoadImage simply finds nothing without one.
const (
	appIcon   = 1
	nightIcon = 2
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procGetWindowLongPtr    = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtr    = user32.NewProc("SetWindowLongPtrW")
	procGetWindowPlace      = user32.NewProc("GetWindowPlacement")
	procSetWindowPlace      = user32.NewProc("SetWindowPlacement")
	procMonitorFromWin      = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfo      = user32.NewProc("GetMonitorInfoW")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procGetModuleHandle     = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
	procLoadImage           = user32.NewProc("LoadImageW")
	procSendMessage         = user32.NewProc("SendMessageW")
	procGetDpiForWindow     = user32.NewProc("GetDpiForWindow")
	procGetSystemMetricsDpi = user32.NewProc("GetSystemMetricsForDpi")
	procDestroyIcon         = user32.NewProc("DestroyIcon")
	procDwmSetWindowAttr    = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
)

const (
	wmSetIcon  = 0x0080
	iconSmall  = 0
	iconBig    = 1
	imageIcon  = 1
	smCxIcon   = 11
	smCxSmIcon = 49
)

func (win *Window) setIcons(dark bool) {
	group := uintptr(appIcon)
	if dark {
		group = nightIcon
	}
	inst, _, _ := procGetModuleHandle.Call(0)
	dpi, _, _ := procGetDpiForWindow.Call(win.hwnd)
	if dpi == 0 {
		dpi = 96
	}
	for i, v := range []struct{ which, metric uintptr }{{iconSmall, smCxSmIcon}, {iconBig, smCxIcon}} {
		n, _, _ := procGetSystemMetricsDpi.Call(v.metric, dpi)
		if h, _, _ := procLoadImage.Call(inst, group, imageIcon, n, n, 0); h != 0 {
			procSendMessage.Call(win.hwnd, wmSetIcon, v.which, h)
			if win.icons[i] != 0 {
				procDestroyIcon.Call(win.icons[i])
			}
			win.icons[i] = h
		}
	}
}

const (
	gwlStyle           = ^uintptr(15) // -16
	wsOverlappedWindow = 0x00CF0000
	monitorDefaultNear = 2
	swpNoMove          = 0x0002
	swpNoSize          = 0x0001
	swpNoZOrder        = 0x0004
	swpNoOwnerZOrder   = 0x0200
	swpFrameChanged    = 0x0020
)

type rect struct{ Left, Top, Right, Bottom int32 }

type windowPlacement struct {
	Length, Flags, ShowCmd uint32
	MinPos, MaxPos         [2]int32
	Normal                 rect
}

type monitorInfo struct {
	Size          uint32
	Monitor, Work rect
	Flags         uint32
}

const (
	dwmwaUseImmersiveDarkMode = 20
	dwmwaCaptionColor         = 35
	dwmwaTextColor            = 36
)

func colorref(r, g, b byte) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }

// setDarkFrame makes the title bar and frame follow the page's theme.
func (win *Window) setDarkFrame(dark bool) {
	var v int32
	caption, text := colorref(0xf5, 0xf6, 0xf8), colorref(0x1a, 0x1c, 0x21)
	if dark {
		v = 1
		caption, text = colorref(0x0e, 0x0f, 0x12), colorref(0xe9, 0xea, 0xee)
	}
	procDwmSetWindowAttr.Call(win.hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	procDwmSetWindowAttr.Call(win.hwnd, dwmwaCaptionColor, uintptr(unsafe.Pointer(&caption)), unsafe.Sizeof(caption))
	procDwmSetWindowAttr.Call(win.hwnd, dwmwaTextColor, uintptr(unsafe.Pointer(&text)), unsafe.Sizeof(text))
}

// setFullscreen switches between a borderless full-monitor window and the
// saved normal window.
func (win *Window) setFullscreen(on bool) {
	if on == win.isFull {
		return
	}
	h := win.hwnd
	if on {
		win.style, _, _ = procGetWindowLongPtr.Call(h, gwlStyle)
		win.saved.Length = uint32(unsafe.Sizeof(win.saved))
		procGetWindowPlace.Call(h, uintptr(unsafe.Pointer(&win.saved)))
		mon, _, _ := procMonitorFromWin.Call(h, monitorDefaultNear)
		mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
		procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi)))
		procSetWindowLongPtr.Call(h, gwlStyle, win.style&^wsOverlappedWindow)
		r := mi.Monitor
		procSetWindowPos.Call(h, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top),
			swpNoOwnerZOrder|swpFrameChanged)
	} else {
		procSetWindowLongPtr.Call(h, gwlStyle, win.style|wsOverlappedWindow)
		procSetWindowPlace.Call(h, uintptr(unsafe.Pointer(&win.saved)))
		procSetWindowPos.Call(h, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoOwnerZOrder|swpFrameChanged)
	}
	win.isFull = on
}
