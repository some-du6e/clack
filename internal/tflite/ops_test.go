package tflite

import (
	"math"
	"math/rand"
	"testing"
)

func randVec(r *rand.Rand, n int) []float32 {
	v := make([]float32, n)
	for i := range v {
		v[i] = r.Float32()*2 - 1
	}
	return v
}

func closeTo(t *testing.T, what string, got, want []float32, tol float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d values, want %d", what, len(got), len(want))
	}
	for i := range got {
		if d := math.Abs(float64(got[i] - want[i])); d > tol || math.IsNaN(float64(got[i])) {
			t.Fatalf("%s: [%d] = %v, want %v", what, i, got[i], want[i])
		}
	}
}

func testFail(t *testing.T) func(string, ...any) error {
	return func(f string, a ...any) error { t.Fatalf(f, a...); return nil }
}

// naiveConv is TensorFlow's conv2d (NHWC input, OHWI filter), written
// independently of convOut: SAME pads ⌊total/2⌋ before.
func naiveConv(x []float32, H, W, Ci int, w []float32, Co, KH, KW int, b []float32, s, d int, same, depthwise bool) ([]float32, int, int) {
	ekh, ekw := (KH-1)*d+1, (KW-1)*d+1
	var OH, OW, pt, pl int
	if same {
		OH, OW = int(math.Ceil(float64(H)/float64(s))), int(math.Ceil(float64(W)/float64(s)))
		pt = max((OH-1)*s+ekh-H, 0) / 2
		pl = max((OW-1)*s+ekw-W, 0) / 2
	} else {
		OH, OW = int(math.Ceil(float64(H-ekh+1)/float64(s))), int(math.Ceil(float64(W-ekw+1)/float64(s)))
	}
	out := make([]float32, OH*OW*Co)
	for oy := 0; oy < OH; oy++ {
		for ox := 0; ox < OW; ox++ {
			for o := 0; o < Co; o++ {
				var acc float64
				if b != nil {
					acc = float64(b[o])
				}
				for ky := 0; ky < KH; ky++ {
					for kx := 0; kx < KW; kx++ {
						iy, ix := oy*s-pt+ky*d, ox*s-pl+kx*d
						if iy < 0 || iy >= H || ix < 0 || ix >= W {
							continue
						}
						if depthwise {
							acc += float64(x[(iy*W+ix)*Ci+o]) * float64(w[(ky*KW+kx)*Co+o])
							continue
						}
						for c := 0; c < Ci; c++ {
							acc += float64(x[(iy*W+ix)*Ci+c]) * float64(w[((o*KH+ky)*KW+kx)*Ci+c])
						}
					}
				}
				out[(oy*OW+ox)*Co+o] = float32(acc)
			}
		}
	}
	return out, OH, OW
}

func TestConv2DMatchesNaive(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	it := &Interpreter{pool: newWorkers(3)}
	for _, c := range []struct {
		H, W, Ci, Co, K, s, d int
		same                  bool
		act                   int
	}{
		{144, 256, 3, 16, 3, 2, 1, true, actNone}, // the model's stem
		{9, 16, 32, 128, 1, 1, 1, false, actNone}, // 1×1 VALID
		{7, 9, 5, 10, 3, 2, 1, true, actRelu},
		{11, 6, 4, 5, 5, 1, 2, true, actRelu6}, // dilated, an odd Co
		{8, 8, 3, 9, 3, 3, 1, false, actNone},  // VALID stride 3
		{1, 1, 96, 24, 1, 1, 1, false, actNone},
	} {
		x := randVec(r, c.H*c.W*c.Ci)
		w := randVec(r, c.Co*c.K*c.K*c.Ci)
		b := randVec(r, c.Co)
		want, OH, OW := naiveConv(x, c.H, c.W, c.Ci, w, c.Co, c.K, c.K, b, c.s, c.d, c.same, false)
		activate(want, c.act)
		out := make([]float32, OH*OW*c.Co)
		run, err := it.conv2D(convParams{same: c.same, sw: c.s, sh: c.s, dw: c.d, dh: c.d, act: c.act},
			x, []int{1, c.H, c.W, c.Ci}, w, []int{c.Co, c.K, c.K, c.Ci}, b, out, []int{1, OH, OW, c.Co}, testFail(t))
		if err != nil {
			t.Fatal(err)
		}
		run()
		closeTo(t, "conv", out, want, 1e-4)
	}
}

