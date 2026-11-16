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
	"os"
	"testing"

	xwebp "golang.org/x/image/webp"

	"github.com/some-du6e/clack/internal/imgcache/internal/vp8l"
	"github.com/some-du6e/clack/internal/imgcache/webptest"
)

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

// TestColorCacheTreeTightBudget uses libwebp's own lossless output with a
// 10-bit colour cache (lossless-cc.webp): green's alphabet, and so its
// Huffman tree, is large. It decodes within a generous budget, and one
// byte short of what it charges is refused — proving the whole cc-on decode,
// the large green tree included, is counted to the last byte. (Per-node
// charging is pinned exactly by TestTreeNodeCharge.)
func TestColorCacheTreeTightBudget(t *testing.T) {
	data, err := os.ReadFile("../../testdata/lossless-cc.webp")
	if err != nil {
		t.Fatal(err)
	}
	m, err, u := used(data, bigBudget)
	if err != nil || m == nil {
		t.Fatalf("lossless-cc.webp: %v", err)
	}
	if _, err, _ := used(data, u); err != nil {
		t.Errorf("decode at its exact budget %d failed: %v", u, err)
	}
	if _, err, _ := used(data, u-1); !errors.Is(err, vp8l.ErrBudget) {
		t.Errorf("decode one byte under budget: %v, want ErrBudget", err)
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
