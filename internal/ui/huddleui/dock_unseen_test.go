package huddleui

import (
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
)

// HuddleBottomBarContent's W (render nMFj ~:270) is an effect on the
// huddle's state and isHuddleWindowOpen, not on the bar's drawing: it
// becomes STARTED once the window is open in the call, whether the main
// window draws or not. Clack's main window draws nothing while it is
// minimized or closed to the tray (Gio's runLoop draws only a visible,
// unminimized window), and the invite window's Join starts a huddle
// without raising it. So a huddle joined that way has its window take the
// call unseen, and once you close the window and bring Clack back, the
// bar is the full one, not H's collapsed bar.
func TestDockFullAfterAnUnseenWindowLikeSlack(t *testing.T) {
	e := newShotEnv(t, false)
	e.c.NoWindow = true // no OS window: the window's state by hand
	fk := huddle.NewFake(nil, t.Errorf)
	t.Cleanup(fk.Close)
	e.c.Client = fk.Caller()
	e.c.Tweak = func(o *huddle.Options) { o.Device = huddle.NewToneDevice(440) }
	t.Cleanup(e.c.Close)

	// Start opens the window before it joins (its goroutine's OpenWindow)
	e.c.mu.Lock()
	e.c.win = e.w
	e.c.mu.Unlock()
	e.c.Start(hcChannel)
	for dl := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if s := e.c.Session(); s != nil && s.State().Phase == huddle.Connected {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the huddle never connected")
		}
	}
	// the main window drew no frame meanwhile; you close the huddle window
	// (its DestroyEvent) and bring Clack back
	e.c.mu.Lock()
	e.c.win = nil
	e.c.mu.Unlock()
	var ops op.Ops
	if got, want := e.c.DockHeight(shotCtx(&ops, 1000, 200))/shotScale, dockBar+dockPad; got != want {
		t.Errorf("the window had the call while the main window drew nothing, then closed: the bar is %d dp, Slack's full %d (H ended when the window had the call)", got, want)
	}
}
