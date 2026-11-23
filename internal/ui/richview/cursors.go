package richview

// The pointer over the Block Kit blocks this view draws as cards
// (data_visualization, data_table and its fullscreen view, timeline,
// contact_card, share_prompt): a browser's `cursor: auto`, the text
// cursor over their selectable text and the default arrow everywhere else
// (Karim, α.160: "text thingy instead of pointer" over a chart), the hand
// on what a click acts on. Over a chart, ECharts' (zrender's) cursors:
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
// outside its text (regionCursors).
func (l *lay) kitRegion(x, y, w, h int) {
	if h > 0 && w > 0 {
		l.d.cursorRegions = append(l.d.cursorRegions, image.Rect(x, y, x+w, y+h))
	}
}

// regionCursors sets the card blocks' cursors under the hands handAreas
// adds: the default arrow over each card block, then the text cursor over
// its selectable text (a frag with plain text), the hand over a pointer
// box without a click, and the charts' items.
func (v *View) regionCursors(gtx layout.Context) {
	for _, r := range v.d.cursorRegions {
		s := clip.Rect(r).Push(gtx.Ops)
		pointer.CursorDefault.Add(gtx.Ops)
		s.Pop()
	}
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
