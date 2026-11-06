package composer

import "image"

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
