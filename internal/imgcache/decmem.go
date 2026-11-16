package imgcache

// Decode memory. Image bytes are anyone's (an avatar, a custom emoji, a
// photo or an unfurl any workspace member posts), and what a decoder
// allocates depends on fields in them, so every decode charges what it
// allocates, where it allocates it, to a Budget (internal/budget): the
// vendored PNG, JPEG, GIF, BMP and WebP decoders (internal/png, jpeg, gif,
// bmp, webp, vp8l) before each buffer, the scaler (internal/catmullrom)
// before its weights and band, the animation decoder before its canvas,
// save buffer, frames and scaler (gifstream.go, anim.go), and Copy Image
// before its clipboard copies. Nothing reads a header first to guess.
//
// Every Budget draws from one process-wide pool, DecodeMemory, charge by
// charge, with a try-acquire that never waits: what all the decodes,
// scales, players and copies in flight hold together never passes it. A
// charge the pool can't cover now fails the decode with ErrBusy, which
// the cache treats as "try again shortly" (retryAfter), never as
// undecodable; one past the decode's own limit (MaxDecode) fails with
// ErrTooBig: the same bytes always need the same memory. A decode gives
// back everything it charged when it, and the scale or conversion after
// it, is done. There's no queue: no head-of-line blocking, no fairness to
// manage, no deadlock.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"time"

	"github.com/some-du6e/clack/internal/imgcache/internal/bmp"
	"github.com/some-du6e/clack/internal/imgcache/internal/budget"
	"github.com/some-du6e/clack/internal/imgcache/internal/catmullrom"
	"github.com/some-du6e/clack/internal/imgcache/internal/gif"
	"github.com/some-du6e/clack/internal/imgcache/internal/jpeg"
	"github.com/some-du6e/clack/internal/imgcache/internal/png"
	"github.com/some-du6e/clack/internal/imgcache/internal/webp"
)

// DecodeMemory is the most that every image decode in flight (and the
// scales, animation players and clipboard copies after them) holds at
// once: a third of cmd/clack's 768 MiB soft memory limit, which leaves the
// rest for the session (its heaviest, two 3,000-message channels open, is
// about 500 MB live). Its last decodeReserve bytes are only for decodes
// that stay under smallDecode (emoji, avatars, thumbnails), so the big
// ones in flight can't make every small one wait.
const (
	DecodeMemory  = 256 << 20
	decodeReserve = 32 << 20
	smallDecode   = 8 << 20
)

// MaxDecode is the most one decode may hold: a decode needing more is
// ErrTooBig. A 48 MP phone photo (8064×6048, baseline 4:2:0) decodes to
// 73 MB and scales to 2048 px in about 18 MB more; Copy Image's PNG of it
// takes about 100 MB more again.
const MaxDecode = DecodeMemory - decodeReserve

// decodePool is the memory every Budget draws from.
var decodePool = budget.NewPool(DecodeMemory, decodeReserve, smallDecode)

// ErrBusy is a decode that couldn't get its memory because other decodes
// in flight hold it: nothing is wrong with the image, and it decodes once
// they're done. The cache tries again shortly (retryAfter); WithBudget
// tries again for a while before giving up with it.
var ErrBusy = budget.ErrBusy

// Budget is the memory one decode (and the scale or conversion after it)
// may allocate, drawn from the process-wide pool as it's charged.
type Budget = budget.Budget

// NewBudget returns a Budget of at most MaxDecode drawing from the decode
// pool. Release it once the decode and everything done with its image is
// over.
func NewBudget() *Budget { return budget.New(decodePool, MaxDecode) }

// WithBudget runs f with a new Budget, released when f returns. f failing
// with ErrBusy runs again, with a new Budget, after a wait that starts at
// busyStep and doubles to busyStepMax, until ctx is done (or for busyWait,
// when ctx has no deadline); then the ErrBusy is returned. For one-shot
// callers (Copy Image, the composer's and the emoji dialog's thumbnails, a
// picked profile photo) that have no retry of their own. Nothing waits on
// the pool itself: a decode that can't have its memory fails at once, and
// runs again from the start.
func WithBudget(ctx context.Context, f func(*Budget) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(busyWait)
	}
	step := busyStep
	for {
		b := NewBudget()
		err := f(b)
		b.Release()
		if !errors.Is(err, ErrBusy) || time.Now().Add(step).After(deadline) {
			return err
		}
		t := time.NewTimer(step)
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
		step = min(2*step, busyStepMax)
	}
}

// WithBudget's waits for a busy pool.
const (
	busyWait    = 10 * time.Second
	busyStep    = 50 * time.Millisecond
	busyStepMax = time.Second
)

// ChargeFunc is b.Charge with its errors as imgcache reports them
// (budgetErr): for the copies a caller makes of a decoded image (Copy
// Image's clipboard conversion) under the decode's budget.
func ChargeFunc(b *Budget) func(int64) error {
	return func(n int64) error { return budgetErr(b.Charge(n)) }
}

// budgetErr is err as imgcache reports it: a decode past its budget is
// ErrTooBig (permanent: the same bytes need the same memory), a busy pool
// stays ErrBusy (retryable), anything else is as it is.
func budgetErr(err error) error {
	if err == nil || errors.Is(err, ErrBusy) || errors.Is(err, ErrTooBig) || !errors.Is(err, budget.ErrBudget) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrTooBig, err)
}

