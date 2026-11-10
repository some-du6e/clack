package tflite

import (
	"encoding/binary"
	"fmt"
	"math"
)

// The operators Interpreter runs: exactly the ones MediaPipe's selfie
// segmenter (selfie_segmenter_landscape.tflite) is built from. Every kernel
// is float32 NHWC with batch 1, written as plain loops over contiguous
// channel slices with bounds-check-free inner bodies, so that a SIMD
// version (assembly, or Go's simd package) can replace an inner loop
// without changing the data layout.
//
//	ADD, MUL                (with broadcasting and fused activations)
//	CONV_2D                 (any kernel, stride, dilation, SAME/VALID)
//	DEPTHWISE_CONV_2D       (depth multiplier 1)
//	DEQUANTIZE              (float16 constants, folded at load)
//	HARD_SWISH, LOGISTIC, RELU
//	MEAN                    (over height and width)
//	RESIZE_BILINEAR         (align_corners, half_pixel_centers)
//	CUSTOM Convolution2DTransposeBias (MediaPipe's transposed conv + bias)

// fused activations (schema.fbs ActivationFunctionType)
const (
	actNone   = 0
	actRelu   = 1
	actReluN1 = 2
	actRelu6  = 3
	actTanh   = 4
)

func (it *Interpreter) compile(k int, op Operator) (func(), error) {
	in := func(j int) []float32 {
		if j >= len(op.Inputs) || op.Inputs[j] < 0 {
			return nil
		}
		return it.data[op.Inputs[j]]
	}
	shape := func(j int) []int {
		if j >= len(op.Inputs) || op.Inputs[j] < 0 {
			return nil
		}
		return it.shapes[op.Inputs[j]]
	}
	out := it.data[op.Outputs[0]]
	oshape := it.shapes[op.Outputs[0]]
	fail := func(format string, a ...any) error {
		return fmt.Errorf("tflite: op %d %s: %s", k, op.Code, fmt.Sprintf(format, a...))
	}
	opt := op.opts
	switch op.Code {
	case OpConv2D:
		if len(shape(0)) != 4 || len(shape(1)) != 4 {
			return nil, fail("want 4-D input and filter")
		}
		c := convParams{
			same: opt.u8(0, 0) == 0, sw: int(opt.i32(1, 1)), sh: int(opt.i32(2, 1)),
			act: int(opt.u8(3, 0)), dw: int(opt.i32(4, 1)), dh: int(opt.i32(5, 1)),
		}
		return it.conv2D(c, in(0), shape(0), in(1), shape(1), in(2), out, oshape, fail)
	case OpDepthwiseConv2D:
		c := convParams{
			same: opt.u8(0, 0) == 0, sw: int(opt.i32(1, 1)), sh: int(opt.i32(2, 1)),
			act: int(opt.u8(4, 0)), dw: int(opt.i32(5, 1)), dh: int(opt.i32(6, 1)),
		}
		if m := opt.i32(3, 1); m != 1 {
			// the multiplier is implied by the shapes; only 1 is implemented
			if shape(0)[3] != shape(1)[3] {
				return nil, fail("depth multiplier %d not supported", m)
			}
		}
		return it.depthwise(c, in(0), shape(0), in(1), shape(1), in(2), out, oshape, fail)
	case OpAdd, OpMul:
		act := int(opt.u8(0, 0))
		return binary4(op.Code == OpMul, act, in(0), shape(0), in(1), shape(1), out, oshape, fail)
	case OpRelu:
		a := in(0)
		return func() {
			for i, v := range a[:len(out)] {
				out[i] = max(v, 0)
			}
		}, nil
	case OpHardSwish:
		a := in(0)
		return func() {
			for i, v := range a[:len(out)] {
				out[i] = v * min(6, max(0, v+3)) / 6
			}
		}, nil
	case OpLogistic:
		a := in(0)
		return func() {
			for i, v := range a[:len(out)] {
				out[i] = sigmoid(v)
			}
		}, nil
	case OpMean:
		axes, err := it.ints(op.Inputs[1])
		if err != nil {
			return nil, fail("%v", err)
		}
		s := shape(0)
		if len(s) != 4 || len(axes) != 2 || !((axes[0] == 1 && axes[1] == 2) || (axes[0] == 2 && axes[1] == 1)) {
			return nil, fail("only the mean over height and width of a 4-D tensor is supported (axes %v)", axes)
		}
		a := in(0)
		hw, ch := s[1]*s[2], s[3]
		if numElems(oshape) != ch {
			return nil, fail("output %v", oshape)
		}
		acc := make([]float64, ch)
		return func() {
			clear(acc)
			for p := 0; p < hw; p++ {
				row := a[p*ch : p*ch+ch]
				for c, v := range row {
					acc[c] += float64(v)
				}
			}
			for c := range out[:ch] {
				out[c] = float32(acc[c] / float64(hw))
			}
		}, nil
	case OpResizeBilinear:
		s := shape(0)
		if len(s) != 4 || len(oshape) != 4 || s[3] != oshape[3] {
			return nil, fail("shapes %v → %v", s, oshape)
		}
		if size, err := it.ints(op.Inputs[1]); err != nil || len(size) != 2 || size[0] != oshape[1] || size[1] != oshape[2] {
			return nil, fail("size %v doesn't match output %v (%v)", size, oshape, err)
		}
		return resizeBilinear(in(0), s, out, oshape, opt.b(2), opt.b(3), it.pool), nil
	case OpCustom:
		if op.Custom == "Convolution2DTransposeBias" {
			return it.transposeConvBias(op, in(0), shape(0), in(1), shape(1), in(2), out, oshape, fail)
		}
		return nil, fail("custom operator %q not supported", op.Custom)
	}
	return nil, fail("%v", errUnsupported)
}

