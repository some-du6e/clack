// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"encoding/binary"
	"hash/maphash"
	"image"
	"log"
	"math"
	"sync/atomic"

	"gioui.org/gpu/internal/driver"
	"gioui.org/internal/byteslice"
	"gioui.org/internal/f32color"
	"gioui.org/shader"
	"gioui.org/shader/gio"
)

// Clack patch 46: partial frames.
//
// Gio drew every frame whole: a caret's blink, a spinner's turn, an
// animated emoji's next frame cost the same stencil, cover and API calls
// as a frame where everything changed (on llvmpipe, Clack's focused
// composer cost 55% of a core for its blinks alone). Chromium repaints only
// the damaged rectangle. Here a frame is compared with the last, op by op,
// and drawn again only where they differ:
//
//   - Every op the frame draws gets a signature (sigOps): a hash of all
//     that decides its pixels (its material, its texture, each clip path
//     it's drawn through with the path's geometry, offset, stencil region
//     and place in the atlas, the opacity layers around it and their
//     places), and its clip, the only pixels its quad touches. An opacity
//     layer's composite is an entry of its own over the layer's bounds.
//   - The last frame's signatures and this one's are diffed (Myers). The
//     ops either one has and the other doesn't are the damage: everywhere
//     else the same ops cover each pixel, in the same order, with the same
//     inputs, so the pixel is what the last frame left.
//   - The damage, merged into a few disjoint rectangles, is drawn again
//     with every pass exactly as a whole frame does it (the same packing of
//     the atlases, the same viewports, the same quads), each draw only
//     scissored to the damage. A scissor discards fragments and changes
//     nothing in the ones it keeps, so the result is the whole frame's
//     pixels, bit for bit: the stencil coverage of a path depends on where
//     it lands in the atlas (by a rounding), and lands where the whole
//     frame puts it. Only the atlas texels and layer texels a damaged pixel
//     reads are cleared (to 0, 1 for intersections) and drawn.
//   - A window's frame draws into the frame texture, which keeps its
//     pixels (a window's buffers come back undefined, and Metal's flip
//     through several), and is copied whole into the target after every
//     frame. A frame drawn into a texture target (gpu/headless) is drawn
//     whole, as Gio drew it, unless a test asks for partial frames
//     (ClackPartialTextures: into the texture itself; ClackFrameTexture:
//     through the frame texture and its copy, as a window's). A partial
//     frame's damage starts from the
//     clear colour, copied in from a texture cleared to it (clearTex): a
//     clear of the same format the whole frame's pass does, so no colour
//     conversion can differ.
//
// A frame is drawn whole when there is no last frame to start from (the
// first, a new frame texture, another target), the viewport or the clear
// colour changed, the damage is too much (partialMaxArea of the viewport,
// or more than partialMaxEdits ops added and dropped), partial frames are
// off (ClackPartialFrames) or the device can't scissor and copy
// (driver.ClackPartial: Vulkan, a shared OpenGL context, OpenGL without
// glBlitFramebuffer).

// clackPartial are the switches; a window's GPU reads them on its own
// goroutine.
var clackPartial struct {
	off      atomic.Bool // ClackPartialFrames(false)
	textures atomic.Bool // ClackPartialTextures(true)
	frameTex atomic.Bool // ClackFrameTexture(true)
	whole    atomic.Bool // ClackPartialWhole(true)
	verify   atomic.Bool // ClackVerifyCopy(true)
	epoch    atomic.Uint64
}

// ClackVerifyCopy has a window's GPU read its target back after copying the
// frame texture into it, and count the pixels that differ
// (FrameStats.CopyChecked, CopyMismatchPx), for tests: the window's pixels
// are the frame's. The target must be readable: on Metal the test makes
// the layer's drawables so (CLACK_METAL_READBACK; a window's are
// framebufferOnly).
func ClackVerifyCopy(on bool) { clackPartial.verify.Store(on) }

// ClackPartialWhole has frames drawn whole while what partial frames keep
// (the atlas places, the ops' signatures) goes on as it would: the whole
// frame a partial frame must equal, for tests. Unlike the other switches
// it doesn't start partial frames afresh.
func ClackPartialWhole(on bool) { clackPartial.whole.Store(on) }

// ClackPartialReset has every GPU's next frame start afresh: no last frame
// to compare with, the atlas packed anew. For tests.
func ClackPartialReset() { clackPartial.epoch.Add(1) }

