package inspect

// The tool on its own, through a real Gio router: a drag past dragSlop
// grabs the region dragged out (the shot is that region, as it was, at
// ShotDensity), a short drag is still a click that picks, the review
// marks rects in the shot's pixels, takes the last one back, and Enter
// copies an image with them blurred out; Esc drops the report.

import (
	"image"
	"image/color"
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

	"github.com/some-du6e/clack/internal/ui/theme"
)

type dragHost struct {
	mu     sync.Mutex
	posted []func()
	toasts []string
	size   image.Point
}

func (h *dragHost) Theme() *theme.Theme               { return nil }
func (h *dragHost) Describe(r *Report, red *Redactor) { r.Page = red.Route("c/C0123456789") }
func (h *dragHost) ConvKind(string) string            { return "" }
func (h *dragHost) CopyText(string)                   {}
func (h *dragHost) Invalidate()                       {}
func (h *dragHost) ConfigDir() string                 { return "" }
func (h *dragHost) Toast(s string)                    { h.toasts = append(h.toasts, s) }
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
}

func newDragEnv(t *testing.T) *dragEnv {
	e := &dragEnv{t: t, size: image.Pt(900, 600)}
	e.host = &dragHost{size: e.size}
	e.tool = New(e.host, key.Filter{Name: "D", Required: key.ModShortcut | key.ModAlt | key.ModShift})
	prevShoot, prevWrite := Shoot, WriteImage
	Shoot = func(_ *op.Ops, region image.Rectangle, k float32) (*image.RGBA, error) {
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

func (e *dragEnv) frame() {
	e.host.drain()
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(e.size), Now: time.Now(), Source: e.r.Source()}
	g := e.tool.Begin(gtx)
	_ = g
	e.tool.End(gtx)
	e.r.Frame(&e.ops)
	e.tool.AfterFrame(&e.ops, e.size)
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

func (e *dragEnv) key(name key.Name, mods key.Modifiers) {
	e.r.Queue(key.Event{Name: name, Modifiers: mods, State: key.Press}, key.Event{Name: name, Modifiers: mods, State: key.Release})
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
// (here the window, the host's only area under the pointer); Esc in the
// review drops the report, nothing copied. A drag too thin grabs
// nothing, and the session goes on.
func TestClickStillPicksAndEscDrops(t *testing.T) {
	e := newDragEnv(t)
	e.tool.Toggle()
	e.frame()
	p := f32.Pt(400, 300)
	e.queue(pointer.Event{Kind: pointer.Move, Position: p})
	e.frame()
	e.frame()
	if label, _, ok := e.tool.Hovered(); !ok || label != "primary" {
		t.Fatalf("hovering %q (%v), want the primary area", label, ok)
	}
	e.press(p)
	e.dragTo(p.Add(f32.Pt(3, -2)))
	e.releaseAt(p.Add(f32.Pt(3, -2)))
	e.frame()
	if len(e.shots) != 1 {
		t.Fatalf("%d shots after the click", len(e.shots))
	}
	if want, _ := cropFor(image.Rect(200, 0, 900, 600), e.size, 1); e.shots[0] != want {
		t.Errorf("the click shot %v, want the picked area's crop %v", e.shots[0], want)
	}
	if !e.tool.Reviewing() {
		t.Fatal("no review after the pick")
	}
	e.key(key.NameEscape, 0)
	e.frame()
	if e.tool.Active() || e.copied != nil {
		t.Errorf("after Esc: active %v, copied %v", e.tool.Active(), e.copied != nil)
	}
	if n := len(e.host.toasts); n == 0 || !strings.Contains(e.host.toasts[n-1], "discarded") {
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
	if len(e.shots) != 1 || !e.tool.Active() || e.tool.Reviewing() {
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
	if len(e.shots) != 1 || e.tool.Reviewing() {
		t.Errorf("the dropped drag's release grabbed something: %d shots", len(e.shots))
	}
}