type convParams struct {
	same           bool
	sw, sh, dw, dh int
	act            int
}

// convOut is TFLite's ComputeOutSize and ComputePaddingWithOffset: the
// output size and the padding before the first row/column.
func convOut(in, k, s, d int, same bool) (out, before int) {
	ek := (k-1)*d + 1
	if same {
		out = (in + s - 1) / s
	} else {
		out = (in - ek + s) / s
	}
	total := max((out-1)*s+ek-in, 0)
	return out, total / 2
}

func (it *Interpreter) conv2D(c convParams, x []float32, xs []int, w []float32, ws []int, bias []float32, out []float32, os []int, fail func(string, ...any) error) (func(), error) {
	H, W, Ci := xs[1], xs[2], xs[3]
	Co, KH, KW := ws[0], ws[1], ws[2]
	if ws[3] != Ci {
		return nil, fail("filter %v for input %v", ws, xs)
	}
	OH, pt := convOut(H, KH, c.sh, c.dh, c.same)
	OW, pl := convOut(W, KW, c.sw, c.dw, c.same)
	if len(os) != 4 || os[1] != OH || os[2] != OW || os[3] != Co {
		return nil, fail("output %v, want [1 %d %d %d]", os, OH, OW, Co)
	}
	if bias != nil && len(bias) != Co {
		return nil, fail("bias of %d for %d channels", len(bias), Co)
	}
	// filter OHWI → (HWI)×O: one contiguous row of output channels per tap
	K := KH * KW * Ci
	wt := make([]float32, K*Co)
	for o := 0; o < Co; o++ {
		for r := 0; r < K; r++ {
			wt[r*Co+o] = w[o*K+r]
		}
	}
	act := c.act
	work := OH * OW * K * Co
	if KH == 1 && KW == 1 && c.sh == 1 && c.sw == 1 && pt == 0 && pl == 0 {
		return func() {
			it.pool.rows(OH, work, func(lo, hi int) {
				for p := lo * OW; p < hi*OW; p++ {
					d := out[p*Co : p*Co+Co]
					gemv(d, x[p*Ci:p*Ci+Ci], wt, bias)
					activate(d, act)
				}
			})
		}, nil
	}
	return func() {
		it.pool.rows(OH, work, func(lo, hi int) {
			patch := make([]float32, K)
			for oy := lo; oy < hi; oy++ {
				for ox := 0; ox < OW; ox++ {
					// im2col for one output pixel: zeros where the
					// kernel hangs over the edge
					r := 0
					for ky := 0; ky < KH; ky++ {
						iy := oy*c.sh - pt + ky*c.dh
						for kx := 0; kx < KW; kx++ {
							ix := ox*c.sw - pl + kx*c.dw
							dst := patch[r : r+Ci]
							if iy < 0 || iy >= H || ix < 0 || ix >= W {
								clear(dst)
							} else {
								copy(dst, x[(iy*W+ix)*Ci:])
							}
							r += Ci
						}
					}
					p := oy*OW + ox
					d := out[p*Co : p*Co+Co]
					gemv(d, patch, wt, bias)
					activate(d, act)
				}
			}
		})
	}, nil
}