// ClackPartialFrames turns partial frames on (the default) or off for
// every frame. Safe from any goroutine; it applies from the next frame.
func ClackPartialFrames(on bool) { clackPartial.off.Store(!on) }

// ClackPartialTextures makes frames drawn into a texture target
// (gpu/headless) partial frames, drawn into the texture itself, for tests;
// they're whole frames by default.
func ClackPartialTextures(on bool) { clackPartial.textures.Store(on) }

// ClackFrameTexture makes frames drawn into a texture target go through
// the frame texture and a copy into the target, as a window's frames do,
// partial frames included, for tests.
func ClackFrameTexture(on bool) { clackPartial.frameTex.Store(on) }

const (
	// partialMaxRects is how many disjoint rectangles the damage is kept
	// in; more are merged.
	partialMaxRects = 8
	// partialMaxEdits is how many ops a frame may add and drop against the
	// last before it is drawn whole.
	partialMaxEdits = 128
	// partialMaxArea is how much of the viewport, in 1/1024ths, the damage
	// may cover before the frame is drawn whole.
	partialMaxArea = 512
	// clearTexDim bounds the clear texture's sides; bigger damage is
	// cleared in tiles.
	clearTexDim = 512
)

// partialMode is the texture targets' switches a frame was drawn with.
type partialMode struct{ textures, frameTex bool }

// partialFrames is what the GPU keeps between frames for partial frames.
type partialFrames struct {
	// the switches and ClackPartialReset's epoch the state is from;
	// logged: that the device draws every frame whole
	mode   partialMode
	epoch  uint64
	logged bool
	// atlas is the coverage atlas's sticky places (clack_sticky.go)
	atlas stickyAtlas
	// frameTex keeps a window's frames (frameSize); nil until one needs it
	frameTex  driver.Texture
	frameSize image.Point
	// clearTex is cleared to clearCol (clearOK), for copies into a partial
	// frame's damage
	clearTex  driver.Texture
	clearSize image.Point
	clearCol  f32color.RGBA
	clearOK   bool
	// the last frame drawn: valid once its pixels are in what partial
	// frames draw into (the frame texture, or a texture target: key); its
	// viewport and clear colour, its ops' signatures (haveSigs), whether it
	// was whole
	valid     bool
	key       any
	viewport  image.Point
	color     f32color.RGBA
	sigs      []opSig
	haveSigs  bool
	lastWhole bool
	// scratch: this frame's signatures, an op's words, the layers' chain
	// of words, Myers' trace
	cur   []opSig
	buf   []byte
	chain []byte
	diff  diffScratch
}

// opSig is what an op draws (Clack patch 46): h hashes all that decides
// its pixels except where they are, clip, the rectangle its quad covers.
type opSig struct {
	h    [2]uint64
	clip image.Rectangle
}

// sigSeeds make the two independent hashes of a signature.
var sigSeeds = [2]maphash.Seed{maphash.MakeSeed(), maphash.MakeSeed()}

// pathHash is the signature of a clip path's geometry: the path data as
// recorded and how it's built into vertices (outline or stroke width, the
// transform with its sub-pixel offset). The same hash means the same
// vertices.
func pathHash(data []byte, k opKey) [2]uint64 {
	var hdr [36]byte
	bo := binary.LittleEndian
	if k.outline {
		hdr[0] = 1
	}
	for i, f := range [...]float32{k.strokeWidth, k.sx, k.hx, k.sy, k.hy, k.ox, k.oy} {
		bo.PutUint32(hdr[4+4*i:], math.Float32bits(f))
	}
	var h [2]uint64
	for i, s := range sigSeeds {
		var mh maphash.Hash
		mh.SetSeed(s)
		mh.Write(hdr[:])
		mh.Write(data)
		h[i] = mh.Sum64()
	}
	return h
}

func appendU32(b []byte, vs ...uint32) []byte {
	for _, v := range vs {
		b = binary.LittleEndian.AppendUint32(b, v)
	}
	return b
}

func appendInts(b []byte, vs ...int) []byte {
	for _, v := range vs {
		b = binary.LittleEndian.AppendUint64(b, uint64(int64(v)))
	}
	return b
}

func appendF32(b []byte, vs ...float32) []byte {
	for _, v := range vs {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
	}
	return b
}

func appendRect(b []byte, r image.Rectangle) []byte {
	return appendInts(b, r.Min.X, r.Min.Y, r.Max.X, r.Max.Y)
}

