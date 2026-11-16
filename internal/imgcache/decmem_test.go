package imgcache

// The decode-memory guarantee (decmem.go): every decode charges what it
// allocates, where it allocates it, to a Budget drawn from a process-wide
// pool, so what all the decodes in flight hold together never passes the
// pool's limit, and a decode needing more than its budget fails before the
// allocation that doesn't fit rather than allocating it.
//
// Each bomb test decodes a crafted input under a private pool whose limit
// sits between what the decode really allocates and what it would allocate
// if a particular charge were missing, and a plain control of the same
// size that fits. With the charge, the bomb is refused (ErrTooBig) having
// allocated little and the control decodes; remove that one charge and the
// bomb is charged too little, fits the budget and allocates the full
// amount — so the test fails. Nothing here comes from Slack.

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	imagegif "image/gif"
	imagejpeg "image/jpeg"
	imagepng "image/png"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/imgcache/internal/budget"
)

// decodeAt decodes data under a fresh private pool of limit bytes, and
// reports the image, how many bytes the decode allocated, and the error.
func decodeAt(data []byte, limit int64, maxPixels, maxSide int) (image.Image, uint64, error) {
	p := budget.NewPool(limit, 0, 0)
	b := budget.New(p, limit)
	defer b.Release()
	var img image.Image
	var err error
	n := allocBytes(func() { img, err = decodeImage(data, maxPixels, maxSide, b) })
	return img, n, err
}

// refused reports whether err is the budget refusing the decode (the same
// bytes always need the same memory): ErrTooBig, not a busy pool.
func refused(err error) bool { return errors.Is(err, ErrTooBig) }

// withTestPool runs f with the package decode pool replaced by one of
// limit bytes (no reserve; every decode past 0 bytes is big, so it takes
// the slot and waits 100 ms for room), so a test can drive the real
// loadImage and Decode under a tight cap. Tests in a package run one at a
// time, so the swap is safe.
func withTestPool(limit int64, f func()) {
	old := decodePool
	decodePool = budget.NewPool(limit, 0, 0)
	decodePool.SetBigWait(100 * time.Millisecond)
	defer func() { decodePool = old }()
	f()
}

// fill holds n bytes of the decode pool as an animation player does (it
// never takes the big slot), until released: memory pressure.
func fill(t testing.TB, n int64) *Budget {
	t.Helper()
	b := budget.NewPlayer(decodePool, n)
	if err := b.Charge(n); err != nil {
		t.Fatalf("filling the decode pool with %d bytes: %v", n, err)
	}
	return b
}

// TestGrayTRNSChargedAsNRGBA: a grayscale PNG with a tRNS chunk decodes to
// NRGBA (4 bytes a pixel), not Gray (1), so it must be charged 4×. A budget
// between 1× and 4× refuses it but passes the same image without tRNS.
// Remove the tRNS case from the decoder's per-pixel charge and the budget
// admits it, so it decodes and allocates the 4× image.
func TestGrayTRNSChargedAsNRGBA(t *testing.T) {
	const w, h = 4096, 4096 // 16 MP: Gray 16 MB, NRGBA 64 MB
	budgetBytes := int64(32 << 20)
	plain := pngSolid(w, h, pngGray8, false, nil)
	if _, _, err := decodeAt(plain, budgetBytes, MaxPixels, MaxSide); err != nil {
		t.Fatalf("a plain %d×%d grayscale PNG under a %d-byte budget: %v, want it to decode", w, h, budgetBytes, err)
	}
	trns := pngSolid(w, h, pngGray8, false, []byte{0, 0}) // one transparent grey
	_, n, err := decodeAt(trns, budgetBytes, MaxPixels, MaxSide)
	if !refused(err) {
		t.Errorf("a grayscale+tRNS PNG (decodes to NRGBA, 4×): %v, want ErrTooBig", err)
	}
	if n > 8<<20 {
		t.Errorf("the refused decode allocated %d bytes (the 64 MB NRGBA must not be made)", n)
	}
}

