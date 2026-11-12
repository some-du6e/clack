package theme

import (
	"image/color"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Text ink: how glyph coverage turns into pixels, matched to the browser
// Slack desktop draws its text with on each platform.
//
// Slack desktop is Chromium (4.53: Electron 45, Chrome 156). Chromium draws
// glyph masks through Skia, which "preblends" every mask with a table keyed
// by the text colour's luminance before an sRGB-space blend
// (src/core/SkMaskGamma.cpp, SkTMaskGamma_build_correcting_lut). The table
// adds contrast,
//
//	srca = a + (1-a)·contrast·toLuma(1-src)·a        (apply_contrast)
//
// (src: the colour's luminance; 1-src: the destination Skia guesses,
// "the perceptual inverse"), then pre-distorts srca so that the sRGB-space
// blit lands where a linear-light blend with coverage srca would ("Undo what
// the blit blend will do"). Gio blends in linear light already, so that
// second half is the identity here and the table comes down to
// apply_contrast. And apply_contrast is what a second paint of the same
// coverage at alpha k = contrast·toLuma(1-src) gives in Gio's linear "over"
// blend: a + k·a·(1-a). Both paints sit under one clip, so the GPU renderer
// stencils the glyph run's coverage once and covers it twice
// (gioui.org/gpu: every PaintOp under a clip becomes an imageOp on the
// clip's pathOp); shapes and images aren't touched.
//
// Per platform (Chromium 156.0.8078.49, skia/BUILD.gn):
//   - Windows: SK_GAMMA_SRGB, SK_GAMMA_CONTRAST=1.0, no SK_GAMMA_EXPONENT,
//     so gamma 0 = sRGB (Skia's include/core/SkTypes.h default).
//     ui/gfx/font_render_params_win.cc passes those on
//     (skia::LegacyDisplayGlobals::SetCachedParams); DirectWrite's values
//     replace them only when the ClearType tuner's
//     HKCU\Software\Microsoft\Avalon.Graphics\DISPLAYn key exists
//     (ui/gfx/font_util_win.cc GetTextParameters). Grayscale and ClearType
//     masks both go through the table (src/ports/SkScalerContext_win_dw.cpp
//     generateDWImage: RGBToA8<true>, RGBToLcd16<true>).
//   - Linux: SK_GAMMA_EXPONENT=1.2, SK_GAMMA_CONTRAST=0.2, but grayscale
//     FreeType masks skip the table: SkTypeface_FreeType::onFilterRec calls
//     rec->ignorePreBlend() for non-LCD masks unless SK_GAMMA_APPLY_TO_A8,
//     which Chromium defines for Android only.
//   - macOS: SK_GAMMA_SRGB, SK_GAMMA_CONTRAST=0.0.
//
// One thing the table can't know is the real background: it is built for
// the guessed one (1-src), and Chrome then blits it over whatever is
// there. For dark ink on white (light mode's panes, menus and dialogs)
// that guess is grey for grey text, and the sRGB-space blit over white
// inks it much more than a linear blend would: #616061 timestamps came out
// at 87% of Chrome's ink with Skia's own k. So in light mode dark ink
// (luma < 0.6) gets the k that lands Gio's blend over white where Chrome's
// blit of the table over white does (whiteContrast), never less than
// Skia's. Light ink keeps Skia's k: over a background darker than Skia's
// guess Chrome's light text comes out lighter than a linear blend, which a
// second paint can't take away, and thinning the outline costs stem
// darkness (measured).
//
// Clack's first model, lightTextColor's alpha fade for light text and
// EmboldenFor's outline growth for dark text, was tuned by ink count
// against Chrome on Linux. Against Chrome on Windows (font-shots'
// text-render-windows job) its faded light text keeps Chrome's total ink
// but the cores of the strokes come out 0.09-0.2 dimmer at every scale,
// which reads as washed out, so Windows draws with Skia's table instead.

// TextModel is how glyph runs are inked.
type TextModel uint32

const (
	// TextModelLegacy is Clack's Linux-tuned model: light text faded
	// (lightTextColor), dark text emboldened (EmboldenFor). Linux and macOS.
	TextModelLegacy TextModel = iota
	// TextModelSkiaWin is Chromium on Windows: no fade or embolden, Skia's
	// mask gamma with SK_GAMMA_CONTRAST 1.0 and sRGB gamma.
	TextModelSkiaWin
)

// skiaWinContrast is SK_GAMMA_CONTRAST in Chromium's Windows build
// (skia/BUILD.gn, 156.0.8078.49).
const skiaWinContrast = 1.0

var textModel atomic.Uint32

func init() { textModel.Store(uint32(defaultTextModel(runtime.GOOS, os.Getenv("CLACK_TEXT_MODEL")))) }

// defaultTextModel: Skia's Windows model on Windows, the legacy one
// elsewhere; CLACK_TEXT_MODEL=skia or =legacy picks one on any platform
// (A/B comparisons).
func defaultTextModel(goos, env string) TextModel {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "skia", "windows", "win":
		return TextModelSkiaWin
	case "legacy", "linux":
		return TextModelLegacy
	}
	if goos == "windows" {
		return TextModelSkiaWin
	}
	return TextModelLegacy
}