func pageSize(fbos []FBO, idx int) image.Point {
	if idx >= 0 && idx < len(fbos) {
		return fbos[idx].size
	}
	return image.Point{}
}

func hashSig(b []byte, clip image.Rectangle) opSig {
	return opSig{
		h:    [2]uint64{maphash.Bytes(sigSeeds[0], b), maphash.Bytes(sigSeeds[1], b)},
		clip: clip,
	}
}

// sigOps appends the signatures of the frame's ops, in the order they're
// drawn: the ops, and before the first op of each opacity layer the
// layer's composite (over its bounds). The ops inside a layer carry their
// layers' words. Call it once the frame is packed, its pages sized and its
// textures uploaded, before drawLayers replaces the layers' ops.
func (g *gpu) sigOps(out []opSig) []opSig {
	p := &g.partial
	r := g.renderer
	ops := g.drawOps.imageOps
	layers := g.drawOps.layers
	cover := r.pather.stenciler.fbos.fbos
	inter := r.pather.stenciler.intersections.fbos
	// the layers around the op: stack holds each one's end and where its
	// words start in chain
	type open struct{ end, at int }
	var stackArr [16]open
	stack := stackArr[:0]
	p.chain = p.chain[:0]
	li := 0
	for i := range ops {
		for len(stack) > 0 && stack[len(stack)-1].end <= i {
			p.chain = p.chain[:stack[len(stack)-1].at]
			stack = stack[:len(stack)-1]
		}
		for li < len(layers) && layers[li].opStart <= i {
			l := &layers[li]
			li++
			if l.opStart < i || l.opStart >= l.opEnd || l.opEnd > len(ops) {
				continue // drawLayers skips it: its ops are drawn as they are
			}
			// the composite, inside the layers around it
			b := append(p.buf[:0], 'L')
			b = appendF32(b, l.opacity)
			b = appendRect(b, l.clip)
			b = appendInts(b, l.place.Idx, l.place.Pos.X, l.place.Pos.Y)
			sz := pageSize(r.layerFBOs.fbos, l.place.Idx)
			b = appendInts(b, sz.X, sz.Y)
			n := len(b)
			b = append(b, p.chain...)
			out = append(out, hashSig(b, l.clip))
			stack = append(stack, open{end: l.opEnd, at: len(p.chain)})
			p.chain = append(p.chain, b[:n]...)
			p.buf = b
		}
		img := &ops[i]
		m := &img.material
		b := append(p.buf[:0], 'O', byte(img.clipType), byte(m.material))
		if m.opaque {
			b = append(b, 1)
		} else {
			b = append(b, 0)
		}
		b = appendF32(b, m.color.R, m.color.G, m.color.B, m.color.A,
			m.color1.R, m.color1.G, m.color1.B, m.color1.A,
			m.color2.R, m.color2.G, m.color2.B, m.color2.A, m.opacity)
		t1, t2, t3, t4, t5, t6 := m.uvTrans.Elems()
		b = appendF32(b, t1, t2, t3, t4, t5, t6)
		b = binary.LittleEndian.AppendUint64(b, m.texID)
		if img.clipType == clipTypeIntersection {
			b = appendInts(b, img.place.Idx, img.place.Pos.X, img.place.Pos.Y)
			sz := pageSize(inter, img.place.Idx)
			b = appendInts(b, sz.X, sz.Y)
		}
		for pp := img.path; pp != nil; pp = pp.parent {
			if !pp.path || pp.covers(img.clip) {
				continue
			}
			pclip, place := pp.region(img.clip)
			b = append(b, 'P')
			b = binary.LittleEndian.AppendUint64(b, pp.hash[0])
			b = binary.LittleEndian.AppendUint64(b, pp.hash[1])
			b = appendF32(b, pp.off.X, pp.off.Y)
			b = appendRect(b, pclip)
			b = appendInts(b, place.Idx, place.Pos.X, place.Pos.Y)
			sz := pageSize(cover, place.Idx)
			b = appendInts(b, sz.X, sz.Y)
		}
		b = append(b, p.chain...)
		out = append(out, hashSig(b, img.clip))
		p.buf = b
	}
	return out
}

// clackPartialDevice is a device that can draw partial frames.
type clackPartialDevice interface {
	driver.Device
	driver.ClackPartial
}

// partialTarget is where a frame draws: pass is its render pass's target,
// tex the texture its pixels stay in (copies go into it).
type partialTarget struct {
	pass, tex driver.Texture
	// copy: the frame texture, copied into the target (defFBO) when done
	copy bool
}

