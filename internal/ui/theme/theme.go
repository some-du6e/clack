// Package theme holds yoloslack's look: the Slack 2026 light and dark
// palettes, the embedded Lato typeface, metric constants and small drawing
// helpers shared by every UI package.
//
// A *Theme is used only on the UI goroutine (text.Shaper is not safe for
// concurrent use).
package theme

import (
	"image"
	"image/color"
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
}

var (
	facesOnce sync.Once
	faces     []font.FontFace
)

// Faces returns the parsed Lato faces (parsed once per process).
func Faces() []font.FontFace {
	facesOnce.Do(func() {
		type src struct {
			b []byte
			w font.Weight
			s font.Style
		}
		for _, f := range []src{
			{fonts.Regular, font.Normal, font.Regular},
			{fonts.Bold, font.Bold, font.Regular},
			{fonts.Black, font.Black, font.Regular},
			{fonts.Light, font.Light, font.Regular},
			{fonts.Italic, font.Normal, font.Italic},
			{fonts.BoldItalic, font.Bold, font.Italic},
			{fonts.BlackItalic, font.Black, font.Italic},
			{fonts.LightItalic, font.Light, font.Italic},
		} {
			face, err := opentype.Parse(f.b)
			if err != nil {
				panic("theme: bad embedded font: " + err.Error())
			}
			faces = append(faces, font.FontFace{
				Font: font.Font{Typeface: Typeface, Weight: f.w, Style: f.s},
				Face: face,
			})
		}
	})
	return faces
}

// New builds a theme. System fonts stay enabled as a fallback for scripts
// Lato lacks (CJK, symbols); Lato is preferred for everything it covers.
func New(dark bool) *Theme {
	t := &Theme{Shaper: text.NewShaper(text.WithCollection(Faces()))}
	t.SetDark(dark)
	return t
}

// SetDark switches palettes.
func (t *Theme) SetDark(dark bool) {
	t.Dark = dark
	if dark {
		t.C = DarkPalette
	} else {
		t.C = LightPalette
	}
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
	l := widget.Label{MaxLines: s.MaxLines, Alignment: s.Align, LineHeight: s.LineHeight, WrapPolicy: s.Wrap}
	return l.Layout(gtx, t.Shaper, f, s.Size, txt, Material(gtx, s.Color))
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

// Shadow draws a soft drop shadow under r (a few stacked translucent rrects;
// cheap and close to Slack's box-shadow).
func Shadow(gtx layout.Context, r image.Rectangle, radius int, spread int, c color.NRGBA) {
	for i := spread; i >= 1; i-- {
		a := uint8(int(c.A) / spread)
		if a == 0 {
			a = 1
		}
		RRect(gtx, r.Inset(-i).Add(image.Pt(0, i/2)), radius+i, color.NRGBA{R: c.R, G: c.G, B: c.B, A: a})
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
