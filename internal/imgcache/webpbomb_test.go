package imgcache

// Tests for the lossless (VP8L) meta-prefix memory bound (webpvp8l.go).
// x/image sizes its Huffman-group array make([]hGroup, maxIndex+1) by the
// group index the meta image names — up to 65535, ~175 MB — regardless of
// the picture's 1×1 canvas, and repeats it per still, per animation frame
// and per compression-1 ALPH alpha plane. Each test measures allocation
// across a real decode entry (Decode, loadImage, the frame compose) and
// fails if the gate is removed: without it, the allocation jumps to
// hundreds of MB. The inputs are synthetic (webptest), tiny, and decode to
// nothing — they end right after the meta image.

import (
	"bytes"
	"image/color"
	"os"
	"testing"

	"golang.org/x/image/webp"

	"github.com/some-du6e/clack/internal/imgcache/webptest"
)

// TestWebPMetaBombDecode covers Decode, the shared safe decoder that Copy
// Image, custom-emoji add, profile photos and the cache all route through.
func TestWebPMetaBombDecode(t *testing.T) {
	bomb := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LMeta(65535, color.NRGBA{}, false)))
	var err error
	n := allocBytes(func() { _, err = Decode(bomb) })
	if n > 16<<20 {
		t.Errorf("Decode allocated %d bytes for a 1×1 VP8L meta bomb (x/image would allocate ~175 MB)", n)
	}
	if err == nil {
		t.Error("a 1×1 VP8L meta bomb decoded instead of being rejected")
	}
}

// TestWebPMetaBombAnimationFrame covers loadImage and the animation frame
// decode (drawWebP): the bomb is the first frame of an animated WebP.
func TestWebPMetaBombAnimationFrame(t *testing.T) {
	frame := webptest.Frame{X: 0, Y: 0, W: 1, H: 1, DurMS: 50,
		Chunks: [][]byte{webptest.Chunk("VP8L", webptest.VP8LMeta(65535, color.NRGBA{}, false))}}
	bomb := webptest.Anim(1, 1, 0, frame)
	n, err := loadBytesAlloc(bomb)
	if n > 16<<20 {
		t.Errorf("loadImage allocated %d bytes decoding an animated VP8L meta bomb frame", n)
	}
	if err == nil {
		t.Error("an animated VP8L meta bomb frame decoded instead of being rejected")
	}
}

// TestWebPMetaBombAlpha covers the compression-1 ALPH path: x/image decodes
// a lossy picture's alpha plane as a VP8L image (readAlpha), so a meta bomb
// there allocates the same ~175 MB.
func TestWebPMetaBombAlpha(t *testing.T) {
	vp8 := webptest.Chunk("VP8 ", webptest.VP8Header(2, 2, 16))
	// ALPH body: a filter/compression byte (compression 1 = VP8L), then the
	// headerless VP8L meta bomb for the 2×2 plane.
	alph := webptest.Chunk("ALPH", append([]byte{0x01}, webptest.VP8LMetaBody(65535)...))
	bomb := webptest.Still(2, 2, 0x10, alph, vp8) // VP8X alpha flag
	var err error
	n := allocBytes(func() { _, err = Decode(bomb) })
	if n > 16<<20 {
		t.Errorf("Decode allocated %d bytes for a compression-1 ALPH meta bomb", n)
	}
	if err == nil {
		t.Error("a compression-1 ALPH meta bomb decoded instead of being rejected")
	}
}

// TestWebPMetaValidDecodes confirms the gate accepts a legitimate meta
// image (one tile naming group 0) and x/image decodes it unchanged.
func TestWebPMetaValidDecodes(t *testing.T) {
	c := color.NRGBA{10, 20, 30, 255}
	valid := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LMeta(0, c, true)))
	m, err := Decode(valid)
	if err != nil {
		t.Fatalf("a valid 1×1 VP8L meta image: %v", err)
	}
	if b := m.Bounds(); b.Dx() != 1 || b.Dy() != 1 {
		t.Fatalf("decoded %v, want 1×1", b)
	}
	r, g, bl, a := m.At(0, 0).RGBA()
	if r>>8 != uint32(c.R) || g>>8 != uint32(c.G) || bl>>8 != uint32(c.B) || a>>8 != uint32(c.A) {
		t.Errorf("pixel (%d,%d,%d,%d), want %v", r>>8, g>>8, bl>>8, a>>8, c)
	}
}

// TestWebPMetaGroupBound pins the bound at the canvas's tile count: for one
// tile, group 0 is the only in-bounds index, and group 1 (which x/image
// would allocate two groups for) is rejected. Removing the maxIdx ≥ tiles
// check makes the second case pass.
func TestWebPMetaGroupBound(t *testing.T) {
	if err := validateVP8L(webptest.VP8LMeta(0, color.NRGBA{1, 2, 3, 255}, true), 1, 1); err != nil {
		t.Errorf("group 0 of a one-tile picture was rejected: %v", err)
	}
	if err := validateVP8L(webptest.VP8LMeta(1, color.NRGBA{}, false), 1, 1); err == nil {
		t.Error("group 1 of a one-tile picture was accepted (x/image would allocate 2 groups)")
	}
}

// TestWebPLosslessFixturesDecode proves the validator does not reject real
// libwebp lossless output: these fixtures use the predictor, subtract-green
// and colour-indexing transforms and multi-symbol prefix codes that
// webptest.VP8LSolid never emits, so they exercise the validator's full
// decodePix. Each must decode through Decode (the validator, then the
// rewrap) exactly as x/image decodes it directly.
func TestWebPLosslessFixturesDecode(t *testing.T) {
	for _, name := range []string{"lossless-alpha.webp", "lossless-indexed.webp"} {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: x/image: %v", name, err)
		}
		got, err := Decode(data)
		if err != nil {
			t.Fatalf("%s: imgcache.Decode: %v (the VP8L validator must not reject real libwebp output)", name, err)
		}
		if got.Bounds() != ref.Bounds() {
			t.Fatalf("%s: bounds %v, want %v", name, got.Bounds(), ref.Bounds())
		}
		b := ref.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				gr, gg, gb, ga := got.At(x, y).RGBA()
				rr, rg, rbl, ra := ref.At(x, y).RGBA()
				if gr != rr || gg != rg || gb != rbl || ga != ra {
					t.Fatalf("%s at (%d,%d): got %v, want %v", name, x, y,
						[4]uint32{gr, gg, gb, ga}, [4]uint32{rr, rg, rbl, ra})
				}
			}
		}
	}
}
