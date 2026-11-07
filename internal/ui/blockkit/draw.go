package blockkit

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Measured from Slack's Block Kit renderer (app.slack.com Block Kit
// Builder, which renders with the client's own components, 2026-11):
//
// Messages (small): buttons 28px, 13px/700, radius 4, padding 0 8,
// outline 1px rgba(94,93,96,.45); elements 8px apart, rows 8px apart;
// the first 5 elements of an actions block show, then "+ N more"
// (padding 0 12). static/users/conversations/channels selects 190×28,
// radius 8, 1px #7c7a7f, 13px text at 8px, 20px chevron 8px from the
// right; datepicker 220×28 with the calendar icon at 8 and text at 28;
// timepicker 190×28 with a clock icon; overflow 30×28. Checkboxes 14px
// (radius 4) and radios 13px at y+3, labels 15px/22 at x+22, options 8px
// apart, a description adds a 22px line. checkboxes, radio_buttons and
// multi-selects stack under a section's text instead of sitting at its
// right. datetimepicker is two 208×36 fields 8px apart with a 13px "Time
// zone: …" caption (rgb 69,68,71) under the time.
//
// Modals (medium): 520 wide, header 68 (padding 16 24; app icon 36,
// radius 4; title 22/900 at 52px from the icon's left), body padding 24,
// controls 464 wide; input blocks are a 30px label row (15/700, a muted
// 400 "(optional)"), the 36px control (textarea 104), and a 13px/18
// hint (rgb 94,93,96) 4px below; blocks are 8px apart. Section
// accessory buttons in modals are at least 120 wide.

func dp(gtx C, v float32) int { return gtx.Dp(unit.Dp(v)) }

func at(gtx C, p image.Point, w layout.Widget) D {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	return w(gtx)
}

func fixed(gtx C, sz image.Point) C {
	gtx.Constraints = layout.Exact(sz)
	return gtx
}

func rrect(gtx C, r image.Rectangle, radius int, c color.NRGBA) { theme.RRect(gtx, r, radius, c) }

// outline draws a 1px (w) border inside r with fill bg.
func outline(gtx C, r image.Rectangle, radius, w int, border, bg color.NRGBA) {
	rrect(gtx, r, radius, border)
	rrect(gtx, r.Inset(w), max(radius-w, 0), bg)
}

func label(gtx C, th *theme.Theme, size float32, w font.Weight, c color.NRGBA, s string) D {
	gtx.Constraints.Min = image.Point{}
	return th.Label(gtx, unit.Sp(size), w, c, s)
}

func para(gtx C, th *theme.Theme, size, lh float32, w font.Weight, c color.NRGBA, s string) D {
	gtx.Constraints.Min = image.Point{}
	return th.Layout(gtx, theme.TextStyle{Size: unit.Sp(size), Weight: w, Color: c, LineHeight: unit.Sp(lh)}, s)
}

func measure(gtx C, th *theme.Theme, size float32, w font.Weight, s string) int {
	return th.Measure(gtx, unit.Sp(size), w, s)
}

func icon(gtx C, name string, size float32, c color.NRGBA) D {
	return icons.Get(name)(gtx, unit.Dp(size), c)
}

// vcenter draws w vertically centred in a row of height h.
func vcenter(gtx C, h int, w layout.Widget) D {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	call := m.Stop()
	defer op.Offset(image.Pt(0, (h-d.Size.Y)/2)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(d.Size.X, h)}
}

// colors is the palette blockkit draws with.
type colors struct {
	*popups.Colors
	Text, Text2, Hint, Placeholder color.NRGBA
	SelectBorder, CheckBorder      color.NRGBA
	ErrorText                      color.NRGBA
	Hover                          color.NRGBA
}

func colorsOf(th *theme.Theme) colors {
	cl := popups.ColorsOf(th)
	c := colors{Colors: cl, Text: cl.Fg, Text2: rgb(0x454447), Hint: rgb(0x5e5d60), Placeholder: rgb(0x616061),
		SelectBorder: rgb(0x7c7a7f), CheckBorder: rgba(0x5e5d60, .45), ErrorText: rgb(0xe01e5a), Hover: rgb(0xf8f8f8)}
	if th.Dark {
		c.Text2, c.Hint, c.Placeholder = rgb(0xababad), rgb(0xababad), rgb(0x9a9b9e)
		c.SelectBorder, c.CheckBorder = rgba(0xe8e8e8, .3), rgba(0xe8e8e8, .3)
		c.Hover = rgba(0xffffff, .04)
	}
	return c
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func rgba(v uint32, a float32) color.NRGBA {
	c := rgb(v)
	c.A = uint8(a*255 + .5)
	return c
}

// spinner draws Slack's loading ring (a 3/4 arc turning once a second)
// centred in a box of size d, and keeps frames coming.
func spinner(gtx C, d int, c color.NRGBA) {
	t := float64(gtx.Now.UnixMilli()%1000) / 1000
	r := float32(d)/2 - float32(dp(gtx, 1.5))
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	a0 := float32(t * 2 * math.Pi)
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
	p.ArcTo(ctr, ctr, 1.5*math.Pi)
	st := clip.Stroke{Path: p.End(), Width: float32(dp(gtx, 2))}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
}

// handCursor sets the pointer cursor over r.
func handCursor(gtx C, r image.Rectangle) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	pointer.CursorPointer.Add(gtx.Ops)
}

func unitSp(v float32) unit.Sp { return unit.Sp(v) }