// gemv sets dst = bias + a·w, where w is len(a) rows of len(dst) output
// channels. Eight output channels stay in registers while a streams past.
func gemv(dst, a, w, bias []float32) {
	co := len(dst)
	cb := 0
	for ; cb+8 <= co; cb += 8 {
		var s0, s1, s2, s3, s4, s5, s6, s7 float32
		if bias != nil {
			b := bias[cb : cb+8 : cb+8]
			s0, s1, s2, s3, s4, s5, s6, s7 = b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7]
		}
		off := cb
		for _, av := range a {
			r := w[off : off+8 : off+8]
			s0 += av * r[0]
			s1 += av * r[1]
			s2 += av * r[2]
			s3 += av * r[3]
			s4 += av * r[4]
			s5 += av * r[5]
			s6 += av * r[6]
			s7 += av * r[7]
			off += co
		}
		d := dst[cb : cb+8 : cb+8]
		d[0], d[1], d[2], d[3], d[4], d[5], d[6], d[7] = s0, s1, s2, s3, s4, s5, s6, s7
	}
	for ; cb < co; cb++ {
		var s float32
		if bias != nil {
			s = bias[cb]
		}
		off := cb
		for _, av := range a {
			s += av * w[off]
			off += co
		}
		dst[cb] = s
	}
}

func (it *Interpreter) depthwise(c convParams, x []float32, xs []int, w []float32, ws []int, bias []float32, out []float32, os []int, fail func(string, ...any) error) (func(), error) {
	H, W, C := xs[1], xs[2], xs[3]
	KH, KW := ws[1], ws[2]
	if ws[0] != 1 || ws[3] != C {
		return nil, fail("filter %v for input %v", ws, xs)
	}
	OH, pt := convOut(H, KH, c.sh, c.dh, c.same)
	OW, pl := convOut(W, KW, c.sw, c.dw, c.same)
	if len(os) != 4 || os[1] != OH || os[2] != OW || os[3] != C {
		return nil, fail("output %v, want [1 %d %d %d]", os, OH, OW, C)
	}
	act := c.act
	work := OH * OW * KH * KW * C
	return func() {
		it.pool.rows(OH, work, func(lo, hi int) {
			for oy := lo; oy < hi; oy++ {
				for ox := 0; ox < OW; ox++ {
					p := oy*OW + ox
					acc := out[p*C : p*C+C]
					if bias != nil {
						copy(acc, bias)
					} else {
						clear(acc)
					}
					for ky := 0; ky < KH; ky++ {
						iy := oy*c.sh - pt + ky*c.dh
						if iy < 0 || iy >= H {
							continue
						}
						for kx := 0; kx < KW; kx++ {
							ix := ox*c.sw - pl + kx*c.dw
							if ix < 0 || ix >= W {
								continue
							}
							madd(acc, x[(iy*W+ix)*C:(iy*W+ix)*C+C], w[(ky*KW+kx)*C:(ky*KW+kx)*C+C])
						}
					}
					activate(acc, act)
				}
			}
		})
	}, nil
}

// madd sets acc[i] += a[i]*b[i].
func madd(acc, a, b []float32) {
	a = a[:len(acc)]
	b = b[:len(acc)]
	for i := range acc {
		acc[i] += a[i] * b[i]
	}
}

