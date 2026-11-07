// Package theme holds yoloslack's look: the Slack 2026 light and dark
// palettes, the embedded Lato typeface, metric constants and small drawing
// helpers shared by every UI package.
//
// A *Theme is used only on the UI goroutine (text.Shaper is not safe for
// concurrent use).
package theme

import (
	"image"
	"os"
	"image/color"
	"math"
	"sync"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/assets/fonts"
)

// Typeface is the font family name registered for the embedded Lato faces.
const Typeface font.Typeface = "Lato"

// Font weights in Gio units (CSS weight minus 400).
const (
	Light   = font.Light  // 300
	Regular = font.Normal // 400
	Bold    = font.Bold   // 700
	Black   = font.Black  // 900
	Medium  = font.Medium // 500, rendered with the nearest Lato face
	Semi    = font.SemiBold
)

// Theme is the shared look. Swap palettes with SetDark; everything reads
// colors through t.C each frame so a toggle repaints instantly.
type Theme struct {
	Dark   bool
	C      Palette
	Shaper *text.Shaper
	// Images, when set (the ui root sets the app's imgcache), lets labels
	// draw unicode emoji as Slack CDN images.
	Images EmojiImages
	// EmojiStyle is Slack's emoji set preference: "apple" (default) or "google".
	EmojiStyle string

	shapers    [2]*text.Shaper // light, dark
	shaperDark bool            // Shaper is the dark-mode one
}

// Alias weights. Gio blends glyph coverage in linear light, so dark text on
// a light background renders thinner than Chrome's Lato 400 and light text
// on a dark background heavier. Instead of emboldening every frame, each
// mode gets a shaper whose "Normal" face is what that mode's dominant text
// needs (Medium for light mode, Regular for dark mode); the other face is
// registered under an alias weight that Text picks by text color.
const (
	wRegularAlias font.Weight = -60 // light-mode shaper: true Regular (light text)
	wMediumAlias  font.Weight = 60  // dark-mode shaper: Medium (dark text)
)

var (
	parseMu sync.Mutex
	parsed  = map[*byte]font.Face{}
)

func parseFace(b []byte) font.Face {
	parseMu.Lock()
	defer parseMu.Unlock()
	if f, ok := parsed[&b[0]]; ok {
		return f
	}
	face, err := opentype.Parse(b)
	if err != nil {
		panic("theme: bad embedded font: " + err.Error())
	}
	parsed[&b[0]] = face
	return face
}

// lightTextFace is the 400 face for light text on dark backgrounds. Gio's
// linear blending makes it ~19% inkier than Chrome (measured on the dark
// sidebar); CLACK_LIGHTTEXT=light swaps in Lato Light for A/B measurement.
func lightTextFace() ([]byte, []byte) {
	if os.Getenv("CLACK_LIGHTTEXT") == "light" {
		return fonts.Light, fonts.LightItalic
	}
	return fonts.Regular, fonts.Italic
}

// FacesFor returns the Lato collection for a mode's shaper.
func FacesFor(dark bool) []font.FontFace {
	type src struct {
		b []byte
		w font.Weight
		s font.Style
	}
	// lightReg is the face light-on-dark text uses for 400 (see lightTextFace).
	lightReg, lightRegIt := lightTextFace()
	normal, normalIt := fonts.Medium, fonts.MediumItalic
	alt, altIt, altW := lightReg, lightRegIt, wRegularAlias
	if dark {
		normal, normalIt = lightReg, lightRegIt
		alt, altIt, altW = fonts.Medium, fonts.MediumItalic, wMediumAlias
	}
	var out []font.FontFace
	for _, f := range []src{
		{normal, font.Normal, font.Regular},
		{alt, altW, font.Regular},
		{fonts.Bold, font.Bold, font.Regular},
		{fonts.Black, font.Black, font.Regular},
		{fonts.Light, font.Light, font.Regular},
		{normalIt, font.Normal, font.Italic},
		{altIt, altW, font.Italic},
		{fonts.BoldItalic, font.Bold, font.Italic},
		{fonts.BlackItalic, font.Black, font.Italic},
		{fonts.LightItalic, font.Light, font.Italic},
	} {
		out = append(out, font.FontFace{Font: font.Font{Typeface: Typeface, Weight: f.w, Style: f.s}, Face: parseFace(f.b)})
	}
	return out
}

