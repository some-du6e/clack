// SPDX-License-Identifier: Unlicense OR MIT

/*
Package gpu implements the rendering of Gio drawing operations. It
is used by package app and package gpu/headless and is otherwise not
useful except for integrating with external window implementations.
*/
package gpu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"time"
	"unsafe"

	"gioui.org/gpu/internal/driver"
	"gioui.org/internal/byteslice"
	"gioui.org/internal/f32"
	"gioui.org/internal/f32color"
	"gioui.org/internal/ops"
	"gioui.org/internal/scene"
	"gioui.org/internal/stroke"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/shader"
	"gioui.org/shader/gio"

	// Register backends.
	_ "gioui.org/gpu/internal/d3d11"
	_ "gioui.org/gpu/internal/metal"
	_ "gioui.org/gpu/internal/opengl"
	_ "gioui.org/gpu/internal/vulkan"
)

type GPU interface {
	// Release non-Go resources. The GPU is no longer valid after Release.
	Release()
	// Clear sets the clear color for the next Frame.
	Clear(color color.NRGBA)
	// Frame draws the graphics operations from op into a viewport of target.
	Frame(frame *op.Ops, target RenderTarget, viewport image.Point) error
}

type gpu struct {
	cache *textureCache

	profile                                string
	timers                                 *timers
	frameStart                             time.Time
	stencilTimer, coverTimer, cleanupTimer *timer
	drawOps                                drawOps
	ctx                                    driver.Device
	renderer                               *renderer
	// stats are the last frame's (Clack patch 36)
	stats FrameStats
	// partial is what partial frames keep between frames (Clack patch 46)
	partial partialFrames
}

type renderer struct {
	ctx           driver.Device
	blitter       *blitter
	pather        *pather
	packer        packer
	intersections packer
	layers        packer
	layerFBOs     fboSet
	// the frame's counts so far, and the API's counts as it began (Clack
	// patch 36)
	stats  FrameStats
	calls0 driver.Calls
	// fills are a partial frame's fill pipelines for coverage and layer
	// pages, made on first use (Clack patch 46)
	fills        [2]*pipeline
	fillUniforms *blitColUniforms
	// copyPipe draws the frame texture into a window's target (Metal)
	copyPipe     *pipeline
	copyUniforms *blitTexUniforms
}

type drawOps struct {
	reader       ops.Reader
	states       []f32.Affine2D
	transStack   []f32.Affine2D
	layers       []opacityLayer
	opacityStack []int
	vertCache    []byte
	viewport     image.Point
	clear        bool
	clearColor   f32color.RGBA
	imageOps     []imageOp
	pathOps      []*pathOp
	pathOpCache  []pathOp
	qs           quadSplitter
	pathCache    *opCache
	// statsDetail keeps each path's quad bounds for FrameStats.StencilPx
	// (Clack patch 36)
	statsDetail bool
	// tessellated counts the frame's buildVerts of clip paths (Clack patch
	// 46, FrameStats.PathsTessellated)
	tessellated int
}

type opacityLayer struct {
	opacity float32
	parent  int
	// depth of the opacity stack. Layers of equal depth are
	// independent and may be packed into one atlas.
	depth int
	// opStart and opEnd denote the range of drawOps.imageOps
	// that belong to the layer.
	opStart, opEnd int
	// clip of the layer operations.
	clip  image.Rectangle
	place placement
}

type drawState struct {
	t     f32.Affine2D
	cpath *pathOp

	matType materialType
	// Current paint.ImageOp
	image imageOpData
	// Current paint.ColorOp, if any.
	color color.NRGBA

	// Current paint.LinearGradientOp.
	stop1  f32.Point
	stop2  f32.Point
	color1 color.NRGBA
	color2 color.NRGBA
}

type pathOp struct {
	off f32.Point
	// rect tracks whether the clip stack can be represented by a
	// pixel-aligned rectangle.
	rect bool
	// clip is the union of all
	// later clip rectangles.
	clip   image.Rectangle
	bounds f32.Rectangle
	// intersect is the intersection of bounds and all
	// previous clip bounds.
	intersect f32.Rectangle
	pathKey   opKey
	path      bool
	pathVerts []byte
	parent    *pathOp
	place     placement
	// interior is where the path covers every pixel fully, in window
	// coordinates; empty if not known (Clack patch 38)
	interior image.Rectangle
	// hole is where the path, a stroke, covers no pixel, in window
	// coordinates; empty if not known (Clack patch 42)
	hole image.Rectangle
	// more are the path's other stencil regions (Clack patch 39): clip
	// and place are its first.
	more []pathRegion
	// hash is the path's geometry as its vertices are built from it (Clack
	// patch 46, pathHash)
	hash [2]uint64
	// raw: pathVerts is the path's data, not yet tessellated (Clack patch
	// 46: its cache entry has no GPU data)
	raw bool
}

// pathRegion is a region of the coverage atlas a path is stenciled in.
type pathRegion struct {
	clip  image.Rectangle
	place placement
}

const (
	// regionSlack is how many pixels that no op needs a region may grow
	// by to take in another op's rectangle; past it the op gets a region
	// of its own (Clack patch 39).
	regionSlack = 32 << 10
	// maxPathRegions bounds the stencil passes of one path.
	maxPathRegions = 8
)

// addRegion makes room for an op drawn in r in the path's stencil
// regions (Clack patch 39). Gio stenciled a path once over the box around
// every op it clips. Each pixel's coverage only adds up the curves above
// it, so a path can as well be stenciled in several regions: a rounded
// panel clipping a hovered row near its top, a scroll bar along its right
// edge and its corners isn't stenciled over the whole panel. r joins the
// region it grows the least, unless that adds more than regionSlack
// pixels no op needs.
func (p *pathOp) addRegion(r image.Rectangle) {
	if p.clip.Empty() {
		p.clip = r
		return
	}
	if clackLegacy.regions {
		p.clip = p.clip.Union(r)
		return
	}
	best, bestWaste := -1, int64(-1)
	for i := -1; i < len(p.more); i++ {
		c := p.clip
		if i >= 0 {
			c = p.more[i].clip
		}
		if r.In(c) {
			return
		}
		waste := area(c.Union(r)) - area(c) - area(r) + area(c.Intersect(r))
		if bestWaste < 0 || waste < bestWaste {
			best, bestWaste = i, waste
		}
	}
	if bestWaste > regionSlack && len(p.more) < maxPathRegions-1 {
		p.more = append(p.more, pathRegion{clip: r})
		return
	}
	if best < 0 {
		p.clip = p.clip.Union(r)
	} else {
		p.more[best].clip = p.more[best].clip.Union(r)
	}
}

// region is a stencil region of p holding r, and where it is in the
// atlas.
func (p *pathOp) region(r image.Rectangle) (image.Rectangle, placement) {
	if r.In(p.clip) {
		return p.clip, p.place
	}
	for _, m := range p.more {
		if r.In(m.clip) {
			return m.clip, m.place
		}
	}
	return p.clip, p.place
}

// covers reports whether p is a clip path that leaves everything inside
// r as it is (Clack patch 38): r lies in p's interior, where p's coverage
// is exactly 1. An op there is drawn as if p weren't on its clip stack, so
// a text run inside a rounded panel is one path instead of an
// intersection of two, and p isn't stenciled for it.
func (p *pathOp) covers(r image.Rectangle) bool {
	return p.path && !p.interior.Empty() && r.In(p.interior) && !clackLegacy.interiors
}

// clackLegacy turns parts of Clack's renderer changes off, so tests can
// compare the old way and the new: interiors keeps every clip path on
// every op it clips (patch 38), regions stencils a path in one region
// around all its ops (39), splits draws every op in one piece (42),
// layers gives every nested opacity layer a page of its own (40).
var clackLegacy struct{ interiors, regions, splits, layers bool }

// ClackLegacyClips turns the renderer's clip handling from before Clack
// patches 38, 39 and 42 on or off, for tests comparing the two. Call it
// between frames.
func ClackLegacyClips(on bool) { ClackLegacyParts(on, on, on) }

// ClackLegacyParts sets the old way of patches 38 (interiors), 39
// (regions) and 42 (splits) each on its own. Call it between frames.
func ClackLegacyParts(interiors, regions, splits bool) {
	clackLegacy.interiors, clackLegacy.regions, clackLegacy.splits = interiors, regions, splits
}

// ClackLegacyLayers turns patch 40's sharing of atlas pages by the
// opacity layers of one depth off (on: every nested layer gets a page of
// its own, as before). Call it between frames.
func ClackLegacyLayers(on bool) { clackLegacy.layers = on }

// splitMin is how many pixels a split must leave out of a stencil or
// draw unclipped to be worth the ops it adds (Clack patch 42).
const splitMin = 32 << 10

// splitOp is the parts an op drawn in b through the clip path p should be
// drawn in (Clack patch 42), if any: around p's hole, where a stroke
// covers nothing, without what's inside it, or around and inside p's
// interior, which patch 38 then draws with p left off. The parts are
// whole pixels in window space and tile b (minus the hole), so every
// pixel is drawn once with the coverage it had: a split made of clips
// would blend twice where two parts' antialiased edges meet, under any
// transform but an integer offset, and only there do paths have a hole or
// an interior. Only a uniform colour can be drawn in parts as it is.
func splitOp(p *pathOp, b image.Rectangle, m materialType) (parts [5]image.Rectangle, n int) {
	if clackLegacy.splits || m != materialColor {
		return parts, 0
	}
	if p != nil && p.path && !p.hole.Empty() {
		if h := p.hole.Intersect(b); area(h) >= splitMin {
			return around(b, h)
		}
	}
	for ; p != nil; p = p.parent {
		if !p.path || p.interior.Empty() {
			continue
		}
		in := p.interior.Intersect(b)
		if in == b || area(in) < splitMin {
			return parts, 0
		}
		parts, n = around(b, in)
		parts[n] = in
		return parts, n + 1
	}
	return parts, 0
}

