package messages

import (
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The hover toolbar's buttons against Slack's CSS and JS (toolbar_tilt.go):
// Add reaction…'s scale(1.2) rotate(4deg), the others' scale(1.2), 80 ms
// on cubic-bezier(.36,.19,.29,1) both ways, nothing under a11y_animations
// off; the background and icon colour fading over the same 80 ms; the
// tooltip 60 ms late and fading in over the Popover's 80 ms. Every frame
// they need comes from a deadline, never from Invalidate.

func tiltNear(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// A CSS transition, its interruption and css-transitions-1's reversing
// shortening factor.
func TestCSSTransitionLikeSlack(t *testing.T) {
	t0 := time.Unix(1700000000, 0)
	var tr cssTransition
	tr.target(t0, 0, tiltDur) // mounted at rest: no transition
	if tr.running(t0) || tr.at(t0) != 0 {
		t.Fatalf("mount: %+v", tr)
	}
	tr.target(t0, 1, tiltDur)
	if !tr.running(t0) || tr.at(t0) != 0 || tr.dur != tiltDur {
		t.Fatalf("hover: %+v", tr)
	}
	half := tiltEase(.5)
	if v := tr.at(t0.Add(40 * time.Millisecond)); !tiltNear(v, half) {
		t.Errorf("at 40 ms: %v, want ease(.5) = %v", v, half)
	}
	if half < .5 || half > .9 {
		t.Errorf("cubic-bezier(.36,.19,.29,1) at .5 is %v", half)
	}
	if tr.at(t0.Add(80*time.Millisecond)) != 1 || tr.running(t0.Add(80*time.Millisecond)) {
		t.Errorf("not done at 80 ms")
	}
	// leaving at 40 ms: back from ease(.5), shortened to 80 ms × ease(.5)
	tr = cssTransition{}
	tr.target(t0, 0, tiltDur)
	tr.target(t0, 1, tiltDur)
	t1 := t0.Add(40 * time.Millisecond)
	tr.target(t1, 0, tiltDur)
	if !tiltNear(tr.from, half) || tr.to != 0 || !tiltNear(tr.factor, half) || tr.dur != time.Duration(float64(tiltDur)*half) {
		t.Errorf("reversed at 40 ms: %+v, want from %v over %v", tr, half, time.Duration(float64(tiltDur)*half))
	}
	// a finished one goes back over the whole 80 ms
	tr = cssTransition{}
	tr.target(t0, 0, tiltDur)
	tr.target(t0, 1, tiltDur)
	tr.target(t0.Add(time.Second), 0, tiltDur)
	if tr.dur != tiltDur || tr.from != 1 {
		t.Errorf("after the end: %+v", tr)
	}
	// reduced motion: the value jumps
	tr.target(t0.Add(2*time.Second), 1, 0)
	if tr.running(t0.Add(2*time.Second)) || tr.at(t0.Add(2*time.Second)) != 1 {
		t.Errorf("no duration: %+v", tr)
	}
}

// The tooltip's timing, Slack's: MessageActionButton's Tooltip (delay: 60)
// shows 60 ms after the pointer comes, and its Popover fades in over
// 80 ms (getStyle's inline animationDuration "80ms" over .c-popover--fade's
// 1s, on the rule's "ease"), so it is all there at 140 ms (Karim: "the
// tool tip takes so long to show up").
func TestToolbarTipTimingLikeSlack(t *testing.T) {
	t0 := time.Unix(1700000000, 0)
	var tt toolbarTilt
	alpha := func(key string, ms int) float32 {
		return tt.tipAlpha(layout.Context{Now: t0.Add(time.Duration(ms) * time.Millisecond)}, key)
	}
	ease := func(x float64) float32 { return float32(cubicBez(.25, .1, .25, 1, x)) }
	for _, c := range []struct {
		ms   int
		want float32
	}{{0, 0}, {30, 0}, {59, 0}, {60, 0}, {80, ease(.25)}, {100, ease(.5)}, {120, ease(.75)}, {139, ease(79. / 80)}, {140, 1}, {1000, 1}} {
		if a := alpha("b1", c.ms); !tiltNear(float64(a), float64(c.want)) {
			t.Errorf("%d ms after the pointer came: opacity %v, want %v", c.ms, a, c.want)
		}
	}
	// its frames come from deadlines: the delay's end, the fade's frames up
	// to its end at 140 ms, then none
	wake := func(ms int) (time.Time, bool) {
		var r input.Router
		tt.tipAlpha(layout.Context{Now: t0.Add(time.Duration(ms) * time.Millisecond), Source: r.Source()}, "b1")
		return r.WakeupTime()
	}
	for _, c := range []struct{ ms, at int }{{10, 60}, {100, -1}, {130, 140}} {
		at, ok := wake(c.ms)
		want := t0.Add(time.Duration(c.at) * time.Millisecond)
		if c.at < 0 {
			want = t0.Add(100*time.Millisecond + tiltFrame)
		}
		if !ok || !at.Equal(want) {
			t.Errorf("at %d ms the tooltip asks for a frame at %v %v, want %v", c.ms, at.Sub(t0), ok, want.Sub(t0))
		}
	}
	if at, ok := wake(140); ok {
		t.Errorf("the shown tooltip asks for a frame at %v", at.Sub(t0))
	}
	// the next button's tooltip waits its own 60 ms, then fades in 80
	if a := alpha("b2", 1100); a != 0 {
		t.Errorf("the next button's tooltip at once: %v", a)
	}
	if a := alpha("b2", 1200); !tiltNear(float64(a), float64(ease(.5))) {
		t.Errorf("the next button's tooltip 100 ms in: %v, want %v", a, ease(.5))
	}
	if a := alpha("b2", 1240); a != 1 {
		t.Errorf("the next button's tooltip 140 ms in: %v, want 1", a)
	}
}

// The transform turns clockwise on screen (CSS's positive angle) about the
// icon's centre, and only Add reaction… and the emoji tilt.
func TestTiltAffine(t *testing.T) {
	ctr := f32.Pt(16, 16)
	p := tiltAffine(ctr, 1, true).Transform(ctr.Add(f32.Pt(10, 0)))
	th := 4 * math.Pi / 180
	want := ctr.Add(f32.Pt(float32(12*math.Cos(th)), float32(12*math.Sin(th))))
	if math.Abs(float64(p.X-want.X)) > 1e-3 || math.Abs(float64(p.Y-want.Y)) > 1e-3 {
		t.Errorf("rotate(4deg) scale(1.2): %v, want %v (clockwise: y grows)", p, want)
	}
	if p := tiltAffine(ctr, 1, false).Transform(ctr.Add(f32.Pt(10, 0))); p != ctr.Add(f32.Pt(12, 0)) {
		t.Errorf("scale(1.2) only: %v", p)
	}
	if c := tiltAffine(ctr, .5, true).Transform(ctr); c != ctr {
		t.Errorf("the centre moved: %v", c)
	}
}

// The colours of .c-message_actions__button and its :hover, light and dark
// (the theme's sk_ values), and the toolbar tooltip's box.
func TestToolbarButtonColorsLikeSlack(t *testing.T) {
	for _, tc := range []struct {
		dark           bool
		icon, hov, hbg color.NRGBA
	}{
		{false, rgb(0x616061), rgb(0x1d1c1d), rgb(0xf6f6f6)},
		{true, rgb(0xababad), rgb(0xd1d2d3), rgb(0x222529)},
	} {
		th := theme.New(tc.dark)
		c := colorsFor(th)
		if c.ToolbarIcon != tc.icon || c.ToolbarHoverIcon != tc.hov || c.ToolbarHoverBg != tc.hbg {
			t.Errorf("dark %v: icon %v → %v on %v, want %v → %v on %v", tc.dark, c.ToolbarIcon, c.ToolbarHoverIcon, c.ToolbarHoverBg, tc.icon, tc.hov, tc.hbg)
		}
		if m := mixColor(c.ToolbarIcon, c.ToolbarHoverIcon, .5); m.A != 255 || m.R == c.ToolbarIcon.R || m.R == c.ToolbarHoverIcon.R {
			t.Errorf("dark %v: half-way colour %v", tc.dark, m)
		}
		ops := new(op.Ops)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(900, 700))}
		tip := slackc.BodyScope().Child(tooltipClass, 0)
		win := image.Rect(0, 0, 900, 700)
		b := tooltipBox(gtx, th, c, tip, "Add reaction…", image.Rect(486, 108, 514, 136), win)
		if b.r.Max.Y != 100 || b.r.Dy() < 32 || b.r.Dy() > 33 || b.arrow != (theme.TooltipArrow{C: image.Pt(500, 100), Half: 6}) {
			t.Errorf("dark %v: tooltip %v arrow %+v, want a 32px box (8px 12px padding, 16.25 line) ending at the anchor, the 6px arrow at it", tc.dark, b.r, b.arrow)
		}
		if w := b.r.Dx(); w < 24+60 || w > 200 || b.text.X-b.r.Min.X != 12 || b.text.Y-b.r.Min.Y != 8 {
			t.Errorf("dark %v: tooltip width %d, text at %v", tc.dark, w, b.text)
		}
		if b := tooltipBox(gtx, th, c, tip, "Add reaction…", image.Rect(486, 18, 514, 46), win); b.r.Min.Y != 54 || b.arrow.C.Y != 54 {
			t.Errorf("dark %v: no room above: %v %+v, want the box 8px under the button", tc.dark, b.r, b.arrow)
		}
	}
}