// TestInterlaced16BitChargesEveryPass: a 16-bit truecolor PNG decodes to
// RGBA64 (8 bytes a pixel), and interlaced (Adam7) allocates an image per
// pass on top of the full one (each given back once merged, so the peak is
// the full image and its largest pass, half of it). A budget that fits the
// non-interlaced image refuses the interlaced one. Remove the per-pass
// charge and the interlaced image is charged for the full image only,
// fits, and allocates every pass.
func TestInterlaced16BitChargesEveryPass(t *testing.T) {
	const w, h = 1024, 1024 // RGBA64: full 8 MB, its last pass 4 MB more
	budgetBytes := int64(10 << 20)
	plain := pngSolid(w, h, pngRGB16, false, nil)
	if _, _, err := decodeAt(plain, budgetBytes, MaxPixels, MaxSide); err != nil {
		t.Fatalf("a non-interlaced 16-bit PNG under a %d-byte budget: %v, want it to decode", budgetBytes, err)
	}
	inter := pngSolid(w, h, pngRGB16, true, nil)
	_, n, err := decodeAt(inter, budgetBytes, MaxPixels, MaxSide)
	if !refused(err) {
		t.Errorf("an interlaced 16-bit PNG (full image + 7 pass images): %v, want ErrTooBig", err)
	}
	if n > 16<<20 {
		t.Errorf("the refused interlaced decode allocated %d bytes, want well under the full+passes total", n)
	}
}

// TestProgressiveJPEGChargesCoefficients: a progressive JPEG allocates a
// block of coefficients for every 8×8 block of each scan component (16
// bytes a pixel per component) as soon as the scan header is parsed. A
// budget that fits the image refuses the progressive coefficients. Remove
// their charge and the decode is charged the image only, fits, and
// allocates the coefficients. (A baseline header of the same size, which
// makes no coefficients, is the control: it gets past the allocation.)
func TestProgressiveJPEGChargesCoefficients(t *testing.T) {
	const w, h = 2048, 2048 // YCbCr 4:4:4 image 12.6 MB; DC-scan coeffs (int16) 25 MB
	budgetBytes := int64(24 << 20)
	baseline := jpegHeader(w, h, 3, false)
	if _, _, err := decodeAt(baseline, budgetBytes, MaxPixels, MaxSide); refused(err) {
		t.Fatalf("a baseline %d×%d JPEG header under a %d-byte budget was refused: %v", w, h, budgetBytes, err)
	}
	prog := jpegHeader(w, h, 3, true)
	_, n, err := decodeAt(prog, budgetBytes, MaxPixels, MaxSide)
	if !refused(err) {
		t.Errorf("a progressive JPEG (per-component coefficient blocks): %v, want ErrTooBig", err)
	}
	if n > 24<<20 {
		t.Errorf("the refused progressive decode allocated %d bytes (all 25 MB of coefficients must not be made)", n)
	}
}

// TestCMYKJPEGChargesBlackPlane: a 4-component JPEG allocates a full-size
// plane for the fourth (black) component on top of the three-component
// image. A budget that fits a 3-component image of the same size refuses
// the 4-component one. Remove the black plane's charge and it fits and
// allocates the plane.
func TestCMYKJPEGChargesBlackPlane(t *testing.T) {
	const w, h = 2048, 2048 // YCbCr 12.6 MB, + black plane 4.2 MB
	budgetBytes := int64(14 << 20)
	three := jpegHeader(w, h, 3, false)
	if _, _, err := decodeAt(three, budgetBytes, MaxPixels, MaxSide); refused(err) {
		t.Fatalf("a 3-component JPEG header under a %d-byte budget was refused: %v", budgetBytes, err)
	}
	four := jpegHeader(w, h, 4, false)
	_, n, err := decodeAt(four, budgetBytes, MaxPixels, MaxSide)
	if !refused(err) {
		t.Errorf("a 4-component JPEG (an extra full plane): %v, want ErrTooBig", err)
	}
	if n > 14<<20 {
		t.Errorf("the refused 4-component decode allocated %d bytes, want under the image+plane total", n)
	}
}

