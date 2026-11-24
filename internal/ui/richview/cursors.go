package richview

// The pointer over the Block Kit blocks this view draws as cards
// (data_visualization, data_table and its fullscreen view, timeline,
// contact_card, share_prompt, event, card, and a carousel's scroller): a
// browser's `cursor: auto`, the text cursor over their selectable text
// and the default arrow everywhere else (Karim, α.160: "text thingy
// instead of pointer" over a chart), the hand on what a click acts on.
// An area without a cursor of its own shows its parents' (Gio's hit), so
// a carousel's cards, views of their own drawn in the scroller, show the
// arrow only when the carousel's box is a region too. Over a chart, ECharts' (zrender's) cursors:
// the canvas' default arrow, and `cursor: 'pointer'` over a hoverable
// series item, a bar, a pie segment or a line's symbol. A contact card is
// .p-contact_card__container{cursor:pointer}, a click on it opening the
// profile or not.

import (
	"image"
	"math"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// kitRegion marks a card block's box: its cursor is the default arrow
// outside its text (textCursors, regionCursors).
func (l *lay) kitRegion(x, y, w, h int) {
	if h > 0 && w > 0 {
		l.d.cursorRegions = append(l.d.cursorRegions, image.Rect(x, y, x+w, y+h))
	}
}

// textCursors sets the text cursor over the view (a message's text, as
// before) but not over its card blocks' boxes, whose arrow is no cursor
// at all: Gio's router takes the topmost cursor that isn't CursorDefault,
// so an area can't put the arrow back over the view's text cursor.
func (v *View) textCursors(gtx layout.Context, size image.Point) {
	if len(v.d.cursorRegions) == 0 {
		pointer.CursorText.Add(gtx.Ops)
		return
	}
	for _, r := range rectsOutside(image.Rectangle{Max: size}, v.d.cursorRegions) {
		s := clip.Rect(r).Push(gtx.Ops)
		pointer.CursorText.Add(gtx.Ops)
		s.Pop()
	}
}

// rectsOutside covers view less holes with rectangles.
func rectsOutside(view image.Rectangle, holes []image.Rectangle) []image.Rectangle {
	out := []image.Rectangle{view}
	for _, h := range holes {
		var next []image.Rectangle
		for _, r := range out {
			next = append(next, subtractRect(r, h)...)
		}
		out = next
	}
	return out
}

// subtractRect is r less h: up to four rectangles (above, below, left and
// right of h).
func subtractRect(r, h image.Rectangle) []image.Rectangle {
	h = h.Intersect(r)
	if h.Empty() {
		return []image.Rectangle{r}
	}
	var out []image.Rectangle
	if h.Min.Y > r.Min.Y {
		out = append(out, image.Rect(r.Min.X, r.Min.Y, r.Max.X, h.Min.Y))
	}
	if h.Max.Y < r.Max.Y {
		out = append(out, image.Rect(r.Min.X, h.Max.Y, r.Max.X, r.Max.Y))
	}
	if h.Min.X > r.Min.X {
		out = append(out, image.Rect(r.Min.X, h.Min.Y, h.Min.X, h.Max.Y))
	}
	if h.Max.X < r.Max.X {
		out = append(out, image.Rect(h.Max.X, h.Min.Y, r.Max.X, h.Max.Y))
	}
	return out
}

// regionCursors sets the card blocks' cursors under the hands handAreas
// adds: the text cursor over their selectable text (a frag with plain
// text), the hand over a pointer box without a click, and the charts'
// items. The rest of a card block's box has none: the arrow.
func (v *View) regionCursors(gtx layout.Context) {
	if len(v.d.cursorRegions) > 0 {
		for i := range v.d.frags {
			f := &v.d.frags[i]
			if f.p1 <= f.p0 {
				continue // a button's label, an icon: not selectable
			}
			fr := fragRect(f)
			if !v.inRegion(fr) {
				continue
			}
			s := clip.Rect(fr).Push(gtx.Ops)
			pointer.CursorText.Add(gtx.Ops)
			s.Pop()
		}
	}
	for _, r := range v.d.handRects {
		s := clip.Rect(r).Push(gtx.Ops)
		pointer.CursorPointer.Add(gtx.Ops)
		s.Pop()
	}
	for i := range v.d.charts {
		v.chartCursors(gtx, &v.d.charts[i])
	}
}

// inRegion reports whether r lies in a card block's box.
func (v *View) inRegion(r image.Rectangle) bool {
	for _, b := range v.d.cursorRegions {
		if r.Overlaps(b) {
			return true
		}
	}
	return false
}

// chartCursors sets the hand over chart c's hoverable items (zrender:
// `cursor: 'pointer'` on a series element; the canvas is the default arrow,
// its block's region).
func (v *View) chartCursors(gtx layout.Context, c *chartPrim) {
	l := c.lay
	if l == nil {
		return
	}
	hand := func(ps clip.PathSpec) {
		s := clip.Outline{Path: ps}.Op().Push(gtx.Ops)
		pointer.CursorPointer.Add(gtx.Ops)
		s.Pop()
	}
	for _, b := range l.Bars {
		if ps, ok := c.barPath(gtx.Ops, b); ok {
			hand(ps)
		}
	}
	for _, s := range l.Sectors {
		if s.Index >= 0 && !math.IsNaN(s.Start) && !math.IsNaN(s.End) {
			hand(c.sectorPath(gtx.Ops, s, s.R))
		}
	}
	for _, sy := range l.Symbols {
		p := c.at(sy.X, sy.Y)
		r := max(3*c.scale, 1) // the 6px symbol
		e := clip.Ellipse(image.Rect(int(p.X-r), int(p.Y-r), int(p.X+r+0.999), int(p.Y+r+0.999)))
		s := e.Push(gtx.Ops)
		pointer.CursorPointer.Add(gtx.Ops)
		s.Pop()
	}
}