func activate(d []float32, act int) {
	switch act {
	case actRelu:
		for i, v := range d {
			d[i] = max(v, 0)
		}
	case actRelu6:
		for i, v := range d {
			d[i] = min(max(v, 0), 6)
		}
	case actReluN1:
		for i, v := range d {
			d[i] = min(max(v, -1), 1)
		}
	case actTanh:
		for i, v := range d {
			d[i] = float32(math.Tanh(float64(v)))
		}
	}
}

func sigmoid(v float32) float32 { return float32(1 / (1 + math.Exp(-float64(v)))) }

// binary4 is ADD or MUL with NumPy broadcasting over up to four dims.
func binary4(mul bool, act int, a []float32, as []int, b []float32, bs []int, out []float32, os []int, fail func(string, ...any) error) (func(), error) {
	if len(as) > 4 || len(bs) > 4 || len(os) > 4 {
		return nil, fail("more than 4 dims")
	}
	pad := func(s []int) [4]int {
		r := [4]int{1, 1, 1, 1}
		copy(r[4-len(s):], s)
		return r
	}
	pa, pb, po := pad(as), pad(bs), pad(os)
	for d := 0; d < 4; d++ {
		if (pa[d] != po[d] && pa[d] != 1) || (pb[d] != po[d] && pb[d] != 1) {
			return nil, fail("can't broadcast %v and %v to %v", as, bs, os)
		}
	}
	n := len(out)
	op := func(x, y float32) float32 {
		if mul {
			return x * y
		}
		return x + y
	}
	if pa == po && pb == po {
		return func() {
			a, b := a[:n], b[:n]
			if mul {
				for i := range out {
					out[i] = a[i] * b[i]
				}
			} else {
				for i := range out {
					out[i] = a[i] + b[i]
				}
			}
			activate(out, act)
		}, nil
	}
	c := po[3]
	// one per-channel vector against a full tensor (squeeze-and-excite)
	vec := func(full, v []float32) func() {
		return func() {
			v := v[:c]
			for p := 0; p < n; p += c {
				o, f := out[p:p+c], full[p:p+c]
				if mul {
					for i := range o {
						o[i] = f[i] * v[i]
					}
				} else {
					for i := range o {
						o[i] = f[i] + v[i]
					}
				}
			}
			activate(out, act)
		}
	}
	if pa == po && pb == [4]int{1, 1, 1, c} {
		return vec(a, b), nil
	}
	if pb == po && pa == [4]int{1, 1, 1, c} {
		return vec(b, a), nil
	}
	stride := func(s [4]int) [4]int {
		var st [4]int
		m := 1
		for d := 3; d >= 0; d-- {
			if s[d] != 1 {
				st[d] = m
			}
			m *= s[d]
		}
		return st
	}
	sa, sb := stride(pa), stride(pb)
	return func() {
		i := 0
		for d0 := 0; d0 < po[0]; d0++ {
			for d1 := 0; d1 < po[1]; d1++ {
				for d2 := 0; d2 < po[2]; d2++ {
					for d3 := 0; d3 < po[3]; d3++ {
						ia := d0*sa[0] + d1*sa[1] + d2*sa[2] + d3*sa[3]
						ib := d0*sb[0] + d1*sb[1] + d2*sb[2] + d3*sb[3]
						out[i] = op(a[ia], b[ib])
						i++
					}
				}
			}
		}
		activate(out, act)
	}, nil
}

