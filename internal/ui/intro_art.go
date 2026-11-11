package ui

import (
	"image"
	"image/color"
	"math"
	"sync"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	xfixed "golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/some-du6e/clack/assets/fonts"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The introduction's art: the flat backdrop, the Clack mark,
// and blurred copies of the display text for the blur-in (Gio has no blur
// filter, so the blurred states are rendered on the CPU, small, and the
// sharp state is Gio's own text).

// ---- colour in linear light ----

type lin [3]float32

var srgbToLinLUT = func() (t [256]float32) {
	for i := range t {
		c := float64(i) / 255
		if c <= 0.04045 {
			t[i] = float32(c / 12.92)
		} else {
			t[i] = float32(math.Pow((c+0.055)/1.055, 2.4))
		}
	}
	return t
}()

var linToSRGBLUT = func() (t [4096]uint8) {
	for i := range t {
		l := float64(i) / 4095
		s := l * 12.92
		if l > 0.0031308 {
			s = 1.055*math.Pow(l, 1/2.4) - 0.055
		}
		t[i] = uint8(math.Round(math.Max(0, math.Min(1, s)) * 255))
	}
	return t
}()

func linOf(c color.NRGBA) lin {
	return lin{srgbToLinLUT[c.R], srgbToLinLUT[c.G], srgbToLinLUT[c.B]}
}

func linToSRGB(l float32) uint8 {
	if l <= 0 {
		return 0
	}
	if l >= 1 {
		return 255
	}
	return linToSRGBLUT[int(l*4095+0.5)]
}

// alphaMul is c with its alpha scaled by a (0..1). hexc (codechannel.go)
// makes the opaque colours.
func alphaMul(c color.NRGBA, a float32) color.NRGBA {
	c.A = uint8(math.Round(float64(c.A) * clamp01(float64(a))))
	return c
}

// ---- the backdrop ----

// introBase is the window behind the introduction and the sign-in card:
// one flat colour, Slack's dark app background (#1a1d21), so the window
// the user signs in to, and its dark caption, are the same surface.
var introBase = hexc(0x1a1d21)

// introPill and introInk are the white primary button and its label:
// neutral, the one bright thing on the flat dark window.
var (
	introPill = hexc(0xececec)
	introInk  = hexc(0x111214)
)

// drawBackdrop fills the window with introBase: no gradient, no glows,
// no grain (on a flat fill there are no bands to break up).
func drawBackdrop(gtx layout.Context) {
	theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, introBase)
}

// ---- soft round light (sprites drawn scaled) ----

var (
	radialOnce sync.Once
	radialOps  [2]paint.ImageOp
)

// radialSprites are a white disc of light falling off to nothing: full
// strength and a faint one (the logo's sheen), 64 px, drawn scaled. The
// ops are made once: a new ImageOp is a new texture upload.
func radialSprites() [2]paint.ImageOp {
	radialOnce.Do(func() {
		const n = 64
		for k, peak := range [2]float64{1, 0.2} {
			img := image.NewRGBA(image.Rect(0, 0, n, n))
			for y := 0; y < n; y++ {
				for x := 0; x < n; x++ {
					dx := (float64(x) + 0.5 - n/2) / (n / 2)
					dy := (float64(y) + 0.5 - n/2) / (n / 2)
					d2 := dx*dx + dy*dy
					a := 0.0
					if d2 < 1 {
						a = peak * math.Pow(1-d2, 2)
					}
					v := uint8(math.Round(a * 255))
					o := img.Pix[(y*n+x)*4:]
					o[0], o[1], o[2], o[3] = v, v, v, v
				}
			}
			radialOps[k] = paint.NewImageOp(img)
			radialOps[k].Filter = paint.FilterLinear
		}
	})
	return radialOps
}

// ---- blurred sprites ----

// blurLevel is one blurred copy of a word or of the mark: res sprite px
// per device px, and where the sprite's top-left sits relative to the
// text's origin (its left edge and baseline), in device px.
type blurLevel struct {
	op   paint.ImageOp
	res  float32
	left float32 // device px left of the text's origin
	asc  float32 // device px from the sprite's top to the baseline
	adv  float32 // the text's advance (device px), to match Gio's width
	ok   bool
}

// textBlur is a word blurred wide (the start of the blur-in) and lightly
// (its middle); Gio's text is the end.
type textBlur [2]blurLevel

