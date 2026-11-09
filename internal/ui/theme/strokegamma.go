package theme

import (
	"image/color"
	"math"
	"sync"
)

// StrokeGamma is the width, in device pixels, to add to an antialiased
// stroke of color c so it carries the ink a browser gives the same stroke.
//
// Gio blends coverage in linear light; Chrome blends in sRGB. Interior
// pixels come out the same, but an edge pixel with coverage a reads as
// enc(a·lin(ink) + (1-a)·lin(bg)) instead of a·ink + (1-a)·bg. For
// light-on-dark that is brighter (strokes look heavy), for dark-on-light
// dimmer (strokes look thin). Averaged over the edge positions an edge
// gains F-½ px of ink, F = ∫₀¹ (enc(a·lin(ink)+(1-a)·lin(bg))-bg)/(ink-bg) da,
// so a stroke's two edges are matched by 2·(½-F) px of width. Whatever
// the scale, the amount is the same in pixels: #b9babd on #222529 −0.27 px,
// #454447 on #f8f8f8 +0.23 px, #1d1c1d on white +0.32 px. That matches a
// coverage-exact rasterizer within 0.02 px on stroked 20×20 icons at 18 and
// 36 px (the icons pixel test checks the result against Slack's SVGs).
//
// The background isn't known where icons draw, so it is guessed from the
// ink the way the label fade does (lightTextColor): ink with luma ≥ 0.6 is
// taken to sit on Slack's dark surface (#1a1d21), darker ink on white.
// CLACK_ICON_GAMMA=0 turns it off for A/B comparisons.
func StrokeGamma(c color.NRGBA) float32 {
	if strokeGammaOff || c.A == 0 {
		return 0
	}
	sgOnce.Do(sgBuild)
	y := (0.299*float32(c.R) + 0.587*float32(c.G) + 0.114*float32(c.B)) / 255
	a := float32(c.A) / 255
	// bilinear in (luma, alpha)
	fy, fa := y*sgN, a*sgA
	iy, ia := min(int(fy), sgN-1), min(int(fa), sgA-1)
	ty, ta := fy-float32(iy), fa-float32(ia)
	at := func(i, j int) float32 { return sgTab[i*(sgA+1)+j] }
	lo := at(iy, ia)*(1-ta) + at(iy, ia+1)*ta
	hi := at(iy+1, ia)*(1-ta) + at(iy+1, ia+1)*ta
	// the background guess flips at luma 0.6: don't interpolate across it
	if (float32(iy)/sgN >= sgLight) != (float32(iy+1)/sgN >= sgLight) {
		if y >= sgLight {
			return hi
		}
		return lo
	}
	return lo*(1-ty) + hi*ty
}

var strokeGammaOff = func() bool { v := envFloat("CLACK_ICON_GAMMA", 1); return v == 0 }()

const (
	sgN     = 80 // luma steps
	sgA     = 16 // alpha steps
	sgLight = 0.6
)

var (
	sgOnce sync.Once
	sgTab  [(sgN + 1) * (sgA + 1)]float32
)

func sgBuild() {
	for i := 0; i <= sgN; i++ {
		ink := float64(i) / sgN
		bg := 1.0
		if ink >= sgLight {
			bg = 0x1d / 255.0 // #1a1d21's green: Slack's dark surface
		}
		for j := 0; j <= sgA; j++ {
			sgTab[i*(sgA+1)+j] = float32(strokeGammaPx(ink, bg, float64(j)/sgA))
		}
	}
}

// strokeGammaPx is 2·(½-F) for ink over bg (sRGB-encoded greys in 0..1)
// at alpha al.
func strokeGammaPx(ink, bg, al float64) float64 {
	li, lb := sgLin(ink), sgLin(bg)
	full := sgEnc(al*li+(1-al)*lb) - bg
	if math.Abs(full) < 1e-3 || al <= 0 {
		return 0
	}
	const n = 64
	var f float64
	for k := 0; k < n; k++ {
		a := al * (float64(k) + 0.5) / n
		f += (sgEnc(a*li+(1-a)*lb) - bg) / full
	}
	return 2 * (0.5 - f/n)
}

func sgLin(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func sgEnc(l float64) float64 {
	if l <= 0.0031308 {
		return l * 12.92
	}
	return 1.055*math.Pow(l, 1/2.4) - 0.055
}
