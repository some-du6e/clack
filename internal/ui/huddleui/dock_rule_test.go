package huddleui

import (
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
)

// Slack's rule for the docked bar: HuddleBottomBar (render nMFj) draws
// whenever you're in a huddle (/+fI: `tm && <div
// className="p-client__huddle_bottom_bar">`), none for an external guest
// when external huddles are on (`!_`, SfHq U), and HuddleBottomBarContent
// is the 48px collapsed bar while U = S || B || H: the huddle window open,
// the huddle starting or ending (isHuddleTransitionState), or just started
// with the window never having had it.

func TestDockStatesLikeSlack(t *testing.T) {
	e := newShotEnv(t, false)
	e.c.NoWindow = true // no OS window: the window's state is set by hand
	fk := huddle.NewFake(nil, t.Errorf)
	t.Cleanup(fk.Close)
	e.c.Client = fk.Caller()
	e.c.Tweak = func(o *huddle.Options) { o.Device = huddle.NewToneDevice(440) }
	t.Cleanup(e.c.Close)
	var ops op.Ops
	dp := func() int { return e.c.DockHeight(shotCtx(&ops, 1000, 200)) / shotScale }
	const collapsed, full = dockCollapsed + dockPad, dockBar + dockPad

	e.c.Start(hcChannel)
	if got := dp(); got != collapsed {
		t.Errorf("STARTING: the bar is %d dp, Slack's collapsed %d (B: isHuddleTransitionState)", got, collapsed)
	}
	for dl := time.Now().Add(20 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if s := e.c.Session(); s != nil && s.State().Phase == huddle.Connected {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the huddle never connected")
		}
	}
	if got := dp(); got != collapsed {
		t.Errorf("just started, the window never had it: the bar is %d dp, Slack's collapsed %d (H)", got, collapsed)
	}
	e.c.mu.Lock()
	e.c.attachWindow(e.w) // OpenWindow's
	e.c.mu.Unlock()
	if got := dp(); got != collapsed {
		t.Errorf("the window open: the bar is %d dp, Slack's collapsed %d (S)", got, collapsed)
	}
	e.c.mu.Lock()
	e.c.win = nil
	e.c.mu.Unlock()
	if got := dp(); got != full {
		t.Errorf("the window closed after it had the call: the bar is %d dp, Slack's full %d", got, full)
	}
	e.c.Leave()
	if got := dp(); got != collapsed && got != 0 {
		t.Errorf("ENDING: the bar is %d dp, Slack's collapsed %d (B)", got, collapsed)
	}
}

func TestDockHiddenForAnExternalGuestLikeSlack(t *testing.T) {
	e := newShotEnv(t, false)
	e.join(nil, nil)
	app := e.h.app
	me, ok := app.S.User(huddle.FakeSelfUser)
	if !ok {
		t.Fatal("no self user")
	}
	g := *me
	g.IsRestricted, g.RestrictedUserType = true, "external_guest"
	app.S.PutUsers([]slack.User{g})
	var ops op.Ops
	app.SetExperimentGroup(extHuddles, "on")
	if got := e.c.DockHeight(shotCtx(&ops, 1000, 200)); got != 0 {
		t.Errorf("an external guest in external huddles has a %d px bar; Slack's HuddleBottomBar draws none (SfHq U)", got)
	}
	app.SetExperimentGroup(extHuddles, "off")
	if got := e.c.DockHeight(shotCtx(&ops, 1000, 200)); got == 0 {
		t.Error("with external huddles off an external guest lost the bar")
	}
}

// An external guest has no bar, so the main window draws none, and the
// huddle window must still follow the main window's theme: the main
// window's every frame asks DockHeight first, which notes it
// (window_theme.go).
func TestExternalGuestWindowFollowsYourTheme(t *testing.T) {
	e := newShotEnv(t, false)
	e.join(nil, nil)
	app := e.h.app
	me, ok := app.S.User(huddle.FakeSelfUser)
	if !ok {
		t.Fatal("no self user")
	}
	g := *me
	g.IsRestricted, g.RestrictedUserType = true, "external_guest"
	app.S.PutUsers([]slack.User{g})
	app.SetExperimentGroup(extHuddles, "on")
	main := e.h.th
	var ops op.Ops
	frame := func() int { return e.c.DockHeight(shotCtx(&ops, 1000, 200)) }
	if h := frame(); h != 0 {
		t.Fatalf("an external guest has a %d px bar", h)
	}
	main.SetDark(!main.Dark) // you switch the mode mid-huddle
	frame()
	if wt := e.c.mainTheme(); !wt.set || wt.dark != main.Dark || wt.gen != main.Gen {
		t.Errorf("the huddle window has your theme as dark=%v, gen %d (set %v); the main window's is dark=%v, gen %d",
			wt.dark, wt.gen, wt.set, main.Dark, main.Gen)
	}
}
