package webp

// Clack: tests that every allocation a WebP decode makes charges the
// caller's vp8l.Budget, so a stream whose decode needs more than its
// budget fails before the allocation rather than running the process out
// of memory. Each class is tested by an exact-delta measurement: two
// inputs that differ in one allocation class by a known amount must make
// Budget.Used() differ by exactly that amount, so dropping the charge
// (Used() wouldn't move) fails the test. Inputs are synthetic (webptest)
// or the libwebp fixtures in ../../testdata; nothing comes from Slack.

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"os"
	"runtime"
	"testing"

	xwebp "golang.org/x/image/webp"

	"github.com/some-du6e/clack/internal/imgcache/internal/vp8l"
	"github.com/some-du6e/clack/internal/imgcache/webptest"
)

// allocBytes is how many bytes f allocates (TotalAlloc delta). Used to
// check the budget charged at least as much as a decode really allocated.
func allocBytes(f func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	f()
	runtime.GC()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// referenceDecode decodes data with x/image's own (unbudgeted) webp
// decoder, the output the vendored decoder must reproduce exactly.
func referenceDecode(t *testing.T, name string, data []byte) image.Image {
	t.Helper()
	m, err := xwebp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("%s: x/image reference decode: %v", name, err)
	}
	return m
}

// The byte sizes the vendored vp8l charges, mirrored here for the exact
// assertions (vp8l/budget_test.go checks them against unsafe.Sizeof).
const (
	hGroupCharge = 5 * (24 + 4<<7) // an hGroup: 5 trees of header+LUT = 2680
	// a flat byte code (webptest) has 256 symbols of length 8, so 2*256-1 =
	// 511 nodes of 8 bytes; a one-symbol code (distance) is 1 node.
	flatGroupNodes = 4*(2*256-1)*8 + 1*8 // 16360
	perFullGroup   = hGroupCharge + flatGroupNodes + 4
	perEmptyGroup  = hGroupCharge + 4 // group array + its index map, no trees read
)

// used decodes data under a budget of n bytes and reports the image, the
// error and how many bytes the decode charged.
func used(data []byte, n int64) (image.Image, error, int64) {
	b := vp8l.NewBudget(n)
	m, err := Decode(bytes.NewReader(data), b)
	return m, err, b.Used()
}

const bigBudget = 256 << 20

// TestPixelBufferCharge: the pixel buffer charges 4·w·h. Two literal
// pictures of the same width and the code (so everything but the pixel
// buffer is identical) differ in used by exactly 4·w·(h2−h1).
func TestPixelBufferCharge(t *testing.T) {
	const w, h1, h2 = 8, 50, 100
	mk := func(h int) []byte {
		return webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LLiteral(w, h, make([]byte, 4*w*h))))
	}
	_, err1, u1 := used(mk(h1), bigBudget)
	_, err2, u2 := used(mk(h2), bigBudget)
	if err1 != nil || err2 != nil {
		t.Fatalf("decode errors: %v, %v", err1, err2)
	}
	if got, want := u2-u1, int64(4*w*(h2-h1)); got != want {
		t.Errorf("pixel-buffer charge delta %d, want %d (is the pixel buffer charged?)", got, want)
	}
	// the budget gates: exactly u2 decodes, one byte less is refused.
	if _, err, _ := used(mk(h2), u2); err != nil {
		t.Errorf("decode at its exact budget %d failed: %v", u2, err)
	}
	if _, err, _ := used(mk(h2), u2-1); !errors.Is(err, vp8l.ErrBudget) {
		t.Errorf("decode one byte under budget: %v, want ErrBudget", err)
	}
}

// TestGroupArrayCharge: the Huffman-group array charges one hGroup
// (2680 B) per group the meta image names, plus the 4-byte-per-index map.
// Two meta images on the same canvas naming different numbers of distinct
// groups differ in used by exactly the difference × 2684. (The streams end
// after the meta image, before any group's trees, so only the array is
// charged — this is the finding-#1 allocation.)
func TestGroupArrayCharge(t *testing.T) {
	const nA, nB = 4000, 8000
	mk := func(ng int) []byte {
		return webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LManyGroups(512, 512, 2, ng)))
	}
	// a big budget so the group-array charge is admitted and the decode then
	// fails reading the first group's trees at end of stream.
	_, errA, uA := used(mk(nA), bigBudget)
	_, errB, uB := used(mk(nB), bigBudget)
	if errA == nil || errB == nil {
		t.Fatalf("a bodyless many-groups stream should fail at EOF: %v, %v", errA, errB)
	}
	if got, want := uB-uA, int64(nB-nA)*perEmptyGroup; got != want {
		t.Errorf("group-array charge delta %d, want %d (is the group array charged?)", got, want)
	}
}

