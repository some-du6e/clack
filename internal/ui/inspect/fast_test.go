package inspect

// The pick-to-review path (Karim, 2026-11-18, Windows at 1918x1080:
// "this shit takes to long between a selection and the blur page"): the
// review draws its shot in the frame after the pick's, whatever the size
// of the shot; nothing it doesn't need first (the image's PNG, the
// clipboard) holds it. Enter pressed in the pick's own frame copies the
// exact image the review's Enter copies; Esc there drops the capture
// before anything is shot, nothing comes back later, and the next
// session works as before.

import (
	"bytes"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
)

// shownWithin runs up to n frames after the pick's and reports how many it
// took the review to draw its shot (0: it never did).
func (e *dragEnv) shownWithin(n int) int {
	for i := 1; i <= n; i++ {
		e.frame()
		if e.tool.ShotDrawn() {
			return i
		}
	}
	return 0
}

// TestReviewShotNextFrame: in a 1918x1080 dp window at 1x and 2x, a plain
// click on the primary area (a crop as big as most of the window: a
// 3205x1987 px shot at 1x) and a region dragged over most of it are drawn
// for review in the frame after the pick's.
func TestReviewShotNextFrame(t *testing.T) {
	for _, sc := range []float32{1, 2} {
		size := image.Pt(int(1918*sc), int(1080*sc))
		e := newDragEnvAt(t, size, sc)
		p := f32.Pt(float32(size.X)/2, float32(size.Y)/2)

		e.startAt(p, "primary")
		e.shiftClick(p, false, false) // a plain click
		e.frame()                     // the pick's frame: shot in AfterFrame
		if len(e.shots) != 1 || !e.tool.Reviewing() {
			t.Fatalf("%gx: after the click: %d shots, reviewing %v", sc, len(e.shots), e.tool.Reviewing())
		}
		shot := ShotSize(e.shots[0], e.ks[0])
		if n := e.shownWithin(2); n == 0 {
			t.Errorf("%gx: a %v px click's shot isn't drawn for review within 2 frames of the pick (%s)", sc, shot, e.hintNow())
		}
		e.key(key.NameEscape, 0)
		e.frame()

		e.tool.Toggle()
		e.frame()
		from, to := f32.Pt(60*sc, 40*sc), f32.Pt(float32(size.X)-60*sc, float32(size.Y)-40*sc)
		e.press(from)
		e.frame()
		e.dragTo(to)
		e.frame()
		e.releaseAt(to)
		e.frame()
		if len(e.shots) != 2 || !e.tool.Reviewing() {
			t.Fatalf("%gx: after the drag: %d shots, reviewing %v", sc, len(e.shots), e.tool.Reviewing())
		}
		shot = ShotSize(e.shots[1], e.ks[1])
		if n := e.shownWithin(2); n == 0 {
			t.Errorf("%gx: a %v px region isn't drawn for review within 2 frames of the release (%s)", sc, shot, e.hintNow())
		}
		e.key(key.NameEscape, 0)
		e.frame()
		t.Logf("%gx: %s", sc, LastTimes())
	}
}

// TestFreezeReviewShotNextFrame: a plain click on a frozen frame is drawn
// for review in the frame after the pick's.
func TestFreezeReviewShotNextFrame(t *testing.T) {
	e, _, at := freezeEnv(t)
	e.freeze()
	e.press(at)
	e.releaseAt(at)
	e.frame()
	if !e.tool.Reviewing() || !e.tool.Frozen() {
		t.Fatalf("after a plain click on the frozen frame: reviewing %v, frozen %v", e.tool.Reviewing(), e.tool.Frozen())
	}
	if n := e.shownWithin(2); n == 0 {
		t.Errorf("the frozen frame's shot isn't drawn for review within 2 frames of the pick (%s)", e.hintNow())
	}
}

// hintNow is the review's banner as it would draw now.
func (e *dragEnv) hintNow() string {
	if r := e.tool.rev; r != nil {
		return fmt.Sprintf("banner %q", reviewHint(e.tool.ShotDrawn(), len(r.marks)))
	}
	return "no review"
}

