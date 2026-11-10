package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// colorPopover is Slack's custom-theme colour popover (CustomHEXPickerSection
// renderPopover: "Pick a color", ColorPicker = HSVPicker's saturation /
// brightness square and hue slider, then the "#" hex field). Dragging
// applies the colour when the pointer lets go (Slack debounces 250 ms);
// a typed hex applies on Enter or when the field loses focus.
type colorPopover struct {
	a       *App
	anchor  image.Point
	h, s, v float64 // hue 0–360, saturation and brightness 0–1
	sent    string  // the hex last applied
	set     func(hex string)
	ed      widget.Editor
	focused bool
	done    bool
	sv, hue int // drag tags
	card    image.Rectangle
	drag    int // 1 square, 2 hue, 0 none
}

func newColorPopover(a *App, anchor image.Point, hex string, set func(string)) *colorPopover {
	p := &colorPopover{a: a, anchor: anchor, set: set, sent: strings.ToUpper(hex)}
	p.h, p.s, p.v = hexToHSV(hex)
	p.ed = widget.Editor{SingleLine: true, Submit: true, MaxLen: 7}
	p.ed.SetText(strings.TrimPrefix(p.sent, "#"))
	return p
}

func (p *colorPopover) Dismissed() bool { return p.done }

// hex is the colour the controls show.
func (p *colorPopover) hex() string {
	c := hsvToRGB(p.h, p.s, p.v)
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

// apply sends hex if it changed (ColorPicker's onChange / onBlur).
func (p *colorPopover) apply(hex string) {
	if hex = strings.ToUpper(hex); hex != p.sent {
		p.sent = hex
		p.set(hex)
	}
}

func (p *colorPopover) Layout(gtx layout.Context) layout.Dimensions {
	a := p.a
	c := &a.th.C
	win := gtx.Constraints.Max
	// outside the card: close (applying a typed hex, like onBlur)
	defer clip.Rect{Max: win}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, p)
	for {
		e, ok := gtx.Event(pointer.Filter{Target: p, Kinds: pointer.Press}, key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		switch e := e.(type) {
		case pointer.Event:
			if !e.Position.Round().In(p.card) {
				p.commitText()
				p.done = true
			}
		case key.Event:
			if e.State == key.Press {
				p.done = true
			}
		}
	}
	if p.done {
		return layout.Dimensions{Size: win}
	}
	pad, w := gtx.Dp(16), gtx.Dp(252)
	sqW, sqH, hueH, inH := w-2*pad, gtx.Dp(150), gtx.Dp(12), gtx.Dp(32)
	h := pad + gtx.Dp(24) + sqH + gtx.Dp(12) + hueH + gtx.Dp(12) + inH + pad
	pos := p.anchor
	if pos.X+w > win.X {
		pos.X = win.X - w - gtx.Dp(8)
	}
	if pos.Y+h > win.Y {
		pos.Y = win.Y - h - gtx.Dp(8)
	}
	pos.X, pos.Y = max(pos.X, gtx.Dp(8)), max(pos.Y, gtx.Dp(8))
	p.card = image.Rectangle{Min: pos, Max: pos.Add(image.Pt(w, h))}
	theme.RRect(gtx, p.card, gtx.Dp(8), c.BgRaised)
	theme.Border(gtx, p.card, gtx.Dp(8), gtx.Dp(1), c.Border)
	at(gtx, pos.Add(image.Pt(pad, pad)), image.Pt(sqW, gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 15, theme.Bold, c.Fg, "Pick a color")
	})
	sq := image.Rectangle{Min: pos.Add(image.Pt(pad, pad+gtx.Dp(24)))}
	sq.Max = sq.Min.Add(image.Pt(sqW, sqH))
	hr := image.Rectangle{Min: image.Pt(sq.Min.X, sq.Max.Y+gtx.Dp(12))}
	hr.Max = hr.Min.Add(image.Pt(sqW, hueH))
	p.input(gtx, sq, hr)

	// the square: the hue, white to the left, black to the bottom
	paint.FillShape(gtx.Ops, hsvToRGB(p.h, 1, 1), clip.Rect(sq).Op())
	svGradient(gtx, sq, f32.Pt(float32(sq.Min.X), 0), f32.Pt(float32(sq.Max.X), 0), color.NRGBA{255, 255, 255, 255}, color.NRGBA{255, 255, 255, 0})
	svGradient(gtx, sq, f32.Pt(0, float32(sq.Min.Y)), f32.Pt(0, float32(sq.Max.Y)), color.NRGBA{0, 0, 0, 0}, color.NRGBA{0, 0, 0, 255})
	kx := sq.Min.X + int(p.s*float64(sqW))
	ky := sq.Min.Y + int((1-p.v)*float64(sqH))
	theme.Circle(gtx, image.Pt(kx, ky), gtx.Dp(14), color.NRGBA{255, 255, 255, 255})
	theme.Circle(gtx, image.Pt(kx, ky), gtx.Dp(10), hsvToRGB(p.h, p.s, p.v))
	// the hue slider: six stops around the wheel
	for i := 0; i < 6; i++ {
		x0 := hr.Min.X + i*sqW/6
		x1 := hr.Min.X + (i+1)*sqW/6
		seg := image.Rect(x0, hr.Min.Y, x1, hr.Max.Y)
		svGradient(gtx, seg, f32.Pt(float32(x0), 0), f32.Pt(float32(x1), 0), hsvToRGB(float64(i)*60, 1, 1), hsvToRGB(float64(i+1)*60, 1, 1))
	}
	hx := hr.Min.X + int(p.h/360*float64(sqW))
	theme.Circle(gtx, image.Pt(hx, (hr.Min.Y+hr.Max.Y)/2), gtx.Dp(16), color.NRGBA{255, 255, 255, 255})
	theme.Circle(gtx, image.Pt(hx, (hr.Min.Y+hr.Max.Y)/2), gtx.Dp(12), hsvToRGB(p.h, 1, 1))

	// the hex field
	ir := image.Rectangle{Min: image.Pt(sq.Min.X, hr.Max.Y+gtx.Dp(12))}
	ir.Max = ir.Min.Add(image.Pt(sqW, inH))
	for {
		e, ok := p.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			p.commitText()
		}
	}
	f := gtx.Focused(&p.ed)
	if p.focused && !f {
		p.commitText() // onBlur
	}
	p.focused = f
	if !f && p.ed.Text() != strings.TrimPrefix(p.hex(), "#") && p.drag != 0 {
		p.ed.SetText(strings.TrimPrefix(p.hex(), "#"))
	}
	theme.RRect(gtx, ir, gtx.Dp(6), c.Bg)
	bc := c.BorderStrong
	if f {
		bc = c.Link
	}
	theme.Border(gtx, ir, gtx.Dp(6), gtx.Dp(1), bc)
	at(gtx, ir.Min.Add(image.Pt(gtx.Dp(10), 0)), image.Pt(gtx.Dp(12), inH), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, inH, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Regular, c.FgMuted, "#")
		})
	})
	at(gtx, ir.Min.Add(image.Pt(gtx.Dp(26), 0)), image.Pt(sqW-gtx.Dp(34), inH), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, inH, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return a.th.Editor(gtx, &p.ed, 15, c.Fg, c.Link)
		})
	})
	return layout.Dimensions{Size: win}
}

