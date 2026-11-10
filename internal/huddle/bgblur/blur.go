package bgblur

import "math"

// Slack's blur-h.glsl / blur.glsl: "Gaussian weights for SIGMA 12.0,
// normalized so the 25 taps sum to 1", W0…W12.
var blurWeights = [13]float32{0.0473192, 0.0471551, 0.0466665, 0.0458633, 0.0447620, 0.0433848,
	0.0417590, 0.0399160, 0.0378902, 0.0357184, 0.0334380, 0.0310866, 0.0287005}

// blurRadius is blur.glsl's kSize: (MSIZE 25 - 1) / 2 source pixels.
const blurRadius = 12

// The multi-bilateral shader's constants: MSIZE 15, SIGMA 4.0, four
// passes with u_iter 1..4 and a tap spacing of 4 / u_iter pixels.
const (
	bilatSize   = 15
	bilatHalf   = (bilatSize - 1) / 2
	bilatSigma  = 4.0
	bilatPasses = 4
)

var bilatKernel = func() (k [bilatSize]float32) {
	for j := 0; j <= bilatHalf; j++ {
		w := float32(0.39894 / bilatSigma * math.Exp(-0.5*float64(j*j)/(bilatSigma*bilatSigma)))
		k[bilatHalf+j], k[bilatHalf-j] = w, w
	}
	return
}()

func smoothstep(e0, e1, x float32) float32 {
	t := min(max((x-e0)/(e1-e0), 0), 1)
	return t * t * (3 - 2*t)
}

// rangeWeight is the shader's normpdf(distance(sample, centre), BSIGMA)
// up to its constant factor: normpdf ignores its sigma and uses SIGMA 4,
// so this is exp(-d²/32) for d² in [0, 3]. A degree-4 Taylor series is
// within 1e-7 there.
func rangeWeight(d2 float32) float32 {
	t := d2 * (1.0 / 32)
	return 1 - t*(1-t*(0.5-t*(1.0/6-t*(1.0/24))))
}

// quantize8 is a write to Slack's RGBA8 ping-pong textures.
func quantize8(v float32) float32 { return float32(math.Round(float64(clamp01(v))*255)) / 255 }

// fillMask is MultiBilateralShader.fillMask on the segmenter's mask: four
// passes into e.mask. srcW×srcH is the frame (u_unit is one frame pixel).
func (e *Effect) fillMask(srcW, srcH int) {
	mw, mh, gw, gh := e.mw, e.mh, e.gw, e.gh
	n := mw * mh
	e.pingpong[0] = grow(e.pingpong[0], n)
	e.pingpong[1] = grow(e.pingpong[1], n)
	mx := make([]tap, mw*bilatSize)
	gx := make([]tap, mw*bilatSize)
	my := make([]tap, mh*bilatSize)
	gyT := make([]tap, mh*bilatSize)
	src := e.raw
	for a := 0; a < bilatPasses; a++ {
		step := 4 / float32(a+1)
		dst := e.pingpong[(a+1)%2] // pass 0 → B, 1 → A, 2 → B, 3 → A
		for u := 0; u < mw; u++ {
			for i := -bilatHalf; i <= bilatHalf; i++ {
				nx := (float32(u)+0.5)/float32(mw) + float32(i)*step/float32(srcW)
				mx[u*bilatSize+i+bilatHalf] = texTap(nx, mw)
				gx[u*bilatSize+i+bilatHalf] = texTap(nx, gw)
			}
		}
		for v := 0; v < mh; v++ {
			for j := -bilatHalf; j <= bilatHalf; j++ {
				ny := (float32(v)+0.5)/float32(mh) + float32(j)*step/float32(srcH)
				my[v*bilatSize+j+bilatHalf] = texTap(ny, mh)
				gyT[v*bilatSize+j+bilatHalf] = texTap(ny, gh)
			}
		}
		e.par(mh, func(lo, hi int) {
			for v := lo; v < hi; v++ {
				for u := 0; u < mw; u++ {
					m0 := src[v*mw+u]
					conf := smoothstep(0, 1, m0)
					if conf > 0.95 {
						dst[v*mw+u] = 1
						continue
					}
					if conf < 0.05 {
						dst[v*mw+u] = 0
						continue
					}
					ctx, cty := gx[u*bilatSize+bilatHalf], gyT[v*bilatSize+bilatHalf]
					cr := bilerp(e.gr, gw, ctx, cty)
					cg := bilerp(e.gg, gw, ctx, cty)
					cb := bilerp(e.gb, gw, ctx, cty)
					var acc, z float32
					for jj := 0; jj < bilatSize; jj++ {
						tym, tyg := my[v*bilatSize+jj], gyT[v*bilatSize+jj]
						kj := bilatKernel[jj]
						for ii := 0; ii < bilatSize; ii++ {
							m := bilerp(src, mw, mx[u*bilatSize+ii], tym)
							f := (m - 0.5) * (m - 0.5)
							if f == 0 {
								continue
							}
							txg := gx[u*bilatSize+ii]
							dr := bilerp(e.gr, gw, txg, tyg) - cr
							dg := bilerp(e.gg, gw, txg, tyg) - cg
							db := bilerp(e.gb, gw, txg, tyg) - cb
							w := rangeWeight(dr*dr+dg*dg+db*db) * kj * bilatKernel[ii] * f
							z += w
							acc += w * smoothstep(0, 1, m)
						}
					}
					var out float32
					if z > 0 {
						out = acc / z
					}
					dst[v*mw+u] = quantize8(out)
				}
			}
		})
		src = dst
	}
	e.mask = e.pingpong[0]
}

