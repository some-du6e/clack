package inspect

// The tool on its own, through a real Gio router: a plain click, or a
// drag past dragSlop (which grabs the region dragged out: the shot is
// that region, as it was, at ShotDensity), opens the review first; a
// Shift+click copies the element's report at once. A short drag is still a
// click that picks, the review marks rects in the shot's pixels, takes
// the last one back, and Enter copies an image with them blurred out;
// Esc drops the report.

import (
	"image"
	"image/color"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

type dragHost struct {
	mu     sync.Mutex
	posted []func()
	toasts []string
	size   image.Point
	frozen []bool // Frozen's calls
	reset  bool   // a freeze ended: the next frame resets the pointers
}

func (h *dragHost) Theme() *theme.Theme { return nil }
func (h *dragHost) Describer() func(*Report, *Redactor) {
	return func(r *Report, red *Redactor) { r.Page = red.Route("c/C0123456789") }
}
func (h *dragHost) ConvKind(string) string { return "" }
func (h *dragHost) Invalidate()            {}
func (h *dragHost) ConfigDir() string      { return "" }
func (h *dragHost) Toast(s string)         { h.toasts = append(h.toasts, s) }
func (h *dragHost) Frozen(on bool) {
	h.frozen = append(h.frozen, on)
	h.reset = h.reset || !on
}
func (h *dragHost) Regions() []Region {
	return []Region{{Name: "window", Rect: image.Rectangle{Max: h.size}}, {Name: "sidebar", Rect: image.Rect(0, 0, 200, h.size.Y)}, {Name: "primary", Rect: image.Rect(200, 0, h.size.X, h.size.Y)}}
}
func (h *dragHost) Post(f func()) {
	h.mu.Lock()
	h.posted = append(h.posted, f)
	h.mu.Unlock()
}

func (h *dragHost) drain() {
	h.mu.Lock()
	fs := h.posted
	h.posted = nil
	h.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

// noiseShot is a shot whose every pixel differs from its neighbours
// (what a shot of text looks like at this scale, and worse).
func noiseShot(size image.Point, seed int) *image.RGBA {
	img := image.NewRGBA(image.Rectangle{Max: size})
	for y := 0; y < size.Y; y++ {
		for x := 0; x < size.X; x++ {
			v := uint8((x*37 + y*91 + seed*13 + (x*y)%7*29) % 256)
			img.SetRGBA(x, y, color.RGBA{v, 255 - v, v / 2, 0xff})
		}
	}
	return img
}

type dragEnv struct {
	t      *testing.T
	r      input.Router
	ops    op.Ops
	tool   *Tool
	host   *dragHost
	size   image.Point
	shots  []image.Rectangle
	ks     []float32
	copied image.Image
	// content draws the window under the tool (none: an empty window)
	content func(gtx layout.Context)
	// frozen: the last frame was drawn from the frozen frame (Frame)
	frozen bool
	// scale is the window's px per dp (0: 1)
	scale float32
	// onClip hears each text the window's clipboard took in a frame
	onClip func(text string)
	// skew puts the frames' clock ahead of time.Now (wait): the review
	// arms a region's copy keys by frame time
	skew time.Duration
}

func newDragEnv(t *testing.T) *dragEnv { return newDragEnvAt(t, image.Pt(900, 600), 1) }

// newDragEnvAt is newDragEnv for a window size px at scale px per dp.
func newDragEnvAt(t *testing.T, size image.Point, scale float32) *dragEnv {
	e := &dragEnv{t: t, size: size, scale: scale, content: func(layout.Context) {}}
	e.host = &dragHost{size: e.size}
	e.tool = New(e.host, key.Filter{Name: "D", Required: key.ModShortcut | key.ModAlt | key.ModShift})
	prevShoot, prevWrite := Shoot, WriteImage
	Shoot = func(_ op.CallOp, region image.Rectangle, k float32) (*image.RGBA, error) {
		e.shots, e.ks = append(e.shots, region), append(e.ks, k)
		return noiseShot(ShotSize(region, k), 1), nil
	}
	WriteImage = func(img image.Image) error { e.copied = img; return nil }
	t.Cleanup(func() { Shoot, WriteImage = prevShoot, prevWrite })
	for range 3 {
		e.frame()
	}
	return e
}

// frame is a window frame as internal/ui's: the tool's Frame first,
// before the ops are reset (a frozen frame is drawn from them), then the
// pointer reset a freeze's end asked for, then the layout.
func (e *dragEnv) frame() {
	e.host.drain()
	sc := e.scale
	if sc <= 0 {
		sc = 1
	}
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: sc, PxPerSp: sc}, Constraints: layout.Exact(e.size), Now: time.Now().Add(e.skew), Source: e.r.Source(), Ops: &e.ops}
	ops := e.tool.Frame(gtx)
	if e.host.reset {
		e.host.reset = false
		gtx.Source.ResetPointers()
	}
	e.frozen = ops != nil
	if ops != nil {
		e.r.Frame(ops)
		e.clip()
		e.tool.AfterFrame(ops, e.size)
		return
	}
	e.ops.Reset()
	gtx.Ops = &e.ops
	g := e.tool.Begin(gtx)
	e.content(g)
	e.tool.End(gtx)
	e.r.Frame(&e.ops)
	e.clip()
	e.tool.AfterFrame(&e.ops, e.size)
}

