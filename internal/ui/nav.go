package ui

import (
	"image"
	"image/color"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// History navigation besides the top bar's arrows, like Slack's desktop
// client: the keys (navBindings), the mouse's back/forward buttons and a
// two-finger swipe on a macOS trackpad (swipe.go).

// navBinding is one back/forward key.
type navBinding struct {
	name key.Name
	mods key.Modifiers
	dir  int // -1 back, +1 forward
}

// navBindings are Slack's history keys on goos: ⌘[ / ⌘] on macOS;
// Ctrl+[ / Ctrl+] and Alt+← / Alt+→ elsewhere. macOS has no Option+←/→
// binding: those move by word in the composer.
func navBindings(goos string) []navBinding {
	if goos == "darwin" {
		return []navBinding{{"[", key.ModCommand, -1}, {"]", key.ModCommand, 1}}
	}
	return []navBinding{
		{"[", key.ModCtrl, -1}, {"]", key.ModCtrl, 1},
		{key.NameLeftArrow, key.ModAlt, -1}, {key.NameRightArrow, key.ModAlt, 1},
	}
}

// navigate goes dir steps through history (-1 back, +1 forward).
func (a *App) navigate(dir int) {
	if dir < 0 {
		a.Back()
	} else if dir > 0 {
		a.Forward()
	}
}

// navButtons handles the mouse's back (4) and forward (5) buttons as the
// window-wide handler sees them. Gio reports them on Windows and Wayland;
// on macOS the NSEvent monitor does (nav_darwin.go); X11 drops them.
func (a *App) navButtons(pe pointer.Event) {
	pressed := pe.Buttons &^ a.ptrBtns
	a.ptrBtns = pe.Buttons
	if pe.Kind != pointer.Press {
		return
	}
	switch {
	case pressed.Contain(pointer.ButtonQuaternary):
		a.navigate(-1)
	case pressed.Contain(pointer.ButtonQuinary):
		a.navigate(1)
	}
}

// navGestures is the window-wide ancestor of every other input area (it is
// pushed first in the frame and popped last), so Gio hands it the
// horizontal scroll that the views under the pointer left over. It feeds
// the swipe recogniser and navigates when a swipe completes.
func (a *App) navGestures(gtx layout.Context) clip.Stack {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &a.navTag, Kinds: pointer.Scroll,
			ScrollX: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Scroll && pe.Scroll.X != 0 {
			a.swipe.leftover(pe.Time, pe.Scroll.X, gtx.Metric.PxPerDp)
		}
	}
	st := a.swipe.poll(time.Now())
	a.swipeNow = st
	if !st.wake.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: st.wake})
	}
	if st.nav != 0 {
		a.navigate(st.nav)
	}
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &a.navTag)
	return area
}

// layoutSwipe draws Chromium's history-swipe arrow: a 40dp disc sliding in
// from the primary panel's edge with the swipe's progress, filled once
// letting go would navigate.
func (a *App) layoutSwipe(gtx layout.Context, panel image.Rectangle) {
	st := a.swipeNow
	if st.progress <= 0.05 || (st.dir < 0 && !a.router.CanBack()) || (st.dir > 0 && !a.router.CanForward()) {
		return
	}
	p := min(st.progress, 1)
	d := gtx.Dp(40)
	travel := float32(gtx.Dp(56))
	y := panel.Min.Y + (panel.Dy()-d)/2
	x := panel.Min.X - d + int(p*travel)
	name := "back"
	if st.dir > 0 {
		name = "forward"
		x = panel.Max.X - int(p*travel)
	}
	c := &a.th.C
	bg, fg := c.BgRaised, c.FgMuted
	if st.progress >= 1 {
		bg, fg = c.Link, color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	r := image.Rect(x, y, x+d, y+d)
	theme.Shadow(gtx, r, d/2, gtx.Dp(6), theme.WithAlpha(c.PanelShadow, 0x40))
	theme.RRect(gtx, r, d/2, bg)
	defer op.Offset(image.Pt(x+(d-gtx.Dp(20))/2, y+(d-gtx.Dp(20))/2)).Push(gtx.Ops).Pop()
	icons.Get(name)(gtx, unit.Dp(20), fg)
	if st.progress > 0 && st.progress < 1 {
		gtx.Execute(op.InvalidateCmd{}) // follow the fingers
	}
}

// startNavGlue installs the platform's swipe/mouse-button source
// (nav_darwin.go); nil elsewhere.
var startNavGlue func(a *App)