// TestJPEGDecoyMarkerChargedByRealSOF: a JPEG with a decoy 1×1 frame
// header hidden in a comment, before the real large frame header, is
// charged (and sized) by the real header — the config reader and the
// decoder are the same parser, which skips the comment. A budget too small
// for the real image refuses it; its allocation stays small. Remove the
// image's charge and the real image is allocated.
func TestJPEGDecoyMarkerChargedByRealSOF(t *testing.T) {
	const w, h = 3000, 3000 // real image ≈ 27 MB; the decoy claims 1×1
	data := jpegDecoyHeader(w, h)
	if cfg, _, err := decodeConfig(data); err != nil || cfg.Width != w || cfg.Height != h {
		t.Fatalf("the config read %dx%d (%v); the decoy's 1×1 must not win", cfg.Width, cfg.Height, err)
	}
	_, n, err := decodeAt(data, 8<<20, MaxPixels, MaxSide)
	if !refused(err) {
		t.Errorf("a decoy-marker JPEG under an 8 MB budget: %v, want ErrTooBig (charged by the real 27 MB frame)", err)
	}
	if n > 8<<20 {
		t.Errorf("the refused decode allocated %d bytes (the real image must not be made)", n)
	}
}

// TestGIFCanvasCharged: an animated GIF's decoder allocates an RGBA canvas
// of the whole logical screen (4 bytes a pixel). A budget too small for the
// canvas refuses newGIFDecoder; remove its charge and the canvas is
// allocated. Driven through the index and decoder directly, under a private
// budget.
func TestGIFCanvasCharged(t *testing.T) {
	const w, h = 3000, 3000 // canvas 4 B/px ≈ 36 MB
	data := bigAnimGIF(t, w, h, 2)
	p := budget.NewPool(8<<20, 0, 0)
	b := budget.New(p, 8<<20)
	defer b.Release()
	ix, err := indexGIF(data, b)
	if err != nil {
		t.Fatalf("indexGIF (the index is small and must fit): %v", err)
	}
	var derr error
	n := allocBytes(func() { _, derr = newGIFDecoder(ix, data, b) })
	if !errors.Is(derr, budget.ErrBudget) {
		t.Errorf("newGIFDecoder of a %d×%d canvas under 8 MB: %v, want ErrBudget", w, h, derr)
	}
	if n > 8<<20 {
		t.Errorf("the refused decoder allocated %d bytes (the 36 MB canvas must not be made)", n)
	}
}

// TestManyConcurrentDecodesStayUnderCap: many big decodes at once never
// make the pool hold more than its limit, and all of them finish correctly
// (a decode that can't get its memory now retries, through DecodeWithin's
// WithBudget, until it can). Remove the pool's limit check and the peak
// would run past the cap.
func TestManyConcurrentDecodesStayUnderCap(t *testing.T) {
	data := realJPEG(t, 4000, 4000) // 4:2:0 YCbCr ≈ 24 MB each
	decodePool.ResetPeak()
	const n = 32
	var wg sync.WaitGroup
	var ok, bad atomic.Int64
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			img, err := Decode(data)
			if err != nil || img.Bounds().Dx() != 4000 {
				bad.Add(1)
				return
			}
			ok.Add(1)
		}()
	}
	wg.Wait()
	if pk := decodePool.Peak(); pk > DecodeMemory {
		t.Errorf("%d concurrent decodes peaked at %d bytes, over the %d cap", n, pk, DecodeMemory)
	}
	if bad.Load() != 0 {
		t.Errorf("%d of %d concurrent decodes failed", bad.Load(), n)
	}
	if ok.Load() != n {
		t.Errorf("%d of %d concurrent decodes succeeded", ok.Load(), n)
	}
}

