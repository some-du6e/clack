package imgcache

// Tests for the lossless (VP8L) memory budget (webpanim.go, internal/webp,
// internal/vp8l). WebP bytes are anyone's (a custom emoji), and x/image's
// vp8l decoder sizes its Huffman-group array and each group's trees from
// fields in the bitstream before it reads the data they describe, so a few
// bytes could make it allocate gigabytes. The vendored decoder charges a
// byte budget, tied to the picture's canvas, before every allocation and
// fails before any that would exceed it.
//
// Each test here measures allocation (TotalAlloc) across a real decode
// entry — Decode, DecodeWithin, loadImage, and the animation-frame draw —
// and fails if its guard is removed: without the guard the allocation
// jumps to hundreds of MB. The inputs are synthetic (webptest) or libwebp
// fixtures in testdata; nothing comes from Slack.

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"os"
	"testing"

	"golang.org/x/image/webp"

	"github.com/some-du6e/clack/internal/imgcache/webptest"
)

// TestWebPMetaBombDecode: a 1×1 VP8L whose one meta tile names Huffman
// group 65535. x/image builds make([]hGroup, 65536) — ~168 MB — before
// reading a tree. The vendored decoder builds only the groups a tile
// actually names (here one), so it allocates almost nothing and fails
// reading the first group at EOF. Covers Decode, the shared safe decoder
// Copy Image, custom-emoji add, profile photos and the cache route through.
func TestWebPMetaBombDecode(t *testing.T) {
	bomb := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LMeta(65535, color.NRGBA{}, false)))
	var err error
	n := allocBytes(func() { _, err = Decode(bomb) })
	if n > 16<<20 {
		t.Errorf("Decode allocated %d bytes for a 1×1 VP8L meta bomb (x/image would allocate ~168 MB)", n)
	}
	if err == nil {
		t.Error("a 1×1 VP8L meta bomb decoded instead of being rejected")
	}
}

// TestWebPMetaBombAnimationFrame covers loadImage and the animation-frame
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
// there would allocate the same ~168 MB.
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

// TestWebPManyGroupsBudget maximises the group-count class: a 512×512 VP8L
// whose meta image names 16384 distinct Huffman groups. Every group is
// used, so building only the used ones doesn't help; the group array alone
// is 16384·2680 ≈ 42 MB, over webpBudget(512,512) ≈ 17.5 MB. The budget
// refuses it (ErrTooBig) having allocated well under 16 MB. Remove the
// group-array charge and the decoder allocates the whole 42 MB.
func TestWebPManyGroupsBudget(t *testing.T) {
	bomb := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LManyGroups(512, 512, 2, 16384)))
	var err error
	n := allocBytes(func() { _, err = Decode(bomb) })
	if n > 16<<20 {
		t.Errorf("Decode allocated %d bytes for a 16384-group 512×512 VP8L (the group array is ~42 MB)", n)
	}
	if !errors.Is(err, ErrTooBig) {
		t.Errorf("a 16384-group VP8L: %v, want ErrTooBig", err)
	}
	// DecodeWithin's larger budget scales with the canvas, so it refuses the
	// same bomb too (it is 512×512, far under attMaxPixels).
	n = allocBytes(func() { _, err = DecodeWithin(bomb, 80<<20, MaxSide) })
	if n > 16<<20 {
		t.Errorf("DecodeWithin allocated %d bytes for the 16384-group bomb", n)
	}
	if !errors.Is(err, ErrTooBig) {
		t.Errorf("DecodeWithin on the 16384-group VP8L: %v, want ErrTooBig", err)
	}
}

// TestWebPManyGroupsFrameBudget runs the many-groups bomb through the
// animation-frame path (loadImage → drawWebP).
func TestWebPManyGroupsFrameBudget(t *testing.T) {
	frame := webptest.Frame{X: 0, Y: 0, W: 512, H: 512, DurMS: 50,
		Chunks: [][]byte{webptest.Chunk("VP8L", webptest.VP8LManyGroups(512, 512, 2, 16384))}}
	bomb := webptest.Anim(512, 512, 0, frame)
	n, err := loadBytesAlloc(bomb)
	if n > 16<<20 {
		t.Errorf("loadImage allocated %d bytes decoding a 16384-group animation frame", n)
	}
	if err == nil {
		t.Error("a 16384-group animation frame decoded instead of being rejected")
	}
}

// TestWebPMetaValidDecodes confirms a legitimate meta image (one tile
// naming group 0) decodes unchanged.
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