func TestDepthwiseMatchesNaive(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	it := &Interpreter{pool: newWorkers(3)}
	for _, c := range []struct {
		H, W, C, K, s int
	}{{72, 128, 16, 3, 2}, {18, 32, 88, 3, 1}, {9, 16, 96, 5, 1}, {18, 32, 96, 5, 2}, {5, 7, 3, 3, 1}} {
		x := randVec(r, c.H*c.W*c.C)
		w := randVec(r, c.K*c.K*c.C)
		b := randVec(r, c.C)
		want, OH, OW := naiveConv(x, c.H, c.W, c.C, w, c.C, c.K, c.K, b, c.s, 1, true, true)
		out := make([]float32, OH*OW*c.C)
		run, err := it.depthwise(convParams{same: true, sw: c.s, sh: c.s, dw: 1, dh: 1},
			x, []int{1, c.H, c.W, c.C}, w, []int{1, c.K, c.K, c.C}, b, out, []int{1, OH, OW, c.C}, testFail(t))
		if err != nil {
			t.Fatal(err)
		}
		run()
		closeTo(t, "depthwise", out, want, 1e-4)
	}
}

// TestResizeBilinear checks TFLite's reference semantics, including the
// extrapolating weights at the top/left edge with half-pixel centres.
func TestResizeBilinear(t *testing.T) {
	x := []float32{0, 1, 2, 3} // 2×2, one channel
	out := make([]float32, 16)
	resizeBilinear(x, []int{1, 2, 2, 1}, out, []int{1, 4, 4, 1}, false, true, newWorkers(1))()
	// input coordinate (i+0.5)/2-0.5: -0.25, 0.25, 0.75, 1.25; lower bound
	// clamped to 0, upper to 1, weight t - lower
	coord := []float32{-0.25, 0.25, 0.75, 1.25}
	for oy := 0; oy < 4; oy++ {
		for ox := 0; ox < 4; ox++ {
			fy, fx := coord[oy], coord[ox]
			y0, x0 := max(0, int(math.Floor(float64(fy)))), max(0, int(math.Floor(float64(fx))))
			y1, x1 := min(1, int(math.Ceil(float64(fy)))), min(1, int(math.Ceil(float64(fx))))
			dy, dx := fy-float32(y0), fx-float32(x0)
			v := func(y, x int) float32 { return x0f(x, y) }
			want := v(y0, x0)*(1-dy)*(1-dx) + v(y1, x0)*dy*(1-dx) + v(y0, x1)*(1-dy)*dx + v(y1, x1)*dy*dx
			if d := out[oy*4+ox] - want; d > 1e-6 || d < -1e-6 {
				t.Fatalf("(%d,%d) = %v, want %v", oy, ox, out[oy*4+ox], want)
			}
		}
	}
	// align_corners: the corners land on the corners
	out2 := make([]float32, 9)
	resizeBilinear(x, []int{1, 2, 2, 1}, out2, []int{1, 3, 3, 1}, true, false, newWorkers(1))()
	closeTo(t, "align_corners", out2, []float32{0, 0.5, 1, 1, 1.5, 2, 2, 2.5, 3}, 1e-6)
}

func x0f(x, y int) float32 { return float32(y*2 + x) }

func TestTransposeConvBias(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	it := &Interpreter{pool: newWorkers(1)}
	H, W, Ci, Co, K, S := 5, 7, 4, 2, 3, 2
	x := randVec(r, H*W*Ci)
	w := randVec(r, Co*K*K*Ci)
	b := randVec(r, Co)
	OH, OW := H*S, W*S // SAME
	out := make([]float32, OH*OW*Co)
	op := Operator{CustomOptions: []byte{1, 0, 0, 0, 2, 0, 0, 0, 2, 0, 0, 0}}
	run, err := it.transposeConvBias(op, x, []int{1, H, W, Ci}, w, []int{Co, K, K, Ci}, b, out, []int{1, OH, OW, Co}, testFail(t))
	if err != nil {
		t.Fatal(err)
	}
	run()
	// scatter form: every input pixel adds its filter-weighted patch
	pad := max((H-1)*S+K-OH, 0) / 2 // ComputePaddingHeightWidth on the output
	padW := max((W-1)*S+K-OW, 0) / 2
	want := make([]float64, len(out))
	for i := range want {
		want[i] = float64(b[i%Co])
	}
	for iy := 0; iy < H; iy++ {
		for ix := 0; ix < W; ix++ {
			for ky := 0; ky < K; ky++ {
				for kx := 0; kx < K; kx++ {
					oy, ox := iy*S-pad+ky, ix*S-padW+kx
					if oy < 0 || oy >= OH || ox < 0 || ox >= OW {
						continue
					}
					for o := 0; o < Co; o++ {
						for c := 0; c < Ci; c++ {
							want[(oy*OW+ox)*Co+o] += float64(x[(iy*W+ix)*Ci+c]) * float64(w[((o*K+ky)*K+kx)*Ci+c])
						}
					}
				}
			}
		}
	}
	w32 := make([]float32, len(want))
	for i, v := range want {
		w32[i] = float32(v)
	}
	closeTo(t, "transpose conv", out, w32, 1e-5)
	if _, err := it.transposeConvBias(Operator{CustomOptions: []byte{9}}, x, []int{1, H, W, Ci}, w, []int{Co, K, K, Ci}, b, out, []int{1, OH, OW, Co},
		func(string, ...any) error { return errUnsupported }); err == nil {
		t.Fatal("short custom options accepted")
	}
}

