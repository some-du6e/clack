package catmullrom

// Scale must draw exactly what golang.org/x/image/draw's
// CatmullRom.Scale(dst, dst.Bounds(), src, sr, draw.Src, nil) draws, every
// pixel bit for bit — it is the same code, only scaling a band of
// destination rows at a time instead of the whole image at once. These
// tests decode nothing; they compare the two outputs byte for byte over a
// range of sizes, source types and source rectangles (up, down, and
// equal), so a banding bug shows as a pixel difference.

import (
	"image"
	"math/rand/v2"
	"testing"

	xdraw "golang.org/x/image/draw"

	"github.com/some-du6e/clack/internal/imgcache/internal/budget"
)

func fill(pix []byte, seed uint64) {
	r := rand.New(rand.NewPCG(seed, 0x9e3779b9))
	for i := range pix {
		pix[i] = byte(r.Uint32())
	}
}

func sources(w, h int) map[string]image.Image {
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	fill(rgba.Pix, 1)
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	fill(nrgba.Pix, 2)
	gray := image.NewGray(image.Rect(0, 0, w, h))
	fill(gray.Pix, 3)
	// a YCbCr 4:2:0 and a 4:4:4, the shapes a JPEG decodes to, with a
	// non-zero rectangle origin (as a SubImage has)
	y420 := image.NewYCbCr(image.Rect(3, 5, 3+w, 5+h), image.YCbCrSubsampleRatio420)
	fill(y420.Y, 4)
	fill(y420.Cb, 5)
	fill(y420.Cr, 6)
	y444 := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio444)
	fill(y444.Y, 7)
	fill(y444.Cb, 8)
	fill(y444.Cr, 9)
	return map[string]image.Image{"RGBA": rgba, "NRGBA": nrgba, "Gray": gray, "YCbCr420": y420, "YCbCr444": y444}
}

// TestScaleMatchesXImage: for several source types and destination sizes,
// Scale's output equals x/image/draw's for the whole source.
func TestScaleMatchesXImage(t *testing.T) {
	const sw, sh = 97, 61
	cases := [][2]int{{48, 30}, {33, 20}, {17, 11}, {97, 61}, {200, 120}, {150, 61}, {48, 120}, {1, 1}, {2, 3}}
	for name, src := range sources(sw, sh) {
		sr := src.Bounds()
		for _, c := range cases {
			dw, dh := c[0], c[1]
			want := image.NewRGBA(image.Rect(0, 0, dw, dh))
			xdraw.CatmullRom.Scale(want, want.Bounds(), src, sr, xdraw.Src, nil)
			got := image.NewRGBA(image.Rect(0, 0, dw, dh))
			if err := Scale(got, src, sr, budget.Unlimited()); err != nil {
				t.Fatalf("%s %dx%d→%dx%d: %v", name, sw, sh, dw, dh, err)
			}
			if !equalRGBA(got, want) {
				t.Errorf("%s %dx%d→%dx%d: banded Scale differs from x/image", name, sw, sh, dw, dh)
			}
		}
	}
}

// TestScaleSubRect: Scale of a sub-rectangle of the source (the composer's
// centre-crop thumbnail, and the emoji dialog's) matches x/image too.
func TestScaleSubRect(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 120, 90))
	fill(src.Pix, 11)
	for _, sr := range []image.Rectangle{
		image.Rect(20, 10, 100, 80), // an inset crop
		image.Rect(0, 0, 90, 90),    // a square at the origin
		image.Rect(30, 30, 90, 90),
	} {
		for _, d := range [][2]int{{62, 62}, {40, 50}, {128, 128}} {
			want := image.NewRGBA(image.Rect(0, 0, d[0], d[1]))
			xdraw.CatmullRom.Scale(want, want.Bounds(), src, sr, xdraw.Src, nil)
			got := image.NewRGBA(image.Rect(0, 0, d[0], d[1]))
			if err := Scale(got, src, sr, budget.Unlimited()); err != nil {
				t.Fatal(err)
			}
			if !equalRGBA(got, want) {
				t.Errorf("sub-rect %v → %dx%d differs from x/image", sr, d[0], d[1])
			}
		}
	}
}

// TestScaleChargesAndRefuses: Scale charges its weights and band to the
// budget, and a budget too small refuses it (having drawn nothing) rather
// than allocating the band.
func TestScaleChargesAndRefuses(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4000, 4000))
	dst := image.NewRGBA(image.Rect(0, 0, 2048, 2048))
	b := budget.NewLocal(1 << 10) // far too small for the distrib weights
	if err := Scale(dst, src, src.Bounds(), b); err == nil {
		t.Error("Scale under a 1 KB budget succeeded; it must charge its buffers")
	}
	// a generous budget draws it
	if err := Scale(dst, src, src.Bounds(), budget.NewLocal(64<<20)); err != nil {
		t.Errorf("Scale under a 64 MB budget: %v", err)
	}
}

func equalRGBA(a, b *image.RGBA) bool {
	if a.Rect != b.Rect {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}