// TestBusyDecodeRetryableAndLoadsWhenFreed: a decode that can't get its
// memory because other decodes hold the pool fails with ErrBusy, which is
// retryable (not permanent, not undecodable), and the cache records it as
// still-loading rather than failed; once the memory frees, the same bytes
// load.
func TestBusyDecodeRetryableAndLoadsWhenFreed(t *testing.T) {
	if permanent(ErrBusy) {
		t.Error("ErrBusy is permanent; a busy pool must be retried")
	}
	if got := undecodable(ErrBusy); !errors.Is(got, ErrBusy) || errors.Is(got, ErrUndecodable) {
		t.Errorf("undecodable(ErrBusy) = %v; a busy decode is not undecodable", got)
	}
	if retryAfter(1, ErrBusy) > RetryBusyMax || retryAfter(1, ErrBusy) <= 0 {
		t.Errorf("retryAfter for a busy decode is %v, want a short retry", retryAfter(1, ErrBusy))
	}

	data := pngSolid(1024, 1024, pngRGBA8, false, nil) // NRGBA 4 MB
	withTestPool(16<<20, func() {
		hold := fill(t, decodePool.Limit())
		// decode directly: the pool is full, so it can't get its memory
		b := NewBudget()
		_, err := decodeImage(data, MaxPixels, MaxSide, b)
		b.Release()
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("a decode with the pool held full: %v, want ErrBusy", err)
		}
		hold.Release()
		b2 := NewBudget()
		defer b2.Release()
		img, err := decodeImage(data, MaxPixels, MaxSide, b2)
		if err != nil {
			t.Fatalf("the decode once the memory freed: %v", err)
		}
		if img.Bounds().Dx() != 1024 {
			t.Errorf("decoded %v, want 1024 wide", img.Bounds())
		}
	})
}

// TestBusyLoadNotFailed: a cache load the decode memory is too full for is
// recorded as still-loading (Failed is false) and retried, not a failure.
func TestBusyLoadNotFailed(t *testing.T) {
	data := pngSolid(1024, 1024, pngRGBA8, false, nil) // 4 MB
	withTestPool(16<<20, func() {
		c := New(func(_ context.Context, _ string) ([]byte, error) { return data, nil }, "", nil)
		k := key{url: "https://files.slack.com/x.png", size: 0} // not DiskOK: memory only
		hold := fill(t, decodePool.Limit())
		c.load(k)
		if f, ok := c.failed[k]; !ok || !f.busy || f.final {
			t.Errorf("a busy load recorded %+v, want busy and not final", c.failed[k])
		}
		if c.Failed(k.url, k.size) {
			t.Error("Failed reports a busy (still-loading) image as failed")
		}
		hold.Release()
		c.load(k)
		if _, ok := c.entries[k]; !ok {
			t.Error("the image didn't load once the decode memory freed")
		}
		if _, ok := c.failed[k]; ok {
			t.Errorf("a loaded image is still recorded failed: %+v", c.failed[k])
		}
	})
}

// TestNormalBigImagesDecode: ordinary large photos and screenshots decode
// through the shared entry points — a 24 MP JPEG, a 24 MP PNG, and the
// lightbox's full-size scale of one — without the budget refusing them.
func TestNormalBigImagesDecode(t *testing.T) {
	jpg := realJPEG(t, 6000, 4000) // 24 MP
	img, err := Decode(jpg)
	if err != nil {
		t.Fatalf("a 24 MP JPEG: %v", err)
	}
	if img.Bounds().Dx() != 6000 || img.Bounds().Dy() != 4000 {
		t.Fatalf("decoded %v, want 6000×4000", img.Bounds())
	}
	// the lightbox opens the original scaled to its 2048 px cap
	if err := WithBudget(context.Background(), func(b *Budget) error {
		rgba, err := ScaleBudget(img, 0, b)
		if err != nil {
			return err
		}
		if rgba.Bounds().Dx() != 2048 {
			t.Errorf("scaled to %v, want 2048 wide", rgba.Bounds())
		}
		return nil
	}); err != nil {
		t.Fatalf("scaling a 24 MP photo for the lightbox: %v", err)
	}

	var png bytes.Buffer
	if err := imagepng.Encode(&png, image.NewRGBA(image.Rect(0, 0, 4096, 4096))); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(png.Bytes()); err != nil { // 16 MP PNG
		t.Fatalf("a 16 MP PNG: %v", err)
	}
}

