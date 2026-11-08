package theme

// HCT tonal ramps for Slack's custom-colour themes (users.prefs ia_theme with
// useCustomHex). Slack builds each custom colour's 12 shades with Google's
// Material Color Utilities: hct_Hct.fromInt(argbFromHex(hex)), then
// TonalPalette.fromHct, then .tone(t). This file ports the subset of that code
// Slack ships (webpack module "t+RV" in gantry-v2-vendors) that its wrappers
// generatePaletteFromHex and getClosestCustomPaletteShadeFromCustomHex reach:
// colour utils, ViewingConditions.DEFAULT, Cam16.fromInt, HctSolver.solveToInt
// and Hct.
//
// The goal is byte-identical output, so the arithmetic keeps the JS operation
// order. Products of two inexact literals (1/2.4, 1/.42, 1+100*.007, ...)
// stay as runtime float64 ops via the vars below. Go folds untyped constant
// expressions exactly and would round differently from JS.
//
// In this build TonalPalette.fromHct(h) is new TonalPalette(h.hue, h.chroma, h).
// The key colour is the input Hct itself (the KeyColor search only runs in
// fromHueAndChroma), so keyColor.internalTone is the hex's own L*.

import (
	"image/color"
	"math"
)

// Runtime copies of JS literals whose combinations must round like JS.
var (
	jsPi    = math.Pi
	js2p4   = 2.4
	js042   = 0.42
	js09    = 0.9
	js36    = 3.6
	js1over = 1.0
)

// customTonesLight are Slack's light (and light-inverted-sidebar) tones for
// labels 0,5,10,20,...,100. Dark is the same list reversed.
var customTonesLight = [12]float64{97, 91, 85, 78, 69, 57, 45, 38, 28, 19, 11, 4}

func customTones(dark bool) [12]float64 {
	if !dark {
		return customTonesLight
	}
	var t [12]float64
	for i, v := range customTonesLight {
		t[11-i] = v
	}
	return t
}

// CustomRamp returns the 12 shades Slack's generatePaletteFromHex produces for
// a custom hex, in label order 0,5,10,20,...,100. dark selects the dark-mode
// tone table; light and light-inverted-sidebar share the light table. Alpha of
// hex is ignored; results are opaque.
func CustomRamp(hex color.NRGBA, dark bool) [12]color.NRGBA {
	h := hctFromArgb(argbOf(hex))
	var out [12]color.NRGBA
	for i, t := range customTones(dark) {
		a := hctSolveToInt(h.hue, h.chroma, t)
		out[i] = color.NRGBA{R: uint8(a >> 16), G: uint8(a >> 8), B: uint8(a), A: 0xff}
	}
	return out
}

