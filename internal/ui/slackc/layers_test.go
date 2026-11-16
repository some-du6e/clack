package slackc

import (
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

// testLayer is an overlay layer that counts the Escapes it gets and draws
// a focusable home and field inside its area.
type testLayer struct {
	esc         int
	area        int
	home, field int
	noField     bool // the field isn't drawn
	js          bool
}

func (l *testLayer) LayerEscape() { l.esc++ }

// focusable draws tag as a focusable handler over r.
func focusable(gtx layout.Context, tag event.Tag, r image.Rectangle) {
	for {
		if _, ok := gtx.Event(key.FocusFilter{Target: tag}); !ok {
			break
		}
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
}

func (l *testLayer) draw(gtx layout.Context) {
	DrawLayer(gtx, l, LayerSpec{Area: &l.area, Home: &l.home, JS: l.js}, func(gtx layout.Context) {
		focusable(gtx, &l.home, image.Rect(0, 0, 50, 20))
		if !l.noField {
			focusable(gtx, &l.field, image.Rect(0, 30, 50, 50))
		}
	})
}

// layerWin is a window that draws layers the way the ui root does: Frame,
// then the page and the layers in the order they opened, RouteEscape
// last, under the frame lock.
type layerWin struct {
	r      input.Router
	ops    op.Ops
	now    time.Time
	page   func(gtx layout.Context)
	layers []*testLayer
}

func newLayerWin(t *testing.T) *layerWin {
	t.Helper()
	w := &layerWin{now: time.Unix(5000, 0)}
	t.Cleanup(func() { FocusFallback = nil; SetBridge(nil) })
	w.frames(3) // the window's first frames: layers other tests left close
	return w
}

func (w *layerWin) frame() {
	LockFrame()
	defer UnlockFrame()
	w.ops.Reset()
	w.now = w.now.Add(20 * time.Millisecond)
	gtx := layout.Context{Ops: &w.ops, Now: w.now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(400, 300)), Source: w.r.Source()}
	Frame(gtx)
	if w.page != nil {
		w.page(gtx)
	}
	for _, l := range w.layers {
		l.draw(gtx)
	}
	RouteEscape(gtx)
	w.r.Frame(&w.ops)
}

func (w *layerWin) frames(n int) {
	for i := 0; i < n; i++ {
		w.frame()
	}
}

// escape presses and releases Escape (with mods), then lays out frames.
func (w *layerWin) escape(mods key.Modifiers) {
	w.r.Queue(key.Event{Name: key.NameEscape, Modifiers: mods, State: key.Press}, key.Event{Name: key.NameEscape, Modifiers: mods, State: key.Release})
	w.frames(2)
}

func (w *layerWin) focus(tag event.Tag) {
	w.r.Source().Execute(key.FocusCmd{Tag: tag})
	w.frames(1)
}

func (w *layerWin) ctx() layout.Context { return layout.Context{Source: w.r.Source()} }

// dispatchRec is a bridge that records what Go dispatches to JS and
// answers DispatchResult with stop.
type dispatchRec struct {
	recBridge
	mu     sync.Mutex
	events []string // "<target>:<event JSON>"
	stop   bool
}

func (b *dispatchRec) Dispatch(target int64, ev []byte) {
	b.mu.Lock()
	b.events = append(b.events, keyString(target)+":"+string(ev))
	b.mu.Unlock()
}

func (b *dispatchRec) DispatchResult(target int64, ev []byte, done func(bool)) {
	b.Dispatch(target, ev)
	done(b.stop)
}

// keydowns lists the targets of the Escape keydowns dispatched so far.
func (b *dispatchRec) keydowns() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []string
	for _, e := range b.events {
		if strings.Contains(e, `"type":"keydown"`) && strings.Contains(e, `"key":"Escape"`) {
			out = append(out, e[:strings.IndexByte(e, ':')])
		}
	}
	return out
}

// An Escape no widget took goes as the DOM bubbles a keydown: to the
// innermost layer holding the focus, though another is on top; with the
// focus in none of them, to the topmost; a plugin layer's to JS, at the
// document (its default action cancels the topmost modal dialog), never
// to a Go layer under it. Shift doesn't matter (react-modal's keyCode 27).
func TestEscapeGoesToTheLayerHoldingTheFocus(t *testing.T) {
	w := newLayerWin(t)
	b := &dispatchRec{}
	SetBridge(b)
	var outside int
	w.page = func(gtx layout.Context) { focusable(gtx, &outside, image.Rect(300, 250, 350, 280)) }
	lo, hi := &testLayer{}, &testLayer{}
	w.layers = []*testLayer{lo, hi}
	w.frames(2)
	if !LayerOpen(w.ctx()) {
		t.Fatal("LayerOpen false with two layers drawn")
	}
	got := func(what string, wantLo, wantHi int) {
		t.Helper()
		if lo.esc != wantLo || hi.esc != wantHi {
			t.Errorf("%s: Escapes lower layer %d, upper %d; want %d, %d", what, lo.esc, hi.esc, wantLo, wantHi)
		}
	}
	w.focus(&lo.field)
	w.escape(0)
	got("the focus in the lower layer", 1, 0)
	w.focus(&hi.field)
	w.escape(key.ModShift)
	got("the focus in the upper layer, Shift held", 1, 1)
	w.focus(&outside)
	w.escape(0)
	got("the focus in no layer", 1, 2)

	js := &testLayer{js: true}
	w.layers = append(w.layers, js)
	w.frames(2)
	w.focus(&outside)
	w.escape(0)
	got("a plugin layer on top", 1, 2)
	if js.esc != 0 {
		t.Errorf("the plugin layer's LayerEscape ran %d times; its Escape is JS's", js.esc)
	}
	if k := b.keydowns(); len(k) != 1 || k[0] != "0" {
		t.Errorf("Escape keydowns dispatched at %v; want one, at the document (0)", k)
	}
}