// Faces returns the light-mode collection (compatibility).
func Faces() []font.FontFace { return FacesFor(false) }

// New builds a theme with both mode shapers.
func New(dark bool) *Theme {
	t := Bare(dark)
	t.SetShapers(NewShaperFor(false), NewShaperFor(true))
	return t
}

// Bare returns a theme without shapers (call SetShapers before drawing
// text). Lets the app build them concurrently with window creation.
func Bare(dark bool) *Theme {
	t := &Theme{}
	t.SetDark(dark)
	return t
}

// NewShaperFor builds a mode's shaper (Lato + the system font index as a
// fallback for scripts Lato lacks). Safe to call from any goroutine.
func NewShaperFor(dark bool) *text.Shaper {
	return text.NewShaper(text.WithCollection(FacesFor(dark)))
}

// NewShaper is the light-mode shaper (compatibility).
func NewShaper() *text.Shaper { return NewShaperFor(false) }

// SetShapers installs both mode shapers; Shaper follows the mode.
func (t *Theme) SetShapers(light, dark *text.Shaper) {
	t.shapers = [2]*text.Shaper{light, dark}
	shaperThemes.Store(light, t)
	shaperThemes.Store(dark, t)
	t.pickShaper()
}

func (t *Theme) pickShaper() {
	i := 0
	if t.Dark {
		i = 1
	}
	if t.shapers[i] != nil {
		t.Shaper = t.shapers[i]
		t.shaperDark = t.Dark
	}
}

// weightFor maps Normal to the face the text color needs on this shaper.
func (t *Theme) weightFor(f font.Font, c color.NRGBA) font.Font {
	if f.Weight != font.Normal || (f.Typeface != Typeface && f.Typeface != "") {
		return f
	}
	dark := IsDarkText(c)
	if t.shaperDark && dark {
		f.Weight = wMediumAlias
	} else if !t.shaperDark && !dark {
		f.Weight = wRegularAlias
	}
	return f
}

// SetDark switches palettes.
func (t *Theme) SetDark(dark bool) {
	t.Dark = dark
	defer t.pickShaper()
	if dark {
		t.C = Flatten(DarkPalette)
	} else {
		t.C = Flatten(LightPalette)
	}
}

// Over composites fg over an opaque bg in sRGB space, the way browsers (and
// so Slack) blend. Gio blends in linear light, which makes translucent
// colors look roughly twice as strong; drawing pre-composited opaque colors
// keeps us pixel-identical to Slack.
func Over(fg, bg color.NRGBA) color.NRGBA {
	a := float32(fg.A) / 255
	l := func(f, b uint8) uint8 { return uint8(float32(f)*a + float32(b)*(1-a) + .5) }
	return color.NRGBA{R: l(fg.R, bg.R), G: l(fg.G, bg.G), B: l(fg.B, bg.B), A: 0xff}
}

// OnBg pre-mixes a translucent Slack color over the primary background
// (what CSS rgba() over white/#1a1d21 looks like in Chrome).
func (t *Theme) OnBg(c color.NRGBA) color.NRGBA { return Over(c, t.C.Bg) }

// OnHover pre-mixes c over the message-hover background.
func (t *Theme) OnHover(c color.NRGBA) color.NRGBA { return Over(c, t.C.BgHover) }

// OnRaised pre-mixes c over menus/popovers.
func (t *Theme) OnRaised(c color.NRGBA) color.NRGBA { return Over(c, t.C.BgRaised) }