// partialOn reports whether the frame may be partial (Clack patch 46):
// partial frames are on, the device can draw them, and the target is a
// window's or a test asked for texture targets'. Its paths are then packed
// sticky. A switch changed (or ClackPartialReset) starts afresh. Call it
// before the frame is packed.
func (g *gpu) partialOn(target RenderTarget, viewport image.Point) bool {
	p := &g.partial
	dev, ok := g.ctx.(clackPartialDevice)
	_, isTex := target.(driver.Texture)
	mode := partialMode{textures: clackPartial.textures.Load(), frameTex: clackPartial.frameTex.Load()}
	capable := ok && dev.ClackPartialOK()
	if !capable && !isTex && !p.logged {
		// a window drawing every frame whole: Vulkan (X11 with the GPU
		// blocklist ignored), a shared or old OpenGL context
		p.logged = true
		log.Printf("gio: this window's frames are drawn whole: its %T device can't draw partial frames", g.ctx)
	}
	on := !clackPartial.off.Load() && capable && viewport.X > 0 && viewport.Y > 0 &&
		(!isTex || mode.textures || mode.frameTex)
	if ep := clackPartial.epoch.Load(); !on || p.mode != mode || p.epoch != ep {
		g.partialRelease()
		p.mode, p.epoch = mode, ep
	}
	return on
}

// partialBegin decides where the frame draws and whether whole (dmg nil)
// or only in its damage (Clack patch 46). Call it once the frame is
// packed, its pages sized and its textures uploaded.
//
// A window's frame goes through the frame texture, so the next frame can
// be partial, but a whole frame right after a whole frame (a scroll, a
// reflow) draws straight into the target: going through the texture would
// cost a copy of the window for nothing. The texture is then stale, and
// when frames get small again one whole frame draws into it first. While
// the window's size changes, frames draw straight into the target too, and
// no window-sized texture is made for each new size.
func (g *gpu) partialBegin(on bool, target RenderTarget, defFBO driver.Texture, viewport image.Point) (out partialTarget, dmg []image.Rectangle) {
	p := &g.partial
	out = partialTarget{pass: defFBO}
	if !on {
		return out, nil
	}
	tex, isTex := target.(driver.Texture)
	direct := isTex && !p.mode.frameTex // a texture target keeps its pixels
	var key any = target
	if !direct {
		key = frameTexKey{}
	}
	resized := p.haveSigs && p.viewport != viewport
	same := p.haveSigs && p.key == key && p.viewport == viewport && g.drawOps.clear && p.color == g.drawOps.clearColor &&
		!clackPartial.whole.Load()
	p.cur = g.sigOps(p.cur[:0])
	small := false
	if same {
		if d, ok := damage(p.sigs, p.cur, viewport, &p.diff); ok {
			dmg, small = d, true
		}
	}
	p.sigs, p.cur = p.cur, p.sigs
	p.haveSigs = true
	p.key, p.viewport, p.color = key, viewport, g.drawOps.clearColor
	switch {
	case direct:
		g.releaseFrameTex()
		out.tex = tex
		if !p.valid {
			dmg = nil
		}
	case resized || (!small && p.lastWhole):
		// straight into the target: no copy, the frame texture stale (and
		// let go of at another size)
		if p.frameTex != nil && p.frameSize != viewport {
			g.releaseFrameTex()
		}
		p.valid = false
		dmg = nil
	default:
		ft := g.frameTexture(viewport)
		if ft == nil {
			g.partialRelease()
			return partialTarget{pass: defFBO}, nil
		}
		out = partialTarget{pass: ft, tex: ft, copy: true}
		if !p.valid {
			dmg = nil
		}
	}
	if dmg != nil && len(dmg) > 0 && !g.clearTexture(dmg) {
		dmg = nil // no clear texture: the frame whole
	}
	p.valid = false // until it's drawn (partialEnd)
	p.lastWhole = dmg == nil
	st := &g.renderer.stats
	if dmg != nil {
		st.Partial = true
		st.DamageRects = len(dmg)
		for _, d := range dmg {
			st.DamagePx += area(d)
		}
	}
	return out, dmg
}

// frameTexKey is what a window's frames draw into (the frame texture),
// for partialFrames.key.
type frameTexKey struct{}