type spriteKey struct {
	text   string
	weight font.Weight
	px     int
}

// introSprites renders the blurred copies off the UI goroutine. get
// returns nil until a copy is ready (the element just fades meanwhile).
type introSprites struct {
	mu    sync.Mutex
	text  map[spriteKey]*textBlur
	logo  map[int]*textBlur
	queue []spriteKey
	busy  bool
	done  func() // wakes the window when a batch is ready
}

func (s *introSprites) word(k spriteKey) *textBlur {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.text == nil {
		s.text = map[spriteKey]*textBlur{}
	}
	if tb, ok := s.text[k]; ok {
		return tb // nil while it renders
	}
	s.text[k] = nil
	s.queue = append(s.queue, k)
	s.kick()
	return nil
}

// mark is the Clack mark's blurred copies at side px (k.text "").
func (s *introSprites) mark(px int) *textBlur {
	return s.word(spriteKey{px: px})
}

// kick starts the renderer if it isn't running. Holds mu.
func (s *introSprites) kick() {
	if s.busy || len(s.queue) == 0 {
		return
	}
	s.busy = true
	go func() {
		for {
			s.mu.Lock()
			if len(s.queue) == 0 {
				s.busy = false
				done := s.done
				s.mu.Unlock()
				if done != nil {
					done()
				}
				return
			}
			k := s.queue[0]
			s.queue = s.queue[1:]
			s.mu.Unlock()
			var tb textBlur
			if k.text == "" {
				tb = renderMarkBlur(float32(k.px))
			} else {
				tb = renderTextBlur(k.text, k.weight, float32(k.px))
			}
			s.mu.Lock()
			s.text[k] = &tb
			s.mu.Unlock()
		}
	}()
}

// ready reports whether every sprite asked for so far has rendered.
func (s *introSprites) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.busy && len(s.queue) == 0
}

var introFonts struct {
	sync.Mutex
	f map[font.Weight]*opentype.Font
}

// introFont is the embedded Lato face nearest w, parsed for x/image.
func introFont(w font.Weight) *opentype.Font {
	introFonts.Lock()
	defer introFonts.Unlock()
	if introFonts.f == nil {
		introFonts.f = map[font.Weight]*opentype.Font{}
	}
	if f, ok := introFonts.f[w]; ok {
		return f
	}
	b := fonts.Regular
	switch {
	case w >= font.Black:
		b = fonts.Black
	case w >= font.Bold:
		b = fonts.Bold
	}
	f, err := opentype.Parse(b)
	if err != nil {
		f = nil
	}
	introFonts.f[w] = f
	return f
}

// blurSigmas are the two levels' blur, as fractions of the text size.
var blurSigmas = [2]float32{0.22, 0.08}

func renderTextBlur(txt string, w font.Weight, sizePx float32) textBlur {
	var out textBlur
	f := introFont(w)
	if f == nil || sizePx < 4 {
		return out
	}
	for i, frac := range blurSigmas {
		sig := frac * sizePx
		res := float32(math.Min(1, math.Max(0.12, 4/float64(sig))))
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(sizePx * res), DPI: 72, Hinting: xfont.HintingNone})
		if err != nil {
			continue
		}
		m := face.Metrics()
		adv := xfont.MeasureString(face, txt)
		s := sig * res
		pad := int(math.Ceil(float64(3*s))) + 1
		wpx := adv.Ceil() + 2*pad
		hpx := (m.Ascent + m.Descent).Ceil() + 2*pad
		mask := image.NewAlpha(image.Rect(0, 0, wpx, hpx))
		d := xfont.Drawer{Dst: mask, Src: image.Opaque, Face: face, Dot: xfixed.Point26_6{X: xfixed.I(pad), Y: xfixed.I(pad) + m.Ascent}}
		d.DrawString(txt)
		face.Close()
		px := make([]float32, wpx*hpx)
		for j := range px {
			px[j] = float32(mask.Pix[j])
		}
		boxBlur3(px, wpx, hpx, s)
		img := image.NewRGBA(image.Rect(0, 0, wpx, hpx))
		for j, v := range px {
			a := uint8(math.Max(0, math.Min(255, float64(v)+0.5)))
			o := img.Pix[j*4:]
			o[0], o[1], o[2], o[3] = a, a, a, a
		}
		out[i] = blurLevel{
			op:   paint.NewImageOp(img),
			res:  res,
			left: float32(pad) / res,
			asc:  (float32(pad) + fix26(m.Ascent)) / res,
			adv:  fix26(adv) / res,
			ok:   true,
		}
		out[i].op.Filter = paint.FilterLinear
	}
	return out
}