// clip is the window's clipboard taking the frame's text write, if any.
func (e *dragEnv) clip() {
	if _, b, ok := e.r.WriteClipboard(); ok && e.onClip != nil {
		e.onClip(string(b))
	}
}

func (e *dragEnv) queue(evs ...pointer.Event) {
	for i := range evs {
		evs[i].Source = pointer.Mouse
	}
	for _, ev := range evs {
		e.r.Queue(ev)
	}
}

func (e *dragEnv) press(p f32.Point) {
	e.queue(pointer.Event{Kind: pointer.Move, Position: p}, pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: p})
}
func (e *dragEnv) dragTo(p f32.Point) {
	e.queue(pointer.Event{Kind: pointer.Move, Buttons: pointer.ButtonPrimary, Position: p})
}
func (e *dragEnv) releaseAt(p f32.Point) { e.queue(pointer.Event{Kind: pointer.Release, Position: p}) }

// shiftClick clicks p with Shift held at the press (pressShift) and at
// the release (releaseShift).
func (e *dragEnv) shiftClick(p f32.Point, pressShift, releaseShift bool) {
	mod := func(on bool) key.Modifiers {
		if on {
			return key.ModShift
		}
		return 0
	}
	e.queue(pointer.Event{Kind: pointer.Move, Position: p, Modifiers: mod(pressShift)},
		pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: p, Modifiers: mod(pressShift)},
		pointer.Event{Kind: pointer.Release, Position: p, Modifiers: mod(releaseShift)})
}

// startAt starts a session with the pointer at p and checks it outlines
// the host's area want there.
func (e *dragEnv) startAt(p f32.Point, want string) {
	e.t.Helper()
	e.tool.Toggle()
	e.frame()
	e.queue(pointer.Event{Kind: pointer.Move, Position: p})
	e.frame()
	e.frame()
	if label, _, ok := e.tool.Hovered(); !ok || label != want {
		e.t.Fatalf("hovering %q (%v), want the %s area", label, ok, want)
	}
}

func (e *dragEnv) key(name key.Name, mods key.Modifiers) {
	e.r.Queue(key.Event{Name: name, Modifiers: mods, State: key.Press}, key.Event{Name: name, Modifiers: mods, State: key.Release})
}

// pressKey is a key going down, or one of a held key's auto-repeats
// (Windows' and macOS' come as presses with no release between them);
// releaseKey is it coming up.
func (e *dragEnv) pressKey(name key.Name, mods key.Modifiers) {
	e.r.Queue(key.Event{Name: name, Modifiers: mods, State: key.Press})
}
func (e *dragEnv) releaseKey(name key.Name, mods key.Modifiers) {
	e.r.Queue(key.Event{Name: name, Modifiers: mods, State: key.Release})
}

// armDelayTest is more than a region's review waits on screen before its
// copy keys work (review.go armDelay).
const armDelayTest = 400 * time.Millisecond