// partialEnd finishes a frame drawn into out: the frame texture is copied
// into the target, and the frame is the one the next starts from. A frame
// drawn straight into a window's target leaves the frame texture stale.
func (g *gpu) partialEnd(defFBO driver.Texture, out partialTarget, viewport image.Point) {
	if out.tex == nil {
		return
	}
	if out.copy {
		if dev := g.ctx.(driver.ClackPartial); dev.ClackCopyDraws() {
			g.renderer.drawCopy(defFBO, out.tex, viewport)
		} else {
			dev.ClackCopyToTarget(defFBO, out.tex, viewport)
		}
		g.renderer.stats.CopyPx += int64(viewport.X * viewport.Y)
		if clackPartial.verify.Load() {
			g.verifyCopy(defFBO, out.tex, viewport)
		}
	}
	g.partial.valid = true
}

// verifyCopy reads back target and the frame texture src it was copied
// from over the viewport, and counts the pixels that differ
// (ClackVerifyCopy).
func (g *gpu) verifyCopy(target, src driver.Texture, viewport image.Point) {
	n := viewport.X * viewport.Y * 4
	a, b := make([]byte, n), make([]byte, n)
	r := image.Rectangle{Max: viewport}
	if target.ReadPixels(r, a, viewport.X*4) != nil || src.ReadPixels(r, b, viewport.X*4) != nil {
		return
	}
	st := &g.renderer.stats
	st.CopyChecked = true
	for i := 0; i < n; i += 4 {
		if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] || a[i+3] != b[i+3] {
			st.CopyMismatchPx++
		}
	}
}

// drawCopy draws src into target over the whole viewport (Metal's frame
// copy: a drawable is framebufferOnly): one quad sampling src at its texel
// centres (nearest), with blending off, through the window's transform as
// the frame drew into src. Exact for 8-bit sRGB: decoding a texel and
// encoding it again gives the texel back.
func (r *renderer) drawCopy(target, src driver.Texture, viewport image.Point) {
	pl := r.copyPipeline()
	r.ctx.BeginRenderPass(target, driver.LoadDesc{Action: driver.LoadActionInvalidate})
	r.stats.Passes++
	r.ctx.Viewport(0, 0, viewport.X, viewport.Y)
	r.ctx.BindPipeline(pl.pipeline)
	r.ctx.BindVertexBuffer(r.blitter.quadVerts, 0)
	r.ctx.BindTexture(0, src)
	u := r.copyUniforms
	u.transform = [4]float32{1, 1, 0, 0}
	u.uvTransformR1 = [4]float32{1, 0, 0, 0}
	u.uvTransformR2 = [4]float32{0, 1, 0, 0}
	u.opacity = 1
	u.fbo = 0
	pl.UploadUniforms(r.ctx)
	r.ctx.DrawArrays(0, 4)
	r.stats.Draws++
	r.ctx.EndRenderPass()
}

// copyPipeline is drawCopy's pipeline (made on first use).
func (r *renderer) copyPipeline() *pipeline {
	if r.copyPipe != nil {
		return r.copyPipe
	}
	r.copyUniforms = new(blitTexUniforms)
	vsh, err := r.ctx.NewVertexShader(gio.Shader_blit_vert)
	if err != nil {
		panic(err)
	}
	defer vsh.Release()
	fsh, err := r.ctx.NewFragmentShader(gio.Shader_blit_frag[materialTexture])
	if err != nil {
		panic(err)
	}
	defer fsh.Release()
	pipe, err := r.ctx.NewPipeline(driver.PipelineDesc{
		VertexShader:   vsh,
		FragmentShader: fsh,
		BlendDesc:      driver.BlendDesc{SrcFactor: driver.BlendFactorOne, DstFactor: driver.BlendFactorZero},
		VertexLayout: driver.VertexLayout{
			Inputs: []driver.InputDesc{
				{Type: shader.DataTypeFloat, Size: 2, Offset: 0},
				{Type: shader.DataTypeFloat, Size: 2, Offset: 4 * 2},
			},
			Stride: 4 * 4,
		},
		PixelFormat: driver.TextureFormatOutput,
		Topology:    driver.TopologyTriangleStrip,
	})
	if err != nil {
		panic(err)
	}
	r.copyPipe = &pipeline{pipe, newUniformBuffer(r.ctx, byteslice.View(r.copyUniforms))}
	return r.copyPipe
}