// countHost counts the frames the list asks the host for.
type countHost struct {
	*ctxHost
	n *int
}

func (h countHost) Invalidate() { *h.n++ }

// frameAt lays a frame out at now.
func (e *ctxEnv) frameAt(now time.Time) {
	e.ops.Reset()
	e.now = now
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(900, 700)), Now: now, Source: e.r.Source()}
	e.v.Layout(gtx)
	e.r.Frame(&e.ops)
}

// moveTo moves the pointer to p and lays a frame out at now.
func (e *ctxEnv) moveTo(p f32.Point, now time.Time) {
	e.clock += 10 * time.Millisecond
	e.r.Queue(pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: e.clock})
	e.frameAt(now)
}

// toolbarButtonAt is the centre of toolbar button b (view coordinates).
func toolbarButtonAt(t *testing.T, s *listState, b int) f32.Point {
	t.Helper()
	if s.tbFor < 0 {
		t.Fatal("no toolbar")
	}
	for k, i := range s.tbBtns {
		if i == b {
			bs, pad := int(mt.toolbarBtn), int(mt.toolbarPad)
			x := s.tbRect.Min.X + pad + (len(s.quick.names)+k)*bs + bs/2
			y := s.tbRect.Min.Y + pad + bs/2
			return f32.Pt(float32(x), float32(y)).Add(f32.Pt(float32(s.origin.X), float32(s.origin.Y)))
		}
	}
	t.Fatalf("no button %d in %v", b, s.tbBtns)
	return f32.Point{}
}

