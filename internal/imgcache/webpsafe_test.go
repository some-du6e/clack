package imgcache

// WebP image bytes are untrusted (any workspace member can upload a
// custom emoji), and x/image allocates a frame's pixels from the
// bitstream's own 14-bit size fields before it reads them: a ~30-byte
// VP8L chunk claiming 16384×16384 fills 1 GiB, a lossy VP8 one ~400 MB.
// Here a crafted bomb is rejected by its header, with no large allocation
// and no decode, as libwebp's demux rejects it (a frame's picture must be
// its ANMF rectangle's size, the rectangle must lie in the canvas, and a
// VP8X still's picture must be the canvas's size). Synthetic files only.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"os"
	"runtime"
	"testing"

	"golang.org/x/image/webp"

	"github.com/some-du6e/clack/internal/imgcache/webptest"
)

// allocBytes is how many bytes f allocates (TotalAlloc delta): a header
// bomb that decoded would allocate hundreds of MB, one rejected cheaply
// only kilobytes.
func allocBytes(f func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	f()
	runtime.GC()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// loadBytesAlloc runs the cache's real decode entry (loadImage, which
// drives Decode for stills and the frame compose for animations) on data,
// and reports how many bytes it allocated and the error. A size or
// structure bomb must be rejected cheaply; a decode of the claimed size
// would allocate hundreds of MB.
func loadBytesAlloc(data []byte) (uint64, error) {
	c := &Cache{
		fetch: func(context.Context, string) ([]byte, error) { return data, nil },
		ctx:   context.Background(),
	}
	k := key{url: "https://files.slack.com/x.webp", size: 16} // not DiskOK: memory only
	var err error
	n := allocBytes(func() { _, _, _, _, _, err = c.loadImage(k) })
	return n, err
}

func vp8lChunk(w, h int) []byte {
	return webptest.Chunk("VP8L", webptest.VP8LSolid(w, h, color.NRGBA{1, 2, 3, 255}))
}

// webpSize reads only the picture's header, never its pixels.
func TestWebPSizeReadsHeaderOnly(t *testing.T) {
	// a lossless picture claiming 16384², a lossy one claiming 16383²
	if w, h, ok := webpSize(vp8lChunk(16384, 16384), true); !ok || w != 16384 || h != 16384 {
		t.Errorf("VP8L header: %d×%d ok=%v, want 16384×16384", w, h, ok)
	}
	vp8 := webptest.Chunk("VP8 ", webptest.VP8Header(16383, 16383, 16))
	if w, h, ok := webpSize(vp8, false); !ok || w != 16383 || h != 16383 {
		t.Errorf("VP8 header: %d×%d ok=%v, want 16383×16383", w, h, ok)
	}
	// rubbish, a truncated header, a lossy frame that isn't a key frame
	for _, bad := range [][]byte{
		webptest.Chunk("VP8L", []byte("junk")),
		webptest.Chunk("VP8L", []byte{0x2f, 0}),
		webptest.Chunk("VP8 ", []byte{1, 0, 0, 0x9d, 0x01, 0x2a, 1, 0, 1, 0, 0, 0, 0, 0, 0, 0}),
	} {
		lossless := string(bad[:4]) == "VP8L"
		if _, _, ok := webpSize(bad, lossless); ok {
			t.Errorf("a bad %s picture passed webpSize", bad[:4])
		}
	}
}

// A frame whose picture header claims a larger size than its ANMF
// rectangle is rejected before x/image allocates the claimed size.
// Measured across the real decode entry (loadImage): the frame-size guard
// lives in both indexWebP and drawWebP, so the allocation stays small even
// though the pictures claim 16384².
func TestWebPFrameSizeMustMatchRect(t *testing.T) {
	bombs := map[string][]byte{
		"VP8L 16384² in a 1×1 frame": webptest.Anim(1, 1, 0,
			webptest.Frame{X: 0, Y: 0, W: 1, H: 1, DurMS: 50, Chunks: [][]byte{vp8lChunk(16384, 16384)}}),
		"VP8 16383² in a 1×1 frame": webptest.Anim(1, 1, 0,
			webptest.Frame{X: 0, Y: 0, W: 1, H: 1, DurMS: 50, Chunks: [][]byte{webptest.Chunk("VP8 ", webptest.VP8Header(16383, 16383, 16))}}),
	}
	for name, data := range bombs {
		n, err := loadBytesAlloc(data)
		if n > 16<<20 {
			t.Errorf("%s: decoding allocated %d bytes (a decode of the claimed size, not the 1×1 frame)", name, n)
		}
		if err == nil {
			t.Errorf("%s: decoded instead of being rejected", name)
		}
	}
}

// A frame whose rectangle isn't fully inside the canvas is rejected
// (libwebp's CheckFrameBounds), so it can't compose past the canvas.
func TestWebPFrameBoundsChecked(t *testing.T) {
	for _, f := range []webptest.Frame{
		webptest.Solid(0, 0, 4, 4, 50, color.NRGBA{255, 0, 0, 255}), // 4×4 past a 2×2 canvas
		webptest.Solid(2, 0, 2, 2, 50, color.NRGBA{255, 0, 0, 255}), // offset past the right edge
	} {
		if _, err := testIndexWebP(webptest.Anim(2, 2, 0, f)); err == nil {
			t.Errorf("a frame at %v past the 2×2 canvas was indexed", image.Rect(f.X, f.Y, f.X+f.W, f.Y+f.H))
		}
	}
	// a frame flush with the canvas is in bounds
	if _, err := testIndexWebP(webptest.Anim(4, 4, 0, webptest.Solid(0, 0, 4, 4, 50, color.NRGBA{0, 255, 0, 255}))); err != nil {
		t.Errorf("a frame filling the canvas: %v", err)
	}
}

// A still VP8X WebP whose inner picture header claims a larger size than
// the VP8X canvas is rejected before decoding (libwebp's
// ParseHeadersInternal checks the picture against the canvas): the canvas,
// which the gating sees, is a real bound on the decode.
func TestWebPStillInnerSizeChecked(t *testing.T) {
	bomb := webptest.Still(2, 2, 0, vp8lChunk(16384, 16384)) // 2×2 canvas, 16384² inner
	var err error
	if n := allocBytes(func() { _, err = Decode(bomb) }); n > 16<<20 {
		t.Errorf("decoding allocated %d bytes for a 2×2 canvas with a 16384² inner picture", n)
	}
	if err == nil {
		t.Error("a VP8X still with an oversized inner picture decoded")
	}
	lossy := webptest.Still(2, 2, 0, webptest.Chunk("VP8 ", webptest.VP8Header(16383, 16383, 16)))
	if n := allocBytes(func() { _, err = Decode(lossy) }); n > 16<<20 {
		// without the canvas check, x/image's VP8 DecodeFrame calls ensureImg
		// (~400 MB for 16383²) before it fails parsing the garbage body
		t.Errorf("decoding allocated %d bytes for a 2×2 canvas with a 16383² inner lossy picture", n)
	}
	if err == nil {
		t.Error("a VP8X still with an oversized inner lossy picture decoded")
	}
}

// A legitimate lossless still with alpha and metadata is RIFF/WEBP/VP8X
// (ALPHA_FLAG)/…/VP8L, because libwebp's mux sets ALPHA_FLAG for any
// image with alpha, VP8L included. x/image refuses VP8L under the alpha
// flag, so imgcache rewraps the inner picture as a still of its own and
// decodes it, as Chromium shows it.
func TestWebPStillAlphaFlagLossless(t *testing.T) {
	vp8l := webptest.Chunk("VP8L", webptest.VP8LSolid(8, 8, color.NRGBA{10, 20, 30, 128}))
	iccp := webptest.Chunk("ICCP", []byte("icc profile"))
	exif := webptest.Chunk("EXIF", []byte("exif"))
	// ALPHA_FLAG + ICCP + EXIF around a VP8L (its own alpha): the real shape
	data := webptest.Still(8, 8, 0x10|0x20|0x08, iccp, vp8l, exif)
	if _, err := webp.Decode(bytes.NewReader(data)); err == nil {
		t.Skip("x/image/webp decodes VP8L under the alpha flag now: the rewrap may be unnecessary")
	}
	m, err := Decode(data)
	if err != nil {
		t.Fatalf("a VP8X+ALPHA_FLAG+VP8L still: %v (Chromium and Slack show it)", err)
	}
	if b := m.Bounds(); b.Dx() != 8 || b.Dy() != 8 {
		t.Fatalf("decoded %v, want 8×8", b)
	}
	if g, ok := m.(*image.NRGBA); !ok {
		t.Errorf("decoded %T, want *image.NRGBA", m)
	} else if p := g.NRGBAAt(0, 0); p != (color.NRGBA{10, 20, 30, 128}) {
		t.Errorf("pixel %v, want the VP8L's {10 20 30 128}", p)
	}
}

// Valid still WebP of every shape this package rewraps still decodes to
// the same image: a simple VP8L, a simple lossy VP8, and a VP8X lossy
// still with alpha (the x/image test picture's chunks).
func TestWebPStillsDecode(t *testing.T) {
	simpleL := webptest.Simple(vp8lChunk(5, 5))
	if m, err := Decode(simpleL); err != nil || m.Bounds() != image.Rect(0, 0, 5, 5) {
		t.Errorf("simple VP8L: %v %v", err, m.Bounds())
	}
	still, err := os.ReadFile("testdata/blue-purple-pink.lossy.webp")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := webp.Decode(bytes.NewReader(still)) // x/image, directly
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(still) // through the rewrap
	if err != nil {
		t.Fatalf("simple lossy VP8: %v", err)
	}
	if got.Bounds() != ref.Bounds() {
		t.Errorf("lossy VP8: %v, want %v", got.Bounds(), ref.Bounds())
	}
	for _, p := range []image.Point{{0, 0}, {75, 50}, {149, 99}} {
		gr, _, _, _ := got.At(p.X, p.Y).RGBA()
		rr, _, _, _ := ref.At(p.X, p.Y).RGBA()
		if gr != rr {
			t.Errorf("lossy VP8 at %v: red %d, want %d", p, gr>>8, rr>>8)
		}
	}
	// a VP8X with ALPH + VP8 (the lossy chunk under an alpha flag): the
	// ALPH is wrapped back in
	vp8 := still[12:] // the VP8 chunk of the simple file
	alph := webptest.Chunk("ALPH", append([]byte{0}, make([]byte, 150*100)...))
	ext := webptest.Still(150, 100, 0x10, alph, vp8)
	if m, err := Decode(ext); err != nil {
		t.Errorf("VP8X+ALPH+VP8 still: %v", err)
	} else if _, ok := m.(*image.NYCbCrA); !ok {
		t.Errorf("VP8X+ALPH+VP8 decoded %T, want *image.NYCbCrA", m)
	}
}
