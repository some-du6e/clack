// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"image"
	"image/color"
	"testing"

	pf32 "gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// tiles reports whether parts are disjoint, lie in b and cover all of b
// but hole.
func tiles(parts []image.Rectangle, b, hole image.Rectangle) bool {
	n := 0
	for i, p := range parts {
		if p.Empty() || !p.In(b) || p.Overlaps(hole) {
			return false
		}
		for _, q := range parts[i+1:] {
			if p.Overlaps(q) {
				return false
			}
		}
		n += p.Dx() * p.Dy()
	}
	h := hole.Intersect(b)
	return n+h.Dx()*h.Dy() == b.Dx()*b.Dy()
}

// Clack patch 42: an op is split around a stroke's hole, which is left
// out, or around and inside a rounded clip's interior, in parts that tile
// it, only for a uniform colour and only where that saves enough.
func TestSplitOp(t *testing.T) {
	b := image.Rect(60, 30, 1280, 800)
	ring := &pathOp{path: true, hole: image.Rect(70, 40, 1270, 790)}
	parts, n := splitOp(ring, b, materialColor)
	if n != 4 || !tiles(parts[:n], b, ring.hole) {
		t.Errorf("around the hole: %v, want 4 bands tiling %v but %v", parts[:n], b, ring.hole)
	}
	panel := &pathOp{path: true, interior: image.Rect(337, 41, 1089, 695)}
	fill := &pathOp{parent: panel} // the fill's own rect clip
	pb := image.Rect(330, 40, 1096, 696)
	parts, n = splitOp(fill, pb, materialColor)
	if n != 5 || !tiles(parts[:n], pb, image.Rectangle{}) || parts[n-1] != panel.interior {
		t.Errorf("around and inside the interior: %v, want 5 parts tiling %v, the interior last", parts[:n], pb)
	}
	for _, c := range []struct {
		what string
		p    *pathOp
		b    image.Rectangle
		m    materialType
	}{
		{"an image", ring, b, materialTexture},
		{"a gradient", ring, b, materialLinearGradient},
		{"inside the interior", fill, image.Rect(400, 100, 900, 600), materialColor},
		{"a small part inside", fill, image.Rect(1000, 600, 1096, 696), materialColor},
		{"no hole nor interior", &pathOp{path: true}, b, materialColor},
		{"a hole too small", &pathOp{path: true, hole: image.Rect(0, 0, 100, 100)}, image.Rect(0, 0, 110, 110), materialColor},
	} {
		if _, n := splitOp(c.p, c.b, c.m); n != 0 {
			t.Errorf("%s: split in %d", c.what, n)
		}
	}
	ClackLegacyClips(true)
	defer ClackLegacyClips(false)
	if _, n := splitOp(ring, b, materialColor); n != 0 {
		t.Errorf("legacy: split in %d", n)
	}
}

// collectOps collects build's ops in d, as a frame does.
func collectOps(d *drawOps, viewport image.Point, build func(o *op.Ops)) {
	var o op.Ops
	build(&o)
	d.reset(viewport)
	d.collect(&o, viewport)
}

// Clack patch 42: a ring (a stroked RRect) gets its hole in window
// coordinates under an integer offset and is drawn in the bands around
// it; under a scale or a fraction of a pixel it has none and is drawn in
// one piece, as Gio did.
func TestCollectStrokeHole(t *testing.T) {
	var d drawOps
	d.pathCache = newOpCache()
	viewport := image.Pt(1400, 900)
	rr := clip.UniformRRect(image.Rect(0, 0, 1200, 760), 10)
	var keep op.Ops
	spec := rr.Path(&keep) // cached at the origin, as theme.RRectPath
	ring := func(o *op.Ops) {
		paint.FillShape(o, color.NRGBA{A: 0x30}, clip.Stroke{Path: spec, Width: 1.5}.Op())
	}
	off := image.Pt(70, 40)
	collectOps(&d, viewport, func(o *op.Ops) {
		defer op.Offset(off).Push(o).Pop()
		ring(o)
	})
	want := rr.StrokeHole(1.5).Add(off)
	if len(d.pathOps) != 1 || d.pathOps[0].hole != want {
		t.Fatalf("hole %v, want %v", d.pathOps, want)
	}
	box := image.Rect(69, 39, 1271, 801)
	var parts []image.Rectangle
	for _, img := range d.imageOps {
		parts = append(parts, img.clip)
	}
	if len(parts) != 4 || !tiles(parts, box, want) {
		t.Errorf("the ring drawn in %v, want 4 bands tiling %v but %v", parts, box, want)
	}
	for _, tr := range []pf32.Affine2D{
		pf32.Affine2D{}.Scale(pf32.Point{}, pf32.Pt(1.37, 1.37)),
		pf32.Affine2D{}.Offset(pf32.Pt(70.4, 40.7)),
	} {
		collectOps(&d, viewport, func(o *op.Ops) {
			defer op.Affine(tr).Push(o).Pop()
			ring(o)
		})
		// (the paint gets a clip path of its own there too, Gio's
		// transformed rectangle)
		for _, p := range d.pathOps {
			if !p.hole.Empty() {
				t.Errorf("under %v: a path has the hole %v", tr, p.hole)
			}
		}
		if len(d.imageOps) != 1 {
			t.Errorf("under %v: the ring drawn in %d ops, want 1", tr, len(d.imageOps))
		}
	}
}

