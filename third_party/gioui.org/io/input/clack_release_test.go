// SPDX-License-Identifier: Unlicense OR MIT

package input

import (
	"image"
	"runtime"
	"testing"
	"weak"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"
)

// Clack patch 18: once frames stop laying out a handler, the router lets
// go of its tag. The hit tree, the focus order and the state history are
// refilled every frame into the same arrays; a longer earlier frame's
// entries kept their tags, and whatever owns them, reachable past the end
// of a shorter frame.
func TestRouterReleasesTagsOfEarlierFrames(t *testing.T) {
	type owner struct {
		tag int
		_   [64 << 10]byte
	}
	var r Router
	keep := make([]int, 64)
	frame := func(o *owner) {
		var ops op.Ops
		for i := range keep {
			addPointerHandler(&r, &ops, &keep[i], image.Rect(0, i, 100, i+1))
			events(&r, -1, key.FocusFilter{Target: &keep[i]})
		}
		if o != nil {
			addPointerHandler(&r, &ops, &o.tag, image.Rect(0, 0, 100, 100))
			events(&r, -1, key.FocusFilter{Target: &o.tag})
		}
		r.Frame(&ops)
	}
	o := new(owner)
	w := weak.Make(o)
	frame(o)
	// focused and under the pointer, as an open popup's search field is
	r.Source().Execute(key.FocusCmd{Tag: &o.tag})
	r.Queue(pointer.Event{Kind: pointer.Move, Position: f32.Pt(50, 50)})
	frame(o)
	t.Logf("focused before it went: %v", r.Source().Focused(&o.tag))
	o = nil
	for range 3 {
		frame(nil)
	}
	runtime.GC()
	if w.Value() != nil {
		t.Error("the router still reaches a tag no frame has laid out for three frames")
	}
}