// around is the parts of b outside h, which lies in b: the rows above and
// below h, the width of b, and the columns left and right of h, its height.
func around(b, h image.Rectangle) (parts [5]image.Rectangle, n int) {
	for _, r := range [...]image.Rectangle{
		{Min: b.Min, Max: image.Pt(b.Max.X, h.Min.Y)},
		{Min: image.Pt(b.Min.X, h.Min.Y), Max: image.Pt(h.Min.X, h.Max.Y)},
		{Min: image.Pt(h.Max.X, h.Min.Y), Max: image.Pt(b.Max.X, h.Max.Y)},
		{Min: image.Pt(b.Min.X, h.Max.Y), Max: b.Max},
	} {
		if !r.Empty() {
			parts[n] = r
			n++
		}
	}
	return parts, n
}

type imageOp struct {
	path     *pathOp
	clip     image.Rectangle
	material material
	clipType clipType
	// place is either a placement in the path fbos or intersection fbos,
	// depending on clipType.
	place placement
	// layerOps is the number of operations this
	// operation replaces.
	layerOps int
}

func decodeStrokeOp(data []byte) float32 {
	_ = data[4]
	bo := binary.LittleEndian
	return math.Float32frombits(bo.Uint32(data[1:]))
}

type quadsOp struct {
	key opKey
	aux []byte
	// interior is the next clip's (Clack patch 38), hole its stroke's
	// (42)
	interior, hole image.Rectangle
}

type opKey struct {
	outline        bool
	strokeWidth    float32
	sx, hx, sy, hy float32
	// ox, oy are the fraction of a pixel the offset leaves after
	// transformOffset takes its whole pixels: built into the vertices, so
	// a path at another fraction is built again (Clack patch 42; Gio
	// reused the vertices of the first fraction it drew a path at)
	ox, oy float32
	ops.Key
}

type material struct {
	material materialType
	opaque   bool
	// For materialTypeColor.
	color f32color.RGBA
	// For materialTypeLinearGradient.
	color1  f32color.RGBA
	color2  f32color.RGBA
	opacity float32
	// For materialTypeTexture.
	data    imageOpData
	tex     driver.Texture
	uvTrans f32.Affine2D
	// texID names tex's image (Clack patch 46): a texture made again, or
	// another at the same address, gets another
	texID uint64
}

const (
	filterLinear  = 0
	filterNearest = 1
)

// imageOpData is the shadow of paint.ImageOp.
type imageOpData struct {
	src    *image.RGBA
	handle any
	filter byte
}

type linearGradientOpData struct {
	stop1  f32.Point
	color1 color.NRGBA
	stop2  f32.Point
	color2 color.NRGBA
}

func decodeImageOp(data []byte, refs []any) imageOpData {
	handle := refs[1]
	if handle == nil {
		return imageOpData{}
	}
	return imageOpData{
		src:    refs[0].(*image.RGBA),
		handle: handle,
		filter: data[1],
	}
}

func decodeColorOp(data []byte) color.NRGBA {
	data = data[:ops.TypeColorLen]
	return color.NRGBA{
		R: data[1],
		G: data[2],
		B: data[3],
		A: data[4],
	}
}

func decodeLinearGradientOp(data []byte) linearGradientOpData {
	data = data[:ops.TypeLinearGradientLen]
	bo := binary.LittleEndian
	return linearGradientOpData{
		stop1: f32.Point{
			X: math.Float32frombits(bo.Uint32(data[1:])),
			Y: math.Float32frombits(bo.Uint32(data[5:])),
		},
		stop2: f32.Point{
			X: math.Float32frombits(bo.Uint32(data[9:])),
			Y: math.Float32frombits(bo.Uint32(data[13:])),
		},
		color1: color.NRGBA{
			R: data[17+0],
			G: data[17+1],
			B: data[17+2],
			A: data[17+3],
		},
		color2: color.NRGBA{
			R: data[21+0],
			G: data[21+1],
			B: data[21+2],
			A: data[21+3],
		},
	}
}

type resource interface {
	release()
}

type texture struct {
	src *image.RGBA
	tex driver.Texture
	// id names the image the texture holds (Clack patch 46)
	id uint64
}

type blitter struct {
	ctx                    driver.Device
	viewport               image.Point
	pipelines              [2][3]*pipeline
	colUniforms            *blitColUniforms
	texUniforms            *blitTexUniforms
	linearGradientUniforms *blitLinearGradientUniforms
	quadVerts              driver.Buffer
}

type blitColUniforms struct {
	blitUniforms
	_ [128 - unsafe.Sizeof(blitUniforms{}) - unsafe.Sizeof(colorUniforms{})]byte // Padding to 128 bytes.
	colorUniforms
}

type blitTexUniforms struct {
	blitUniforms
}

type blitLinearGradientUniforms struct {
	blitUniforms
	_ [128 - unsafe.Sizeof(blitUniforms{}) - unsafe.Sizeof(gradientUniforms{})]byte // Padding to 128 bytes.
	gradientUniforms
}

type uniformBuffer struct {
	buf driver.Buffer
	ptr []byte
}

type pipeline struct {
	pipeline driver.Pipeline
	uniforms *uniformBuffer
}

type blitUniforms struct {
	transform     [4]float32
	uvTransformR1 [4]float32
	uvTransformR2 [4]float32
	opacity       float32
	fbo           float32
	_             [2]float32
}

type colorUniforms struct {
	color f32color.RGBA
}

type gradientUniforms struct {
	color1 f32color.RGBA
	color2 f32color.RGBA
}

type clipType uint8

const (
	clipTypeNone clipType = iota
	clipTypePath
	clipTypeIntersection
)

type materialType uint8

const (
	materialColor materialType = iota
	materialLinearGradient
	materialTexture
)

// New creates a GPU for the given API.
func New(api API) (GPU, error) {
	d, err := driver.NewDevice(api)
	if err != nil {
		return nil, err
	}
	return NewWithDevice(d)
}

// NewWithDevice creates a GPU with a pre-existing device.
//
// Note: for internal use only.
func NewWithDevice(d driver.Device) (GPU, error) {
	d.BeginFrame(nil, false, image.Point{})
	defer d.EndFrame()
	feats := d.Caps().Features
	switch {
	case feats.Has(driver.FeatureFloatRenderTargets) && feats.Has(driver.FeatureSRGB):
		return newGPU(d), nil
	}
	return nil, errors.New("no available GPU driver")
}

func newGPU(ctx driver.Device) *gpu {
	g := &gpu{
		cache: newTextureCache(),
	}
	g.ctx = ctx
	g.drawOps.pathCache = newOpCache()
	g.renderer = newRenderer(ctx)
	return g
}

func (g *gpu) Clear(col color.NRGBA) {
	g.drawOps.clear = true
	g.drawOps.clearColor = f32color.LinearFromSRGB(col)
}

func (g *gpu) Release() {
	g.partialRelease()
	g.renderer.release()
	g.drawOps.pathCache.release()
	g.cache.release()
	if g.timers != nil {
		g.timers.Release()
	}
	g.ctx.Release()
}

func (g *gpu) Frame(frameOps *op.Ops, target RenderTarget, viewport image.Point) error {
	g.collect(viewport, frameOps)
	return g.frame(target)
}

func (g *gpu) collect(viewport image.Point, frameOps *op.Ops) {
	g.renderer.blitter.viewport = viewport
	g.renderer.pather.viewport = viewport
	g.drawOps.reset(viewport)
	g.drawOps.collect(frameOps, viewport)
	if false && g.timers == nil && g.ctx.Caps().Features.Has(driver.FeatureTimers) {
		g.frameStart = time.Now()
		g.timers = newTimers(g.ctx)
		g.stencilTimer = g.timers.newTimer()
		g.coverTimer = g.timers.newTimer()
		g.cleanupTimer = g.timers.newTimer()
	}
}

