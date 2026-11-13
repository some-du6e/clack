package popups

// A popover card's 1px ring is Slack's `box-shadow: 0 0 0 1px …` (the
// ring before the drop shadow in .c-menu, .c-popover and kin), and
// Chromium paints box-shadows differently from borders
// (BoxPainterBase::PaintNormalBoxShadow, read from chromium/chromium):
//
//   - The border box is pixel-snapped (PixelSnappedContouredBorder), but
//     the spread isn't: StyleBuilderConverter::ConvertShadow takes it as
//     ComputeLength<float>, with no ClampLineWidth. One CSS px is the
//     device scale: 1.25 device px at 1.25x, 1.5 at 1.5x, 2 at 2x.
//   - The shadow shape is the snapped box outset by the spread, each
//     radius grown by it (ApplySpreadToShadowShape → corner correction:
//     radius + spread whenever the radius is larger than the spread; a
//     square corner stays square), filled with the border box clipped out.
//   - The fill is antialiased: a fractional outer edge covers its pixel
//     partly (a quarter at 1.25x, half at 1.5x), blended in sRGB like
//     every Chromium fill.
//
// So the ring is whole device pixels plus one partly covered pixel at
// fractional scales, never one device pixel at every scale (the cards
// before polish-1113b), and never snapped like a border (borderW).

import (
	"image"
	"image/color"
	"math"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// ringSpread is a 1 CSS px box-shadow spread in device px: the scale,
// unsnapped (one device pixel with cssBorders off, the "before" shots).
func ringSpread(gtx layout.Context) float32 {
	s := gtx.Metric.PxPerDp
	if s <= 0 || !cssBorders {
		return 1
	}
	return s
}

// cardRing paints a card's 1px box-shadow ring outside r, every corner
// rounded rad (px). ring is the card's Ring (already corrected for Gio's
// linear blending over bg, the card's backdrop: Colors' init).
func cardRing(gtx layout.Context, r image.Rectangle, rad int, ring, bg color.NRGBA) {
	cardRingCorners(gtx, r, [4]int{rad, rad, rad, rad}, ring, bg)
}

// CardRing is cardRing for the other packages' popover cards.
func CardRing(gtx layout.Context, r image.Rectangle, rad int, ring, bg color.NRGBA) {
	cardRing(gtx, r, rad, ring, bg)
}

// cardRingCorners is cardRing with per-corner radii (nw, ne, se, sw).
func cardRingCorners(gtx layout.Context, r image.Rectangle, rad [4]int, ring, bg color.NRGBA) {
	if ring.A == 0 {
		return
	}
	spread := ringSpread(gtx)
	full := int(math.Floor(float64(spread) + 1e-4))
	frac := spread - float32(full)
	if full > 0 {
		ringBand(gtx, r, rad, 0, full, ring)
	}
	if frac > 1e-3 {
		if c := partialRingAlpha(ring, bg, frac); c.A > 0 {
			ringBand(gtx, r, rad, full, 1, c)
		}
	}
}

// ringBand fills the band between r outset by from and by from+w device
// px, the corners' radii grown as much (square corners stay square).
func ringBand(gtx layout.Context, r image.Rectangle, rad [4]int, from, w int, c color.NRGBA) {
	grow := func(by int) [4]float32 {
		var g [4]float32
		for i, v := range rad {
			if v > 0 {
				g[i] = float32(v + by)
			}
		}
		return g
	}
	var p clip.Path
	p.Begin(gtx.Ops)
	addRRect4(&p, r.Inset(-(from + w)), grow(from+w), false)
	addRRect4(&p, r.Inset(-from), grow(from), true)
	paint.FillShape(gtx.Ops, c, clip.Outline{Path: p.End()}.Op())
}

// addRRect4 is addRRect with radii per corner (nw, ne, se, sw).
func addRRect4(p *clip.Path, r image.Rectangle, rad [4]float32, reverse bool) {
	x0, y0, x1, y1 := float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y)
	lim := min((x1-x0)/2, (y1-y0)/2)
	for i := range rad {
		rad[i] = max(0, min(rad[i], lim))
	}
	nw, ne, se, sw := rad[0], rad[1], rad[2], rad[3]
	const k = 0.5523 // bezier circle constant
	if reverse {
		p.MoveTo(pt(x0+nw, y0))
		p.CubeTo(pt(x0+nw-nw*k, y0), pt(x0, y0+nw-nw*k), pt(x0, y0+nw))
		p.LineTo(pt(x0, y1-sw))
		p.CubeTo(pt(x0, y1-sw+sw*k), pt(x0+sw-sw*k, y1), pt(x0+sw, y1))
		p.LineTo(pt(x1-se, y1))
		p.CubeTo(pt(x1-se+se*k, y1), pt(x1, y1-se+se*k), pt(x1, y1-se))
		p.LineTo(pt(x1, y0+ne))
		p.CubeTo(pt(x1, y0+ne-ne*k), pt(x1-ne+ne*k, y0), pt(x1-ne, y0))
		p.Close()
		return
	}
	p.MoveTo(pt(x0+nw, y0))
	p.LineTo(pt(x1-ne, y0))
	p.CubeTo(pt(x1-ne+ne*k, y0), pt(x1, y0+ne-ne*k), pt(x1, y0+ne))
	p.LineTo(pt(x1, y1-se))
	p.CubeTo(pt(x1, y1-se+se*k), pt(x1-se+se*k, y1), pt(x1-se, y1))
	p.LineTo(pt(x0+sw, y1))
	p.CubeTo(pt(x0+sw-sw*k, y1), pt(x0, y1-sw+sw*k), pt(x0, y1-sw))
	p.LineTo(pt(x0, y0+nw))
	p.CubeTo(pt(x0, y0+nw-nw*k), pt(x0+nw-nw*k, y0), pt(x0+nw, y0))
	p.Close()
}

// partialRingAlpha is ring with the alpha that makes Gio's linear blend
// over bg land where Chromium's sRGB blend of a pixel covered frac lands:
// bg + (full − bg) × frac per channel, full being the fully covered
// pixel (ring over bg as Gio draws it).
func partialRingAlpha(ring, bg color.NRGBA, frac float32) color.NRGBA {
	a := float64(ring.A) / 255
	best, bestD := 0.0, 0.0
	for _, ch := range [][2]uint8{{ring.R, bg.R}, {ring.G, bg.G}, {ring.B, bg.B}} {
		lc, lb := srgbToLin(ch[0]), srgbToLin(ch[1])
		d := math.Abs(lc - lb)
		if d < 1e-3 || d <= bestD {
			continue
		}
		full := linToSRGB(lb + (lc-lb)*a) // 0…255, unrounded
		want := float64(ch[1]) + (full-float64(ch[1]))*float64(frac)
		best, bestD = (srgbToLin(uint8(math.Round(want)))-lb)/(lc-lb), d
	}
	if bestD == 0 { // the ring is the backdrop's color: coverage is moot
		best = a * float64(frac)
	}
	ring.A = uint8(math.Round(math.Max(0, math.Min(1, best)) * 255))
	return ring
}

// srgbToLin is an sRGB channel in linear light, 0…1.
func srgbToLin(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// linToSRGB is a linear-light value as an sRGB channel, 0…255 unrounded.
func linToSRGB(l float64) float64 {
	l = math.Max(0, math.Min(1, l))
	if l <= 0.0031308 {
		return l * 12.92 * 255
	}
	return (1.055*math.Pow(l, 1/2.4) - 0.055) * 255
}
