//go:build windows

package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

var (
	trayTestUser32      = windows.NewLazySystemDLL("user32.dll")
	trayTestPostMessage = trayTestUser32.NewProc("PostMessageW")
	trayTestIsVisible   = trayTestUser32.NewProc("IsWindowVisible")
	trayTestIsWindow    = trayTestUser32.NewProc("IsWindow")
	trayTestSetHook     = trayTestUser32.NewProc("SetWindowsHookExW")
	trayTestUnhook      = trayTestUser32.NewProc("UnhookWindowsHookEx")
	trayTestNextHook    = trayTestUser32.NewProc("CallNextHookEx")
	trayTestPostThread  = trayTestUser32.NewProc("PostThreadMessageW")
)

// trayTestCWP is CWPSTRUCT, what a WH_CALLWNDPROC hook gets.
type trayTestCWP struct {
	LParam, WParam uintptr
	Message        uint32
	HWND           uintptr
}

// Gio sends DestroyEvent from WM_DESTROY, while DestroyWindow is still
// running (WM_NCDESTROY comes after it, then the HWND goes), so App.Run
// can return before the window is gone: on the runner by 0.3 to 3.4 ms,
// in 19 of 50 runs. Two hooks on the window's thread, set just before
// Quit (steps 1-4 run without them), make that gap every run's and give
// the test the moment DestroyWindow returned: trayTestOnSend holds the window's WM_NCDESTROY for
// trayTestNCDestroyHold and posts the thread a WM_APP; trayTestOnGet
// sees the thread's next message read after it, which can only come once
// DestroyWindow returned to the message loop.
var trayTestDestroy struct {
	mu   sync.Mutex
	hwnd uintptr       // the window the hooks watch
	nc   bool          // its WM_NCDESTROY was sent
	gone chan struct{} // closed at the thread's first message read after that
}

var trayTestOnSend = syscall.NewCallback(func(code, wParam, lParam uintptr) uintptr {
	if int32(code) == 0 {
		cwp := *(**trayTestCWP)(unsafe.Pointer(&lParam)) // lParam is a *CWPSTRUCT
		d := &trayTestDestroy
		d.mu.Lock()
		ours := cwp.Message == trayTestWMNCDestroy && cwp.HWND == d.hwnd
		if ours {
			d.nc = true
		}
		d.mu.Unlock()
		if ours {
			time.Sleep(trayTestNCDestroyHold)
			// a message for the loop to read once DestroyWindow returned:
			// GetMessage returns posted ones before WM_QUIT, which the
			// WH_GETMESSAGE hook didn't always see (9 runs in 50)
			trayTestPostThread.Call(uintptr(windows.GetCurrentThreadId()), trayTestWMApp, 0, 0)
		}
	}
	r, _, _ := trayTestNextHook.Call(0, code, wParam, lParam)
	return r
})

var trayTestOnGet = syscall.NewCallback(func(code, wParam, lParam uintptr) uintptr {
	if int32(code) == 0 {
		d := &trayTestDestroy
		d.mu.Lock()
		if d.nc && d.gone != nil {
			close(d.gone)
			d.gone = nil
		}
		d.mu.Unlock()
	}
	r, _, _ := trayTestNextHook.Call(0, code, wParam, lParam)
	return r
})

const (
	trayTestWMClose     = 0x0010
	trayTestWMSysCmd    = 0x0112
	trayTestSCClose     = 0xF060
	trayTestWaitTimeout = 30 * time.Second

	trayTestWHGetMessage  = 3 // WH_GETMESSAGE
	trayTestWHCallWndProc = 4 // WH_CALLWNDPROC
	trayTestWMNCDestroy   = 0x0082
	trayTestWMApp         = 0x8000
	trayTestNCDestroyHold = 100 * time.Millisecond
)

// trayTestStacks is the goroutines in Gio's app package or the App, for a
// timeout's message.
func trayTestStacks() string {
	buf := make([]byte, 4<<20)
	buf = buf[:runtime.Stack(buf, true)]
	var keep []string
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "gioui.org/app.") || strings.Contains(g, "internal/ui.(*App)") {
			keep = append(keep, g)
		}
	}
	return strings.Join(keep, "\n\n")
}