// LinearAlpha adjusts c's alpha so that Gio's linear-light blend of c over
// bg lands on the color a browser's sRGB blend produces (translucent colors
// that must stay translucent: scrims, selection, shadows). From ui-popups.
func LinearAlpha(c, bg color.NRGBA) color.NRGBA {
	if c.A == 0 || c.A == 255 {
		return c
	}
	a := float64(c.A) / 255
	best, bestD := a, 0.0
	for _, ch := range [][2]uint8{{c.R, bg.R}, {c.G, bg.G}, {c.B, bg.B}} {
		lc, lb := srgbLin(ch[0]), srgbLin(ch[1])
		d := math.Abs(lc - lb)
		if d < 1e-3 || d <= bestD {
			continue
		}
		want := float64(ch[0])*a + float64(ch[1])*(1-a)
		lw := srgbLin(uint8(math.Round(want)))
		best, bestD = (lw-lb)/(lc-lb), d
	}
	best = math.Max(0, math.Min(1, best))
	c.A = uint8(math.Round(best * 255))
	return c
}

func srgbLin(v uint8) float64 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// Flatten pre-composites the panel-content colors that Slack defines as
// translucent over the primary background (borders, code, reactions, …).
// Chrome/sidebar colors stay translucent; the shell composites those itself.
func Flatten(p Palette) Palette {
	for _, c := range []*color.NRGBA{&p.BgHover, &p.BgInset, &p.Border, &p.BorderStrong, &p.Skeleton,
		&p.MentionBg, &p.SelfMention, &p.Highlight, &p.CodeBg, &p.CodeBorder, &p.PreBg, &p.QuoteBar,
		&p.Reaction, &p.ReactionSelf, &p.ScrollThumb, &p.FgMuted, &p.FgSubtle} {
		if c.A != 0xff {
			*c = Over(*c, p.Bg)
		}
	}
	// still translucent, alpha corrected for linear blending
	p.Overlay = LinearAlpha(p.Overlay, p.Bg)
	p.Selection = LinearAlpha(p.Selection, p.Bg)
	p.PanelShadow = LinearAlpha(p.PanelShadow, p.Bg)
	return p
}

// Font returns a Lato font of the given weight.
func Font(w font.Weight) font.Font { return font.Font{Typeface: Typeface, Weight: w} }

// ItalicFont returns an italic Lato font.
func ItalicFont(w font.Weight) font.Font {
	return font.Font{Typeface: Typeface, Weight: w, Style: font.Italic}
}

