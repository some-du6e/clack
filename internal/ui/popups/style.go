package popups

import (
	"image"
	"image/color"
	"io"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Colors are the popup tokens measured from the 2026 Slack client
// (see the measurement notes in each file). Light and dark sets.
type Colors struct {
	Bg, BgAlt, BgHover   color.NRGBA // card, footer (#f8f8f8), row hover
	Fg, Fg2, Fg3, Muted  color.NRGBA // #1d1c1d, #454447, #5e5d60, #616061
	Subtle               color.NRGBA // #868686 (inactive tab icons)
	Ring                 color.NRGBA // 1px card ring rgba(29,28,29,.13)
	Divider              color.NRGBA // separators rgba(94,93,96,.13)
	Shadow               color.NRGBA // soft popover shadow
	ModalShadow          color.NRGBA
	Backdrop             color.NRGBA // modal scrim rgba(29,28,29,.7)
	Highlight, HighText  color.NRGBA // menu item highlight #1264a3 / white
	Danger, DangerBg     color.NRGBA // #c01343 text, #e01e5a highlighted bg
	Link                 color.NRGBA // #1264a3
	Primary, PrimaryText color.NRGBA // #007a5a
	BtnBorder            color.NRGBA // rgba(94,93,96,.45)
	BtnBg, BtnHover      color.NRGBA
	Disabled, DisText    color.NRGBA // #eaeaea, #454447
	Selected             color.NRGBA // search row selection #eaeaea
	Tab                  color.NRGBA // emoji category active tab bg #dddddd
	Tooltip, TooltipText color.NRGBA
	Kbd, KbdText         color.NRGBA // tooltip shortcut chip
	KbdLight             color.NRGBA // light chips (search footer) #eaeaea
	MatchBg              color.NRGBA // search highlight rgba(255,198,0,.18)
	EmojiHover           color.NRGBA // rgba(247,233,34,.7)
	EmojiSelect          color.NRGBA // rgba(155,223,88,.7)
	Focus, FocusRing     color.NRGBA // input focus #1264a3 + rgba(18,100,163,.3)
	InputBorder          color.NRGBA
	Aubergine            color.NRGBA // active tab underline #83388a
	Presence, Away       color.NRGBA
	Badge, BadgeText     color.NRGBA
	Placeholder          color.NRGBA
	Lightbox             color.NRGBA
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func rgba(v uint32, a float32) color.NRGBA {
	c := rgb(v)
	c.A = uint8(a*255 + .5)
	return c
}

var lightColors = Colors{
	Bg: rgb(0xffffff), BgAlt: rgb(0xf8f8f8), BgHover: rgb(0xf8f8f8),
	Fg: rgb(0x1d1c1d), Fg2: rgb(0x454447), Fg3: rgb(0x5e5d60), Muted: rgb(0x616061),
	Subtle:  rgb(0x868686),
	Ring:    rgba(0x1d1c1d, .13),
	Divider: rgba(0x5e5d60, .13),
	Shadow:  rgba(0x000000, .12), ModalShadow: rgba(0x000000, .35),
	Backdrop:  rgba(0x1d1c1d, .70),
	Highlight: rgb(0x1264a3), HighText: rgb(0xffffff),
	Danger: rgb(0xc01343), DangerBg: rgb(0xe01e5a),
	Link:    rgb(0x1264a3),
	Primary: rgb(0x007a5a), PrimaryText: rgb(0xffffff),
	BtnBorder: rgba(0x5e5d60, .45), BtnBg: rgb(0xffffff), BtnHover: rgb(0xf8f8f8),
	Disabled: rgb(0xeaeaea), DisText: rgb(0x454447),
	Selected: rgb(0xeaeaea),
	Tab:      rgb(0xdddddd),
	Tooltip:  rgb(0x1d1c1d), TooltipText: rgb(0xffffff),
	Kbd: rgb(0x000000), KbdText: rgb(0xffffff), KbdLight: rgb(0xeaeaea),
	MatchBg:     rgba(0xffc600, .18),
	EmojiHover:  rgba(0xf7e922, .7),
	EmojiSelect: rgba(0x9bdf58, .7),
	Focus:       rgb(0x1264a3), FocusRing: rgba(0x1264a3, .3),
	InputBorder: rgba(0x1d1c1d, .3),
	Aubergine:   rgb(0x83388a),
	Presence:    rgb(0x2bac76), Away: rgb(0x616061),
	Badge: rgb(0x7c3085), BadgeText: rgb(0xffffff),
	Placeholder: rgb(0x5e5d60),
	Lightbox:    rgba(0x000000, .7),
}

var darkColors = Colors{
	Bg: rgb(0x222529), BgAlt: rgb(0x1a1d21), BgHover: rgb(0x27292d),
	Fg: rgb(0xd1d2d3), Fg2: rgb(0xababad), Fg3: rgb(0x9a9b9e), Muted: rgb(0xababad),
	Subtle:  rgb(0x868686),
	Ring:    rgba(0xd1d2d3, .13),
	Divider: rgba(0xd1d2d3, .13),
	Shadow:  rgba(0x000000, .35), ModalShadow: rgba(0x000000, .6),
	Backdrop:  rgba(0x000000, .70),
	Highlight: rgb(0x1164a3), HighText: rgb(0xffffff),
	Danger: rgb(0xe8577e), DangerBg: rgb(0xe01e5a),
	Link:    rgb(0x1d9bd1),
	Primary: rgb(0x007a5a), PrimaryText: rgb(0xffffff),
	BtnBorder: rgba(0xd1d2d3, .3), BtnBg: rgb(0x222529), BtnHover: rgb(0x2c2d30),
	Disabled: rgb(0x2c2d30), DisText: rgb(0x9a9b9e),
	Selected: rgb(0x2c2d30),
	Tab:      rgb(0x35373b),
	Tooltip:  rgb(0xf8f8f8), TooltipText: rgb(0x1d1c1d),
	Kbd: rgb(0xdddddd), KbdText: rgb(0x1d1c1d), KbdLight: rgb(0x35373b),
	MatchBg:     rgba(0xffc600, .25),
	EmojiHover:  rgba(0xf7e922, .5),
	EmojiSelect: rgba(0x9bdf58, .5),
	Focus:       rgb(0x1d9bd1), FocusRing: rgba(0x1d9bd1, .3),
	InputBorder: rgba(0xd1d2d3, .3),
	Aubergine:   rgb(0xd29bd6),
	Presence:    rgb(0x2bac76), Away: rgb(0xababad),
	Badge: rgb(0xcd2553), BadgeText: rgb(0xffffff),
	Placeholder: rgb(0x9a9b9e),
	Lightbox:    rgba(0x000000, .7),
}

// colorsOf returns the popup palette for th.
func colorsOf(th *theme.Theme) *Colors {
	if th != nil && th.Dark {
		return &darkColors
	}
	return &lightColors
}

// ---- small layout helpers ----

func dp(gtx layout.Context, v float32) int { return gtx.Dp(unit.Dp(v)) }

// at lays out w with its origin at p (constraints unchanged).
func at(gtx layout.Context, p image.Point, w layout.Widget) layout.Dimensions {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	return w(gtx)
}

// exact returns gtx constrained to exactly sz.
func exact(gtx layout.Context, sz image.Point) layout.Context {
	gtx.Constraints = layout.Exact(sz)
	return gtx
}

// loose returns gtx with Min zero and Max sz.
func loose(gtx layout.Context, sz image.Point) layout.Context {
	gtx.Constraints = layout.Constraints{Max: sz}
	return gtx
}

// record lays out w into a macro and returns it with its dimensions.
func record(gtx layout.Context, w layout.Widget) (op.CallOp, layout.Dimensions) {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	return m.Stop(), d
}

// vcenter lays out w vertically centered in a band of height h.
func vcenter(gtx layout.Context, h int, w layout.Widget) layout.Dimensions {
	call, d := record(gtx, w)
	off := (h - d.Size.Y) / 2
	t := op.Offset(image.Pt(0, off)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return layout.Dimensions{Size: image.Pt(d.Size.X, h)}
}

// fill paints r with c.
func fill(gtx layout.Context, r image.Rectangle, c color.NRGBA) { theme.Fill(gtx, r, c) }

// rrect paints a rounded rect.
func rrect(gtx layout.Context, r image.Rectangle, radius int, c color.NRGBA) {
	theme.RRect(gtx, r, radius, c)
}

// stroke draws a 1-device-pixel-ish inner border of width w around r.
func stroke(gtx layout.Context, r image.Rectangle, radius, w int, c color.NRGBA) {
	if w <= 0 || c.A == 0 {
		return
	}
	// draw as ring = outer rrect minus inner rrect (crisper than clip.Stroke).
	var p clip.Path
	p.Begin(gtx.Ops)
	addRRect(&p, r, float32(radius), false)
	in := r.Inset(w)
	ir := radius - w
	if ir < 0 {
		ir = 0
	}
	addRRect(&p, in, float32(ir), true)
	paint.FillShape(gtx.Ops, c, clip.Outline{Path: p.End()}.Op())
}

// addRRect appends a rounded rectangle sub-path. Gio fills with the
// non-zero winding rule, so a reversed inner rect cuts a hole (ring).
func addRRect(p *clip.Path, r image.Rectangle, rad float32, reverse bool) {
	x0, y0, x1, y1 := float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y)
	if m := (x1 - x0) / 2; rad > m {
		rad = m
	}
	if m := (y1 - y0) / 2; rad > m {
		rad = m
	}
	const k = 0.5523 // bezier circle constant
	c := rad * k
	if reverse {
		p.MoveTo(pt(x0+rad, y0))
		p.CubeTo(pt(x0+rad-c, y0), pt(x0, y0+rad-c), pt(x0, y0+rad))
		p.LineTo(pt(x0, y1-rad))
		p.CubeTo(pt(x0, y1-rad+c), pt(x0+rad-c, y1), pt(x0+rad, y1))
		p.LineTo(pt(x1-rad, y1))
		p.CubeTo(pt(x1-rad+c, y1), pt(x1, y1-rad+c), pt(x1, y1-rad))
		p.LineTo(pt(x1, y0+rad))
		p.CubeTo(pt(x1, y0+rad-c), pt(x1-rad+c, y0), pt(x1-rad, y0))
		p.Close()
		return
	}
	p.MoveTo(pt(x0+rad, y0))
	p.LineTo(pt(x1-rad, y0))
	p.CubeTo(pt(x1-rad+c, y0), pt(x1, y0+rad-c), pt(x1, y0+rad))
	p.LineTo(pt(x1, y1-rad))
	p.CubeTo(pt(x1, y1-rad+c), pt(x1-rad+c, y1), pt(x1-rad, y1))
	p.LineTo(pt(x0+rad, y1))
	p.CubeTo(pt(x0+rad-c, y1), pt(x0, y1-rad+c), pt(x0, y1-rad))
	p.LineTo(pt(x0, y0+rad))
	p.CubeTo(pt(x0, y0+rad-c), pt(x0+rad-c, y0), pt(x0+rad, y0))
	p.Close()
}

// softShadow approximates CSS box-shadow 0 dy blur c under r by stacking
// translucent rounded rects (Gio has no blur).
func softShadow(gtx layout.Context, r image.Rectangle, radius, dy, blur int, c color.NRGBA) {
	if blur <= 0 || c.A == 0 {
		return
	}
	steps := blur / 2
	if steps < 3 {
		steps = 3
	}
	if steps > 10 {
		steps = 10
	}
	base := r.Add(image.Pt(0, dy))
	for i := steps; i >= 1; i-- {
		grow := blur * i / (steps * 2)
		// outer layers fainter; the sum at the center ≈ c.A.
		a := float32(c.A) * 2 / float32(steps+1) * float32(steps-i+1) / float32(steps)
		col := c
		col.A = uint8(a + .5)
		if col.A == 0 {
			continue
		}
		rrect(gtx, base.Inset(-grow), radius+grow, col)
	}
}

// card paints a popover card: shadow, 1px ring, background.
func card(gtx layout.Context, cl *Colors, r image.Rectangle, radius int) {
	softShadow(gtx, r, radius, dp(gtx, 4), dp(gtx, 12), cl.Shadow)
	rrect(gtx, r.Inset(-1), radius+1, cl.Ring)
	rrect(gtx, r, radius, cl.Bg)
}

// modalCard paints a modal card (bigger shadow).
func modalCard(gtx layout.Context, cl *Colors, r image.Rectangle, radius int) {
	softShadow(gtx, r, radius, dp(gtx, 18), dp(gtx, 48), cl.ModalShadow)
	rrect(gtx, r.Inset(-1), radius+1, cl.Ring)
	rrect(gtx, r, radius, cl.Bg)
}

// clipRRect pushes a rounded clip for r.
func clipRRect(gtx layout.Context, r image.Rectangle, radius int) clip.Stack {
	return clip.UniformRRect(r, radius).Push(gtx.Ops)
}

// ---- text ----

// text draws one ellipsized line.
func text(gtx layout.Context, th *theme.Theme, size float32, w font.Weight, c color.NRGBA, s string) layout.Dimensions {
	gtx.Constraints.Min = image.Point{} // labels take their natural size
	return th.Label(gtx, unit.Sp(size), w, c, s)
}

// para draws wrapping text.
func para(gtx layout.Context, th *theme.Theme, size, lh float32, w font.Weight, c color.NRGBA, s string, maxLines int) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	return th.Layout(gtx, theme.TextStyle{Size: unit.Sp(size), Weight: w, Color: c, LineHeight: unit.Sp(lh), MaxLines: maxLines}, s)
}

// textW measures one line.
func textW(gtx layout.Context, th *theme.Theme, size float32, w font.Weight, s string) int {
	return th.Measure(gtx, unit.Sp(size), w, s)
}

// icon draws a registered icon.
func icon(gtx layout.Context, name string, size float32, c color.NRGBA) layout.Dimensions {
	return icons.Get(name)(gtx, unit.Dp(size), c)
}

// ---- interaction ----

// pointerCursor sets the hand cursor over sz.
func pointerCursor(gtx layout.Context, sz image.Point) {
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	pointer.CursorPointer.Add(gtx.Ops)
}

// clickBox lays out w inside c with a pointer cursor; bg is painted behind
// using the final size.
func clickBox(gtx layout.Context, c *widget.Clickable, bg func(gtx layout.Context, sz image.Point, hovered bool), w layout.Widget) layout.Dimensions {
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		call, d := record(gtx, w)
		if bg != nil {
			bg(gtx, d.Size, c.Hovered())
		}
		call.Add(gtx.Ops)
		pointerCursor(gtx, d.Size)
		return d
	})
}

