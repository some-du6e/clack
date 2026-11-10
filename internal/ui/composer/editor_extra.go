package composer

import (
	"image"
	"time"

	"gioui.org/gesture"
	"gioui.org/layout"
)

// PosRect returns the line box at document position pos (x of the caret
// before pos, top and bottom of its visual line) in the editor's local
// pixel coordinates from the last Layout. Zero before the first Layout.
func (e *Editor) PosRect(pos int) image.Rectangle {
	l := &e.lay
	if len(l.lines) == 0 {
		return image.Rectangle{}
	}
	pos = min(max(pos, 0), e.doc.Len())
	li := l.lineOf(pos)
	ln := &l.lines[li]
	x := l.vx(li, pos)
	return image.Rect(x, ln.top-e.scrollY, x+1, ln.top+ln.h-e.scrollY)
}

// HitPos maps a point in the editor's local pixel coordinates (as Layout
// drew it) to a document position: the caret position nearest the point,
// as a browser's caretPositionFromPoint picks it. Before the first
// Layout it is the end of the document.
func (e *Editor) HitPos(pt image.Point) int {
	e.init()
	return e.hitPos(pt)
}

// WordRange is the word around pos (a double click's selection).
func (e *Editor) WordRange(pos int) (lo, hi int) {
	e.init()
	return edWordAt(e.doc, min(max(pos, 0), e.doc.Len()))
}

// LineRange is the line around pos (a triple click's selection).
func (e *Editor) LineRange(pos int) (lo, hi int) {
	e.init()
	l := e.doc.LineOf(min(max(pos, 0), e.doc.Len()))
	return e.doc.LineStart(l), e.doc.LineEnd(l)
}

// SeedClick tells the editor about a click its host handled before the
// editor was shown (the click that opened it, at pt in the editor's
// coordinates): a press that follows it quickly counts on from it, so a
// double click that both opens the editor and lands its second press in
// it still selects the word.
func (e *Editor) SeedClick(now time.Time, pt image.Point) {
	e.seedAt, e.seedPt, e.clickBias = now, pt, 0
}

// seedWindow is how close a press must follow the seeded click (and
// gesture.Click's own double-click window).
const seedWindow = 400 * time.Millisecond

func (e *Editor) seededClicks(gtx layout.Context, ev gesture.ClickEvent) int {
	if ev.NumClicks <= 1 {
		e.clickBias = 0
		if !e.seedAt.IsZero() && gtx.Now.Sub(e.seedAt) < seedWindow {
			if d := ev.Position.Sub(e.seedPt); d.X*d.X+d.Y*d.Y <= 16 {
				e.clickBias = 1
			}
		}
		e.seedAt = time.Time{}
	}
	return ev.NumClicks + e.clickBias
}
