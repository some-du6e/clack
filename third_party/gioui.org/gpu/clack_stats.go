// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"encoding/binary"
	"image"
	"math"
	"unsafe"

	"gioui.org/gpu/internal/driver"
)

// Clack patch 36: what one frame cost the renderer and the graphics API.

// FrameStats counts what the renderer did for a frame.
type FrameStats struct {
	// Blits are ops drawn with no clip path, Covers ops drawn through a
	// path's coverage, Intersections the ops clipped by two paths or more
	// (each needs a pass of its own over every path it is clipped by).
	Blits, Covers, Intersections int
	// StencilPaths are the paths stenciled into the coverage atlas.
	StencilPaths int
	// PathsBuilt are the paths whose vertices the frame built and
	// uploaded (a vertex buffer each), PathBytes their size: a path the
	// cache held from the frame before (the same op, see clip.PathSpec)
	// costs neither.
	PathsBuilt int
	PathBytes  int64
	// Draws are the draw calls, Passes the render passes.
	Draws, Passes int
	// APICalls are the calls into the graphics API (Metal's render
	// commands and other calls, COM calls into Direct3D 11, calls into
	// OpenGL), Skipped the binds its state cache left out, Crossings the
	// calls from Go into C or the system they took (Metal runs a render
	// pass's commands in one); all -1 where the backend doesn't count.
	APICalls, Skipped, Crossings int64
	// Pixels: StencilPx the stencil fragments (from the quads of each path,
	// in its stencil region; with ClackStatsDetail only, else -1), CoverPx
	// the pixels covered through a path, BlitPx the pixels blitted,
	// IntersectPx the fragments of the intersection passes, ClearPx the
	// atlas and layer pixels cleared for their passes.
	StencilPx, CoverPx, BlitPx, IntersectPx, ClearPx int64
	// AtlasPx is the size of the coverage and intersection atlases,
	// LayerPx of the opacity layers' pages; LayerPages are the layer
	// pages the frame drew into.
	AtlasPx, LayerPx int64
	LayerPages       int
	// Partial is whether the frame was drawn only where it differs from
	// the last (Clack patch 46), in DamageRects rectangles of DamagePx
	// pixels; CopyPx are the pixels copied: the frame texture into the
	// window's target, the clear colour into a partial frame's damage.
	Partial     bool
	DamageRects int
	DamagePx    int64
	CopyPx      int64
	// PackWork is the free spaces the coverage atlas's packer looked at;
	// StickySlots the slots of its sticky sequence (partial frames), of
	// them StickyHoles holes. PathsTessellated are the clip paths whose
	// vertices the frame built on the CPU.
	PackWork                 int64
	StickySlots, StickyHoles int
	PathsTessellated         int
	// CopyChecked: the window's pixels were read back after the frame
	// texture's copy (ClackVerifyCopy); CopyMismatchPx of them differed
	// from the frame texture's.
	CopyChecked    bool
	CopyMismatchPx int64
}

// ClackStats reports the last frame's stats.
type ClackStats interface {
	ClackFrameStats() FrameStats
	// ClackStatsDetail turns the stencil fragment counts on (each path
	// keeps its quads' bounds on the CPU).
	ClackStatsDetail(on bool)
}

func (g *gpu) ClackFrameStats() FrameStats { return g.stats }

// ClackStateCaches turns the backends' state caches and Metal's command
// batching (Clack patch 37) on or off, for tests checking that they leave
// every pixel as it is. Call it between frames.
func ClackStateCaches(on bool) {
	driver.ClackStateCache = on
	driver.ClackBatchCommands = on
}

func (g *gpu) ClackStatsDetail(on bool) {
	if g.drawOps.statsDetail == on {
		return
	}
	g.drawOps.statsDetail = on
	// paths built before have no quad bounds: build them again
	g.drawOps.pathCache.release()
	g.drawOps.pathCache = newOpCache()
}

// statsBegin starts counting a frame.
func (g *gpu) statsBegin() {
	r := g.renderer
	r.stats = FrameStats{StencilPx: -1, APICalls: -1, Skipped: -1, Crossings: -1}
	if g.drawOps.statsDetail {
		r.stats.StencilPx = 0
	}
	if c, ok := g.ctx.(driver.CallCounter); ok {
		r.calls0 = c.ClackCalls()
	}
}

// statsEnd finishes the frame's counts.
func (g *gpu) statsEnd() {
	r := g.renderer
	st := r.stats
	if c, ok := g.ctx.(driver.CallCounter); ok {
		c1 := c.ClackCalls()
		st.APICalls, st.Skipped, st.Crossings = c1.API-r.calls0.API, c1.Skipped-r.calls0.Skipped, c1.Crossings-r.calls0.Crossings
	}
	for _, f := range r.pather.stenciler.fbos.fbos {
		st.AtlasPx += int64(f.size.X * f.size.Y)
	}
	for _, f := range r.pather.stenciler.intersections.fbos {
		st.AtlasPx += int64(f.size.X * f.size.Y)
	}
	for _, f := range r.layerFBOs.fbos {
		st.LayerPx += int64(f.size.X * f.size.Y)
	}
	st.LayerPages = len(r.layers.sizes)
	g.stats = st
}

// quadBounds are the regions a path's stencil quads cover, as stencil.vert
// draws them: each curve's x range and from its top down to its contour's
// bottom, a pixel more each way.
func quadBounds(verts []byte) []image.Rectangle {
	bo := binary.LittleEndian
	f := func(v []byte, off uintptr) float32 { return math.Float32frombits(bo.Uint32(v[off:])) }
	var vt vertex
	var out []image.Rectangle
	for i := 0; i+vertStride*4 <= len(verts); i += vertStride * 4 {
		v := verts[i : i+vertStride]
		fx, fy := f(v, unsafe.Offsetof(vt.FromX)), f(v, unsafe.Offsetof(vt.FromY))
		cx, cy := f(v, unsafe.Offsetof(vt.CtrlX)), f(v, unsafe.Offsetof(vt.CtrlY))
		tx, ty := f(v, unsafe.Offsetof(vt.ToX)), f(v, unsafe.Offsetof(vt.ToY))
		maxy := f(v, unsafe.Offsetof(vt.MaxY))
		minx := min(fx, cx, tx) - 1
		maxx := max(fx, cx, tx) + 1
		miny := min(fy, cy, ty) - 1
		out = append(out, image.Rect(int(math.Floor(float64(minx))), int(math.Floor(float64(miny))), int(math.Ceil(float64(maxx))), int(math.Ceil(float64(maxy+1)))))
	}
	return out
}

// stencilPx is the stencil fragments of a path with quads bounds placed at
// off and drawn in region.
func stencilPx(bounds []image.Rectangle, off image.Point, region image.Rectangle) int64 {
	var n int64
	for _, b := range bounds {
		b = b.Add(off).Intersect(region)
		n += int64(b.Dx() * b.Dy())
	}
	return n
}

func area(r image.Rectangle) int64 { return int64(r.Dx() * r.Dy()) }
