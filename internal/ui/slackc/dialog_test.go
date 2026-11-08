package slackc

import (
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// dialogEnv draws a native button filling a w×h window with the
// document.body overlay over it, as the ui root does.
type dialogEnv struct {
	t      *testing.T
	r      input.Router
	btn    widget.Clickable
	clicks int
	w, h   int
}

func newDialogEnv(t *testing.T, body string) *dialogEnv {
	t.Helper()
	e := &dialogEnv{t: t, w: 1000, h: 800}
	windowSize = image.Pt(e.w, e.h)
	deliverOverlay(OverlayBody, mustTree(t, body))
	t.Cleanup(func() { deliverOverlay(OverlayBody, nil) })
	e.frame()
	return e
}

func (e *dialogEnv) frame() {
	gtx := testGtx(e.w, e.h)
	gtx.Source = e.r.Source()
	for e.btn.Clicked(gtx) {
		e.clicks++
	}
	e.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Max} })
	LayoutOverlays(gtx)
	e.r.Frame(gtx.Ops)
}

// click reports how many clicks at p reached the native button.
func (e *dialogEnv) click(p f32.Point) int {
	before := e.clicks
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: 10 * time.Millisecond},
		pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: 20 * time.Millisecond},
		pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: 30 * time.Millisecond},
	)
	e.frame()
	e.frame()
	return e.clicks - before
}

func (e *dialogEnv) box(k int64) *box {
	e.t.Helper()
	overlays.Lock()
	v := overlays.views[OverlayBody]
	overlays.Unlock()
	if v == nil {
		e.t.Fatal("no body overlay view")
	}
	return v.byK[k]
}

func within1(a, b float32) bool { return math.Abs(float64(a-b)) <= 1 }

// HaikuWarning's dialog: document.createElement('dialog') in
// document.body, its own width/padding/border and a ::backdrop colour,
// then showModal(). Like a browser, Clack centres it in the window over a
// backdrop that keeps clicks from the page under it.
func TestModalDialogCentredOverBackdrop(t *testing.T) {
	SetStyle("test:dialog", `.slick-haiku-dialog { width: 520px; max-width: calc(100vw - 32px); box-sizing: border-box;
		padding: 24px; border: 1px solid #666; border-radius: 12px; background: #fff; color: #1d1c1d; }
		.slick-haiku-dialog::backdrop { background: #0008; }
		dialog:modal { outline: 2px solid red; }`)
	defer RemoveStyle("test:dialog")
	e := newDialogEnv(t, `[{"t":"div","k":1,"c":["text before the dialog"]},
		{"t":"dialog","k":2,"p":{"className":"slick-haiku-dialog","open":""},"modal":true,"c":[
			{"t":"h2","k":3,"c":["Found a haiku in your message!"]},
			{"t":"button","k":4,"p":{"type":"button"},"c":["Keep editing"]}]}]`)
	d := e.box(2)
	if d == nil {
		t.Fatal("the dialog wasn't laid out")
	}
	if !d.topLayer || d.st.Position != css.PosFixed {
		t.Errorf("modal dialog: top layer %v, position %v; want the top layer, fixed", d.topLayer, d.st.Position)
	}
	if !within1(d.w, 520) {
		t.Errorf("width %v, want 520", d.w)
	}
	if !within1(d.x, float32(e.w)/2-d.w/2) || !within1(d.y, float32(e.h)/2-d.h/2) {
		t.Errorf("dialog at (%v, %v) %vx%v; want centred in the %dx%d window", d.x, d.y, d.w, d.h, e.w, e.h)
	}
	if d.h <= 48 || d.h > float32(e.h) {
		t.Errorf("height %v: not sized to its content", d.h)
	}
	if d.st.Outline.Width == 0 {
		t.Error("dialog:modal didn't match")
	}
	if d.backdrop == nil {
		t.Fatal("no ::backdrop")
	}
	if c := d.backdrop.st.Background.Color; c != (color.NRGBA{A: 0x88}) {
		t.Errorf("::backdrop background %v, want the plugin's #0008", c)
	}
	// the page under the backdrop is inert; the dialog's own button works
	if n := e.click(f32.Pt(20, 20)); n != 0 {
		t.Errorf("a click on the backdrop reached the page under it (%d clicks)", n)
	}
	if n := e.click(f32.Pt(float32(e.w)-5, float32(e.h)-5)); n != 0 {
		t.Errorf("a click in the backdrop's corner reached the page under it (%d clicks)", n)
	}
}

// Without author CSS a modal dialog gets the UA style: content-sized,
// 1em padding, a medium border, centred both ways, a 10% black backdrop.
func TestModalDialogUADefaults(t *testing.T) {
	e := newDialogEnv(t, `[{"t":"dialog","k":2,"p":{"open":""},"modal":true,"c":[{"t":"p","k":3,"c":["hello"]}]}]`)
	d := e.box(2)
	if d == nil {
		t.Fatal("the dialog wasn't laid out")
	}
	if d.w >= float32(e.w)/2 {
		t.Errorf("width %v: a modal dialog is sized to its content (fit-content)", d.w)
	}
	if d.bd[0] != 3 || d.pad[0] != d.st.FontSize {
		t.Errorf("border %v padding %v; want 3px (medium) and 1em", d.bd, d.pad)
	}
	if !within1(d.x, float32(e.w)/2-d.w/2) || !within1(d.y, float32(e.h)/2-d.h/2) {
		t.Errorf("dialog at (%v, %v) %vx%v; want centred in the window", d.x, d.y, d.w, d.h)
	}
	if d.backdrop == nil || d.backdrop.st.Background.Color != (color.NRGBA{A: 0x1a}) {
		t.Errorf("::backdrop %+v, want the UA's rgba(0,0,0,.1)", d.backdrop)
	}
	if n := e.click(f32.Pt(20, 20)); n != 0 {
		t.Errorf("a click on the backdrop reached the page under it (%d clicks)", n)
	}
}

// show() (no "modal"): absolutely positioned at its place in the body,
// centred across it, no backdrop, the page stays clickable. A closed
// dialog isn't rendered.
func TestNonModalAndClosedDialog(t *testing.T) {
	e := newDialogEnv(t, `[{"t":"div","k":1,"p":{"style":{"height":"100px"}}},
		{"t":"dialog","k":2,"p":{"open":"","style":{"width":"200px"}},"c":["non-modal"]},
		{"t":"dialog","k":5,"c":["closed"]}]`)
	d := e.box(2)
	if d == nil {
		t.Fatal("the dialog wasn't laid out")
	}
	if d.topLayer || d.backdrop != nil || d.st.Position != css.PosAbsolute {
		t.Errorf("non-modal dialog: top layer %v, backdrop %v, position %v; want absolute in place", d.topLayer, d.backdrop != nil, d.st.Position)
	}
	if !within1(d.x, float32(e.w)/2-d.w/2) || !within1(d.y, 100) {
		t.Errorf("non-modal dialog at (%v, %v) %vx%v; want centred across the body at y=100", d.x, d.y, d.w, d.h)
	}
	if closed := e.box(5); closed != nil && closed.st.Display != css.DisplayNone {
		t.Errorf("a closed dialog is displayed (%v)", closed.st.Display)
	}
	if n := e.click(f32.Pt(20, 600)); n != 1 {
		t.Errorf("a click beside a non-modal dialog: %d clicks, want 1", n)
	}
}