// errUnsupported is image bytes in a format imgcache doesn't decode: not
// PNG, JPEG, GIF, WebP or BMP. TIFF is linked into the binary (go-text's
// typesetting imports x/image/tiff, which registers itself with package
// image), and its decoder allocates up to 8 bytes a pixel plus a strip
// buffer as big again by fields in the file, with no budget; a network
// image never reaches it here. It is image.ErrFormat: permanent, as bytes
// that aren't an image at all.
var errUnsupported = fmt.Errorf("%w: imgcache decodes PNG, JPEG, GIF, WebP and BMP only", image.ErrFormat)

// formatOf is the format imgcache decodes data as, by its signature (the
// magic package image sniffs for each), or "" for none of them.
func formatOf(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case bytes.HasPrefix(data, []byte("\xff\xd8")):
		return "jpeg"
	case len(data) >= 6 && string(data[:4]) == "GIF8" && data[5] == 'a':
		return "gif"
	case len(data) >= 15 && isWebP(data) && string(data[12:15]) == "VP8":
		return "webp"
	case len(data) >= 10 && string(data[:2]) == "BM" && string(data[6:10]) == "\x00\x00\x00\x00":
		return "bmp"
	}
	return ""
}

// decodeConfig reads data's header with the vendored decoder that decodes
// it (the same parser, so the size it reports is the one its decode
// allocates by), and its format.
func decodeConfig(data []byte) (image.Config, string, error) {
	f := formatOf(data)
	r := bytes.NewReader(data)
	var cfg image.Config
	var err error
	switch f {
	case "png":
		cfg, err = png.DecodeConfig(r)
	case "jpeg":
		cfg, err = jpeg.DecodeConfig(r)
	case "gif":
		cfg, err = gif.DecodeConfig(r)
	case "webp":
		cfg, err = webp.DecodeConfig(r)
	case "bmp":
		cfg, err = bmp.DecodeConfig(r)
	default:
		return image.Config{}, "", errUnsupported
	}
	return cfg, f, err
}

// decodeImage decodes data, refusing a header past maxPixels or maxSide
// (ErrTooBig) before decoding, every allocation of the decode charged to b
// first. A WebP decodes its picture's chunks alone (decodeWebP), within its
// picture's budget too.
func decodeImage(data []byte, maxPixels, maxSide int, b *Budget) (image.Image, error) {
	cfg, format, err := decodeConfig(data)
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxSide || cfg.Height > maxSide || cfg.Width*cfg.Height > maxPixels {
		return nil, ErrTooBig
	}
	r := bytes.NewReader(data)
	var m image.Image
	switch format {
	case "png":
		m, err = png.Decode(r, b)
	case "jpeg":
		m, err = jpeg.Decode(r, b)
	case "gif":
		m, err = gif.Decode(r, b)
	case "webp":
		m, err = decodeWebP(data, maxPixels, maxSide, b)
	case "bmp":
		m, err = bmp.Decode(r, b)
	}
	if err != nil {
		return nil, budgetErr(err)
	}
	return m, nil
}

// DecodeBudget is Decode within maxPixels and maxSide, everything the
// decode allocates charged to b, which the caller holds for as long as it
// works with the image (and so its conversions charge b too), then
// releases.
func DecodeBudget(data []byte, maxPixels, maxSide int, b *Budget) (image.Image, error) {
	return decodeImage(data, maxPixels, maxSide, b)
}

// rgbaBytes is an RGBA image of size sz's pixels.
func rgbaBytes(sz image.Point) int64 { return 4 * int64(sz.X) * int64(sz.Y) }

// ScaleBudget is Scale charged to b: the scaled image, and the scaler's
// weights and band (internal/catmullrom: the pixels x/image's
// CatmullRom.Scale draws, a band at a time) or the converted copy.
func ScaleBudget(img image.Image, max int, b *Budget) (*image.RGBA, error) {
	bd := img.Bounds()
	w, h := bd.Dx(), bd.Dy()
	if max <= 0 {
		max = 2048
	}
	if w > max || h > max {
		sz := scaledSize(w, h, max)
		if err := b.Charge(rgbaBytes(sz)); err != nil {
			return nil, budgetErr(err)
		}
		dst := image.NewRGBA(image.Rectangle{Max: sz})
		if err := catmullrom.Scale(dst, img, bd, b); err != nil {
			return nil, budgetErr(err)
		}
		return dst, nil
	}
	if r, ok := img.(*image.RGBA); ok && bd.Min == (image.Point{}) {
		return r, nil
	}
	if err := b.Charge(rgbaBytes(bd.Size())); err != nil {
		return nil, budgetErr(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), img, bd.Min, draw.Src)
	return dst, nil
}

// ScaleInto draws src's sr scaled to the whole of dst, the pixels
// x/image/draw's CatmullRom.Scale(dst, dst.Bounds(), src, sr, draw.Src,
// nil) draws, a band of rows at a time, its weights and band charged to b
// (dst is the caller's, charged by it).
func ScaleInto(dst *image.RGBA, src image.Image, sr image.Rectangle, b *Budget) error {
	return budgetErr(catmullrom.Scale(dst, src, sr, b))
}
