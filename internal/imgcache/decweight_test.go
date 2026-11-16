package imgcache

// Tests that a decode's memory weight (decweight.go) covers what the stdlib
// decoder really allocates, measured as a TotalAlloc delta, for the cases
// the plain 4·w·h weight got wrong: progressive JPEG, 16-bit PNG, the YCbCr
// subsampling, and that an unsupported format (TIFF) is rejected, not
// decoded unbounded. Synthetic images only; nothing comes from Slack.

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	xtiff "golang.org/x/image/tiff" // importing it registers TIFF, so the allowlist test sees format "tiff"
)

// weightCoversAlloc checks the header weight is an upper bound on what
// decoding data really allocates, and not grossly over (so legitimate
// images aren't needlessly refused).
func weightCoversAlloc(t *testing.T, name string, data []byte, weight int64) {
	t.Helper()
	var derr error
	actual := allocBytes(func() { _, _, derr = image.Decode(bytes.NewReader(data)) })
	// derr may be non-nil for a deliberately-truncated bomb; the allocation
	// still happened and is what we measure.
	_ = derr
	if int64(actual) > weight {
		t.Errorf("%s: decode allocated %d bytes but the weight reserves only %d (under-count)", name, actual, weight)
	}
	if weight > int64(actual)*3+1<<20 {
		t.Errorf("%s: weight %d is far over the %d allocated (over-reserve)", name, weight, actual)
	}
}

