package imgcache

// Animated WebP, played like a GIF (gifstream.go, anim.go). Slack serves
// some custom emoji as animated WebP behind a .png URL (:explodes:), and
// x/image/webp decodes only still WebP: the ANIM/ANMF chunks are skipped,
// the file ends without an image, and the decode fails. Here the RIFF
// container is indexed as a GIF is, without decoding any pixels: the
// canvas, the loop count and each ANMF frame's rectangle, duration,
// blending, disposal and where its ALPH and VP8/VP8L chunks are. A
// frame's chunks decode through x/image/webp as a still WebP of their
// own when the frame is due.
//
// Composition follows Chromium's WEBPImageDecoder: every pass starts
// from a transparent canvas; a frame is alpha-blended over the canvas, or
// with its no-blend bit replaces its rectangle; dispose-to-background
// clears the rectangle to transparent (the ANIM background colour is
// ignored, as Chromium ignores it); the loop count is the number of
// passes (0: forever); and a duration of 10 ms or less plays at 100 ms
// (DeferredImageDecoder, for every format). A frame reaching past the
// canvas is clipped, and one that doesn't decode leaves the canvas as it
// was.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/gif"
	"log"
	"time"

	"golang.org/x/image/webp"
)

var errBadWebP = errors.New("imgcache: bad animated WebP")

// isAnimWebP reports whether data is a WebP whose VP8X header says it's
// animated.
func isAnimWebP(b []byte) bool {
	return len(b) >= 30 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" &&
		string(b[12:16]) == "VP8X" && b[20]&0x02 != 0
}

// isAnim reports whether data may hold an animation (a GIF or an
// animated WebP): it decodes through the frame index.
func isAnim(data []byte) bool { return isGIF(data) || isAnimWebP(data) }

// indexAnim is indexGIF or indexWebP, by data's signature.
func indexAnim(data []byte) (*gifIndex, error) {
	if isAnimWebP(data) {
		return indexWebP(data)
	}
	return indexGIF(data)
}

func le24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

// indexWebP reads an animated WebP's canvas, loop count and frames. A
// file that breaks off after its first frame is the frames it has.
func indexWebP(b []byte) (*gifIndex, error) {
	if !isAnimWebP(b) {
		return nil, errBadWebP
	}
	end := len(b)
	if n := int(binary.LittleEndian.Uint32(b[4:8])) + 8; n >= 12 && n < end {
		end = n
	}
	ix := &gifIndex{webp: true}
	truncated, seenX := false, false
	for p := 12; p+8 <= end; {
		id, size := string(b[p:p+4]), int(binary.LittleEndian.Uint32(b[p+4:p+8]))
		q := p + 8
		if size < 0 || q+size > end { // past the end: what's there is the file
			if id != "ANMF" {
				break
			}
			size = end - q
		}
		c := b[q : q+size]
		switch id {
		case "VP8X":
			if size < 10 || seenX {
				return nil, errBadWebP
			}
			seenX = true
			ix.w, ix.h = 1+le24(c[4:]), 1+le24(c[7:])
			if ix.w > MaxSide || ix.h > MaxSide || ix.w*ix.h > MaxPixels {
				return nil, ErrTooBig
			}
		case "ANIM":
			if size >= 6 {
				// WebP counts passes, GIF's (and gifIndex's) repetitions
				switch n := int(binary.LittleEndian.Uint16(c[4:6])); n {
				case 0:
					ix.loop = 0 // forever
				case 1:
					ix.loop = -1 // once
				default:
					ix.loop = n - 1
				}
			}
		case "ANMF":
			if size < 16 {
				break
			}
			x, y := 2*le24(c[0:]), 2*le24(c[3:])
			fw, fh := 1+le24(c[6:]), 1+le24(c[9:])
			if fw > MaxSide || fh > MaxSide || fw*fh > MaxPixels {
				return nil, ErrTooBig
			}
			if len(ix.frames) == maxFrames {
				truncated = true
				p = end
				continue
			}
			fl := c[15]
			f := gifFrame{r: image.Rect(x, y, x+fw, y+fh), trans: -1, data: q + 16, n: size - 16,
				delay: animDelay(time.Duration(le24(c[12:])) * time.Millisecond), blend: fl&0x02 == 0}
			if fl&0x01 != 0 {
				f.disp = gif.DisposalBackground
			}
			ix.frames = append(ix.frames, f)
		}
		p = q + size + size&1
	}
	if !seenX || len(ix.frames) == 0 {
		return nil, errBadWebP
	}
	if truncated {
		log.Printf("imgcache: an animated WebP of more than %d frames plays its first %d", maxFrames, maxFrames)
	}
	return ix, nil
}

