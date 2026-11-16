package slackc

import (
	"image"
	"slices"
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
	plugin      bool // a plugin's (kit's Popover): Escape is routed to it
	async       bool // its Escape closes it later (JS)
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
	DrawLayer(gtx, l, LayerSpec{Area: &l.area, Home: &l.home, JS: l.js, Plugin: l.plugin, Async: l.async}, func(gtx layout.Context) {
		focusable(gtx, &l.home, image.Rect(0, 0, 50, 20))
		if !l.noField {
			focusable(gtx, &l.field, image.Rect(0, 30, 50, 50))
		}
	})
}

// layerWin is a window that draws layers the way the ui root does: Frame,
// the shell's Escape binding (shell, when set), then the page and the
// layers in the order they opened, RouteEscape, and after it the
// window's key listeners (post), under the frame lock.
type layerWin struct {
	r      input.Router
	ops    op.Ops
	now    time.Time
	shell  func(gtx layout.Context)
	page   func(gtx layout.Context)
	post   func(gtx layout.Context)
	layers []*testLayer
}

func newLayerWin(t *testing.T) *layerWin {
	t.Helper()
	w := &layerWin{now: time.Unix(5000, 0)}
	t.Cleanup(func() { FocusFallback, ShellEscape = nil, nil; SetBridge(nil) })
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
	if w.shell != nil {
		w.shell(gtx)
	}
	if w.page != nil {
		w.page(gtx)
	}
	for _, l := range w.layers {
		l.draw(gtx)
	}
	RouteEscape(gtx)
	if w.post != nil {
		w.post(gtx)
	}
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

func (w *layerWin) focused(tag event.Tag) bool { return w.r.Source().Focused(tag) }

// shellBinding is the ui root's "Close / mark as read": read first in the
// frame unless the focus is in a plugin's layer, given to a plugin's layer
// it goes on to (EscapeToLayer), else counted as the shell's own.
func (w *layerWin) shellBinding(own *int) {
	w.shell = func(gtx layout.Context) {
		if EscapeToFocus(gtx) {
			return
		}
		for {
			e, ok := gtx.Event(key.Filter{Name: key.NameEscape})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press && !EscapeToLayer(gtx, ke, false) {
				*own++
			}
		}
	}
}

// dispatchRec is a bridge that records what Go dispatches to JS and
// answers DispatchResult with stop, or holds the answers (hold) until
// answer.
type dispatchRec struct {
	recBridge
	mu      sync.Mutex
	events  []string // "<target>:<event JSON>"
	stop    bool
	hold    bool
	pending []func(bool)
}

func (b *dispatchRec) Dispatch(target int64, ev []byte) {
	b.mu.Lock()
	b.events = append(b.events, keyString(target)+":"+string(ev))
	b.mu.Unlock()
}

func (b *dispatchRec) DispatchResult(target int64, ev []byte, done func(bool)) {
	b.Dispatch(target, ev)
	b.mu.Lock()
	if b.hold {
		b.pending = append(b.pending, done)
		b.mu.Unlock()
		return
	}
	stop := b.stop
	b.mu.Unlock()
	done(stop)
}

// answer answers the held dispatches (JS got round to them).
func (b *dispatchRec) answer(stopped bool) {
	b.mu.Lock()
	p := b.pending
	b.pending = nil
	b.mu.Unlock()
	for _, done := range p {
		done(stopped)
	}
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

// With plugins' layers open an Escape no widget took goes as the DOM
// bubbles a keydown: to the innermost layer holding the focus, though
// another is on top; with the focus in none of them, to the topmost; a
// plugin overlay root's to JS, at the document (its default action
// cancels the topmost modal dialog), never to a Go layer under it. Shift
// doesn't matter (react-modal's keyCode 27).
func TestEscapeGoesToTheLayerHoldingTheFocus(t *testing.T) {
	w := newLayerWin(t)
	b := &dispatchRec{}
	SetBridge(b)
	var outside int
	w.page = func(gtx layout.Context) { focusable(gtx, &outside, image.Rect(300, 250, 350, 280)) }
	lo, hi := &testLayer{plugin: true}, &testLayer{plugin: true}
	w.layers = []*testLayer{lo, hi}
	w.frames(2)
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
	got("a plugin overlay root on top", 1, 2)
	if js.esc != 0 {
		t.Errorf("the plugin overlay root's LayerEscape ran %d times; its Escape is JS's", js.esc)
	}
	if k := b.keydowns(); len(k) != 1 || k[0] != "0" {
		t.Errorf("Escape keydowns dispatched at %v; want one, at the document (0)", k)
	}
}

// The shell's own layers keep the shell's Escape, as before there was a
// stack: with no plugin's layer involved, RouteEscape takes no Escape
// (the window's key listeners still hear it) and gives none to them;
// their own readers and the shell's binding close them.
func TestNativeLayersKeepTheirEscape(t *testing.T) {
	w := newLayerWin(t)
	heard := 0
	w.post = func(gtx layout.Context) {
		for {
			e, ok := gtx.Event(key.Filter{Name: key.NameEscape})
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				heard++
			}
		}
	}
	lo, hi := &testLayer{}, &testLayer{}
	w.layers = []*testLayer{lo, hi}
	w.frames(2)
	w.focus(&lo.field)
	w.escape(0)
	if lo.esc != 0 || hi.esc != 0 {
		t.Errorf("the stack gave a native layer an Escape (lower %d, upper %d); the shell's are their own", lo.esc, hi.esc)
	}
	if heard != 1 {
		t.Errorf("the window's key listeners heard %d Escapes after RouteEscape; want 1 (it took none)", heard)
	}
}

// The shell's binding, read first in the frame, keeps the shell's Escape
// with only its own layers open; a plugin's layer opened from inside one
// of them (the focus was in it) is on top and gets the Escape first, as
// does a plugin's layer over them; with the focus in a plugin's layer the
// binding stands down and the layer gets what its element didn't take.
func TestPluginLayerOverNativeGetsEscapeFirst(t *testing.T) {
	w := newLayerWin(t)
	own := 0
	w.shellBinding(&own)
	native := &testLayer{}
	w.layers = []*testLayer{native}
	w.frames(2)
	w.focus(&native.field)
	w.escape(0)
	if own != 1 || native.esc != 0 {
		t.Fatalf("only a native layer open: the shell's binding had %d Escapes, the stack gave the layer %d; want 1, 0", own, native.esc)
	}
	child := &testLayer{plugin: true, noField: true}
	w.layers = []*testLayer{native, child} // opened with the focus in the native layer: its child
	w.frames(2)
	if !w.focused(&native.field) {
		t.Fatal("the child took the focus: this case is about one that doesn't")
	}
	w.escape(0)
	if child.esc != 1 || native.esc != 0 || own != 1 {
		t.Errorf("the focus in a native layer under a plugin's it opened: Escapes plugin layer %d, native %d, shell %d; want 1, 0, 1",
			child.esc, native.esc, own)
	}
	w.focus(&child.home)
	w.escape(0)
	if child.esc != 2 || own != 1 {
		t.Errorf("the focus in the plugin's layer: Escapes plugin layer %d, shell %d; want 2, 1 (the binding stood down)", child.esc, own)
	}
}

// A layer gives the focus back in the frame it stops drawing
// (react-modal's shouldReturnFocusAfterClose): to what had it when the
// layer opened, any widget; with that gone, to the layer it opened from;
// with none left, to the window's own (FocusFallback). A focus put
// somewhere else since stays. A layer whose focused field went takes the
// focus back.
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
	FocusFallback = func(layout.Context) { fallback++ }
	w.frames(1)

	// back to the opener, in the frame the layer stopped drawing
	w.focus(&opener)
	a := &testLayer{}
	w.layers = []*testLayer{a}
	w.frames(1)
	w.focus(&a.field)
	w.layers = nil
	w.frames(1)
	if !w.focused(&opener) {
		t.Errorf("the layer closed and the focus went to %v; want the widget that had it when it opened, in that frame", w.r.Source().FocusTag())
	}

	// the opener (the lower layer's field) gone: the layer it opened from
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
	w.frames(1)
	if !w.focused(&under.home) {
		t.Errorf("the upper layer closed, its opener gone: the focus is on %v; want the lower layer's home", w.r.Source().FocusTag())
	}
	under.noField = false

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
	if !w.r.Source().Focused(nil) {
		t.Errorf("the closed layer's field still has the focus (%v): nothing took it, so nothing should have it", w.r.Source().FocusTag())
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
	if !w.focused(&other) {
		t.Errorf("a layer closed while another widget had the focus: it went to %v; want it left there", w.r.Source().FocusTag())
	}

	// the focused field went: the layer takes the focus back
	y := &testLayer{}
	w.layers = []*testLayer{y}
	w.frames(1)
	w.focus(&y.field)
	y.noField = true
	w.frames(2)
	if !w.focused(&y.home) {
		t.Errorf("the layer's focused field went: the focus is on %v; want the layer's home", w.r.Source().FocusTag())
	}
	// but a layer that never had the focus (a peek) gives none
	w.layers = nil
	w.frames(2)
	w.focus(nil)
	peek := &testLayer{noField: true}
	w.layers = []*testLayer{peek}
	w.frames(2)
	w.layers = nil
	n = fallback
	w.frames(2)
	if fallback != n || !w.r.Source().Focused(nil) {
		t.Errorf("a layer that never had the focus closed: FocusFallback ran %d times, the focus is on %v; want neither", fallback-n, w.r.Source().FocusTag())
	}
}

// A layer opened from inside one that then closes (a modal from a menu
// item: the menu goes as the modal opens) gives the focus back where the
// menu would have, to what had it before the menu opened, not to the
// menu's dead field.
func TestLayerOpenedFromAClosedOneGivesItsFocusBack(t *testing.T) {
	w := newLayerWin(t)
	var opener int
	w.page = func(gtx layout.Context) { focusable(gtx, &opener, image.Rect(0, 250, 50, 280)) }
	FocusFallback = func(layout.Context) { t.Error("FocusFallback ran: the opener was there to take the focus") }
	w.frames(1)
	w.focus(&opener)
	menu := &testLayer{}
	w.layers = []*testLayer{menu}
	w.frames(1)
	w.focus(&menu.field)
	modal := &testLayer{}
	w.layers = []*testLayer{menu, modal} // the item's click opens the modal...
	w.frames(1)
	w.layers = []*testLayer{modal} // ...and the menu goes
	w.focus(&modal.field)
	w.frames(2)
	w.layers = nil
	w.frames(1)
	if !w.focused(&opener) {
		t.Errorf("the modal opened from the menu closed: the focus is on %v; want what had it before the menu", w.r.Source().FocusTag())
	}
}

// A field a hold keeps focusable after its layer went (composer.KeepFocus
// draws a focused editor that wasn't drawn this frame at the window's
// root, after the frame's layout) doesn't keep the focus: the layer gives
// it back in the frame it stopped drawing (Gio runs that FocusCmd as the
// frame ends, the opener having read its focus events already: the hold
// has that frame, and no other). Also after a frame in which the layer
// missed the field and the hold bridged it (the field's area is the
// root's then).
func TestClosedLayerFieldKeepsNoFocus(t *testing.T) {
	for _, glitch := range []bool{false, true} {
		w := newLayerWin(t)
		var opener int
		w.page = func(gtx layout.Context) { focusable(gtx, &opener, image.Rect(0, 250, 50, 280)) }
		l := &testLayer{plugin: true}
		held := 0
		w.post = func(gtx layout.Context) { // KeepFocus
			if !gtx.Focused(&l.field) || slices.Contains(w.layers, l) && !l.noField {
				return
			}
			held++
			for {
				if _, ok := gtx.Event(key.FocusFilter{Target: &l.field}); !ok {
					break
				}
			}
			defer clip.Rect{}.Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, &l.field)
		}
		w.frames(1)
		w.focus(&opener)
		w.layers = []*testLayer{l}
		w.frames(1)
		w.focus(&l.field)
		if glitch {
			l.noField = true
			w.frames(1)
			if held != 1 || !w.focused(&l.field) {
				t.Fatalf("the hold didn't bridge the frame that missed the field (held %d, focus on %v)", held, w.r.Source().FocusTag())
			}
		}
		w.layers = nil
		w.frames(1)
		if !w.focused(&opener) {
			t.Errorf("glitch %v: the layer closed with its field focused: the focus is on %v; want the opener, as that frame ended",
				glitch, w.r.Source().FocusTag())
		}
		held = 0
		w.frames(3)
		if !w.focused(&opener) || held != 0 {
			t.Errorf("glitch %v: frames after the layer closed the focus is on %v and the hold kept the field %d frames; want the opener, none",
				glitch, w.r.Source().FocusTag(), held)
		}
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
		DrawLayer(gtx, l, LayerSpec{Area: &l.area, Plugin: true}, func(gtx layout.Context) {
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

// pluginInputIn lays out a plugin <input> (k 7, autoFocus) as the content
// of layer l, a plugin's async one (a kit Popover), over a native layer
// it was opened from.
func pluginInputIn(t *testing.T, w *layerWin, l, under *testLayer) {
	t.Helper()
	v := NewView(907)
	t.Cleanup(v.Close)
	nodes := mustTree(t, `[{"t":"input","k":7,"p":{"autoFocus":true}}]`)
	w.layers = []*testLayer{under}
	w.frames(1)
	w.focus(&under.field)
	w.page = func(gtx layout.Context) {
		DrawLayer(gtx, l, LayerSpec{Area: &l.area, Plugin: true, Async: true}, func(gtx layout.Context) {
			gtx.Constraints = layout.Constraints{Max: image.Pt(300, 100)}
			v.Layout(gtx, nodes, "PluginPanel", nil, nil)
		})
	}
	w.frames(3)
	if w.focused(&under.field) || w.r.Source().Focused(nil) {
		t.Fatal("the autoFocus input didn't take the focus")
	}
}

// Two Escapes pressed before JS answered are both the plugin's layer's,
// as react-modal stops every Escape that reaches it: the first calls its
// onClose, the second nothing (onClose once per round trip), and neither
// reaches the layer it was opened from.
func TestTwoEscapesBeforeJSAnswers(t *testing.T) {
	w := newLayerWin(t)
	b := &dispatchRec{hold: true}
	SetBridge(b)
	under := &testLayer{}
	pop := &testLayer{noField: true}
	pluginInputIn(t, w, pop, under)
	w.escape(0)
	w.escape(0)
	if k := b.keydowns(); len(k) != 2 {
		t.Fatalf("Escape keydowns dispatched at %v; want two at the input", k)
	}
	b.answer(false)
	w.frames(2)
	if pop.esc != 1 || under.esc != 0 {
		t.Errorf("two Escapes JS let go: the plugin's layer got %d, the one under it %d; want 1 and 0", pop.esc, under.esc)
	}
}

// A plugin's Popover that stays open after its onClose (a plugin keeping
// it, or none to call) takes every Escape aimed at it, with the focus in
// it or not: nothing goes past it to the shell's binding (a second press
// marked the channel read, or closed the thread, under the open popover)
// or to the layer under it. Within the time JS has to close it a second
// one calls nothing; after it, onClose again.
func TestStillOpenPopoverTakesEveryEscape(t *testing.T) {
	w := newLayerWin(t)
	own, shell := 0, 0
	w.shellBinding(&own)
	ShellEscape = func(layout.Context, key.Event) { shell++ }
	var outside int
	w.page = func(gtx layout.Context) { focusable(gtx, &outside, image.Rect(300, 250, 350, 280)) }
	pop := &testLayer{plugin: true, async: true}
	w.layers = []*testLayer{pop}
	w.frames(2)
	got := func(what string, wantPop int) {
		t.Helper()
		if pop.esc != wantPop || own != 0 || shell != 0 {
			t.Errorf("%s: the popover got %d Escapes, the shell's binding %d (ShellEscape %d); want %d, 0, 0", what, pop.esc, own, shell, wantPop)
		}
	}
	w.focus(&pop.field)
	w.escape(0)
	got("the first Escape in it", 1)
	w.escape(0)
	got("a second one while it's still open", 1)
	w.focus(&outside)
	w.escape(0)
	got("one with the focus outside it", 1)
	w.now = w.now.Add(time.Second) // past the time JS has to close it
	w.frames(1)
	w.escape(0)
	got("one after JS had the time to close it", 2)
}

// An answer that comes after the layer it was aimed at closed some other
// way (a click) closes nothing: not a layer opened since, nor the one
// under it.
func TestLateEscapeAnswerClosesNothingNew(t *testing.T) {
	w := newLayerWin(t)
	b := &dispatchRec{hold: true}
	SetBridge(b)
	under := &testLayer{}
	pop := &testLayer{noField: true}
	pluginInputIn(t, w, pop, under)
	w.escape(0)
	w.page = nil // a click closed the popover...
	later := &testLayer{plugin: true}
	w.layers = []*testLayer{under, later} // ...and opened another layer
	w.frames(2)
	b.answer(false)
	w.frames(2)
	if pop.esc != 0 || under.esc != 0 || later.esc != 0 {
		t.Errorf("a late Escape answer: Escapes closed popover %d, layer under %d, layer opened since %d; want none", pop.esc, under.esc, later.esc)
	}
}

// With no answer from JS (a stuck task, a runtime gone), the Escape goes
// on as unstopped after escAnswerWait; the answer that comes after counts
// for nothing.
func TestEscapeWithoutAnswerGoesOn(t *testing.T) {
	w := newLayerWin(t)
	b := &dispatchRec{hold: true}
	SetBridge(b)
	under := &testLayer{}
	pop := &testLayer{noField: true}
	pluginInputIn(t, w, pop, under)
	w.escape(0)
	if pop.esc != 0 {
		t.Fatal("the Escape went on before JS answered or the wait ran out")
	}
	time.Sleep(escAnswerWait + 100*time.Millisecond)
	w.frames(2)
	if pop.esc != 1 {
		t.Errorf("no answer from JS: the plugin's layer got %d Escapes; want 1 after %v", pop.esc, escAnswerWait)
	}
	b.answer(true)
	w.frames(2)
	if pop.esc != 1 || under.esc != 0 {
		t.Errorf("the late answer changed it: Escapes plugin layer %d, under %d; want 1, 0", pop.esc, under.esc)
	}
}

// An Escape in the frame after the plugin's layer that had the focus
// stopped drawing (JS closed it) isn't lost: the shell's binding stood
// down for the layer, and it gets the Escape (ShellEscape).
func TestEscapeAfterTheLastLayerWent(t *testing.T) {
	w := newLayerWin(t)
	own := 0
	w.shellBinding(&own)
	shell := 0
	ShellEscape = func(layout.Context, key.Event) { shell++ }
	l := &testLayer{plugin: true}
	w.layers = []*testLayer{l}
	w.frames(1)
	w.focus(&l.field)
	w.layers = nil // gone in the frame the Escape comes in
	w.r.Queue(key.Event{Name: key.NameEscape, State: key.Press}, key.Event{Name: key.NameEscape, State: key.Release})
	w.frames(2)
	if shell+own != 1 || l.esc != 0 {
		t.Errorf("an Escape as the last layer went: the shell's binding got %d (%d stood down), the closed layer %d; want the shell's 1", shell+own, shell, l.esc)
	}
}

// The stack answers for its own window: another router (a test laying a
// widget out on its own, or a second window) sees no routing and keeps
// the overlays' own Escape.
func TestStackOnlyInItsWindow(t *testing.T) {
	w := newLayerWin(t)
	l := &testLayer{plugin: true}
	w.layers = []*testLayer{l}
	w.frames(2)
	w.focus(&l.field)
	var other input.Router
	o := layout.Context{Source: other.Source()}
	in := layout.Context{Source: w.r.Source()}
	if !EscapeRouted(in) || !EscapeToFocus(in) {
		t.Errorf("in its window: EscapeRouted %v, EscapeToFocus %v; want true, true", EscapeRouted(in), EscapeToFocus(in))
	}
	if EscapeRouted(o) || EscapeToFocus(o) {
		t.Errorf("for another router: EscapeRouted %v, EscapeToFocus %v; want false, false", EscapeRouted(o), EscapeToFocus(o))
	}
	w.layers = nil
	w.frames(2)
	if EscapeToFocus(in) {
		t.Error("EscapeToFocus true after the plugin's layer closed")
	}
}
