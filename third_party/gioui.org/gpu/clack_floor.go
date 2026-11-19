// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"cmp"
	"image"
	"math/rand"
	"slices"
	"sync/atomic"

	"gioui.org/gpu/internal/driver"
	"gioui.org/internal/f32color"
)

// Clack patch 46, for tests: the placement floor and a wrong texel.
//
// A path's coverage depends by a rounding on where its region lands in the
// coverage atlas: each region is stenciled through a viewport at its
// place, and the vertices' fp32 transform into it, then the rasterizer's
// snapping, round differently at another place. Any change to the
// packing (another set of paths stenciled, sticky places) moves
// anti-aliased edges by that rounding. ClackPackOrder packs the regions in
// an order of the seed's, so every path lands somewhere else: the floor a
// change to the packing has to stay within. ClackCanaryTexels writes a
// wrong coverage into a few texels, the kind of error a floor must still
// catch (a texel sampled from the wrong place, a stale region).

var clackFloor struct {
	packSeed atomic.Int64 // ClackPackOrder
	canary   atomic.Int64 // ClackCanaryTexels
}

// ClackPackOrder has frames whose regions Gio packs itself (no sticky
// places: partial frames off, or a texture target) pack them in an order
// of seed's instead of the ops' (0: the ops' order, Gio's). For tests.
func ClackPackOrder(seed int64) { clackFloor.packSeed.Store(seed) }

// ClackCanaryTexels has a whole frame's stencil pass write coverage one
// half into the centre texel of its n largest stencil regions (0: none),
// for tests: pixels a renderer error would get wrong.
func ClackCanaryTexels(n int) { clackFloor.canary.Store(int64(n)) }

// packShuffled places ops' regions with pk in seed's order.
func packShuffled(pk *packer, ops []*pathOp, seed int64) {
	var regs []stickyRegion
	for _, p := range ops {
		for i := -1; i < len(p.more); i++ {
			regs = append(regs, stickyRegion{p, i})
		}
	}
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(regs), func(a, b int) { regs[a], regs[b] = regs[b], regs[a] })
	for _, sr := range regs {
		sr.setPlace(stickyAdd(pk, sr.clip().Size()))
	}
}

// canaryRegions are the n largest stencil regions of ops (3×3 at least),
// for ClackCanaryTexels; nil for none.
func canaryRegions(ops []*pathOp, n int) map[stickyRegion]bool {
	if n <= 0 {
		return nil
	}
	var regs []stickyRegion
	for _, p := range ops {
		for i := -1; i < len(p.more); i++ {
			if sz := (stickyRegion{p, i}).clip().Size(); sz.X >= 3 && sz.Y >= 3 {
				regs = append(regs, stickyRegion{p, i})
			}
		}
	}
	slices.SortStableFunc(regs, func(a, b stickyRegion) int {
		return cmp.Compare(area(b.clip()), area(a.clip()))
	})
	m := make(map[stickyRegion]bool)
	for _, sr := range regs[:min(n, len(regs))] {
		m[sr] = true
	}
	return m
}

// canaryTexel writes coverage one half into the centre texel of the region
// clip at place, inside the stencil pass, and gives the pass back its
// state.
func (r *renderer) canaryTexel(clip image.Rectangle, place placement) {
	c := place.Pos.Add(clip.Size().Div(2))
	r.fill(driver.TextureFormatFloat, place.Pos, clip.Size(), image.Rectangle{Min: c, Max: c.Add(image.Pt(1, 1))}, f32color.RGBA{R: 0.5})
	r.scissor(image.Rectangle{})
	r.ctx.BindPipeline(r.pather.stenciler.pipeline.pipeline.pipeline)
	r.ctx.BindIndexBuffer(r.pather.stenciler.indexBuf)
}