// Material records a solid color material for text widgets.
func Material(gtx layout.Context, c color.NRGBA) op.CallOp {
	m := op.Record(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	return m.Stop()
}

// TextStyle describes a run of label text.
type TextStyle struct {
	Size       unit.Sp
	Weight     font.Weight
	Italic     bool
	Color      color.NRGBA
	MaxLines   int     // 0 = unlimited
	LineHeight unit.Sp // 0 = font default
	Align      text.Alignment
	Wrap       text.WrapPolicy
}

// Layout draws txt with style s.
func (t *Theme) Layout(gtx layout.Context, s TextStyle, txt string) layout.Dimensions {
	f := Font(s.Weight)
	if s.Italic {
		f.Style = font.Italic
	}
	return t.Text(gtx, LabelOpts{Font: f, Size: s.Size, LineHeight: s.LineHeight, MaxLines: s.MaxLines, Align: s.Align, Wrap: s.Wrap, Color: s.Color}, txt)
}

// Label draws single-line text (ellipsized when it overflows).
func (t *Theme) Label(gtx layout.Context, size unit.Sp, weight font.Weight, col color.NRGBA, txt string) layout.Dimensions {
	return t.Layout(gtx, TextStyle{Size: size, Weight: weight, Color: col, MaxLines: 1}, txt)
}

// Para draws wrapping text with a line height.
func (t *Theme) Para(gtx layout.Context, size, lineHeight unit.Sp, weight font.Weight, col color.NRGBA, txt string) layout.Dimensions {
	return t.Layout(gtx, TextStyle{Size: size, Weight: weight, Color: col, LineHeight: lineHeight}, txt)
}

// Measure returns the pixel width of single-line txt without drawing it.
func (t *Theme) Measure(gtx layout.Context, size unit.Sp, weight font.Weight, txt string) int {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	d := t.Label(g, size, weight, color.NRGBA{}, txt)
	m.Stop()
	return d.Size.X
}

// Editor draws a widget.Editor with Lato.
func (t *Theme) Editor(gtx layout.Context, e *widget.Editor, size unit.Sp, col, selection color.NRGBA) layout.Dimensions {
	return e.Layout(gtx, t.Shaper, Font(Regular), size, Material(gtx, col), Material(gtx, selection))
}

// ---- drawing helpers ----

// Fill fills r with c.
func Fill(gtx layout.Context, r image.Rectangle, c color.NRGBA) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// RRect fills a rounded rectangle.
func RRect(gtx layout.Context, r image.Rectangle, radius int, c color.NRGBA) {
	defer clip.UniformRRect(r, radius).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// Border strokes a rounded rectangle outline of width w px.
func Border(gtx layout.Context, r image.Rectangle, radius, w int, c color.NRGBA) {
	rr := clip.UniformRRect(r, radius)
	paint.FillShape(gtx.Ops, c, clip.Stroke{Path: rr.Path(gtx.Ops), Width: float32(w)}.Op())
}

// Circle fills a circle with the given center and diameter.
func Circle(gtx layout.Context, center image.Point, d int, c color.NRGBA) {
	r := image.Rect(center.X-d/2, center.Y-d/2, center.X-d/2+d, center.Y-d/2+d)
	defer clip.Ellipse(r).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// Image draws img scaled to fill size (cover), clipped to a rounded rect.
func Image(gtx layout.Context, img paint.ImageOp, size image.Point, radius int) layout.Dimensions {
	defer clip.UniformRRect(image.Rectangle{Max: size}, radius).Push(gtx.Ops).Pop()
	is := img.Size()
	if is.X > 0 && is.Y > 0 {
		sx := float32(size.X) / float32(is.X)
		sy := float32(size.Y) / float32(is.Y)
		s := sx
		if sy > s {
			s = sy
		}
		off := layout.FPt(size).Sub(layout.FPt(is).Mul(s)).Div(2)
		defer op.Affine(f32Aff(s, off.X, off.Y)).Push(gtx.Ops).Pop()
		img.Filter = paint.FilterLinear
		img.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}
	return layout.Dimensions{Size: size}
}

// Placeholder draws the neutral tile shown while an image loads.
func (t *Theme) Placeholder(gtx layout.Context, size image.Point, radius int) layout.Dimensions {
	RRect(gtx, image.Rectangle{Max: size}, radius, t.C.Skeleton)
	return layout.Dimensions{Size: size}
}

// Shadow draws a soft drop shadow around r as concentric 1px rings of
// decreasing alpha (cheap: only the perimeter is touched, which matters on
// software GL). The area under r itself is not painted; draw r on top.
func Shadow(gtx layout.Context, r image.Rectangle, radius int, spread int, c color.NRGBA) {
	if spread < 1 {
		return
	}
	for i := 1; i <= spread; i++ {
		// quadratic falloff approximates a gaussian blur's tail
		f := float32(spread-i+1) / float32(spread)
		a := uint8(float32(c.A) * f * f)
		if a == 0 {
			continue
		}
		rr := clip.UniformRRect(r.Inset(-i), radius+i)
		paint.FillShape(gtx.Ops, color.NRGBA{R: c.R, G: c.G, B: c.B, A: a}, clip.Stroke{Path: rr.Path(gtx.Ops), Width: 1.5}.Op())
	}
}

// WithAlpha returns c with alpha a (0..255).
func WithAlpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// Mix linearly blends a toward b by t (0..1), alpha included.
func Mix(a, b color.NRGBA, t float32) color.NRGBA {
	l := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t + 0.5) }
	return color.NRGBA{R: l(a.R, b.R), G: l(a.G, b.G), B: l(a.B, b.B), A: l(a.A, b.A)}
}

func f32Aff(s, dx, dy float32) f32.Affine2D {
	return f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(s, s)).Offset(f32.Pt(dx, dy))
}