// CurrentTextModel is the model text is drawn with.
func CurrentTextModel() TextModel { return TextModel(textModel.Load()) }

// SetTextModel switches the model (tests and A/B comparisons; call it
// between frames).
func SetTextModel(m TextModel) {
	textModel.Store(uint32(m))
	inkCache.Clear()
}

// LegacyTextInk reports whether text gets the legacy model's fade and
// embolden (TextModelLegacy).
func LegacyTextInk() bool { return CurrentTextModel() == TextModelLegacy }

// GlyphEmbolden is the outline growth (px) a painter should apply when its
// own tuning asks for w: w under the legacy model, none under Skia's
// (richview's Style.Embolden and LightEmbolden, the composer's).
func GlyphEmbolden(w float32) float32 {
	if !LegacyTextInk() {
		return 0
	}
	return w
}

// TextContrast is the contrast glyphs drawn in c get under the current
// model: the k in a + k·a·(1-a). 0 under the legacy model.
func TextContrast(c color.NRGBA) float32 {
	if CurrentTextModel() != TextModelSkiaWin {
		return 0
	}
	if onWhite(c) {
		return whiteContrast(c, skiaWinContrast)
	}
	return skiaContrast(c, skiaWinContrast)
}

// primaryTheme is the main window's theme (SetPrimary): its mode says
// whether dark ink sits on white. Other themes (huddle windows, fallbacks)
// don't change it. primaryDark mirrors its Dark for painters on other
// windows' goroutines.
var (
	primaryTheme atomic.Pointer[Theme]
	primaryDark  atomic.Bool
)

// SetPrimary makes t the theme whose mode the text model follows (the
// app's own; ui.New sets it).
func SetPrimary(t *Theme) {
	primaryTheme.Store(t)
	primaryDark.Store(t != nil && t.Dark)
}

// notePrimaryDark is SetDark's: the primary theme switched modes.
func notePrimaryDark(t *Theme, dark bool) {
	if primaryTheme.Load() == t {
		primaryDark.Store(dark)
	}
}

// onWhite reports whether c is dark ink (luma < 0.6, StrokeGamma's split)
// drawn while the primary theme is light (or there is none).
func onWhite(c color.NRGBA) bool {
	if primaryDark.Load() {
		return false
	}
	return (0.2126*float64(c.R)+0.7152*float64(c.G)+0.0722*float64(c.B))/255 < sgLight
}

// skiaContrast is SkTMaskGamma_build_correcting_lut's adjustedContrast,
// contrast·toLuma(1-src) with sRGB luma (SkSRGBColorSpaceLuminance), for an
// A8 glyph mask drawn in c. src is c's luminance as SkScalerContext reduces
// it: each channel to 3 bits (SkScalerContextRec::setLuminanceColor:
// SkMaskGamma::CanonicalColor, sk_t_scale255<3>; SkScalerContext.h
// "typedef SkTMaskGamma<3, 3, 3> SkMaskGamma"), then
// SkComputeLuminance's (r·54 + g·183 + b·19) >> 8 for A8 masks
// (SkScalerContext::PreprocessRec), then 3 bits again; the table for it is
// built from sk_t_scale255<3> of the top 3 bits (SkTMaskGamma::preBlend).
// The paint's alpha plays no part (setLuminanceColor keeps RGB only).
func skiaContrast(c color.NRGBA, contrast float64) float32 {
	return float32(contrast * sgLin(1-skiaLuminance(c)))
}

func skiaLuminance(c color.NRGBA) float64 {
	cn := func(v uint8) uint8 { b := v >> 5 << 5; return b | b>>3 | b>>6 } // sk_t_scale255<3>(v >> 5)
	lum := (int(cn(c.R))*54 + int(cn(c.G))*183 + int(cn(c.B))*19) >> 8
	return float64(cn(uint8(lum))) / 255
}

// whiteContrast is the k that makes Gio's linear blend of c over white
// land where Chrome's sRGB-space blit of Skia's table (skiaTable) over
// white does, fitted by least squares over the coverage, and no less than
// Skia's own k. Luminance stands in for the channels.
func whiteContrast(c color.NRGBA, contrast float64) float32 {
	k := float64(skiaContrast(c, contrast))
	fg := (0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)) / 255
	const bg = 1.0
	lf, lb := sgLin(fg), sgLin(bg)
	if math.Abs(lf-lb) < 1e-3 {
		return float32(k)
	}
	src := skiaLuminance(c)
	var num, den float64
	for i := 0; i < 64; i++ {
		a := (float64(i) + 0.5) / 64
		r := skiaTable(src, contrast, a)
		want := (sgLin(bg+r*(fg-bg)) - lb) / (lf - lb)
		w := a * (1 - a)
		num += (want - a) * w
		den += w * w
	}
	return float32(min(1, max(k, num/den)))
}

