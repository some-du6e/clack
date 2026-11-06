package ui

import (
	"image"
	"image/color"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// clickedSet records clicks seen by clickArea. Gio's Clickable.Layout
// drains pending events, so clicks must be read before Layout; clickArea
// does that and callers check clicked(c) after laying out. UI goroutine only.
var clickedSet = map[*widget.Clickable]bool{}

// clicked reports (once) whether c was clicked this frame.
func clicked(c *widget.Clickable) bool {
	if clickedSet[c] {
		delete(clickedSet, c)
		return true
	}
	return false
}

// clickArea lays out w as a clickable with a pointer cursor; bg(hovered,
// pressed) paints behind it. Check clicked(c) afterwards.
func clickArea(gtx layout.Context, c *widget.Clickable, bg func(gtx layout.Context, sz image.Point, hovered bool), w layout.Widget) layout.Dimensions {
	if c.Clicked(gtx) {
		clickedSet[c] = true
	}
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		m := op.Record(gtx.Ops)
		d := w(gtx)
		call := m.Stop()
		if bg != nil {
			bg(gtx, d.Size, c.Hovered())
		}
		call.Add(gtx.Ops)
		defer clip.Rect{Max: d.Size}.Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		return d
	})
}

// iconButton draws a square button of side box with an icon centered.
func iconButton(gtx layout.Context, c *widget.Clickable, name string, box, iconSz unit.Dp, col, hover color.NRGBA, radius unit.Dp) layout.Dimensions {
	return clickArea(gtx, c, func(gtx layout.Context, sz image.Point, hovered bool) {
		if hovered && hover.A > 0 {
			theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(radius), hover)
		}
	}, func(gtx layout.Context) layout.Dimensions {
		s := gtx.Dp(box)
		is := gtx.Dp(iconSz)
		off := (s - is) / 2
		t := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
		icons.Get(name)(gtx, iconSz, col)
		t.Pop()
		return layout.Dimensions{Size: image.Pt(s, s)}
	})
}

// fixed returns constraints forcing exactly sz.
func fixed(gtx layout.Context, sz image.Point) layout.Context {
	gtx.Constraints = layout.Exact(sz)
	return gtx
}

// at runs w with its origin at p and max constraints sz.
func at(gtx layout.Context, p image.Point, sz image.Point, w layout.Widget) layout.Dimensions {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Constraints{Max: sz}
	return w(gtx)
}

// atExact is at with exact constraints.
func atExact(gtx layout.Context, p image.Point, sz image.Point, w layout.Widget) layout.Dimensions {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(sz)
	return w(gtx)
}

// vcenter offsets w vertically so it is centered in height h.
func vcenter(gtx layout.Context, h int, w layout.Widget) layout.Dimensions {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	call := m.Stop()
	defer op.Offset(image.Pt(0, (h-d.Size.Y)/2)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(d.Size.X, h)}
}

// badge draws a mention-count pill (Slack: 18px tall, 700 12px, 9px padding).
func (a *App) badge(gtx layout.Context, n int, bg, fg color.NRGBA) layout.Dimensions {
	txt := itoa(n)
	h := gtx.Dp(18)
	tw := a.th.Measure(gtx, 12, theme.Bold, txt)
	w := tw + gtx.Dp(14)
	if w < h {
		w = h
	}
	theme.RRect(gtx, image.Rect(0, 0, w, h), h/2, bg)
	defer op.Offset(image.Pt((w-tw)/2, 0)).Push(gtx.Ops).Pop()
	vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 12, theme.Bold, fg, txt)
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// clickables is a lazily grown keyed set of widget.Clickable (sidebar rows,
// rail tabs, ...). Clickables must persist across frames.
type clickables map[string]*widget.Clickable

func (c clickables) get(k string) *widget.Clickable {
	if b, ok := c[k]; ok {
		return b
	}
	b := new(widget.Clickable)
	c[k] = b
	return b
}
