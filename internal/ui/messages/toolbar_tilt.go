package messages

import (
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The hover toolbar's icon transitions, from Slack's CSS (modern.vendor
// 414032e44d78d577.min.css):
//
//	.c-message-actions--hover-actions:not(.c-message-actions--a11y-reduce-animations) .c-message_actions__button>img,
//	… .c-message_actions__button>svg
//	  {transform-origin:50%; transition-duration:80ms; transition-timing-function:cubic-bezier(.36,.19,.29,1)}
//	… :hover>img, :focus>img, :hover>svg[data-qa=add-reaction], :focus>svg[data-qa=add-reaction]
//	  {transform:scale(1.2) rotate(4deg)}
//	… :hover>svg, :focus>svg {transform:scale(1.2)}
//
// So "Add reaction…" and the one-click reactions' emoji (an <img>, the
// Emoji component without inline) grow and tilt 4° clockwise, and the
// other buttons' icons only grow, over 80 ms, both ways. MessageActions
// adds c-message-actions--a11y-reduce-animations when the a11y_animations
// pref is off (boot-shared: a11yAnimationsPref = ty(e, "a11y_animations")),
// and then nothing moves at all: no tilt, no scale, no transition.
//
// The icon buttons are c-icon_buttons too, whose transition-property is
// color, background-color, border-color (80 ms, the same curve): the hover
// background and the icon's colour (the svg inherits it, and its own
// transition is "all") fade in and out whatever the pref. The one-click
// reactions are plain buttons: their background comes and goes at once.
//
// Each button's tooltip (MessageActionButton's Tooltip: position "top",
// delay 60) shows 60 ms after the pointer comes (BaseTooltip defers it
// unless the keyboard is in use), in a popover with shouldFade (BaseTooltip's
// default): .c-popover--fade {animation: 1s forwards c-popover-fade}
// (opacity 0 to 1, the default "ease"), but the Popover's inline style sets
// animationDuration "80ms" when it fades (getStyle, client-boot L1PI), so
// the fade takes 80 ms and the tip is all there 140 ms after the pointer
// came. It goes at once: BaseTooltip unmounts the Popover, so react-modal's
// 80 ms closeTimeoutMS and --before-close fade never run.

const (
	tiltDur   = 80 * time.Millisecond
	tiltScale = 1.2
	tiltDeg   = 4.0
	// tiltFrame is how far apart the deadlines of a running transition are.
	tiltFrame = time.Second / 60

	tipDelay = 60 * time.Millisecond // MessageActionButton's Tooltip delay
	tipFade  = 80 * time.Millisecond // c-popover-fade at the Popover's inline 80ms
)

// tiltEase is cubic-bezier(.36,.19,.29,1).
func tiltEase(x float64) float64 { return cubicBez(.36, .19, .29, 1, x) }

// cssTransition is a CSS transition of one value, as css-transitions-1
// starts them: a change while a transition runs starts a new one from the
// current value, and going back to where the running one started
// shortens the new one by the reversing shortening factor.
type cssTransition struct {
	set      bool
	from, to float64
	adjFrom  float64 // the reversing-adjusted start value
	factor   float64 // the reversing shortening factor
	start    time.Time
	dur      time.Duration
}

// out is the timing function's output at now (1 when done).
func (t *cssTransition) out(now time.Time) float64 {
	if t.dur <= 0 || !now.Before(t.start.Add(t.dur)) {
		return 1
	}
	return tiltEase(float64(now.Sub(t.start)) / float64(t.dur))
}

// at is the value at now.
func (t *cssTransition) at(now time.Time) float64 {
	if !t.set {
		return 0
	}
	return t.from + (t.to-t.from)*t.out(now)
}

// running reports whether the value still moves after now.
func (t *cssTransition) running(now time.Time) bool {
	return t.set && t.dur > 0 && now.Before(t.start.Add(t.dur))
}

// end is when the running transition ends.
func (t *cssTransition) end() time.Time { return t.start.Add(t.dur) }

// target changes the value's computed end to v at now, with dur the
// property's transition-duration (0: it jumps). The first call sets the
// value without a transition: the style an element mounts with.
func (t *cssTransition) target(now time.Time, v float64, dur time.Duration) {
	if !t.set || dur <= 0 || now.IsZero() {
		*t = cssTransition{set: true, from: v, to: v, adjFrom: v, factor: 1}
		return
	}
	if v == t.to {
		return
	}
	cur := t.at(now)
	if t.running(now) && v == t.adjFrom {
		f := math.Abs(t.out(now)*t.factor + 1 - t.factor)
		*t = cssTransition{set: true, from: cur, to: v, adjFrom: t.to, factor: f, start: now, dur: time.Duration(float64(dur) * f)}
		return
	}
	*t = cssTransition{set: true, from: cur, to: v, adjFrom: cur, factor: 1, start: now, dur: dur}
}

// iconAnim is one toolbar button's transitions: its icon's transform
// (0 at rest, 1 grown and tilted) and its hover paint (background and
// icon colour).
type iconAnim struct{ move, paint cssTransition }

// toolbarTilt is the shown toolbar's transitions. A toolbar is mounted
// afresh for each message it shows on (MessageActions), so a new row, or
// a frame without the toolbar, starts them over at rest.
type toolbarTilt struct {
	row   *row
	tb    [tbCount]iconAnim
	quick [3]iconAnim
	next  time.Time // the deadline the last frame asked for (zero: none)

	tipKey   string    // the button whose tooltip is pending or shown ("": none)
	tipSince time.Time // when the pointer came to it
}

// mount starts the transitions over when the toolbar now shows on r and
// didn't on the last frame's row.
func (tt *toolbarTilt) mount(r *row) {
	if tt.row != r {
		*tt = toolbarTilt{row: r}
	}
	tt.next = time.Time{}
}

// step moves a's transitions to the button's state at gtx.Now (active:
// :hover or :focus, the transform's; hover: the paint's) and returns
// where they are. paintDur is the background's transition (0 for the
// one-click reactions). A transition still running asks for the next
// frame with a deadline.
func (tt *toolbarTilt) step(gtx layout.Context, a *iconAnim, active, hover bool, paintDur time.Duration) (move, paint float64) {
	now := gtx.Now
	mv, dur := 0.0, tiltDur
	if uipref.Get().NoAnimations { // c-message-actions--a11y-reduce-animations
		dur = 0
	} else if active {
		mv = 1
	}
	pt := 0.0
	if hover {
		pt = 1
	}
	a.move.target(now, mv, dur)
	a.paint.target(now, pt, paintDur)
	for _, t := range []*cssTransition{&a.move, &a.paint} {
		if !t.running(now) {
			continue
		}
		at := now.Add(tiltFrame)
		if e := t.end(); e.Before(at) {
			at = e
		}
		if tt.next.IsZero() || at.Before(tt.next) {
			tt.next = at
		}
	}
	return a.move.at(now), a.paint.at(now)
}

// ask asks for the frame the running transitions need.
func (tt *toolbarTilt) ask(gtx layout.Context) {
	if !tt.next.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: tt.next})
	}
}