// resizeBilinear is TFLite's reference ResizeBilinear (float).
func resizeBilinear(x []float32, xs []int, out []float32, os []int, alignCorners, halfPixel bool, pool *workers) func() {
	H, W, C := xs[1], xs[2], xs[3]
	OH, OW := os[1], os[2]
	scale := func(in, o int) float32 {
		if alignCorners && o > 1 {
			return float32(in-1) / float32(o-1)
		}
		return float32(in) / float32(o)
	}
	type tap struct {
		lo, hi int
		f      float32
	}
	taps := func(o, in int, s float32) []tap {
		t := make([]tap, o)
		for i := range t {
			v := float32(i) * s
			if halfPixel {
				v = (float32(i)+0.5)*s - 0.5
			}
			fl := float32(math.Floor(float64(v)))
			lo := max(int(fl), 0)
			hi := min(int(math.Ceil(float64(v))), in-1)
			t[i] = tap{lo, hi, v - float32(lo)}
		}
		return t
	}
	ty, tx := taps(OH, H, scale(H, OH)), taps(OW, W, scale(W, OW))
	return func() {
		pool.rows(OH, OH*OW*C*4, func(lo, hi int) {
			for oy := lo; oy < hi; oy++ {
				yt := ty[oy]
				r0, r1 := x[yt.lo*W*C:], x[yt.hi*W*C:]
				for ox := 0; ox < OW; ox++ {
					xt := tx[ox]
					a, b := r0[xt.lo*C:xt.lo*C+C], r0[xt.hi*C:xt.hi*C+C]
					cc, d := r1[xt.lo*C:xt.lo*C+C], r1[xt.hi*C:xt.hi*C+C]
					o := out[(oy*OW+ox)*C : (oy*OW+ox)*C+C]
					fy, fx := yt.f, xt.f
					for i := range o {
						o[i] = a[i]*(1-fy)*(1-fx) + cc[i]*fy*(1-fx) + b[i]*(1-fy)*fx + d[i]*fy*fx
					}
				}
			}
		})
	}
}

// transposeConvBias is MediaPipe's Convolution2DTransposeBias custom op
// (mediapipe/util/tflite/operations/transpose_conv_bias.cc): TFLite's
// reference TransposeConv, then the bias. Its custom options are a raw
// TfLiteTransposeConvParams: padding (1 SAME, 2 VALID), stride_width,
// stride_height, little-endian int32s.
func (it *Interpreter) transposeConvBias(op Operator, x []float32, xs []int, w []float32, ws []int, bias []float32, out []float32, os []int, fail func(string, ...any) error) (func(), error) {
	co := op.CustomOptions
	if len(co) < 12 {
		return nil, fail("custom options %x", co)
	}
	padding := int32(binary.LittleEndian.Uint32(co[0:]))
	sw := int(int32(binary.LittleEndian.Uint32(co[4:])))
	sh := int(int32(binary.LittleEndian.Uint32(co[8:])))
	if sw < 1 || sh < 1 || (padding != 1 && padding != 2) {
		return nil, fail("custom options %x", co)
	}
	H, W, Ci := xs[1], xs[2], xs[3]
	Co, KH, KW := ws[0], ws[1], ws[2]
	if ws[3] != Ci || len(os) != 4 || os[3] != Co || len(bias) != Co {
		return nil, fail("shapes %v %v %v", xs, ws, os)
	}
	OH, OW := os[1], os[2]
	// ComputePaddingHeightWidth with the output as the forward input
	_, pt := convOut(OH, KH, sh, 1, padding == 1)
	_, pl := convOut(OW, KW, sw, 1, padding == 1)
	return func() {
		for p := 0; p < OH*OW; p++ {
			copy(out[p*Co:p*Co+Co], bias)
		}
		for iy := 0; iy < H; iy++ {
			for ix := 0; ix < W; ix++ {
				px := x[(iy*W+ix)*Ci : (iy*W+ix)*Ci+Ci]
				for ky := 0; ky < KH; ky++ {
					oy := iy*sh - pt + ky
					if oy < 0 || oy >= OH {
						continue
					}
					for kx := 0; kx < KW; kx++ {
						ox := ix*sw - pl + kx
						if ox < 0 || ox >= OW {
							continue
						}
						o := out[(oy*OW+ox)*Co : (oy*OW+ox)*Co+Co]
						for c := range o {
							f := w[((c*KH+ky)*KW+kx)*Ci : ((c*KH+ky)*KW+kx)*Ci+Ci]
							o[c] += dot(px, f)
						}
					}
				}
			}
		}
	}, nil
}

func dot(a, b []float32) float32 {
	b = b[:len(a)]
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}
