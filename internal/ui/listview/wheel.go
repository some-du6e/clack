package listview

import (
	"math"

	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
)

// wheelScroll animates a notched mouse wheel for the views that read
// pointer.Scroll themselves (the grid, the board, an item's page), with
// the shared animation every gesture.Scroll uses (Gio patch 9,
// third_party/gioui.org/gesture/clack_wheel.go). Precise deltas move at
// once.
type wheelScroll struct {
	w      gesture.Wheel
	fx, fy float32 // precise deltas' sub-pixel leftovers
	// the animation's distance so far and how much of it was reported in
	// whole pixels
	ax, ay float64
	px, py int
}

// event is the distance to move now for scroll event pe, with the view
// at (x, y) of at most (maxX, maxY). Shift turns a vertical wheel
// horizontal when shift is set.
func (s *wheelScroll) event(gtx layout.Context, pe pointer.Event, x, y, maxX, maxY int, shift bool) (dx, dy int) {
	if shift && pe.Modifiers.Contain(key.ModShift) && pe.Scroll.X == 0 {
		pe.Scroll.X, pe.Scroll.Y = pe.Scroll.Y, 0
	}
	rx := pointer.ScrollRange{Min: -x, Max: max(0, maxX-x)}
	ry := pointer.ScrollRange{Min: -y, Max: max(0, maxY-y)}
	d := s.w.Scroll(gtx.Now, pe, rx, ry)
	if !s.w.Active() {
		s.ax, s.ay, s.px, s.py = 0, 0, 0, 0
	}
	s.fx += d.X
	s.fy += d.Y
	dx, dy = int(s.fx), int(s.fy)
	s.fx -= float32(dx)
	s.fy -= float32(dy)
	return dx, dy
}

// tick is the distance the animation moved since the last frame; it asks
// for the next frame while it runs.
func (s *wheelScroll) tick(gtx layout.Context) (dx, dy int) {
	if !s.w.Active() {
		return 0, 0
	}
	d := s.w.Tick(gtx.Source, gtx.Now)
	s.ax += float64(d.X)
	s.ay += float64(d.Y)
	dx, dy = int(math.Round(s.ax))-s.px, int(math.Round(s.ay))-s.py
	s.px += dx
	s.py += dy
	if !s.w.Active() {
		s.ax, s.ay, s.px, s.py = 0, 0, 0, 0
	}
	return dx, dy
}