func (g *gpu) frame(target RenderTarget) error {
	viewport := g.renderer.blitter.viewport
	g.statsBegin()
	defer g.statsEnd()
	defFBO := g.ctx.BeginFrame(target, g.drawOps.clear, viewport)
	defer g.ctx.EndFrame()
	r := g.renderer
	for _, img := range g.drawOps.imageOps {
		expandPathOp(img.path, img.clip)
	}
	g.stencilTimer.begin()
	// Clack patch 46: everything the passes draw from is decided first (the
	// atlases' packing and page sizes, the textures), so the frame can be
	// compared with the last and drawn again only where it differs (dmg;
	// nil: the whole frame), into a texture that keeps its pixels (out).
	// The pages are sized, and aged (patch 40), even in a frame that
	// needs none.
	on := g.partialOn(target, viewport)
	var sticky *stickyAtlas
	if on {
		sticky = &g.partial.atlas
	}
	r.packer.work = 0
	var repacked int
	if sticky != nil {
		repacked = sticky.repacked
	}
	r.packStencils(&g.drawOps.pathOps, sticky)
	r.stats.PackWork = r.packer.work
	if sticky != nil {
		r.stats.StickySlots, r.stats.StickyHoles = len(sticky.slots), sticky.holeSlots
		r.stats.StickyRepacks = sticky.repacked - repacked
	}
	r.packIntersections(g.drawOps.imageOps)
	r.uploadImages(g.cache, g.drawOps.imageOps)
	g.drawOps.layers = r.packLayers(g.drawOps.layers)
	r.pather.begin(r.packer.sizes)
	r.pather.stenciler.beginIntersect(r.intersections.sizes)
	r.layerFBOs.resize(r.ctx, driver.TextureFormatSRGBA, r.layers.sizes)
	out, dmg := g.partialBegin(on, target, defFBO, viewport)
	// the paths the frame stencils: a partial frame's are the ones in its
	// damage
	r.stats.PathsBuilt, r.stats.PathBytes = g.drawOps.buildPaths(g.ctx, dmg)
	r.stats.PathsTessellated = g.drawOps.tessellated
	r.stencilClips(g.drawOps.pathCache, g.drawOps.pathOps, dmg)
	r.prepareIntersections(g.drawOps.imageOps)
	r.intersect(g.drawOps.imageOps, dmg)
	g.stencilTimer.end()
	g.coverTimer.begin()
	r.prepareDrawOps(g.drawOps.imageOps)
	r.drawLayers(g.drawOps.layers, g.drawOps.imageOps, dmg)
	g.partialClear(out, dmg, viewport)
	d := driver.LoadDesc{
		ClearColor: g.drawOps.clearColor,
	}
	switch {
	case dmg != nil:
		// the last frame's pixels, its damage cleared (partialClear)
		d.Action = driver.LoadActionKeep
	case g.drawOps.clear:
		d.Action = driver.LoadActionClear
		r.stats.ClearPx += int64(viewport.X * viewport.Y)
	}
	g.drawOps.clear = false
	if dmg == nil || len(dmg) > 0 {
		r.stats.Passes++
		g.ctx.BeginRenderPass(out.pass, d)
		g.ctx.Viewport(0, 0, viewport.X, viewport.Y)
		r.drawOps(false, image.Point{}, r.blitter.viewport, g.drawOps.imageOps, r.mainRects(dmg, viewport))
		g.ctx.EndRenderPass()
	}
	g.coverTimer.end()
	g.partialEnd(defFBO, out, viewport)
	g.cleanupTimer.begin()
	g.cache.frame()
	g.drawOps.pathCache.frame()
	g.cleanupTimer.end()
	// what the device holds once the caches let go (Clack patch 46)
	r.stats.DeviceTextureBytes, r.stats.DeviceBufferBytes = -1, -1
	if rd, ok := g.ctx.(driver.ClackResources); ok {
		r.stats.DeviceTextureBytes, r.stats.DeviceBufferBytes = rd.ClackResourceBytes()
	}
	r.stats.FrameTextureBytes = g.partial.heldBytes()
	if false && g.timers.ready() {
		st, covt, cleant := g.stencilTimer.Elapsed, g.coverTimer.Elapsed, g.cleanupTimer.Elapsed
		ft := st + covt + cleant
		q := 100 * time.Microsecond
		st, covt = st.Round(q), covt.Round(q)
		frameDur := time.Since(g.frameStart).Round(q)
		ft = ft.Round(q)
		g.profile = fmt.Sprintf("draw:%7s gpu:%7s st:%7s cov:%7s", frameDur, ft, st, covt)
	}
	return nil
}

func (g *gpu) Profile() string {
	return g.profile
}

// liveImage is a live ImageOp's handle (paint.NewLiveImageOp, Clack patch
// 35): the image to draw now and its texture's key.
type liveImage interface {
	LiveImage() (*image.RGBA, any)
}

// resolveLive is data drawing a live image's image as it is now: once a
// paint of it is on screen (collect drops the ones outside the viewport
// and their clips), so only what the frame shows asks. One with no image
// now draws the image it was made with.
func resolveLive(data imageOpData) imageOpData {
	if l, ok := data.handle.(liveImage); ok {
		if src, h := l.LiveImage(); src != nil && h != nil {
			data.src, data.handle = src, h
		}
	}
	return data
}

func (r *renderer) texHandle(cache *textureCache, data imageOpData) (driver.Texture, uint64) {
	data = resolveLive(data)
	key := textureCacheKey{
		filter: data.filter,
		handle: data.handle,
	}

	var tex *texture
	t, exists := cache.get(key)
	if !exists {
		cache.nextID++
		t = &texture{
			src: data.src,
			id:  cache.nextID,
		}
		cache.put(key, t)
	}
	tex = t.(*texture)
	if tex.tex != nil {
		return tex.tex, tex.id
	}

	var minFilter, magFilter driver.TextureFilter
	switch data.filter {
	case filterLinear:
		minFilter, magFilter = driver.FilterLinearMipmapLinear, driver.FilterLinear
	case filterNearest:
		minFilter, magFilter = driver.FilterNearest, driver.FilterNearest
	}

	handle, err := r.ctx.NewTexture(driver.TextureFormatSRGBA,
		data.src.Bounds().Dx(), data.src.Bounds().Dy(),
		minFilter, magFilter,
		driver.BufferBindingTexture,
	)
	if err != nil {
		panic(err)
	}
	driver.UploadImage(handle, image.Pt(0, 0), data.src)
	tex.tex = handle
	return tex.tex, tex.id
}

func (t *texture) release() {
	if t.tex != nil {
		t.tex.Release()
	}
}

func newRenderer(ctx driver.Device) *renderer {
	r := &renderer{
		ctx:     ctx,
		blitter: newBlitter(ctx),
		pather:  newPather(ctx),
	}

	maxDim := ctx.Caps().MaxTextureSize
	// Large atlas textures cause artifacts due to precision loss in
	// shaders.
	if cap := 8192; maxDim > cap {
		maxDim = cap
	}
	d := image.Pt(maxDim, maxDim)

	r.packer.maxDims = d
	r.intersections.maxDims = d
	r.layers.maxDims = d
	return r
}

func (r *renderer) release() {
	r.pather.release()
	r.blitter.release()
	r.layerFBOs.delete(r.ctx, 0)
	for _, p := range r.fills {
		if p != nil {
			p.Release()
		}
	}
	if r.copyPipe != nil {
		r.copyPipe.Release()
	}
}

func newBlitter(ctx driver.Device) *blitter {
	quadVerts, err := ctx.NewImmutableBuffer(driver.BufferBindingVertices,
		byteslice.Slice([]float32{
			-1, -1, 0, 0,
			+1, -1, 1, 0,
			-1, +1, 0, 1,
			+1, +1, 1, 1,
		}),
	)
	if err != nil {
		panic(err)
	}
	b := &blitter{
		ctx:       ctx,
		quadVerts: quadVerts,
	}
	b.colUniforms = new(blitColUniforms)
	b.texUniforms = new(blitTexUniforms)
	b.linearGradientUniforms = new(blitLinearGradientUniforms)
	pipelines, err := createColorPrograms(ctx, gio.Shader_blit_vert, gio.Shader_blit_frag,
		[...][]byte{
			byteslice.View(b.colUniforms),
			byteslice.View(b.linearGradientUniforms),
			byteslice.View(b.texUniforms),
		},
	)
	if err != nil {
		panic(err)
	}
	b.pipelines = pipelines
	return b
}

func (b *blitter) release() {
	b.quadVerts.Release()
	for _, p := range b.pipelines {
		for _, p := range p {
			p.Release()
		}
	}
}

func createColorPrograms(b driver.Device, vsSrc shader.Sources, fsSrc [3]shader.Sources, uniforms [3][]byte) (pipelines [2][3]*pipeline, err error) {
	defer func() {
		if err != nil {
			for _, p := range pipelines {
				for _, p := range p {
					if p != nil {
						p.Release()
					}
				}
			}
		}
	}()
	blend := driver.BlendDesc{
		SrcFactor: driver.BlendFactorOne,
		DstFactor: driver.BlendFactorOneMinusSrcAlpha,
	}
	layout := driver.VertexLayout{
		Inputs: []driver.InputDesc{
			{Type: shader.DataTypeFloat, Size: 2, Offset: 0},
			{Type: shader.DataTypeFloat, Size: 2, Offset: 4 * 2},
		},
		Stride: 4 * 4,
	}
	vsh, err := b.NewVertexShader(vsSrc)
	if err != nil {
		return pipelines, err
	}
	defer vsh.Release()
	for i, format := range []driver.TextureFormat{driver.TextureFormatOutput, driver.TextureFormatSRGBA} {
		{
			fsh, err := b.NewFragmentShader(fsSrc[materialTexture])
			if err != nil {
				return pipelines, err
			}
			defer fsh.Release()
			pipe, err := b.NewPipeline(driver.PipelineDesc{
				VertexShader:   vsh,
				FragmentShader: fsh,
				BlendDesc:      blend,
				VertexLayout:   layout,
				PixelFormat:    format,
				Topology:       driver.TopologyTriangleStrip,
			})
			if err != nil {
				return pipelines, err
			}
			var vertBuffer *uniformBuffer
			if u := uniforms[materialTexture]; u != nil {
				vertBuffer = newUniformBuffer(b, u)
			}
			pipelines[i][materialTexture] = &pipeline{pipe, vertBuffer}
		}
		{
			var vertBuffer *uniformBuffer
			fsh, err := b.NewFragmentShader(fsSrc[materialColor])
			if err != nil {
				return pipelines, err
			}
			defer fsh.Release()
			pipe, err := b.NewPipeline(driver.PipelineDesc{
				VertexShader:   vsh,
				FragmentShader: fsh,
				BlendDesc:      blend,
				VertexLayout:   layout,
				PixelFormat:    format,
				Topology:       driver.TopologyTriangleStrip,
			})
			if err != nil {
				return pipelines, err
			}
			if u := uniforms[materialColor]; u != nil {
				vertBuffer = newUniformBuffer(b, u)
			}
			pipelines[i][materialColor] = &pipeline{pipe, vertBuffer}
		}
		{
			var vertBuffer *uniformBuffer
			fsh, err := b.NewFragmentShader(fsSrc[materialLinearGradient])
			if err != nil {
				return pipelines, err
			}
			defer fsh.Release()
			pipe, err := b.NewPipeline(driver.PipelineDesc{
				VertexShader:   vsh,
				FragmentShader: fsh,
				BlendDesc:      blend,
				VertexLayout:   layout,
				PixelFormat:    format,
				Topology:       driver.TopologyTriangleStrip,
			})
			if err != nil {
				return pipelines, err
			}
			if u := uniforms[materialLinearGradient]; u != nil {
				vertBuffer = newUniformBuffer(b, u)
			}
			pipelines[i][materialLinearGradient] = &pipeline{pipe, vertBuffer}
		}
	}
	return pipelines, nil
}