func fix26(v xfixed.Int26_6) float32 { return float32(v) / 64 }

// boxBlur3 blurs a w×h channel with three box passes each way (close to a
// Gaussian of sigma s).
func boxBlur3(px []float32, w, h int, s float32) {
	r := int(math.Round(float64(s)))
	if r < 1 {
		return
	}
	line := make([]float32, max(w, h))
	out := make([]float32, max(w, h))
	for pass := 0; pass < 3; pass++ {
		for y := 0; y < h; y++ {
			copy(line[:w], px[y*w:(y+1)*w])
			boxLine(line[:w], out[:w], r)
			copy(px[y*w:(y+1)*w], out[:w])
		}
		for x := 0; x < w; x++ {
			for y := 0; y < h; y++ {
				line[y] = px[y*w+x]
			}
			boxLine(line[:h], out[:h], r)
			for y := 0; y < h; y++ {
				px[y*w+x] = out[y]
			}
		}
	}
}

// boxLine is dst[i] = the mean of src[i-r..i+r] (zero outside).
func boxLine(src, dst []float32, r int) {
	n := len(src)
	inv := 1 / float32(2*r+1)
	var acc float32
	for j := 0; j <= r && j < n; j++ {
		acc += src[j]
	}
	for i := 0; i < n; i++ {
		dst[i] = acc * inv
		if k := i + r + 1; k < n {
			acc += src[k]
		}
		if k := i - r; k >= 0 {
			acc -= src[k]
		}
	}
}

// ---- the Clack mark ----

// The mark is assets/icon.svg: a violet tile (1024 units, corner 225),
// a white C (a 248-unit arc, 150 wide, round caps) with a speech-bubble
// tail, and two amber strokes, the clack.
var (
	markTileA = hexc(0x7b5cff)
	markTileB = hexc(0x3b1fb8)
	markAmber = hexc(0xffb547)
)

const (
	markCX, markCY = 476.0, 500.0
	markR          = 248.0
	markStroke     = 150.0
	markA0         = -0.73290 // the C's upper end
	markSweep      = -4.81738 // to its lower end, through the top and the left
)

// markParts is how much of each part shows: tile (opacity), the amber
// strokes' length (0..1, each), and a ring of light leaving the tile.
type markParts struct {
	lines [2]float32
	ring  float32 // 0..1 through the pulse, 0 none
	sheen float32 // 0..1 across the tile, 0 none
}

// drawMark draws the mark with its top-left at o, side s device px.
func drawMark(gtx layout.Context, o f32.Point, s float32, p markParts) {
	k := s / 1024
	defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(k, k)).Offset(o)).Push(gtx.Ops).Pop()
	func() {
		defer clip.UniformRRect(image.Rect(0, 0, 1024, 1024), 225).Push(gtx.Ops).Pop()
		paint.LinearGradientOp{Stop1: f32.Pt(0, 0), Color1: markTileA, Stop2: f32.Pt(358, 1024), Color2: markTileB}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		// the sheen: a faint light high on the left
		rs := radialSprites()
		drawSprite(gtx, rs[1], f32.Pt(307-768, 123-768), 1536/64)
		if p.sheen > 0 && p.sheen < 1 {
			// a light sweeping across once, after the mark lands
			x := lerp(-500, 1500, float64(p.sheen))
			st := paint.PushOpacity(gtx.Ops, float32(0.16*math.Sin(math.Pi*float64(p.sheen))))
			t := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(4.5, 22)).Rotate(f32.Point{}, 0.5).Offset(f32.Pt(float32(x), -380))).Push(gtx.Ops)
			rs[0].Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			t.Pop()
			st.Pop()
		}
	}()
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	var c clip.Path
	c.Begin(gtx.Ops)
	c.MoveTo(f32.Pt(markCX+markR*float32(math.Cos(markA0)), markCY+markR*float32(math.Sin(markA0))))
	c.ArcTo(f32.Pt(markCX, markCY), f32.Pt(markCX, markCY), markSweep)
	paint.FillShape(gtx.Ops, white, clip.Stroke{Path: c.End(), Width: markStroke}.Op())
	var t clip.Path
	t.Begin(gtx.Ops)
	t.MoveTo(f32.Pt(194.4, 580.8))
	t.QuadTo(f32.Pt(190, 692.9), f32.Pt(206.4, 810.2))
	t.QuadTo(f32.Pt(325.5, 760.7), f32.Pt(415.1, 786.6))
	t.Close()
	tail := t.End()
	paint.FillShape(gtx.Ops, white, clip.Outline{Path: tail}.Op())
	paint.FillShape(gtx.Ops, white, clip.Stroke{Path: tail, Width: 28}.Op())
	for i, l := range [2][2]f32.Point{
		{f32.Pt(748.9, 432.2), f32.Pt(821.8, 386.6)},
		{f32.Pt(748.9, 567.8), f32.Pt(821.8, 613.4)},
	} {
		f := p.lines[i]
		if f <= 0.01 {
			continue
		}
		var q clip.Path
		q.Begin(gtx.Ops)
		q.MoveTo(l[0])
		q.LineTo(l[0].Add(l[1].Sub(l[0]).Mul(f)))
		paint.FillShape(gtx.Ops, markAmber, clip.Stroke{Path: q.End(), Width: 62}.Op())
	}
}