// iconButton is a square icon button with a hover tile.
func iconButton(gtx layout.Context, c *widget.Clickable, name string, box, iconSz float32, col, hover color.NRGBA, radius float32) layout.Dimensions {
	return clickBox(gtx, c, func(gtx layout.Context, sz image.Point, hovered bool) {
		if hovered && hover.A > 0 {
			rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, radius), hover)
		}
	}, func(gtx layout.Context) layout.Dimensions {
		s := dp(gtx, box)
		is := dp(gtx, iconSz)
		off := (s - is) / 2
		t := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
		icon(gtx, name, iconSz, col)
		t.Pop()
		return layout.Dimensions{Size: image.Pt(s, s)}
	})
}

// copyText puts s on the clipboard.
func copyText(gtx layout.Context, s string) {
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(s))})
}

// ---- buttons (Slack c-button) ----

// BtnKind is a Slack button style.
type BtnKind uint8

const (
	BtnOutline BtnKind = iota
	BtnPrimary
	BtnDanger
	BtnDisabled
)

// BtnSize: small = 28px / 13px / radius 4 (cards, picker), medium = 36px /
// 15px / radius 8 (modals, full profile).
type BtnSize uint8

const (
	BtnSmall BtnSize = iota
	BtnMedium
)

// Button draws a Slack button. icon may be "" ; minW in dp (0 = content).
func Button(gtx layout.Context, th *theme.Theme, c *widget.Clickable, kind BtnKind, size BtnSize, label, iconName string, minW float32) layout.Dimensions {
	cl := colorsOf(th)
	h, fs, rad, pad, isz := float32(28), float32(13), float32(4), float32(12), float32(16)
	if size == BtnMedium {
		h, fs, rad, isz = 36, 15, 8, 18
	}
	bg, fg, border := cl.BtnBg, cl.Fg, cl.BtnBorder
	switch kind {
	case BtnPrimary:
		bg, fg, border = cl.Primary, cl.PrimaryText, color.NRGBA{}
	case BtnDanger:
		bg, fg, border = cl.DangerBg, cl.HighText, color.NRGBA{}
	case BtnDisabled:
		bg, fg, border = cl.Disabled, cl.DisText, color.NRGBA{}
	}
	tw := 0
	if label != "" {
		tw = textW(gtx, th, fs, theme.Bold, label)
	}
	iw := 0
	if iconName != "" {
		iw = dp(gtx, isz)
		if label != "" {
			iw += dp(gtx, 4)
		}
	}
	w := tw + iw + 2*dp(gtx, pad)
	if label == "" {
		w = dp(gtx, h)
	}
	if mw := dp(gtx, minW); w < mw {
		w = mw
	}
	if gtx.Constraints.Min.X > w {
		w = gtx.Constraints.Min.X
	}
	hh := dp(gtx, h)
	sz := image.Pt(w, hh)
	draw := func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: sz}
		hovered := c.Hovered() && kind != BtnDisabled
		b := bg
		if hovered {
			switch kind {
			case BtnOutline:
				b = cl.BtnHover
			case BtnPrimary:
				b = theme.Mix(bg, rgb(0x000000), .1)
			case BtnDanger:
				b = theme.Mix(bg, rgb(0x000000), .1)
			}
		}
		if border.A > 0 {
			rrect(gtx, r, dp(gtx, rad), border)
			rrect(gtx, r.Inset(1), dp(gtx, rad)-1, b)
		} else {
			rrect(gtx, r, dp(gtx, rad), b)
		}
		x := (w - tw - iw) / 2
		if iconName != "" {
			at(gtx, image.Pt(x, (hh-dp(gtx, isz))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, iconName, isz, fg)
			})
			x += iw
		}
		if label != "" {
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(exact(gtx, image.Pt(tw, hh)), hh, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = image.Point{}
					return text(gtx, th, fs, theme.Bold, fg, label)
				})
			})
		}
		if kind != BtnDisabled {
			pointerCursor(gtx, sz)
		}
		return layout.Dimensions{Size: sz}
	}
	if kind == BtnDisabled {
		return draw(gtx)
	}
	return c.Layout(gtx, draw)
}

