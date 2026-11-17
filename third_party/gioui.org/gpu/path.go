// SPDX-License-Identifier: Unlicense OR MIT

package gpu

// GPU accelerated path drawing using the algorithms from
// Pathfinder (https://github.com/servo/pathfinder).

import (
	"encoding/binary"
	"image"
	"math"
	"unsafe"

	"gioui.org/gpu/internal/driver"
	"gioui.org/internal/byteslice"
	"gioui.org/internal/f32"
	"gioui.org/internal/f32color"
	"gioui.org/shader"
	"gioui.org/shader/gio"
)

type pather struct {
	ctx driver.Device

	viewport image.Point

	stenciler *stenciler
	coverer   *coverer
}

type coverer struct {
	ctx                    driver.Device
	pipelines              [2][3]*pipeline
	texUniforms            *coverTexUniforms
	colUniforms            *coverColUniforms
	linearGradientUniforms *coverLinearGradientUniforms
}

type coverTexUniforms struct {
	coverUniforms
	_ [12]byte // Padding to multiple of 16.
}

type coverColUniforms struct {
	coverUniforms
	_ [128 - unsafe.Sizeof(coverUniforms{}) - unsafe.Sizeof(colorUniforms{})]byte // Padding to 128 bytes.
	colorUniforms
}

type coverLinearGradientUniforms struct {
	coverUniforms
	_ [128 - unsafe.Sizeof(coverUniforms{}) - unsafe.Sizeof(gradientUniforms{})]byte // Padding to 128.
	gradientUniforms
}

type coverUniforms struct {
	transform        [4]float32
	uvCoverTransform [4]float32
	uvTransformR1    [4]float32
	uvTransformR2    [4]float32
	fbo              float32
}

type stenciler struct {
	ctx      driver.Device
	pipeline struct {
		pipeline *pipeline
		uniforms *stencilUniforms
	}
	ipipeline struct {
		pipeline *pipeline
		uniforms *intersectUniforms
	}
	fbos          fboSet
	intersections fboSet
	indexBuf      driver.Buffer
}

type stencilUniforms struct {
	transform  [4]float32
	pathOffset [2]float32
	_          [8]byte // Padding to multiple of 16.
}

type intersectUniforms struct {
	vert struct {
		uvTransform    [4]float32
		subUVTransform [4]float32
	}
}

type fboSet struct {
	fbos []FBO
}

type FBO struct {
	size image.Point
	tex  driver.Texture
	// idle counts the frames in a row this texture was more than twice
	// the size its page needed, unused the frames in a row its page
	// wasn't needed at all (Clack patch 40)
	idle, unused int
}

// fboIdleFrames is how long a texture may stay much bigger than needed, or
// unused, before it is made again to size or let go (Clack patch 40).
const fboIdleFrames = 60

type pathData struct {
	ncurves int
	data    driver.Buffer
}

// vertex data suitable for passing to vertex programs.
type vertex struct {
	// Corner encodes the corner: +0.5 for south, +.25 for east.
	Corner       float32
	MaxY         float32
	FromX, FromY float32
	CtrlX, CtrlY float32
	ToX, ToY     float32
}

// encode needs to stay in-sync with the code in clip.go encodeQuadTo.
func (v vertex) encode(d []byte, maxy uint32) {
	d = d[0:32]
	bo := binary.LittleEndian
	bo.PutUint32(d[0:4], math.Float32bits(v.Corner))
	bo.PutUint32(d[4:8], maxy)
	bo.PutUint32(d[8:12], math.Float32bits(v.FromX))
	bo.PutUint32(d[12:16], math.Float32bits(v.FromY))
	bo.PutUint32(d[16:20], math.Float32bits(v.CtrlX))
	bo.PutUint32(d[20:24], math.Float32bits(v.CtrlY))
	bo.PutUint32(d[24:28], math.Float32bits(v.ToX))
	bo.PutUint32(d[28:32], math.Float32bits(v.ToY))
}

const (
	// Number of path quads per draw batch.
	pathBatchSize = 10000
	// Size of a vertex as sent to gpu
	vertStride = 8 * 4
)

func newPather(ctx driver.Device) *pather {
	return &pather{
		ctx:       ctx,
		stenciler: newStenciler(ctx),
		coverer:   newCoverer(ctx),
	}
}

