package inspect

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// The review. A plain click's pick or a dragged region doesn't go to
// the clipboard at once (a Shift+click's does): its shot is shown still over the dimmed
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

	started     time.Time // startReview (the capture's stage times)
	drawnFrames int       // frames that drew the shot (ShotDrawn)
	// shownAt is the first frame read after one that drew the review (its
	// input time): the review was on screen by then. A guarded capture's
	// copy keys work from armDelay after it (Tool.armed).
	shownAt time.Time

	dragging bool
	from, to f32.Point       // the drag, window px
	view     image.Rectangle // where the shot was drawn last frame, window px
}

// minMark is the smallest drag that marks a rect, dp each way: less is
// a click.
const minMark = 4

// armDelay is how long a guarded capture's review (a region, a region's
// size) is on screen before Enter or Mod+C copies it: a reaction time, so
// it never goes out unseen (review of a614d3b3: Enter was taken as soon as
// a frame had recorded the review, still on its way to the screen, which
// a software GPU takes a long while over).
const armDelay = 300 * time.Millisecond

// armed reports that r's copy keys work: always for an element's pick (its
// margin is blurred anyway), for a guarded capture once the review has
// been on screen armDelay (the frame's input time).
func (t *Tool) armed(r *review) bool {
	if !r.c.guarded() {
		return true
	}
	return !r.shownAt.IsZero() && !t.inAt.Before(r.shownAt.Add(armDelay))
}

// startReview shows c's shot for blurring (UI goroutine, from
// AfterFrame).
// The shot as the copy shows it is made here, at once: the next frame
// draws the review with it (Karim, 2026-11-18: "this shit takes to long
// between a selection and the blur page"). Its margin's blur (paintShot,
// blurOutside) computes only the margin, over the CPUs: milliseconds for
// a 3200x2000 px shot that took the old whole-shot blur over a second.
// The rects marked afterwards are blurred off the UI goroutine (redraw).
func (t *Tool) startReview(c *capture, shot *image.RGBA) {
	t.release()
	r := &review{c: c, shot: shot, started: time.Now()}
	img := shownImage(shot, nil, c.elem, c.scale*c.k)
	r.shown, r.ready = paint.NewImageOp(img), true
	c.tm.shown = time.Since(r.started)
	t.state, t.rev = reviewing, r
	t.host.Invalidate()
}

// redraw remakes the shown image from the shot and the marks as Compose
// will draw them, off the UI goroutine; the newest version wins.
func (t *Tool) redraw(r *review) {
	v := r.version
	marks := append([]image.Rectangle(nil), r.marks...)
	shot, elem, scale, h := r.shot, r.c.elem, r.c.scale*r.c.k, t.host
	go func() {
		img := shownImage(shot, marks, elem, scale)
		h.Post(func() {
			if t.rev != r || v < r.shownV {
				return
			}
			r.shown, r.shownV, r.ready = paint.NewImageOp(img), v, true
			if r.c.tm.shown == 0 {
				r.c.tm.shown = time.Since(r.started)
			}
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
	if r := t.rev; r != nil {
		r.c.tm.outcome = "dropped"
		logTimes(r.c)
	}
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
		if !t.armed(r) {
			// a region (or a region's size) is copied only once its review
			// has been on screen armDelay: before that the key is dropped,
			// and held until it comes up, so it never goes out unseen
			t.hold(ke)
			return
		}
		// a rect still being dragged out goes with the copy: what the user
		// was blurring never goes out sharp
		r.endDrag(t.scale)
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
			if r.endDrag(t.scale) {
				t.changed(r)
			}
		}
	case pointer.Cancel:
		r.dragging = false
	}
}

// endDrag ends the rect being dragged out at r.to (scale px per dp): it is
// a mark if it's at least minMark dp each way (less was a click). It
// reports whether a mark was added.
func (r *review) endDrag(scale float32) bool {
	if !r.dragging {
		return false
	}
	r.dragging = false
	min := float32(minMark) * scale
	if abs32(r.to.X-r.from.X) < min || abs32(r.to.Y-r.from.Y) < min {
		return false
	}
	m := r.markFor(r.from, r.to)
	if m.Empty() {
		return false
	}
	r.marks = append(r.marks, m)
	return true
}

// Marks are the rects marked in the review so far (shot px), and the
// shot's size; ok false outside a review (tests).
func (t *Tool) Marks() (marks []image.Rectangle, shot image.Point, ok bool) {
	if t.state != reviewing {
		return nil, image.Point{}, false
	}
	return append([]image.Rectangle(nil), t.rev.marks...), t.rev.shot.Bounds().Size(), true
}

// ShotDrawn reports that a review is up and a frame has drawn its shot
// (tests: the review appears in the frame after the pick's).
func (t *Tool) ShotDrawn() bool {
	return t.state == reviewing && t.rev != nil && t.rev.drawnFrames > 0
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
//
// A window too small for the banner's margins to leave it half its width
// and height (the presenter's 60 dp panel in a huddle) gives the shot all
// of itself, the banner over it: the review is never empty, so a region
// there can still be reviewed and copied.
func reviewView(size, s image.Point, k float32, top, side, bottom int) image.Rectangle {
	if k <= 0 {
		k = 1
	}
	avail := image.Pt(size.X-2*side, size.Y-top-bottom)
	if avail.X < size.X/2 || avail.Y < size.Y/2 {
		top, avail = 0, size
	}
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
		if r.c.tm.drawn == 0 && !r.c.tm.pick.IsZero() {
			r.c.tm.drawn, r.c.tm.frames = time.Since(r.c.tm.pick), t.frameN-r.c.tm.pickN
		}
		r.drawnFrames++
		if r.c.guarded() && !t.armed(r) {
			// a frame when it's on screen (the next: shownAt), then when it
			// arms, for the banner's "Enter copy"
			at := time.Time{}
			if !r.shownAt.IsZero() {
				at = r.shownAt.Add(armDelay)
			}
			gtx.Execute(op.InvalidateCmd{At: at})
		}
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
	t.banner(gtx, th, reviewBanner(r.ready, len(r.marks), t.armed(r)))
}

// shownMarks is how many marks the shown image has blurred: all but
// the newest when a change is on its way (a removal shows at once: its
// frame is gone).
func (r *review) shownMarks() int {
	return max(len(r.marks)-1, 0)
}

// reviewHint is the review's banner, its copy keys armed.
func reviewHint(ready bool, n int) string { return reviewBanner(ready, n, true) }

// reviewBanner is the review's banner: "Enter copy" only once the copy
// keys work (armed).
func reviewBanner(ready bool, n int, armed bool) string {
	cp := " · Enter copy"
	if !armed {
		cp = ""
	}
	msg := "Blur: drag over anything private · ⌫ undo" + cp + " · Esc cancel"
	switch {
	case !ready:
		msg = "Blur: preparing the shot…" + cp + " · Esc cancel"
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
