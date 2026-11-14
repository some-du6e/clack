package inspect

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// The image Karim pastes into #clack: the window as it was drawn, the
// picked element outlined, and under it a panel with the report's rows.
// It is drawn on the CPU with the Go fonts (already in the binary for the
// demo's art), so it needs no GPU and no UI goroutine, and a test sees the
// same pixels CI does.

var (
	// outlineCol is the pick's outline: no Slack colour, so it can't be
	// read as part of the UI.
	outlineCol = color.NRGBA{R: 0xff, G: 0x2e, B: 0x93, A: 0xff}
	haloCol    = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xe6}
	panelBg    = color.NRGBA{R: 0x16, G: 0x17, B: 0x1a, A: 0xff}
	panelKey   = color.NRGBA{R: 0x9e, G: 0xa0, B: 0xa6, A: 0xff}
	panelText  = color.NRGBA{R: 0xf2, G: 0xf2, B: 0xf3, A: 0xff}
)

var fonts struct {
	once      sync.Once
	reg, bold *opentype.Font
	err       error
}

func loadFonts() error {
	fonts.once.Do(func() {
		if fonts.reg, fonts.err = opentype.Parse(goregular.TTF); fonts.err != nil {
			return
		}
		fonts.bold, fonts.err = opentype.Parse(gobold.TTF)
	})
	return fonts.err
}

// Compose draws shot (the window, px) with rect (px) outlined, over the
// report panel for lines; scale is the window's px per dp, which sizes the
// outline and the panel's text.
func Compose(shot *image.RGBA, rect image.Rectangle, scale float32, lines []Line) (*image.RGBA, error) {
	if err := loadFonts(); err != nil {
		return nil, err
	}
	if scale <= 0 {
		scale = 1
	}
	W := shot.Bounds().Dx()
	px := clampF(12*scale, 11, 30)
	reg, err := opentype.NewFace(fonts.reg, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	defer reg.Close()
	bold, err := opentype.NewFace(fonts.bold, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	defer bold.Close()

	pad := int(16 * scale)
	gap := int(12 * scale)
	lh := int(math.Ceil(float64(px) * 1.45))
	keyW := 0
	for _, l := range lines {
		keyW = max(keyW, font.MeasureString(bold, l.Key).Ceil())
	}
	valX := pad + keyW + gap
	valW := max(W-valX-pad, 8*int(px))
	type row struct {
		key   string
		parts []string
	}
	rows := make([]row, 0, len(lines))
	n := 0
	for _, l := range lines {
		parts := wrap(reg, l.Value, valW)
		rows = append(rows, row{l.Key, parts})
		n += len(parts)
	}
	panelH := 2*pad + n*lh
	band := max(int(3*scale), 2)

	out := image.NewRGBA(image.Rect(0, 0, W, shot.Bounds().Dy()+band+panelH))
	draw.Draw(out, shot.Bounds().Sub(shot.Bounds().Min), shot, shot.Bounds().Min, draw.Src)
	outline(out, rect.Sub(shot.Bounds().Min), scale)
	top := shot.Bounds().Dy()
	draw.Draw(out, image.Rect(0, top, W, top+band), image.NewUniform(outlineCol), image.Point{}, draw.Src)
	top += band
	draw.Draw(out, image.Rect(0, top, W, out.Bounds().Max.Y), image.NewUniform(panelBg), image.Point{}, draw.Src)

	asc := reg.Metrics().Ascent.Ceil()
	y := top + pad + (lh-int(px))/2 + asc - int(px)/8
	for _, r := range rows {
		(&font.Drawer{Dst: out, Src: image.NewUniform(panelKey), Face: bold, Dot: fixed.P(pad, y)}).DrawString(r.key)
		for _, p := range r.parts {
			(&font.Drawer{Dst: out, Src: image.NewUniform(panelText), Face: reg, Dot: fixed.P(valX, y)}).DrawString(p)
			y += lh
		}
	}
	return out, nil
}

// outline strokes r on img: a white halo, then the pick's colour, and a
// faint wash of it inside.
func outline(img *image.RGBA, r image.Rectangle, scale float32) {
	r = r.Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	sw := max(int(math.Round(float64(2*scale))), 2)
	wash := outlineCol
	wash.A = 0x1c
	blendRect(img, r, wash)
	ring(img, r.Inset(-sw-1), 1, haloCol)
	ring(img, r.Inset(-sw), sw, outlineCol)
	ring(img, r, 1, haloCol)
}

// ring strokes w px inside r.
func ring(img *image.RGBA, r image.Rectangle, w int, c color.NRGBA) {
	if r.Empty() {
		return
	}
	blendRect(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+w), c)
	blendRect(img, image.Rect(r.Min.X, r.Max.Y-w, r.Max.X, r.Max.Y), c)
	blendRect(img, image.Rect(r.Min.X, r.Min.Y+w, r.Min.X+w, r.Max.Y-w), c)
	blendRect(img, image.Rect(r.Max.X-w, r.Min.Y+w, r.Max.X, r.Max.Y-w), c)
}

// blendRect draws c over r (source over).
func blendRect(img *image.RGBA, r image.Rectangle, c color.NRGBA) {
	r = r.Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Over)
}

// wrap breaks s into lines at most w px wide in face, at spaces, or
// anywhere inside a word wider than w.
func wrap(face font.Face, s string, w int) []string {
	if s == "" {
		return []string{""}
	}
	var out []string
	line := ""
	for _, word := range strings.Split(s, " ") {
		try := word
		if line != "" {
			try = line + " " + word
		}
		if font.MeasureString(face, try).Ceil() <= w {
			line = try
			continue
		}
		if line != "" {
			out = append(out, line)
			line = ""
		}
		for font.MeasureString(face, word).Ceil() > w {
			// a word wider than the column: as many runes as fit
			rs := []rune(word)
			k := len(rs)
			for k > 1 && font.MeasureString(face, string(rs[:k])).Ceil() > w {
				k--
			}
			out = append(out, string(rs[:k]))
			word = string(rs[k:])
		}
		line = word
	}
	return append(out, line)
}

func clampF(v, lo, hi float32) float32 {
	return min(max(v, lo), hi)
}