// rowPoint is a point on the row at ts, away from the toolbar.
func rowPoint(t *testing.T, s *listState, ts string) f32.Point {
	t.Helper()
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].key == ts && s.heights[i] > 0 {
			return f32.Pt(300, float32(s.pos[i]+s.heights[i]-4+s.origin.Y))
		}
	}
	t.Fatalf("row %s isn't on screen", ts)
	return f32.Point{}
}

// TestToolbarHoverLikeSlack drives the toolbar through a real router.
func TestToolbarHoverLikeSlack(t *testing.T) {
	t.Cleanup(uipref.Reset)
	uipref.Reset()
	e := newCtxEnv(t)
	inval := 0
	e.v = NewChannelView(countHost{e.h, &inval}, tGen)
	e.v.Preview = true
	until(t, 5*time.Second, "the rows", e.frame, func() bool { return e.row(tsCtxLink) != nil })
	s := e.v.s
	now := e.now.Add(time.Second)
	e.moveTo(rowPoint(t, s, tsCtxLink), now)
	e.frameAt(now.Add(20 * time.Millisecond))
	if s.tbFor < 0 {
		t.Fatal("no toolbar over the hovered row")
	}
	inval = 0
	add := &s.tilt.tb[tbAddReact]
	if add.move.at(e.now) != 0 || add.move.running(e.now) {
		t.Fatalf("the toolbar mounted moving: %+v", add.move)
	}

	// onto Add reaction…: the transition starts in the frame the pointer
	// comes, and asks for the next frame with a deadline
	now = e.now.Add(time.Second)
	e.moveTo(toolbarButtonAt(t, s, tbAddReact), now)
	if add.move.to != 1 {
		e.frameAt(now.Add(5 * time.Millisecond))
	}
	st := add.move.start
	if add.move.to != 1 || add.paint.to != 1 || add.move.dur != tiltDur || add.paint.dur != tiltDur {
		t.Fatalf("hovered: move %+v paint %+v", add.move, add.paint)
	}
	if want := e.now.Add(tiltFrame); s.tilt.next != want {
		t.Errorf("the frame's deadline %v, want %v", s.tilt.next, want)
	}
	if at, ok := e.r.WakeupTime(); !ok || !at.After(e.now) || at.After(st.Add(tipDelay)) {
		t.Errorf("router wake-up %v %v, want a deadline after %v", at, ok, e.now)
	}
	if s.tilt.tipKey != "b0" || s.tilt.tipAlpha(layout.Context{Now: st.Add(30 * time.Millisecond)}, "b0") != 0 {
		t.Errorf("the tooltip shows before Slack's 60 ms: key %q", s.tilt.tipKey)
	}

	// mid-transition: ease(.5) of scale(1.2) rotate(4deg)
	e.frameAt(st.Add(40 * time.Millisecond))
	if v := add.move.at(e.now); !tiltNear(v, tiltEase(.5)) {
		t.Errorf("at 40 ms the tilt is %v of the way, want %v", v, tiltEase(.5))
	}
	if !add.paint.running(e.now) || s.tilt.next.IsZero() {
		t.Errorf("at 40 ms: paint %+v next %v", add.paint, s.tilt.next)
	}
	// done at 80 ms: no more frames from the tilt
	e.frameAt(st.Add(80 * time.Millisecond))
	if add.move.at(e.now) != 1 || add.paint.at(e.now) != 1 || add.move.running(e.now) || !s.tilt.next.IsZero() {
		t.Errorf("at 80 ms: move %v paint %v next %v", add.move.at(e.now), add.paint.at(e.now), s.tilt.next)
	}
	// the tooltip: 60 ms late, then the Popover's 80 ms ease-in, all there
	// 140 ms after the pointer came
	if a := s.tilt.tipAlpha(layout.Context{Now: st.Add(100 * time.Millisecond)}, "b0"); !tiltNear(float64(a), float64(float32(cubicBez(.25, .1, .25, 1, .5)))) {
		t.Errorf("tooltip 100 ms in (half its fade): %v", a)
	}
	e.r.WakeupTime() // only this frame's deadlines below
	e.frameAt(st.Add(120 * time.Millisecond))
	if at, ok := e.r.WakeupTime(); !ok || at.After(st.Add(140*time.Millisecond)) {
		t.Errorf("mid-fade the router wakes at %v %v, want by %v", at, ok, st.Add(140*time.Millisecond))
	}
	e.frameAt(st.Add(140 * time.Millisecond))
	if a := s.tilt.tipAlpha(layout.Context{Now: e.now}, "b0"); a != 1 {
		t.Errorf("tooltip 140 ms in: %v, want 1 (Slack's 60 ms delay and 80 ms fade)", a)
	}

	// Reply in thread: Add reaction… goes back over the whole 80 ms (its
	// transition had ended), the reply icon only grows, its own tooltip
	// waits its 60 ms
	if !containsInt(s.tbBtns, tbReply) {
		t.Fatalf("no Reply button: %v", s.tbBtns)
	}
	now = e.now.Add(100 * time.Millisecond)
	e.moveTo(toolbarButtonAt(t, s, tbReply), now)
	rep := &s.tilt.tb[tbReply]
	if add.move.to != 0 || add.move.dur != tiltDur || rep.move.to != 1 || s.tilt.tipKey != "b1" {
		t.Errorf("onto Reply: add %+v reply %+v tip %q", add.move, rep.move, s.tilt.tipKey)
	}

	// back to Add reaction…, then away after 40 ms: the shortened reverse
	now = e.now.Add(500 * time.Millisecond)
	e.moveTo(toolbarButtonAt(t, s, tbAddReact), now)
	r0 := add.move.start
	e.moveTo(toolbarButtonAt(t, s, tbReply), r0.Add(40*time.Millisecond))
	half := tiltEase(.5)
	if !tiltNear(add.move.from, half) || add.move.to != 0 || add.move.dur != time.Duration(float64(tiltDur)*half) {
		t.Errorf("left after 40 ms: %+v, want back from %v over %v", add.move, half, time.Duration(float64(tiltDur)*half))
	}

	// "Allow animations" off (c-message-actions--a11y-reduce-animations):
	// no transform at all; the colours still fade (c-icon_button)
	uipref.Set(store.Prefs{NoAnimations: true})
	now = e.now.Add(time.Second)
	e.moveTo(toolbarButtonAt(t, s, tbAddReact), now)
	e.frameAt(now.Add(40 * time.Millisecond))
	if add.move.at(e.now) != 0 || add.move.running(e.now) || add.paint.to != 1 {
		t.Errorf("reduced motion: move %+v paint %+v", add.move, add.paint)
	}
	uipref.Reset()

	// off the toolbar: the next one mounts at rest
	e.moveTo(f32.Pt(450, 690), e.now.Add(time.Second))
	e.frameAt(e.now.Add(20 * time.Millisecond))
	if s.tbFor < 0 && (s.tilt.row != nil || s.tilt.tipKey != "") {
		t.Errorf("the hidden toolbar kept its transitions: %+v", s.tilt)
	}
	if inval != 0 {
		t.Errorf("the toolbar asked the host for %d frames; its frames come from deadlines", inval)
	}
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
