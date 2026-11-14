//go:build darwin && cgo

package ui

import (
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/app/cocoatest"
	"gioui.org/io/event"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Slack's mainWindowSettings on App.Run's own window on a real macOS
// desktop (CLACK_MACOS_WINDOW_TESTS=1, ci's macos job; TestMain runs
// app.Main), signed in to internal/fakeslack, each test one launch with
// ui.json seeded as a previous one left it (Gio patch 25 places the
// window, in points from the primary screen's top left).

type macBoundsApp struct {
	a      *App
	view   uintptr
	dir    string
	frames atomic.Int64
	done   chan struct{} // closed when Run returns, with err
	err    error
}

func runMacBoundsApp(t *testing.T, seed *mainWindowSettings) *macBoundsApp {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ma := &macBoundsApp{dir: t.TempDir(), done: make(chan struct{})}
	if seed != nil {
		b, _ := json.Marshal(map[string]any{"main_window_settings": seed})
		if err := os.WriteFile(filepath.Join(ma.dir, "ui.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cache := filepath.Join(ma.dir, "cache")
	views := make(chan uintptr, 1)
	images := imgcache.New(fs.Fetch, "", nil)
	ma.a = New(Options{ConfigDir: ma.dir, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		switch e := e.(type) {
		case app.AppKitViewEvent:
			if e.View != 0 {
				select {
				case views <- e.View:
				default:
				}
			}
		case app.FrameEvent:
			ma.frames.Add(1)
		}
	}}, images)
	ma.a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	go func() { ma.err = ma.a.Run(); close(ma.done) }()
	select {
	case ma.view = <-views:
	case <-time.After(60 * time.Second):
		t.Fatal("no AppKitViewEvent")
	}
	ma.until(t, "the first frame", func() bool { return ma.frames.Load() > 0 })
	time.Sleep(2 * time.Second) // boot settles (the reveal, the first history)
	return ma
}

func (ma *macBoundsApp) until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !ok() {
		select {
		case <-ma.done:
			t.Fatalf("%s: Run returned (%v)", what, ma.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s (window %+v, saved %+v)", what, ma.state(), ma.saved())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// onUI runs f on the UI goroutine and waits.
func (ma *macBoundsApp) onUI(t *testing.T, f func()) {
	t.Helper()
	ran := make(chan struct{})
	ma.a.Post(func() { f(); close(ran) })
	select {
	case <-ran:
	case <-ma.done:
		t.Fatalf("Run returned (%v)", ma.err)
	case <-time.After(20 * time.Second):
		t.Fatal("posted work didn't run")
	}
}

func (ma *macBoundsApp) state() cocoatest.Window {
	var s cocoatest.Window
	onMain(func() { s = cocoatest.State(ma.view) })
	return s
}

// frame is the window's frame in whole points.
func (ma *macBoundsApp) frame() image.Rectangle {
	f := ma.state().ScreenFrame
	x, y := int(f.X+.5), int(f.Y+.5)
	return image.Rect(x, y, x+int(f.W+.5), y+int(f.H+.5))
}

func (ma *macBoundsApp) saved() *mainWindowSettings {
	b, err := os.ReadFile(filepath.Join(ma.dir, "ui.json"))
	if err != nil {
		return nil
	}
	var p prefs
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	return p.MainWindow
}

// quit is Quit; lenient says a Run that doesn't return is only logged
// (a full-screen window the runner may never let go of).
func (ma *macBoundsApp) quit(t *testing.T, lenient bool) {
	t.Helper()
	ma.a.Quit()
	select {
	case <-ma.done:
		if ma.err != nil {
			t.Errorf("Run: %v", ma.err)
		}
	case <-time.After(30 * time.Second):
		if !lenient {
			t.Fatal("Quit didn't end Run")
		}
		t.Logf("Quit didn't end Run within 30 s (window %+v)", ma.state())
	}
}

func macBoundsPrimary(t *testing.T) app.Display {
	t.Helper()
	ds := app.Displays()
	t.Logf("displays: %+v", ds)
	if len(ds) == 0 {
		t.Fatal("no displays")
	}
	return primaryDisplay(ds)
}

// TestMacWindowBounds:
//  1. the window opens exactly at ui.json's saved bounds;
//  2. moved (setFrameTopLeftPoint, as a drag ends), ui.json has the new
//     bounds soon after;
//  3. zoomed (Window > Zoom), ui.json says maximized, with Slack's
//     default bounds on the screen;
//  4. Quit ends Run, and ui.json keeps the maximized state.
func TestMacWindowBounds(t *testing.T) {
	if !macWindowTests {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	prim := macBoundsPrimary(t)
	wa := prim.WorkArea
	r := image.Rect(wa.Min.X+40, wa.Min.Y+30, wa.Min.X+40+900, wa.Min.Y+30+600)
	ma := runMacBoundsApp(t, &mainWindowSettings{Bounds: winRectOf(r)})

	// 1.
	if got := ma.frame(); got != r {
		t.Errorf("opened at %v, want the saved %v", got, r)
	}

	// 2.
	moved := r.Add(image.Pt(25, 15))
	onMain(func() { cocoatest.SetTopLeft(ma.view, float64(moved.Min.X), float64(moved.Min.Y)) })
	ma.until(t, "ui.json with the moved bounds", func() bool {
		s := ma.saved()
		return s != nil && s.Bounds != nil && s.Bounds.rect() == moved && !s.Maximized && !s.FullScreen
	})

	// 3. (the zoomed frame is AppKit's standard frame, the screen's
	// visible frame: the work area as the save read it; the runner's Dock
	// comes and goes)
	onMain(func() { cocoatest.ZoomWindow(ma.view) })
	ma.until(t, "zoomed", func() bool { return ma.state().Zoomed })
	want := defaultWindowBounds(app.Display{Bounds: prim.Bounds, WorkArea: ma.frame(), Scale: 1, Primary: true})
	ma.until(t, "ui.json maximized", func() bool {
		s := ma.saved()
		return s != nil && s.Maximized && s.Bounds != nil && s.Bounds.rect() == want
	})

	// 4.
	ma.quit(t, false)
	if s := ma.saved(); s == nil || !s.Maximized || s.Bounds == nil || s.Bounds.rect() != want {
		t.Errorf("after Quit, ui.json has %+v, want maximized with %v", s, want)
	}
}

// TestMacWindowBoundsOffScreen: saved maximized off every screen, the
// window opens zoomed on the runner's screen, and Window > Zoom takes it
// to Slack's validateBoundsForDisplay fallback there (900x600, its right
// edge on the screen's: x 124 on the 1024-point screen; its y follows the
// Dock, which comes and goes on the runner), which ui.json then keeps.
func TestMacWindowBoundsOffScreen(t *testing.T) {
	if !macWindowTests {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	prim := macBoundsPrimary(t)
	seed := &mainWindowSettings{Bounds: &winRect{X: 5000, Y: 5000, Width: 900, Height: 600}, Maximized: true}
	ma := runMacBoundsApp(t, seed)
	var want image.Rectangle
	ma.onUI(t, func() { want = ma.a.winOpened })
	if !want.In(prim.Bounds) || want.Dx() != 900 || want.Dy() != 600 || want.Max.X != prim.WorkArea.Max.X {
		t.Fatalf("opened at %v: not Slack's fallback on the screen %+v", want, prim)
	}
	ma.until(t, "zoomed", func() bool { return ma.state().Zoomed })
	onMain(func() { cocoatest.ZoomWindow(ma.view) })
	ma.until(t, "unzoomed onto the screen", func() bool { return !ma.state().Zoomed && ma.frame() == want })
	ma.until(t, "ui.json with the bounds on the screen", func() bool {
		s := ma.saved()
		return s != nil && !s.Maximized && s.Bounds != nil && s.Bounds.rect() == want
	})
	ma.quit(t, false)
}

// TestMacWindowBoundsFullScreen: saved full screen, the window opens full
// screen (Slack's setFullScreen before the show) once it is on the
// screen, and ui.json keeps it full screen. The runner never takes a
// window out of full screen, so this runs alone and last.
func TestMacWindowBoundsFullScreen(t *testing.T) {
	if !macWindowTests {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	prim := macBoundsPrimary(t)
	wa := prim.WorkArea
	r := image.Rect(wa.Min.X+40, wa.Min.Y+30, wa.Min.X+40+900, wa.Min.Y+30+600)
	ma := runMacBoundsApp(t, &mainWindowSettings{Bounds: winRectOf(r), FullScreen: true})
	ma.until(t, "full screen", func() bool { return ma.state().FullScreen })
	ma.until(t, "ui.json full screen", func() bool {
		s := ma.saved()
		return s != nil && s.FullScreen && s.Bounds != nil
	})
	t.Logf("full screen at %v, saved %+v", ma.frame(), ma.saved().Bounds)
	ma.quit(t, true)
}