func newCoverer(ctx driver.Device) *coverer {
	c := &coverer{
		ctx: ctx,
	}
	c.colUniforms = new(coverColUniforms)
	c.texUniforms = new(coverTexUniforms)
	c.linearGradientUniforms = new(coverLinearGradientUniforms)
	pipelines, err := createColorPrograms(ctx, gio.Shader_cover_vert, gio.Shader_cover_frag,
		[...][]byte{
			byteslice.View(c.colUniforms),
			byteslice.View(c.linearGradientUniforms),
			byteslice.View(c.texUniforms),
		},
	)
	if err != nil {
		panic(err)
	}
	c.pipelines = pipelines
	return c
}

func newStenciler(ctx driver.Device) *stenciler {
	// Allocate a suitably large index buffer for drawing paths.
	indices := make([]uint16, pathBatchSize*6)
	for i := range pathBatchSize {
		i := uint16(i)
		indices[i*6+0] = i*4 + 0
		indices[i*6+1] = i*4 + 1
		indices[i*6+2] = i*4 + 2
		indices[i*6+3] = i*4 + 2
		indices[i*6+4] = i*4 + 1
		indices[i*6+5] = i*4 + 3
	}
	indexBuf, err := ctx.NewImmutableBuffer(driver.BufferBindingIndices, byteslice.Slice(indices))
	if err != nil {
		panic(err)
	}
	progLayout := driver.VertexLayout{
		Inputs: []driver.InputDesc{
			{Type: shader.DataTypeFloat, Size: 1, Offset: int(unsafe.Offsetof((*(*vertex)(nil)).Corner))},
			{Type: shader.DataTypeFloat, Size: 1, Offset: int(unsafe.Offsetof((*(*vertex)(nil)).MaxY))},
			{Type: shader.DataTypeFloat, Size: 2, Offset: int(unsafe.Offsetof((*(*vertex)(nil)).FromX))},
			{Type: shader.DataTypeFloat, Size: 2, Offset: int(unsafe.Offsetof((*(*vertex)(nil)).CtrlX))},
			{Type: shader.DataTypeFloat, Size: 2, Offset: int(unsafe.Offsetof((*(*vertex)(nil)).ToX))},
		},
		Stride: vertStride,
	}
	iprogLayout := driver.VertexLayout{
		Inputs: []driver.InputDesc{
			{Type: shader.DataTypeFloat, Size: 2, Offset: 0},
			{Type: shader.DataTypeFloat, Size: 2, Offset: 4 * 2},
		},
		Stride: 4 * 4,
	}
	st := &stenciler{
		ctx:      ctx,
		indexBuf: indexBuf,
	}
	vsh, fsh, err := newShaders(ctx, gio.Shader_stencil_vert, gio.Shader_stencil_frag)
	if err != nil {
		panic(err)
	}
	defer vsh.Release()
	defer fsh.Release()
	st.pipeline.uniforms = new(stencilUniforms)
	vertUniforms := newUniformBuffer(ctx, byteslice.View(st.pipeline.uniforms))
	pipe, err := st.ctx.NewPipeline(driver.PipelineDesc{
		VertexShader:   vsh,
		FragmentShader: fsh,
		VertexLayout:   progLayout,
		BlendDesc: driver.BlendDesc{
			SrcFactor: driver.BlendFactorOne,
			DstFactor: driver.BlendFactorOne,
		},
		PixelFormat: driver.TextureFormatFloat,
		Topology:    driver.TopologyTriangles,
	})
	st.pipeline.pipeline = &pipeline{pipe, vertUniforms}
	if err != nil {
		panic(err)
	}
	vsh, fsh, err = newShaders(ctx, gio.Shader_intersect_vert, gio.Shader_intersect_frag)
	if err != nil {
		panic(err)
	}
	defer vsh.Release()
	defer fsh.Release()
	st.ipipeline.uniforms = new(intersectUniforms)
	vertUniforms = newUniformBuffer(ctx, byteslice.View(&st.ipipeline.uniforms.vert))
	ipipe, err := st.ctx.NewPipeline(driver.PipelineDesc{
		VertexShader:   vsh,
		FragmentShader: fsh,
		VertexLayout:   iprogLayout,
		BlendDesc: driver.BlendDesc{
			SrcFactor: driver.BlendFactorDstColor,
			DstFactor: driver.BlendFactorZero,
		},
		PixelFormat: driver.TextureFormatFloat,
		Topology:    driver.TopologyTriangleStrip,
	})
	st.ipipeline.pipeline = &pipeline{ipipe, vertUniforms}
	if err != nil {
		panic(err)
	}
	return st
}

