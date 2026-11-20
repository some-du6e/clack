package inspect

// The pick-to-review path (Karim, 2026-11-18, Windows at 1918x1080:
// "this shit takes to long between a selection and the blur page"): the
// review draws its shot in the frame after the pick's, whatever the size
// of the shot, and making that image takes a fraction of blurring the
// whole shot; nothing it doesn't need first (the image's PNG, the
// clipboard) holds it. Enter pressed in an element pick's own frame copies
// the exact image the review's Enter copies; a region still opens its
// review (the only place a region is blurred). Esc in the frame a press
// is let go of drops that press, as Esc while pressing always has: nothing
// shot, the session going on.

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"
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
			e.untilShown() // how long it took after all: the line below
		}
		e.key(key.NameEscape, 0)
		e.frame()
		t.Logf("%gx: %s", sc, LastTimes())
		e.checkShownTime(sc, "a click's", shot, sc*e.ks[0])

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
			e.untilShown()
		}
		e.key(key.NameEscape, 0)
		e.frame()
		t.Logf("%gx: %s", sc, LastTimes())
	}
}

// checkShownTime fails unless the last capture's review image (its
// "review shot ready" stage: the shot copied, its margin blurred) took at
// most half of what the image cost before only the margin was blurred:
// the shot copied and blurred whole (a second and more for a 3200x2000 px
// shot on the old blur), timed here on as big a shot. Frames alone can't
// tell: the image is made before the review's first frame, however long
// it takes. (A dragged region's image is a copy, then and now: nothing
// to bound.)
func (e *dragEnv) checkShownTime(sc float32, what string, shot image.Point, scale float32) {
	e.t.Helper()
	line := LastTimes()
	shown, ok := stageTime(line, "review shot ready ")
	if !ok {
		e.t.Errorf("%gx: %s capture logged no review image time: %s", sc, what, line)
		return
	}
	src := noiseShot(shot, 2)
	rad := max(int(math.Round(float64(MarginBlur*scale))), 1)
	t0 := time.Now()
	img := image.NewRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, image.Point{}, draw.Src)
	blur(img, img.Bounds(), rad)
	whole := time.Since(t0)
	if shown > whole/2 {
		e.t.Errorf("%gx: %s %v px review image took %v, more than half of copying the shot and blurring it whole (%v): the margin alone is blurred (blurOutside)", sc, what, shot, shown, whole)
	}
}

