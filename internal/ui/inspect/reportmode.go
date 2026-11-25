package inspect

import (
	"image"
	"image/draw"
	"log"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
)

// The report box's captures (Hyper+R, internal/ui/report_box.go): what a
// pick would report about the element under the pointer, with a shot of
// the whole window, taken the way a session takes a pick but with
// nothing drawn and no review; and the review run over that shot, on
// request, to blur parts of it before it's sent.

// reportJob is a report box capture waiting for its frames.
type reportJob struct {
	done func(rep *Report, shot *image.RGBA)
}

// StartReport takes what the report box shows: the next frame is
// recorded as a session's is, the one after reads what the pointer is
// over in it and builds the pick's report (its component, element and
// classes, the ids as tags, the experiments its code read, the window,
// the page; with nothing under the pointer, the whole window's, as P's
// grab reports it), and that frame is shot whole at the window's own px
// in its AfterFrame. done then gets the report and the shot (nil when the
// shot failed), on the UI goroutine, and the tool is off again. Nothing is
// drawn meanwhile, not even the cursor. Esc or the tool's shortcut in
// those frames drops it (done isn't called), and a pick whose frame isn't
// drawn gets its report with no shot. Seed the pointer first
// (SeedPointer). false: the tool is busy (a session, a review).
func (t *Tool) StartReport(done func(rep *Report, shot *image.RGBA)) bool {
	if t.state != off || done == nil {
		return false
	}
	t.start()
	t.report = &reportJob{done: done}
	t.host.Invalidate()
	return true
}

// Capturing reports a report box capture under way (StartReport): the
// tool draws nothing and keeps the window's cursor.
func (t *Tool) Capturing() bool {
	return t.report != nil || t.capture != nil && t.capture.report != nil
}

// reportPick makes the report box's capture from the frame just recorded
// (input, in the frame after it): t.hover's report, or with nothing under
// the pointer the whole window's, shot whole in AfterFrame.
func (t *Tool) reportPick(gtx layout.Context) {
	job := t.report
	t.report = nil
	ptr := image.Pt(int(t.ptr.X+.5), int(t.ptr.Y+.5))
	var c *capture
	if p := t.hover; p != nil {
		c = t.newCapture(gtx, p)
	} else {
		c = t.newRegionCapture(gtx, image.Rectangle{Max: t.win}, ptr)
		c.rep.Capture = "whole window"
	}
	c.report = job
	c.tm.kind, c.tm.pick, c.tm.pickN, c.tm.win = "report box", time.Now(), t.frameN, t.win
	t.release() // the report holds what it needs of the frame's record
	t.capture, t.state = c, picked
	t.host.Invalidate()
}

// shootReport is AfterFrame for a report box capture: the whole frame at
// the window's own px (a shot to send, not a pasted element: no denser,
// no margin, no outline), its colours sampled off the UI goroutine, then
// done.
func (t *Tool) shootReport(c *capture, frame op.CallOp, size image.Point) {
	crop := image.Rectangle{Max: size}
	var img *image.RGBA
	var err error
	t0 := time.Now()
	if !crop.Empty() {
		img, err = Shoot(frame, crop, 1)
	}
	t.stop() // the session's shot GPU goes now (shooter.go)
	if err != nil || img == nil {
		log.Printf("inspect: the report box's capture has no screenshot: %v", err)
		img = nil
	} else {
		log.Printf("inspect: the report box's capture: a %dx%d px shot in %v", img.Bounds().Dx(), img.Bounds().Dy(), time.Since(t0).Round(time.Millisecond))
	}
	c.elem = c.rect.Intersect(crop)
	c.ptrShot = c.ptr
	rep, job, h := c.rep, c.report, t.host
	go func() {
		if img != nil {
			rep.Colors = sampleColors(img, c.elem, c.ptrShot, 1)
		}
		h.Post(func() { job.done(rep, img) })
	}()
}

// reportUndrawn: a report box capture's frame wasn't drawn (input, the
// frame after its pick): its report goes to done with no shot.
func (t *Tool) reportUndrawn(c *capture) {
	t.capture = nil
	t.stop()
	job, rep := c.report, c.rep
	t.host.Post(func() { job.done(rep, nil) })
}

// ReviewShot shows shot (the report box's screenshot of the window, scale
// px per dp) in the inspect review, as a dragged region's is: rects
// dragged over it are blurred out of it (Redact: nothing of what was under
// them is kept), P draws on it, ⌫ takes the last rect back. Enter (once
// it has been on screen a moment) gives done a copy of the shot with the
// rects and lines burnt in, and how many rects were blurred; Esc or the
// tool's shortcut gives done nil. done runs on the UI goroutine. false:
// the tool is busy.
func (t *Tool) ReviewShot(shot *image.RGBA, scale float32, done func(img *image.RGBA, blurred int)) bool {
	if t.state != off || shot == nil || shot.Bounds().Empty() || done == nil {
		return false
	}
	if scale <= 0 {
		scale = 1
	}
	b := shot.Bounds()
	c := &capture{rep: &Report{When: time.Now(), OS: goos(), Capture: "whole window"}, rect: b, scale: scale,
		region: true, review: true, k: 1, elem: b, blurDone: done}
	c.tm = capTimes{kind: "report box screenshot", scale: scale}
	t.startReview(c, shot)
	return true
}

// finishShotReview is the review's Enter for ReviewShot: the shot with
// the rects blurred out and the lines burnt in, made off the UI
// goroutine, to done.
func (t *Tool) finishShotReview(r *review) {
	marks := append([]image.Rectangle(nil), r.marks...)
	strokes := append([]Stroke(nil), r.pen.strokes...)
	shot, scale, done, h := r.shot, r.c.scale*r.c.k, r.c.blurDone, t.host
	t.stop()
	go func() {
		img := Redacted(shot, marks, scale)
		if img == shot {
			img = image.NewRGBA(shot.Bounds())
			draw.Draw(img, img.Bounds(), shot, shot.Bounds().Min, draw.Src)
		}
		if len(strokes) > 0 {
			burnStrokes(img, img.Bounds().Min, shot.Bounds(), strokes)
		}
		h.Post(func() { done(img, len(marks)) })
	}()
}

// dropShotReview is the review's Esc for ReviewShot: done gets nil.
func (t *Tool) dropShotReview(r *review) {
	done := r.c.blurDone
	t.stop()
	done(nil, 0)
}

// reportBanner is the review's banner for ReviewShot: Enter keeps the
// blurred shot for the report rather than copying it.
func reportBanner(msg string) string {
	return strings.Replace(msg, "Enter copy", "Enter done", 1)
}
