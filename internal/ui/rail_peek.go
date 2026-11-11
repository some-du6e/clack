package ui

import (
	"image"
	"math"
	"time"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// The rail's More menu opens like Slack's: BrowsePeekTrigger (boot-render
// Zxh6) wraps the More button in a PeekTrigger (boot-shared 20Sq) with
// position right-bottom, offsetX 6, offsetY -44, allowanceY 4 and
// forceOpenOnClick.
//
//   - Hover: entering the button starts the trigger delay (T = 300 ms) when
//     peeksEnabledOnMouseEnter is on (the client-side "Show previews when
//     hovering over tabs", default on, off in the simplified layout);
//     leaving the button before it's up cancels it.
//   - While a hover peek is open, every pointer move is checked against
//     the safe area (isMouseInsideSafeArea): the button, the card, then
//     the union of both until there is a previous position, after that the
//     triangle from the previous position to the card's two outermost
//     corners (getSafeTriangleVertices, accurateIsPointInTriangle with 1px
//     of slack). A move outside closes it at once.
//   - Click: the peek opens at once, or a hover peek is kept
//     (forceOpenOnClick); from then on it's an ordinary menu: the overlay
//     covers the window (--clickable), a click outside closes it, and it
//     has the keyboard focus. A non-primary or Ctrl press closes it.
//   - Escape or a row closes it; while the pointer stays on the button it
//     doesn't reopen until the pointer moves on it again (eu / em).
//   - The button keeps its hover tile while the peek is open
//     (.p-peek_trigger--is-peek-visible).

// railPeekDelay is PeekTrigger's default triggerDelay.
const railPeekDelay = 300 * time.Millisecond

// railPeek is the More button's PeekTrigger state (UI goroutine only).
type railPeek struct {
	trig    image.Rectangle // the More button, window coords (last frame)
	hovered bool            // the pointer was on the button last frame
	enterAt time.Time       // the hover delay runs from here (zero: not counting)
	menu    *popups.Menu    // the open peek or menu
	clicked bool            // opened or pinned by a click

	prev    image.Point // the last pointer move while open (safe triangle)
	hasPrev bool

	dismissed bool // eu: closed by Escape or a row with the pointer on the button
	rearm     bool // em: the first move after that; the next one counts again
	lastPtr   image.Point
}

// railPeekOpen reports whether the More peek or menu is showing.
func (a *App) railPeekOpen() bool {
	p := &a.peek
	if p.menu != nil && p.menu.Dismissed() {
		p.menu = nil
	}
	return p.menu != nil
}

// peeksOnHover is the accessibility store's peeksEnabledOnMouseEnter, a
// client-side setting (the prefs dialog's AccessibilityTabPreviews "Show
// previews when hovering over tabs" logs "Changed client-side pref for
// peeks enabled"; nothing goes to users.prefs). It starts on; the
// simplified layout turns it off (BBM's setPeeksEnabledOnMouseEnter).
// Clack's Preferences don't offer the toggle yet, so it's on outside BBM.
func (a *App) peeksOnHover() bool { return !a.prefs.BBM }

// railMoreFrame runs the More button's peek logic for this frame: trig is
// the button's window rectangle, hov whether the pointer is on it.
func (a *App) railMoreFrame(gtx layout.Context, trig image.Rectangle, hov bool) {
	p := &a.peek
	p.trig = trig
	open := a.railPeekOpen()
	switch {
	case hov && !p.hovered: // onMouseEnter
		if !p.dismissed && !open && a.peeksOnHover() {
			p.enterAt = gtx.Now
		}
	case !hov && p.hovered: // onMouseLeave: cancel a pending open
		p.dismissed, p.rearm = false, false
		if !open {
			p.enterAt = time.Time{}
		}
	}
	p.hovered = hov
	if open || p.enterAt.IsZero() {
		return
	}
	if due := p.enterAt.Add(railPeekDelay); gtx.Now.Before(due) {
		gtx.Execute(op.InvalidateCmd{At: due})
		return
	}
	p.enterAt = time.Time{}
	a.openRailMorePeek(false)
}

// railMoreClick is the More button's click (forceOpenOnClick).
func (a *App) railMoreClick() {
	p := &a.peek
	p.enterAt = time.Time{}
	if a.railPeekOpen() {
		if !p.clicked {
			p.clicked = true
			p.menu.Pin()
			a.Invalidate()
		}
		return
	}
	a.openRailMorePeek(true)
}

// openRailMorePeek opens BrowsePeekMenu by the button: a hover peek, or
// (clicked) an ordinary menu.
func (a *App) openRailMorePeek(clicked bool) {
	p := &a.peek
	items := a.railMoreItems()
	var m *popups.Menu
	m = popups.NewMenuOpts(a, toPopupItems(items), popups.MenuOpts{
		Anchor: p.trig, Popover: "right-bottom", OffsetX: 6, OffsetY: -44, AllowanceY: 4,
		Peek: !clicked,
		OnClose: func() {
			if p.menu != m {
				return
			}
			p.menu = nil
			p.hasPrev = false
			if p.hovered && !p.clicked { // eA: Escape or a row, the pointer still on the button
				p.dismissed = true
			}
			p.clicked = false
			a.Invalidate()
		},
	})
	p.menu, p.clicked, p.hasPrev = m, clicked, false
	a.ShowOverlay(m)
}

// railPeekPointer is the PeekTrigger's document mousemove and the
// trigger's onMouseMove (pe in window coordinates).
func (a *App) railPeekPointer(pe pointer.Event) {
	p := &a.peek
	pt := image.Pt(int(pe.Position.X+.5), int(pe.Position.Y+.5))
	if pe.Kind == pointer.Press && pt.In(p.trig) && a.railPeekOpen() &&
		(pe.Buttons != pointer.ButtonPrimary || pe.Modifiers.Contain(key.ModCtrl)) {
		// onMouseDown: a non-primary or Ctrl press on the button closes it
		p.menu.Dismiss()
		a.Invalidate()
		return
	}
	if pe.Kind != pointer.Move && pe.Kind != pointer.Drag {
		return
	}
	moved := pt != p.lastPtr
	p.lastPtr = pt
	// eN: after a dismissal on the button, the first move only rearms it
	// and the next one starts the delay again
	if moved && pt.In(p.trig) && !a.railPeekOpen() {
		switch {
		case p.dismissed:
			p.dismissed, p.rearm = false, true
		case p.rearm:
			p.rearm = false
			if a.peeksOnHover() {
				p.enterAt = time.Now()
				a.Invalidate()
			}
		}
	}
	if !a.railPeekOpen() || p.clicked || !p.menu.Peeking() {
		return
	}
	if !railPeekSafe(pt, p.trig, p.menu.Rect(), p.prev, p.hasPrev) {
		p.menu.Dismiss()
		a.Invalidate()
		return
	}
	p.prev, p.hasPrev = pt, true
}

// railPeekSafe is PeekTrigger's isMouseInsideSafeArea for pointer e, the
// trigger t, the card c and the previous position prev (when has).
func railPeekSafe(e image.Point, t, c image.Rectangle, prev image.Point, has bool) bool {
	in := func(p image.Point, r image.Rectangle) bool { // pointInsideBounds: edges included
		return p.X >= r.Min.X && p.X <= r.Max.X && p.Y >= r.Min.Y && p.Y <= r.Max.Y
	}
	if in(e, t) {
		return true
	}
	if c.Empty() {
		return false
	}
	if in(e, c) {
		return true
	}
	if !has {
		return in(e, t.Union(c))
	}
	a, b := safeTriangle(prev, c)
	return pointInTriangle(e, prev, a, b, 1)
}

// safeTriangle is getSafeTriangleVertices: of the card's corners, the two
// with the smallest and largest angle seen from p (mirrored when the card
// is to p's left).
func safeTriangle(p image.Point, c image.Rectangle) (image.Point, image.Point) {
	left := c.Max.X < p.X && c.Min.Y <= p.Y && p.Y <= c.Max.Y
	corners := [4]image.Point{{c.Min.X, c.Min.Y}, {c.Min.X, c.Max.Y}, {c.Max.X, c.Min.Y}, {c.Max.X, c.Max.Y}}
	var lo, hi image.Point
	loA, hiA := math.Inf(1), math.Inf(-1)
	for _, k := range corners {
		dx := float64(k.X - p.X)
		if left {
			dx = float64(p.X - k.X)
		}
		ang := math.Atan2(float64(k.Y-p.Y), dx)
		if ang < loA {
			loA, lo = ang, k
		}
		if ang > hiA {
			hiA, hi = ang, k
		}
	}
	return lo, hi
}

// pointInTriangle is accurateIsPointInTriangle (boot dUEq): inside the
// triangle, or within eps of an edge.
func pointInTriangle(e, t, n, a image.Point, eps float64) bool {
	ex, ey := float64(e.X), float64(e.Y)
	tx, ty, nx, ny, ax, ay := float64(t.X), float64(t.Y), float64(n.X), float64(n.Y), float64(a.X), float64(a.Y)
	if ex <= math.Min(tx, math.Min(nx, ax))-eps || ex >= math.Max(tx, math.Max(nx, ax))+eps ||
		ey <= math.Min(ty, math.Min(ny, ay))-eps || ey >= math.Max(ty, math.Max(ny, ay))+eps {
		return false
	}
	// isPointInTriangle with no slack (barycentric)
	r0, r1 := ax-tx, ay-ty
	s0, s1 := nx-tx, ny-ty
	o0, o1 := ex-tx, ey-ty
	l := r0*r0 + r1*r1
	d := r0*s0 + r1*s1
	c := r0*o0 + r1*o1
	u := s0*s0 + s1*s1
	m := s0*o0 + s1*o1
	if den := l*u - d*d; den != 0 {
		f := 1 / den
		h := (u*c - d*m) * f
		q := (l*m - d*c) * f
		if h >= 0 && q >= 0 && h+q < 1 {
			return true
		}
	}
	e2 := eps * eps
	return segDist2(ex, ey, tx, ty, nx, ny) <= e2 || segDist2(ex, ey, nx, ny, ax, ay) <= e2 || segDist2(ex, ey, ax, ay, tx, ty) <= e2
}

// segDist2 is the squared distance from (px,py) to the segment a-b.
func segDist2(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	l := dx*dx + dy*dy
	t := 0.0
	if l > 0 {
		t = math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/l))
	}
	qx, qy := ax+t*dx-px, ay+t*dy-py
	return qx*qx + qy*qy
}