// frameTexture is the frame texture at size sz, made again for a new size
// (and then not valid).
func (g *gpu) frameTexture(sz image.Point) driver.Texture {
	p := &g.partial
	if p.frameTex != nil && p.frameSize == sz {
		return p.frameTex
	}
	g.releaseFrameTex()
	t, err := g.ctx.NewTexture(driver.TextureFormatOutput, sz.X, sz.Y, driver.FilterNearest, driver.FilterNearest,
		driver.BufferBindingFramebuffer|driver.BufferBindingTexture)
	if err != nil {
		return nil
	}
	p.frameTex, p.frameSize = t, sz
	p.valid = false
	return t
}

func (g *gpu) releaseFrameTex() {
	p := &g.partial
	if p.frameTex != nil {
		p.frameTex.Release()
		p.frameTex = nil
		p.valid = false
	}
}

// ClackFrameReleaser is a GPU that can let go of what partial frames keep
// (Clack patch 46): its frame texture and clear texture, the last frame's
// signatures and the sticky atlas places. Its next frame is whole.
type ClackFrameReleaser interface {
	ClackReleaseFrames()
}

// ClackReleaseFrames implements ClackFrameReleaser: for a window that draws
// nothing for now (hidden, minimized, occluded), with its context current.
func (g *gpu) ClackReleaseFrames() { g.partialRelease() }

// partialRelease drops what partial frames keep: frames are drawn whole,
// as Gio does.
func (g *gpu) partialRelease() {
	p := &g.partial
	g.releaseFrameTex()
	if p.clearTex != nil {
		p.clearTex.Release()
		p.clearTex = nil
		p.clearOK = false
	}
	p.valid, p.haveSigs, p.lastWhole = false, false, false
	p.sigs, p.cur = p.sigs[:0], p.cur[:0]
	p.atlas.reset()
}

// clearTexture makes sure clearTex holds the clear colour and is big
// enough for the damage's tiles (partialClear); false if it can't be made.
func (g *gpu) clearTexture(dmg []image.Rectangle) bool {
	p := &g.partial
	var need image.Point
	for _, d := range dmg {
		need.X = max(need.X, d.Dx())
		need.Y = max(need.Y, d.Dy())
	}
	need.X, need.Y = min(need.X, clearTexDim), min(need.Y, clearTexDim)
	col := g.drawOps.clearColor
	if p.clearTex == nil || p.clearSize.X < need.X || p.clearSize.Y < need.Y {
		if p.clearTex != nil {
			p.clearTex.Release()
			p.clearTex = nil
		}
		sz := image.Pt(max(need.X, p.clearSize.X, 64), max(need.Y, p.clearSize.Y, 64))
		sz.X, sz.Y = min(sz.X, clearTexDim), min(sz.Y, clearTexDim)
		t, err := g.ctx.NewTexture(driver.TextureFormatOutput, sz.X, sz.Y, driver.FilterNearest, driver.FilterNearest,
			driver.BufferBindingFramebuffer|driver.BufferBindingTexture)
		if err != nil {
			p.clearSize, p.clearOK = image.Point{}, false
			return false
		}
		p.clearTex, p.clearSize, p.clearOK = t, sz, false
	}
	if !p.clearOK || p.clearCol != col {
		g.ctx.BeginRenderPass(p.clearTex, driver.LoadDesc{Action: driver.LoadActionClear, ClearColor: col})
		g.ctx.EndRenderPass()
		g.renderer.stats.Passes++
		g.renderer.stats.ClearPx += int64(p.clearSize.X * p.clearSize.Y)
		p.clearCol, p.clearOK = col, true
	}
	return true
}

// partialClear sets the damage of out's texture to the clear colour, as
// the whole frame's load action does: copied from clearTex (clearTexture),
// which a clear of the same format made. Before the frame's render pass:
// Metal copies in a blit encoder, OpenGL through framebuffer bindings.
func (g *gpu) partialClear(out partialTarget, dmg []image.Rectangle, viewport image.Point) {
	if len(dmg) == 0 {
		return
	}
	p := &g.partial
	flip := g.ctx.Caps().BottomLeftOrigin
	cs := p.clearSize
	for _, d := range dmg {
		t := d
		if flip {
			t.Min.Y, t.Max.Y = viewport.Y-d.Max.Y, viewport.Y-d.Min.Y
		}
		for y := t.Min.Y; y < t.Max.Y; y += cs.Y {
			for x := t.Min.X; x < t.Max.X; x += cs.X {
				w, h := min(cs.X, t.Max.X-x), min(cs.Y, t.Max.Y-y)
				g.ctx.CopyTexture(out.tex, image.Pt(x, y), p.clearTex, image.Rect(0, 0, w, h))
				g.renderer.stats.CopyPx += int64(w * h)
			}
		}
	}
}