// stencilClips stencils the frame's clip paths into the coverage atlas, in
// each of their regions (Clack patch 39). A partial frame (dmg not nil,
// Clack patch 46) stencils only the part of each region in the damage, at
// the place the whole frame does, and clears just those texels first.
// The pages are sized before (frame).
func (r *renderer) stencilClips(pathCache *opCache, ops []*pathOp, dmg []image.Rectangle) {
	var canary map[stickyRegion]bool // ClackCanaryTexels (tests)
	if dmg == nil {
		canary = canaryRegions(ops, int(clackFloor.canary.Load()))
	}
	// page by page: with sticky places (Clack patch 46) a page's regions
	// aren't together in the ops' order, and a page begun again would be
	// cleared again
	for page := range r.packer.sizes {
		begun := false
		begin := func() {
			if begun {
				return
			}
			begun = true
			f := r.pather.stenciler.cover(page)
			d := driver.LoadDesc{Action: driver.LoadActionClear}
			if dmg != nil {
				d.Action = driver.LoadActionKeep // only the damage's texels are read
			} else {
				r.stats.ClearPx += int64(f.size.X * f.size.Y)
			}
			r.ctx.BeginRenderPass(f.tex, d)
			r.stats.Passes++
			r.ctx.BindPipeline(r.pather.stenciler.pipeline.pipeline.pipeline)
			r.ctx.BindIndexBuffer(r.pather.stenciler.indexBuf)
		}
		for _, p := range ops {
			var v opCacheValue
			got := false
			// the path in each of its regions (Clack patch 39)
			for i := -1; i < len(p.more); i++ {
				clip, place := p.clip, p.place
				if i >= 0 {
					clip, place = p.more[i].clip, p.more[i].place
				}
				if place.Idx != page {
					continue
				}
				if !got {
					v, _ = pathCache.get(p.pathKey)
					got = true
				}
				if dmg == nil {
					begin()
					r.stencil(p, v, clip, place, clip)
					if canary[stickyRegion{p, i}] {
						r.canaryTexel(clip, place)
					}
					continue
				}
				for _, d := range dmg {
					sc := passScissor(d, clip, place.Pos)
					if sc.Empty() {
						continue
					}
					begin()
					r.fill(driver.TextureFormatFloat, place.Pos, clip.Size(), sc, f32color.RGBA{})
					r.ctx.BindPipeline(r.pather.stenciler.pipeline.pipeline.pipeline)
					r.ctx.BindIndexBuffer(r.pather.stenciler.indexBuf)
					r.scissor(sc)
					r.stencil(p, v, clip, place, d.Intersect(clip))
				}
			}
		}
		if begun {
			r.ctx.EndRenderPass()
		}
	}
}

// stencil draws path p's region clip at place, counting the stencil
// fragments in in (window coordinates).
func (r *renderer) stencil(p *pathOp, v opCacheValue, clip image.Rectangle, place placement, in image.Rectangle) {
	r.pather.stencilPath(clip, p.off, place.Pos, v.data)
	r.stats.StencilPaths++
	r.stats.Draws += (v.data.ncurves/4 + pathBatchSize - 1) / pathBatchSize
	if r.stats.StencilPx >= 0 {
		r.stats.StencilPx += stencilPx(v.quads, image.Pt(int(p.off.X), int(p.off.Y)), in)
	}
}

func (r *renderer) prepareIntersections(ops []imageOp) {
	for _, img := range ops {
		if img.clipType != clipTypeIntersection {
			continue
		}
		// the paths of an intersection are read from any of the coverage
		// pages, in their regions (Clack patch 39)
		for _, f := range r.pather.stenciler.fbos.fbos {
			r.ctx.PrepareTexture(f.tex)
		}
		return
	}
}

// intersect draws the coverage of the ops clipped by two paths or more
// into the intersection atlas; a partial frame (Clack patch 46) only the
// part in the damage, its texels set to 1 first. The pages are sized
// before (frame).
func (r *renderer) intersect(ops []imageOp, dmg []image.Rectangle) {
	if len(r.intersections.sizes) == 0 {
		return
	}
	fbo := -1
	page := func(idx int) {
		if fbo == idx {
			return
		}
		if fbo != -1 {
			r.ctx.EndRenderPass()
		}
		fbo = idx
		f := r.pather.stenciler.intersections.fbos[fbo]
		d := driver.LoadDesc{Action: driver.LoadActionClear}
		d.ClearColor.R = 1.0
		if dmg != nil {
			d.Action = driver.LoadActionKeep
		} else {
			r.stats.ClearPx += int64(f.size.X * f.size.Y)
		}
		r.ctx.BeginRenderPass(f.tex, d)
		r.stats.Passes++
		r.ctx.BindPipeline(r.pather.stenciler.ipipeline.pipeline.pipeline)
		r.ctx.BindVertexBuffer(r.blitter.quadVerts, 0)
	}
	for _, img := range ops {
		if img.clipType != clipTypeIntersection {
			continue
		}
		if dmg == nil {
			page(img.place.Idx)
			r.ctx.Viewport(img.place.Pos.X, img.place.Pos.Y, img.clip.Dx(), img.clip.Dy())
			r.intersectPath(img.path, img.clip, area(img.clip))
			continue
		}
		for _, d := range dmg {
			sc := passScissor(d, img.clip, img.place.Pos)
			if sc.Empty() {
				continue
			}
			page(img.place.Idx)
			r.fill(driver.TextureFormatFloat, img.place.Pos, img.clip.Size(), sc, f32color.RGBA{R: 1})
			r.ctx.BindPipeline(r.pather.stenciler.ipipeline.pipeline.pipeline)
			r.ctx.BindVertexBuffer(r.blitter.quadVerts, 0)
			r.ctx.Viewport(img.place.Pos.X, img.place.Pos.Y, img.clip.Dx(), img.clip.Dy())
			r.scissor(sc)
			r.intersectPath(img.path, img.clip, area(sc))
		}
	}
	if fbo != -1 {
		r.ctx.EndRenderPass()
	}
}

// intersectPath multiplies the coverage of each path clip is clipped by
// into the intersection atlas; px is how many of its texels are drawn
// (the scissor's, in a partial frame).
func (r *renderer) intersectPath(p *pathOp, clip image.Rectangle, px int64) {
	if p.parent != nil {
		r.intersectPath(p.parent, clip, px)
	}
	if !p.path || p.covers(clip) {
		return
	}
	pclip, place := p.region(clip) // Clack patch 39
	uv := image.Rectangle{
		Min: place.Pos,
		Max: place.Pos.Add(pclip.Size()),
	}
	o := clip.Min.Sub(pclip.Min)
	sub := image.Rectangle{
		Min: o,
		Max: o.Add(clip.Size()),
	}
	fbo := r.pather.stenciler.cover(place.Idx)
	r.ctx.BindTexture(0, fbo.tex)
	coverScale, coverOff := texSpaceTransform(f32.FRect(uv), fbo.size)
	subScale, subOff := texSpaceTransform(f32.FRect(sub), pclip.Size())
	r.pather.stenciler.ipipeline.uniforms.vert.uvTransform = [4]float32{coverScale.X, coverScale.Y, coverOff.X, coverOff.Y}
	r.pather.stenciler.ipipeline.uniforms.vert.subUVTransform = [4]float32{subScale.X, subScale.Y, subOff.X, subOff.Y}
	r.pather.stenciler.ipipeline.pipeline.UploadUniforms(r.ctx)
	r.ctx.DrawArrays(0, 4)
	r.stats.Draws++
	r.stats.IntersectPx += px
}

func (r *renderer) packIntersections(ops []imageOp) {
	r.intersections.clear()
	for i, img := range ops {
		var npaths int
		var onePath *pathOp
		for p := img.path; p != nil; p = p.parent {
			if p.path && !p.covers(img.clip) {
				onePath = p
				npaths++
			}
		}
		switch npaths {
		case 0:
		case 1:
			pclip, place := onePath.region(img.clip) // Clack patch 39
			place.Pos = place.Pos.Sub(pclip.Min).Add(img.clip.Min)
			ops[i].place = place
			ops[i].clipType = clipTypePath
		default:
			sz := image.Point{X: img.clip.Dx(), Y: img.clip.Dy()}
			place, ok := r.intersections.add(sz)
			if !ok {
				panic("internal error: if the intersection fit, the intersection should fit as well")
			}
			ops[i].clipType = clipTypeIntersection
			ops[i].place = place
		}
	}
}