func TestBroadcast(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	a := randVec(r, 2*3*4)
	v := randVec(r, 4)
	col := randVec(r, 3)
	cases := []struct {
		mul    bool
		as, bs []int
		b      []float32
		want   func(y, x, c int) float32
	}{
		{true, []int{1, 2, 3, 4}, []int{1, 1, 1, 4}, v, func(y, x, c int) float32 { return a[(y*3+x)*4+c] * v[c] }},
		{false, []int{1, 2, 3, 4}, []int{4}, v, func(y, x, c int) float32 { return a[(y*3+x)*4+c] + v[c] }},
		{true, []int{1, 2, 3, 4}, []int{1, 1, 3, 1}, col, func(y, x, c int) float32 { return a[(y*3+x)*4+c] * col[x] }},
		{false, []int{1, 2, 3, 4}, []int{1, 2, 3, 4}, a, func(y, x, c int) float32 { return 2 * a[(y*3+x)*4+c] }},
	}
	for k, c := range cases {
		out := make([]float32, 24)
		run, err := binary4(c.mul, actNone, a, c.as, c.b, c.bs, out, []int{1, 2, 3, 4}, testFail(t))
		if err != nil {
			t.Fatal(err)
		}
		run()
		want := make([]float32, 24)
		for y := 0; y < 2; y++ {
			for x := 0; x < 3; x++ {
				for ch := 0; ch < 4; ch++ {
					want[(y*3+x)*4+ch] = c.want(y, x, ch)
				}
			}
		}
		closeTo(t, "broadcast "+string(rune('0'+k)), out, want, 1e-6)
		// and with the operands swapped
		run, err = binary4(c.mul, actRelu, c.b, c.bs, a, c.as, out, []int{1, 2, 3, 4}, testFail(t))
		if err != nil {
			t.Fatal(err)
		}
		run()
		activate(want, actRelu)
		closeTo(t, "broadcast swapped "+string(rune('0'+k)), out, want, 1e-6)
	}
	if _, err := binary4(true, 0, a, []int{1, 2, 3, 4}, v, []int{1, 1, 1, 3}, make([]float32, 24), []int{1, 2, 3, 4},
		func(string, ...any) error { return errUnsupported }); err == nil {
		t.Fatal("bad broadcast accepted")
	}
}

func TestFloat16(t *testing.T) {
	for h, want := range map[uint16]float32{
		0x3c00: 1, 0xc000: -2, 0x3800: 0.5, 0x7bff: 65504, 0x0001: 5.9604645e-8, 0x03ff: 6.097555e-5,
		0x0000: 0, 0x3555: 0.33325195,
	} {
		if got := f16to32(h); got != want {
			t.Errorf("f16 %#04x = %v, want %v", h, got, want)
		}
	}
	if !math.IsInf(float64(f16to32(0x7c00)), 1) || !math.IsNaN(float64(f16to32(0x7e00))) {
		t.Error("inf/nan")
	}
}

func TestGemvTail(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	for _, co := range []int{1, 7, 8, 13, 24} {
		a := randVec(r, 11)
		w := randVec(r, 11*co)
		b := randVec(r, co)
		got := make([]float32, co)
		gemv(got, a, w, b)
		want := make([]float32, co)
		for o := range want {
			s := float64(b[o])
			for i := range a {
				s += float64(a[i]) * float64(w[i*co+o])
			}
			want[o] = float32(s)
		}
		closeTo(t, "gemv", got, want, 1e-5)
	}
}
