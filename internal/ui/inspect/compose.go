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

// The image Karim pastes into #clack: the picked element as it was drawn
// (Shoot: at ShotDensity px per dp) with CropMargin dp around it, that
// margin blurred past reading so nothing else in the workspace comes
// along, the element outlined, and under it a panel with the report's
// rows. It is drawn on the CPU with the Go fonts (already in the binary
// for the demo's art), so it needs no GPU and no UI goroutine, and a test
// sees the same pixels CI does.

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

// PanelMinWidth is the narrowest the report panel gets, in the panel's
// dp (its text is 12 of them): a small element's image is this wide, the
// element at its own size centred above the panel.
const PanelMinWidth = 600

// MarginBlur is how far the margin around the element is blurred, dp: a
// box blur this far each way, three times (near a Gaussian of this
// sigma), so a neighbour's 15 dp text is a smear.
const MarginBlur = 5

// RedactBlur is how far (dp) a rect the user marked in the review is
// smoothed after Redact fills it from its edges.
const RedactBlur = 6

// Compose draws shot (the element and the margin around it, px) with
// elem (px, in shot) outlined and everything outside it blurred, over
// the report panel for lines; scale is the shot's px per dp, which sizes
// the outline, the blur and the panel's text. A dragged region's shot
// is its elem whole: nothing to blur around it, and no outline. redact
// are the rects (px, in shot) the user marked in the review (review.go):
// Redact paints them over, so the image holds nothing of what was there.
func Compose(shot *image.RGBA, elem image.Rectangle, scale float32, lines []Line, redact ...image.Rectangle) (*image.RGBA, error) {
	if err := loadFonts(); err != nil {
		return nil, err
	}
	if scale <= 0 {
		scale = 1
	}
	px := clampF(12*scale, 11, 30)
	u := px / 12 // the panel's px per dp
	sb := shot.Bounds()
	W := max(sb.Dx(), int(PanelMinWidth*u))
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

	pad := int(16 * u)
	gap := int(12 * u)
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

	out := image.NewRGBA(image.Rect(0, 0, W, sb.Dy()+band+panelH))
	draw.Draw(out, out.Bounds(), image.NewUniform(panelBg), image.Point{}, draw.Src)
	// the shot at its own size, centred: the user's rects painted over
	// first (so the margin's blur has nothing of them either), then all
	// of it blurred, and the element itself drawn sharp again
	at := image.Pt((W-sb.Dx())/2, 0)
	paintShot(out, at, Redacted(shot, redact, scale), elem, scale)
	el := elem.Sub(sb.Min).Add(at)
	if !sb.In(elem) {
		outline(out, el, scale)
	}
	top := sb.Dy()
	draw.Draw(out, image.Rect(0, top, W, top+band), image.NewUniform(outlineCol), image.Point{}, draw.Src)
	top += band

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

// paintShot draws shot onto dst with its top-left at at: blurred
// MarginBlur dp (scale px per dp) everywhere but elem (shot px), which
// stays sharp. A shot that is all elem (a dragged region) is copied. Only
// the margin's pixels are blurred (blurOutside): what the whole shot's
// blur would leave there, bit for bit.
func paintShot(dst *image.RGBA, at image.Point, shot *image.RGBA, elem image.Rectangle, scale float32) {
	sb := shot.Bounds()
	r := image.Rectangle{Min: at, Max: at.Add(sb.Size())}
	draw.Draw(dst, r, shot, sb.Min, draw.Src)
	if sb.In(elem) {
		return
	}
	c := elem.Intersect(sb)
	blurOutside(dst, r, max(int(math.Round(float64(MarginBlur*scale))), 1), c.Sub(sb.Min).Add(at))
	if !c.Empty() {
		draw.Draw(dst, c.Sub(sb.Min).Add(at), shot, c.Min, draw.Src)
	}
}

// shownImage is the shot as the copy will show it, the rects in marks
// painted over (Redacted) and the margin around elem blurred (paintShot):
// what the review draws.
func shownImage(shot *image.RGBA, marks []image.Rectangle, elem image.Rectangle, scale float32) *image.RGBA {
	img := image.NewRGBA(shot.Bounds())
	paintShot(img, shot.Bounds().Min, Redacted(shot, marks, scale), elem, scale)
	return img
}

// Redacted is shot with the rects in redact (px in shot) painted over
// (Redact), a copy; shot itself when there are none.
func Redacted(shot *image.RGBA, redact []image.Rectangle, scale float32) *image.RGBA {
	if len(redact) == 0 {
		return shot
	}
	img := image.NewRGBA(shot.Bounds())
	draw.Draw(img, img.Bounds(), shot, shot.Bounds().Min, draw.Src)
	Redact(img, img.Bounds(), redact, scale)
	return img
}

// Redact paints the rects in marks (px in img) over so that they keep
// nothing of what was drawn there: each is filled from the nearest
// pixels outside every rect (on each side, the row or column level with
// the pixel, the nearer side weighing more, sides past bounds left out),
// a smooth patch that runs into what's around it, then smoothed
// RedactBlur dp (scale px per dp) inside itself. Unlike a blur of the
// rects' own pixels nothing under any of them comes through, so no text
// or face can be read back out, however the image is sharpened, and
// overlapping rects leak nothing into each other.
func Redact(img *image.RGBA, bounds image.Rectangle, marks []image.Rectangle, scale float32) {
	bounds = bounds.Intersect(img.Bounds())
	if scale <= 0 {
		scale = 1
	}
	var ms []image.Rectangle
	for _, m := range marks {
		if m = m.Canon().Intersect(bounds); !m.Empty() {
			ms = append(ms, m)
		}
	}
	if len(ms) == 0 {
		return
	}
	// every fill from the image as it was, so the order doesn't matter
	src := image.NewRGBA(img.Bounds())
	copy(src.Pix, img.Pix)
	for _, m := range ms {
		fillFromEdges(img, src, bounds, m, ms)
	}
	rad := max(int(math.Round(float64(RedactBlur*scale))), 1)
	for _, m := range ms {
		blur(img, m, rad)
	}
}

// fillFromEdges fills m in img from src: each pixel the nearest pixels
// level with it outside every rect in all (inside b) up, down, left and
// right, weighted by 1 over their distance. A pixel with none (the rects
// cover all of b around it) is grey.
func fillFromEdges(img, src *image.RGBA, b, m image.Rectangle, all []image.Rectangle) {
	covered := func(x, y int) bool {
		p := image.Pt(x, y)
		for _, r := range all {
			if p.In(r) {
				return true
			}
		}
		return false
	}
	type sample struct {
		c  [4]float32
		d  int // past the rect's edge
		ok bool
	}
	at := func(x, y, d int) sample {
		o := src.PixOffset(x, y)
		p := src.Pix[o : o+4 : o+4]
		return sample{[4]float32{float32(p[0]), float32(p[1]), float32(p[2]), float32(p[3])}, d, true}
	}
	// the nearest uncovered pixel from (x, y) stepping (dx, dy)
	walk := func(x, y, dx, dy int) sample {
		for d := 0; ; d++ {
			if !image.Pt(x, y).In(b) {
				return sample{}
			}
			if !covered(x, y) {
				return at(x, y, d)
			}
			x, y = x+dx, y+dy
		}
	}
	T, B := make([]sample, m.Dx()), make([]sample, m.Dx())
	for x := range T {
		T[x] = walk(m.Min.X+x, m.Min.Y-1, 0, -1)
		B[x] = walk(m.Min.X+x, m.Max.Y, 0, 1)
	}
	L, R := make([]sample, m.Dy()), make([]sample, m.Dy())
	for y := range L {
		L[y] = walk(m.Min.X-1, m.Min.Y+y, -1, 0)
		R[y] = walk(m.Max.X, m.Min.Y+y, 1, 0)
	}
	for y := 0; y < m.Dy(); y++ {
		for x := 0; x < m.Dx(); x++ {
			var acc [4]float32
			var w float32
			add := func(s sample, d int) {
				if !s.ok {
					return
				}
				k := 1 / float32(d+s.d)
				for c := range acc {
					acc[c] += s.c[c] * k
				}
				w += k
			}
			add(T[x], y+1)
			add(B[x], m.Dy()-y)
			add(L[y], x+1)
			add(R[y], m.Dx()-x)
			o := img.PixOffset(m.Min.X+x, m.Min.Y+y)
			if w == 0 {
				copy(img.Pix[o:o+4], []uint8{0x80, 0x80, 0x80, 0xff})
				continue
			}
			for c := range acc {
				img.Pix[o+c] = uint8(min(acc[c]/w+.5, 255))
			}
		}
	}
}

// outline strokes r on img: a white halo, then the pick's colour, and a
// faint wash of it inside.
func outline(img *image.RGBA, r image.Rectangle, scale float32) {
	r = r.Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	sw := max(int(math.Round(float64(2*scale))), 2)
	hw := max(int(math.Round(float64(scale/2))), 1) // the halo
	wash := outlineCol
	wash.A = 0x1c
	blendRect(img, r, wash)
	ring(img, r.Inset(-sw-hw), hw, haloCol)
	ring(img, r.Inset(-sw), sw, outlineCol)
	ring(img, r, hw, haloCol)
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