// tipAlpha is the tooltip's opacity for the button key ("" none) at
// gtx.Now: 0 for the first 60 ms, then c-popover-fade over 80 ms. It asks
// for the frames it needs with deadlines.
func (tt *toolbarTilt) tipAlpha(gtx layout.Context, key string) float32 {
	if key != tt.tipKey {
		tt.tipKey, tt.tipSince = key, gtx.Now
	}
	if key == "" {
		return 0
	}
	now := gtx.Now
	if now.IsZero() {
		return 1
	}
	show := tt.tipSince.Add(tipDelay)
	if now.Before(show) {
		gtx.Execute(op.InvalidateCmd{At: show})
		return 0
	}
	x := float64(now.Sub(show)) / float64(tipFade)
	if x >= 1 {
		return 1
	}
	at := now.Add(tiltFrame)
	if e := show.Add(tipFade); e.Before(at) {
		at = e
	}
	gtx.Execute(op.InvalidateCmd{At: at})
	return float32(cubicBez(.25, .1, .25, 1, x)) // ease
}

// tiltAffine is the icon's transform at move (0 to 1) about its centre
// ctr: scale(1.2), and rotate(4deg) when tilt.
func tiltAffine(ctr f32.Point, move float64, tilt bool) f32.Affine2D {
	sc := float32(1 + (tiltScale-1)*move)
	a := f32.Affine2D{}.Scale(ctr, f32.Pt(sc, sc))
	if tilt {
		// CSS's positive angle turns clockwise on screen, as Gio's does
		// with y pointing down
		a = a.Rotate(ctr, float32(tiltDeg*move*math.Pi/180))
	}
	return a
}

// mixColor is a's colour transitioning to b at p, interpolated as CSS does
// (premultiplied sRGB).
func mixColor(a, b color.NRGBA, p float64) color.NRGBA {
	if p <= 0 {
		return a
	}
	if p >= 1 {
		return b
	}
	aa, ba := float64(a.A)/255, float64(b.A)/255
	al := aa + (ba-aa)*p
	if al <= 0 {
		return color.NRGBA{}
	}
	ch := func(x, y uint8) uint8 {
		v := (float64(x)*aa + (float64(y)*ba-float64(x)*aa)*p) / al
		return uint8(math.Round(math.Max(0, math.Min(255, v))))
	}
	return color.NRGBA{R: ch(a.R, b.R), G: ch(a.G, b.G), B: ch(a.B, b.B), A: uint8(math.Round(al * 255))}
}
