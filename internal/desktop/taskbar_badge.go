package desktop

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
)

// The Windows taskbar's unread badge, as Slack desktop 4.53.276 sets it
// (win32 main.bundle.cjs @1171101, SET_BADGE_EPIC and vc):
//
//	0 === count → setOverlayIcon(nativeImage.createEmpty(), "")
//	else png = resources/unreads-badge/unread-badge-resources.json[count > 99 ? 100 : count]
//	     setOverlayIcon(nativeImage.createFromBuffer(base64 png), "You have unread messages")
//
// The JSON is Slack's own file from the win32 app.asar
// (dist/resources/unreads-badge/unread-badge-resources.json, sha256
// 22d76a5d…cd18c1): 101 base64 PNGs, 64×64, index 0 to 99 the count and
// 100 the "+" past 99. Electron's TaskbarHost::SetOverlayIcon
// (shell/browser/ui/win/taskbar_host.cc) then makes the 16×16 icon Windows
// gets: skia::ImageOperations::Resize with RESIZE_LANCZOS3 to 16 wide,
// drawn centred on a transparent 16×16 canvas under an antialiased oval
// clip, and IconUtil::CreateHICONFromSkBitmap copies those premultiplied
// pixels into the icon's 32-bit DIB. BadgeOverlay is that pipeline.

//go:embed unread-badge-resources.json
var badgeJSON []byte

var badgePNGs = sync.OnceValues(func() ([]string, error) { // vu: the file is read once
	var l []string
	if err := json.Unmarshal(badgeJSON, &l); err != nil {
		return nil, err
	}
	if len(l) != 101 {
		return nil, fmt.Errorf("desktop: unread-badge-resources.json has %d images, want 101", len(l))
	}
	return l, nil
})

// BadgePNG is Slack's badge image for count (1 or more; past 99 the "+").
func BadgePNG(count int) ([]byte, error) {
	l, err := badgePNGs()
	if err != nil {
		return nil, err
	}
	if count < 0 {
		count = 0
	}
	return base64.StdEncoding.DecodeString(l[min(count, 100)])
}

// overlayIconSize is TaskbarHost's kOverlayIconSize.
const overlayIconSize = 16

// BadgeOverlay is the 16×16 premultiplied image Electron hands Windows
// for count (nil for 0: the overlay is cleared).
func BadgeOverlay(count int) (*image.RGBA, error) {
	if count <= 0 {
		return nil, nil
	}
	b, err := BadgePNG(count)
	if err != nil {
		return nil, err
	}
	src, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return overlayFromBitmap(premultiplied(src)), nil
}

// premultiplied is the decoded PNG as Skia holds it (kN32, premultiplied
// with SkMulDiv255Round).
func premultiplied(src image.Image) *image.RGBA {
	r := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			c := color.NRGBAModel.Convert(src.At(r.Min.X+x, r.Min.Y+y)).(color.NRGBA)
			i := dst.PixOffset(x, y)
			dst.Pix[i+0] = mulDiv255Round(c.R, c.A)
			dst.Pix[i+1] = mulDiv255Round(c.G, c.A)
			dst.Pix[i+2] = mulDiv255Round(c.B, c.A)
			dst.Pix[i+3] = c.A
		}
	}
	return dst
}

// mulDiv255Round is Skia's SkMulDiv255Round.
func mulDiv255Round(a, b uint8) uint8 {
	p := uint32(a)*uint32(b) + 128
	return uint8((p + p>>8) >> 8)
}

// overlayFromBitmap is TaskbarHost::SetOverlayIcon's drawing: the
// resize, the 16×16 canvas, the oval clip.
func overlayFromBitmap(bm *image.RGBA) *image.RGBA {
	w, h := bm.Rect.Dx(), bm.Rect.Dy()
	// "Maintain aspect ratio on resize, but prefer more square."
	rh := int(math.Ceil(float64(overlayIconSize * (float32(h) / float32(w)))))
	icon := resizeLanczos3(bm, overlayIconSize, rh)
	out := image.NewRGBA(image.Rect(0, 0, overlayIconSize, overlayIconSize))
	yOff := int(math.Ceil(float64(float32(overlayIconSize-rh) / 2)))
	for y := 0; y < rh; y++ {
		dy := y + yOff
		if dy < 0 || dy >= overlayIconSize {
			continue
		}
		for x := 0; x < overlayIconSize; x++ {
			cov := ovalCoverage(x, dy)
			if cov == 0 {
				continue
			}
			s := icon.PixOffset(x, y)
			d := out.PixOffset(x, dy)
			// src-over onto transparent through the clip's coverage
			// (SkAlphaMulQ with SkAlpha255To256)
			scale := uint32(cov) + 1
			for k := 0; k < 4; k++ {
				out.Pix[d+k] = uint8(uint32(icon.Pix[s+k]) * scale >> 8)
			}
		}
	}
	return out
}

