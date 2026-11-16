package webpreg_test

// This binary must not import golang.org/x/image/webp (directly or through
// a helper), or the point of the package is lost: its init would register
// "webp" first and mask imgcache's. imgcache registers the vendored,
// budgeted decoder; webptest builds WebP bytes without decoding them.

import (
	"bytes"
	"image"
	"image/color"
	"runtime"
	"testing"

	_ "github.com/some-du6e/clack/internal/imgcache" // registers the vendored WebP decoder with image
	"github.com/some-du6e/clack/internal/imgcache/webptest"
)

// TestWebPConfigRegistered: with x/image/webp not linked, importing
// imgcache must still let image.DecodeConfig read a WebP header — the
// composer, emoji add and profile edit size-gate attachments with it.
// Remove imgcache's image.RegisterFormat and no decoder is registered
// here, so DecodeConfig fails and this fails. (In imgcache's own test
// binary x/image/webp is linked, so its DecodeConfig would still work and
// the test couldn't catch the removal — which is why it lives here.)
func TestWebPConfigRegistered(t *testing.T) {
	data := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LLiteral(12, 9, make([]byte, 4*12*9))))
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("image.DecodeConfig on a WebP (is imgcache's WebP format registered?): %v", err)
	}
	if format != "webp" || cfg.Width != 12 || cfg.Height != 9 {
		t.Errorf("DecodeConfig got %q %dx%d, want webp 12x9", format, cfg.Width, cfg.Height)
	}
}

// TestWebPDecodeRegisteredIsBudgeted: the full decoder registered for
// image.Decode is imgcache's budgeted one, not an unbudgeted decoder. A
// 1×1 VP8L whose one meta tile names Huffman group 65535 would make
// x/image's decoder allocate make([]hGroup, 65536) ≈ 168 MB before reading
// a tree; the vendored decoder builds only the group the tile uses and
// fails cheaply. Decoding the bomb through image.Decode (the registry)
// must therefore both error and allocate little — if x/image/webp were
// linked and registered first, this would allocate ~168 MB.
func TestWebPDecodeRegisteredIsBudgeted(t *testing.T) {
	bomb := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LMeta(65535, color.NRGBA{}, false)))
	var derr error
	n := allocBytes(func() { _, _, derr = image.Decode(bytes.NewReader(bomb)) })
	if derr == nil {
		t.Error("a 1×1 VP8L meta bomb decoded through image.Decode instead of being rejected")
	}
	if n > 16<<20 {
		t.Errorf("image.Decode allocated %d bytes for a 1×1 VP8L meta bomb (x/image's decoder would allocate ~168 MB)", n)
	}
}

func allocBytes(f func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	f()
	runtime.GC()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}