func (s *fboSet) resize(ctx driver.Device, format driver.TextureFormat, sizes []image.Point) {
	// Add fbos.
	for i := len(s.fbos); i < len(sizes); i++ {
		s.fbos = append(s.fbos, FBO{})
	}
	// Resize fbos.
	for i, sz := range sizes {
		f := &s.fbos[i]
		f.unused = 0
		// Resizing or recreating FBOs can introduce rendering stalls.
		// Clack patch 40: the waste check here could never trip (needed
		// over current is at most 1 when it fits), so an atlas page kept
		// its biggest size for good, and every frame cleared and stored
		// all of it. A texture more than twice what its page needs for
		// fboIdleFrames in a row is made again to size.
		grow := f.tex != nil && (sz.X > f.size.X || sz.Y > f.size.Y)
		resize := f.tex == nil || grow
		if !resize && f.size.X*f.size.Y > 2*sz.X*sz.Y {
			f.idle++
			resize = f.idle >= fboIdleFrames
		} else {
			f.idle = 0
		}
		if resize {
			f.idle = 0
			if f.tex != nil {
				f.tex.Release()
			}
			// Add 5% extra space in each dimension to minimize resizing.
			sz = sz.Mul(105).Div(100)
			if grow {
				// Clack patch 44: a grow keeps the dimension that still
				// fits. Making the texture just the new need dropped it,
				// so a page whose needs alternate between wide and tall
				// was made again on every frame that used it; the
				// oversize rule above still brings it down to size.
				if f.size.X > sz.X {
					sz.X = f.size.X
				}
				if f.size.Y > sz.Y {
					sz.Y = f.size.Y
				}
			}
			max := ctx.Caps().MaxTextureSize
			if sz.Y > max {
				sz.Y = max
			}
			if sz.X > max {
				sz.X = max
			}
			tex, err := ctx.NewTexture(format, sz.X, sz.Y, driver.FilterNearest, driver.FilterNearest,
				driver.BufferBindingTexture|driver.BufferBindingFramebuffer)
			if err != nil {
				panic(err)
			}
			f.size = sz
			f.tex = tex
		}
	}
	// Delete extra fbos: the ones this frame didn't need, once they
	// weren't needed for fboIdleFrames (Clack patch 40; a page count going
	// down and up, as when an opacity layer fades in, made and released
	// textures each time).
	n := len(s.fbos)
	for i := len(sizes); i < n; i++ {
		s.fbos[i].unused++
		// a frame that doesn't use the page needs none of it, so it
		// counts toward the page's oversized frames; resetting them
		// here kept a page used only every other frame at its biggest
		// size for good
		s.fbos[i].idle++
	}
	for n > len(sizes) && s.fbos[n-1].unused >= fboIdleFrames {
		n--
	}
	s.delete(ctx, n)
}

func (s *fboSet) delete(ctx driver.Device, idx int) {
	for i := idx; i < len(s.fbos); i++ {
		f := s.fbos[i]
		f.tex.Release()
	}
	s.fbos = s.fbos[:idx]
}

func (s *stenciler) release() {
	s.fbos.delete(s.ctx, 0)
	s.intersections.delete(s.ctx, 0)
	s.pipeline.pipeline.Release()
	s.ipipeline.pipeline.Release()
	s.indexBuf.Release()
}

func (p *pather) release() {
	p.stenciler.release()
	p.coverer.release()
}

func (c *coverer) release() {
	for _, p := range c.pipelines {
		for _, p := range p {
			p.Release()
		}
	}
}

func buildPath(ctx driver.Device, p []byte) pathData {
	buf, err := ctx.NewImmutableBuffer(driver.BufferBindingVertices, p)
	if err != nil {
		panic(err)
	}
	return pathData{
		ncurves: len(p) / vertStride,
		data:    buf,
	}
}

func (p pathData) release() {
	p.data.Release()
}

func (p *pather) begin(sizes []image.Point) {
	p.stenciler.begin(sizes)
}