// Clack patch 42: a fill of a rounded panel is drawn in the edges around
// its interior, through the clip, and the interior, without it (patch
// 38); under a transform in one piece through the clip.
func TestCollectPanelSplit(t *testing.T) {
	var d drawOps
	d.pathCache = newOpCache()
	viewport := image.Pt(1400, 900)
	panel := clip.RRect{Rect: image.Rect(330, 40, 1096, 696), NE: 6, SE: 6}
	fill := func(o *op.Ops) {
		defer panel.Push(o).Pop()
		paint.FillShape(o, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect(panel.Rect).Op())
	}
	collectOps(&d, viewport, fill)
	var parts []image.Rectangle
	covered := 0
	for _, img := range d.imageOps {
		parts = append(parts, img.clip)
		if img.path.parent.covers(img.clip) {
			covered++
		}
	}
	if len(parts) != 5 || !tiles(parts, panel.Rect, image.Rectangle{}) || covered != 1 {
		t.Errorf("the panel drawn in %v (%d without the clip), want 5 parts tiling %v, the interior without the clip", parts, covered, panel.Rect)
	}
	collectOps(&d, viewport, func(o *op.Ops) {
		defer op.Affine(pf32.Affine2D{}.Scale(pf32.Point{}, pf32.Pt(1.37, 1.37))).Push(o).Pop()
		fill(o)
	})
	if len(d.imageOps) != 1 {
		t.Errorf("under a scale the panel is drawn in %d parts, want 1", len(d.imageOps))
	}
}

// Clack patch 43: a path cached at the origin is built again for a new
// fraction of a pixel, and found again at whole-pixel offsets.
func TestCollectSubPixelKey(t *testing.T) {
	var d drawOps
	d.pathCache = newOpCache()
	viewport := image.Pt(400, 300)
	var keep op.Ops
	spec := clip.UniformRRect(image.Rect(0, 0, 36, 36), 8).Path(&keep)
	key := func(tr pf32.Affine2D) opKey {
		collectOps(&d, viewport, func(o *op.Ops) {
			defer op.Affine(tr).Push(o).Pop()
			paint.FillShape(o, color.NRGBA{A: 0xff}, clip.Outline{Path: spec}.Op())
		})
		return d.pathOps[0].pathKey
	}
	at := func(x, y float32) opKey { return key(pf32.Affine2D{}.Offset(pf32.Pt(x, y))) }
	// fractions a float32 holds exactly at any of these offsets
	k0, k1, k2 := at(10, 10), at(20, 30), at(10.375, 10.625)
	if k0 != k1 {
		t.Errorf("whole-pixel offsets give keys %+v and %+v", k0, k1)
	}
	if k2 == k0 {
		t.Errorf("an offset of (10.375, 10.625) finds the path built at (10, 10)")
	}
	if at(30.375, 20.625) != k2 {
		t.Errorf("the same fraction at another whole-pixel offset gives another key")
	}
}

// Clack patch 40's old way: every nested layer on a page of its own.
func TestPackLayersLegacy(t *testing.T) {
	r := &renderer{}
	r.layers.maxDims = image.Pt(4096, 4096)
	layers := func() []opacityLayer {
		l := func(depth, parent int, x int) opacityLayer {
			return opacityLayer{depth: depth, parent: parent, clip: image.Rect(x, 0, x+50, 50), opStart: 0, opEnd: 1}
		}
		return []opacityLayer{l(0, -1, 0), l(1, 0, 10), l(1, 0, 20), l(1, 0, 30)}
	}
	ClackLegacyLayers(true)
	defer ClackLegacyLayers(false)
	ls := r.packLayers(layers())
	if ls[1].place.Idx == ls[2].place.Idx || ls[2].place.Idx == ls[3].place.Idx {
		t.Errorf("legacy: nested layers on pages %d, %d, %d, want one each", ls[1].place.Idx, ls[2].place.Idx, ls[3].place.Idx)
	}
}