// buttonColors are Block Kit button colors per style.
func buttonColors(cl colors, style string, hovered bool) (bg, fg, border color.NRGBA) {
	switch style {
	case "primary":
		bg, fg = cl.Primary, cl.PrimaryText
		if hovered {
			bg = theme.Mix(bg, color.NRGBA{A: 255}, 0.12)
		}
		return bg, fg, color.NRGBA{}
	case "danger":
		bg, fg = cl.DangerBg, cl.HighText
		if hovered {
			bg = theme.Mix(bg, color.NRGBA{A: 255}, 0.12)
		}
		return bg, fg, color.NRGBA{}
	}
	bg = cl.BtnBg
	if hovered {
		bg = cl.BtnHover
	}
	return bg, cl.Fg, cl.BtnBorder
}

// drawButton paints a Block Kit button of size sz (label centred; busy
// shows the spinner instead, keeping the width).
func drawButton(gtx C, th *theme.Theme, sz image.Point, style, text string, radius, fs float32, hovered, busy, disabled bool) {
	cl := colorsOf(th)
	bg, fg, border := buttonColors(cl, style, hovered && !busy)
	if disabled {
		bg, fg, border = cl.Disabled, cl.DisText, color.NRGBA{}
	}
	r := image.Rectangle{Max: sz}
	rad := dp(gtx, radius)
	if border.A > 0 {
		outline(gtx, r, rad, max(1, dp(gtx, 1)), border, bg)
	} else {
		rrect(gtx, r, rad, bg)
	}
	if busy {
		s := dp(gtx, 16)
		at(gtx, image.Pt((sz.X-s)/2, (sz.Y-s)/2), func(gtx C) D { spinner(gtx, s, fg); return D{} })
		return
	}
	tw := measure(gtx, th, fs, theme.Bold, text)
	pad := dp(gtx, 8)
	maxW := sz.X - 2*pad
	x := (sz.X - min(tw, maxW)) / 2
	at(gtx, image.Pt(x, 0), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxW, sz.Y)}
		return vcenter(gtx, sz.Y, func(gtx C) D { return label(gtx, th, fs, theme.Bold, fg, text) })
	})
}

// drawField paints a select-like field: border (focus ring when active),
// optional leading icon, text (placeholder color when ph), chevron.
func drawField(gtx C, th *theme.Theme, sz image.Point, lead, text string, ph, active, chevron bool, fs float32, errored bool) {
	cl := colorsOf(th)
	r := image.Rectangle{Max: sz}
	rad := dp(gtx, 8)
	bw := max(1, dp(gtx, 1))
	switch {
	case active:
		ring := dp(gtx, 4)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		outline(gtx, r, rad, bw, cl.Focus, cl.Bg)
	case errored:
		outline(gtx, r, rad, bw, cl.ErrorText, cl.Bg)
	default:
		outline(gtx, r, rad, bw, cl.SelectBorder, cl.Bg)
	}
	small := sz.Y < dp(gtx, 32)
	x := dp(gtx, 12)
	if small {
		x = dp(gtx, 8)
	}
	if lead != "" {
		is := float32(16)
		if !small {
			is = 18
		}
		at(gtx, image.Pt(x, (sz.Y-dp(gtx, is))/2), func(gtx C) D { return icon(gtx, lead, is, cl.Text2) })
		x += dp(gtx, is) + dp(gtx, 4)
		if small {
			x = dp(gtx, 28)
		} else {
			x = dp(gtx, 32)
		}
	}
	right := sz.X - dp(gtx, 8)
	if chevron {
		cs := dp(gtx, 20)
		right = sz.X - dp(gtx, 8) - cs
		if !small {
			right = sz.X - dp(gtx, 12) - cs
		}
		at(gtx, image.Pt(right, (sz.Y-cs)/2), func(gtx C) D { return icon(gtx, "chevron-down", 20, cl.Text) })
		right -= dp(gtx, 4)
	}
	col := cl.Text
	if ph {
		col = cl.Placeholder
	}
	at(gtx, image.Pt(x, 0), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(max(right-x, 0), sz.Y)}
		return vcenter(gtx, sz.Y, func(gtx C) D { return label(gtx, th, fs, theme.Regular, col, text) })
	})
}

// drawCheckbox paints Slack's 14px checkbox.
func drawCheckbox(gtx C, th *theme.Theme, on, hovered bool) {
	cl := colorsOf(th)
	s := dp(gtx, 14)
	r := image.Rect(0, 0, s, s)
	rad := dp(gtx, 4)
	if on {
		rrect(gtx, r, rad, cl.Highlight)
		at(gtx, image.Pt(0, 0), func(gtx C) D { return icon(gtx, "check", 14, cl.HighText) })
		return
	}
	border := cl.CheckBorder
	if hovered {
		border = cl.Text2
	}
	outline(gtx, r, rad, max(1, dp(gtx, 1)), border, cl.Bg)
}

// drawRadio paints Slack's 13px radio button.
func drawRadio(gtx C, th *theme.Theme, on, hovered bool) {
	cl := colorsOf(th)
	s := dp(gtx, 13)
	ctr := image.Pt(s/2, s/2)
	if on {
		theme.Circle(gtx, ctr, s, cl.Highlight)
		theme.Circle(gtx, ctr, dp(gtx, 5), cl.HighText)
		return
	}
	border := cl.CheckBorder
	if hovered {
		border = cl.Text2
	}
	theme.Circle(gtx, ctr, s, border)
	theme.Circle(gtx, ctr, s-2*max(1, dp(gtx, 1)), cl.Bg)
}