// TestTreeNodeCharge: each group's five Huffman trees charge their node
// arrays. Two pictures whose meta images name different numbers of groups,
// each group a full flat byte code, differ in used by exactly the group
// difference × perFullGroup (the array, the five trees' nodes, the index
// map). Drop the per-node charge and the delta falls to perEmptyGroup.
func TestTreeNodeCharge(t *testing.T) {
	const nA, nB = 4, 12
	mk := func(ng int) []byte {
		return webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LManyGroupsFull(16, 16, 2, ng)))
	}
	mA, errA, uA := used(mk(nA), bigBudget)
	mB, errB, uB := used(mk(nB), bigBudget)
	if errA != nil || errB != nil {
		t.Fatalf("many-groups-full should decode: %v, %v", errA, errB)
	}
	if mA == nil || mB == nil {
		t.Fatal("no image")
	}
	if got, want := uB-uA, int64(nB-nA)*perFullGroup; got != want {
		t.Errorf("tree-node charge delta %d, want %d (are the groups' tree nodes charged?)", got, want)
	}
}

// TestColorCacheCharge: a colour cache of ccBits charges its 4·2^ccBits-byte
// table (and widens green's alphabet, so its code-length scratch grows too).
// Two otherwise-identical literal pictures differing only in ccBits differ
// in used by exactly (4+8)·(2^b2−2^b1): 4 for the table, 8 for the two
// scratch arrays sized by the alphabet.
func TestColorCacheCharge(t *testing.T) {
	const w, h, b1, b2 = 6, 4, 1, 11
	pix := make([]byte, 4*w*h)
	mk := func(cc int) []byte {
		return webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LLiteralCC(w, h, cc, pix)))
	}
	_, err1, u1 := used(mk(b1), bigBudget)
	_, err2, u2 := used(mk(b2), bigBudget)
	if err1 != nil || err2 != nil {
		t.Fatalf("decode errors: %v, %v", err1, err2)
	}
	if got, want := u2-u1, int64(12)*((1<<b2)-(1<<b1)); got != want {
		t.Errorf("colour-cache charge delta %d, want %d (is the cache table charged?)", got, want)
	}
}

// TestFixtureChargesCoverAllocation decodes each libwebp fixture and checks
// the budget charged at least as many bytes as the decode actually
// allocated (TotalAlloc). The budget exists to be a true upper bound on a
// decode's memory, so every allocation — the pixel buffer, each group's
// trees, the colour cache, the colour-indexing unpack buffer — must be
// charged; drop any one and the charge falls below what was allocated and
// this fails. It subsumes the old tautological cc-tree test (any
// deterministic charge sequence passed that), and is the only check that
// catches an allocation nobody charged (the colour-indexing unpack buffer,
// the large cc green tree). It also pins the tight budget: one byte under
// what a decode charges is refused.
func TestFixtureChargesCoverAllocation(t *testing.T) {
	// Lossless (VP8L) fixtures only: a lossy VP8 decode also copies the
	// frame's partitions out of the chunk, which the budget deliberately
	// doesn't charge (bounded by the input, not amplified), so its charge is
	// not an upper bound on its allocation. Every VP8L allocation is charged.
	for _, name := range []string{
		"lossless-alpha.webp", "lossless-indexed.webp",
		"lossless-cc.webp", "lossless-multigroup.webp",
	} {
		data, err := os.ReadFile("../../testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var m image.Image
		var derr error
		var u int64
		actual := allocBytes(func() { m, derr, u = used(data, bigBudget) })
		if derr != nil || m == nil {
			t.Fatalf("%s: %v", name, derr)
		}
		// the whole decode's allocation must be covered by what it charged
		if u < int64(actual) {
			t.Errorf("%s: charged %d bytes but allocated %d (an allocation went uncharged)", name, u, actual)
		}
		// and the budget gates to the last byte: exactly u decodes, one less is refused
		if _, err, _ := used(data, u); err != nil {
			t.Errorf("%s: decode at its exact budget %d failed: %v", name, u, err)
		}
		if _, err, _ := used(data, u-1); !errors.Is(err, vp8l.ErrBudget) {
			t.Errorf("%s: decode one byte under budget: %v, want ErrBudget", name, err)
		}
	}
}

// TestDropsUnusedGroups decodes a valid VP8L whose tiles name only a few of
// the Huffman groups the stream defines, the rest full byte-code groups
// with no tile (VP8LMetaGapped). The vendored decoder keeps only the groups
// tiles use, renumbering them, and reads the unused ones into reused scratch
// and drops them. So the charge stays far below what building a group per
// named index (up to maxIdx) would cost, and far below charging each
// dropped group's trees; and the pixels still match x/image's reference
// (which builds every group), proving a dropped group's codes didn't
// corrupt a kept group's tree or its renumbering.
func TestDropsUnusedGroups(t *testing.T) {
	// 16×16, 8px tiles → 2×2 = 4 tiles naming groups {0, 40, 70, 100}; the
	// 97 unnamed indices up to 100 are full byte-code groups, read and
	// dropped.
	tileGroup := []int{0, 100, 40, 70}
	colour := make([]color.NRGBA, 101)
	colour[0] = color.NRGBA{10, 20, 30, 255}
	colour[40] = color.NRGBA{200, 40, 40, 255}
	colour[70] = color.NRGBA{40, 200, 40, 255}
	colour[100] = color.NRGBA{40, 40, 200, 255}
	maxIdx := 100
	data := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LMetaGapped(16, 16, 3, tileGroup, colour)))

	m, err, u := used(data, bigBudget)
	if err != nil || m == nil {
		t.Fatalf("VP8LMetaGapped decode: %v", err)
	}
	// building a group per index 0..maxIdx would charge at least that many
	// hGroups for the array alone; only the used groups are kept, so u is
	// well under it (and under charging each dropped group's trees, far more).
	if max := int64(maxIdx+1) * hGroupCharge; u >= max {
		t.Errorf("charged %d bytes, want well under %d (one hGroup per index 0..%d): unused groups weren't dropped", u, max, maxIdx)
	}
	// the kept groups, renumbered, still paint each tile its own colour
	ref := referenceDecode(t, "VP8LMetaGapped", data)
	for ty := 0; ty < 2; ty++ {
		for tx := 0; tx < 2; tx++ {
			want := colour[tileGroup[ty*2+tx]]
			x, y := tx*8+4, ty*8+4
			gr, gg, gb, ga := m.At(x, y).RGBA()
			if gr>>8 != uint32(want.R) || gg>>8 != uint32(want.G) || gb>>8 != uint32(want.B) || ga>>8 != uint32(want.A) {
				t.Errorf("tile (%d,%d) at (%d,%d): (%d,%d,%d,%d), want %v", tx, ty, x, y, gr>>8, gg>>8, gb>>8, ga>>8, want)
			}
			rr, rg, rb, ra := ref.At(x, y).RGBA()
			if gr != rr || gg != rg || gb != rb || ga != ra {
				t.Errorf("tile (%d,%d): vendored %v, x/image %v", tx, ty,
					[4]uint32{gr, gg, gb, ga}, [4]uint32{rr, rg, rb, ra})
			}
		}
	}
}