// drawMarkRing is the light ring the mark sends out when it lands
// (centre c, side s): it widens and fades.
func drawMarkRing(gtx layout.Context, c f32.Point, s, p float32) {
	if p <= 0 || p >= 1 {
		return
	}
	r := s * (0.55 + 0.7*p)
	a := 0.42 * (1 - p) * (1 - p)
	w := s * 0.018 * (1.4 - p)
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(c.X+r, c.Y))
	path.ArcTo(c, c, 2*math.Pi)
	path.Close()
	paint.FillShape(gtx.Ops, alphaMul(hexc(0xc9bcff), a), clip.Stroke{Path: path.End(), Width: w}.Op())
}

// drawSprite draws img with its top-left at o, scaled by s.
func drawSprite(gtx layout.Context, img paint.ImageOp, o f32.Point, s float32) {
	defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(s, s)).Offset(o)).Push(gtx.Ops).Pop()
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// renderMarkBlur is the mark (tile, C, tail; the amber strokes come
// later) blurred at both levels, for side px. Its "text origin" is the
// tile's top-left: left and asc are how far the blur reaches past it.
func renderMarkBlur(side float32) textBlur {
	var out textBlur
	if side < 8 {
		return out
	}
	for i, frac := range [2]float32{0.14, 0.05} {
		sig := frac * side
		res := float32(math.Min(1, math.Max(0.1, 5/float64(sig))))
		s := side * res
		bs := sig * res
		pad := int(math.Ceil(float64(3*bs))) + 1
		n := int(math.Ceil(float64(s))) + 2*pad
		u := s / 1024
		at := func(x, y float32) (float32, float32) { return float32(pad) + x*u, float32(pad) + y*u }
		// the tile
		z := vector.NewRasterizer(n, n)
		rrectPoly(z, float32(pad), float32(pad), s, s, 225*u)
		tile := image.NewAlpha(image.Rect(0, 0, n, n))
		z.Draw(tile, tile.Bounds(), image.Opaque, image.Point{})
		// the C and its tail
		z.Reset(n, n)
		arcStrokePoly(z, at, markCX, markCY, markR, markStroke/2, markA0, markSweep)
		z.MoveTo(at(194.4, 580.8))
		qx, qy := at(190, 692.9)
		ex, ey := at(206.4, 810.2)
		z.QuadTo(qx, qy, ex, ey)
		qx, qy = at(325.5, 760.7)
		ex, ey = at(415.1, 786.6)
		z.QuadTo(qx, qy, ex, ey)
		z.ClosePath()
		glyph := image.NewAlpha(image.Rect(0, 0, n, n))
		z.Draw(glyph, glyph.Bounds(), image.Opaque, image.Point{})
		// compose (premultiplied, sRGB), then blur each channel
		ch := [4][]float32{make([]float32, n*n), make([]float32, n*n), make([]float32, n*n), make([]float32, n*n)}
		ta, tb := markTileA, markTileB
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				j := y*n + x
				// the gradient runs from (0,0) to (358,1024) in tile units
				gx, gy := (float32(x)-float32(pad))/u, (float32(y)-float32(pad))/u
				gt := (gx*358 + gy*1024) / (358*358 + 1024*1024)
				gt = float32(clamp01(float64(gt)))
				cr := lerp(float64(ta.R), float64(tb.R), float64(gt))
				cg := lerp(float64(ta.G), float64(tb.G), float64(gt))
				cb := lerp(float64(ta.B), float64(tb.B), float64(gt))
				a := float64(tile.Pix[j]) / 255
				r, g, b := cr*a, cg*a, cb*a
				ga := float64(glyph.Pix[j]) / 255
				r, g, b, a = r*(1-ga)+255*ga, g*(1-ga)+255*ga, b*(1-ga)+255*ga, a*(1-ga)+ga
				ch[0][j], ch[1][j], ch[2][j], ch[3][j] = float32(r), float32(g), float32(b), float32(a*255)
			}
		}
		for c := range ch {
			boxBlur3(ch[c], n, n, bs)
		}
		img := image.NewRGBA(image.Rect(0, 0, n, n))
		for j := 0; j < n*n; j++ {
			o := img.Pix[j*4:]
			av := math.Max(0, math.Min(255, float64(ch[3][j])+0.5))
			for c := 0; c < 3; c++ {
				o[c] = uint8(math.Max(0, math.Min(av, float64(ch[c][j])+0.5)))
			}
			o[3] = uint8(av)
		}
		out[i] = blurLevel{op: paint.NewImageOp(img), res: res, left: float32(pad) / res, asc: float32(pad) / res, adv: side, ok: true}
		out[i].op.Filter = paint.FilterLinear
	}
	return out
}

