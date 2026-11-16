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
// charge: what all the decodes, scales, players and copies in flight hold
// together never passes it. A decoder gives a buffer's charge back (Free)
// as soon as it no longer references it (progressive coefficients once the
// image is built, an Adam7 pass once merged, the scaler's band), and a
// decode gives back the rest when it, and the scale or conversion after
// it, is done: so a decode holds what's live.
//
// Small decodes (under smallDecode: emoji, avatars, message thumbnails)
// never wait: a charge fits now or fails with ErrBusy, and the pool's last
// decodeReserve bytes are theirs. A decode growing past smallDecode takes
// the pool's one big slot first, behind the big decodes before it (a queue
// of big decodes only), so one big decode at a time competes for the pool;
// holding it, a charge the pool can't cover waits for memory to be given
// back (budget.DefaultBigWait) before it fails with ErrBusy. The cache
// tries a busy load again shortly with the bytes it fetched (busyTries
// times; then the image has failed, the lightbox shows the file's largest
// thumbnail, and it's tried again later), never as undecodable. A decode
// past its own limit (MaxDecode) fails with ErrOverCap, an ErrTooBig: the
// same bytes always need the same memory, and a file's thumbnail stands in
// for it (the lightbox, Copy Image).

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

// MaxDecode is the most one decode may hold at once: a decode needing more
// is ErrOverCap. What a decode holds at its peak, a pixel: a baseline JPEG
// 1.5 bytes (4:2:0) to 3 (4:4:4); a progressive one 4.5 (4:2:0), 6 (4:2:2)
// or 9 (4:4:4), its int16 coefficients given back once the image is built;
// a CMYK or RGB JPEG 7 to 12; an 8-bit PNG 1 to 4 and a 16-bit one 8
// (interlaced, half as much again for its largest pass); a GIF's canvas 4,
// and 8 when it disposes to previous. So a 48 MP phone photo (8064×6048,
// baseline 4:2:0) takes 73 MB and its 2048 px scale about 18 MB more, a
// 40 MP progressive 4:2:0 JPEG 180 MB, a 26 MP progressive 4:4:4 one
// 235 MB; Copy Image's PNG of a photo takes about 2 bytes a pixel more.
const MaxDecode = DecodeMemory - decodeReserve

// ErrOverCap is a decode that needs more memory than one decode may hold
// (MaxDecode): an ErrTooBig, as the same bytes always need the same
// memory. A Slack file's largest thumbnail stands in for it: the lightbox
// shows it, Copy Image copies it.
var ErrOverCap = fmt.Errorf("%w: it needs more decode memory than an image may have", ErrTooBig)

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
// over. Past smallDecode it takes the pool's one big slot (after the big
// decodes before it: small decodes never wait), and then waits for room
// (budget.DefaultBigWait) when the pool can't cover a charge.
func NewBudget() *Budget { return budget.New(decodePool, MaxDecode) }

// newPlayerBudget is an animation player's Budget (anim.go): as NewBudget,
// but it never takes the big slot or waits, as a streaming player holds its
// memory for as long as it plays.
func newPlayerBudget() *Budget { return budget.NewPlayer(decodePool, MaxDecode) }

// NewKeepBudget is a Budget for decoded images a caller keeps (the GIF
// picker's composed frames): charge them to it for as long as they're
// kept, and Release it when they go. It never takes the big slot or waits
// (its memory isn't given back on demand), and a big decode waiting for
// memory doesn't wait for it.
func NewKeepBudget() *Budget { return budget.NewPlayer(decodePool, MaxDecode) }

// WithBudget runs f with a new Budget, released when f returns. f failing
// with ErrBusy runs again, with a new Budget, after a wait that starts at
// busyStep and doubles to busyStepMax, until ctx is done (or for busyWait,
// when ctx has no deadline); then the ErrBusy is returned. For one-shot
// callers (Copy Image, the composer's and the emoji dialog's thumbnails, a
// picked profile photo) that have no retry of their own. f must not start
// another Budget of its own (a second big decode would wait for the slot
// its first holds).
func WithBudget(ctx context.Context, f func(*Budget) error) error {
	return withBudget(ctx, f, func(ctx context.Context) *Budget { return budget.NewContext(ctx, decodePool, MaxDecode) })
}

// TrySmallBudget runs f once with a small-lane Budget
// (budget.NewSmallContext), released when f returns: it never takes the
// big slot, so it never waits behind (or holds up) the big decodes, and a
// pool that can't cover a charge is f's ErrBusy at once, for the caller to
// try again later (the GIF picker's previews do, on a later draw); its
// memory still counts as a decode's, given back when it's done, so a big
// decode can wait for it. For many small decodes at once.
func TrySmallBudget(ctx context.Context, f func(*Budget) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	b := budget.NewSmallContext(ctx, decodePool, MaxDecode)
	defer b.Release()
	return f(b)
}

func withBudget(ctx context.Context, f func(*Budget) error, mk func(context.Context) *Budget) error {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(busyWait)
	}
	step := busyStep
	for {
		b := mk(ctx)
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
// ErrOverCap, an ErrTooBig (permanent: the same bytes need the same
// memory), a busy pool stays ErrBusy (retryable), anything else is as it
// is.
func budgetErr(err error) error {
	if err == nil || errors.Is(err, ErrBusy) || errors.Is(err, ErrTooBig) || !errors.Is(err, budget.ErrBudget) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrOverCap, err)
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

// GIF is a decoded GIF's frames and timing (the vendored image/gif's).
type GIF = gif.GIF

// DecodeGIF decodes data's first maxFrames frames (every one, when it has
// fewer) and their timing, refusing a logical screen wider or taller than
// maxSide (ErrTooBig) before decoding, every allocation charged to b: the
// GIF picker's animations. A GIF of more frames than that stops being read
// past them.
func DecodeGIF(data []byte, maxFrames, maxSide int, b *Budget) (*GIF, error) {
	if formatOf(data) != "gif" {
		return nil, errUnsupported
	}
	cfg, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width > maxSide || cfg.Height > maxSide {
		return nil, fmt.Errorf("%w: a %dx%d GIF", ErrTooBig, cfg.Width, cfg.Height)
	}
	g, err := gif.DecodeAllN(bytes.NewReader(data), b, maxFrames)
	if err != nil {
		return nil, budgetErr(err)
	}
	return g, nil
}