// composite is BlurShader on the frame: the mask-weighted Gaussian over
// the background, computed on the grid and sampled back at full size.
func (e *Effect) composite(f *Frame) {
	g, gw, gh, mw, mh := e.g, e.gw, e.gh, e.mw, e.mh
	// the blur's taps on the grid: Slack's weights at multiples of g
	// pixels, rescaled to Slack's sum so the 1e-6 test means the same
	var wts []float32
	var sum float32
	for k := 0; k*g <= blurRadius; k++ {
		wts = append(wts, blurWeights[k*g])
		if k == 0 {
			sum += blurWeights[0]
		} else {
			sum += 2 * blurWeights[k*g]
		}
	}
	for k := range wts {
		wts[k] /= sum
	}
	K := len(wts) - 1
	// c and the premultiplied field at each grid cell's centre
	e.par(gh, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			ty := texTap((float32(y)+0.5)*float32(g)/float32(f.H), mh)
			for x := 0; x < gw; x++ {
				tx := texTap((float32(x)+0.5)*float32(g)/float32(f.W), mw)
				c := smoothstep(0.4, 0.6, bilerp(e.mask, mw, tx, ty))
				i := y*gw + x
				e.gc[i] = c
				e.pre[0][i], e.pre[1][i], e.pre[2][i], e.pre[3][i] = c*e.gy[i], c*e.gu[i], c*e.gv[i], c
			}
		}
	})
	// horizontal into tmp, vertical back into pre (CLAMP_TO_EDGE)
	e.par(gh, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			for ch := 0; ch < 4; ch++ {
				in, out := e.pre[ch][y*gw:y*gw+gw], e.tmp[ch][y*gw:y*gw+gw]
				for x := range out {
					s := wts[0] * in[x]
					for k := 1; k <= K; k++ {
						s += wts[k] * (in[max(x-k, 0)] + in[min(x+k, gw-1)])
					}
					out[x] = s
				}
			}
		}
	})
	e.par(gh, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			for ch := 0; ch < 4; ch++ {
				in, out := e.tmp[ch], e.pre[ch][y*gw:y*gw+gw]
				for x := range out {
					s := wts[0] * in[y*gw+x]
					for k := 1; k <= K; k++ {
						s += wts[k] * (in[max(y-k, 0)*gw+x] + in[min(y+k, gh-1)*gw+x])
					}
					out[x] = s
				}
			}
		}
	})
	// luma at full size, then chroma at half size: each output row first
	// interpolates its mask row and grid rows vertically, so a pixel is
	// three horizontal lerps
	W, H := f.W, f.H
	e.compositePlane(f.Y, f.YStride, W, H, 1, g, [3][]float32{e.pre[0], nil, e.pre[3]})
	e.compositePlane(f.UV, f.UVStride, W/2, H/2, 2, g, [3][]float32{e.pre[1], e.pre[2], e.pre[3]})
}

// compositePlane writes the blurred background into one plane of pw×ph
// samples (luma: 1 per pixel; chroma: Cb,Cr pairs) from the blurred
// premultiplied grid fields (num0, num1 or nil, den).
func (e *Effect) compositePlane(plane []byte, stride, pw, ph, n int, g int, f [3][]float32) {
	mw, mh, gw, gh := e.mw, e.mh, e.gw, e.gh
	xm := make([]tap, pw)
	xg := make([]tap, pw)
	for x := 0; x < pw; x++ {
		xm[x] = texTap((float32(x)+0.5)/float32(pw), mw)
		xg[x] = mkTap((float32(x)+0.5)*float32(n)/float32(g)-0.5, gw)
	}
	e.par(ph, func(lo, hi int) {
		mrow := make([]float32, mw)
		var rows [3][]float32
		for k := range rows {
			rows[k] = make([]float32, gw)
		}
		lerpRows := func(dst, p []float32, w int, t tap) {
			r0, r1 := p[int(t.i0)*w:int(t.i0)*w+w], p[int(t.i1)*w:int(t.i1)*w+w]
			for i := range dst {
				dst[i] = r0[i] + (r1[i]-r0[i])*t.f
			}
		}
		for y := lo; y < hi; y++ {
			lerpRows(mrow, e.mask, mw, texTap((float32(y)+0.5)/float32(ph), mh))
			ty := mkTap((float32(y)+0.5)*float32(n)/float32(g)-0.5, gh)
			for k, p := range f {
				if p != nil {
					lerpRows(rows[k], p, gw, ty)
				}
			}
			row := plane[y*stride : y*stride+pw*n]
			for x := 0; x < pw; x++ {
				tm := xm[x]
				m := mrow[tm.i0] + (mrow[tm.i1]-mrow[tm.i0])*tm.f
				if m <= 0.4 { // smoothstep(0.4, 0.6, m) is 0
					continue
				}
				if smoothstep(0.4, 0.6, m) < 0.001 {
					continue // foreground: the camera's sample
				}
				tg := xg[x]
				d := rows[2]
				den := d[tg.i0] + (d[tg.i1]-d[tg.i0])*tg.f
				if den <= 1e-6 {
					continue
				}
				a := rows[0]
				row[x*n] = to8((a[tg.i0] + (a[tg.i1]-a[tg.i0])*tg.f) / den)
				if n == 2 {
					b := rows[1]
					row[x*n+1] = to8((b[tg.i0] + (b[tg.i1]-b[tg.i0])*tg.f) / den)
				}
			}
		}
	})
}

func to8(v float32) byte { return byte(min(max(v+0.5, 0), 255)) }