// TestEnterRightAfterPick: Enter in the pick's own frame, and Enter in the
// frame after it, copy the image the review's Enter copies (the shot, its
// margin blurred, composed over the report the tool delivered), pixel for
// pixel, and end the session.
func TestEnterRightAfterPick(t *testing.T) {
	e := newDragEnv(t)
	p := f32.Pt(400, 300)
	for _, when := range []string{"in the pick's frame", "in the frame after it"} {
		e.copied = nil
		e.startAt(p, "primary")
		n := len(e.shots)
		e.shiftClick(p, false, false)
		if when == "in the pick's frame" {
			e.key(key.NameReturn, 0)
		}
		e.frame()
		if when == "in the frame after it" {
			e.key(key.NameReturn, 0)
			e.frame()
		}
		e.tool.Wait()
		e.frame()
		img, ok := e.copied.(*image.RGBA)
		if !ok {
			t.Errorf("Enter %s: copied %T, want the report's image", when, e.copied)
			if e.tool.Reviewing() {
				e.key(key.NameEscape, 0)
				e.frame()
			}
			continue
		}
		if e.tool.Active() || e.tool.Reviewing() {
			t.Errorf("Enter %s: session %v, reviewing %v after the copy", when, e.tool.Active(), e.tool.Reviewing())
		}
		if len(e.shots) != n+1 {
			t.Fatalf("Enter %s: %d shots, want one", when, len(e.shots)-n)
		}
		crop, k := e.shots[n], e.ks[n]
		shot := noiseShot(ShotSize(crop, k), 1)
		elem := inShot(image.Rect(200, 0, 900, 600), crop, k) // the primary area picked
		want, err := Compose(shot, elem, k, e.tool.lastRep.Lines())
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds() != want.Bounds() {
			t.Errorf("Enter %s: the copy is %v, want %v", when, img.Bounds(), want.Bounds())
			continue
		}
		if !bytes.Equal(img.Pix, want.Pix) {
			diff := 0
			for i := range img.Pix {
				if img.Pix[i] != want.Pix[i] {
					diff++
				}
			}
			t.Errorf("Enter %s: %d of %d bytes of the copy differ from the image the review copies", when, diff, len(img.Pix))
		}
	}
}

// TestEscDuringPreparation: Esc in the pick's own frame drops the capture
// before anything is shot: no review, nothing copied or saved, the session
// over with the review's toast, and nothing comes back in later frames.
// The next session picks and reviews as before. Esc in the frame after the
// pick drops the review the same way.
func TestEscDuringPreparation(t *testing.T) {
	e := newDragEnv(t)
	p := f32.Pt(400, 300)
	e.startAt(p, "primary")
	n := len(e.shots)
	e.shiftClick(p, false, false)
	e.key(key.NameEscape, 0)
	e.frame()
	if e.tool.Reviewing() || e.tool.Active() {
		t.Fatalf("Esc in the pick's frame: reviewing %v, session %v; want the capture dropped, the session over", e.tool.Reviewing(), e.tool.Active())
	}
	if len(e.shots) != n {
		t.Errorf("Esc in the pick's frame: %d shots taken, want none", len(e.shots)-n)
	}
	for range 5 {
		time.Sleep(20 * time.Millisecond)
		e.frame()
	}
	if e.tool.Reviewing() || e.tool.Active() || e.copied != nil || e.tool.lastRep != nil {
		t.Fatalf("after Esc in the pick's frame: reviewing %v, session %v, copied %v, a report delivered %v", e.tool.Reviewing(), e.tool.Active(), e.copied != nil, e.tool.lastRep != nil)
	}
	if k := len(e.host.toasts); k == 0 || !strings.Contains(e.host.toasts[k-1], "discarded") {
		t.Errorf("toasts after Esc in the pick's frame %q, want the review's discarded one", e.host.toasts)
	}

	// the next session: a pick, its review in the frame after
	e.startAt(p, "primary")
	e.shiftClick(p, false, false)
	e.frame()
	if !e.tool.Reviewing() || len(e.shots) != n+1 {
		t.Fatalf("the next session's click: reviewing %v, %d shots", e.tool.Reviewing(), len(e.shots)-n)
	}
	if k := e.shownWithin(2); k == 0 {
		t.Errorf("the next session's review didn't draw its shot within 2 frames (%s)", e.hintNow())
	}
	// Esc in the frame after the pick: the review dropped
	e.key(key.NameEscape, 0)
	e.frame()
	if e.tool.Active() || e.copied != nil {
		t.Errorf("Esc in the review: session %v, copied %v", e.tool.Active(), e.copied != nil)
	}
}