// packStencils places the paths' stencil regions in the coverage atlas;
// with sticky (partial frames, Clack patch 46) a region that was in the
// last frame keeps its place.
func (r *renderer) packStencils(pops *[]*pathOp, sticky *stickyAtlas) {
	r.packer.clear()
	ops := *pops
	if sticky != nil {
		// the paths with a region, in order
		k := 0
		for _, p := range ops {
			if !p.clip.Empty() {
				ops[k] = p
				k++
			}
		}
		clear(ops[k:])
		ops = ops[:k]
		sticky.pack(&r.packer, ops)
		*pops = ops
		return
	}
	if seed := clackFloor.packSeed.Load(); seed != 0 {
		// Clack patch 46, tests: the regions Gio keeps, in seed's order
		// (ClackPackOrder)
		var i int
		for i < len(ops) {
			if ops[i].clip.Empty() {
				ops[i] = ops[len(ops)-1]
				ops = ops[:len(ops)-1]
				continue
			}
			i++
		}
		packShuffled(&r.packer, ops, seed)
		*pops = ops
		return
	}
	// Allocate atlas space for cover textures.
	var i int
	for i < len(ops) {
		p := ops[i]
		if p.clip.Empty() {
			ops[i] = ops[len(ops)-1]
			ops = ops[:len(ops)-1]
			continue
		}
		place, ok := r.packer.add(p.clip.Size())
		if !ok {
			// The clip area is at most the entire screen. Hopefully no
			// screen is larger than GL_MAX_TEXTURE_SIZE.
			panic(fmt.Errorf("clip area %v is larger than maximum texture size %v", p.clip, r.packer.maxDims))
		}
		p.place = place
		for j := range p.more { // Clack patch 39
			place, ok := r.packer.add(p.more[j].clip.Size())
			if !ok {
				panic(fmt.Errorf("clip area %v is larger than maximum texture size %v", p.more[j].clip, r.packer.maxDims))
			}
			p.more[j].place = place
		}
		i++
	}
	*pops = ops
}

func (r *renderer) packLayers(layers []opacityLayer) []opacityLayer {
	// Make every layer bounds contain nested layers; cull empty layers.
	for i, l := range slices.Backward(layers) {

		if l.parent != -1 {
			b := layers[l.parent].clip
			layers[l.parent].clip = b.Union(l.clip)
		}
		if l.clip.Empty() {
			layers = slices.Delete(layers, i, i+1)
		}
	}
	// Pack layers.
	r.layers.clear()
	depth := 0
	for i := range layers {
		l := &layers[i]
		// Only layers of the same depth may be packed together.
		if l.depth != depth {
			r.layers.newPage()
			// Clack patch 40: depth stayed 0, so every nested layer got a
			// page (an FBO and a render pass) of its own
			if !clackLegacy.layers {
				depth = l.depth
			}
		}
		place, ok := r.layers.add(l.clip.Size())
		if !ok {
			// The layer area is at most the entire screen. Hopefully no
			// screen is larger than GL_MAX_TEXTURE_SIZE.
			panic(fmt.Errorf("layer size %v is larger than maximum texture size %v", l.clip.Size(), r.layers.maxDims))
		}
		l.place = place
	}
	return layers
}

// drawLayers draws the opacity layers into their pages and replaces each
// layer's ops with one op drawing its page; a partial frame (Clack patch
// 46) draws only the layers in the damage and only there, their texels
// cleared first. The pages are sized before (frame).
func (r *renderer) drawLayers(layers []opacityLayer, ops []imageOp, dmg []image.Rectangle) {
	if len(r.layers.sizes) == 0 {
		return
	}
	fbo := -1
	for _, l := range slices.Backward(layers) {

		if l.opStart >= l.opEnd || l.opEnd > len(ops) {
			// Not a valid range of ops; skip it rather than index out of
			// bounds while a render pass is open.
			continue
		}
		var rects []passRect
		for _, d := range dmg {
			if sc := passScissor(d, l.clip, l.place.Pos); !sc.Empty() {
				rects = append(rects, passRect{win: d.Intersect(l.clip), s: sc})
			}
		}
		if dmg == nil || len(rects) > 0 {
			if fbo != l.place.Idx {
				if fbo != -1 {
					r.ctx.EndRenderPass()
					r.ctx.PrepareTexture(r.layerFBOs.fbos[fbo].tex)
				}
				fbo = l.place.Idx
				f := r.layerFBOs.fbos[fbo]
				d := driver.LoadDesc{Action: driver.LoadActionClear}
				if dmg != nil {
					d.Action = driver.LoadActionKeep
				} else {
					r.stats.ClearPx += int64(f.size.X * f.size.Y)
				}
				r.ctx.BeginRenderPass(f.tex, d)
				r.stats.Passes++
			}
			for _, pr := range rects {
				r.fill(driver.TextureFormatSRGBA, l.place.Pos, l.clip.Size(), pr.s, f32color.RGBA{})
			}
			v := image.Rectangle{
				Min: l.place.Pos,
				Max: l.place.Pos.Add(l.clip.Size()),
			}
			r.ctx.Viewport(v.Min.X, v.Min.Y, v.Dx(), v.Dy())
			r.drawOps(true, l.clip.Min.Mul(-1), l.clip.Size(), ops[l.opStart:l.opEnd], rects)
		}
		f := r.layerFBOs.fbos[l.place.Idx]
		v := image.Rectangle{
			Min: l.place.Pos,
			Max: l.place.Pos.Add(l.clip.Size()),
		}
		sr := f32.FRect(v)
		uvScale, uvOffset := texSpaceTransform(sr, f.size)
		uvTrans := f32.AffineId().Scale(f32.Point{}, uvScale).Offset(uvOffset)
		// Replace layer ops with one textured op.
		ops[l.opStart] = imageOp{
			clip: l.clip,
			material: material{
				material: materialTexture,
				tex:      f.tex,
				uvTrans:  uvTrans,
				opacity:  l.opacity,
			},
			layerOps: l.opEnd - l.opStart - 1,
		}
	}
	if fbo != -1 {
		r.ctx.EndRenderPass()
		r.ctx.PrepareTexture(r.layerFBOs.fbos[fbo].tex)
	}
}

func (d *drawOps) reset(viewport image.Point) {
	d.viewport = viewport
	// Clack patch 18: the ops are collected every frame into the same
	// arrays; cleared first, a longer earlier frame's entries don't keep
	// what they point at (images' pixels, path vertices) reachable past
	// the end of a shorter one.
	clear(d.imageOps)
	clear(d.pathOps)
	clear(d.pathOpCache)
	clear(d.layers)
	d.imageOps = d.imageOps[:0]
	d.pathOps = d.pathOps[:0]
	d.pathOpCache = d.pathOpCache[:0]
	d.vertCache = d.vertCache[:0]
	d.transStack = d.transStack[:0]
	d.layers = d.layers[:0]
	d.opacityStack = d.opacityStack[:0]
	d.tessellated = 0
}

func (d *drawOps) collect(root *op.Ops, viewport image.Point) {
	viewf := f32.Rectangle{
		Max: f32.Point{X: float32(viewport.X), Y: float32(viewport.Y)},
	}
	var ops *ops.Ops
	if root != nil {
		ops = &root.Internal
	}
	d.reader.Reset(ops)
	d.collectOps(&d.reader, viewf)
}

// buildPaths uploads the vertices of the paths the cache doesn't hold,
// and reports how many and their bytes (Clack patch 36). A partial frame
// (dmg not nil, Clack patch 46) uploads only the ones it stencils, in its
// damage: a path recorded anew every frame (a new key) costs a vertex
// buffer made and released each frame.
func (d *drawOps) buildPaths(ctx driver.Device, dmg []image.Rectangle) (n int, bytes int64) {
	for _, p := range d.pathOps {
		if dmg != nil && !p.inDamage(dmg) {
			p.pathVerts = nil
			continue
		}
		if v, exists := d.pathCache.get(p.pathKey); !exists || v.data.data == nil {
			verts := p.pathVerts
			if p.raw {
				// its path data, from a cache entry no frame uploaded
				// (Clack patch 46)
				k := p.pathKey
				verts, _ = d.buildVerts(verts, f32.NewAffine2D(k.sx, k.hx, k.ox, k.hy, k.sy, k.oy), k.outline, k.strokeWidth)
				d.tessellated++
			}
			n++
			bytes += int64(len(verts))
			data := buildPath(ctx, verts)
			val := opCacheValue{
				data:   data,
				bounds: p.bounds,
				hash:   p.hash,
			}
			if d.statsDetail {
				val.quads = quadBounds(verts)
			}
			d.pathCache.put(p.pathKey, val)
		}
		p.pathVerts = nil
	}
	return n, bytes
}

// inDamage reports whether a stencil region of p overlaps the damage.
func (p *pathOp) inDamage(dmg []image.Rectangle) bool {
	for _, d := range dmg {
		if p.clip.Overlaps(d) {
			return true
		}
		for _, m := range p.more {
			if m.clip.Overlaps(d) {
				return true
			}
		}
	}
	return false
}

func (d *drawOps) newPathOp() *pathOp {
	d.pathOpCache = append(d.pathOpCache, pathOp{})
	return &d.pathOpCache[len(d.pathOpCache)-1]
}

func (d *drawOps) addClipPath(state *drawState, aux []byte, auxKey opKey, bounds f32.Rectangle, off f32.Point, interior, hole image.Rectangle, hash [2]uint64, raw bool) {
	npath := d.newPathOp()
	*npath = pathOp{
		parent:    state.cpath,
		bounds:    bounds,
		off:       off,
		intersect: bounds.Add(off),
		rect:      true,
		interior:  interior,
		hole:      hole,
		hash:      hash,
	}
	if npath.parent != nil {
		npath.rect = npath.parent.rect
		npath.intersect = npath.parent.intersect.Intersect(npath.intersect)
	}
	if len(aux) > 0 {
		npath.rect = false
		npath.pathKey = auxKey
		npath.path = true
		npath.pathVerts = aux
		npath.raw = raw
		d.pathOps = append(d.pathOps, npath)
	}
	state.cpath = npath
}