// wait is d passing with no input: a frame now, as the window's next
// comes at once after one that drew a review (drawReview asks for it, and
// it finds the review on screen: shownAt), then the frames' clock d ahead
// and a frame.
func (e *dragEnv) wait(d time.Duration) {
	e.frame()
	e.skew += d
	e.frame()
}

// drawPill is a 40x20 px component at (380,290) for the content to draw (a
// pick there is an element's, its margin blurred)
func drawPill(gtx layout.Context) {
	defer op.Offset(image.Pt(380, 290)).Push(gtx.Ops).Pop()
	slackc.InspectAs("Pill").End(gtx, image.Pt(40, 20))
}

// untilReady runs frames until the review shows its shot.
func (e *dragEnv) untilReady() {
	e.t.Helper()
	dl := time.Now().Add(10 * time.Second)
	for e.tool.rev == nil || !e.tool.rev.ready || e.tool.rev.shownV != e.tool.rev.version {
		if time.Now().After(dl) {
			e.t.Fatal("the review's shot never got ready")
		}
		e.frame()
		time.Sleep(2 * time.Millisecond)
	}
	e.frame()
}

func TestDragGrabsRegion(t *testing.T) {
	e := newDragEnv(t)
	e.tool.Toggle()
	e.frame()
	e.frame()

	from, to := f32.Pt(150, 120), f32.Pt(450, 320)
	e.press(from)
	e.frame()
	e.dragTo(from.Add(f32.Pt(2, 2))) // inside the slop: still a click
	e.frame()
	if _, ok := e.tool.Dragging(); ok {
		t.Fatal("a 2 px move started a drag")
	}
	e.dragTo(f32.Pt(300, 200))
	e.frame()
	if r, ok := e.tool.Dragging(); !ok || r != image.Rect(150, 120, 300, 200) {
		t.Fatalf("mid-drag: region %v (%v), want 150,120-300,200", r, ok)
	}
	e.dragTo(to)
	e.releaseAt(to)
	e.frame()
	want := image.Rect(150, 120, 450, 320)
	if len(e.shots) != 1 || e.shots[0] != want || e.ks[0] != ShotDensity {
		t.Fatalf("shots %v at %v, want the region %v at %d px per px", e.shots, e.ks, want, ShotDensity)
	}
	if !e.tool.Reviewing() {
		t.Fatal("no review after the region was shot")
	}
	if e.copied != nil {
		t.Fatal("copied before the review finished")
	}
	e.untilReady()

	// the review: two rects, the last taken back, another one
	v := e.tool.ReviewView()
	if v.Empty() {
		t.Fatal("the review shows no shot")
	}
	shotSz := ShotSize(want, ShotDensity)
	mark := func(a, b f32.Point) {
		e.press(a)
		e.frame()
		e.dragTo(a.Add(b).Mul(.5))
		e.frame()
		e.dragTo(b)
		e.releaseAt(b)
		e.frame()
	}
	pt := func(fx, fy float32) f32.Point {
		return f32.Pt(float32(v.Min.X)+fx*float32(v.Dx()), float32(v.Min.Y)+fy*float32(v.Dy()))
	}
	a, b := pt(.1, .2), pt(.4, .6)
	mark(a, b)
	mark(pt(.6, .6), pt(.9, .9))
	marks, sz, _ := e.tool.Marks()
	if len(marks) != 2 || sz != shotSz {
		t.Fatalf("marks %v in a %v shot, want 2 in %v", marks, sz, shotSz)
	}
	if got, want := marks[0], markIn(v, image.Rectangle{Max: shotSz}, a, b); got != want {
		t.Errorf("the first rect is %v in the shot, want %v", got, want)
	}
	e.key(key.NameDeleteBackward, 0)
	e.frame()
	if marks, _, _ = e.tool.Marks(); len(marks) != 1 {
		t.Fatalf("after ⌫: %d marks, want 1", len(marks))
	}
	// a click is no rect
	e.press(pt(.5, .5))
	e.releaseAt(pt(.5, .5))
	e.frame()
	if marks, _, _ = e.tool.Marks(); len(marks) != 1 {
		t.Fatalf("a click in the review marked a rect: %v", marks)
	}
	e.untilReady()
	m := marks[0]
	e.wait(armDelayTest) // a region's copy keys arm once its review has been on screen a while
	e.key(key.NameReturn, 0)
	e.frame()
	e.tool.Wait()
	if e.tool.Active() {
		t.Error("the session is still running after Enter")
	}
	img, ok := e.copied.(*image.RGBA)
	if !ok {
		t.Fatalf("copied %T", e.copied)
	}
	// the shot is at its own size (wider than the panel's minimum), at
	// the top: outside the rect it's the shot as it was (a region: no
	// blur, no outline), inside it holds none of the shot's noise
	shot := noiseShot(shotSz, 1)
	at := image.Pt((img.Bounds().Dx()-shotSz.X)/2, 0)
	diff, sharp := 0, 0
	for y := 0; y < shotSz.Y; y += 3 {
		for x := 0; x < shotSz.X; x += 3 {
			p := image.Pt(x, y)
			got := img.RGBAAt(x+at.X, y+at.Y)
			switch {
			case p.In(m):
				if p.In(m.Inset(2)) && x+1 < m.Max.X {
					n := img.RGBAAt(x+1+at.X, y+at.Y)
					if absI(int(got.R)-int(n.R)) > 6 {
						sharp++
					}
				}
			case got != shot.RGBAAt(x, y):
				diff++
			}
		}
	}
	if diff > 0 {
		t.Errorf("%d px outside the rect differ from the region's shot", diff)
	}
	if sharp > 0 {
		t.Errorf("%d px inside the rect still differ from their neighbour: not blurred out", sharp)
	}
	txt := reportText(t, e)
	rows := map[string]string{}
	for _, l := range strings.Split(txt, "\n") {
		if k, v, ok := strings.Cut(l, "  "); ok {
			rows[k] = strings.TrimSpace(v)
		}
	}
	for k, want := range map[string]string{"capture": "dragged region", "blurred": "1 rect blurred out of the image by the reporter",
		"region": "sidebar, primary", "rect": "x 150 y 120 w 300 h 200 dp"} {
		if rows[k] != want {
			t.Errorf("report row %s = %q, want %q", k, rows[k], want)
		}
	}
	if _, ok := rows["component"]; ok || strings.Contains(txt, "C0123456789") {
		t.Errorf("the region's report names a component or holds the channel id:\n%s", txt)
	}
}

