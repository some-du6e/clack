package inspect

// The fast blurs (blur.go) against the blur the tool used before them,
// kept here as blurRef: every pixel the same, bit for bit, whatever the
// size, the radius and the element kept sharp (a margin's blur, which
// computes only the margin), on both sides of the parallel threshold.

import (
	"image"
	"image/draw"
	"math"
	"math/rand/v2"
	"runtime"
	"testing"
)

// blurRef box-blurs img inside r three times, rad px each way, rows then
// columns (edges clamped: nothing outside r comes in).
func blurRef(img *image.RGBA, r image.Rectangle, rad int) {
	r = r.Intersect(img.Bounds())
	w, h := r.Dx(), r.Dy()
	if w == 0 || h == 0 || rad <= 0 {
		return
	}
	n := max(w, h)
	line := make([]uint8, 4*n)
	tmp := make([]uint8, 4*n)
	d := 2*rad + 1
	// pass blurs line's first l pixels once
	pass := func(l int) {
		for c := 0; c < 4; c++ {
			at := func(i int) int { return int(line[4*min(max(i, 0), l-1)+c]) }
			sum := 0
			for i := -rad; i <= rad; i++ {
				sum += at(i)
			}
			for i := 0; i < l; i++ {
				tmp[4*i+c] = uint8((sum + d/2) / d)
				sum += at(i+rad+1) - at(i-rad)
			}
		}
		copy(line[:4*l], tmp[:4*l])
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		o := img.PixOffset(r.Min.X, y)
		copy(line, img.Pix[o:o+4*w])
		for range 3 {
			pass(w)
		}
		copy(img.Pix[o:o+4*w], line[:4*w])
	}
	for x := r.Min.X; x < r.Max.X; x++ {
		for y := range h {
			o := img.PixOffset(x, r.Min.Y+y)
			copy(line[4*y:4*y+4], img.Pix[o:o+4])
		}
		for range 3 {
			pass(h)
		}
		for y := range h {
			o := img.PixOffset(x, r.Min.Y+y)
			copy(img.Pix[o:o+4], line[4*y:4*y+4])
		}
	}
}

// noiseImage is w x h px of noise (alpha too).
func noiseImage(w, h int, rng *rand.Rand) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.IntN(256))
	}
	return img
}

func cloneRGBA(img *image.RGBA) *image.RGBA {
	c := image.NewRGBA(img.Bounds())
	copy(c.Pix, img.Pix)
	return c
}

// paintShotRef is paintShot as it was: the whole shot blurred, the
// element drawn sharp again over it.
func paintShotRef(dst *image.RGBA, at image.Point, shot *image.RGBA, elem image.Rectangle, scale float32) {
	sb := shot.Bounds()
	r := image.Rectangle{Min: at, Max: at.Add(sb.Size())}
	draw.Draw(dst, r, shot, sb.Min, draw.Src)
	if sb.In(elem) {
		return
	}
	blurRef(dst, r, max(int(math.Round(float64(MarginBlur*scale))), 1))
	if c := elem.Intersect(sb); !c.Empty() {
		draw.Draw(dst, c.Sub(sb.Min).Add(at), shot, c.Min, draw.Src)
	}
}