func (d *drawOps) save(id int, state f32.Affine2D) {
	if extra := id - len(d.states) + 1; extra > 0 {
		for range extra {
			d.states = append(d.states, f32.AffineId())
		}
	}
	d.states[id] = state
}

func (k opKey) SetTransform(t f32.Affine2D) opKey {
	sx, hx, ox, hy, sy, oy := t.Elems()
	k.sx = sx
	k.hx = hx
	k.hy = hy
	k.sy = sy
	k.ox, k.oy = ox, oy
	return k
}

func (d *drawOps) collectOps(r *ops.Reader, viewport f32.Rectangle) {
	var quads quadsOp
	state := drawState{
		t: f32.AffineId(),
	}
	reset := func() {
		state = drawState{
			t:     f32.AffineId(),
			color: color.NRGBA{A: 0xff},
		}
	}
	reset()
loop:
	for encOp, ok := r.Decode(); ok; encOp, ok = r.Decode() {
		switch ops.OpType(encOp.Data[0]) {
		case ops.TypeTransform:
			dop, push := ops.DecodeTransform(encOp.Data)
			if push {
				d.transStack = append(d.transStack, state.t)
			}
			state.t = state.t.Mul(dop)
		case ops.TypePopTransform:
			n := len(d.transStack)
			state.t = d.transStack[n-1]
			d.transStack = d.transStack[:n-1]

		case ops.TypePushOpacity:
			opacity := ops.DecodeOpacity(encOp.Data)
			parent := -1
			depth := len(d.opacityStack)
			if depth > 0 {
				parent = d.opacityStack[depth-1]
			}
			lidx := len(d.layers)
			d.layers = append(d.layers, opacityLayer{
				opacity: opacity,
				parent:  parent,
				depth:   depth,
				opStart: len(d.imageOps),
			})
			d.opacityStack = append(d.opacityStack, lidx)
		case ops.TypePopOpacity:
			n := len(d.opacityStack)
			idx := d.opacityStack[n-1]
			d.layers[idx].opEnd = len(d.imageOps)
			d.opacityStack = d.opacityStack[:n-1]

		case ops.TypeStroke:
			quads.key.strokeWidth = decodeStrokeOp(encOp.Data)

		case ops.TypePath:
			encOp, ok = r.Decode()
			if !ok {
				break loop
			}
			quads.aux = encOp.Data[ops.TypeAuxLen:]
			quads.key.Key = encOp.Key

		case ops.TypeClipInterior:
			quads.interior = ops.DecodeClipInterior(encOp.Data)
		case ops.TypeClipHole:
			quads.hole = ops.DecodeClipHole(encOp.Data)
		case ops.TypeClip:
			var op ops.ClipOp
			op.Decode(encOp.Data)
			quads.key.outline = op.Outline
			bounds := f32.FRect(op.Bounds)
			trans, off := transformOffset(state.t)
			// the interior and the hole hold under an integer offset
			// only (Clack patches 38, 42)
			var interior, hole image.Rectangle
			if len(quads.aux) > 0 && trans == f32.AffineId() {
				o := image.Pt(int(off.X), int(off.Y))
				if !quads.interior.Empty() {
					interior = quads.interior.Add(o)
				}
				if !quads.hole.Empty() {
					hole = quads.hole.Add(o)
				}
			}
			// hash: the path's geometry, for partial frames (Clack patch 46);
			// raw: aux is path data still to be tessellated
			var hash [2]uint64
			raw := false
			if len(quads.aux) > 0 {
				// There is a clipping path, build the gpu data and update the
				// cache key such that it will be equal only if the transform is the
				// same also. Use cached data if we have it.
				quads.key = quads.key.SetTransform(trans)
				if v, ok := d.pathCache.get(quads.key); ok {
					// Since the GPU data exists in the cache aux will not be used.
					// Why is this not used for the offset shapes?
					bounds = v.bounds
					hash = v.hash
					// Clack patch 46: an entry without GPU data keeps the path
					// data, tessellated only if this frame uploads it
					// (buildPaths). A partial frame uploads only the paths its
					// damage stencils, and a path no op needs stenciled (all
					// in its interior) never: the same ops drawn again (a
					// quiet frame) left their data to be uploaded as if it
					// were vertices, and building them again every frame cost
					// the CPU what Gio tessellated once.
					raw = v.data.data == nil
				} else {
					hash = pathHash(quads.aux, quads.key)
					var pathData []byte
					pathData, bounds = d.buildVerts(
						quads.aux, trans, quads.key.outline, quads.key.strokeWidth,
					)
					d.tessellated++
					quads.aux = pathData
					// add it to the cache, without GPU data, so the transform can be
					// reused.
					d.pathCache.put(quads.key, opCacheValue{bounds: bounds, hash: hash})
				}
			} else {
				quads.aux, bounds, _ = d.boundsForTransformedRect(bounds, trans)
				quads.key = opKey{Key: encOp.Key}
				quads.key = quads.key.SetTransform(trans)
				if len(quads.aux) > 0 {
					// a rectangle under a transform: its vertices are its geometry
					hash = pathHash(quads.aux, opKey{})
				}
			}
			d.addClipPath(&state, quads.aux, quads.key, bounds, off, interior, hole, hash, raw)
			quads = quadsOp{}
		case ops.TypePopClip:
			state.cpath = state.cpath.parent

		case ops.TypeColor:
			state.matType = materialColor
			state.color = decodeColorOp(encOp.Data)
		case ops.TypeLinearGradient:
			state.matType = materialLinearGradient
			op := decodeLinearGradientOp(encOp.Data)
			state.stop1 = op.stop1
			state.stop2 = op.stop2
			state.color1 = op.color1
			state.color2 = op.color2
		case ops.TypeImage:
			state.matType = materialTexture
			state.image = decodeImageOp(encOp.Data, encOp.Refs)
		case ops.TypePaint:
			// Transform (if needed) the painting rectangle and if so generate a clip path,
			// for those cases also compute a partialTrans that maps texture coordinates between
			// the new bounding rectangle and the transformed original paint rectangle.
			t, off := transformOffset(state.t)
			// Fill the clip area, unless the material is a (bounded) image.
			// TODO: Find a tighter bound.
			inf := float32(1e6)
			dst := f32.Rect(-inf, -inf, inf, inf)
			if state.matType == materialTexture {
				sz := state.image.src.Rect.Size()
				dst = f32.Rectangle{Max: layout.FPt(sz)}
			}
			clipData, bnd, partialTrans := d.boundsForTransformedRect(dst, t)
			cl := viewport.Intersect(bnd.Add(off))
			if state.cpath != nil {
				cl = state.cpath.intersect.Intersect(cl)
			}
			if cl.Empty() {
				continue
			}

			if clipData != nil {
				// The paint operation is sheared or rotated, add a clip path representing
				// this transformed rectangle.
				k := opKey{Key: encOp.Key}
				k = k.SetTransform(t)
				d.addClipPath(&state, clipData, k, bnd, off, image.Rectangle{}, image.Rectangle{}, pathHash(clipData, opKey{}), false)
			}

			bounds := cl.Round()
			mat := state.materialFor(bnd, off, partialTrans, bounds)

			rect := state.cpath == nil || state.cpath.rect
			if bounds.Min == (image.Point{}) && bounds.Max == d.viewport && rect && mat.opaque && (mat.material == materialColor) && len(d.opacityStack) == 0 {
				// The image is a uniform opaque color and takes up the whole screen.
				// Scrap images up to and including this image and set clear color.
				clear(d.imageOps) // Clack patch 18
				d.imageOps = d.imageOps[:0]
				// The opacity stack is empty, so every layer recorded so far
				// has ended and is covered by this image. Drop them as well;
				// their op ranges index the scrapped imageOps.
				clear(d.layers) // Clack patch 18
				d.layers = d.layers[:0]
				d.clearColor = mat.color.Opaque()
				d.clear = true
				continue
			}
			img := imageOp{
				path:     state.cpath,
				clip:     bounds,
				material: mat,
			}
			if n := len(d.opacityStack); n > 0 {
				idx := d.opacityStack[n-1]
				lb := d.layers[idx].clip
				if lb.Empty() {
					d.layers[idx].clip = img.clip
				} else {
					d.layers[idx].clip = lb.Union(img.clip)
				}
			}

			if parts, n := splitOp(img.path, img.clip, mat.material); n > 0 {
				// in whole-pixel parts (Clack patch 42)
				for _, c := range parts[:n] {
					part := img
					part.clip = c
					d.imageOps = append(d.imageOps, part)
				}
			} else {
				d.imageOps = append(d.imageOps, img)
			}
			if clipData != nil {
				// we added a clip path that should not remain
				state.cpath = state.cpath.parent
			}
		case ops.TypeSave:
			id := ops.DecodeSave(encOp.Data)
			d.save(id, state.t)
		case ops.TypeLoad:
			reset()
			id := ops.DecodeLoad(encOp.Data)
			state.t = d.states[id]
		}
	}
}

// expandPathOp gives every clip path the op drawn in clip is clipped by
// room for it in the path's stencil regions: one region around all of a
// path's ops, as Gio had it, or several (Clack patch 39). A path that
// leaves clip as it is isn't stenciled for it (patch 38). Rect clips have
// no regions.
func expandPathOp(p *pathOp, clip image.Rectangle) {
	for ; p != nil; p = p.parent {
		if !p.path || p.covers(clip) {
			continue
		}
		p.addRegion(clip)
	}
}

