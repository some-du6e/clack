//go:build windows

package ui

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Slack's mainWindowSettings on App.Run's own window on a real desktop
// (CLACK_WINDOWS_BOUNDS_TESTS, ci's windows-applog job), signed in to
// internal/fakeslack, each test one launch with ui.json seeded as a
// previous one left it (Gio patch 25 places the window).

// boundsApp is one launch of App.Run.
type boundsApp struct {
	a      *App
	hwnd   uintptr
	dir    string
	frames atomic.Int64
	done   chan struct{} // closed when Run returns, with err
	err    error
}

// runBoundsApp launches Clack with ui.json's main_window_settings set to
// seed (nil: none) and waits for the reveal.
func runBoundsApp(t *testing.T, seed *mainWindowSettings) *boundsApp {
	t.Helper()
	if os.Getenv("CLACK_WINDOWS_BOUNDS_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_BOUNDS_TESTS not set")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ba := &boundsApp{dir: t.TempDir(), done: make(chan struct{})}
	if seed != nil {
		b, _ := json.Marshal(map[string]any{"main_window_settings": seed})
		if err := os.WriteFile(filepath.Join(ba.dir, "ui.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cache := filepath.Join(ba.dir, "cache")
	hwnds := make(chan uintptr, 1)
	images := imgcache.New(fs.Fetch, "", nil)
	ba.a = New(Options{ConfigDir: ba.dir, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		switch e := e.(type) {
		case app.Win32ViewEvent:
			if e.HWND != 0 {
				select {
				case hwnds <- e.HWND:
				default:
				}
			}
		case app.FrameEvent:
			ba.frames.Add(1)
		}
	}}, images)
	ba.a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	go func() { ba.err = ba.a.Run(); close(ba.done) }()
	t.Cleanup(func() {
		ba.a.Quit()
		select {
		case <-ba.done:
		case <-time.After(30 * time.Second):
			t.Log("Run didn't return after Quit")
		}
	})
	select {
	case ba.hwnd = <-hwnds:
	case <-time.After(60 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	ba.until(t, "the first frames", func() bool { return ba.frames.Load() > 2 })
	ba.until(t, "the reveal", func() (ok bool) { ba.on(t, func() { ok = ba.a.rv.done }); return })
	return ba
}

func (ba *boundsApp) until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !ok() {
		select {
		case <-ba.done:
			t.Fatalf("%s: Run returned (%v)", what, ba.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s (window %v, saved %+v)", what, ba.rect(), ba.saved())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// on runs f on the UI goroutine and waits.
func (ba *boundsApp) on(t *testing.T, f func()) {
	t.Helper()
	ran := make(chan struct{})
	ba.a.Post(func() { f(); close(ran) })
	select {
	case <-ran:
	case <-ba.done:
		t.Fatalf("Run returned (%v)", ba.err)
	case <-time.After(20 * time.Second):
		t.Fatal("posted work didn't run")
	}
}

func (ba *boundsApp) rect() image.Rectangle {
	var r tbRect
	tbGetWindowRect.Call(ba.hwnd, uintptr(unsafe.Pointer(&r)))
	return r.img()
}

func (ba *boundsApp) zoomed() bool {
	r, _, _ := tbIsZoomed.Call(ba.hwnd)
	return r != 0
}

// saved is ui.json's main_window_settings as written now.
func (ba *boundsApp) saved() *mainWindowSettings {
	b, err := os.ReadFile(filepath.Join(ba.dir, "ui.json"))
	if err != nil {
		return nil
	}
	var p prefs
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	return p.MainWindow
}

func (ba *boundsApp) quit(t *testing.T) {
	t.Helper()
	ba.a.Quit()
	select {
	case <-ba.done:
		if ba.err != nil {
			t.Fatalf("Run: %v", ba.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Quit didn't end Run")
	}
}

func boundsPrimary(t *testing.T) app.Display {
	t.Helper()
	ds := app.Displays()
	t.Logf("displays: %+v", ds)
	if len(ds) == 0 {
		t.Fatal("no displays")
	}
	return primaryDisplay(ds)
}

// TestWindowBoundsWindow:
//  1. the window opens exactly at ui.json's saved bounds;
//  2. moved and resized, ui.json has the new bounds soon after;
//  3. maximized (SC_MAXIMIZE, as Clack's caption button posts it),
//     ui.json says maximized, with Slack's default bounds on the display;
//  4. Quit ends Run, and ui.json keeps the maximized state.
func TestWindowBoundsWindow(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_BOUNDS_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_BOUNDS_TESTS not set")
	}
	prim := boundsPrimary(t)
	wa := prim.WorkArea
	r := image.Rect(wa.Min.X+40, wa.Min.Y+30, wa.Min.X+40+900, wa.Min.Y+30+620)
	ba := runBoundsApp(t, &mainWindowSettings{Bounds: winRectOf(r)})

	// 1.
	if got := ba.rect(); got != r {
		t.Errorf("opened at %v, want the saved %v", got, r)
	}

	// 2.
	r2 := image.Rect(wa.Min.X+70, wa.Min.Y+50, wa.Min.X+70+860, wa.Min.Y+50+600)
	tbSetWindowPos.Call(ba.hwnd, 0, uintptr(r2.Min.X), uintptr(r2.Min.Y), uintptr(r2.Dx()), uintptr(r2.Dy()), 0x0004|0x0010) // SWP_NOZORDER|SWP_NOACTIVATE
	ba.until(t, "ui.json with the moved bounds", func() bool {
		s := ba.saved()
		return s != nil && s.Bounds != nil && s.Bounds.rect() == r2 && !s.Maximized && !s.FullScreen
	})

	// 3.
	procPostMessage.Call(ba.hwnd, wmSysCommand, scMaximize, 0)
	ba.until(t, "maximized", ba.zoomed)
	want := defaultWindowBounds(prim)
	ba.until(t, "ui.json maximized", func() bool {
		s := ba.saved()
		return s != nil && s.Maximized && s.Bounds != nil && s.Bounds.rect() == want
	})

	// 4.
	ba.quit(t)
	if s := ba.saved(); s == nil || !s.Maximized || s.Bounds == nil || s.Bounds.rect() != want {
		t.Errorf("after Quit, ui.json has %+v, want maximized with %v", s, want)
	}
}

// TestWindowBoundsOffScreenWindow: saved maximized on a display that's
// gone (bounds off every display), the window opens maximized on the
// runner's display, and restores to Slack's validateBoundsForDisplay
// fallback there, which ui.json then keeps.
func TestWindowBoundsOffScreenWindow(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_BOUNDS_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_BOUNDS_TESTS not set")
	}
	prim := boundsPrimary(t)
	seed := &mainWindowSettings{Bounds: &winRect{X: 5000, Y: 5000, Width: 900, Height: 620}, Maximized: true}
	want := openWindowBounds(seed, app.Displays(), mainMinW, mainMinH)
	if !want.In(prim.WorkArea) {
		t.Fatalf("Slack's fallback %v isn't in the work area %v", want, prim.WorkArea)
	}
	ba := runBoundsApp(t, seed)
	ba.until(t, "maximized", ba.zoomed)
	procPostMessage.Call(ba.hwnd, wmSysCommand, scRestore, 0)
	ba.until(t, "restored onto the display", func() bool { return !ba.zoomed() && ba.rect() == want })
	ba.until(t, "ui.json with the bounds on the display", func() bool {
		s := ba.saved()
		return s != nil && !s.Maximized && s.Bounds != nil && s.Bounds.rect() == want
	})
	ba.quit(t)
}

// TestWindowBoundsFullScreenWindow: saved full screen, the window opens
// full screen (Slack's BrowserWindow fullscreen option), on the display
// its bounds are on, and ui.json keeps it full screen.
func TestWindowBoundsFullScreenWindow(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_BOUNDS_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_BOUNDS_TESTS not set")
	}
	prim := boundsPrimary(t)
	wa := prim.WorkArea
	r := image.Rect(wa.Min.X+40, wa.Min.Y+30, wa.Min.X+40+900, wa.Min.Y+30+620)
	ba := runBoundsApp(t, &mainWindowSettings{Bounds: winRectOf(r), FullScreen: true})
	ba.until(t, "full screen", func() (ok bool) { ba.on(t, func() { ok = ba.a.winMode == app.Fullscreen }); return })
	if got := ba.rect(); !wa.In(got) {
		t.Errorf("full screen at %v, want over the work area %v", got, wa)
	}
	ba.until(t, "ui.json full screen", func() bool {
		s := ba.saved()
		return s != nil && s.FullScreen && s.Bounds != nil && wa.In(s.Bounds.rect())
	})
	ba.quit(t)
}
