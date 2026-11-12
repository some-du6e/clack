package huddle

import (
	"image"
	"math"
)

// Pixel helpers for the screen share: thumbnails for the picker, the
// capture backends' RGB(A)/BGRA to NV12, and scaling NV12 down.

// fitIn is w×h scaled to fit inside box, aspect kept (Electron's
// thumbnails: never larger than the size asked for).
func fitIn(w, h int, box image.Point) (int, int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	if w <= box.X && h <= box.Y {
		return w, h
	}
	if w*box.Y > h*box.X { // wider than the box
		return box.X, max(1, h*box.X/w)
	}
	return max(1, w*box.Y/h), box.Y
}

// fitArea is the largest even w×h with the same aspect and at most
// pixels pixels.
func fitArea(w, h, pixels int) (int, int) {
	if w*h <= pixels {
		return w &^ 1, h &^ 1
	}
	s := math.Sqrt(float64(pixels) / float64(w*h))
	sw := int(float64(w)*s+0.5) &^ 1
	sh := (sw * h / w) &^ 1
	for sw*sh > pixels && sw > 16 {
		sw -= 2
		sh = (sw * h / w) &^ 1
	}
	return max(16, sw), max(16, sh)
}

// thumbOf scales img into box (area average), aspect kept.
func thumbOf(img *image.RGBA, box image.Point) *image.RGBA {
	sw, sh := img.Rect.Dx(), img.Rect.Dy()
	tw, th := fitIn(sw, sh, box)
	if tw == 0 {
		return nil
	}
	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		y0, y1 := y*sh/th, max(y*sh/th+1, (y+1)*sh/th)
		for x := 0; x < tw; x++ {
			x0, x1 := x*sw/tw, max(x*sw/tw+1, (x+1)*sw/tw)
			var r, g, b, n uint32
			for yy := y0; yy < y1; yy++ {
				row := img.Pix[(yy)*img.Stride:]
				for xx := x0; xx < x1; xx++ {
					i := xx * 4
					r += uint32(row[i])
					g += uint32(row[i+1])
					b += uint32(row[i+2])
					n++
				}
			}
			i := y*out.Stride + x*4
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = uint8(r/n), uint8(g/n), uint8(b/n), 0xff
		}
	}
	return out
}

// rgbToYUV is BT.601 video range.
func rgbToYUV(r, g, b int32) (y, u, v uint8) {
	y = uint8((66*r + 129*g + 25*b + 128 + 16<<8) >> 8)
	u = clamp8((-38*r - 74*g + 112*b + 128 + 128<<8) >> 8)
	v = clamp8((112*r - 94*g - 18*b + 128 + 128<<8) >> 8)
	return
}

// ensureNV12 sizes f's planes for w×h (even), reusing them.
func ensureNV12(f *CameraFrame, w, h int) {
	w, h = w&^1, h&^1
	f.W, f.H, f.YStride, f.UVStride, f.FullRange = w, h, w, w, false
	if cap(f.Y) < w*h {
		f.Y = make([]byte, w*h)
	}
	if cap(f.UV) < w*h/2 {
		f.UV = make([]byte, w*h/2)
	}
	f.Y, f.UV = f.Y[:w*h], f.UV[:w*h/2]
}

// rgbaToNV12 converts img (RGBA) into f, reusing f's planes.
func rgbaToNV12(f *CameraFrame, img *image.RGBA) {
	packedToNV12(f, img.Pix, img.Stride, img.Rect.Dx(), img.Rect.Dy(), 0, 1, 2)
}

// bgraToNV12 converts 32-bit BGRA/BGRX pixels (Windows DIBs, X11 ZPixmaps
// on little-endian) into f.
func bgraToNV12(f *CameraFrame, pix []byte, stride, w, h int) {
	packedToNV12(f, pix, stride, w, h, 2, 1, 0)
}