// passRect is a damage rectangle in a pass: win in window coordinates,
// s where it is in the pass's target, in the coordinates Viewport takes.
type passRect struct {
	win, s image.Rectangle
}

// passScissor is where the part of d in region is in a pass that draws
// region at pos (an atlas region, an intersection, a layer).
func passScissor(d, region image.Rectangle, pos image.Point) image.Rectangle {
	return d.Intersect(region).Add(pos.Sub(region.Min))
}

// mainRects are the damage rectangles in the frame's own pass.
func (r *renderer) mainRects(dmg []image.Rectangle, viewport image.Point) []passRect {
	if dmg == nil {
		return nil
	}
	flip := r.ctx.Caps().BottomLeftOrigin
	out := make([]passRect, 0, len(dmg))
	for _, d := range dmg {
		s := d
		if flip {
			// the window pass draws with Y up from the bottom (windowTransform)
			s.Min.Y, s.Max.Y = viewport.Y-d.Max.Y, viewport.Y-d.Min.Y
		}
		out = append(out, passRect{win: d, s: s})
	}
	return out
}

// scissor restricts the open pass's draws to s (a partial frame's only).
func (r *renderer) scissor(s image.Rectangle) {
	r.ctx.(driver.ClackPartial).ClackScissor(s)
}

// fill sets the texels in s of a pass's region at pos of size sz to col,
// with blending off: a partial frame's clear of the atlas and layer
// texels it draws again (0 coverage, 1 for an intersection, transparent
// for a layer: exact in every format).
func (r *renderer) fill(format driver.TextureFormat, pos, sz image.Point, s image.Rectangle, col f32color.RGBA) {
	pl := r.fillPipeline(format)
	r.ctx.Viewport(pos.X, pos.Y, sz.X, sz.Y)
	r.scissor(s)
	r.ctx.BindPipeline(pl.pipeline)
	r.ctx.BindVertexBuffer(r.blitter.quadVerts, 0)
	u := r.fillUniforms
	u.color = col
	u.transform = [4]float32{1, 1, 0, 0}
	u.opacity = 1
	u.fbo = 1
	pl.UploadUniforms(r.ctx)
	r.ctx.DrawArrays(0, 4)
	r.stats.Draws++
	r.stats.ClearPx += area(s)
}

// fillPipeline is the fill pipeline for a format (made on first use).
func (r *renderer) fillPipeline(format driver.TextureFormat) *pipeline {
	i := 0
	if format == driver.TextureFormatSRGBA {
		i = 1
	}
	if r.fills[i] != nil {
		return r.fills[i]
	}
	if r.fillUniforms == nil {
		r.fillUniforms = new(blitColUniforms)
	}
	vsh, err := r.ctx.NewVertexShader(gio.Shader_blit_vert)
	if err != nil {
		panic(err)
	}
	defer vsh.Release()
	fsh, err := r.ctx.NewFragmentShader(gio.Shader_blit_frag[materialColor])
	if err != nil {
		panic(err)
	}
	defer fsh.Release()
	pipe, err := r.ctx.NewPipeline(driver.PipelineDesc{
		VertexShader:   vsh,
		FragmentShader: fsh,
		BlendDesc:      driver.BlendDesc{SrcFactor: driver.BlendFactorOne, DstFactor: driver.BlendFactorZero},
		VertexLayout: driver.VertexLayout{
			Inputs: []driver.InputDesc{
				{Type: shader.DataTypeFloat, Size: 2, Offset: 0},
				{Type: shader.DataTypeFloat, Size: 2, Offset: 4 * 2},
			},
			Stride: 4 * 4,
		},
		PixelFormat: format,
		Topology:    driver.TopologyTriangleStrip,
	})
	if err != nil {
		panic(err)
	}
	r.fills[i] = &pipeline{pipe, newUniformBuffer(r.ctx, byteslice.View(r.fillUniforms))}
	return r.fills[i]
}

