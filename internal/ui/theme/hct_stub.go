package theme

// TEMPORARY: CIELAB stand-ins for the Material HCT port (hct.go, coming
// from branch prefs-hct). HCT tone is CIELAB L*, so HexTone is exact; the
// ramps hold LCh hue/chroma instead of CAM16's.

import (
	"image/color"
	"math"
)

var hctTonesLight = [12]float64{97, 91, 85, 78, 69, 57, 45, 38, 28, 19, 11, 4}

func lin(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func delin(l float64) uint8 {
	var c float64
	if l <= 0.0031308 {
		c = 12.92 * l
	} else {
		c = 1.055*math.Pow(l, 1/2.4) - 0.055
	}
	return uint8(math.Round(math.Max(0, math.Min(1, c)) * 255))
}

func labF(t float64) float64 {
	if t > 216.0/24389 {
		return math.Cbrt(t)
	}
	return (24389.0/27*t + 16) / 116
}

func labFInv(f float64) float64 {
	if f*f*f > 216.0/24389 {
		return f * f * f
	}
	return (116*f - 16) / (24389.0 / 27)
}

func toLab(c color.NRGBA) (l, a, b float64) {
	r, g, bl := lin(c.R), lin(c.G), lin(c.B)
	x := (0.41233895*r + 0.35762064*g + 0.18051042*bl) / 0.95047
	y := 0.2126*r + 0.7152*g + 0.0722*bl
	z := (0.01932141*r + 0.11916382*g + 0.95034478*bl) / 1.08883
	fx, fy, fz := labF(x), labF(y), labF(z)
	return 116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)
}

func fromLab(l, a, b float64) color.NRGBA {
	fy := (l + 16) / 116
	x, y, z := labFInv(fy+a/500)*0.95047, labFInv(fy), labFInv(fy-b/200)*1.08883
	r := 3.2413774792388685*x - 1.5376652402851851*y - 0.49885366846268053*z
	g := -0.9691452513005321*x + 1.8758853451067872*y + 0.04156585616912061*z
	bl := 0.05562093689691305*x - 0.20395524564742123*y + 1.0571799111220335*z
	return color.NRGBA{R: delin(r), G: delin(g), B: delin(bl), A: 0xff}
}

// HexTone is the colour's HCT tone (CIELAB L*).
func HexTone(c color.NRGBA) float64 { l, _, _ := toLab(c); return l }

// CustomRamp is the custom colour's 12-shade ramp (Slack's label order).
func CustomRamp(c color.NRGBA, dark bool) [12]color.NRGBA {
	_, a, b := toLab(c)
	var out [12]color.NRGBA
	for i := range out {
		t := hctTonesLight[i]
		if dark {
			t = hctTonesLight[11-i]
		}
		out[i] = fromLab(t, a, b)
	}
	return out
}

// CustomKeyShade is the ramp index whose tone is closest to the colour's.
func CustomKeyShade(c color.NRGBA, dark bool) int {
	t := HexTone(c)
	best, bd := 0, math.Inf(1)
	for i := range hctTonesLight {
		tt := hctTonesLight[i]
		if dark {
			tt = hctTonesLight[11-i]
		}
		if d := math.Abs(tt - t); d < bd {
			best, bd = i, d
		}
	}
	return best
}