// reportText is the last report's text, composed again from what the
// tool kept (the env saves nothing).
func reportText(t *testing.T, e *dragEnv) string {
	t.Helper()
	if e.tool.lastRep == nil {
		t.Fatal("no report delivered")
	}
	return e.tool.lastRep.Text()
}

func absI(v int) int { return max(v, -v) }

// A press that moves less than dragSlop is still a click: it picks
// (here the primary area, the host's innermost area under the pointer)
// and opens the review, where Esc drops the report, nothing copied. A
// Shift+click copies at once instead, the session over, with no review.
// A drag too thin grabs nothing, and the session goes on.
func TestClickStillPicksAndEscDrops(t *testing.T) {
	e := newDragEnv(t)
	p := f32.Pt(400, 300)
	crop, _ := cropFor(image.Rect(200, 0, 900, 600), e.size, 1)
	e.startAt(p, "primary")
	e.press(p)
	e.dragTo(p.Add(f32.Pt(3, -2)))
	e.releaseAt(p.Add(f32.Pt(3, -2)))
	e.frame()
	if len(e.shots) != 1 {
		t.Fatalf("%d shots after the click", len(e.shots))
	}
	if e.shots[0] != crop {
		t.Errorf("the click shot %v, want the picked area's crop %v", e.shots[0], crop)
	}
	if !e.tool.Reviewing() {
		t.Fatal("no review after a plain click")
	}
	if e.copied != nil {
		t.Fatal("a plain click copied before its review")
	}
	e.key(key.NameEscape, 0)
	e.frame()
	if e.tool.Active() || e.copied != nil {
		t.Errorf("after Esc: active %v, copied %v", e.tool.Active(), e.copied != nil)
	}
	if n := len(e.host.toasts); n == 0 || !strings.Contains(e.host.toasts[n-1], "discarded") {
		t.Errorf("toasts %q", e.host.toasts)
	}

	// Shift+click: copied at once, the session over, no review
	e.startAt(p, "primary")
	e.shiftClick(p, true, true)
	e.frame()
	if len(e.shots) != 2 || e.shots[1] != crop {
		t.Fatalf("shots after the Shift+click %v, want a second %v", e.shots, crop)
	}
	if e.tool.Reviewing() || e.tool.Active() {
		t.Fatalf("after a Shift+click: reviewing %v, active %v; want the report copied at once, the session over", e.tool.Reviewing(), e.tool.Active())
	}
	e.tool.Wait()
	e.frame()
	if e.copied == nil {
		t.Fatal("a Shift+click copied nothing")
	}
	if n := len(e.host.toasts); n == 0 || !strings.Contains(e.host.toasts[n-1], "Copied the inspect report") {
		t.Errorf("toasts %q", e.host.toasts)
	}

	// a thin drag grabs nothing; Esc mid-drag drops only the drag
	e.tool.Toggle()
	e.frame()
	e.press(f32.Pt(100, 100))
	e.frame()
	e.dragTo(f32.Pt(300, 103))
	e.releaseAt(f32.Pt(300, 103))
	e.frame()
	if len(e.shots) != 2 || !e.tool.Active() || e.tool.Reviewing() {
		t.Errorf("a 3 px tall drag: %d shots, active %v, reviewing %v; want nothing grabbed, the session on", len(e.shots), e.tool.Active(), e.tool.Reviewing())
	}
	e.press(f32.Pt(100, 100))
	e.frame()
	e.dragTo(f32.Pt(300, 300))
	e.frame()
	e.key(key.NameEscape, 0)
	e.frame()
	if _, ok := e.tool.Dragging(); ok || !e.tool.Active() {
		t.Errorf("Esc mid-drag: dragging %v, active %v; want the drag gone, the session on", ok, e.tool.Active())
	}
	e.releaseAt(f32.Pt(300, 300))
	e.frame()
	if len(e.shots) != 2 || e.tool.Reviewing() {
		t.Errorf("the dropped drag's release grabbed something: %d shots", len(e.shots))
	}
}