func TestJPEGWeightBaseline(t *testing.T) {
	const w, h = 320, 240
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range src.Pix {
		src.Pix[i] = byte(i * 7)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	weight, ok := jpegWeight(data)
	if !ok {
		t.Fatal("jpegWeight rejected a stdlib-encoded JPEG")
	}
	weightCoversAlloc(t, "baseline JPEG", data, weight)
}

// A progressive JPEG allocates the progressive coefficient blocks (256 B
// per 8×8 per component, ~12 B/px at 4:4:4) on top of the YCbCr image, all
// before any entropy data is read; 4·w·h would reserve a quarter of it.
func TestJPEGWeightProgressive(t *testing.T) {
	const w, h = 512, 512
	data := progressiveJPEGHeader(w, h)
	weight, ok := jpegWeight(data)
	if !ok {
		t.Fatal("jpegWeight rejected a progressive JPEG header")
	}
	// confirm the big allocation really happens (else the test is vacuous):
	// YCbCr 4:4:4 (~786 KB) + 3 progCoeffs (~3 MB) ≫ the plain 4·w·h.
	var derr error
	actual := allocBytes(func() { _, _, derr = image.Decode(bytes.NewReader(data)) })
	if derr == nil {
		t.Fatal("the truncated progressive JPEG decoded instead of erroring")
	}
	if int64(actual) < 4*int64(w)*int64(h) {
		t.Fatalf("progressive decode allocated only %d bytes; the coefficient blocks weren't reached (bad craft)", actual)
	}
	if int64(actual) > weight {
		t.Errorf("progressive JPEG: allocated %d but weight reserves only %d", actual, weight)
	}
	if weight > int64(actual)*3+1<<20 {
		t.Errorf("progressive JPEG: weight %d far over %d allocated", weight, actual)
	}
}

func TestPNGWeight8bit(t *testing.T) {
	const w, h = 300, 200
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range src.Pix {
		src.Pix[i] = byte(i * 5)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if _, _, bpp, _, ok := pngIHDR(data); !ok || bpp != 4 {
		t.Fatalf("pngIHDR of an 8-bit NRGBA PNG: bpp %d ok %v, want 4", bpp, ok)
	}
	weight, ok := pngWeight(data)
	if !ok {
		t.Fatal("pngWeight rejected an 8-bit PNG")
	}
	weightCoversAlloc(t, "8-bit PNG", data, weight)
}

// A 16-bit PNG decodes to NRGBA64 (8 B/px), twice the plain 4·w·h weight.
func TestPNGWeight16bit(t *testing.T) {
	const w, h = 300, 200
	src := image.NewNRGBA64(image.Rect(0, 0, w, h))
	for i := range src.Pix {
		src.Pix[i] = byte(i * 3)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	_, _, bpp, _, ok := pngIHDR(data)
	if !ok || bpp != 8 {
		t.Fatalf("pngIHDR of a 16-bit PNG: bpp %d ok %v, want 8", bpp, ok)
	}
	weight, _ := pngWeight(data)
	weightCoversAlloc(t, "16-bit PNG", data, weight)
}

// The interlace flag doubles the weight (full image plus the Adam7 pass
// images); pngIHDR reads it and pngWeight applies it. stdlib can't encode
// interlaced PNG, so this crafts the IHDR directly.
func TestPNGWeightInterlace(t *testing.T) {
	const w, h = 256, 256
	plain := pngIHDRBytes(w, h, 8, 6, 0) // 8-bit RGBA, no interlace
	woven := pngIHDRBytes(w, h, 8, 6, 1) // Adam7
	wp, _ := pngWeight(plain)
	wi, _ := pngWeight(woven)
	if wi != 2*(wp-smallConst)+smallConst {
		t.Errorf("interlaced weight %d, want double the %d non-interlaced pixel bytes", wi, wp-smallConst)
	}
}

func TestGIFWeight(t *testing.T) {
	const w, h = 200, 150
	src := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White, color.RGBA{255, 0, 0, 255}})
	for i := range src.Pix {
		src.Pix[i] = byte(i % 3)
	}
	var buf bytes.Buffer
	if err := gif.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	weight, ok := decodeWeight(data, cfg, "gif")
	if !ok {
		t.Fatal("decodeWeight rejected a GIF")
	}
	weightCoversAlloc(t, "GIF", data, weight)
}

// TIFF is linked into the binary (go-text reads sbix font glyphs with it),
// so image.DecodeConfig recognises it; imgcache must still refuse to decode
// a network TIFF, whose allocation it can't bound from the header. A PNG of
// the same size is accepted, so the rejection is the format, not the size.
func TestWeighDecodeRejectsTIFF(t *testing.T) {
	const w, h = 64, 64
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := xtiff.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	if _, format, _, err := weighDecode(buf.Bytes(), MaxPixels, MaxSide); err != ErrTooBig {
		t.Errorf("weighDecode of a %s TIFF: err %v (want ErrTooBig; the format must be refused)", format, err)
	}
	var png0 bytes.Buffer
	if err := png.Encode(&png0, src); err != nil {
		t.Fatal(err)
	}
	if _, format, _, err := weighDecode(png0.Bytes(), MaxPixels, MaxSide); err != nil {
		t.Errorf("weighDecode of a same-size PNG: err %v format %q, want it accepted", err, format)
	}
}

// progressiveJPEGHeader is a minimal 3-component (4:4:4) progressive JPEG:
// SOI, a SOF2 frame header and an interleaved DC scan header, then one
// entropy byte. Go's decoder allocates the YCbCr image and the progressive
// coefficient blocks in processSOS before it reads entropy data, so it
// reaches those allocations and then fails on the truncated/ tableless
// scan. No DQT/DHT is needed to reach the allocation.
func progressiveJPEGHeader(w, h int) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xff, 0xd8}) // SOI
	sof := []byte{
		8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), 3, // precision, H, W, nComp
		1, 0x11, 0, // Y  1×1
		2, 0x11, 0, // Cb 1×1
		3, 0x11, 0, // Cr 1×1
	}
	b.Write([]byte{0xff, 0xc2, byte((len(sof) + 2) >> 8), byte(len(sof) + 2)}) // SOF2
	b.Write(sof)
	sos := []byte{
		3,       // components in scan
		1, 0x00, // Y  DC/AC table 0
		2, 0x00, // Cb
		3, 0x00, // Cr
		0, 0, 0x00, // Ss=0, Se=0, Ah/Al=0 → first DC scan
	}
	b.Write([]byte{0xff, 0xda, byte((len(sos) + 2) >> 8), byte(len(sos) + 2)}) // SOS
	b.Write(sos)
	b.Write([]byte{0x00}) // one entropy byte, then EOF
	return b.Bytes()
}

// pngIHDRBytes is a PNG signature and a well-formed IHDR chunk (no image
// data) for pngIHDR to parse: dimensions, bit depth, colour type and
// interlace method.
func pngIHDRBytes(w, h, depth, colorType, interlace int) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	ihdr[0], ihdr[1], ihdr[2], ihdr[3] = byte(w>>24), byte(w>>16), byte(w>>8), byte(w)
	ihdr[4], ihdr[5], ihdr[6], ihdr[7] = byte(h>>24), byte(h>>16), byte(h>>8), byte(h)
	ihdr[8] = byte(depth)
	ihdr[9] = byte(colorType)
	ihdr[12] = byte(interlace)
	b.Write([]byte{0, 0, 0, 13})
	b.WriteString("IHDR")
	b.Write(ihdr)
	b.Write([]byte{0, 0, 0, 0}) // CRC (pngIHDR doesn't check it)
	return b.Bytes()
}
