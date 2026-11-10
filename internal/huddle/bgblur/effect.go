// Package bgblur is Slack's huddle background blur, on the CPU.
//
// Slack (web client bv1-13, the background.worker chunk and the
// video-effects module it loads) runs MediaPipe's image segmenter in a
// worker on every frame of a MediaStreamTrackProcessor, then three WebGL
// passes; Clack runs the same passes on NV12 frames on the camera
// goroutine:
//
//  1. Segmentation, every decimation-th frame (cadence.go, Slack's
//     MaskCadenceController: every second frame to begin with). The mask is
//     the background's confidence.
//  2. MultiBilateralShader.fillMask: four passes over the mask with a 15×15
//     kernel (Gaussian σ 4 in taps of 4, 2, 4/3 and 1 source pixels),
//     weighting each tap by (m - 0.5)² and by the colour distance to the
//     centre pixel (through normpdf, which ignores its σ argument and uses
//     4, so that weight is nearly flat), each pass written to an 8-bit
//     texture. Taps smoothstep(0, 1, m); pixels above 0.95 or below 0.05
//     are snapped to 1 or 0.
//  3. BlurShader: a separable, mask-weighted 25-tap Gaussian (σ 12 px,
//     Slack's 13 precomputed weights) of the frame weighted by
//     c = smoothstep(0.4, 0.6, mask), divided by the summed weight; pixels
//     with c < 0.001 keep the camera's picture. There is no other mix:
//     background colour never bleeds into the person and the person never
//     bleeds into the blur.
//
// The model differs: Slack's worker loads selfie_multiclass_256x256 (a
// 16 MB vision transformer, 4.2 G multiply-adds, on the GPU delegate);
// Clack runs MediaPipe's selfie segmenter, landscape (32.7 M), which the
// Chime SDK's BackgroundBlurProcessor uses, through internal/tflite.
package bgblur

import (
	"math"
	"runtime"
	"sync"
	"time"
)

// Frame is an NV12 picture (huddle.CameraFrame's layout): full-size luma,
// half-size interleaved Cb,Cr. Apply changes it in place.
type Frame struct {
	W, H      int
	Y, UV     []byte
	YStride   int
	UVStride  int
	FullRange bool
	At        time.Time
}

// Effect blurs the background of a stream of frames. It keeps the last
// mask between frames, so one Effect belongs to one camera; it is not
// safe for concurrent use.
type Effect struct {
	seg     *Segmenter
	cad     *cadence
	start   time.Time
	threads int

	mw, mh   int
	raw      []float32 // the segmenter's background confidence
	mask     []float32 // after fillMask: what the blur reads
	pingpong [2][]float32
	have     bool

	// the frame on a grid of g×g pixel cells (g even, the grid at most
	// maxGrid wide): YCbCr for the blur, RGB 0..1 for the bilateral pass
	// and the model's input
	g, gw, gh  int
	gy, gu, gv []float32
	gr, gg, gb []float32
	pre, tmp   [4][]float32
	gc         []float32

	// Stats of the last Apply
	Segmented bool
	Took      time.Duration
}

// maxGrid caps the blur grid's width.
const maxGrid = 480

// NewEffect loads the model.
func NewEffect() (*Effect, error) {
	threads := min(runtime.GOMAXPROCS(0), 4)
	seg, err := NewSegmenter(threads)
	if err != nil {
		return nil, err
	}
	return &Effect{seg: seg, cad: newCadence(len(cadenceRungs) - 1), start: time.Now(), threads: threads,
		mw: seg.W, mh: seg.H}, nil
}

// Close frees the model.
func (e *Effect) Close() { e.seg.Close() }

// Reset forgets the mask (the next frame is segmented).
func (e *Effect) Reset() { e.have = false }

// Mask returns the current background mask (1 background, 0 person), the
// segmenter's size, after Slack's bilateral pass.
func (e *Effect) Mask() (w, h int, m []float32) { return e.mw, e.mh, e.mask }

// Decimation is how many frames each mask is used for right now.
func (e *Effect) Decimation() int { return e.cad.decimation() }

// Apply blurs f's background in place.
func (e *Effect) Apply(f *Frame) {
	if f.W < 4 || f.H < 4 {
		return
	}
	t0 := time.Now()
	at := f.At
	if at.IsZero() {
		at = t0
	}
	e.cad.trackSourcePeriod(float64(at.UnixMicro()))
	e.buildGrid(f)
	e.Segmented = false
	if e.cad.shouldSegment(e.have) {
		e.segment()
		e.fillMask(f.W, f.H)
		e.have = true
		e.Segmented = true
	}
	e.composite(f)
	e.Took = time.Since(t0)
	now := float64(time.Since(e.start).Microseconds())/1000 + 1
	e.cad.accumulateUtilization(float64(e.Took.Microseconds())/1000, now)
}

func grow(b []float32, n int) []float32 {
	if cap(b) < n {
		return make([]float32, n)
	}
	return b[:n]
}