// Which capture opens the review: a plain click and a drag always do
// (a drag Shift or not), and a Shift+click never does, whether Shift was
// held at the press or only at the release: it copies at once.
func TestCaptureOpensReview(t *testing.T) {
	e := newDragEnv(t)
	p := f32.Pt(100, 300)
	for _, c := range []struct {
		name   string
		do     func()
		review bool
	}{
		{"a plain click", func() { e.shiftClick(p, false, false) }, true},
		{"a Shift+click", func() { e.shiftClick(p, true, true) }, false},
		{"Shift at the press only", func() { e.shiftClick(p, true, false) }, false},
		{"Shift at the release only", func() { e.shiftClick(p, false, true) }, false},
		{"a drag", func() {
			e.press(p)
			e.frame()
			e.dragTo(p.Add(f32.Pt(60, 40)))
			e.frame()
			e.releaseAt(p.Add(f32.Pt(60, 40)))
		}, true},
		{"a Shift+drag", func() { // Shift copies a pick at once, never a region
			to := p.Add(f32.Pt(60, 40))
			e.queue(pointer.Event{Kind: pointer.Move, Position: p, Modifiers: key.ModShift},
				pointer.Event{Kind: pointer.Press, Buttons: pointer.ButtonPrimary, Position: p, Modifiers: key.ModShift})
			e.frame()
			e.queue(pointer.Event{Kind: pointer.Move, Buttons: pointer.ButtonPrimary, Position: to, Modifiers: key.ModShift})
			e.frame()
			e.queue(pointer.Event{Kind: pointer.Release, Position: to, Modifiers: key.ModShift})
		}, true},
	} {
		n := len(e.shots)
		e.startAt(p, "sidebar")
		c.do()
		e.frame()
		if len(e.shots) != n+1 {
			t.Fatalf("%s: %d shots, want %d", c.name, len(e.shots), n+1)
		}
		if got := e.tool.Reviewing(); got != c.review {
			t.Errorf("%s: reviewing %v, want %v", c.name, got, c.review)
		}
		if got := e.tool.Active(); got != c.review {
			t.Errorf("%s: session running %v, want %v", c.name, got, c.review)
		}
		if e.tool.Reviewing() {
			e.key(key.NameEscape, 0)
			e.frame()
		}
		e.tool.Wait()
		e.frame()
	}
}