// input handles drags on the square and the slider.
func (p *colorPopover) input(gtx layout.Context, sq, hr image.Rectangle) {
	for _, t := range []struct {
		tag  *int
		r    image.Rectangle
		kind int
	}{{&p.sv, sq, 1}, {&p.hue, hr.Inset(-gtx.Dp(4)), 2}} {
		st := clip.Rect(t.r).Push(gtx.Ops)
		event.Op(gtx.Ops, t.tag)
		st.Pop()
		for {
			e, ok := gtx.Event(pointer.Filter{Target: t.tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
			if !ok {
				break
			}
			pe, ok := e.(pointer.Event)
			if !ok {
				continue
			}
			switch pe.Kind {
			case pointer.Press, pointer.Drag:
				p.drag = t.kind
				x := clamp01((float64(pe.Position.X) - float64(sq.Min.X)) / float64(sq.Dx()))
				if t.kind == 1 {
					p.s, p.v = x, 1-clamp01((float64(pe.Position.Y)-float64(sq.Min.Y))/float64(sq.Dy()))
				} else {
					p.h = x * 360
				}
				p.ed.SetText(strings.TrimPrefix(p.hex(), "#"))
				p.a.Invalidate()
			case pointer.Release, pointer.Cancel:
				p.drag = 0
				p.apply(p.hex())
			}
		}
	}
}

// commitText applies a valid typed hex (Slack's $V test); otherwise the
// field goes back to the colour shown.
func (p *colorPopover) commitText() {
	if h, ok := theme.ParseHexColor(p.ed.Text()); ok {
		p.h, p.s, p.v = hexToHSV(h)
		p.apply(h)
		return
	}
	p.ed.SetText(strings.TrimPrefix(p.sent, "#"))
}

func svGradient(gtx layout.Context, r image.Rectangle, p1, p2 f32.Point, c1, c2 color.NRGBA) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.LinearGradientOp{Stop1: p1, Color1: c1, Stop2: p2, Color2: c2}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }

// hsvToRGB is the picker's HSV (hue in degrees) as an opaque colour.
func hsvToRGB(h, s, v float64) color.NRGBA {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g = c, x
	case h < 120:
		r, g = x, c
	case h < 180:
		g, b = c, x
	case h < 240:
		g, b = x, c
	case h < 300:
		r, b = x, c
	default:
		r, b = c, x
	}
	u := func(f float64) uint8 { return uint8(math.Round((f + m) * 255)) }
	return color.NRGBA{R: u(r), G: u(g), B: u(b), A: 255}
}

// hexToHSV reads "#RRGGBB" (black for junk).
func hexToHSV(hex string) (h, s, v float64) {
	c, ok := hexColor(hex)
	if !ok {
		return 0, 0, 0
	}
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	d := mx - mn
	switch {
	case d == 0:
		h = 0
	case mx == r:
		h = 60 * math.Mod((g-b)/d, 6)
	case mx == g:
		h = 60 * ((b-r)/d + 2)
	default:
		h = 60 * ((r-g)/d + 4)
	}
	if h < 0 {
		h += 360
	}
	if mx > 0 {
		s = d / mx
	}
	return h, s, mx
}
