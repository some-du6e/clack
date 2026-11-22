//go:build darwin && cgo

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/app/cocoatest"
	"gioui.org/io/event"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/idleprobe"
)

// TestMacIdleCPU is TestIdleFrames on Clack's real macOS window
// (CLACK_MACOS_WINDOW_TESTS=1, ci's macos job; TestMain runs app.Main):
// App.Run's own window with Metal and the AppKit glue (the menu bar, the
// nav monitor, the Dock, the appearance and motion watchers), signed in
// to internal/fakeslack with realtime on, left alone on each surface and
// then hidden by its red button. Each period logs the frames drawn and
// who asked for each (idleprobe.Recorder), every Invalidate and
// InvalidateCmd by caller, and the process's CPU time; with
// CLACK_IDLE_CPUPROFILE=<prefix> each period also writes a CPU profile
// (<prefix>-<n>-<surface>.pprof; ci prints their tops).
//
// A surface may draw what TestIdleFrames allows (a focused caret's
// blinks and idleOtherFrames more), also when every frame takes longer
// than a vsync, and a hidden window draws nothing.
func TestMacIdleCPU(t *testing.T) {
	if !macWindowTests {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	sz := fakeslack.Small
	sz.Unread = 0
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	rec := idleprobe.NewRecorder()
	defer rec.Close()
	views := make(chan uintptr, 1)
	var a *App
	// slow: every frame takes longer than a vsync, as a busy or swapping
	// Mac's do (Karim's 8 GB one, 2026-11-14)
	var slow atomic.Bool
	images := imgcache.New(fs.Fetch, "", nil)
	a = New(Options{ConfigDir: home, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		switch e := e.(type) {
		case app.AppKitViewEvent:
			if e.View != 0 {
				select {
				case views <- e.View:
				default:
				}
			}
		case app.FrameEvent:
			rec.Frame()
			if slow.Load() {
				time.Sleep(30 * time.Millisecond)
			}
		}
	}}, images)
	invalidateHook = func(b *App) {
		if b == a {
			rec.Invalidated()
		}
	}
	defer func() { invalidateHook = nil }()
	on := true
	a.prefs.Realtime = &on // the first-run banner's "Turn on"
	a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	done := make(chan error, 1)
	go func() { done <- a.Run() }()

	var view uintptr
	select {
	case view = <-views:
	case <-time.After(60 * time.Second):
		t.Fatal("no AppKitViewEvent")
	}
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
	until := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(d)
		for !ok() {
			select {
			case err := <-done:
				t.Fatalf("%s: Run returned (%v)", what, err)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	state := func() cocoatest.Window {
		var s cocoatest.Window
		onMain(func() { s = cocoatest.State(view) })
		return s
	}
	var booted time.Time
	until("core ready", 60*time.Second, func() bool {
		onUI(func() {
			if a.data != nil {
				booted = a.data.booted
			}
		})
		return !booted.IsZero()
	})
	onMain(func() { cocoatest.OrderFront(view) })
	for dl := time.Now().Add(10 * time.Second); !state().Key && time.Now().Before(dl); {
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("window: %+v", state())

	ch := fakeslack.ThreadPublicID
	onUI(func() { a.Navigate("c/" + ch) })
	until("history", 30*time.Second, func() bool {
		n := 0
		onUI(func() {
			if tl := a.data.core.S.Timeline(ch); tl != nil {
				n = len(tl.Msgs)
			}
		})
		return n > 0
	})
	focus := func(ts string) {
		t.Helper()
		time.Sleep(300 * time.Millisecond) // a frame lays the conversation out
		onUI(func() {
			if c := a.convoFor(ch, ts); c != nil && c.comp != nil {
				c.comp.Focus()
			} else {
				t.Errorf("no composer for %s/%s", ch, ts)
			}
		})
	}
	focus("")
	// core's deferred boot loads (a.later 5 s after boot) are start-up
	if d := 10*time.Second - time.Since(booted); d > 0 {
		time.Sleep(d)
	}

	prefix := os.Getenv("CLACK_IDLE_CPUPROFILE")
	n := 0
	measure := func(name string, caret bool) idleprobe.Report {
		t.Helper()
		time.Sleep(3 * time.Second) // loads, opening animations
		n++
		var prof *os.File
		if prefix != "" {
			slug := strings.NewReplacer(" ", "-", ",", "", "/", "-").Replace(name)
			if f, err := os.Create(fmt.Sprintf("%s-%02d-%s.pprof", prefix, n, slug)); err == nil {
				if err := pprof.StartCPUProfile(f); err == nil {
					prof = f
				} else {
					f.Close()
				}
			}
		}
		rec.Start(name)
		time.Sleep(idleFor)
		rep := rec.Stop()
		if prof != nil {
			pprof.StopCPUProfile()
			prof.Close()
		}
		rep.Log(t)
		blinks := rep.CountMatching("Editor).Layout")
		other := rep.Count() - blinks
		maxBlinks := 0
		if blink := widget.CaretBlinkInterval(); caret && blink > 0 {
			maxBlinks = int(idleFor/blink) + 1
		}
		// on a failure, every frame of the pass: when it came and what asked
		// for it (one failure, "channel again", 11-21, said only how many)
		if blinks > maxBlinks {
			t.Errorf("%s: %d caret frames in %v idle, want at most %d (the caret blinks every %v); the frames:\n%s",
				name, blinks, idleFor, maxBlinks, widget.CaretBlinkInterval(), strings.Join(frameTimeline(rep.Result, 60), "\n"))
		}
		if other > idleOtherFrames {
			t.Errorf("%s: %d frames in %v idle besides the caret's %d, want at most %d:\n%s\nthe frames:\n%s", name, other, idleFor, blinks, idleOtherFrames,
				strings.Join(firstCauses(rep.Result, 8), "\n"), strings.Join(frameTimeline(rep.Result, 60), "\n"))
		}
		return rep
	}

	var reps []idleprobe.Report
	reps = append(reps, measure("channel", true))
	// a frame slower than a vsync: the display link's tick during it
	// asked AppKit for one more frame after it, nobody's (Gio patch 28)
	slow.Store(true)
	reps = append(reps, measure("channel, frames slower than a vsync", true))
	slow.Store(false)
	onUI(func() { a.OpenThread(ch, fakeslack.ThreadRootTS) })
	focus(fakeslack.ThreadRootTS)
	reps = append(reps, measure("thread", true))
	onUI(func() { a.CloseSecondary() })
	for _, page := range []string{"activity", "threads", "later", "dms", "files", "huddles", "unreads"} {
		onUI(func() { a.Navigate(page) })
		reps = append(reps, measure(page, false))
	}
	onUI(func() { a.Navigate("c/" + ch) })
	focus("")
	reps = append(reps, measure("channel again", true))

	// the red button hides the window: no frames, and no wake-ups either
	// while nothing changes
	onMain(func() { cocoatest.Press(view, cocoatest.Close) })
	until("the window hidden by the close button", 30*time.Second, func() bool { return !state().Visible })
	hidden := measure("hidden (red button)", false)
	reps = append(reps, hidden)
	if hidden.Count() != 0 {
		t.Errorf("the hidden window drew %d frames", hidden.Count())
	}

	var sb strings.Builder
	for _, r := range reps {
		sb.WriteString(r.Summary() + "\n")
	}
	t.Logf("summary:\n%s", sb.String())

	onMain(func() { cocoatest.KeyEquivalent("q", cocoatest.Cmd) })
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Error("⌘Q didn't end Run")
	}
}