func (d *drawState) materialFor(rect f32.Rectangle, off f32.Point, partTrans f32.Affine2D, clip image.Rectangle) material {
	m := material{
		opacity: 1.,
		uvTrans: f32.AffineId(),
	}
	switch d.matType {
	case materialColor:
		m.material = materialColor
		m.color = f32color.LinearFromSRGB(d.color)
		m.opaque = m.color.A == 1.0
	case materialLinearGradient:
		m.material = materialLinearGradient

		m.color1 = f32color.LinearFromSRGB(d.color1)
		m.color2 = f32color.LinearFromSRGB(d.color2)
		m.opaque = m.color1.A == 1.0 && m.color2.A == 1.0

		m.uvTrans = partTrans.Mul(gradientSpaceTransform(clip, off, d.stop1, d.stop2))
	case materialTexture:
		m.material = materialTexture
		dr := rect.Add(off).Round()
		sz := d.image.src.Bounds().Size()
		sr := f32.Rectangle{
			Max: f32.Point{
				X: float32(sz.X),
				Y: float32(sz.Y),
			},
		}
		dx := float32(dr.Dx())
		sdx := sr.Dx()
		sr.Min.X += float32(clip.Min.X-dr.Min.X) * sdx / dx
		sr.Max.X -= float32(dr.Max.X-clip.Max.X) * sdx / dx
		dy := float32(dr.Dy())
		sdy := sr.Dy()
		sr.Min.Y += float32(clip.Min.Y-dr.Min.Y) * sdy / dy
		sr.Max.Y -= float32(dr.Max.Y-clip.Max.Y) * sdy / dy
		uvScale, uvOffset := texSpaceTransform(sr, sz)
		m.uvTrans = partTrans.Mul(f32.AffineId().Scale(f32.Point{}, uvScale).Offset(uvOffset))
		m.data = d.image
	}
	return m
}

func (r *renderer) uploadImages(cache *textureCache, ops []imageOp) {
	for i := range ops {
		img := &ops[i]
		m := img.material
		if m.material == materialTexture {
			img.material.tex, img.material.texID = r.texHandle(cache, m.data)
		}
	}
}

func (r *renderer) prepareDrawOps(ops []imageOp) {
	for _, img := range ops {
		m := img.material
		switch m.material {
		case materialTexture:
			r.ctx.PrepareTexture(m.tex)
		}

		var fbo FBO
		switch img.clipType {
		case clipTypeNone:
			continue
		case clipTypePath:
			fbo = r.pather.stenciler.cover(img.place.Idx)
		case clipTypeIntersection:
			fbo = r.pather.stenciler.intersections.fbos[img.place.Idx]
		}
		r.ctx.PrepareTexture(fbo.tex)
	}
}

// drawOps draws ops into the open pass; with rects (a partial frame, Clack
// patch 46) only the ops in each damage rectangle, scissored to it.
func (r *renderer) drawOps(isFBO bool, opOff, viewport image.Point, ops []imageOp, rects []passRect) {
	if rects == nil {
		r.drawOpsIn(isFBO, opOff, viewport, ops, image.Rectangle{}, false)
		return
	}
	for _, pr := range rects {
		r.scissor(pr.s)
		r.drawOpsIn(isFBO, opOff, viewport, ops, pr.win, true)
	}
}

// drawOpsIn draws ops, only the ones overlapping win if only.
func (r *renderer) drawOpsIn(isFBO bool, opOff, viewport image.Point, ops []imageOp, win image.Rectangle, only bool) {
	var coverTex driver.Texture
	for i := 0; i < len(ops); i++ {
		img := ops[i]
		i += img.layerOps
		if only && !img.clip.Overlaps(win) {
			continue
		}
		// the pixels it draws: in a partial frame, its part in the damage
		px := area(img.clip.Add(opOff).Intersect(image.Rectangle{Max: viewport}))
		if only {
			px = area(img.clip.Intersect(win))
		}
		m := img.material
		switch m.material {
		case materialTexture:
			r.ctx.BindTexture(0, m.tex)
		}
		drc := img.clip.Add(opOff)

		scale, off := clipSpaceTransform(drc, viewport)
		var fbo FBO
		fboIdx := 0
		if isFBO {
			fboIdx = 1
		}
		r.stats.Draws++
		switch img.clipType {
		case clipTypeNone:
			r.stats.Blits++
			r.stats.BlitPx += px
			p := r.blitter.pipelines[fboIdx][m.material]
			r.ctx.BindPipeline(p.pipeline)
			r.ctx.BindVertexBuffer(r.blitter.quadVerts, 0)
			r.blitter.blit(m.material, isFBO, m.color, m.color1, m.color2, scale, off, m.opacity, m.uvTrans)
			continue
		case clipTypePath:
			fbo = r.pather.stenciler.cover(img.place.Idx)
		case clipTypeIntersection:
			r.stats.Intersections++
			fbo = r.pather.stenciler.intersections.fbos[img.place.Idx]
		}
		r.stats.Covers++
		r.stats.CoverPx += px
		if coverTex != fbo.tex {
			coverTex = fbo.tex
			r.ctx.BindTexture(1, coverTex)
		}
		uv := image.Rectangle{
			Min: img.place.Pos,
			Max: img.place.Pos.Add(drc.Size()),
		}
		coverScale, coverOff := texSpaceTransform(f32.FRect(uv), fbo.size)
		p := r.pather.coverer.pipelines[fboIdx][m.material]
		r.ctx.BindPipeline(p.pipeline)
		r.ctx.BindVertexBuffer(r.blitter.quadVerts, 0)
		r.pather.cover(m.material, isFBO, m.color, m.color1, m.color2, scale, off, m.uvTrans, coverScale, coverOff)
	}
}

func (b *blitter) blit(mat materialType, fbo bool, col f32color.RGBA, col1, col2 f32color.RGBA, scale, off f32.Point, opacity float32, uvTrans f32.Affine2D) {
	fboIdx := 0
	if fbo {
		fboIdx = 1
	}
	p := b.pipelines[fboIdx][mat]
	b.ctx.BindPipeline(p.pipeline)
	var uniforms *blitUniforms
	switch mat {
	case materialColor:
		b.colUniforms.color = col
		uniforms = &b.colUniforms.blitUniforms
	case materialTexture:
		t1, t2, t3, t4, t5, t6 := uvTrans.Elems()
		uniforms = &b.texUniforms.blitUniforms
		uniforms.uvTransformR1 = [4]float32{t1, t2, t3, 0}
		uniforms.uvTransformR2 = [4]float32{t4, t5, t6, 0}
	case materialLinearGradient:
		b.linearGradientUniforms.color1 = col1
		b.linearGradientUniforms.color2 = col2

		t1, t2, t3, t4, t5, t6 := uvTrans.Elems()
		uniforms = &b.linearGradientUniforms.blitUniforms
		uniforms.uvTransformR1 = [4]float32{t1, t2, t3, 0}
		uniforms.uvTransformR2 = [4]float32{t4, t5, t6, 0}
	}
	uniforms.fbo = 0
	if fbo {
		uniforms.fbo = 1
	}
	uniforms.opacity = opacity
	uniforms.transform = [4]float32{scale.X, scale.Y, off.X, off.Y}
	p.UploadUniforms(b.ctx)
	b.ctx.DrawArrays(0, 4)
}

// newUniformBuffer creates a new GPU uniform buffer backed by the
// structure uniformBlock points to.
func newUniformBuffer(b driver.Device, uniforms []byte) *uniformBuffer {
	ubuf, err := b.NewBuffer(driver.BufferBindingUniforms, len(uniforms))
	if err != nil {
		panic(err)
	}
	return &uniformBuffer{buf: ubuf, ptr: uniforms}
}

func (u *uniformBuffer) Upload() {
	u.buf.Upload(u.ptr)
}

func (u *uniformBuffer) Release() {
	u.buf.Release()
	u.buf = nil
}

func (p *pipeline) UploadUniforms(ctx driver.Device) {
	if p.uniforms != nil {
		p.uniforms.Upload()
		ctx.BindUniforms(p.uniforms.buf)
	}
}

func (p *pipeline) Release() {
	p.pipeline.Release()
	if p.uniforms != nil {
		p.uniforms.Release()
	}
	*p = pipeline{}
}

// texSpaceTransform return the scale and offset that transforms the given subimage
// into quad texture coordinates.
func texSpaceTransform(r f32.Rectangle, bounds image.Point) (f32.Point, f32.Point) {
	size := f32.Point{X: float32(bounds.X), Y: float32(bounds.Y)}
	scale := f32.Point{X: r.Dx() / size.X, Y: r.Dy() / size.Y}
	offset := f32.Point{X: r.Min.X / size.X, Y: r.Min.Y / size.Y}
	return scale, offset
}

// gradientSpaceTransform transforms stop1 and stop2 to [(0,0), (1,1)].
func gradientSpaceTransform(clip image.Rectangle, off f32.Point, stop1, stop2 f32.Point) f32.Affine2D {
	d := stop2.Sub(stop1)
	l := float32(math.Sqrt(float64(d.X*d.X + d.Y*d.Y)))
	a := float32(math.Atan2(float64(-d.Y), float64(d.X)))

	// TODO: optimize
	zp := f32.Point{}
	return f32.AffineId().
		Scale(zp, layout.FPt(clip.Size())).            // scale to pixel space
		Offset(zp.Sub(off).Add(layout.FPt(clip.Min))). // offset to clip space
		Offset(zp.Sub(stop1)).                         // offset to first stop point
		Rotate(zp, a).                                 // rotate to align gradient
		Scale(zp, f32.Pt(1/l, 1/l))                    // scale gradient to right size
}