// damage is where frame b differs from frame a, in at most
// partialMaxRects disjoint rectangles: the clips of the ops one has and
// the other doesn't, by a shortest edit script (Myers) between them. ok is
// false when there are too many edits or the damage is too big.
func damage(a, b []opSig, viewport image.Point, sc *diffScratch) (rects []image.Rectangle, ok bool) {
	// the common start and end
	s := 0
	for s < len(a) && s < len(b) && a[s] == b[s] {
		s++
	}
	e := 0
	for e < len(a)-s && e < len(b)-s && a[len(a)-1-e] == b[len(b)-1-e] {
		e++
	}
	a, b = a[s:len(a)-e], b[s:len(b)-e]
	var set rectSet
	if !sc.edits(a, b, partialMaxEdits, func(fromA bool, i int) {
		if fromA {
			set.add(a[i].clip)
		} else {
			set.add(b[i].clip)
		}
	}) {
		return nil, false
	}
	vp := image.Rectangle{Max: viewport}
	var px int64
	rects = make([]image.Rectangle, 0, len(set.r))
	for _, r := range set.r {
		if r = r.Intersect(vp); !r.Empty() {
			rects = append(rects, r)
			px += area(r)
		}
	}
	if px*1024 > area(vp)*partialMaxArea {
		return nil, false
	}
	return rects, true
}

// rectSet is a set of at most partialMaxRects disjoint rectangles covering
// every rectangle added.
type rectSet struct {
	r []image.Rectangle
}

func (s *rectSet) add(r image.Rectangle) {
	if r.Empty() {
		return
	}
	for i := 0; i < len(s.r); i++ {
		if s.r[i].Overlaps(r) {
			r = r.Union(s.r[i])
			s.r = append(s.r[:i], s.r[i+1:]...)
			i = -1 // the union may overlap ones already passed
		}
	}
	s.r = append(s.r, r)
	for len(s.r) > partialMaxRects {
		// merge the two whose union wastes the least
		bi, bj, best := 0, 1, int64(-1)
		for i := range s.r {
			for j := i + 1; j < len(s.r); j++ {
				w := area(s.r[i].Union(s.r[j])) - area(s.r[i]) - area(s.r[j])
				if best < 0 || w < best {
					bi, bj, best = i, j, w
				}
			}
		}
		u := s.r[bi].Union(s.r[bj])
		s.r = append(s.r[:bj], s.r[bj+1:]...)
		s.r = append(s.r[:bi], s.r[bi+1:]...)
		s.add(u)
	}
}

// diffScratch is Myers' working memory, kept between frames.
type diffScratch struct {
	v     []int
	trace []int // every step's v[-d..d], one after the other
	at    []int // where each step's slice starts in trace
}

// edits finds a shortest edit script turning a into b (Myers' greedy
// O((N+M)D) algorithm) and calls f for each op of a it drops and each op
// of b it adds. It reports false when more than max edits are needed.
func (sc *diffScratch) edits(a, b []opSig, maxD int, f func(fromA bool, i int)) bool {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return true
	}
	if n+m <= maxD && (n == 0 || m == 0) {
		for i := range a {
			f(true, i)
		}
		for i := range b {
			f(false, i)
		}
		return true
	}
	off := maxD + 1
	if cap(sc.v) < 2*off+1 {
		sc.v = make([]int, 2*off+1)
	}
	v := sc.v[:2*off+1]
	clear(v)
	sc.trace, sc.at = sc.trace[:0], sc.at[:0]
	final := -1
	for d := 0; d <= maxD && final < 0; d++ {
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1] // down: b[y-1] added
			} else {
				x = v[off+k-1] + 1 // right: a[x-1] dropped
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[off+k] = x
			if x >= n && y >= m {
				final = d
			}
		}
		sc.at = append(sc.at, len(sc.trace))
		sc.trace = append(sc.trace, v[off-d:off+d+1]...)
	}
	if final < 0 {
		return false
	}
	// back from (n, m) through each step's furthest points
	x, y := n, m
	for d := final; d > 0; d-- {
		prev := sc.trace[sc.at[d-1] : sc.at[d-1]+2*(d-1)+1] // v[-(d-1)..d-1]
		pv := func(k int) int { return prev[k+d-1] }
		k := x - y
		var pk int
		if k == -d || (k != d && pv(k-1) < pv(k+1)) {
			pk = k + 1
		} else {
			pk = k - 1
		}
		px := pv(pk)
		py := px - pk
		if pk == k+1 {
			f(false, py) // b[py] added: (px, py) → (px, py+1)
		} else {
			f(true, px) // a[px] dropped: (px, py) → (px+1, py)
		}
		x, y = px, py
	}
	return true
}