func (p *pather) stencilPath(bounds image.Rectangle, offset f32.Point, uv image.Point, data pathData) {
	p.stenciler.stencilPath(bounds, offset, uv, data)
}

func (s *stenciler) beginIntersect(sizes []image.Point) {
	// 8 bit coverage is enough, but OpenGL ES only supports single channel
	// floating point formats. Replace with GL_RGB+GL_UNSIGNED_BYTE if
	// no floating point support is available.
	s.intersections.resize(s.ctx, driver.TextureFormatFloat, sizes)
}

func (s *stenciler) cover(idx int) FBO {
	return s.fbos.fbos[idx]
}

func (s *stenciler) begin(sizes []image.Point) {
	s.fbos.resize(s.ctx, driver.TextureFormatFloat, sizes)
}

func (s *stenciler) stencilPath(bounds image.Rectangle, offset f32.Point, uv image.Point, data pathData) {
	s.ctx.Viewport(uv.X, uv.Y, bounds.Dx(), bounds.Dy())
	// Transform UI coordinates to OpenGL coordinates.
	texSize := f32.Point{X: float32(bounds.Dx()), Y: float32(bounds.Dy())}
	scale := f32.Point{X: 2 / texSize.X, Y: 2 / texSize.Y}
	orig := f32.Point{X: -1 - float32(bounds.Min.X)*2/texSize.X, Y: -1 - float32(bounds.Min.Y)*2/texSize.Y}
	s.pipeline.uniforms.transform = [4]float32{scale.X, scale.Y, orig.X, orig.Y}
	s.pipeline.uniforms.pathOffset = [2]float32{offset.X, offset.Y}
	s.pipeline.pipeline.UploadUniforms(s.ctx)
	// Draw in batches that fit in uint16 indices.
	start := 0
	nquads := data.ncurves / 4
	for start < nquads {
		batch := nquads - start
		if max := pathBatchSize; batch > max {
			batch = max
		}
		off := vertStride * start * 4
		s.ctx.BindVertexBuffer(data.data, off)
		s.ctx.DrawElements(0, batch*6)
		start += batch
	}
}

func (p *pather) cover(mat materialType, isFBO bool, col f32color.RGBA, col1, col2 f32color.RGBA, scale, off f32.Point, uvTrans f32.Affine2D, coverScale, coverOff f32.Point) {
	p.coverer.cover(mat, isFBO, col, col1, col2, scale, off, uvTrans, coverScale, coverOff)
}

func (c *coverer) cover(mat materialType, isFBO bool, col f32color.RGBA, col1, col2 f32color.RGBA, scale, off f32.Point, uvTrans f32.Affine2D, coverScale, coverOff f32.Point) {
	var uniforms *coverUniforms
	switch mat {
	case materialColor:
		c.colUniforms.color = col
		uniforms = &c.colUniforms.coverUniforms
	case materialLinearGradient:
		c.linearGradientUniforms.color1 = col1
		c.linearGradientUniforms.color2 = col2

		t1, t2, t3, t4, t5, t6 := uvTrans.Elems()
		c.linearGradientUniforms.uvTransformR1 = [4]float32{t1, t2, t3, 0}
		c.linearGradientUniforms.uvTransformR2 = [4]float32{t4, t5, t6, 0}
		uniforms = &c.linearGradientUniforms.coverUniforms
	case materialTexture:
		t1, t2, t3, t4, t5, t6 := uvTrans.Elems()
		c.texUniforms.uvTransformR1 = [4]float32{t1, t2, t3, 0}
		c.texUniforms.uvTransformR2 = [4]float32{t4, t5, t6, 0}
		uniforms = &c.texUniforms.coverUniforms
	}
	uniforms.fbo = 0
	if isFBO {
		uniforms.fbo = 1
	}
	uniforms.transform = [4]float32{scale.X, scale.Y, off.X, off.Y}
	uniforms.uvCoverTransform = [4]float32{coverScale.X, coverScale.Y, coverOff.X, coverOff.Y}
	fboIdx := 0
	if isFBO {
		fboIdx = 1
	}
	c.pipelines[fboIdx][mat].UploadUniforms(c.ctx)
	c.ctx.DrawArrays(0, 4)
}

func init() {
	// Check that struct vertex has the expected size and
	// that it contains no padding.
	if unsafe.Sizeof(*(*vertex)(nil)) != vertStride {
		panic("unexpected struct size")
	}
}
