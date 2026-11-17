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

// Clack patch 39: ops far apart get stencil regions of their own, ops
// close together share one, and every op finds a region holding it.
func TestAddRegion(t *testing.T) {
	p := &pathOp{path: true}
	rects := []image.Rectangle{
		image.Rect(0, 0, 1000, 2),     // the top edge of a panel
		image.Rect(990, 0, 1000, 800), // its right edge
		image.Rect(0, 400, 1000, 440), // a hovered row
		image.Rect(0, 2, 1000, 4),     // just under the top edge: joins it
		image.Rect(992, 100, 998, 300),
	}
	for _, r := range rects {
		p.addRegion(r)
	}
	if n := 1 + len(p.more); n != 3 {
		t.Errorf("%d regions (%v, %v), want 3: the top, the right edge, the row", n, p.clip, p.more)
	}
	for _, r := range rects {
		if c, _ := p.region(r); !r.In(c) {
			t.Errorf("%v: region %v doesn't hold it", r, c)
		}
	}
	var area int
	area += p.clip.Dx() * p.clip.Dy()
	for _, m := range p.more {
		area += m.clip.Dx() * m.clip.Dy()
	}
	if area > 60000 {
		t.Errorf("regions cover %d px, want the edges' and the row's few", area)
	}
	// the old way: one region around everything
	ClackLegacyClips(true)
	defer ClackLegacyClips(false)
	q := &pathOp{path: true}
	for _, r := range rects {
		q.addRegion(r)
	}
	if len(q.more) != 0 || q.clip != image.Rect(0, 0, 1000, 800) {
		t.Errorf("legacy: %v + %v, want one region (0,0)-(1000,800)", q.clip, q.more)
	}
}

// Clack patch 38: collect gives a rounded clip its interior in window
// coordinates under an integer offset, none under a scale, and an op
// inside it doesn't count the clip.
func TestCollectClipInterior(t *testing.T) {
	var d drawOps
	d.pathCache = newOpCache()
	viewport := image.Pt(800, 600)
	rr := clip.RRect{Rect: image.Rect(0, 0, 400, 300), NE: 12, SE: 12}
	collect := func(build func(o *op.Ops)) {
		var o op.Ops
		build(&o)
		d.reset(viewport)
		d.collect(&o, viewport)
	}
	collect(func(o *op.Ops) {
		defer op.Offset(image.Pt(100, 50)).Push(o).Pop()
		defer rr.Push(o).Pop()
		paint.FillShape(o, color.NRGBA{A: 0xff}, clip.Rect(image.Rect(10, 10, 200, 40)).Op())
		paint.FillShape(o, color.NRGBA{A: 0xff}, clip.Rect(image.Rect(390, 10, 400, 40)).Op())
	})
	want := rr.Interior().Add(image.Pt(100, 50))
	var p *pathOp
	for _, q := range d.pathOps {
		p = q
	}
	if p == nil || p.interior != want {
		t.Fatalf("the clip's interior %v, want %v", p, want)
	}
	if !p.covers(d.imageOps[0].clip) {
		t.Errorf("the op at %v inside %v doesn't drop the clip", d.imageOps[0].clip, want)
	}
	if p.covers(d.imageOps[1].clip) {
		t.Errorf("the op at %v, at the rounded edge, drops the clip", d.imageOps[1].clip)
	}
	collect(func(o *op.Ops) {
		defer op.Affine(pf32.Affine2D{}.Scale(pf32.Point{}, pf32.Pt(1.5, 1.5))).Push(o).Pop()
		defer rr.Push(o).Pop()
		paint.FillShape(o, color.NRGBA{A: 0xff}, clip.Rect(image.Rect(10, 10, 200, 40)).Op())
	})
	for _, q := range d.pathOps {
		if !q.interior.Empty() {
			t.Errorf("a scaled clip has the interior %v", q.interior)
		}
	}
}

// Clack patch 40: layers of one depth share an atlas page; a nested
// layer gets a page of its own.
func TestPackLayersOneDepthOnePage(t *testing.T) {
	r := &renderer{}
	r.layers.maxDims = image.Pt(4096, 4096)
	layer := func(depth, parent int, x int) opacityLayer {
		return opacityLayer{depth: depth, parent: parent, clip: image.Rect(x, 0, x+50, 50), opStart: 0, opEnd: 1}
	}
	layers := []opacityLayer{
		layer(0, -1, 0),
		layer(1, 0, 10), layer(1, 0, 20), layer(1, 0, 30),
		layer(0, -1, 300),
	}
	layers = r.packLayers(layers)
	pages := map[int]bool{}
	for _, l := range layers {
		pages[l.place.Idx] = true
	}
	if layers[1].place.Idx != layers[2].place.Idx || layers[2].place.Idx != layers[3].place.Idx {
		t.Errorf("the three nested layers are on pages %d, %d, %d, want one", layers[1].place.Idx, layers[2].place.Idx, layers[3].place.Idx)
	}
	if layers[0].place.Idx == layers[1].place.Idx {
		t.Errorf("a layer and the layers nested in it share page %d", layers[0].place.Idx)
	}
	if len(pages) != 3 {
		t.Errorf("%d pages, want 3 (the outer layer, its children, the next outer layer)", len(pages))
	}
}
