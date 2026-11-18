package ui

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The control strip's + (boot-render CreateMenuTrigger): a
// p-control_strip__circle_button p-control_strip__create_button whose
// classes add p-control_strip__circle_button--active while its Create menu
// is open (onPopoverOpen / onPopoverClose), in the tab rail's theme scope.
// Slack's stylesheet:
//
//   - .p-control_strip__circle_button: theme-surf-inv-pry with
//     theme-content-inv-sec; background, color and transform transition
//     over .125s cubic-bezier(.17,.67,.55,1.09).
//   - :hover: theme-content-inv-pry, the blended theme-surf-inv-pry-hover
//     layer over the surf-inv-pry, scale(1.1).
//   - :active and --active: theme-base-pry with theme-content-ter (only
//     color and background-color, so :hover's layer and scale stay while
//     hovered).
//   - --active .p-control_strip__create_button__icon: rotate(135deg),
//     transitioning over .32s cubic-bezier(.34,1.56,.64,1): the plus
//     turns into an x while the menu is open.

// createStrip is the + button's state: its open menu and transitions.
type createStrip struct {
	menu      Overlay
	bg, fg    colorTransition
	scale, rt cssTransition
}

var createStrips = map[*App]*createStrip{}

func (a *App) createStrip() *createStrip {
	s := createStrips[a]
	if s == nil {
		s = &createStrip{}
		createStrips[a] = s
	}
	return s
}

// createMenuOpen reports whether the + button's Create menu is showing
// (the button's --active).
func (a *App) createMenuOpen() bool {
	m := a.createStrip().menu
	return m != nil && !m.Dismissed()
}

// createIconEase is the icon's rotation timing, cubic-bezier(.34,1.56,.64,1).
func createIconEase(x float64) float64 { return cubicBezierY(.34, 1.56, .64, 1, x) }

// createButton draws the + at org (window coordinates), cs wide. The
// colours read the Clickable inside clickArea's content, after its
// update, so a press shows :active in the frame that delivers it (Gio
// asks for no other while it's held); the scale wraps the button, so it
// takes the hover the last frame left, as the strip's other buttons do.
func (a *App) createButton(gtx layout.Context, org image.Point, cs int) layout.Dimensions {
	c := &a.th.C
	st := a.createStrip()
	btn := a.clicks.get("rail.new")
	open := a.createMenuOpen()
	base := a.chromeBase(org.Add(image.Pt(cs/2, cs/2)))
	now, reduce := gtx.Now, a.reduceMotion()
	scaleT, rotT := 1.0, 0.0
	if btn.Hovered() {
		scaleT = 1.1
	}
	if open {
		rotT = 135
	}
	scale := st.scale.value(now, scaleT, 125*time.Millisecond, stripEase, reduce)
	rot := st.rt.value(now, rotT, 320*time.Millisecond, createIconEase, reduce)
	if st.scale.running(now) || st.rt.running(now) {
		gtx.Execute(op.InvalidateCmd{}) // the transitions' frames, then none
	}
	ctr := f32.Pt(float32(cs)/2, float32(cs)/2)
	ts := op.Affine(f32.Affine2D{}.Scale(ctr, f32.Pt(float32(scale), float32(scale)))).Push(gtx.Ops)
	defer ts.Pop()
	var bg, bgT color.NRGBA
	var on, hov bool
	return clickArea(gtx, btn, func(gtx layout.Context, sz image.Point, _ bool) {
		if !on && !hov && bg == bgT { // settled: the strip's own chrome composite
			a.chrome(gtx, org, image.Rectangle{Max: sz}, sz.X/2, c.ChromeActive)
			return
		}
		theme.RRect(gtx, image.Rectangle{Max: sz}, sz.X/2, bg)
	}, func(gtx layout.Context) layout.Dimensions {
		hov = btn.Hovered()
		on = open || btn.Pressed()
		var fgT color.NRGBA
		switch {
		case on: // :active, --active
			bgT, fgT = c.ChromeBasePry, c.ChromeContentTer
			if hov {
				bgT = c.ChromeBasePryHover
			}
		case hov:
			bgT, fgT = theme.Over(c.ChromeHoverTint, theme.Over(c.ChromeActive, base)), c.ChromeText
		default:
			bgT = theme.Over(c.ChromeActive, base)
			fgT = theme.Over(c.ChromeTextDim, bgT)
		}
		bg = st.bg.value(now, bgT, 125*time.Millisecond, stripEase, false)
		fg := st.fg.value(now, fgT, 125*time.Millisecond, stripEase, false)
		if st.bg.running(now) || st.fg.running(now) {
			gtx.Execute(op.InvalidateCmd{})
		}
		off := (cs - gtx.Dp(20)) / 2
		r := op.Affine(f32.Affine2D{}.Rotate(ctr, float32(rot*math.Pi/180))).Push(gtx.Ops)
		at(gtx, image.Pt(off, off), image.Pt(cs, cs), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("plus")(gtx, 20, fg)
		})
		r.Pop()
		return layout.Dimensions{Size: image.Pt(cs, cs)}
	})
}
