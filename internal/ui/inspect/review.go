package inspect

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// The review. A Shift+click's pick or a dragged region doesn't go to
// the clipboard at once (a plain click's does): its shot is shown frozen over the dimmed
// window as the copy will show it (an element's margin blurred, the
// element sharp and outlined; a region as it was), and the user drags
// rects over anything in it (a name, an avatar, someone else's message)
// to blur those out. The shot shown is the shot itself, scaled to fit
// the window, so a rect lands on exactly the pixels it was drawn over,
// whatever the window has done since. Enter (or Mod+C) copies and saves
// the report with the rects baked into the image (Compose, through
// Redact: what the copy shows there comes from the pixels around each
// rect, nothing from inside it), Backspace, Delete or Mod+Z takes the
// last rect back, a right click takes back the newest one under the
// pointer, and Esc or the shortcut drops the report: nothing is saved
// or copied.

// review is a capture's shot waiting for the user's rects.
type review struct {
	c    *capture
	shot *image.RGBA

	// shown is the shot as the copy shows it with the rects of version
	// shownV (Redacted, then paintShot: an element's margin blurred),
	// made off the UI goroutine.
	shown   paint.ImageOp
	ready   bool
	shownV  int
	version int // bumped by every change to marks

	marks []image.Rectangle // shot px, oldest first

	dragging bool
	from, to f32.Point       // the drag, window px
	view     image.Rectangle // where the shot was drawn last frame, window px
}

// minMark is the smallest drag that marks a rect, dp each way: less is
// a click.
const minMark = 4

// startReview shows c's shot for blurring (UI goroutine, from
// AfterFrame).
func (t *Tool) startReview(c *capture, shot *image.RGBA) {
	t.release()
	r := &review{c: c, shot: shot}
	t.state, t.rev = reviewing, r
	t.redraw(r)
	t.host.Invalidate()
}

// redraw remakes the shown image from the shot and the marks as Compose
// will draw them, off the UI goroutine; the newest version wins.
func (t *Tool) redraw(r *review) {
	v := r.version
	marks := append([]image.Rectangle(nil), r.marks...)
	shot, elem, scale, h := r.shot, r.c.elem, r.c.scale*r.c.k, t.host
	go func() {
		img := image.NewRGBA(shot.Bounds())
		paintShot(img, shot.Bounds().Min, Redacted(shot, marks, scale), elem, scale)
		h.Post(func() {
			if t.rev != r || v < r.shownV {
				return
			}
			r.shown, r.shownV, r.ready = paint.NewImageOp(img), v, true
			h.Invalidate()
		})
	}()
}

// changed is a change to r's marks.
func (t *Tool) changed(r *review) {
	r.version++
	t.redraw(r)
}

// finishReview sends the report with the rects marked so far.
func (t *Tool) finishReview() {
	r := t.rev
	r.c.redact = append([]image.Rectangle(nil), r.marks...)
	r.c.rep.Redacted = len(r.marks)
	t.stop()
	t.deliver(r.c, r.shot, nil)
}

// dropReview ends the review with nothing saved or copied.
func (t *Tool) dropReview() {
	t.stop()
	t.host.Toast("Inspect report discarded: nothing copied")
}

// reviewKey handles a key press in the review.
func (t *Tool) reviewKey(ke key.Event) {
	r := t.rev
	mod := ke.Modifiers.Contain(key.ModShortcut)
	switch {
	case comboPress(t.combo, ke):
		t.dropReview()
	case ke.Name == key.NameEscape:
		if r.dragging {
			r.dragging = false // only the rect being drawn
			return
		}
		t.dropReview()
	case ke.Name == key.NameReturn || ke.Name == key.NameEnter || mod && ke.Name == "C":
		t.finishReview()
	case ke.Name == key.NameDeleteBackward || ke.Name == key.NameDeleteForward || mod && ke.Name == "Z":
		if r.dragging {
			r.dragging = false
		} else if n := len(r.marks); n > 0 {
			r.marks = r.marks[:n-1]
			t.changed(r)
		}
	}
}

