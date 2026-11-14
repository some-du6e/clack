//go:build windows

package ui

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/desktop/uiatest"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

var trayTestIsIconic = trayTestUser32.NewProc("IsIconic")

// TestTaskbarWindow is the taskbar button of App.Run's own window on a
// real Windows desktop (CLACK_WINDOWS_TASKBAR_TESTS=1, ci's windows-applog
// job), signed in to internal/fakeslack: the unread badge goes on through
// ITaskbarList3 (S_OK for the window's HWND); with "Always" flashing a
// close minimizes the window and shows the WindowFlash balloon, and a
// notification flashes its button (the shell's HSHELL_FLASH); with "When
// left idle", an idle user (the idle time faked past 20 s) and the window
// hidden to the tray, a notification shows it minimized without
// activating it and flashes it. CLACK_TOAST_SHOTS gets the desktop.
//
// What it proves: dock.go's calls on Gio's window thread through
// Window.Run, from Clack's own count, close and notification paths, reach
// Windows and the shell. What it doesn't: the overlay's look (the
// screenshots), the runner's real idle time (desktop's TestSystemIdleTime
// covers GetLastInputInfo), or a real toast (the notifier here records).
func TestTaskbarWindow(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_TASKBAR_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_TASKBAR_TESTS not set")
	}
	t.Logf("ToastsEnabled for Clack's app id on this runner: %v", desktop.ToastsEnabled())
	hook, err := uiatest.StartShellHook()
	if err != nil {
		t.Fatal(err)
	}
	defer hook.Close()

	type overlayCall struct {
		hwnd uintptr
		n    int
		err  error
	}
	var mu sync.Mutex
	var overlays []overlayCall
	realOverlay, realIdle, realToasts := taskbarOverlay, systemIdleTime, toastsEnabled
	taskbarOverlay = func(h uintptr, n int, d string) error {
		err := realOverlay(h, n, d)
		mu.Lock()
		overlays = append(overlays, overlayCall{h, n, err})
		mu.Unlock()
		return err
	}
	var idle atomic.Int64
	systemIdleTime = func() int { return int(idle.Load()) }
	toastsEnabled = func() bool { return true }
	t.Cleanup(func() { taskbarOverlay, systemIdleTime, toastsEnabled = realOverlay, realIdle, realToasts })

	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	hwnds := make(chan uintptr, 1)
	var frames atomic.Int64
	images := imgcache.New(fs.Fetch, "", nil)
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
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
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	onUI := func(f func()) { // a.Post, waited for: the setting is in before the next close
		ran := make(chan struct{})
		a.Post(func() { f(); close(ran) })
		select {
		case <-ran:
		case <-time.After(10 * time.Second):
			t.Fatal("posted work didn't run")
		}
	}
	visible := func() bool { r, _, _ := trayTestIsVisible.Call(hwnd); return r != 0 }
	iconic := func() bool { r, _, _ := trayTestIsIconic.Call(hwnd); return r != 0 }
	// retitle changes the title's count while the window is minimized or
	// hidden, as core's next count does: the test's own HomeCount is
	// replaced by the fake's on the next model (flakes-1114: "4 new
	// items" became "3 new items" just after a hide or a revive in 2 of
	// 30 runs). Window.Option(Title) used to end in Gio's
	// ShowWindow(SW_SHOWNORMAL), bringing the window back; Gio patch 20
	// leaves it as it was.
	retitle := func(what string, n int, want func() bool) {
		t.Helper()
		onUI(func() {
			a.model().HomeCount = n
			a.updateTitle()
		})
		time.Sleep(300 * time.Millisecond)
		if !want() {
			t.Fatalf("%s: the title's count (%d) changed the window: visible %v, iconic %v", what, n, visible(), iconic())
		}
	}
	until("the first frames", func() bool { return frames.Load() > 2 })
	time.Sleep(2 * time.Second)

	// 1. the badge: Clack's count for the window, through Window.Run
	a.Post(func() {
		a.model().HomeCount = 4
		a.updateDock()
	})
	until("the overlay for 4", func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, o := range overlays {
			if o.n == 4 && o.hwnd == hwnd {
				if o.err != nil {
					t.Fatalf("SetTaskbarOverlay: %v", o.err)
				}
				return true
			}
		}
		return false
	})
	time.Sleep(time.Second)
	uiScreenshot(t, "clack-taskbar-badge-4")

	// 2. "Always": the close minimizes, the balloon explains, a
	// notification flashes the button
	onUI(func() {
		a.prefs.WindowFlashBehavior = flashAlways
		a.notify = &recNotifier{}
	})
	trayTestPostMessage.Call(hwnd, trayTestWMClose, 0, 0)
	until("the window minimized by the close", func() bool { return visible() && iconic() })
	retitle("minimized by the close", 5, func() bool { return visible() && iconic() })
	until("the WindowFlash balloon", func() bool { return len(tray.got()) == 1 })
	if b := tray.got()[0]; b[1] != "Clack’s set to always flash when you get notifications. To change this, visit Preferences > Notifications." {
		t.Errorf("balloon %q", b)
	}
	hook.Drain()
	a.Post(func() { a.Notify("fakeslack", "a message") })
	if !hook.WaitFlash(hwnd, 10*time.Second) {
		t.Fatalf("always: no HSHELL_FLASH for Clack's window %#x after a notification; the shell hook heard %v", hwnd, hook.Seen())
	}
	uiScreenshot(t, "clack-taskbar-flash-always")
	a.Raise() // FOREGROUND_APP: the flash stopped, the badge again
	until("the window back from Raise", func() bool { return visible() && !iconic() })

	// 3. "When left idle", an idle user, the window hidden to the tray
	idle.Store(30)
	onUI(func() { a.prefs.WindowFlashBehavior = flashIdle })
	trayTestPostMessage.Call(hwnd, trayTestWMClose, 0, 0)
	until("the window hidden by the close", func() bool { return !visible() })
	retitle("hidden by the close", 6, func() bool { return !visible() })
	hook.Drain()
	a.Post(func() { a.Notify("fakeslack", "another message") })
	until("the hidden window shown minimized", func() bool { return visible() && iconic() })
	retitle("shown minimized", 7, func() bool { return visible() && iconic() })
	if !hook.WaitFlash(hwnd, 10*time.Second) {
		t.Fatalf("idle: no HSHELL_FLASH for the revived window; the shell hook heard %v", hook.Seen())
	}
	uiScreenshot(t, "clack-taskbar-flash-revived")

	// 4. an active user: nothing
	idle.Store(3)
	a.Raise()
	until("the window back", func() bool { return visible() && !iconic() })
	time.Sleep(500 * time.Millisecond)
	hook.Drain()
	a.Post(func() { a.Notify("fakeslack", "a third message") })
	if hook.WaitFlash(hwnd, 3*time.Second) {
		t.Error("idle mode, active user: the window flashed")
	}

	a.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Quit didn't end Run")
	}
}
