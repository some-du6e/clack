package bgblur

import (
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

func cloneFrame(f *Frame) *Frame {
	g := *f
	g.Y = append([]byte(nil), f.Y...)
	g.UV = append([]byte(nil), f.UV...)
	return &g
}

// iou compares the mask (background < 0.5 is the person) with the drawn
// silhouette, sampled at the mask's texel centres.
func iou(e *Effect, person []bool, W, H int) float64 {
	w, h, m := e.Mask()
	var in, un int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx, sy := (2*x+1)*W/(2*w), (2*y+1)*H/(2*h)
			p, g := m[y*w+x] < 0.5, person[sy*W+sx]
			if p && g {
				in++
			}
			if p || g {
				un++
			}
		}
	}
	return float64(in) / float64(un)
}

// TestMaskSyntheticPerson: the segmenter plus Slack's bilateral pass find
// the drawn person at the camera sizes Clack sees.
func TestMaskSyntheticPerson(t *testing.T) {
	e, err := NewEffect()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for _, sz := range [][2]int{{1280, 720}, {640, 480}, {320, 180}} {
		img, person := SyntheticPerson(sz[0], sz[1])
		f := FrameFromRGBA(img)
		e.Reset()
		e.Apply(cloneFrame(f))
		if !e.Segmented {
			t.Fatal("first frame after Reset not segmented")
		}
		got := iou(e, person, sz[0], sz[1])
		t.Logf("%dx%d: IoU %.3f, grid %d px, %v", sz[0], sz[1], got, e.g, e.Took)
		if got < 0.93 {
			t.Errorf("%dx%d: IoU %.3f under 0.93", sz[0], sz[1], got)
		}
	}
}

func gradEnergy(p []byte, stride, x0, y0, x1, y1 int) float64 {
	var s float64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1-1; x++ {
			d := float64(p[y*stride+x+1]) - float64(p[y*stride+x])
			s += d * d
		}
	}
	return s / float64((y1-y0)*(x1-x0-1))
}

// TestBlurKeepsPerson: pixels the mask calls foreground (Slack's c < 0.001)
// keep the camera's bytes; the bookshelf behind loses its edges.
func TestBlurKeepsPerson(t *testing.T) {
	const W, H = 1280, 720
	img, _ := SyntheticPerson(W, H)
	src := FrameFromRGBA(img)
	e, err := NewEffect()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f := cloneFrame(src)
	e.Apply(f)
	mw, mh, m := e.Mask()
	kept, changed := 0, 0
	for y := 0; y < H; y++ {
		// composite's order: the mask row first, then along it
		ty := texTap((float32(y)+0.5)/H, mh)
		row := make([]float32, mw)
		for u := range row {
			r0, r1 := m[int(ty.i0)*mw+u], m[int(ty.i1)*mw+u]
			row[u] = r0 + (r1-r0)*ty.f
		}
		for x := 0; x < W; x++ {
			tx := texTap((float32(x)+0.5)/W, mw)
			c := smoothstep(0.4, 0.6, row[tx.i0]+(row[tx.i1]-row[tx.i0])*tx.f)
			if c < 0.001 {
				if f.Y[y*W+x] != src.Y[y*W+x] {
					t.Fatalf("foreground pixel (%d,%d) changed %d → %d", x, y, src.Y[y*W+x], f.Y[y*W+x])
				}
				kept++
			} else if f.Y[y*W+x] != src.Y[y*W+x] {
				changed++
			}
		}
	}
	before := gradEnergy(src.Y, W, 960, 100, 1230, 700) // the shelf
	after := gradEnergy(f.Y, W, 960, 100, 1230, 700)
	t.Logf("foreground %d px kept, %d background px changed; shelf gradient energy %.2f → %.2f", kept, changed, before, after)
	if kept < W*H/6 || changed < W*H/5 {
		t.Errorf("kept %d, changed %d", kept, changed)
	}
	if after > before/5 {
		t.Errorf("shelf gradient energy %.2f → %.2f: not blurred", before, after)
	}
	if dir := os.Getenv("CLACK_SHOTS"); dir != "" {
		for name, fr := range map[string]*Frame{"bgblur-source.png": src, "bgblur-blurred.png": f} {
			out, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			png.Encode(out, fr.RGBA())
			out.Close()
		}
	}
}

// TestBlurWeights: Slack's 13 constants are a σ 12 Gaussian normalized over
// its 25 taps.
func TestBlurWeights(t *testing.T) {
	var z float64
	for k := -blurRadius; k <= blurRadius; k++ {
		z += math.Exp(-float64(k*k) / (2 * 144))
	}
	var sum float32
	for k, w := range blurWeights {
		want := math.Exp(-float64(k*k)/(2*144)) / z
		if math.Abs(float64(w)-want) > 2e-7 {
			t.Errorf("W%d = %v, Gaussian %v", k, w, want)
		}
		sum += w
		if k > 0 {
			sum += w
		}
	}
	if math.Abs(float64(sum)-1) > 1e-5 {
		t.Errorf("weights sum to %v", sum)
	}
	if d := rangeWeight(3) - float32(math.Exp(-3.0/32)); d > 1e-7 || d < -1e-7 {
		t.Errorf("rangeWeight(3) off by %v", d)
	}
}

