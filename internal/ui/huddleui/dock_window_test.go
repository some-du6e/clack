package huddleui

import (
	"os"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Karim, α.112: "should be a bar down here if i close the window". Slack's
// client shows HuddleBottomBar whenever you're in a huddle (/+fI: ts =
// getActiveHuddleReference, `tm && <div class="p-client__huddle_bottom_bar">`;
// nMFj HuddleBottomBar: `!_ && (p || u || c)`), collapsed while
// isHuddleWindowOpen and full once the window is gone. Here the huddle
// window is a real one (Xvfb on linux, the runner's desktop on Windows:
// CLACK_HUDDLE_WINDOW_TESTS=1), closed the way you close it, and the bar
// has to take its full shape at once, with the main window asked to draw it.

// invalHost counts the main window's redraws.
type invalHost struct {
	*hcHost
	n atomic.Int64
}

func (h *invalHost) Invalidate() { h.n.Add(1) }

func until(t *testing.T, what string, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDockAfterHuddleWindowCloses(t *testing.T) { dockAfterHuddleWindowCloses(t, true) }

// The same with the main window drawing nothing while the huddle window
// has the call: minimized, or closed to the tray (the invite window's Join
// doesn't raise it). HuddleBottomBarContent's W became STARTED while the
// window was open in the call (an effect, not a paint: nMFj ~:270), so the
// bar the main window draws once it's back is the full one.
func TestDockAfterAnUnseenHuddleWindowCloses(t *testing.T) { dockAfterHuddleWindowCloses(t, false) }

func dockAfterHuddleWindowCloses(t *testing.T, seen bool) {
	if os.Getenv("CLACK_HUDDLE_WINDOW_TESTS") == "" {
		t.Skip("CLACK_HUDDLE_WINDOW_TESTS not set (opens a real huddle window)")
	}
	st := store.New("")
	st.PutUsers([]slack.User{
		{ID: huddle.FakeSelfUser, Name: "me", RealName: "Test Self"},
		{ID: huddle.FakeRemoteUser, Name: "remote", RealName: "Remote Example"},
	})
	st.PutConv(slack.Conversation{ID: hcChannel, Name: "huddle-test", IsChannel: true, IsMember: true, NumMembers: 2})
	app := core.New(slack.New(slack.Creds{}), st)
	if u, ok := st.User(huddle.FakeSelfUser); ok {
		app.S.Self = *u
	}
	h := &invalHost{hcHost: &hcHost{app: app, th: theme.New(false)}}
	c := New(h)
	fk := huddle.NewFake(nil, t.Errorf)
	t.Cleanup(fk.Close)
	c.Client, c.SelfID = fk.Caller(), huddle.FakeSelfUser
	c.Tweak = func(o *huddle.Options) { o.Device = huddle.NewToneDevice(440) }
	t.Cleanup(c.Close)

	c.Start(hcChannel)
	until(t, "the huddle, connected, with its window open", 20*time.Second, func() bool {
		s := c.Session()
		return s != nil && s.State().Phase == huddle.Connected && c.WindowOpen()
	})
	c.mu.Lock()
	w := c.win
	c.mu.Unlock()
	until(t, "the huddle window on screen", 10*time.Second, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.w != nil
	})
	time.Sleep(time.Second) // its first frames
	var ops op.Ops
	if seen { // the main window draws the bar meanwhile
		if got, want := c.DockHeight(shotCtx(&ops, 1000, 200)), (dockCollapsed+dockPad)*shotScale; got != want {
			t.Fatalf("with the window open the bar is %d px, want the collapsed %d", got, want)
		}
	}

	n0 := h.n.Load()
	closeHuddleWindowLikeYou(t, w)
	until(t, "the huddle window gone (isHuddleWindowOpen false)", 10*time.Second, func() bool { return !c.WindowOpen() })
	if c.Session() == nil {
		t.Fatal("closing the huddle window left the huddle")
	}
	until(t, "the main window asked to draw the bar", 5*time.Second, func() bool { return h.n.Load() > n0 })
	if got, want := c.DockHeight(shotCtx(&ops, 1000, 200)), (dockBar+dockPad)*shotScale; got != want {
		t.Fatalf("with the window closed the bar is %d px, want the full %d (main window drawing while the huddle window was open: %v)", got, want, seen)
	}
	drawn := hudStrings(hudSurface{w: 1000, h: dockBar + dockPad, draw: func(gtx C) { c.LayoutDock(gtx, h.th) }})
	for _, want := range []string{"Huddle in #huddle-test", "Leave"} {
		if !slices.Contains(drawn, want) {
			t.Fatalf("the bar after the window closed doesn't draw %q: %q", want, drawn)
		}
	}
	t.Logf("window closed: the bar is full (%d px), drew %q", c.DockHeight(shotCtx(&ops, 1000, 200)), drawn)
}