// packedToNV12 converts 4-byte pixels with R, G, B at offsets ri, gi, bi;
// chroma is the 2×2 average.
func packedToNV12(f *CameraFrame, pix []byte, stride, w, h, ri, gi, bi int) {
	ensureNV12(f, w, h)
	w, h = f.W, f.H
	for y := 0; y < h; y += 2 {
		r0 := pix[y*stride:]
		r1 := pix[(y+1)*stride:]
		yo0, yo1 := f.Y[y*w:], f.Y[(y+1)*w:]
		uv := f.UV[(y/2)*w:]
		for x := 0; x < w; x += 2 {
			var rs, gs, bs int32
			for k, row := range [2][]byte{r0, r1} {
				out := yo0
				if k == 1 {
					out = yo1
				}
				for d := 0; d < 2; d++ {
					i := (x + d) * 4
					r, g, b := int32(row[i+ri]), int32(row[i+gi]), int32(row[i+bi])
					out[x+d], _, _ = rgbToYUV(r, g, b)
					rs += r
					gs += g
					bs += b
				}
			}
			_, u, v := rgbToYUV(rs/4, gs/4, bs/4)
			uv[x], uv[x+1] = u, v
		}
	}
}

// scaleNV12 scales src down to w×h (even) into dst by area average.
func scaleNV12(dst, src *CameraFrame, w, h int) {
	ensureNV12(dst, w, h)
	dst.FullRange, dst.At = src.FullRange, src.At
	w, h = dst.W, dst.H
	sw, sh := src.W, src.H
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, max(y*sh/h+1, (y+1)*sh/h)
		out := dst.Y[y*w:]
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, max(x*sw/w+1, (x+1)*sw/w)
			var t, n int
			for yy := y0; yy < y1; yy++ {
				row := src.Y[yy*src.YStride:]
				for xx := x0; xx < x1; xx++ {
					t += int(row[xx])
					n++
				}
			}
			out[x] = uint8(t / n)
		}
	}
	cw, ch, scw, sch := w/2, h/2, sw/2, sh/2
	for y := 0; y < ch; y++ {
		y0, y1 := y*sch/ch, max(y*sch/ch+1, (y+1)*sch/ch)
		out := dst.UV[y*w:]
		for x := 0; x < cw; x++ {
			x0, x1 := x*scw/cw, max(x*scw/cw+1, (x+1)*scw/cw)
			var u, v, n int
			for yy := y0; yy < y1; yy++ {
				row := src.UV[yy*src.UVStride:]
				for xx := x0; xx < x1; xx++ {
					u += int(row[2*xx])
					v += int(row[2*xx+1])
					n++
				}
			}
			out[2*x], out[2*x+1] = uint8(u/n), uint8(v/n)
		}
	}
}

// nv12ToRGBA converts f into dst (f's size divided by step).
func nv12ToRGBA(dst *image.RGBA, f *CameraFrame, step int) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	for y := 0; y < h; y++ {
		sy := y * step
		yrow := f.Y[sy*f.YStride:]
		crow := f.UV[(sy/2)*f.UVStride:]
		pix := dst.Pix[y*dst.Stride:]
		for x := 0; x < w; x++ {
			sx := x * step
			yy := int32(yrow[sx])
			cb := int32(crow[sx&^1]) - 128
			cr := int32(crow[sx|1]) - 128
			var r, g, b int32
			if f.FullRange {
				yy <<= 16
				r = (yy + 91881*cr) >> 16
				g = (yy - 22554*cb - 46802*cr) >> 16
				b = (yy + 116130*cb) >> 16
			} else {
				yy = (yy - 16) * 76309
				r = (yy + 104597*cr) >> 16
				g = (yy - 25675*cb - 53279*cr) >> 16
				b = (yy + 132201*cb) >> 16
			}
			i := x * 4
			pix[i] = clamp8(r)
			pix[i+1] = clamp8(g)
			pix[i+2] = clamp8(b)
			pix[i+3] = 0xff
		}
	}
}

// capSize is the capture size ContentShareManager asks for: at most
// shareMaxSide on each side (maxWidth/maxHeight 2560), aspect kept, even.
func capSize(w, h int) (int, int) {
	cw, ch := fitIn(w, h, image.Pt(shareMaxSide, shareMaxSide))
	return max(16, cw&^1), max(16, ch&^1)
}
