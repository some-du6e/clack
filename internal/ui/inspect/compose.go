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
// stays sharp. A shot that is all elem (a dragged region) is copied.
func paintShot(dst *image.RGBA, at image.Point, shot *image.RGBA, elem image.Rectangle, scale float32) {
	sb := shot.Bounds()
	r := image.Rectangle{Min: at, Max: at.Add(sb.Size())}
	draw.Draw(dst, r, shot, sb.Min, draw.Src)
	if sb.In(elem) {
		return
	}
	blur(dst, r, max(int(math.Round(float64(MarginBlur*scale))), 1))
	if c := elem.Intersect(sb); !c.Empty() {
		draw.Draw(dst, c.Sub(sb.Min).Add(at), shot, c.Min, draw.Src)
	}
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

// Redact paints each rect in marks (px in img, oldest first) over so
// that it keeps nothing of what was drawn there: it is filled from the
// pixels just outside it (each side's row or column, the nearer side
// weighing more, the sides past bounds left out), a smooth patch that
// runs into what's around it, then smoothed RedactBlur dp (scale px per
// dp) inside itself. Unlike a blur of the rect's own pixels nothing
// inside it comes through, so no text or face can be read back out of
// it, however it is sharpened. A rect marked over an earlier one is
// filled from what the earlier one left.
func Redact(img *image.RGBA, bounds image.Rectangle, marks []image.Rectangle, scale float32) {
	bounds = bounds.Intersect(img.Bounds())
	if scale <= 0 {
		scale = 1
	}
	rad := max(int(math.Round(float64(RedactBlur*scale))), 1)
	for _, m := range marks {
		m = m.Canon().Intersect(bounds)
		if m.Empty() {
			continue
		}
		fillFromEdges(img, bounds, m)
		blur(img, m, rad)
	}
}

// fillFromEdges fills m from the rows and columns of img just outside
// it (inside b): each pixel the sides' samples level with it, weighted
// by 1 over the distance to each side. A rect that covers all of b has
// no sides: it's filled grey.
func fillFromEdges(img *image.RGBA, b, m image.Rectangle) {
	top, bot := m.Min.Y > b.Min.Y, m.Max.Y < b.Max.Y
	left, right := m.Min.X > b.Min.X, m.Max.X < b.Max.X
	if !top && !bot && !left && !right {
		draw.Draw(img, m, image.NewUniform(color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}), image.Point{}, draw.Src)
		return
	}
	px := func(x, y int) [4]float32 {
		o := img.PixOffset(x, y)
		p := img.Pix[o : o+4 : o+4]
		return [4]float32{float32(p[0]), float32(p[1]), float32(p[2]), float32(p[3])}
	}
	// the samples, read before any pixel of m is written
	row := func(y int) [][4]float32 {
		out := make([][4]float32, m.Dx())
		for x := range out {
			out[x] = px(m.Min.X+x, y)
		}
		return out
	}
	col := func(x int) [][4]float32 {
		out := make([][4]float32, m.Dy())
		for y := range out {
			out[y] = px(x, m.Min.Y+y)
		}
		return out
	}
	var T, B, L, R [][4]float32
	if top {
		T = row(m.Min.Y - 1)
	}
	if bot {
		B = row(m.Max.Y)
	}
	if left {
		L = col(m.Min.X - 1)
	}
	if right {
		R = col(m.Max.X)
	}
	for y := 0; y < m.Dy(); y++ {
		for x := 0; x < m.Dx(); x++ {
			var acc [4]float32
			var w float32
			add := func(s [4]float32, d int) {
				k := 1 / float32(d)
				for c := range acc {
					acc[c] += s[c] * k
				}
				w += k
			}
			if top {
				add(T[x], y+1)
			}
			if bot {
				add(B[x], m.Dy()-y)
			}
			if left {
				add(L[y], x+1)
			}
			if right {
				add(R[y], m.Dx()-x)
			}
			o := img.PixOffset(m.Min.X+x, m.Min.Y+y)
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

// blur box-blurs img inside r three times, rad px each way, rows then
// columns (edges clamped: nothing outside r comes in).
func blur(img *image.RGBA, r image.Rectangle, rad int) {
	r = r.Intersect(img.Bounds())
	w, h := r.Dx(), r.Dy()
	if w == 0 || h == 0 || rad <= 0 {
		return
	}
	n := max(w, h)
	line := make([]uint8, 4*n)
	tmp := make([]uint8, 4*n)
	d := 2*rad + 1
	// pass blurs line's first l pixels once
	pass := func(l int) {
		for c := 0; c < 4; c++ {
			at := func(i int) int { return int(line[4*min(max(i, 0), l-1)+c]) }
			sum := 0
			for i := -rad; i <= rad; i++ {
				sum += at(i)
			}
			for i := 0; i < l; i++ {
				tmp[4*i+c] = uint8((sum + d/2) / d)
				sum += at(i+rad+1) - at(i-rad)
			}
		}
		copy(line[:4*l], tmp[:4*l])
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		o := img.PixOffset(r.Min.X, y)
		copy(line, img.Pix[o:o+4*w])
		for range 3 {
			pass(w)
		}
		copy(img.Pix[o:o+4*w], line[:4*w])
	}
	for x := r.Min.X; x < r.Max.X; x++ {
		for y := range h {
			o := img.PixOffset(x, r.Min.Y+y)
			copy(line[4*y:4*y+4], img.Pix[o:o+4])
		}
		for range 3 {
			pass(h)
		}
		for y := range h {
			o := img.PixOffset(x, r.Min.Y+y)
			copy(img.Pix[o:o+4], line[4*y:4*y+4])
		}
	}
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