// reviewPointer handles the window-wide area's pointer events in the
// review: a primary drag over the shot marks a rect, a secondary press
// takes back the newest rect under it.
func (t *Tool) reviewPointer(pe pointer.Event) {
	r := t.rev
	switch pe.Kind {
	case pointer.Press:
		switch {
		case pe.Buttons&pointer.ButtonPrimary != 0:
			if !r.view.Empty() {
				r.dragging, r.from, r.to = true, pe.Position, pe.Position
			}
		case pe.Buttons&pointer.ButtonSecondary != 0 && !r.dragging:
			p := r.toShot(pe.Position)
			for i := len(r.marks) - 1; i >= 0; i-- {
				if p.In(r.marks[i]) {
					r.marks = append(r.marks[:i], r.marks[i+1:]...)
					t.changed(r)
					break
				}
			}
		}
	case pointer.Drag:
		if r.dragging {
			r.to = pe.Position
		}
	case pointer.Release:
		if r.dragging && pe.Buttons&pointer.ButtonPrimary == 0 {
			r.to = pe.Position
			r.dragging = false
			min := float32(minMark) * t.scale
			if abs32(r.to.X-r.from.X) >= min && abs32(r.to.Y-r.from.Y) >= min {
				if m := r.markFor(r.from, r.to); !m.Empty() {
					r.marks = append(r.marks, m)
					t.changed(r)
				}
			}
		}
	case pointer.Cancel:
		r.dragging = false
	}
}

// Marks are the rects marked in the review so far (shot px), and the
// shot's size; ok false outside a review (tests).
func (t *Tool) Marks() (marks []image.Rectangle, shot image.Point, ok bool) {
	if t.state != reviewing {
		return nil, image.Point{}, false
	}
	return append([]image.Rectangle(nil), t.rev.marks...), t.rev.shot.Bounds().Size(), true
}

// ReviewView is where the review shows the shot (window px), empty
// outside a review (tests).
func (t *Tool) ReviewView() image.Rectangle {
	if t.state != reviewing {
		return image.Rectangle{}
	}
	return t.rev.view
}

// toShot maps p (window px) into the shot (px), by last frame's view.
func (r *review) toShot(p f32.Point) image.Point {
	v, s := r.view, r.shot.Bounds()
	if v.Empty() {
		return image.Pt(-1, -1)
	}
	x := (p.X - float32(v.Min.X)) * float32(s.Dx()) / float32(v.Dx())
	y := (p.Y - float32(v.Min.Y)) * float32(s.Dy()) / float32(v.Dy())
	return image.Pt(int(math.Floor(float64(x))), int(math.Floor(float64(y)))).Add(s.Min)
}

// markFor is the drag from a to b (window px) in the shot (px), rounded
// out to whole px and clipped to the shot: a rect that covers every
// pixel the drag touched.
func (r *review) markFor(a, b f32.Point) image.Rectangle {
	v, s := r.view, r.shot.Bounds()
	if v.Empty() {
		return image.Rectangle{}
	}
	return markIn(v, s, a, b)
}

// markIn maps the drag from a to b over view (window px) into shot
// (px), rounded out and clipped.
func markIn(view, shot image.Rectangle, a, b f32.Point) image.Rectangle {
	kx := float64(shot.Dx()) / float64(view.Dx())
	ky := float64(shot.Dy()) / float64(view.Dy())
	x0, x1 := float64(min(a.X, b.X)-float32(view.Min.X))*kx, float64(max(a.X, b.X)-float32(view.Min.X))*kx
	y0, y1 := float64(min(a.Y, b.Y)-float32(view.Min.Y))*ky, float64(max(a.Y, b.Y)-float32(view.Min.Y))*ky
	m := image.Rect(int(math.Floor(x0)), int(math.Floor(y0)), int(math.Ceil(x1)), int(math.Ceil(y1))).Add(shot.Min)
	return m.Intersect(shot)
}

// toView maps m (shot px) to the window (px), by last frame's view.
func (r *review) toView(m image.Rectangle) image.Rectangle {
	v, s := r.view, r.shot.Bounds()
	if v.Empty() || s.Empty() {
		return image.Rectangle{}
	}
	f := func(x, o, sw, vw, vo int) int { return vo + int(math.Round(float64(x-o)*float64(vw)/float64(sw))) }
	return image.Rect(
		f(m.Min.X, s.Min.X, s.Dx(), v.Dx(), v.Min.X), f(m.Min.Y, s.Min.Y, s.Dy(), v.Dy(), v.Min.Y),
		f(m.Max.X, s.Min.X, s.Dx(), v.Dx(), v.Min.X), f(m.Max.Y, s.Min.Y, s.Dy(), v.Dy(), v.Min.Y))
}

