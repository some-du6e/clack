package ui

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/idleprobe"
)

// TestIdleFrames lets the whole client sit for 10 s with no input on each
// surface and counts the frames it draws (CLACK_IDLE_FRAMES=1; ci's linux
// and windows-applog jobs run it). Slack's Chromium draws nothing when
// nothing changes: in 10 s that's a focused caret's blinks
// (FrameCaret's repeating timer, every LayoutTheme::CaretBlinkInterval,
// 500 ms by default) and at most a clock-minute tick (relative times,
// the sidebar's "x minutes ago"). So a surface may draw
//
//   - one frame per 500 ms caret toggle, only while the window is focused
//     (FrameSelection::FocusedOrActiveStateChanged disables the caret in
//     an inactive window), and
//   - idleOtherFrames more, for minute ticks;
//
// and every frame's requester is logged.
//
// The window lifecycle is App.Run's (idleprobe.Run): op.InvalidateCmd
// wake-ups at their time, App.Invalidate at once, router events pending
// after a frame at the next vsync. Realtime is on (the fake's socket
// idles after hello), so presence and typing paths are live.
func TestIdleFrames(t *testing.T) {
	if os.Getenv("CLACK_IDLE_FRAMES") == "" {
		t.Skip("CLACK_IDLE_FRAMES not set")
	}
	p := idleprobe.New()
	defer p.Close()
	// only the App being measured: an earlier one's goroutines may still
	// finish work after its shutdown
	var measured atomic.Pointer[App]
	invalidateHook = func(a *App) {
		if a == measured.Load() {
			p.Invalidated()
		}
	}
	defer func() { invalidateHook = nil }()

	for _, motion := range []struct {
		name     string
		override int
	}{{"motion", -1}, {"reduced motion", 1}} {
		t.Run(motion.name, func(t *testing.T) {
			// the main view, a thread, Activity, Preferences
			sz := fakeslack.Small
			sz.Unread = 0
			sz.ThreadHeader = true
			e := newIdleEnv(t, sz, motion.override)
			measured.Store(e.a)
			ch := fakeslack.ThreadPublicID
			e.openChannel(ch)
			e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
			e.idle(t, p, "channel", true)
			e.blur()
			e.idle(t, p, "channel, window in the background", false)
			e.unblur()
			e.a.OpenThread(ch, fakeslack.ThreadRootTS)
			e.frame()
			e.idle(t, p, "thread", true)
			e.a.CloseSecondary()
			e.a.Navigate("activity")
			e.frame()
			e.idle(t, p, "activity", false)
			e.openChannel(ch)
			e.frame()
			e.a.openPrefs()
			e.frame()
			e.idle(t, p, "preferences", false)
			for e.a.dismissTop() {
			}
			e.shutdown()

			// the sidebar with unread channels and mentions
			sz = fakeslack.Small
			sz.Unreads = true
			e = newIdleEnv(t, sz, motion.override)
			measured.Store(e.a)
			ch = e.fs.ChannelIDs()[0]
			e.openChannel(ch)
			e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
			e.idle(t, p, "sidebar with unreads", true)
		})
	}
}

// idleOtherFrames is what a surface may draw in idleFor besides caret
// blinks: a minute tick, with room for two more.
const (
	idleFor         = 10 * time.Second
	idleOtherFrames = 3
)

// newIdleEnv is startEnv with live updates on and motion set (-1 full
// motion, 1 reduced), on its own fake.
func newIdleEnv(t *testing.T, sz fakeslack.Size, motion int) *perfEnv {
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	e := &perfEnv{tb: t, fs: fs, home: home, size: image.Pt(1440, 900), now: time.Now(), start: time.Now()}
	images := imgcache.New(fs.Fetch, "", nil)
	e.a = New(Options{ConfigDir: home, Started: e.start}, images)
	on := true
	e.a.prefs.Realtime = &on // the first-run banner's "Turn on"
	e.a.motionOverride = motion
	e.a.focused = true // App.Run: the first ConfigEvent of a shown window
	cache := filepath.Join(home, "cache")
	svc := core.New(slack.New(fs.Creds()), store.New(cache))
	e.a.Attach(svc, cache)
	images.SetInvalidate(e.a.Invalidate) // what Run does once the window exists
	e.a.th.Images = images
	if installHooks != nil {
		installHooks(e.a)
	}
	evs, stop := svc.Subscribe()
	e.a.data.start(e.a)
	t.Cleanup(e.shutdown)
	e.waitReady()
	dl := time.After(20 * time.Second)
	for up := false; !up; {
		select {
		case ev := <-evs:
			up = ev.Type == "connection" && ev.Connected
		case <-dl:
			t.Fatal("no realtime connection")
		}
	}
	stop()
	return e
}

// blur is the window losing the focus as Gio reports it: a pointer
// Cancel and a FocusEvent{Focus: false} to the focused tag (Window's
// ConfigEvent), then App.Run's a.focused.
func (e *perfEnv) blur() {
	e.r.Queue(pointer.Event{Kind: pointer.Cancel}, key.FocusEvent{Focus: false})
	e.a.focused = false
	e.frame()
}

func (e *perfEnv) unblur() {
	e.r.Queue(key.FocusEvent{Focus: true})
	e.a.focused = true
	e.frame()
}

// idleFrame is one App.Run FrameEvent at now (layout and the router; no
// GPU).
func (e *perfEnv) idleFrame(now time.Time) {
	a := e.a
	a.drain()
	e.now = now
	gtx := e.context()
	before := a.router.Current()
	a.frame(gtx)
	if a.router.Current() != before {
		gtx = e.context()
		a.frame(gtx)
	}
	a.revealFrame()
	e.r.Frame(&e.ops)
}

// idle settles the surface (loads, opening animations), then counts 10 s
// of frames with no input and checks them against the budget. caret is
// whether a text field has the focus (Slack focuses the composer when a
// conversation opens).
func (e *perfEnv) idle(t *testing.T, p *idleprobe.Probe, name string, caret bool) {
	t.Helper()
	e.settle(2 * time.Second)
	res := p.Run(name, idleFor, &e.r, e.idleFrame)
	res.Log(t)
	blinks := res.CountMatching("Editor).Layout")
	other := res.Count() - blinks
	maxBlinks := 0
	if caret && e.a.focused {
		maxBlinks = int(idleFor/(500*time.Millisecond)) + 1
	}
	if blinks > maxBlinks {
		t.Errorf("%s: %d caret frames in %v idle, want at most %d (focused window: %v)", name, blinks, idleFor, maxBlinks, e.a.focused)
	}
	if other > idleOtherFrames {
		t.Errorf("%s: %d frames in %v idle besides the caret's %d, want at most %d:\n%s", name, other, idleFor, blinks, idleOtherFrames, strings.Join(firstCauses(res, 8), "\n"))
	}
}

// firstCauses lists the first n distinct non-caret causes.
func firstCauses(r idleprobe.Result, n int) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range r.Frames {
		for _, c := range f.Causes {
			if !strings.Contains(c, "Editor).Layout") && !seen[c] && len(out) < n {
				seen[c] = true
				out = append(out, "\t"+c)
			}
		}
	}
	return out
}