// clipSpaceTransform returns the scale and offset that transforms the given
// rectangle from a viewport into GPU driver device coordinates.
func clipSpaceTransform(r image.Rectangle, viewport image.Point) (f32.Point, f32.Point) {
	// First, transform UI coordinates to device coordinates:
	//
	//	[(-1, -1) (+1, -1)]
	//	[(-1, +1) (+1, +1)]
	//
	x, y := float32(r.Min.X), float32(r.Min.Y)
	w, h := float32(r.Dx()), float32(r.Dy())
	vx, vy := 2/float32(viewport.X), 2/float32(viewport.Y)
	x = x*vx - 1
	y = y*vy - 1
	w *= vx
	h *= vy

	// Then, compute the transformation from the fullscreen quad to
	// the rectangle at (x, y) and dimensions (w, h).
	scale := f32.Point{X: w * .5, Y: h * .5}
	offset := f32.Point{X: x + w*.5, Y: y + h*.5}

	return scale, offset
}

// Fill in maximal Y coordinates of the NW and NE corners.
func fillMaxY(verts []byte) {
	contour := 0
	bo := binary.LittleEndian
	for len(verts) > 0 {
		maxy := float32(math.Inf(-1))
		i := 0
		for ; i+vertStride*4 <= len(verts); i += vertStride * 4 {
			vert := verts[i : i+vertStride]
			// MaxY contains the integer contour index.
			pathContour := int(bo.Uint32(vert[int(unsafe.Offsetof(((*vertex)(nil)).MaxY)):]))
			if contour != pathContour {
				contour = pathContour
				break
			}
			fromy := math.Float32frombits(bo.Uint32(vert[int(unsafe.Offsetof(((*vertex)(nil)).FromY)):]))
			ctrly := math.Float32frombits(bo.Uint32(vert[int(unsafe.Offsetof(((*vertex)(nil)).CtrlY)):]))
			toy := math.Float32frombits(bo.Uint32(vert[int(unsafe.Offsetof(((*vertex)(nil)).ToY)):]))
			if fromy > maxy {
				maxy = fromy
			}
			if ctrly > maxy {
				maxy = ctrly
			}
			if toy > maxy {
				maxy = toy
			}
		}
		fillContourMaxY(maxy, verts[:i])
		verts = verts[i:]
	}
}

func fillContourMaxY(maxy float32, verts []byte) {
	bo := binary.LittleEndian
	for i := 0; i < len(verts); i += vertStride {
		off := int(unsafe.Offsetof(((*vertex)(nil)).MaxY))
		bo.PutUint32(verts[i+off:], math.Float32bits(maxy))
	}
}

func (d *drawOps) writeVertCache(n int) []byte {
	d.vertCache = append(d.vertCache, make([]byte, n)...)
	return d.vertCache[len(d.vertCache)-n:]
}

// transform, split paths as needed, calculate maxY, bounds and create GPU vertices.
func (d *drawOps) buildVerts(pathData []byte, tr f32.Affine2D, outline bool, strWidth float32) (verts []byte, bounds f32.Rectangle) {
	inf := float32(math.Inf(+1))
	d.qs.bounds = f32.Rectangle{
		Min: f32.Point{X: inf, Y: inf},
		Max: f32.Point{X: -inf, Y: -inf},
	}
	d.qs.d = d
	startLength := len(d.vertCache)

	switch {
	case strWidth > 0:
		// Stroke path.
		ss := stroke.StrokeStyle{
			Width: strWidth,
		}
		quads := stroke.StrokePathCommands(ss, pathData)
		for _, quad := range quads {
			d.qs.contour = quad.Contour
			quad.Quad = quad.Quad.Transform(tr)

			d.qs.splitAndEncode(quad.Quad)
		}

	case outline:
		decodeToOutlineQuads(&d.qs, tr, pathData)
	}

	fillMaxY(d.vertCache[startLength:])
	return d.vertCache[startLength:], d.qs.bounds
}

// decodeOutlineQuads decodes scene commands, splits them into quadratic béziers
// as needed and feeds them to the supplied splitter.
func decodeToOutlineQuads(qs *quadSplitter, tr f32.Affine2D, pathData []byte) {
	for len(pathData) >= scene.CommandSize+4 {
		qs.contour = binary.LittleEndian.Uint32(pathData)
		cmd := ops.DecodeCommand(pathData[4:])
		switch cmd.Op() {
		case scene.OpLine:
			var q stroke.QuadSegment
			q.From, q.To = scene.DecodeLine(cmd)
			q.Ctrl = q.From.Add(q.To).Mul(.5)
			q = q.Transform(tr)
			qs.splitAndEncode(q)
		case scene.OpGap:
			var q stroke.QuadSegment
			q.From, q.To = scene.DecodeGap(cmd)
			q.Ctrl = q.From.Add(q.To).Mul(.5)
			q = q.Transform(tr)
			qs.splitAndEncode(q)
		case scene.OpQuad:
			var q stroke.QuadSegment
			q.From, q.Ctrl, q.To = scene.DecodeQuad(cmd)
			q = q.Transform(tr)
			qs.splitAndEncode(q)
		case scene.OpCubic:
			from, ctrl0, ctrl1, to := scene.DecodeCubic(cmd)
			qs.scratch = stroke.SplitCubic(from, ctrl0, ctrl1, to, qs.scratch[:0])
			for _, q := range qs.scratch {
				q = q.Transform(tr)
				qs.splitAndEncode(q)
			}
		default:
			panic("unsupported scene command")
		}
		pathData = pathData[scene.CommandSize+4:]
	}
}

// create GPU vertices for transformed r, find the bounds and establish texture transform.
func (d *drawOps) boundsForTransformedRect(r f32.Rectangle, tr f32.Affine2D) (aux []byte, bnd f32.Rectangle, ptr f32.Affine2D) {
	ptr = f32.AffineId()
	if tr == f32.AffineId() {
		// fast-path to allow blitting of pure rectangles.
		bnd = r
		return
	}

	// transform all corners, find new bounds
	corners := [4]f32.Point{
		tr.Transform(r.Min), tr.Transform(f32.Pt(r.Max.X, r.Min.Y)),
		tr.Transform(r.Max), tr.Transform(f32.Pt(r.Min.X, r.Max.Y)),
	}
	bnd.Min = f32.Pt(math.MaxFloat32, math.MaxFloat32)
	bnd.Max = f32.Pt(-math.MaxFloat32, -math.MaxFloat32)
	for _, c := range corners {
		if c.X < bnd.Min.X {
			bnd.Min.X = c.X
		}
		if c.Y < bnd.Min.Y {
			bnd.Min.Y = c.Y
		}
		if c.X > bnd.Max.X {
			bnd.Max.X = c.X
		}
		if c.Y > bnd.Max.Y {
			bnd.Max.Y = c.Y
		}
	}

	// build the GPU vertices
	l := len(d.vertCache)
	d.vertCache = append(d.vertCache, make([]byte, vertStride*4*4)...)
	aux = d.vertCache[l:]
	encodeQuadTo(aux, 0, corners[0], corners[0].Add(corners[1]).Mul(0.5), corners[1])
	encodeQuadTo(aux[vertStride*4:], 0, corners[1], corners[1].Add(corners[2]).Mul(0.5), corners[2])
	encodeQuadTo(aux[vertStride*4*2:], 0, corners[2], corners[2].Add(corners[3]).Mul(0.5), corners[3])
	encodeQuadTo(aux[vertStride*4*3:], 0, corners[3], corners[3].Add(corners[0]).Mul(0.5), corners[0])
	fillMaxY(aux)

	// establish the transform mapping from bounds rectangle to transformed corners
	var P1, P2, P3 f32.Point
	P1.X = (corners[1].X - bnd.Min.X) / (bnd.Max.X - bnd.Min.X)
	P1.Y = (corners[1].Y - bnd.Min.Y) / (bnd.Max.Y - bnd.Min.Y)
	P2.X = (corners[2].X - bnd.Min.X) / (bnd.Max.X - bnd.Min.X)
	P2.Y = (corners[2].Y - bnd.Min.Y) / (bnd.Max.Y - bnd.Min.Y)
	P3.X = (corners[3].X - bnd.Min.X) / (bnd.Max.X - bnd.Min.X)
	P3.Y = (corners[3].Y - bnd.Min.Y) / (bnd.Max.Y - bnd.Min.Y)
	sx, sy := P2.X-P3.X, P2.Y-P3.Y
	ptr = f32.NewAffine2D(sx, P2.X-P1.X, P1.X-sx, sy, P2.Y-P1.Y, P1.Y-sy).Invert()

	return aux, bnd, ptr
}

// transformOffset a transform into two parts, one which is pure integer offset
// and the other representing the scaling, shearing and rotation and fractional
// offset.
func transformOffset(t f32.Affine2D) (f32.Affine2D, f32.Point) {
	sx, hx, ox, hy, sy, oy := t.Elems()
	iox, fox := math.Modf(float64(ox))
	ioy, foy := math.Modf(float64(oy))
	ft := f32.NewAffine2D(sx, hx, float32(fox), hy, sy, float32(foy))
	ip := f32.Pt(float32(iox), float32(ioy))
	return ft, ip
}

func newShaders(ctx driver.Device, vsrc, fsrc shader.Sources) (vert driver.VertexShader, frag driver.FragmentShader, err error) {
	vert, err = ctx.NewVertexShader(vsrc)
	if err != nil {
		return
	}
	frag, err = ctx.NewFragmentShader(fsrc)
	if err != nil {
		vert.Release()
	}
	return
}