// TestAlphaPlaneCompression1Charge: a lossy picture's VP8L-compressed
// (compression 1) alpha plane is decoded as a VP8L image and then its green
// channel copied out to the alpha plane; both scale with the plane's size.
// Two pictures differing only in height differ in used by the VP8L plane's
// pixel buffer (4 bytes a pixel), the copied-out plane (1 byte a pixel) and
// the VP8 frame. Drop the plane-copy charge (webp/decode.go's
// make([]byte, len(pix)/4)) and the delta is short by one byte a pixel.
// TestAlphaPlaneCharge covers only the compression-0 path.
func TestAlphaPlaneCompression1Charge(t *testing.T) {
	const w, h1, h2 = 48, 16, 48
	mk := func(h int) []byte {
		// compression 1 = a headerless VP8L plane; readAlpha synthesizes its
		// header from the VP8X size, so one solid body serves any height.
		alph := webptest.Chunk("ALPH", append([]byte{0x01}, webptest.VP8LSolidBody(0x80)...))
		vp8 := webptest.Chunk("VP8 ", webptest.VP8Header(w, h, 32)) // garbage body: charged, then fails
		return webptest.Still(w, h, 0x10, alph, vp8)                // VP8X alpha flag
	}
	_, _, u1 := used(mk(h1), bigBudget)
	_, _, u2 := used(mk(h2), bigBudget)
	mbw := int64((w + 15) / 16)
	frame := 388 * mbw * (int64((h2+15)/16) - int64((h1+15)/16))
	planePix := int64(4 * w * (h2 - h1)) // the VP8L plane's NRGBA pixel buffer
	planeCopy := int64(w * (h2 - h1))    // the alpha bytes copied out of it
	if got, want := u2-u1, planePix+planeCopy+frame; got != want {
		t.Errorf("compression-1 ALPH charge delta %d, want %d (plane pix %d + copy %d + frame %d)",
			got, want, planePix, planeCopy, frame)
	}
}

