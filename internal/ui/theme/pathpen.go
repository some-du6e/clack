package theme

import (
	"image"
	"math"
	"sync"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// PathPen is a drop-in for clip.Path that dedupes paths by content.
//
// Gio's GPU renderer caches a path's tessellation and vertex upload by op
// identity. A shape rebuilt into the frame's ops every frame (icons,
// borders) gets a new identity each time, so its clip.Stroke was
// re-tessellated on the CPU every frame: ~12k allocations and a few MB of
// garbage per idle frame. PathPen records into a scratch buffer and hashes
// the commands; End returns the PathSpec first recorded for that content,
// so the op identity stays the same across frames and the GPU cache hits.
//
// Build shapes in local coordinates (translate with op.Offset), or every
// position is a new shape. Use on the UI goroutine.
type PathPen struct {
	p   clip.Path
	ops *op.Ops
	h   uint64
}

const (
	penOffset = 14695981039346656037
	penPrime  = 1099511628211
	// maxPenPaths bounds the cache; past it the cache starts over.
	maxPenPaths = 4096
)

var pens struct {
	mu    sync.Mutex
	paths map[uint64]penEntry
	free  []*op.Ops
}

type penEntry struct {
	spec clip.PathSpec
	ops  *op.Ops // owns the recording
}

func (q *PathPen) mix(v uint64) { q.h = (q.h ^ v) * penPrime }

func (q *PathPen) pt(p f32.Point) {
	q.mix(uint64(math.Float32bits(p.X)))
	q.mix(uint64(math.Float32bits(p.Y)))
}

// Begin starts a path. ops is ignored (the pen records into its own buffer);
// it is there so PathPen replaces clip.Path without other changes.
func (q *PathPen) Begin(_ *op.Ops) {
	pens.mu.Lock()
	if n := len(pens.free); n > 0 {
		q.ops = pens.free[n-1]
		pens.free = pens.free[:n-1]
	} else {
		q.ops = new(op.Ops)
	}
	pens.mu.Unlock()
	q.ops.Reset()
	q.p.Begin(q.ops)
	q.h = penOffset
}

func (q *PathPen) MoveTo(to f32.Point) { q.mix(1); q.pt(to); q.p.MoveTo(to) }
func (q *PathPen) LineTo(to f32.Point) { q.mix(2); q.pt(to); q.p.LineTo(to) }
func (q *PathPen) QuadTo(ctrl, to f32.Point) {
	q.mix(3)
	q.pt(ctrl)
	q.pt(to)
	q.p.QuadTo(ctrl, to)
}
func (q *PathPen) CubeTo(c0, c1, to f32.Point) {
	q.mix(4)
	q.pt(c0)
	q.pt(c1)
	q.pt(to)
	q.p.CubeTo(c0, c1, to)
}
func (q *PathPen) ArcTo(f1, f2 f32.Point, angle float32) {
	q.mix(5)
	q.pt(f1)
	q.pt(f2)
	q.mix(uint64(math.Float32bits(angle)))
	q.p.ArcTo(f1, f2, angle)
}
func (q *PathPen) Close() { q.mix(6); q.p.Close() }

// End finishes the path and returns the cached spec for its content.
func (q *PathPen) End() clip.PathSpec {
	spec := q.p.End()
	pens.mu.Lock()
	defer pens.mu.Unlock()
	if e, ok := pens.paths[q.h]; ok {
		pens.free = append(pens.free, q.ops) // the scratch recording is discarded
		q.ops = nil
		return e.spec
	}
	if pens.paths == nil || len(pens.paths) >= maxPenPaths {
		pens.paths = make(map[uint64]penEntry, 256)
	}
	pens.paths[q.h] = penEntry{spec: spec, ops: q.ops}
	q.ops = nil
	return spec
}

// RRectPath is the outline of a w×h rounded rectangle at the origin,
// cached by size and radius (for borders drawn every frame).
func RRectPath(w, h, radius int) clip.PathSpec {
	k := uint64(penOffset)
	for _, v := range [...]int{7, w, h, radius} {
		k = (k ^ uint64(v)) * penPrime
	}
	pens.mu.Lock()
	defer pens.mu.Unlock()
	if e, ok := pens.paths[k]; ok {
		return e.spec
	}
	if pens.paths == nil || len(pens.paths) >= maxPenPaths {
		pens.paths = make(map[uint64]penEntry, 256)
	}
	ops := new(op.Ops)
	spec := clip.UniformRRect(image.Rect(0, 0, w, h), radius).Path(ops)
	pens.paths[k] = penEntry{spec: spec, ops: ops}
	return spec
}