// skiaTable is SkTMaskGamma_build_correcting_lut's entry for coverage a with
// sRGB gamma: the coverage the sRGB-space blit uses.
func skiaTable(src, contrast, a float64) float64 {
	dst := 1 - src
	ls, ld := sgLin(src), sgLin(dst)
	s := a + (1-a)*contrast*ld*a
	if math.Abs(src-dst) < 1.0/256 {
		return s
	}
	return (sgEnc(ls*s+(1-s)*ld) - dst) / (src - dst)
}

// inkCache maps a colour (and model) to its two paints' alphas.
var inkCache sync.Map // uint64 -> [2]uint8

// textAlphas are the alphas of the two paints that ink a glyph run in c:
// c's own alpha and the contrast paint (0: none). For opaque c that is
// (1, k). For translucent c the pair keeps the fully covered pixels at c's
// alpha, 1 - (1-a1)(1-a2) = α, and gets as close to Skia's initial slope
// α(1+k) as two paints allow (α·k·a·(1-a) over translucent ink can't be
// reached exactly in a linear blend), so fades don't jump.
func textAlphas(c color.NRGBA) (a1, a2 uint8) {
	if c.A == 0 || CurrentTextModel() != TextModelSkiaWin {
		return c.A, 0
	}
	white := onWhite(c)
	key := uint64(c.R)<<24 | uint64(c.G)<<16 | uint64(c.B)<<8 | uint64(c.A)
	if white {
		key |= 1 << 40
	}
	if v, ok := inkCache.Load(key); ok {
		p := v.([2]uint8)
		return p[0], p[1]
	}
	k := float64(skiaContrast(c, skiaWinContrast))
	if white {
		k = float64(whiteContrast(c, skiaWinContrast))
	}
	f1, f2 := contrastPaints(float64(c.A)/255, k)
	p := [2]uint8{uint8(f1*255 + 0.5), uint8(f2*255 + 0.5)}
	if p[1] < 5 { // k under 2% moves coverage by at most half a percent: not worth a pass
		p = [2]uint8{c.A, 0}
	}
	inkCache.Store(key, p)
	return p[0], p[1]
}

// contrastPaints solves 1-(1-a1·x)(1-a2·x) ≈ α·(x + k·x·(1-x)) with the
// value at x = 1 held at α: a1+a2 = α(1+k) (the slope) where reachable,
// else the steepest pair, a1 = a2 = 1-√(1-α).
func contrastPaints(alpha, k float64) (a1, a2 float64) {
	if k <= 0 || alpha <= 0 {
		return alpha, 0
	}
	if alpha >= 1 {
		return 1, min(k, 1)
	}
	s := alpha * (1 + k)
	if smax := 2 * (1 - math.Sqrt(1-alpha)); s >= smax {
		return 1 - math.Sqrt(1-alpha), 1 - math.Sqrt(1-alpha)
	}
	// u = 1-a1, v = 1-a2: u+v = 2-s, u·v = 1-α
	b := 2 - s
	d := math.Sqrt(max(0, b*b-4*(1-alpha)))
	u, v := (b-d)/2, (b+d)/2
	return 1 - u, 1 - v
}

// FillText fills a glyph run's outline (already positioned) in c the way
// the current model inks text. Under the legacy model it is exactly
// paint.FillShape(ops, c, clip.Outline{Path: path}.Op()).
func FillText(ops *op.Ops, c color.NRGBA, path clip.PathSpec) {
	defer clip.Outline{Path: path}.Op().Push(ops).Pop()
	a1, a2 := textAlphas(c)
	c.A = a1
	paint.ColorOp{Color: c}.Add(ops)
	paint.PaintOp{}.Add(ops)
	if a2 > 0 {
		c.A = a2
		paint.ColorOp{Color: c}.Add(ops)
		paint.PaintOp{}.Add(ops)
	}
}

// TextMaterial is Material for a widget.Editor's text. The editor paints
// each glyph run as "material, PaintOp" under the run's clip
// (gioui.org/widget's textIterator.paintGlyph) and its caret the same way
// under a rectangle, so a material that paints c and then sets the
// contrast paint's colour makes the editor's own PaintOp the second paint.
// A fully covered pixel (the caret) still ends at c.
func TextMaterial(gtx layout.Context, c color.NRGBA) op.CallOp {
	a1, a2 := textAlphas(c)
	m := op.Record(gtx.Ops)
	if a2 == 0 {
		paint.ColorOp{Color: c}.Add(gtx.Ops)
		return m.Stop()
	}
	c1, c2 := c, c
	c1.A, c2.A = a1, a2
	paint.ColorOp{Color: c1}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	paint.ColorOp{Color: c2}.Add(gtx.Ops)
	return m.Stop()
}
