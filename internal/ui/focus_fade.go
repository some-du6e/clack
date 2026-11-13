package ui

// The focus mode toggle's view transition (boot-render eLK8,
// maybeToggleFocusModeWithViewTransition; web client build fa938a086569):
// the button's toggleFocusMode runs the store update inside
// document.startViewTransition. Turning focus mode on, the old page stays
// (::view-transition-old(root) { animation: none }) and the new one
// fades in over it (fade-in 0.35s ease-in-out from opacity 0); turning it
// off, both cross-fade for 0.35s ease-in-out. Either way each pixel goes
// from the old page to the new one by the timing's progress. Under
// prefers-reduced-motion Slack adds neither sheet, so Chromium's own
// cross-fade runs (0.25s, ease). While it runs the transition's source is
// "silent-sidebar-trigger", which keeps the button out of its idle look
// (useFocusModeViewTransitionSource), and its pseudo-elements take the
// pointer, so the page under it has no hover until it ends.
//
// What changes is the theme (the silent sidebar's palette): Clack moves
// the palette from the old colours to the new ones over the transition,
// which is the cross-fade for everything drawn in theme colours. Rows that
// lose their bold or their mention badge (shouldSuppressUnreadEmphasis)
// change at once.

import (
	"image/color"
	"reflect"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// focusFade is one running view transition.
type focusFade struct {
	active   bool
	from, to theme.Palette
	at       time.Time // the first frame drawn under it (zero: not yet)
	dur      time.Duration
	ease     func(float64) float64
	written  theme.Palette // the palette the last frame set
}

// startFocusFade starts the transition from the palette old to the one
// just built (a.th.C), and puts old back for the rest of this frame.
func (a *App) startFocusFade(old theme.Palette) {
	f := a.fm()
	if a.th.C == old { // nothing to fade (a toggle back before any frame)
		f.fade.active = false
		return
	}
	dur, ease := 350*time.Millisecond, cssEaseInOut
	if a.reduceMotion() {
		dur, ease = 250*time.Millisecond, cssEase // Chromium's default cross-fade
	}
	f.fade = focusFade{active: true, from: old, to: a.th.C, dur: dur, ease: ease, written: old}
	a.th.C = old
}

// focusFading reports whether the toggle's transition is running at now.
func (a *App) focusFading(now time.Time) bool {
	f, ok := focusModes[a]
	if !ok || !f.fade.active {
		return false
	}
	return f.fade.at.IsZero() || now.Before(f.fade.at.Add(f.fade.dur))
}

// focusFadeFrame sets this frame's palette while a transition runs (start
// of the frame, before anything draws).
func (a *App) focusFadeFrame(gtx layout.Context) {
	f, ok := focusModes[a]
	if !ok || !f.fade.active {
		return
	}
	fd := &f.fade
	if a.th.C != fd.written { // rebuilt since (the account theme landed, the OS scheme): the new target
		fd.to = a.th.C
	}
	if fd.at.IsZero() {
		fd.at = gtx.Now
	}
	x := float64(gtx.Now.Sub(fd.at)) / float64(fd.dur)
	if x >= 1 {
		fd.active = false
		a.th.C = fd.to
		a.th.Gen++ // caches keyed on the palette rebuild from the final colours
		a.Invalidate()
		return
	}
	a.th.C = lerpPalette(fd.from, fd.to, fd.ease(x))
	fd.written = a.th.C
	gtx.Execute(op.InvalidateCmd{}) // a frame per vsync while it runs
}

var nrgbaType = reflect.TypeOf(color.NRGBA{})

// lerpPalette mixes every colour of two palettes, k of the way from a to b
// (sRGB, as a cross-fade mixes pixels); anything else is b's.
func lerpPalette(a, b theme.Palette, k float64) theme.Palette {
	out := b
	va, vb, vo := reflect.ValueOf(&a).Elem(), reflect.ValueOf(&b).Elem(), reflect.ValueOf(&out).Elem()
	for i := 0; i < vo.NumField(); i++ {
		if vo.Field(i).Type() != nrgbaType || !vo.Field(i).CanSet() {
			continue
		}
		ca := va.Field(i).Interface().(color.NRGBA)
		cb := vb.Field(i).Interface().(color.NRGBA)
		vo.Field(i).Set(reflect.ValueOf(lerpNRGBA(ca, cb, k)))
	}
	return out
}

func lerpNRGBA(a, b color.NRGBA, k float64) color.NRGBA {
	l := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*k + .5) }
	return color.NRGBA{R: l(a.R, b.R), G: l(a.G, b.G), B: l(a.B, b.B), A: l(a.A, b.A)}
}