// CustomKeyShade is getClosestCustomPaletteShadeFromCustomHex: the index 0..11
// (label order) of the shade whose table tone is nearest the hex's HCT tone.
// Ties keep the earlier label, as Slack's strict < over Object.entries does.
func CustomKeyShade(hex color.NRGBA, dark bool) int {
	tone := HexTone(hex)
	best, bestD := 0, math.Inf(1)
	for i, t := range customTones(dark) {
		if d := math.Abs(t - tone); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// HexTone is the HCT tone (CIE L*) of hex. Slack picks dark content on custom
// colours when tone >= 66.
func HexTone(hex color.NRGBA) float64 {
	return lstarFromArgb(argbOf(hex))
}

func argbOf(c color.NRGBA) uint32 {
	return 0xff000000 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

// --- math utils ---

func signum(x float64) float64 {
	if x < 0 {
		return -1
	}
	if x == 0 {
		return 0
	}
	return 1
}

// jsRound is Math.round: nearest, ties toward +Inf.
func jsRound(x float64) float64 {
	f := math.Floor(x)
	if x-f >= 0.5 {
		f++
	}
	return f
}

func sanitizeDegreesDouble(d float64) float64 {
	d = math.Mod(d, 360)
	if d < 0 {
		d += 360
	}
	return d
}

func matrixMultiply(v [3]float64, m *[3][3]float64) [3]float64 {
	return [3]float64{
		v[0]*m[0][0] + v[1]*m[0][1] + v[2]*m[0][2],
		v[0]*m[1][0] + v[1]*m[1][1] + v[2]*m[1][2],
		v[0]*m[2][0] + v[1]*m[2][1] + v[2]*m[2][2],
	}
}

// --- color utils ---

var srgbToXyz = [3][3]float64{
	{0.41233895, 0.35762064, 0.18051042},
	{0.2126, 0.7152, 0.0722},
	{0.01932141, 0.11916382, 0.95034478},
}

var xyzToSrgb = [3][3]float64{
	{3.2413774792388685, -1.5376652402851851, -0.49885366846268053},
	{-0.9691452513005321, 1.8758853451067872, 0.04156585616912061},
	{0.05562093689691305, -0.20395524564742123, 1.0571799111220335},
}

var whitePointD65 = [3]float64{95.047, 100, 108.883}

func argbFromRgb(r, g, b int) uint32 {
	return 0xff000000 | uint32(r&255)<<16 | uint32(g&255)<<8 | uint32(b&255)
}

func argbFromLinrgb(l [3]float64) uint32 {
	return argbFromRgb(delinearized(l[0]), delinearized(l[1]), delinearized(l[2]))
}

func argbFromXyz(x, y, z float64) uint32 {
	m := &xyzToSrgb
	r := m[0][0]*x + m[0][1]*y + m[0][2]*z
	g := m[1][0]*x + m[1][1]*y + m[1][2]*z
	b := m[2][0]*x + m[2][1]*y + m[2][2]*z
	return argbFromRgb(delinearized(r), delinearized(g), delinearized(b))
}

func xyzFromArgb(a uint32) [3]float64 {
	return matrixMultiply([3]float64{
		linearized(int(a >> 16 & 255)),
		linearized(int(a >> 8 & 255)),
		linearized(int(a & 255)),
	}, &srgbToXyz)
}

func argbFromLstar(l float64) uint32 {
	c := delinearized(yFromLstar(l))
	return argbFromRgb(c, c, c)
}

func lstarFromArgb(a uint32) float64 {
	return 116*labF(xyzFromArgb(a)[1]/100) - 16
}

func yFromLstar(l float64) float64 {
	return 100 * labInvf((l+16)/116)
}

func lstarFromY(y float64) float64 {
	return 116*labF(y/100) - 16
}

func linearized(c int) float64 {
	n := float64(c) / 255
	if n <= 0.040449936 {
		return n / 12.92 * 100
	}
	return 100 * math.Pow((n+0.055)/1.055, js2p4)
}

func delinearized(l float64) int {
	n := l / 100
	var d float64
	if n <= 0.0031308 {
		d = 12.92 * n
	} else {
		d = 1.055*math.Pow(n, js1over/js2p4) - 0.055
	}
	v := jsRound(255 * d)
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return int(v)
}

func labF(t float64) float64 {
	if t > 216.0/24389.0 {
		return math.Pow(t, 1.0/3.0)
	}
	return (24389.0/27.0*t + 16) / 116
}

func labInvf(ft float64) float64 {
	ft3 := ft * ft * ft
	if ft3 > 216.0/24389.0 {
		return ft3
	}
	return (116*ft - 16) / (24389.0 / 27.0)
}

// --- ViewingConditions ---

type viewingConditions struct {
	n, aw, nbb, ncb, c, nc float64
	rgbD                   [3]float64
	fl, fLRoot, z          float64
}

var defaultVC = makeViewingConditions(whitePointD65, 200/jsPi*yFromLstar(50)/100, 50, 2, false)

func makeViewingConditions(wp [3]float64, adaptingLuminance, backgroundLstar, surround float64, discounting bool) viewingConditions {
	H := adaptingLuminance
	rW := 0.401288*wp[0] + 0.650173*wp[1] + -0.051461*wp[2]
	gW := -0.250268*wp[0] + 1.204414*wp[1] + 0.045854*wp[2]
	bW := -0.002079*wp[0] + 0.048952*wp[1] + 0.953127*wp[2]
	f := 0.8 + surround/10
	var c float64
	if f >= 0.9 {
		j := (f - 0.9) * 10
		c = (1-j)*0.59 + 0.69*j
	} else {
		j := (f - 0.8) * 10
		c = (1-j)*0.525 + 0.59*j
	}
	var d float64
	if discounting {
		d = 1
	} else {
		d = f * (1 - js1over/js36*math.Exp((-H-42)/92))
	}
	if d > 1 {
		d = 1
	} else if d < 0 {
		d = 0
	}
	rgbD := [3]float64{100/rW*d + 1 - d, 100/gW*d + 1 - d, 100/bW*d + 1 - d}
	k := 1 / (5*H + 1)
	k4 := k * k * k * k
	k4F := 1 - k4
	fl := k4*H + 0.1*k4F*k4F*math.Cbrt(5*H)
	n := yFromLstar(backgroundLstar) / wp[1]
	z := 1.48 + math.Sqrt(n)
	nbb := 0.725 / math.Pow(n, 0.2)
	rgbAF := [3]float64{
		math.Pow(fl*rgbD[0]*rW/100, js042),
		math.Pow(fl*rgbD[1]*gW/100, js042),
		math.Pow(fl*rgbD[2]*bW/100, js042),
	}
	rgbA := [3]float64{
		400 * rgbAF[0] / (rgbAF[0] + 27.13),
		400 * rgbAF[1] / (rgbAF[1] + 27.13),
		400 * rgbAF[2] / (rgbAF[2] + 27.13),
	}
	aw := (2*rgbA[0] + rgbA[1] + 0.05*rgbA[2]) * nbb
	return viewingConditions{
		n: n, aw: aw, nbb: nbb, ncb: nbb, c: c, nc: f, rgbD: rgbD,
		fl: fl, fLRoot: math.Pow(fl, 0.25), z: z,
	}
}

// --- Cam16 (hue and chroma only; that's all Hct reads) ---

func cam16HueChroma(a uint32) (hue, chroma float64) {
	vc := &defaultVC
	R := linearized(int(a & 0xff0000 >> 16))
	G := linearized(int(a & 0xff00 >> 8))
	B := linearized(int(a & 0xff))
	x := 0.41233895*R + 0.35762064*G + 0.18051042*B
	y := 0.2126*R + 0.7152*G + 0.0722*B
	z := 0.01932141*R + 0.11916382*G + 0.95034478*B
	rC := vc.rgbD[0] * (0.401288*x + 0.650173*y - 0.051461*z)
	gC := vc.rgbD[1] * (-0.250268*x + 1.204414*y + 0.045854*z)
	bC := vc.rgbD[2] * (-0.002079*x + 0.048952*y + 0.953127*z)
	rAF := math.Pow(vc.fl*math.Abs(rC)/100, js042)
	gAF := math.Pow(vc.fl*math.Abs(gC)/100, js042)
	bAF := math.Pow(vc.fl*math.Abs(bC)/100, js042)
	rA := 400 * signum(rC) * rAF / (rAF + 27.13)
	gA := 400 * signum(gC) * gAF / (gAF + 27.13)
	bA := 400 * signum(bC) * bAF / (bAF + 27.13)
	aa := (11*rA + -12*gA + bA) / 11
	bb := (rA + gA - 2*bA) / 9
	deg := 180 * math.Atan2(bb, aa) / jsPi
	switch {
	case deg < 0:
		hue = deg + 360
	case deg >= 360:
		hue = deg - 360
	default:
		hue = deg
	}
	j := 100 * math.Pow((40*rA+20*gA+bA)/20*vc.nbb/vc.aw, vc.c*vc.z)
	hp := hue
	if hp < 20.14 {
		hp += 360
	}
	t := math.Pow(5e4/13*(0.25*(math.Cos(hp*jsPi/180+2)+3.8))*vc.nc*vc.ncb*math.Sqrt(aa*aa+bb*bb)/((20*rA+20*gA+21*bA)/20+0.305), js09) *
		math.Pow(1.64-math.Pow(0.29, vc.n), 0.73)
	chroma = t * math.Sqrt(j/100)
	return hue, chroma
}

// --- Hct ---

type hct struct {
	hue, chroma, tone float64
}

func hctFromArgb(a uint32) hct {
	h, c := cam16HueChroma(a)
	return hct{hue: h, chroma: c, tone: lstarFromArgb(a)}
}

// --- HctSolver ---

var scaledDiscountFromLinrgb = [3][3]float64{
	{0.001200833568784504, 0.002389694492170889, 2795742885861124e-19},
	{5891086651375999e-19, 0.0029785502573438758, 3270666104008398e-19},
	{10146692491640572e-20, 5364214359186694e-19, 0.0032979401770712076},
}

var linrgbFromScaledDiscount = [3][3]float64{
	{1373.2198709594231, -1100.4251190754821, -7.278681089101213},
	{-271.815969077903, 559.6580465940733, -32.46047482791194},
	{1.9622899599665666, -57.173814538844006, 308.7233197812385},
}

var yFromLinrgb = [3]float64{0.2126, 0.7152, 0.0722}

func sanitizeRadians(a float64) float64 {
	return math.Mod(a+8*jsPi, 2*jsPi)
}

func trueDelinearized(l float64) float64 {
	n := l / 100
	var d float64
	if n <= 0.0031308 {
		d = 12.92 * n
	} else {
		d = 1.055*math.Pow(n, js1over/js2p4) - 0.055
	}
	return 255 * d
}

func chromaticAdaptation(c float64) float64 {
	af := math.Pow(math.Abs(c), js042)
	return 400 * signum(c) * af / (af + 27.13)
}

func hueOf(linrgb [3]float64) float64 {
	s := matrixMultiply(linrgb, &scaledDiscountFromLinrgb)
	rA := chromaticAdaptation(s[0])
	gA := chromaticAdaptation(s[1])
	bA := chromaticAdaptation(s[2])
	return math.Atan2((rA+gA-2*bA)/9, (11*rA+-12*gA+bA)/11)
}

func areInCyclicOrder(a, b, c float64) bool {
	return sanitizeRadians(b-a) < sanitizeRadians(c-a)
}

func intercept(src, mid, dst float64) float64 {
	return (mid - src) / (dst - src)
}

func lerpPoint(src [3]float64, t float64, dst [3]float64) [3]float64 {
	return [3]float64{
		src[0] + (dst[0]-src[0])*t,
		src[1] + (dst[1]-src[1])*t,
		src[2] + (dst[2]-src[2])*t,
	}
}

func setCoordinate(src [3]float64, coord float64, dst [3]float64, axis int) [3]float64 {
	return lerpPoint(src, intercept(src[axis], coord, dst[axis]), dst)
}

func isBounded(x float64) bool { return 0 <= x && x <= 100 }

func nthVertex(y float64, n int) [3]float64 {
	kR, kG, kB := yFromLinrgb[0], yFromLinrgb[1], yFromLinrgb[2]
	coordA := 0.0
	if n%4 > 1 {
		coordA = 100
	}
	coordB := 0.0
	if n%2 != 0 {
		coordB = 100
	}
	none := [3]float64{-1, -1, -1}
	switch {
	case n < 4:
		r := (y - coordA*kG - coordB*kB) / kR
		if isBounded(r) {
			return [3]float64{r, coordA, coordB}
		}
		return none
	case n < 8:
		g := (y - coordB*kR - coordA*kB) / kG
		if isBounded(g) {
			return [3]float64{coordB, g, coordA}
		}
		return none
	default:
		b := (y - coordA*kR - coordB*kG) / kB
		if isBounded(b) {
			return [3]float64{coordA, coordB, b}
		}
		return none
	}
}

func bisectToSegment(y, targetHue float64) (left, right [3]float64) {
	left = [3]float64{-1, -1, -1}
	right = left
	leftHue, rightHue := 0.0, 0.0
	initialized, uncut := false, true
	for n := 0; n < 12; n++ {
		mid := nthVertex(y, n)
		if mid[0] < 0 {
			continue
		}
		midHue := hueOf(mid)
		if !initialized {
			left, right = mid, mid
			leftHue, rightHue = midHue, midHue
			initialized = true
			continue
		}
		if uncut || areInCyclicOrder(leftHue, midHue, rightHue) {
			uncut = false
			if areInCyclicOrder(leftHue, targetHue, midHue) {
				right, rightHue = mid, midHue
			} else {
				left, leftHue = mid, midHue
			}
		}
	}
	return left, right
}

func midpoint(a, b [3]float64) [3]float64 {
	return [3]float64{(a[0] + b[0]) / 2, (a[1] + b[1]) / 2, (a[2] + b[2]) / 2}
}

func criticalPlaneBelow(x float64) float64 { return math.Floor(x - 0.5) }
func criticalPlaneAbove(x float64) float64 { return math.Ceil(x - 0.5) }

func bisectToLimit(y, targetHue float64) [3]float64 {
	left, right := bisectToSegment(y, targetHue)
	leftHue := hueOf(left)
	for axis := 0; axis < 3; axis++ {
		if left[axis] == right[axis] {
			continue
		}
		var lPlane, rPlane float64
		if left[axis] < right[axis] {
			lPlane = criticalPlaneBelow(trueDelinearized(left[axis]))
			rPlane = criticalPlaneAbove(trueDelinearized(right[axis]))
		} else {
			lPlane = criticalPlaneAbove(trueDelinearized(left[axis]))
			rPlane = criticalPlaneBelow(trueDelinearized(right[axis]))
		}
		for i := 0; i < 8; i++ {
			if math.Abs(rPlane-lPlane) <= 1 {
				break
			}
			mPlane := math.Floor((lPlane + rPlane) / 2)
			midPlaneCoord := criticalPlanes[int(mPlane)]
			mid := setCoordinate(left, midPlaneCoord, right, axis)
			midHue := hueOf(mid)
			if areInCyclicOrder(leftHue, targetHue, midHue) {
				right, rPlane = mid, mPlane
			} else {
				left, leftHue, lPlane = mid, midHue, mPlane
			}
		}
	}
	return midpoint(left, right)
}

func inverseChromaticAdaptation(adapted float64) float64 {
	a := math.Abs(adapted)
	base := math.Max(0, 27.13*a/(400-a))
	return signum(adapted) * math.Pow(base, js1over/js042)
}

func findResultByJ(hueRadians, chroma, y float64) uint32 {
	j := 11 * math.Sqrt(y)
	vc := &defaultVC
	tInnerCoeff := 1 / math.Pow(1.64-math.Pow(0.29, vc.n), 0.73)
	eHue := 0.25 * (math.Cos(hueRadians+2) + 3.8)
	p1 := 5e4 / 13 * eHue * vc.nc * vc.ncb
	hSin, hCos := math.Sin(hueRadians), math.Cos(hueRadians)
	for iter := 0; iter < 5; iter++ {
		jNorm := j / 100
		var a0 float64
		if chroma != 0 && j != 0 {
			a0 = chroma / math.Sqrt(jNorm)
		}
		t := math.Pow(a0*tInnerCoeff, js1over/js09)
		ac := vc.aw * math.Pow(jNorm, 1/vc.c/vc.z)
		p2 := ac / vc.nbb
		gamma := 23 * (p2 + 0.305) * t / (23*p1 + 11*t*hCos + 108*t*hSin)
		a := gamma * hCos
		b := gamma * hSin
		rA := (460*p2 + 451*a + 288*b) / 1403
		gA := (460*p2 - 891*a - 261*b) / 1403
		bA := (460*p2 - 220*a - 6300*b) / 1403
		lin := matrixMultiply([3]float64{
			inverseChromaticAdaptation(rA),
			inverseChromaticAdaptation(gA),
			inverseChromaticAdaptation(bA),
		}, &linrgbFromScaledDiscount)
		if lin[0] < 0 || lin[1] < 0 || lin[2] < 0 {
			break
		}
		fnj := yFromLinrgb[0]*lin[0] + yFromLinrgb[1]*lin[1] + yFromLinrgb[2]*lin[2]
		if fnj <= 0 {
			break
		}
		if iter == 4 || math.Abs(fnj-y) < 0.002 {
			if lin[0] > 100.01 || lin[1] > 100.01 || lin[2] > 100.01 {
				return 0
			}
			return argbFromLinrgb(lin)
		}
		j -= (fnj - y) * j / (2 * fnj)
	}
	return 0
}

func hctSolveToInt(hueDegrees, chroma, lstar float64) uint32 {
	if chroma < 1e-4 || lstar < 1e-4 || lstar > 99.9999 {
		return argbFromLstar(lstar)
	}
	hueDegrees = sanitizeDegreesDouble(hueDegrees)
	hueRadians := hueDegrees / 180 * jsPi
	y := yFromLstar(lstar)
	if a := findResultByJ(hueRadians, chroma, y); a != 0 {
		return a
	}
	return argbFromLinrgb(bisectToLimit(y, hueRadians))
}

var criticalPlanes = [255]float64{
	0.015176349177441876, 0.045529047532325624, 0.07588174588720938, 0.10623444424209313, 0.13658714259697685, 0.16693984095186062, 0.19729253930674434, 0.2276452376616281, 0.2579979360165119, 0.28835063437139563, 0.3188300904430532, 0.350925934958123, 0.3848314933096426, 0.42057480301049466, 0.458183274052838, 0.4976837250274023, 0.5391024159806381, 0.5824650784040898, 0.6277969426914107, 0.6751227633498623, 0.7244668422128921, 0.775853049866786, 0.829304845476233, 0.8848452951698498, 0.942497089126609, 1.0022825574869039, 1.0642236851973577, 1.1283421258858297, 1.1946592148522128, 1.2631959812511864, 1.3339731595349034, 1.407011200216447, 1.4823302800086415, 1.5599503113873272, 1.6398909516233677, 1.7221716113234105, 1.8068114625156377, 1.8938294463134073, 1.9832442801866852, 2.075074464868551, 2.1693382909216234, 2.2660538449872063, 2.36523901573795, 2.4669114995532007, 2.5710888059345764, 2.6777882626779785, 2.7870270208169257, 2.898822059350997, 3.0131901897720907, 3.1301480604002863, 3.2497121605402226, 3.3718988244681087, 3.4967242352587946, 3.624204428461639, 3.754355295633311, 3.887192587735158, 4.022731918402185, 4.160988767090289, 4.301978482107941, 4.445716283538092, 4.592217266055746, 4.741496401646282, 4.893568542229298, 5.048448422192488, 5.20615066083972, 5.3666897647573375, 5.5300801301023865, 5.696336044816294, 5.865471690767354, 6.037501145825082, 6.212438385869475, 6.390297286737924, 6.571091626112461, 6.7548350853498045, 6.941541251256611, 7.131223617812143, 7.323895587840543, 7.5195704746346665, 7.7182615035334345, 7.919981813454504, 8.124744458384042, 8.332562408825165, 8.543448553206703, 8.757415699253682, 8.974476575321063, 9.194643831691977, 9.417930041841839, 9.644347703669503, 9.873909240696694, 10.106627003236781, 10.342513269534024, 10.58158024687427, 10.8238400726681, 11.069304815507364, 11.317986476196008, 11.569896988756009, 11.825048221409341, 12.083451977536606, 12.345119996613247, 12.610063955123938, 12.878295467455942, 13.149826086772048, 13.42466730586372, 13.702830557985108, 13.984327217668513, 14.269168601521828, 14.55736596900856, 14.848930523210871, 15.143873411576273, 15.44220572664832, 15.743938506781891, 16.04908273684337, 16.35764934889634, 16.66964922287304, 16.985093187232053, 17.30399201960269, 17.62635644741625, 17.95219714852476, 18.281524751807332, 18.614349837764564, 18.95068293910138, 19.290534541298456, 19.633915083172692, 19.98083495742689, 20.331304511189067, 20.685334046541502, 21.042933821039977, 21.404114048223256, 21.76888489811322, 22.137256497705877, 22.50923893145328, 22.884842241736916, 23.264076429332462, 23.6469514538663, 24.033477234264016, 24.42366364919083, 24.817520537484558, 25.21505769858089, 25.61628489293138, 26.021211842414342, 26.429848230738664, 26.842203703840827, 27.258287870275353, 27.678110301598522, 28.10168053274597, 28.529008062403893, 28.96010235337422, 29.39497283293396, 29.83362889318845, 30.276079891419332, 30.722335150426627, 31.172403958865512, 31.62629557157785, 32.08401920991837, 32.54558406207592, 33.010999283389665, 33.4802739966603, 33.953417292456834, 34.430438229418264, 34.911345834551085, 35.39614910352207, 35.88485700094671, 36.37747846067349, 36.87402238606382, 37.37449765026789, 37.87891309649659, 38.38727753828926, 38.89959975977785, 39.41588851594697, 39.93615253289054, 40.460400508064545, 40.98864111053629, 41.520882981230194, 42.05713473317016, 42.597404951718396, 43.141702194811224, 43.6900349931913, 44.24241185063697, 44.798841244188324, 45.35933162437017, 45.92389141541209, 46.49252901546552, 47.065252796817916, 47.64207110610409, 48.22299226451468, 48.808024568002054, 49.3971762874833, 49.9904556690408, 50.587870934119984, 51.189430279724725, 51.79514187861014, 52.40501387947288, 53.0190544071392, 53.637271562750364, 54.259673423945976, 54.88626804504493, 55.517063457223934, 56.15206766869424, 56.79128866487574, 57.43473440856916, 58.08241284012621, 58.734331877617365, 59.39049941699807, 60.05092333227251, 60.715611475655585, 61.38457167773311, 62.057811747619894, 62.7353394731159, 63.417162620860914, 64.10328893648692, 64.79372614476921, 65.48848194977529, 66.18756403501224, 66.89098006357258, 67.59873767827808, 68.31084450182222, 69.02730813691093, 69.74813616640164, 70.47333615344107, 71.20291564160104, 71.93688215501312, 72.67524319850172, 73.41800625771542, 74.16517879925733, 74.9167682708136, 75.67278210128072, 76.43322770089146, 77.1981124613393, 77.96744375590167, 78.74122893956174, 79.51947534912904, 80.30219030335869, 81.08938110306934, 81.88105503125999, 82.67721935322541, 83.4778813166706, 84.28304815182372, 85.09272707154808, 85.90692527145302, 86.72564993000343, 87.54890820862819, 88.3767072518277, 89.2090541872801, 90.04595612594655, 90.88742016217518, 91.73345337380438, 92.58406282226491, 93.43925555268066, 94.29903859396902, 95.16341895893969, 96.03240364439274, 96.9059996312159, 97.78421388448044, 98.6670533535366, 99.55452497210776,
}