// ovalCoverage is the antialiased oval clip's coverage of pixel (x, y)
// on the 16×16 canvas, 0 to 255: SkRRect::MakeOval(0, 0, 16, 16) with
// clipRRect(…, true). Skia's rasterizer works out partial coverage with
// 4× vertical supersampling; this samples 16×16 points in the pixel, which
// agrees with it to a level or two on the rim and exactly elsewhere.
func ovalCoverage(x, y int) uint8 {
	const n = 16
	const r = overlayIconSize / 2.0
	in := 0
	for j := 0; j < n; j++ {
		py := float64(y) + (float64(j)+0.5)/n - r
		for i := 0; i < n; i++ {
			px := float64(x) + (float64(i)+0.5)/n - r
			if px*px+py*py <= r*r {
				in++
			}
		}
	}
	return uint8((in*255 + n*n/2) / (n * n))
}

// ---- skia::ImageOperations::Resize (skia/ext/image_operations.cc,
// convolver.cc), RESIZE_LANCZOS3 ----

const filterShiftBits = 14 // ConvolutionFilter1D::kShiftBits

// filter1D is one output pixel's ConvolutionFilter1D entry.
type filter1D struct {
	offset int
	values []int16
}

// evalLanczos is EvalLanczos(3, x) in float32, as Chromium computes it.
func evalLanczos(size int, x float32) float32 {
	fs := float32(size)
	if x <= -fs || x >= fs {
		return 0
	}
	const eps = 1.1920929e-07 // numeric_limits<float>::epsilon
	if x > -eps && x < eps {
		return 1
	}
	xpi := x * float32(math.Pi)
	return float32(math.Sin(float64(xpi))) / xpi * float32(math.Sin(float64(xpi/fs))) / (xpi / fs)
}

// computeFilters is ResizeFilter::ComputeFilters for a whole axis.
func computeFilters(srcSize, destSize int) []filter1D {
	scale := float32(destSize) / float32(srcSize)
	clamped := min(1, scale)
	srcSupport := float32(3) / clamped // GetFilterSupport(LANCZOS3) / clamped_scale
	invScale := 1 / scale
	out := make([]filter1D, destSize)
	for i := 0; i < destSize; i++ {
		srcPixel := (float32(i) + 0.5) * invScale
		begin := max(0, int(math.Floor(float64(srcPixel-srcSupport))))
		end := min(srcSize-1, int(math.Ceil(float64(srcPixel+srcSupport))))
		vals := make([]float32, 0, end+1-begin)
		var sum float32
		for p := begin; p <= end; p++ {
			dist := (float32(p) + 0.5 - srcPixel) * clamped
			v := evalLanczos(3, dist)
			vals = append(vals, v)
			sum += v
		}
		fixed := make([]int16, len(vals))
		var fixedSum int16
		for k, v := range vals {
			fixed[k] = int16(v / sum * (1 << filterShiftBits)) // FloatToFixed: a truncating cast
			fixedSum += fixed[k]
		}
		fixed[len(fixed)/2] += int16(1<<filterShiftBits) - fixedSum // the leftovers go to the middle
		out[i] = filter1D{offset: begin, values: fixed}
	}
	return out
}

// clampTo8 is convolver.cc's ClampTo8.
func clampTo8(a int32) uint8 {
	switch {
	case a < 0:
		return 0
	case a > 255:
		return 255
	}
	return uint8(a)
}

// resizeLanczos3 is BGRAConvolve2D over premultiplied pixels with alpha:
// each source row convolved horizontally into 8-bit rows, then each
// output row vertically, its alpha raised to its largest colour channel.
func resizeLanczos3(src *image.RGBA, dw, dh int) *image.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	fx, fy := computeFilters(sw, dw), computeFilters(sh, dh)
	rows := make([][]uint8, sh)
	for y := 0; y < sh; y++ {
		row := make([]uint8, dw*4)
		in := src.Pix[y*src.Stride:]
		for ox, f := range fx {
			var acc [4]int32
			for k, v := range f.values {
				p := (f.offset + k) * 4
				for c := 0; c < 4; c++ {
					acc[c] += int32(v) * int32(in[p+c])
				}
			}
			for c := 0; c < 4; c++ {
				row[ox*4+c] = clampTo8(acc[c] >> filterShiftBits)
			}
		}
		rows[y] = row
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for oy, f := range fy {
		out := dst.Pix[oy*dst.Stride:]
		for x := 0; x < dw; x++ {
			var acc [4]int32
			for k, v := range f.values {
				r := rows[f.offset+k]
				for c := 0; c < 4; c++ {
					acc[c] += int32(v) * int32(r[x*4+c])
				}
			}
			var px [4]uint8
			for c := 0; c < 4; c++ {
				px[c] = clampTo8(acc[c] >> filterShiftBits)
			}
			if m := max(px[0], px[1], px[2]); px[3] < m {
				px[3] = m
			}
			copy(out[x*4:], px[:])
		}
	}
	return dst
}
