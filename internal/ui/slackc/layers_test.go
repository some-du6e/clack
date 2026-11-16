package slackc

import (
	"image/color"
	"testing"
	"time"
)

// countLayer counts its LayerClosed calls.
type countLayer struct{ closed int }

func (l *countLayer) LayerClosed() { l.closed++ }

// A window's plugin layers: the topmost is the one its last frame laid
// out last (Escape is its); one the last frame left out closes as the
// next frame begins; each window has its own.
func TestLayersTopmostAndClosed(t *testing.T) {
	_, at := sweepEnv(t)
	frame := func() {
		Frame(cardCtx(at))
		at = at.Add(20 * time.Millisecond)
	}
	frame()
	frame() // what earlier tests left open closes
	var outer, inner countLayer
	check := func(what string, l *countLayer, top, others bool) {
		t.Helper()
		if gotTop, gotOthers := OpenLayer(l); gotTop != top || gotOthers != others {
			t.Errorf("%s: OpenLayer = top %v, others %v; want %v, %v", what, gotTop, gotOthers, top, others)
		}
	}
	frame()
	check("the outer layer opening", &outer, false, false)
	check("the inner layer opening over it", &inner, false, true)
	frame()
	if !LayerOpen() {
		t.Error("LayerOpen false with two layers open")
	}
	check("the outer layer, under the inner one", &outer, false, true)
	check("the inner layer, laid out last", &inner, true, true)
	frame()
	check("the outer layer without the inner one (open in the last frame)", &outer, false, true)
	frame()
	if inner.closed != 1 || outer.closed != 0 {
		t.Errorf("the frame without the inner layer: LayerClosed inner %d, outer %d times; want 1, 0", inner.closed, outer.closed)
	}
	check("the outer layer, topmost now", &outer, true, false)
	frame()
	frame()
	if outer.closed != 1 || LayerOpen() {
		t.Errorf("no layer laid out: outer closed %d times, LayerOpen %v; want 1, false", outer.closed, LayerOpen())
	}
	// a secondary window's layers are its own
	var w SubWindow
	var sub countLayer
	for i := 0; i < 2; i++ {
		restore := w.Frame(cardCtx(at), "p-huddle_window", color.NRGBA{A: 0xff})
		OpenLayer(&sub)
		restore()
		frame()
		if LayerOpen() {
			t.Fatal("the main window has a layer open: the secondary window's")
		}
	}
	restore := w.Frame(cardCtx(at), "p-huddle_window", color.NRGBA{A: 0xff})
	if !LayerOpen() {
		t.Error("the secondary window's layer isn't open in its frame")
	}
	restore()
}