// TestUnsupportedFormatRejected: TIFF (linked into the binary through
// go-text, so image.Decode would decode it unbudgeted) and other formats
// imgcache doesn't decode are rejected as image.ErrFormat, cheaply.
func TestUnsupportedFormatRejected(t *testing.T) {
	// a little-endian TIFF header (II*\0) — enough for the sniffer
	tiff := []byte("II\x2a\x00\x08\x00\x00\x00")
	_, n, err := decodeAt(tiff, MaxDecode, MaxPixels, MaxSide)
	if !errors.Is(err, image.ErrFormat) {
		t.Errorf("a TIFF: %v, want image.ErrFormat (imgcache decodes PNG/JPEG/GIF/WebP/BMP only)", err)
	}
	if n > 1<<20 {
		t.Errorf("rejecting a TIFF allocated %d bytes", n)
	}
}

// realJPEG is a valid baseline JPEG of a gradient, w×h, for decode tests.
func realJPEG(t testing.TB, w, h int) []byte {
	t.Helper()
	m := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	for i := range m.Y {
		m.Y[i] = byte(i)
	}
	var b bytes.Buffer
	if err := imagejpeg.Encode(&b, m, &imagejpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encoding a %d×%d JPEG: %v", w, h, err)
	}
	return b.Bytes()
}

// bigAnimGIF is an animated GIF of a w×h solid screen (frames of it).
func bigAnimGIF(t testing.TB, w, h, frames int) []byte {
	t.Helper()
	pal := color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{255, 255, 255, 255}}
	g := &imagegif.GIF{LoopCount: 0}
	for i := 0; i < frames; i++ {
		g.Image = append(g.Image, image.NewPaletted(image.Rect(0, 0, w, h), pal))
		g.Delay = append(g.Delay, 5)
		g.Disposal = append(g.Disposal, imagegif.DisposalNone)
	}
	var b bytes.Buffer
	if err := imagegif.EncodeAll(&b, g); err != nil {
		t.Fatalf("encoding a %d×%d GIF: %v", w, h, err)
	}
	return b.Bytes()
}

// TestDecodeGIFBudgetedAndCapped: DecodeGIF (the GIF picker's decode)
// keeps a GIF's first maxFrames frames, reading no further, each charged:
// under a budget too small for 120 of a GIF's 1024×1024 frames it's refused
// having allocated about the budget, and 30 of them decode. A screen past
// maxSide is refused by its header.
func TestDecodeGIFBudgetedAndCapped(t *testing.T) {
	data := manyFrameGIF(1024, 1024, 400)
	p := budget.NewPool(64<<20, 0, 0)
	b := budget.New(p, 64<<20)
	var err error
	n := allocBytes(func() { _, err = DecodeGIF(data, 120, 1024, b) })
	b.Release()
	if !errors.Is(err, ErrTooBig) {
		t.Errorf("120 frames of 1 MB under a 64 MB budget: %v, want ErrTooBig", err)
	}
	if n > 80<<20 {
		t.Errorf("the refused GIF decode allocated %d bytes, want about its 64 MB budget", n)
	}
	b = budget.New(p, 64<<20)
	g, err := DecodeGIF(data, 30, 1024, b)
	b.Release()
	if err != nil {
		t.Fatalf("the first 30 frames: %v", err)
	}
	if len(g.Image) != 30 {
		t.Errorf("%d frames, want 30", len(g.Image))
	}
	if _, err := DecodeGIF(manyFrameGIF(2048, 16, 1), 1, 1024, budget.Unlimited()); !errors.Is(err, ErrTooBig) {
		t.Errorf("a 2048-wide GIF with a 1024 limit: %v, want ErrTooBig", err)
	}
}
