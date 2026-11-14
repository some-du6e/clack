//go:build darwin && cgo

package ui

import (
	"math"
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

// TestMacWindowCloseHides is the macOS main window on a real desktop
// (CLACK_MACOS_WINDOW_TESTS=1, ci's macos job; TestMain runs app.Main):
// App.Run's own window, signed in to internal/fakeslack, with no tray
// icon. The window opens as Slack's does on macOS: no title bar, the
// content under it, the traffic lights at {12, 12}. The red button hides
// it, and Clack keeps running (Run doesn't return) with hasRunFromTray
// set; the hidden window draws no frames while posted work still runs;
// the Dock icon's reopen shows it again; ⌘W (File > Close) hides it; the
// app's activation with no window focused (⌘Tab) shows it; ⌘Q while it is
// hidden quits: Run returns.
//
// What it proves: Gio patch 24's hidden title bar and hide on a real
// NSWindow, runFromTray's macOS path, window_darwin.m's reopen and
// activation handlers, the menu bar's Close and Quit. What it doesn't:
// the Dock itself (the test calls the app delegate's reopen as the Dock's
// click does) or the window server's own drags.
func TestMacWindowCloseHides(t *testing.T) {
	if !macWindowTests {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	views := make(chan uintptr, 1)
	var frames atomic.Int64
	images := imgcache.New(fs.Fetch, "", nil)
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		switch e := e.(type) {
		case app.AppKitViewEvent:
			if e.View != 0 {
				select {
				case views <- e.View:
				default:
				}
			}
		case app.FrameEvent:
			frames.Add(1)
		}
	}}, images)
	a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	done := make(chan error, 1)
	go func() { done <- a.Run() }()

	var view uintptr
	select {
	case view = <-views:
	case <-time.After(60 * time.Second):
		t.Fatal("no AppKitViewEvent")
	}
	until := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
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
	state := func() cocoatest.Window {
		var s cocoatest.Window
		onMain(func() { s = cocoatest.State(view) })
		return s
	}
	visible := func() bool { return state().Visible }
	onUI := func(f func()) {
		t.Helper()
		ran := make(chan struct{})
		a.Post(func() { f(); close(ran) })
		select {
		case <-ran:
		case <-time.After(15 * time.Second):
			t.Fatal("work posted to the UI goroutine didn't run")
		}
	}
	near := func(x, y float64) bool { return math.Abs(x-y) < 0.5 }
	lights := func(when string) {
		t.Helper()
		s := state()
		if b := s.Buttons[cocoatest.Close]; !near(b.X, macTrafficLightX) || !near(b.Y, macTrafficLightY) || s.ButtonsHidden != [3]bool{} {
			t.Errorf("%s: the close button at %v (buttons hidden %v), want Slack's {%d, %d}", when, b, s.ButtonsHidden, macTrafficLightX, macTrafficLightY)
		}
	}
	until("the first frame", func() bool { return frames.Load() > 0 })
	time.Sleep(2 * time.Second) // boot settles (the reveal, the first history)

	// 0. Slack's macOS window: no title bar, the traffic lights at {12, 12}
	s := state()
	t.Logf("window: %+v", s)
	if !s.FullSizeContent || !s.TitlebarTransparent || !s.TitleHidden {
		t.Errorf("full-size content %v, transparent title bar %v, title hidden %v: want all", s.FullSizeContent, s.TitlebarTransparent, s.TitleHidden)
	}
	if s.Content != (cocoatest.Rect{W: s.Frame.W, H: s.Frame.H}) {
		t.Errorf("content view %v, want the whole %vx%v window", s.Content, s.Frame.W, s.Frame.H)
	}
	lights("opened")
	onUI(func() {
		if !a.macNav {
			t.Error("the top nav isn't the window's drag area")
		}
	})

	// 1. the red button hides the window and Clack keeps running
	onMain(func() { cocoatest.Press(view, cocoatest.Close) })
	until("the window hidden by the close button", func() bool { return !visible() })
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("the close quit Clack (Run returned %v)", err)
	default:
	}
	onUI(func() {
		if !a.prefs.HasRunFromTray {
			t.Error("hasRunFromTray not set by the first close")
		}
	})

	// 2. hidden: no frames, but posted work runs
	time.Sleep(time.Second)
	f0 := frames.Load()
	onUI(func() {})
	a.Invalidate()
	time.Sleep(time.Second)
	if n := frames.Load() - f0; n != 0 {
		t.Errorf("the hidden window drew %d frames", n)
	}

	// 3. the Dock icon (applicationShouldHandleReopen:hasVisibleWindows: NO)
	var ret, handled bool
	onMain(func() { ret, handled = cocoatest.Reopen(false) })
	if !handled {
		t.Fatal("the app delegate has no reopen handler")
	}
	if ret {
		t.Error("reopen with no visible windows returned YES, want Electron's hasVisibleWindows (NO)")
	}
	until("the window shown by the Dock icon", visible)
	until("a frame after the reopen", func() bool { return frames.Load() > f0 })
	time.Sleep(300 * time.Millisecond)
	lights("after the reopen")

	// 4. ⌘W: File > Close hides it (it needs a key window)
	onMain(func() { cocoatest.OrderFront(view) })
	deadline := time.Now().Add(10 * time.Second)
	for !state().Key && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if st := state(); !st.Key {
		t.Errorf("the window never became key (app active %v): ⌘W has nothing to close", st.AppActive)
	} else {
		var took bool
		onMain(func() { took = cocoatest.KeyEquivalent("w", cocoatest.Cmd) })
		if !took {
			t.Error("⌘W: no menu item took it")
		}
		until("the window hidden by ⌘W", func() bool { return !visible() })
		time.Sleep(500 * time.Millisecond)

		// 5. ⌘Tab back: the app's activation, no window focused
		onMain(cocoatest.PostDidBecomeActive)
		until("the window shown by the app's activation", visible)
	}

	// 6. ⌘Q while hidden quits
	onMain(func() { cocoatest.Press(view, cocoatest.Close) })
	until("the window hidden again", func() bool { return !visible() })
	var took bool
	onMain(func() { took = cocoatest.KeyEquivalent("q", cocoatest.Cmd) })
	if !took {
		t.Fatal("⌘Q: no menu item took it")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("⌘Q didn't end Run")
	}
}
