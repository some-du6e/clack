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
// image.Decode is imgcache's size-gated, budgeted one, not an ungated
// decode. The bomb is a VP8L whose header claims 16384×16384 (268 M px,
// over imgcache's 64 MP MaxPixels) but whose body is a few bytes of
// zero-bit codes. The registered decoder refuses it on the header, cheaply;
// an ungated vendored decode would allocate its 1 GiB pixel buffer
// (4·16384·16384) before reading the body. So decoding the bomb through
// image.Decode must both error and allocate little — this fails if
// decodeWebPRegistered is changed to skip the size gate / budget (e.g. call
// webp.Decode directly), which a meta-group bomb could not catch (the
// vendored decoder drops unused groups cheaply whatever the budget).
func TestWebPDecodeRegisteredIsBudgeted(t *testing.T) {
	bomb := webptest.Simple(webptest.Chunk("VP8L", webptest.VP8LSolid(16384, 16384, color.NRGBA{1, 2, 3, 255})))
	var derr error
	n := allocBytes(func() { _, _, derr = image.Decode(bytes.NewReader(bomb)) })
	if derr == nil {
		t.Error("a 16384² VP8L bomb decoded through image.Decode instead of being rejected on its header")
	}
	if n > 16<<20 {
		t.Errorf("image.Decode allocated %d bytes for a 16384² VP8L bomb (an ungated decode allocates ~1 GiB)", n)
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