// A Shift+click delivers what a plain click's review copies when no rect
// is marked (Mod+C here): the same crop, the same shot in the image, the
// same report but for its time and its tags' hashes (salted per
// report); no capture or blurred row.
func TestShiftClickSameAsReviewed(t *testing.T) {
	e := newDragEnv(t)
	p := f32.Pt(100, 300)
	e.startAt(p, "sidebar")
	e.shiftClick(p, true, true)
	e.frame()
	if e.tool.Reviewing() {
		t.Fatal("a Shift+click opened the review")
	}
	e.tool.Wait()
	e.frame()
	plain, ok := e.copied.(*image.RGBA)
	if !ok {
		t.Fatalf("a Shift+click copied %T", e.copied)
	}
	plainRep := e.tool.lastRep

	e.copied = nil
	e.startAt(p, "sidebar")
	e.shiftClick(p, false, false)
	e.frame()
	if !e.tool.Reviewing() {
		t.Fatal("no review after the plain click")
	}
	e.untilReady()
	e.wait(armDelayTest) // a region's copy keys arm once its review has been on screen a while
	e.key("C", key.ModShortcut)
	e.frame()
	e.tool.Wait()
	e.frame()
	reviewed, ok := e.copied.(*image.RGBA)
	if !ok {
		t.Fatalf("Mod+C in the review copied %T", e.copied)
	}
	if e.tool.Active() {
		t.Error("the session is still running after Mod+C")
	}

	if len(e.shots) != 2 || e.shots[0] != e.shots[1] || e.ks[0] != e.ks[1] {
		t.Fatalf("shots %v at %v, want the same crop twice", e.shots, e.ks)
	}
	if want, _ := cropFor(image.Rect(0, 0, 200, 600), e.size, 1); e.shots[0] != want {
		t.Errorf("the click shot %v, want the sidebar's crop %v", e.shots[0], want)
	}
	if plain.Bounds() != reviewed.Bounds() {
		t.Fatalf("the images are %v and %v", plain.Bounds(), reviewed.Bounds())
	}
	shotSz := ShotSize(e.shots[0], e.ks[0])
	at := image.Pt((plain.Bounds().Dx()-shotSz.X)/2, 0)
	diff := 0
	for y := 0; y < shotSz.Y; y += 2 {
		for x := 0; x < shotSz.X; x += 2 {
			if plain.RGBAAt(x+at.X, y+at.Y) != reviewed.RGBAAt(x+at.X, y+at.Y) {
				diff++
			}
		}
	}
	if diff > 0 {
		t.Errorf("%d px of the shot differ between the Shift+click's image and the reviewed one", diff)
	}
	rows := func(r *Report) map[string]string {
		m := map[string]string{}
		for _, l := range r.Lines() {
			m[l.Key] = tagHash.ReplaceAllString(l.Value, ":*")
		}
		delete(m, "clack") // the time
		return m
	}
	a, b := rows(plainRep), rows(e.tool.lastRep)
	if len(a) != len(b) {
		t.Errorf("the Shift+click's report has %d rows, the reviewed one %d:\n%s\n---\n%s", len(a), len(b), plainRep.Text(), e.tool.lastRep.Text())
	}
	for k, v := range a {
		if b[k] != v {
			t.Errorf("report row %s: Shift+click %q, reviewed %q", k, v, b[k])
		}
	}
	for _, k := range []string{"capture", "blurred"} {
		if _, ok := a[k]; ok {
			t.Errorf("a Shift+click's report has a %s row: %q", k, a[k])
		}
	}
	if a["region"] != "sidebar" || a["colors"] == "" {
		t.Errorf("the Shift+click's report: region %q, colors %q", a["region"], a["colors"])
	}
}

// tagHash is a tag's hash (ch:3fa2b1), which a report's salt makes its own.
var tagHash = regexp.MustCompile(`:[0-9a-f]{6}\b`)