// TestCadence is MaskCadenceController's schedule.
func TestCadence(t *testing.T) {
	c := newCadence(3)
	if c.decimation() != 2 {
		t.Fatalf("starts at decimation %d, want 2", c.decimation())
	}
	var seg []bool
	have := false
	for i := 0; i < 6; i++ {
		s := c.shouldSegment(have)
		have = true
		seg = append(seg, s)
	}
	if want := []bool{true, false, true, false, true, false}; !equalBools(seg, want) {
		t.Errorf("schedule %v, want %v", seg, want)
	}
	// 60% busy: two hot windows (4 s) climb a rung, no sooner than 3 s
	now := 1.0
	for now < 4100 {
		c.accumulateUtilization(40, now)
		now += 66.7
	}
	if c.decimation() != 3 {
		t.Errorf("after 4 s at 60%% decimation %d, want 3", c.decimation())
	}
	// 10% busy: three cool windows (6 s) step back down
	for end := now + 6100; now < end; now += 66.7 {
		c.accumulateUtilization(6.7, now)
	}
	if c.decimation() != 2 {
		t.Errorf("after 6 s at 10%% decimation %d, want 2", c.decimation())
	}
	if newCadence(0).decimation() != 1 {
		t.Error("maxRung 0 must segment every frame")
	}
}

func equalBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestFillMaskSnaps: confident texels snap to 0 and 1 like the shader's
// early exits, and an uncertain edge comes out on Slack's 8-bit steps.
func TestFillMaskSnaps(t *testing.T) {
	e, err := NewEffect()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	f := FrameFromRGBA(mustPerson(320, 180))
	e.buildGrid(f)
	e.raw = make([]float32, e.mw*e.mh)
	for y := 0; y < e.mh; y++ {
		for x := 0; x < e.mw; x++ {
			e.raw[y*e.mw+x] = float32(x) / float32(e.mw-1) // a ramp left to right
		}
	}
	e.fillMask(f.W, f.H)
	if e.mask[0] != 0 || e.mask[e.mw-1] != 1 {
		t.Errorf("ends %v %v, want 0 1", e.mask[0], e.mask[e.mw-1])
	}
	for _, v := range e.mask {
		if q := float32(math.Round(float64(v)*255)) / 255; q != v {
			t.Fatalf("%v isn't an 8-bit value", v)
		}
	}
	mid := e.mask[e.mh/2*e.mw+e.mw/2]
	if mid < 0.3 || mid > 0.7 {
		t.Errorf("middle %v", mid)
	}
}

func mustPerson(w, h int) *image.RGBA {
	img, _ := SyntheticPerson(w, h)
	return img
}

// TestOddSizes: frames that don't divide by the grid, and tiny ones.
func TestOddSizes(t *testing.T) {
	e, err := NewEffect()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for _, sz := range [][2]int{{322, 182}, {16, 16}, {962, 542}, {1920, 1080}} {
		f := FrameFromRGBA(mustPerson(sz[0], sz[1]))
		e.Apply(f)
	}
}

func timeApply(t testing.TB, W, H, n int) (seg, plain []time.Duration) {
	img, _ := SyntheticPerson(W, H)
	src := FrameFromRGBA(img)
	e, err := NewEffect()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	at := time.Now()
	for i := 0; i < n; i++ {
		f := cloneFrame(src)
		f.At = at.Add(time.Duration(i) * time.Second / 15)
		e.Apply(f)
		if i < 4 {
			continue
		}
		if e.Segmented {
			seg = append(seg, e.Took)
		} else {
			plain = append(plain, e.Took)
		}
	}
	sort.Slice(seg, func(a, b int) bool { return seg[a] < seg[b] })
	sort.Slice(plain, func(a, b int) bool { return plain[a] < plain[b] })
	return
}

// TestApplySpeed logs the whole effect's cost per 720p frame on the runner
// and asserts it fits a 15 fps camera (CLACK_PERF_ASSERT=1).
func TestApplySpeed(t *testing.T) {
	if os.Getenv("CLACK_PERF_ASSERT") == "" {
		t.Skip("CLACK_PERF_ASSERT not set")
	}
	seg, plain := timeApply(t, 1280, 720, 40)
	if len(seg) == 0 || len(plain) == 0 {
		t.Fatalf("seg %d plain %d", len(seg), len(plain))
	}
	ms, mp := seg[len(seg)/2], plain[len(plain)/2]
	t.Logf("%s/%s GOMAXPROCS %d, 1280x720: segmented frames median %v, others median %v", runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0), ms, mp)
	if (ms+mp)/2 > time.Second/15 {
		t.Errorf("average %v a frame doesn't keep up with 15 fps", (ms+mp)/2)
	}
}

func BenchmarkApply720p(b *testing.B) {
	img, _ := SyntheticPerson(1280, 720)
	src := FrameFromRGBA(img)
	e, err := NewEffect()
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	f := cloneFrame(src)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(f.Y, src.Y)
		copy(f.UV, src.UV)
		e.Apply(f)
	}
}
