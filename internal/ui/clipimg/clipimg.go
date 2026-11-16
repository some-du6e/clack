// Package clipimg puts an image on the system clipboard. Gio's clipboard
// carries text only, so the image goes through the platform: wl-copy or
// xclip on Linux (the tools the composer pastes images with), NSPasteboard
// on macOS, and user32 (CF_DIB plus the registered "PNG" format) on
// Windows. Write blocks (exec, or AppKit's main thread on macOS), so call
// it off the UI goroutine: it reports a UI-goroutine caller to uiguard.
//
// The copies Write makes of the image in Go's heap (the PNG, and its
// encoder's buffers) are charged first to the charge func WriteCharged is
// given (Copy Image's decode budget, imgcache.WithBudget), so a big image
// fails the copy instead of taking the app past its memory; the PNG is
// built in chunks as it's encoded, never one buffer grown and copied to
// its size. Windows' DIB is written straight into the clipboard's own
// memory block, never into Go's heap.
package clipimg

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"

	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// ErrUnsupported means this system has no way to put an image on the
// clipboard (on Linux: neither wl-copy nor xclip is installed).
var ErrUnsupported = errors.New("copying images isn't supported on this system")

// Write replaces the clipboard with img.
func Write(ctx context.Context, img image.Image) error {
	return WriteCharged(ctx, img, nil)
}

// WriteCharged is Write with every copy it makes of img in Go's heap
// charged to charge first (nil: charged to nothing). A charge that fails
// fails the copy with its error, before the clipboard is touched.
func WriteCharged(ctx context.Context, img image.Image, charge func(int64) error) error {
	uiguard.OffUI("clipimg.Write")
	if img == nil || img.Bounds().Empty() {
		return errors.New("empty image")
	}
	if charge == nil {
		charge = func(int64) error { return nil }
	}
	return write(ctx, img, charge)
}

// pngData is a PNG encoding, in chunks: each made (and charged) as the
// encoding reaches it, half as big again as the encoding so far, between
// minChunk and maxChunk.
type pngData struct {
	ctx    context.Context // the copy's: the encode stops once it's done
	chunks [][]byte
	n      int
	charge func(int64) error
}

const (
	minChunk = 64 << 10
	maxChunk = 4 << 20
)

func (d *pngData) Write(p []byte) (int, error) {
	if err := d.ctx.Err(); err != nil {
		return 0, err
	}
	n := len(p)
	for len(p) > 0 {
		if k := len(d.chunks); k == 0 || len(d.chunks[k-1]) == cap(d.chunks[k-1]) {
			size := min(max(minChunk, d.n/2), maxChunk)
			if err := d.charge(int64(size)); err != nil {
				return n - len(p), err
			}
			d.chunks = append(d.chunks, make([]byte, 0, size))
		}
		last := &d.chunks[len(d.chunks)-1]
		k := min(len(p), cap(*last)-len(*last))
		*last = append(*last, p[:k]...)
		p = p[k:]
		d.n += k
	}
	return n, nil
}

// Len is the encoding's length.
func (d *pngData) Len() int { return d.n }

// reader reads the encoding.
func (d *pngData) reader() io.Reader {
	rs := make([]io.Reader, len(d.chunks))
	for i, c := range d.chunks {
		rs[i] = bytes.NewReader(c)
	}
	return io.MultiReader(rs...)
}

// copyTo copies the encoding into dst (at least Len long).
func (d *pngData) copyTo(dst []byte) {
	o := 0
	for _, c := range d.chunks {
		o += copy(dst[o:], c)
	}
}

// encodePNG is img as a PNG, in chunks, charged first: the encoder's own
// buffers (pngEncoderBytes), then each chunk as it's made. It stops, with
// ctx's error, once ctx is done.
func encodePNG(ctx context.Context, img image.Image, charge func(int64) error) (*pngData, error) {
	w := img.Bounds().Dx()
	if err := charge(pngEncoderBytes(w)); err != nil {
		return nil, err
	}
	d := &pngData{ctx: ctx, charge: charge}
	if err := png.Encode(d, pngSource(img)); err != nil {
		return nil, err
	}
	return d, nil
}

// pngEncoderBytes is what png.Encode allocates for an image w wide besides
// its output (Go 1.24's image/png writer): its encoder, a 32 KB
// bufio.Writer, the zlib/flate compressor (its 512 KB hash table, 128 KB
// chain, 64 KB window and 64 KB of tokens), six rows of at most 8 bytes a
// pixel, and pngSource's converted row.
func pngEncoderBytes(w int) int64 { return 1<<20 + 6*(1+8*int64(w)) + 4*int64(w) }

// pngSource is img as png.Encode is given it. A JPEG's YCbCr or CMYK image
// png would write at 16 bits a channel (their colour models aren't 8-bit
// RGB): twice the bytes of the 8-bit RGB the pixels are, which is what the
// DIB holds and what browsers copy. So those are shown to it as 8-bit RGB,
// a row converted at a time (png reads the rows in order), the same values
// png's own At conversion would round to. Anything else is as it is.
func pngSource(img image.Image) image.Image {
	switch img.(type) {
	case *image.YCbCr, *image.CMYK:
		b := img.Bounds()
		return &rgbRows{src: img, row: image.NewRGBA(image.Rect(b.Min.X, 0, b.Max.X, 1)), y: b.Min.Y - 1}
	}
	return img
}

// rgbRows is an opaque image as 8-bit RGB, converted a row at a time.
type rgbRows struct {
	src image.Image
	row *image.RGBA
	y   int // the source row in row
}

func (m *rgbRows) ColorModel() color.Model { return color.RGBAModel }
func (m *rgbRows) Bounds() image.Rectangle { return m.src.Bounds() }
func (m *rgbRows) Opaque() bool            { return true }

func (m *rgbRows) At(x, y int) color.Color {
	if y != m.y {
		b := m.src.Bounds()
		draw.Draw(m.row, image.Rect(b.Min.X, 0, b.Max.X, 1), m.src, image.Pt(b.Min.X, y), draw.Src)
		m.y = y
	}
	return m.row.RGBAAt(x, 0)
}