// par runs f over [0, n) split into up to e.threads chunks.
func (e *Effect) par(n int, f func(lo, hi int)) {
	parts := min(e.threads, n)
	if parts <= 1 {
		f(0, n)
		return
	}
	var wg sync.WaitGroup
	wg.Add(parts - 1)
	for p := 1; p < parts; p++ {
		lo, hi := n*p/parts, n*(p+1)/parts
		go func() {
			defer wg.Done()
			f(lo, hi)
		}()
	}
	f(0, n/parts)
	wg.Wait()
}

// buildGrid averages f over g×g cells.
func (e *Effect) buildGrid(f *Frame) {
	g := 2
	for f.W/g > maxGrid {
		g += 2
	}
	gw, gh := max(1, f.W/g), max(1, f.H/g)
	e.g, e.gw, e.gh = g, gw, gh
	n := gw * gh
	for _, p := range []*[]float32{&e.gy, &e.gu, &e.gv, &e.gr, &e.gg, &e.gb, &e.gc} {
		*p = grow(*p, n)
	}
	for k := range e.pre {
		e.pre[k] = grow(e.pre[k], n)
		e.tmp[k] = grow(e.tmp[k], n)
	}
	h := g / 2
	invY := 1 / float32(g*g)
	invC := 1 / float32(h*h)
	e.par(gh, func(lo, hi int) {
		for gy := lo; gy < hi; gy++ {
			for gx := 0; gx < gw; gx++ {
				var sy, su, sv int
				for dy := 0; dy < g; dy++ {
					row := f.Y[(gy*g+dy)*f.YStride+gx*g:]
					for _, v := range row[:g] {
						sy += int(v)
					}
				}
				for dy := 0; dy < h; dy++ {
					row := f.UV[(gy*h+dy)*f.UVStride+gx*g:]
					for dx := 0; dx < h; dx++ {
						su += int(row[2*dx])
						sv += int(row[2*dx+1])
					}
				}
				i := gy*gw + gx
				y, u, v := float32(sy)*invY, float32(su)*invC, float32(sv)*invC
				e.gy[i], e.gu[i], e.gv[i] = y, u, v
				r, gg, b := toRGB(y, u, v, f.FullRange)
				e.gr[i], e.gg[i], e.gb[i] = r, gg, b
			}
		}
	})
}

// toRGB is BT.601 (the self preview's conversion), to 0..1.
func toRGB(y, u, v float32, full bool) (r, g, b float32) {
	cb, cr := u-128, v-128
	if full {
		r, g, b = y+1.402*cr, y-0.344136*cb-0.714136*cr, y+1.772*cb
	} else {
		yy := (y - 16) * 1.164383
		r, g, b = yy+1.596027*cr, yy-0.391762*cb-0.812968*cr, yy+2.017232*cb
	}
	return clamp01(r / 255), clamp01(g / 255), clamp01(b / 255)
}

func clamp01(v float32) float32 { return min(max(v, 0), 1) }

// tap is one bilinear sample along an axis: CLAMP_TO_EDGE + LINEAR.
type tap struct {
	i0, i1 int32
	f      float32
}

func mkTap(t float32, n int) tap {
	fl := float32(math.Floor(float64(t)))
	i0 := int(fl)
	f := t - fl
	i1 := i0 + 1
	i0 = min(max(i0, 0), n-1)
	i1 = min(max(i1, 0), n-1)
	return tap{int32(i0), int32(i1), f}
}

// texTap samples a texture of n texels at normalized coordinate x.
func texTap(x float32, n int) tap { return mkTap(x*float32(n)-0.5, n) }

func bilerp(p []float32, w int, tx, ty tap) float32 {
	r0, r1 := int(ty.i0)*w, int(ty.i1)*w
	a, b := p[r0+int(tx.i0)], p[r0+int(tx.i1)]
	c, d := p[r1+int(tx.i0)], p[r1+int(tx.i1)]
	top := a + (b-a)*tx.f
	bot := c + (d-c)*tx.f
	return top + (bot-top)*ty.f
}

// segment fills the model's input from the grid (stretched to the model's
// 256×144, as MediaPipe's ImageToTensor does) and keeps 1 - person.
func (e *Effect) segment() {
	mw, mh := e.mw, e.mh
	in := e.seg.Input()
	xs := make([]tap, mw)
	for x := range xs {
		xs[x] = texTap((float32(x)+0.5)/float32(mw), e.gw)
	}
	for y := 0; y < mh; y++ {
		ty := texTap((float32(y)+0.5)/float32(mh), e.gh)
		for x := 0; x < mw; x++ {
			o := in[(y*mw+x)*3:]
			o[0] = bilerp(e.gr, e.gw, xs[x], ty)
			o[1] = bilerp(e.gg, e.gw, xs[x], ty)
			o[2] = bilerp(e.gb, e.gw, xs[x], ty)
		}
	}
	p := e.seg.Run()
	e.raw = grow(e.raw, mw*mh)
	for i, v := range p[:mw*mh] {
		e.raw[i] = 1 - v
	}
}