// kbd draws a shortcut chip; dark chips sit on tooltips, light ones on white.
func kbd(gtx layout.Context, th *theme.Theme, label string, dark bool) layout.Dimensions {
	cl := colorsOf(th)
	bg, fg := cl.KbdLight, cl.Fg
	if dark {
		bg, fg = cl.Kbd, cl.KbdText
	}
	h := dp(gtx, 20)
	tw := textW(gtx, th, 13, theme.Regular, label)
	w := tw + dp(gtx, 8)
	if w < h {
		w = h
	}
	rrect(gtx, image.Rect(0, 0, w, h), dp(gtx, 4), bg)
	at(gtx, image.Pt((w-tw)/2, 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Regular, fg, label)
		})
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// splitKeys turns "Ctrl+Shift+K" into chips "Ctrl", "Shift", "K".
func splitKeys(s string) []string {
	if s == "" {
		return nil
	}
	if s == "+" {
		return []string{"+"}
	}
	parts := strings.Split(s, "+")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---- text input ----

// field draws a bordered single-line input (Slack c-input_text) with an
// optional leading icon and a placeholder. h in dp, radius 8.
func field(gtx layout.Context, th *theme.Theme, ed *widget.Editor, placeholder string, leadIcon string, h, size float32) layout.Dimensions {
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	hh := dp(gtx, h)
	r := image.Rect(0, 0, w, hh)
	rad := dp(gtx, 8)
	focused := gtx.Focused(ed)
	if focused {
		ring := dp(gtx, 4)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		rrect(gtx, r, rad, cl.Focus)
		rrect(gtx, r.Inset(1), rad-1, cl.Bg)
	} else {
		rrect(gtx, r, rad, cl.InputBorder)
		rrect(gtx, r.Inset(1), rad-1, cl.Bg)
	}
	x := dp(gtx, 12)
	if leadIcon != "" {
		at(gtx, image.Pt(x, (hh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, leadIcon, 20, cl.Fg2)
		})
		x += dp(gtx, 28)
	}
	tw := w - x - dp(gtx, 12)
	if tw < 1 {
		tw = 1
	}
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, hh)}
			if ed.Len() == 0 && placeholder != "" {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, size, theme.Regular, cl.Placeholder, placeholder)
				})
				call.Add(gtx.Ops)
			}
			return th.Editor(gtx, ed, unit.Sp(size), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
		})
	})
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	pointer.CursorText.Add(gtx.Ops)
	return layout.Dimensions{Size: r.Max}
}

// pt is a float point.
func pt(x, y float32) f32.Point { return f32.Point{X: x, Y: y} }

// ---- sRGB-correct translucency ----
//
// CSS blends colors in sRGB space; Gio blends in linear space, which makes
// every translucent color look lighter (a .7 scrim over white measures 150
// instead of Slack's 96). linearAlpha returns c with its alpha adjusted so
// that Gio's linear blend over bg produces the same pixel CSS would.

// linearAlpha is theme.LinearAlpha (moved into theme by ui-shell).
func linearAlpha(c, bg color.NRGBA) color.NRGBA { return theme.LinearAlpha(c, bg) }

func init() {
	for _, p := range []struct {
		c  *Colors
		bg color.NRGBA
	}{{&lightColors, rgb(0xffffff)}, {&darkColors, rgb(0x222529)}} {
		c := p.c
		for _, f := range []*color.NRGBA{&c.Ring, &c.Divider, &c.Shadow, &c.ModalShadow, &c.Backdrop,
			&c.BtnBorder, &c.MatchBg, &c.EmojiHover, &c.EmojiSelect, &c.FocusRing, &c.InputBorder, &c.Lightbox} {
			*f = linearAlpha(*f, p.bg)
		}
	}
}