// reviewView is where a shot of size s (px), k shot px per window px,
// is drawn in a size px window: centred under the banner, as big as
// fits, at most 3 times the window's own size of it and never past one
// shot px per window px.
func reviewView(size, s image.Point, k float32, top, side, bottom int) image.Rectangle {
	if k <= 0 {
		k = 1
	}
	avail := image.Pt(size.X-2*side, size.Y-top-bottom)
	if avail.X <= 0 || avail.Y <= 0 || s.X <= 0 || s.Y <= 0 {
		return image.Rectangle{}
	}
	nx, ny := float32(s.X)/k, float32(s.Y)/k // the shot at the window's density
	z := min(float32(avail.X)/nx, float32(avail.Y)/ny, k, 3)
	w, h := max(int(nx*z), 1), max(int(ny*z), 1)
	x := (size.X - w) / 2
	y := top + (avail.Y-h)/2
	return image.Rect(x, y, x+w, y+h)
}

var (
	scrimCol = color.NRGBA{R: 0x0b, G: 0x0c, B: 0x0e, A: 0xc8}
	markCol  = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	markEdge = color.NRGBA{R: 0x16, G: 0x17, B: 0x1a, A: 0xff}
	markWash = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x90}
)

// drawReview draws the review over the window: a scrim, the shot as the
// copy will show it, the marked rects framed (a rect whose blur isn't
// shown yet, and the one being drawn, washed out), and the banner.
func (t *Tool) drawReview(gtx layout.Context) {
	r := t.rev
	sz := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: sz}, scrimCol)
	r.view = reviewView(sz, r.shot.Bounds().Size(), r.c.k, gtx.Dp(52), gtx.Dp(24), gtx.Dp(24))
	one := max(gtx.Dp(1), 1)
	if r.ready && !r.view.Empty() {
		s := r.shot.Bounds()
		drawShot(gtx, r.shown, r.view, s)
		if !r.c.region {
			// the element's outline, as in the copy
			w := max(gtx.Dp(2), 2)
			if el := r.toView(r.c.elem).Intersect(r.view); !el.Empty() {
				frame(gtx, el.Inset(-w), w, outlineCol)
			}
		}
		for i, m := range r.marks {
			v := r.toView(m)
			if r.shownV != r.version && i >= r.shownMarks() {
				fill(gtx, v, markWash) // its blur is on its way
			}
			frame(gtx, v.Inset(-one), one, markEdge)
			frame(gtx, v, one, markCol)
		}
		frame(gtx, r.view.Inset(-one), one, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x40})
	}
	if r.dragging && !r.view.Empty() {
		if m := r.markFor(r.from, r.to); !m.Empty() {
			v := r.toView(m)
			fill(gtx, v, markWash)
			frame(gtx, v.Inset(-one), one, markEdge)
			frame(gtx, v, one, markCol)
		}
	}
	th := t.host.Theme()
	if th == nil || th.Shaper == nil {
		return
	}
	t.banner(gtx, th, reviewHint(r.ready, len(r.marks)))
}

// shownMarks is how many marks the shown image has blurred: all but
// the newest when a change is on its way (a removal shows at once: its
// frame is gone).
func (r *review) shownMarks() int {
	return max(len(r.marks)-1, 0)
}

// reviewHint is the review's banner.
func reviewHint(ready bool, n int) string {
	msg := "Blur: drag over anything private · ⌫ undo · Enter copy · Esc cancel"
	switch {
	case !ready:
		msg = "Blur: preparing the shot… · Enter copy · Esc cancel"
	case n == 1:
		msg += " · 1 blurred"
	case n > 1:
		msg += fmt.Sprintf(" · %d blurred", n)
	}
	return msg
}

// drawShot paints img, the shot s scaled into view (window px).
func drawShot(gtx layout.Context, img paint.ImageOp, view, s image.Rectangle) {
	if s.Empty() {
		return
	}
	defer op.Offset(view.Min).Push(gtx.Ops).Pop()
	sx, sy := float32(view.Dx())/float32(s.Dx()), float32(view.Dy())/float32(s.Dy())
	defer op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(sx, sy))).Push(gtx.Ops).Pop()
	defer clip.Rect(image.Rectangle{Max: s.Size()}).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