func TestBlurMatchesOld(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range 400 {
		w, h := 1+rng.IntN(80), 1+rng.IntN(80)
		if i%8 == 0 { // past the parallel threshold (64K px)
			w, h = 260+rng.IntN(300), 260+rng.IntN(200)
		}
		rad := 1 + rng.IntN(24)
		img := noiseImage(w+8, h+8, rng)
		r := image.Rect(rng.IntN(5), rng.IntN(5), w+rng.IntN(5), h+rng.IntN(5)).Intersect(img.Bounds())
		want := cloneRGBA(img)
		blurRef(want, r, rad)
		got := cloneRGBA(img)
		blur(got, r, rad)
		if d := diffPx(got, want, img.Bounds(), image.Rectangle{}); d > 0 {
			t.Fatalf("case %d: %v in %v at rad %d: %d px differ from the old blur", i, r, img.Bounds(), rad, d)
		}
		// a margin's blur: keep somewhere inside r, at its edges, past
		// them, or all of it
		var keep image.Rectangle
		switch i % 5 {
		case 0:
			keep = image.Rect(r.Min.X+rng.IntN(max(r.Dx(), 1)), r.Min.Y+rng.IntN(max(r.Dy(), 1)), r.Max.X-rng.IntN(max(r.Dx()/2, 1)), r.Max.Y-rng.IntN(max(r.Dy()/2, 1)))
		case 1:
			keep = image.Rect(r.Min.X, r.Min.Y+rng.IntN(max(r.Dy(), 1)), r.Max.X, r.Max.Y)
		case 2:
			keep = image.Rect(r.Min.X-3, r.Min.Y-3, r.Min.X+rng.IntN(max(r.Dx(), 1)), r.Max.Y+3)
		case 3:
			keep = r
		case 4:
			k := min(r.Dx(), r.Dy()) / 3
			keep = r.Inset(k)
		}
		got = cloneRGBA(img)
		blurOutside(got, r, rad, keep)
		if d := diffPx(got, want, img.Bounds(), keep.Intersect(r)); d > 0 {
			t.Fatalf("case %d: %v in %v at rad %d keeping %v: %d px outside it differ from the old blur", i, r, img.Bounds(), rad, keep, d)
		}
	}
}

// diffPx counts the px of b where x and y differ, those in skip left out.
func diffPx(x, y *image.RGBA, b, skip image.Rectangle) int {
	n := 0
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			if image.Pt(px, py).In(skip) {
				continue
			}
			if x.RGBAAt(px, py) != y.RGBAAt(px, py) {
				n++
			}
		}
	}
	return n
}

// TestPaintShotMatchesOld: the review's and the copy's shot, its margin
// blurred and the element sharp, is the one the old whole-shot blur made,
// for shots as big as Karim's window gives, at every density.
func TestPaintShotMatchesOld(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for i, c := range []struct {
		w, h  int
		elem  image.Rectangle
		scale float32
	}{
		{364, 288, image.Rect(96, 96, 268, 192), 4},
		{3200, 1984, image.Rect(44, 0, 3200, 1984), 1.84},
		{1460, 352, image.Rect(53, 53, 1407, 299), 2.2},
		{700, 500, image.Rect(-20, 40, 650, 520), 3},
		{300, 200, image.Rect(400, 400, 500, 500), 4},
		{300, 200, image.Rect(0, 0, 300, 200), 4},
	} {
		shot := noiseImage(c.w, c.h, rng)
		at := image.Pt(rng.IntN(40), 0)
		want := image.NewRGBA(image.Rect(0, 0, c.w+80, c.h+10))
		got := image.NewRGBA(want.Bounds())
		paintShotRef(want, at, shot, c.elem, c.scale)
		paintShot(got, at, shot, c.elem, c.scale)
		if d := diffPx(got, want, want.Bounds(), image.Rectangle{}); d > 0 {
			t.Errorf("case %d (%dx%d, elem %v, %gx): %d px differ from the old whole-shot blur", i, c.w, c.h, c.elem, c.scale, d)
		}
	}
}

// TestBlurOutsideAllocs: blurring a big shot's margin again allocates
// little: each goroutine's lines are the ones its phase reads, and come
// back from a pool (review of a8fa243c: 34 lines of the longest side per
// goroutine and phase, ~7 MB of garbage per blur on the UI goroutine).
func TestBlurOutsideAllocs(t *testing.T) {
	prev := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(prev)
	img := noiseImage(3200, 2000, rand.New(rand.NewPCG(5, 6)))
	keep := image.Rect(60, 60, 3140, 1940)
	blurOutside(img, img.Bounds(), 8, keep) // the pool filled
	least := uint64(1 << 62)
	for range 3 {
		var a, b runtime.MemStats
		runtime.ReadMemStats(&a)
		blurOutside(img, img.Bounds(), 8, keep)
		runtime.ReadMemStats(&b)
		least = min(least, b.TotalAlloc-a.TotalAlloc)
	}
	if least > 512<<10 {
		t.Errorf("blurring a 3200x2000 shot's margin again allocated %d KB, want under 512 KB", least>>10)
	}
}