// stageTime is the duration after prefix in a stage-time line (timing.go
// dur: "12.3ms", or a time.Duration from 10 s on).
func stageTime(line, prefix string) (time.Duration, bool) {
	i := strings.Index(line, prefix)
	if i < 0 {
		return 0, false
	}
	v := line[i+len(prefix):]
	if j := strings.IndexAny(v, ",;( "); j >= 0 {
		v = v[:j]
	}
	if ms, ok := strings.CutSuffix(v, "ms"); ok {
		f, err := strconv.ParseFloat(ms, 64)
		return time.Duration(f * float64(time.Millisecond)), err == nil
	}
	d, err := time.ParseDuration(v)
	return d, err == nil
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

// untilShown runs frames until the review draws its shot, 30 s at most.
func (e *dragEnv) untilShown() {
	for dl := time.Now().Add(30 * time.Second); !e.tool.ShotDrawn() && time.Now().Before(dl); {
		time.Sleep(2 * time.Millisecond)
		e.frame()
	}
}

// hintNow is the review's banner as it would draw now.
func (e *dragEnv) hintNow() string {
	if r := e.tool.rev; r != nil {
		return fmt.Sprintf("banner %q", reviewHint(e.tool.ShotDrawn(), len(r.marks)))
	}
	return "no review"
}

// TestEnterRightAfterPick: Enter in an element pick's own frame, and
// Enter in the frame after it, copy the image the review's Enter copies
// (the shot, its margin blurred, composed over the report the tool
// delivered), pixel for pixel, and end the session. A pick of no component
// and no element (the shell's area under the pointer: a whole column, and
// a whole-window layer: no margin to blur) is reviewed like a region
// instead (review of a614d3b3): Enter there copies nothing, and the review
// comes up.
func TestEnterRightAfterPick(t *testing.T) {
	e := newDragEnv(t)
	e.content = drawPill
	p := f32.Pt(400, 300) // on the pill
	for _, when := range []string{"in the pick's frame", "in the frame after it"} {
		e.copied = nil
		e.startAt(p, "Pill")
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
		// the stage-time line says what happened: no review was shown (in
		// the frame after the pick, the review was started but never drawn)
		if line := LastTimes(); when == "in the pick's frame" && (!strings.Contains(line, "copied at once") || strings.Contains(line, "after the review showed")) {
			t.Errorf("Enter %s: logged %q, want the capture copied at once, no review", when, line)
		}
		if line := LastTimes(); when == "in the frame after it" && (!strings.Contains(line, "before the review showed") || strings.Contains(line, "after the review showed")) {
			t.Errorf("Enter %s: logged %q, want Enter before the review showed (it was never drawn)", when, line)
		}
		if len(e.shots) != n+1 {
			t.Fatalf("Enter %s: %d shots, want one", when, len(e.shots)-n)
		}
		crop, k := e.shots[n], e.ks[n]
		shot := noiseShot(ShotSize(crop, k), 1)
		elem := inShot(image.Rect(380, 290, 420, 310), crop, k) // the pill picked
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

	// the primary area, no component: reviewed like a region
	area := f32.Pt(700, 300)
	for _, when := range []string{"in the pick's frame", "in the frame after it"} {
		e.copied = nil
		e.startAt(area, "primary")
		e.shiftClick(area, false, false)
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
		if e.copied != nil || !e.tool.Reviewing() {
			t.Errorf("Enter %s on an area with no component: copied %v, reviewing %v; want its review, nothing copied", when, e.copied != nil, e.tool.Reviewing())
		}
		if e.tool.Active() {
			e.key(key.NameEscape, 0)
			e.frame()
		}
	}
}

// TestEscDuringPreparation: Esc in the frame a press is let go of drops
// that press, as Esc while pressing or dragging always has (review of
// a8fa243c: it ended the whole session, whether it did depending on
// whether Esc and the release fell in one frame): nothing is shot, saved
// or copied, no review comes in later frames, and the session goes on, its
// picker outlining again until a second Esc ends it. The same for a drag
// let go of in Esc's frame and for one too small to grab. The next pick is
// reviewed in the frame after it, and Esc in that review drops it.
func TestEscDuringPreparation(t *testing.T) {
	e := newDragEnv(t)
	p := f32.Pt(400, 300)
	for _, how := range []string{"a click", "a drag", "a drag too small to grab"} {
		e.startAt(p, "primary")
		n := len(e.shots)
		e.press(p)
		e.frame()
		end := p
		switch how {
		case "a drag":
			end = f32.Pt(700, 500)
		case "a drag too small to grab":
			end = p.Add(f32.Pt(20, 4)) // past the slop, under minRegion one way
		}
		if end != p {
			e.dragTo(end)
			e.frame()
		}
		e.releaseAt(end)
		e.key(key.NameEscape, 0)
		e.frame()
		if !e.tool.Active() || e.tool.Reviewing() || len(e.shots) != n {
			t.Fatalf("Esc in the frame %s was let go of: session %v, reviewing %v, %d shots; want only the press dropped, the session on", how, e.tool.Active(), e.tool.Reviewing(), len(e.shots)-n)
		}
		for range 3 {
			time.Sleep(20 * time.Millisecond)
			e.frame()
		}
		if e.tool.Reviewing() || e.copied != nil || e.tool.lastRep != nil || len(e.shots) != n {
			t.Fatalf("after Esc with %s: reviewing %v, copied %v, a report delivered %v, %d shots", how, e.tool.Reviewing(), e.copied != nil, e.tool.lastRep != nil, len(e.shots)-n)
		}
		e.queue(pointer.Event{Kind: pointer.Move, Position: p.Add(f32.Pt(3, 0))})
		e.frame()
		e.frame()
		if label, _, ok := e.tool.Hovered(); !ok || label != "primary" {
			t.Errorf("after Esc with %s: the picker outlines %q (%v), want it going on over the primary area", how, label, ok)
		}
		e.key(key.NameEscape, 0)
		e.frame()
		if e.tool.Active() {
			t.Fatalf("a second Esc after %s left the session on", how)
		}
	}

	// the next session: a pick, its review in the frame after
	n := len(e.shots)
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
	if k := len(e.host.toasts); k == 0 || !strings.Contains(e.host.toasts[k-1], "discarded") {
		t.Errorf("toasts after Esc in the review %q, want the review's discarded one", e.host.toasts)
	}
}

// TestRegionEnterOpensReview: Enter or Mod+C before a dragged region's
// review has been on screen a while doesn't copy the region (reviews of
// a8fa243c, 3a75558d and a614d3b3): the review is the only place a region
// can be blurred. In the release's own frame, in the frame after it (the
// review started, not drawn yet: where a quick Enter lands, and on a
// software GPU for the seconds the shot takes) and two frames after (its
// first frame drawn, maybe still on its way to the screen), the key is
// dropped and the review stays with nothing copied; once it has been on
// screen armDelay, Enter copies. An element's pick still copies at once
// (TestEnterRightAfterPick: its margin is blurred anyway).
func TestRegionEnterOpensReview(t *testing.T) {
	e := newDragEnv(t)
	for _, k := range []struct {
		name key.Name
		mods key.Modifiers
	}{{key.NameReturn, 0}, {"C", key.ModShortcut}} {
		for _, when := range []string{"in the release's frame", "in the frame after it", "two frames after it"} {
			e.copied = nil
			e.tool.Toggle()
			e.frame()
			from, to := f32.Pt(150, 120), f32.Pt(450, 320)
			e.press(from)
			e.frame()
			e.dragTo(to)
			e.frame()
			e.releaseAt(to)
			switch when {
			case "in the frame after it":
				e.frame() // the release's: shot, its review started
			case "two frames after it":
				e.frame()
				e.frame() // the review drawn
			}
			e.key(k.name, k.mods)
			e.frame()
			e.tool.Wait()
			e.frame()
			if e.copied != nil || !e.tool.Reviewing() {
				t.Fatalf("%v%v %s: copied %v, reviewing %v; want its review, nothing copied", k.mods, k.name, when, e.copied != nil, e.tool.Reviewing())
			}
			e.wait(armDelayTest)
			e.key(key.NameReturn, 0)
			e.frame()
			e.tool.Wait()
			e.frame()
			if e.copied == nil || e.tool.Active() {
				t.Errorf("Enter in the region's review, on screen a while (%v%v %s): copied %v, session %v", k.mods, k.name, when, e.copied != nil, e.tool.Active())
			}
		}
	}
}

// regionReview drags a region and runs frames until its review has been
// drawn and a frame has been read after that one (the review on screen).
func (e *dragEnv) regionReview(t *testing.T) {
	t.Helper()
	e.tool.Toggle()
	e.frame()
	from, to := f32.Pt(150, 120), f32.Pt(450, 320)
	e.press(from)
	e.frame()
	e.dragTo(to)
	e.frame()
	e.releaseAt(to)
	e.frame() // the release's: shot, its review started
	e.frame() // the review drawn
	e.frame() // the first frame read after it went to the screen
	if !e.tool.Reviewing() || !e.tool.ShotDrawn() {
		t.Fatalf("no region review drawn: reviewing %v, drawn %v", e.tool.Reviewing(), e.tool.ShotDrawn())
	}
}

// TestRegionCopyArms: a region's review takes its copy keys only once it
// has been on screen armDelay (review of a614d3b3: Enter was armed as soon
// as a frame had recorded the review, though that frame was still on its
// way to the screen, for a long while on a software GPU): Enter 200 ms
// after its first frame went out copies nothing, Enter after armDelay
// copies.
func TestRegionCopyArms(t *testing.T) {
	e := newDragEnv(t)
	e.regionReview(t)
	e.skew += 200 * time.Millisecond
	e.key(key.NameReturn, 0)
	e.frame()
	e.tool.Wait()
	e.frame()
	if e.copied != nil || !e.tool.Reviewing() {
		t.Fatalf("Enter 200 ms after the region's review showed: copied %v, reviewing %v; want it dropped, the review up", e.copied != nil, e.tool.Reviewing())
	}
	e.wait(armDelayTest - 200*time.Millisecond)
	e.key(key.NameReturn, 0)
	e.frame()
	e.tool.Wait()
	e.frame()
	if e.copied == nil || e.tool.Active() {
		t.Errorf("Enter after the review had been on screen %v: copied %v, session %v", armDelayTest, e.copied != nil, e.tool.Active())
	}
}

// TestHeldCopyKeyRepeats: a copy key that went down before the region's
// review armed and is held (its auto-repeats: presses with no release
// between them, as Windows and macOS send them) copies nothing however
// long it is held (review of a614d3b3: the first repeat after the review
// drew copied the region); once it has come up, a press copies.
func TestHeldCopyKeyRepeats(t *testing.T) {
	e := newDragEnv(t)
	e.tool.Toggle()
	e.frame()
	from, to := f32.Pt(150, 120), f32.Pt(450, 320)
	e.press(from)
	e.frame()
	e.dragTo(to)
	e.frame()
	e.releaseAt(to)
	e.pressKey(key.NameReturn, 0) // down with the release, and held
	e.frame()
	e.frame()
	e.frame()
	e.wait(armDelayTest)
	e.pressKey(key.NameReturn, 0) // auto-repeats
	e.frame()
	e.wait(100 * time.Millisecond)
	e.pressKey(key.NameReturn, 0)
	e.frame()
	e.wait(1800 * time.Millisecond) // macOS's slowest repeat
	e.pressKey(key.NameReturn, 0)
	e.frame()
	e.tool.Wait()
	e.frame()
	if e.copied != nil || !e.tool.Reviewing() {
		t.Fatalf("a held Enter's auto-repeats: copied %v, reviewing %v; want nothing copied, the review up", e.copied != nil, e.tool.Reviewing())
	}
	e.releaseKey(key.NameReturn, 0)
	e.frame()
	e.key(key.NameReturn, 0)
	e.frame()
	e.tool.Wait()
	e.frame()
	if e.copied == nil || e.tool.Active() {
		t.Errorf("Enter after the held one came up: copied %v, session %v", e.copied != nil, e.tool.Active())
	}
}

// TestEnterMidMarkKeepsIt: Enter while a rect is still being dragged out
// in the review copies the image with that rect blurred (review of
// a8fa243c: the rect joined the marks only when the button came up, so
// what was being blurred went out sharp): the copy is what the review's
// Enter copies with the rect marked.
func TestEnterMidMarkKeepsIt(t *testing.T) {
	e := newDragEnv(t)
	e.tool.Toggle()
	e.frame()
	from, to := f32.Pt(150, 120), f32.Pt(450, 320)
	e.press(from)
	e.frame()
	e.dragTo(to)
	e.frame()
	e.releaseAt(to)
	e.frame()
	if !e.tool.Reviewing() || len(e.shots) != 1 {
		t.Fatalf("after the region: reviewing %v, %d shots", e.tool.Reviewing(), len(e.shots))
	}
	e.untilReady()
	e.wait(armDelayTest) // a region's copy keys arm once its review has been on screen a while
	v := e.tool.ReviewView()
	a, b := v.Min.Add(v.Size().Div(3)), v.Min.Add(v.Size().Mul(2).Div(3))
	pa, pb := f32.Pt(float32(a.X), float32(a.Y)), f32.Pt(float32(b.X), float32(b.Y))
	e.press(pa)
	e.frame()
	e.dragTo(pb)
	e.frame()
	if marks, _, _ := e.tool.Marks(); len(marks) != 0 {
		t.Fatalf("marks %v before the button came up", marks)
	}
	m := e.tool.rev.markFor(pa, pb)
	if m.Empty() {
		t.Fatal("the drag covers no shot px")
	}
	e.key(key.NameReturn, 0)
	e.frame()
	e.tool.Wait()
	e.frame()
	img, ok := e.copied.(*image.RGBA)
	if !ok {
		t.Fatalf("Enter mid-drag copied %T", e.copied)
	}
	shot := noiseShot(ShotSize(e.shots[0], e.ks[0]), 1)
	want, err := Compose(shot, shot.Bounds(), e.ks[0], e.tool.lastRep.Lines(), m)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != want.Bounds() || !bytes.Equal(img.Pix, want.Pix) {
		t.Errorf("Enter mid-drag: the copy (%v) isn't the image with the rect being drawn (%v in the shot) blurred", img.Bounds(), m)
	}
}

// frameUnshot is a frame whose AfterFrame doesn't run (a frame under the
// boot placeholder).
func (e *dragEnv) frameUnshot() {
	e.host.drain()
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(e.size), Now: time.Now(), Source: e.r.Source(), Ops: &e.ops}
	if ops := e.tool.Frame(gtx); ops != nil {
		e.r.Frame(ops)
		return
	}
	e.ops.Reset()
	g := e.tool.Begin(gtx)
	e.content(g)
	e.tool.End(gtx)
	e.r.Frame(&e.ops)
}

// TestGiveUpDropsCapture: a pick whose frame is never shot ends the
// session at the next frame with a toast, and drops the capture with it:
// no later frame's AfterFrame shoots it or opens a review after the
// session ended (review of a8fa243c).
func TestGiveUpDropsCapture(t *testing.T) {
	e := newDragEnv(t)
	p := f32.Pt(400, 300)
	e.startAt(p, "primary")
	n := len(e.shots)
	e.shiftClick(p, false, false)
	e.frameUnshot() // the pick's
	e.frameUnshot() // the next: given up
	if e.tool.Active() {
		t.Fatal("the session went on after its pick's frame was never shot")
	}
	if k := len(e.host.toasts); k == 0 || !strings.Contains(e.host.toasts[k-1], "wasn't drawn") {
		t.Errorf("toasts %q, want the give-up's", e.host.toasts)
	}
	e.frame()
	e.frame()
	if len(e.shots) != n || e.tool.Reviewing() || e.tool.Active() {
		t.Errorf("after the give-up: %d shots, reviewing %v, session %v; want nothing", len(e.shots)-n, e.tool.Reviewing(), e.tool.Active())
	}
}

// TestCloseLogsCapture: the tool closed with a review up (sign-out, the
// tool turned off, hide to tray) logs the capture's stage-time line, as
// every capture does whatever ends it.
func TestCloseLogsCapture(t *testing.T) {
	e := newDragEnv(t)
	p := f32.Pt(400, 300)
	e.startAt(p, "primary")
	e.shiftClick(p, false, false)
	e.frame()
	if !e.tool.Reviewing() {
		t.Fatal("no review after the click")
	}
	e.frame()
	before := LastTimes()
	e.tool.Close()
	if got := LastTimes(); got == before || !strings.Contains(got, "closed") {
		t.Errorf("Close with a review up logged %q (before it %q), want the capture's line, closed", got, before)
	}
}