// webpFrameFile writes into buf (reused) frame f's ALPH and VP8/VP8L
// chunks as a still WebP of their own, for x/image/webp.
func webpFrameFile(buf []byte, payload []byte, w, h int) ([]byte, error) {
	var alph, pic []byte
	lossless := false
	for p := 0; p+8 <= len(payload); {
		id, size := string(payload[p:p+4]), int(binary.LittleEndian.Uint32(payload[p+4:p+8]))
		if size < 0 || p+8+size > len(payload) {
			if id != "VP8 " && id != "VP8L" {
				break
			}
			size = len(payload) - p - 8 // cut short: what's there decodes as far as it goes
		}
		ch := payload[p : p+8+size]
		switch id {
		case "ALPH":
			if alph == nil {
				alph = ch
			}
		case "VP8 ", "VP8L":
			pic, lossless = ch, id == "VP8L"
		}
		if pic != nil {
			break
		}
		p += 8 + size + size&1
	}
	if pic == nil {
		return nil, errBadWebP
	}
	b := append(buf[:0], "RIFF\x00\x00\x00\x00WEBP"...)
	chunk := func(ch []byte) {
		b = append(b, ch...)
		binary.LittleEndian.PutUint32(b[len(b)-len(ch)+4:], uint32(len(ch)-8)) // a cut one's real length
		if len(ch)&1 != 0 {
			b = append(b, 0)
		}
	}
	if alph != nil && !lossless { // VP8L carries its own alpha (x/image refuses ALPH with it)
		b = append(b, "VP8X\x0a\x00\x00\x00\x10\x00\x00\x00"...)
		b = append(b, byte(w-1), byte((w-1)>>8), byte((w-1)>>16), byte(h-1), byte((h-1)>>8), byte((h-1)>>16))
		chunk(alph)
	}
	chunk(pic)
	binary.LittleEndian.PutUint32(b[4:8], uint32(len(b)-8))
	return b, nil
}

// drawWebP decodes WebP frame f and composes it on the canvas: blended
// over what's there, or replacing its rectangle. A frame that doesn't
// decode leaves the canvas as it is (d.werr says why).
func (d *gifDecoder) drawWebP(f *gifFrame) {
	d.werr = nil
	end := min(f.data+f.n, len(d.data))
	if f.data >= end {
		d.werr = errBadWebP
		return
	}
	file, err := webpFrameFile(d.wfile, d.data[f.data:end], f.r.Dx(), f.r.Dy())
	if err != nil {
		d.werr = err
		return
	}
	d.wfile = file
	m, err := webp.Decode(bytes.NewReader(file))
	if err != nil {
		d.werr = err
		return
	}
	mb := m.Bounds()
	// the ALPH plane is the ANMF rectangle's size, the picture its VP8
	// header's: a picture larger than the plane would read past it
	if a, ok := m.(*image.NYCbCrA); ok && (a.AStride < mb.Dx() || len(a.A) < a.AStride*mb.Dy()) {
		d.werr = errBadWebP
		return
	}
	r := f.r.Intersect(d.canvas.Rect).Intersect(mb.Sub(mb.Min).Add(f.r.Min))
	if r.Empty() {
		return
	}
	op := draw.Src
	if f.blend {
		op = draw.Over
	}
	draw.Draw(d.canvas, r, m, mb.Min.Add(r.Min.Sub(f.r.Min)), op)
}
