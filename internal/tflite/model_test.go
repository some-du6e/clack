package tflite_test

import (
	"encoding/binary"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle/bgblur"
	"github.com/some-du6e/clack/internal/tflite"
)

func load(t testing.TB) *tflite.Model {
	m, err := tflite.Parse(bgblur.ModelTFLite)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestModelOps is the selfie segmenter's operator inventory: what the
// interpreter implements, no more.
func TestModelOps(t *testing.T) {
	m := load(t)
	got := map[string]int{}
	for _, op := range m.Ops {
		name := op.Code.String()
		if op.Code == tflite.OpCustom {
			name += " " + op.Custom
		}
		got[name]++
	}
	want := map[string]int{
		"ADD": 14, "CONV_2D": 43, "CUSTOM Convolution2DTransposeBias": 1, "DEPTHWISE_CONV_2D": 11,
		"DEQUANTIZE": 110, "HARD_SWISH": 11, "LOGISTIC": 11, "MEAN": 10, "MUL": 10, "RELU": 22, "RESIZE_BILINEAR": 3,
	}
	if len(got) != len(want) {
		t.Errorf("ops %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s ×%d, want ×%d", k, got[k], v)
		}
	}
	in, out := m.Tensors[m.Inputs[0]], m.Tensors[m.Outputs[0]]
	if !slices.Equal(in.Shape, []int{1, 144, 256, 3}) || !slices.Equal(out.Shape, []int{1, 144, 256, 1}) {
		t.Errorf("input %v output %v", in.Shape, out.Shape)
	}
	if _, ok := m.Metadata["TFLITE_METADATA"]; !ok {
		t.Error("no TFLITE_METADATA")
	}
}

func TestParseRejects(t *testing.T) {
	if _, err := tflite.Parse([]byte("nope")); err == nil {
		t.Error("garbage parsed")
	}
	b := slices.Clone(bgblur.ModelTFLite)
	binary.LittleEndian.PutUint32(b, uint32(len(b)+10)) // root offset past the end
	if _, err := tflite.Parse(b); err == nil {
		t.Error("bad root offset parsed")
	}
	if _, err := tflite.Parse(bgblur.ModelTFLite[:len(bgblur.ModelTFLite)/3]); err == nil {
		t.Error("truncated model parsed")
	}
}

func readPNG(t *testing.T, p string) *image.RGBA {
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}

func readF32(t *testing.T, p string) []float32 {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

// TestMediaPipeReference runs the model on the fixed inputs and compares
// with what MediaPipe's runtime (TFLite + XNNPack + its custom op; the
// bgblur workflow's "reference" job, testdata/mediapipe_ref.py) produced.
func TestMediaPipeReference(t *testing.T) {
	m := load(t)
	for _, threads := range []int{1, 4} {
		it, err := tflite.NewInterpreter(m, tflite.Options{Threads: threads})
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"ref-person", "ref-noise"} {
			img := readPNG(t, filepath.Join("testdata", name+".png"))
			want := readF32(t, filepath.Join("testdata", name+".mediapipe.f32"))
			in := it.Input(0)
			for y := 0; y < 144; y++ {
				for x := 0; x < 256; x++ {
					p := img.Pix[y*img.Stride+4*x:]
					for c := 0; c < 3; c++ {
						in[(y*256+x)*3+c] = float32(p[c]) / 255
					}
				}
			}
			it.Invoke()
			got := it.Output(0)
			if len(got) != len(want) {
				t.Fatalf("%s: %d values, reference has %d", name, len(got), len(want))
			}
			var maxd, sum float64
			worst := 0
			for i := range got {
				d := math.Abs(float64(got[i] - want[i]))
				sum += d
				if d > maxd {
					maxd, worst = d, i
				}
			}
			t.Logf("%s (%d threads): max |Δ| %.2e at %d (%v vs %v), mean |Δ| %.2e", name, threads, maxd, worst, got[worst], want[worst], sum/float64(len(got)))
			if maxd > 1e-4 {
				t.Errorf("%s: max |Δ| %.2e over 1e-4", name, maxd)
			}
		}
		it.Close()
	}
}

// TestKeepAllMatches: buffer reuse doesn't change the result.
func TestKeepAllMatches(t *testing.T) {
	m := load(t)
	a, _ := tflite.NewInterpreter(m, tflite.Options{Threads: 2})
	b, _ := tflite.NewInterpreter(m, tflite.Options{Threads: 1, KeepAll: true})
	for i := range a.Input(0) {
		v := float32(i%251) / 251
		a.Input(0)[i], b.Input(0)[i] = v, v
	}
	a.Invoke()
	b.Invoke()
	if !slices.Equal(a.Output(0), b.Output(0)) {
		t.Fatal("reuse changed the output")
	}
}

func timeInvokes(it *tflite.Interpreter, n int) []time.Duration {
	for i := 0; i < 5; i++ {
		it.Invoke()
	}
	d := make([]time.Duration, n)
	for i := range d {
		t0 := time.Now()
		it.Invoke()
		d[i] = time.Since(t0)
	}
	sort.Slice(d, func(a, b int) bool { return d[a] < d[b] })
	return d
}

// TestInvokeSpeed asserts the interpreter's budget, ~20 ms a frame, on
// the CI runner (CLACK_PERF_ASSERT=1; the bgblur workflow sets it). Shared
// runners have noisy neighbours (a p90 of 100 ms beside a 12 ms minimum
// on macOS), so the assertion takes the best of three 20-run batch
// medians, with the default threads or one, whichever is faster there.
func TestInvokeSpeed(t *testing.T) {
	if os.Getenv("CLACK_PERF_ASSERT") == "" {
		t.Skip("CLACK_PERF_ASSERT not set")
	}
	m := load(t)
	best := time.Hour
	for _, threads := range []int{0, 1} {
		it, err := tflite.NewInterpreter(m, tflite.Options{Threads: threads})
		if err != nil {
			t.Fatal(err)
		}
		for b := 0; b < 3; b++ {
			d := timeInvokes(it, 20)
			med := d[len(d)/2]
			t.Logf("%s/%s GOMAXPROCS %d, threads %d, batch %d: median %v, p90 %v, min %v", runtime.GOOS, runtime.GOARCH,
				runtime.GOMAXPROCS(0), threads, b, med, d[len(d)*9/10], d[0])
			best = min(best, med)
		}
		it.Close()
	}
	t.Logf("best batch median %v", best)
	if best > 20*time.Millisecond {
		t.Errorf("best batch median %v over the 20 ms budget", best)
	}
}

func BenchmarkInvoke(b *testing.B) {
	it, err := tflite.NewInterpreter(load(b), tflite.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		it.Invoke()
	}
}

func BenchmarkInvoke1Thread(b *testing.B) {
	it, err := tflite.NewInterpreter(load(b), tflite.Options{Threads: 1})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		it.Invoke()
	}
}
