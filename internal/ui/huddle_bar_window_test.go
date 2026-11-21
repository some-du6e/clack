package ui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Karim, α.112 on Windows: "should be a bar down here if i close the
// window". Slack's client shows HuddleBottomBar whenever you're in a
// huddle (render /+fI: `tm && <div className="p-client__huddle_bottom_bar">`,
// tm = getActiveHuddleReference || a bottom-bar banner), collapsed while
// the huddle window is open and full once it's gone. App.Run's real main
// window and the real huddle window (CLACK_REAL_WINDOW_TESTS and
// CLACK_HUDDLE_WINDOW_TESTS), the huddle window closed the way you close
// it (closeHuddleWindowLikeYou: WM_CLOSE on Windows): the main window has
// to draw the bar's title and Leave.
func TestRealWindowHuddleBarAfterClose(t *testing.T) {
	if os.Getenv("CLACK_HUDDLE_WINDOW_TESTS") == "" {
		t.Skip("CLACK_HUDDLE_WINDOW_TESTS not set")
	}
	if on, why := realWindowGate(); !on {
		t.Skip(why)
	}
	if !canCloseHuddleWindowLikeYou {
		t.Skip("no OS close for the huddle window here")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	cache := filepath.Join(home, "cache")

	var mu sync.Mutex
	drawn := map[string]time.Time{} // what any window's labels drew, and when last
	theme.DrawHook = func(s string) {
		mu.Lock()
		drawn[s] = time.Now()
		mu.Unlock()
	}
	t.Cleanup(func() { theme.DrawHook = nil })
	since := func(s string, t0 time.Time) bool {
		mu.Lock()
		defer mu.Unlock()
		at, ok := drawn[s]
		return ok && at.After(t0)
	}
	// the full bar's subtitle (HuddleBottomBarContent's eS: who's
	// speaking or sharing, else the head count); the collapsed bar has none
	subtitleSince := func(t0 time.Time) bool {
		mu.Lock()
		defer mu.Unlock()
		for s, at := range drawn {
			if at.After(t0) && (strings.Contains(s, " is sp") || strings.Contains(s, "people here") || s == "You’re the only one here") {
				return true
			}
		}
		return false
	}
	frames := 0
	a := New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		realWindowEvent(e)
		if _, ok := e.(app.FrameEvent); ok {
			mu.Lock()
			frames++
			mu.Unlock()
		}
	}}, imgcache.New(fs.Fetch, "", nil))
	a.prefs.MainWindow = &mainWindowSettings{Size: &winSize{Width: 1000, Height: 700}, Bounds: &winRect{X: 60, Y: 60, Width: 1000, Height: 700}}
	a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	done := make(chan error, 1)
	go func() { done <- a.Run() }()
	t.Cleanup(func() {
		a.Quit()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Error("Quit didn't end Run")
		}
	})
	until := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		for dl := time.Now().Add(d); !ok(); time.Sleep(50 * time.Millisecond) {
			select {
			case err := <-done:
				t.Fatalf("%s: Run returned (%v)", what, err)
			default:
			}
			if time.Now().After(dl) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	onUI := func(f func()) {
		t.Helper()
		ran := make(chan struct{})
		a.Post(func() { f(); close(ran) })
		select {
		case <-ran:
		case <-time.After(30 * time.Second):
			t.Fatal("work posted to the UI goroutine didn't run")
		}
	}
	until("core ready", 90*time.Second, func() bool {
		booted := false
		onUI(func() { booted = a.data != nil && !a.data.booted.IsZero() })
		return booted
	})
	realWindowShow(t)
	ch := fs.ChannelIDs()[0]
	conv, ok := a.Core().Conv(ch)
	if !ok || conv.Name == "" {
		t.Fatalf("no channel %s", ch)
	}
	title := "Huddle in #" + conv.Name // the bar's (the huddle window's own title has no #)

	fk := huddle.NewFake(nil, t.Errorf)
	t.Cleanup(fk.Close)
	hud := a.huddles()
	hud.Client, hud.SelfID = fk.Caller(), huddle.FakeSelfUser
	hud.Tweak = func(o *huddle.Options) { o.Device = huddle.NewToneDevice(440) }
	onUI(func() { a.StartHuddle(ch) })
	t.Cleanup(hud.Close)
	until("the huddle, connected, with its window open", 30*time.Second, func() bool {
		s := hud.Session()
		return s != nil && s.State().Phase == huddle.Connected && hud.WindowOpen()
	})
	t0 := time.Now()
	until("the collapsed bar in the main window", 15*time.Second, func() bool { return since(title, t0) })

	closeHuddleWindowLikeYou(t)
	until("the huddle window gone", 10*time.Second, func() bool { return !hud.WindowOpen() })
	if hud.Session() == nil {
		t.Fatal("closing the huddle window left the huddle")
	}
	t1 := time.Now()
	until("the main window drawing the full bar (its title, its subtitle and Leave)", 10*time.Second, func() bool {
		return since(title, t1) && since("Leave", t1) && subtitleSince(t1)
	})
	mu.Lock()
	n := frames
	mu.Unlock()
	t.Logf("the bar is drawn after the huddle window closed (%d main window frames)", n)
}