// TestWebPLiteralDecodes confirms the vendored decoder decodes a
// multi-colour literal picture (webptest's flat byte codes, which the bomb
// builders rely on) to exactly its pixels.
func TestWebPLiteralDecodes(t *testing.T) {
	const w, h = 7, 5
	pix := make([]byte, 4*w*h)
	for i := range pix {
		pix[i] = byte(i*7 + 1)
	}
	data := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LLiteral(w, h, pix)))
	m, err := Decode(data)
	if err != nil {
		t.Fatalf("a %d×%d literal VP8L: %v", w, h, err)
	}
	nrgba, ok := m.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded %T, want *image.NRGBA", m)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := 4 * (y*w + x)
			want := color.NRGBA{pix[i], pix[i+1], pix[i+2], pix[i+3]}
			if got := nrgba.NRGBAAt(x, y); got != want {
				t.Fatalf("pixel (%d,%d) %v, want %v", x, y, got, want)
			}
		}
	}
}

// TestWebPLosslessFixturesDecode proves the vendored decoder does not
// reject real libwebp lossless output and decodes it exactly as x/image
// does: these fixtures use the predictor, subtract-green and
// colour-indexing transforms and multi-symbol prefix codes that
// webptest.VP8LSolid never emits. Each must decode through Decode and match
// x/image's own decode pixel for pixel.
func TestWebPLosslessFixturesDecode(t *testing.T) {
	for _, name := range []string{"lossless-alpha.webp", "lossless-indexed.webp", "lossless-cc.webp", "lossless-multigroup.webp"} {
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
			t.Fatalf("%s: imgcache.Decode: %v (the vendored decoder must not reject real libwebp output)", name, err)
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

// TestWebPMetaGroupsRenumber: building only the groups tiles name means
// renumbering them, so a picture whose tiles name groups out of the order
// their codes appear must still paint each tile its own group's colour. A
// 16×16 picture of 4 tiles names groups 2,0,3,1; the vendored decode must
// match x/image's (which here builds all four groups and so is a correct
// reference — the group count is tiny). libwebp always numbers groups
// densely in order, so only a crafted file exercises this.
func TestWebPMetaGroupsRenumber(t *testing.T) {
	colours := []color.NRGBA{{10, 20, 30, 255}, {200, 40, 40, 255}, {40, 200, 40, 255}, {40, 40, 200, 255}}
	tileGroup := []int{2, 0, 3, 1} // tile (0,0)->2, (1,0)->0, (0,1)->3, (1,1)->1
	data := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LMetaSolids(16, 16, 3, tileGroup, colours)))
	ref, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("x/image reference decode: %v", err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("imgcache.Decode: %v", err)
	}
	for ty := 0; ty < 2; ty++ {
		for tx := 0; tx < 2; tx++ {
			want := colours[tileGroup[ty*2+tx]]
			x, y := tx*8+4, ty*8+4 // middle of the 8×8 tile
			gr, gg, gb, ga := got.At(x, y).RGBA()
			if gr>>8 != uint32(want.R) || gg>>8 != uint32(want.G) || gb>>8 != uint32(want.B) || ga>>8 != uint32(want.A) {
				t.Errorf("tile (%d,%d) at px (%d,%d): (%d,%d,%d,%d), want %v (group %d)",
					tx, ty, x, y, gr>>8, gg>>8, gb>>8, ga>>8, want, tileGroup[ty*2+tx])
			}
			rr, rg, rb, ra := ref.At(x, y).RGBA()
			if gr != rr || gg != rg || gb != rb || ga != ra {
				t.Errorf("tile (%d,%d): vendored %v, x/image %v", tx, ty,
					[4]uint32{gr, gg, gb, ga}, [4]uint32{rr, rg, rb, ra})
			}
		}
	}
}

// TestWebPConfigRegistered: dropping x/image/webp's init (its unbudgeted
// decoder) must not stop image.DecodeConfig reading a WebP header — the
// composer, emoji add and profile edit size-gate attachments with it.
// imgcache registers the vendored header reader in its place.
func TestWebPConfigRegistered(t *testing.T) {
	data := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LLiteral(12, 9, make([]byte, 4*12*9))))
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("image.DecodeConfig on a WebP: %v", err)
	}
	if format != "webp" || cfg.Width != 12 || cfg.Height != 9 {
		t.Errorf("DecodeConfig got %q %dx%d, want webp 12x9", format, cfg.Width, cfg.Height)
	}
}