// TestCloseToTrayWindow is runFromTray on a real Windows desktop
// (CLACK_WINDOWS_TRAY_TESTS=1, ci's windows-applog job): App.Run's own
// window, signed in to internal/fakeslack, a tray icon (a fake that
// records balloons). The close button's SC_CLOSE and a WM_CLOSE (Alt+F4,
// the taskbar's Close window) hide the window instead of closing it; the
// first close shows the RunFromTray balloon once; the hidden window draws
// no frames while posted work still runs; Raise (the tray icon's click,
// another launch) shows it again; Quit while hidden ends Run and destroys
// the window.
//
// What it proves: Gio patch 13's hide and show on a real HWND, the
// ClosingEvent abort from the UI goroutine, the wake-up drain, Quit's
// isQuitting. What it doesn't: Explorer's tray icon and balloon (the
// desktop package's TestTrayBalloon calls Shell_NotifyIcon), or that
// SetForegroundWindow wins the foreground on a runner.
func TestCloseToTrayWindow(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_TRAY_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_TRAY_TESTS not set")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	// The fonts load only once the window is hidden, as on CI's cold first
	// run (no "first frame painted" before the close): the hidden window
	// draws no frame to take them, and its wake-ups ran no posted work.
	fontsGo := make(chan struct{})
	var fontsOnce sync.Once
	releaseFonts := func() { fontsOnce.Do(func() { close(fontsGo) }) }
	t.Cleanup(releaseFonts)
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	hwnds := make(chan uintptr, 1)
	var frames atomic.Int64
	images := imgcache.New(fs.Fetch, "", nil)
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), shaperHold: fontsGo, OnWindowEvent: func(e event.Event) {
		switch e := e.(type) {
		case app.Win32ViewEvent:
			if e.HWND != 0 {
				select {
				case hwnds <- e.HWND:
				default:
				}
			}
		case app.FrameEvent:
			frames.Add(1)
		}
	}}, images)
	a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	tray := &fakeTray{}
	a.SetTray(tray)
	done := make(chan error, 1)
	go func() { done <- a.Run() }()

	var hwnd uintptr
	select {
	case hwnd = <-hwnds:
	case <-time.After(60 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	until := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(trayTestWaitTimeout)
		for !ok() {
			select {
			case err := <-done:
				t.Fatalf("%s: Run returned (%v)", what, err)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s\n%s", what, trayTestStacks())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	visible := func() bool { r, _, _ := trayTestIsVisible.Call(hwnd); return r != 0 }
	exists := func() bool { r, _, _ := trayTestIsWindow.Call(hwnd); return r != 0 }
	until("the first frame (the boot placeholder: the fonts are held)", func() bool { return frames.Load() > 0 })
	time.Sleep(2 * time.Second) // boot settles (the reveal, the first history)

	// 1. the caption's close button: WM_SYSCOMMAND SC_CLOSE → WM_CLOSE
	trayTestPostMessage.Call(hwnd, trayTestWMSysCmd, trayTestSCClose, 0)
	until("the window hidden by the close button", func() bool { return !visible() })
	if !exists() {
		t.Fatal("the close destroyed the window")
	}
	until("the RunFromTray balloon", func() bool { return len(tray.got()) == 1 })
	releaseFonts()
	if b := tray.got()[0]; b[0] != "Why didn’t Clack quit?" {
		t.Errorf("balloon title %q", b[0])
	}

	// 2. hidden: no frames, but posted work runs
	time.Sleep(time.Second)
	f0 := frames.Load()
	ran := make(chan struct{})
	a.Post(func() { close(ran) })
	select {
	case <-ran:
	case <-time.After(10 * time.Second):
		t.Fatalf("work posted while hidden didn't run\n%s", trayTestStacks())
	}
	a.Invalidate()
	time.Sleep(time.Second)
	if n := frames.Load() - f0; n != 0 {
		t.Errorf("the hidden window drew %d frames", n)
	}

	// 3. the tray icon's click: Raise shows it, and it draws again
	a.Raise()
	until("the window shown by Raise", visible)
	until("a frame after Raise", func() bool { return frames.Load() > f0 })

	// 4. WM_CLOSE (Alt+F4, the taskbar's Close window): hidden, and the
	// notice was the first close's only
	trayTestPostMessage.Call(hwnd, trayTestWMClose, 0, 0)
	until("the window hidden by WM_CLOSE", func() bool { return !visible() })
	time.Sleep(500 * time.Millisecond)
	if n := len(tray.got()); n != 1 {
		t.Errorf("%d balloons after the second close, want 1", n)
	}

	// 5. Quit Clack while hidden: the window closes and Run returns
	tid, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), nil)
	if err != nil {
		t.Fatal(err)
	}
	gone := make(chan struct{})
	trayTestDestroy.mu.Lock()
	trayTestDestroy.hwnd, trayTestDestroy.nc, trayTestDestroy.gone = hwnd, false, gone
	trayTestDestroy.mu.Unlock()
	for _, hk := range []struct{ id, proc uintptr }{{trayTestWHCallWndProc, trayTestOnSend}, {trayTestWHGetMessage, trayTestOnGet}} {
		h, _, err := trayTestSetHook.Call(hk.id, hk.proc, 0, uintptr(tid))
		if h == 0 {
			t.Fatalf("SetWindowsHookEx(%d): %v", hk.id, err)
		}
		defer trayTestUnhook.Call(h)
	}
	a.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatalf("Quit didn't end Run\n%s", trayTestStacks())
	}
	_ = gone // BEFORE: Run's return taken as the window gone
	if exists() {
		t.Error("the window outlived Quit")
	}
}