// TestVP8FrameCharge: a lossy frame charges vp8FrameBytes by its header's
// size before it is decoded. Two VP8 pictures differing only in height
// differ in used by exactly the macroblock-count difference × 388.
func TestVP8FrameCharge(t *testing.T) {
	const w, h1, h2 = 48, 16, 48 // 3 macroblocks wide; 1 vs 3 tall
	mk := func(h int) []byte {
		return webptest.Simple(webptest.Chunk("VP8 ", webptest.VP8Header(w, h, 32)))
	}
	// the garbage VP8 body fails to decode, but the frame is charged first.
	_, _, u1 := used(mk(h1), bigBudget)
	_, _, u2 := used(mk(h2), bigBudget)
	mbw := int64((w + 15) / 16)
	want := 388 * mbw * (int64((h2+15)/16) - int64((h1+15)/16))
	if got := u2 - u1; got != want {
		t.Errorf("vp8 frame charge delta %d, want %d (is the frame charged by its header?)", got, want)
	}
}

// TestAlphaPlaneCharge: an uncompressed (compression-0) ALPH plane charges
// w·h bytes. A lossy picture with an ALPH plane, grown in height, charges
// the plane difference plus the VP8 frame difference; dropping the plane
// charge leaves only the frame difference.
func TestAlphaPlaneCharge(t *testing.T) {
	const w, h1, h2 = 48, 16, 48
	mk := func(h int) []byte {
		alph := webptest.Chunk("ALPH", append([]byte{0x00}, make([]byte, w*h)...)) // compression 0
		vp8 := webptest.Chunk("VP8 ", webptest.VP8Header(w, h, 32))
		return webptest.Still(w, h, 0x10, alph, vp8) // VP8X alpha flag
	}
	_, _, u1 := used(mk(h1), bigBudget)
	_, _, u2 := used(mk(h2), bigBudget)
	mbw := int64((w + 15) / 16)
	frame := 388 * mbw * (int64((h2+15)/16) - int64((h1+15)/16))
	plane := int64(w * (h2 - h1))
	if got, want := u2-u1, plane+frame; got != want {
		t.Errorf("alpha-plane+frame charge delta %d, want %d (plane %d + frame %d)", got, want, plane, frame)
	}
}

// TestColorIndexUnpackCharge: a colour-indexing transform that packs
// several pixels to a byte (here 2 colours → 8 a byte) unpacks them into a
// fresh 4·oldWidth·h buffer on inversion; that buffer is charged before
// it's made (decode.go, before inverseColorIndexing). Two pictures of the
// same width and palette differing only in height differ in used by the
// packed main image (4·⌈w/8⌉·Δh) plus that unpack buffer (4·w·Δh). Drop the
// unpack charge and the delta is short by 4·w·Δh. No fixture delta covered
// this (lossless-indexed.webp is one size).
func TestColorIndexUnpackCharge(t *testing.T) {
	const w, h1, h2 = 16, 16, 64
	mk := func(h int) []byte {
		return webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LIndexed(w, h, color.NRGBA{10, 20, 30, 255})))
	}
	m1, err1, u1 := used(mk(h1), bigBudget)
	m2, err2, u2 := used(mk(h2), bigBudget)
	if err1 != nil || err2 != nil || m1 == nil || m2 == nil {
		t.Fatalf("indexed decode errors: %v, %v", err1, err2)
	}
	rw := (w + 7) / 8
	mainPix := int64(4 * rw * (h2 - h1)) // the packed main image
	unpack := int64(4 * w * (h2 - h1))   // the buffer inverseColorIndexing unpacks into
	if got, want := u2-u1, mainPix+unpack; got != want {
		t.Errorf("colour-index charge delta %d, want %d (main %d + unpack %d): is the unpack buffer charged?",
			got, want, mainPix, unpack)
	}
	// a sanity check that it really decodes to the palette colour
	if gr, gg, gb, ga := m1.At(0, 0).RGBA(); gr>>8 != 10 || gg>>8 != 20 || gb>>8 != 30 || ga>>8 != 255 {
		t.Errorf("indexed pixel (%d,%d,%d,%d), want (10,20,30,255)", gr>>8, gg>>8, gb>>8, ga>>8)
	}
}

// TestFixturesDecode confirms the budgeted decoder decodes every libwebp
// fixture (a generous budget) to the same image x/image's own webp decoder
// produces, pixel for pixel — the vendored changes alter no output.
func TestFixturesDecode(t *testing.T) {
	for _, name := range []string{
		"lossless-alpha.webp", "lossless-indexed.webp", "lossless-cc.webp",
		"lossless-multigroup.webp", "blue-purple-pink.lossy.webp",
	} {
		data, err := os.ReadFile("../../testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(bytes.NewReader(data), vp8l.NewBudget(bigBudget))
		if err != nil {
			t.Fatalf("%s: budgeted decode: %v", name, err)
		}
		ref := referenceDecode(t, name, data)
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