// A layer gives the focus back as it closes (react-modal's
// shouldReturnFocusAfterClose): to what had it when the layer opened, any
// widget; with that gone, to the layer under it; with none left, to the
// window's own (the channel composer). A focus put somewhere else since
// stays. A layer whose focused field went takes the focus back.
func TestLayerGivesFocusBack(t *testing.T) {
	w := newLayerWin(t)
	var opener, other int
	drawOpener := true
	w.page = func(gtx layout.Context) {
		if drawOpener {
			focusable(gtx, &opener, image.Rect(0, 250, 50, 280))
		}
		focusable(gtx, &other, image.Rect(100, 250, 150, 280))
	}
	fallback := 0
	FocusFallback = func() { fallback++ }
	focused := func(tag event.Tag) bool { return w.r.Source().Focused(tag) }
	w.frames(1)

	// back to the opener
	w.focus(&opener)
	a := &testLayer{}
	w.layers = []*testLayer{a}
	w.frames(1)
	w.focus(&a.field)
	w.layers = nil
	w.frames(2)
	if !focused(&opener) {
		t.Errorf("the layer closed and the focus went to %v; want the widget that had it when it opened", w.r.Source().FocusTag())
	}

	// the opener (the lower layer's field) gone: the layer under it
	under := &testLayer{}
	w.layers = []*testLayer{under}
	w.frames(1)
	w.focus(&under.field)
	over := &testLayer{}
	w.layers = []*testLayer{under, over}
	w.frames(1)
	w.focus(&over.field)
	under.noField = true
	w.layers = []*testLayer{under}
	w.frames(2)
	if !focused(&under.home) {
		t.Errorf("the upper layer closed, its opener gone: the focus is on %v; want the lower layer's home", w.r.Source().FocusTag())
	}

	// no layer left and the opener gone: the window's own
	w.layers = nil
	w.frames(2)
	w.focus(&opener)
	last := &testLayer{}
	w.layers = []*testLayer{last}
	w.frames(1)
	w.focus(&last.field)
	drawOpener = false
	w.layers = nil
	n := fallback
	w.frames(2)
	if fallback != n+1 {
		t.Errorf("the last layer closed with its opener gone: FocusFallback ran %d times, want once", fallback-n)
	}
	drawOpener = true

	// a focus put elsewhere since stays
	w.frames(1)
	w.focus(&opener)
	x := &testLayer{}
	w.layers = []*testLayer{x}
	w.frames(1)
	w.focus(&other)
	w.layers = nil
	w.frames(2)
	if !focused(&other) {
		t.Errorf("a layer closed while another widget had the focus: it went to %v; want it left there", w.r.Source().FocusTag())
	}

	// the focused field went: the layer takes the focus back
	y := &testLayer{}
	w.layers = []*testLayer{y}
	w.frames(1)
	w.focus(&y.field)
	y.noField = true
	w.frames(2)
	if !focused(&y.home) {
		t.Errorf("the layer's focused field went: the focus is on %v; want the layer's home", w.r.Source().FocusTag())
	}
}

// A plugin <input> in a layer gets Escape first: its keydown goes to JS,
// whose listeners can stop it; one they don't stop goes on to the layer.
func TestPluginInputEscapeBubbles(t *testing.T) {
	w := newLayerWin(t)
	b := &dispatchRec{}
	SetBridge(b)
	v := NewView(906)
	t.Cleanup(v.Close)
	nodes := mustTree(t, `[{"t":"input","k":7,"p":{"autoFocus":true}}]`)
	l := &testLayer{noField: true}
	w.page = func(gtx layout.Context) {
		DrawLayer(gtx, l, LayerSpec{Area: &l.area}, func(gtx layout.Context) {
			gtx.Constraints = layout.Constraints{Max: image.Pt(300, 100)}
			v.Layout(gtx, nodes, "PluginPanel", nil, nil)
		})
	}
	w.frames(3)
	if w.r.Source().Focused(nil) {
		t.Fatal("the autoFocus input didn't take the focus")
	}
	b.stop = true
	w.escape(0)
	if l.esc != 0 {
		t.Errorf("a listener stopped the input's Escape and the layer still got it (%d)", l.esc)
	}
	if k := b.keydowns(); len(k) != 1 || k[0] != "7" {
		t.Errorf("Escape keydowns dispatched at %v; want one, at the input (7)", k)
	}
	b.stop = false
	w.escape(0)
	if l.esc != 1 {
		t.Errorf("no listener stopped the input's Escape: the layer got %d, want 1", l.esc)
	}
}

// The stack answers for its own window: another router (a test laying a
// widget out on its own, or a second window) sees no layer open and keeps
// the overlays' own Escape.
func TestLayerOpenOnlyInItsWindow(t *testing.T) {
	w := newLayerWin(t)
	w.layers = []*testLayer{{}}
	w.frames(2)
	var other input.Router
	o := layout.Context{Source: other.Source()}
	if !LayerOpen(w.ctx()) || !EscapeRouted(w.ctx()) {
		t.Errorf("in its window: LayerOpen %v, EscapeRouted %v; want true, true", LayerOpen(w.ctx()), EscapeRouted(w.ctx()))
	}
	if LayerOpen(o) || EscapeRouted(o) {
		t.Errorf("for another router: LayerOpen %v, EscapeRouted %v; want false, false", LayerOpen(o), EscapeRouted(o))
	}
	w.layers = nil
	w.frames(2)
	if LayerOpen(w.ctx()) {
		t.Error("LayerOpen true after the last layer closed")
	}
}