// rrectPoly adds a rounded rectangle to z (corners as short polylines).
func rrectPoly(z *vector.Rasterizer, x, y, w, h, r float32) {
	const seg = 10
	corner := func(cx, cy float32, a0 float64, first bool) {
		for i := 0; i <= seg; i++ {
			a := a0 + float64(i)/seg*math.Pi/2
			px, py := cx+r*float32(math.Cos(a)), cy+r*float32(math.Sin(a))
			if first && i == 0 {
				z.MoveTo(px, py)
			} else {
				z.LineTo(px, py)
			}
		}
	}
	corner(x+w-r, y+r, -math.Pi/2, true)
	corner(x+w-r, y+h-r, 0, false)
	corner(x+r, y+h-r, math.Pi/2, false)
	corner(x+r, y+r, math.Pi, false)
	z.ClosePath()
}

// arcStrokePoly adds the outline of a round-capped stroke along a circle
// arc (centre cx,cy, radius r, half width hw, from angle a0 by sweep) in
// tile units mapped by at.
func arcStrokePoly(z *vector.Rasterizer, at func(x, y float32) (float32, float32), cx, cy, r, hw, a0, sweep float64) {
	const steps = 72
	pt := func(rad, a float64) (float32, float32) {
		return at(float32(cx+rad*math.Cos(a)), float32(cy+rad*math.Sin(a)))
	}
	a1 := a0 + sweep
	dir := math.Copysign(1, sweep)
	x, y := pt(r+hw, a0)
	z.MoveTo(x, y)
	for i := 1; i <= steps; i++ {
		x, y = pt(r+hw, a0+sweep*float64(i)/steps)
		z.LineTo(x, y)
	}
	// the end cap: around the arc's end, outer edge to inner edge
	ex, ey := cx+r*math.Cos(a1), cy+r*math.Sin(a1)
	for i := 1; i <= 16; i++ {
		a := a1 + dir*math.Pi*float64(i)/16
		z.LineTo(at(float32(ex+hw*math.Cos(a)), float32(ey+hw*math.Sin(a))))
	}
	for i := steps - 1; i >= 0; i-- {
		x, y = pt(r-hw, a0+sweep*float64(i)/steps)
		z.LineTo(x, y)
	}
	sx, sy := cx+r*math.Cos(a0), cy+r*math.Sin(a0)
	for i := 1; i <= 16; i++ {
		a := a0 + math.Pi + dir*math.Pi*float64(i)/16
		z.LineTo(at(float32(sx+hw*math.Cos(a)), float32(sy+hw*math.Sin(a))))
	}
	z.ClosePath()
}
