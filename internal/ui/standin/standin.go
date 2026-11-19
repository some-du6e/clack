// Package standin draws the browser demo's stand-ins for the pictures the
// native app copies from Slack's bundles (THIRD_PARTY_NOTICES.md, "Slack"):
// the thread loader, the default bot avatar and app icon, the empty-state
// pictures and the drag-and-drop cards. Each is drawn at its original's
// size in plain shapes and greys, so whatever decodes it lays out as
// before; none of it is Slack's. Only the js files that take those embeds'
// place use it (cmd/clack-web's build); the native app keeps Slack's.
package standin

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"math"

	"golang.org/x/image/vector"
)

// Kind is an empty-state picture.
type Kind int

const (
	Search  Kind = iota // a magnifier over a card: nothing found
	Glass               // a magnifier on its own
	Folder              // an empty folder
	Group               // three people: a user group
	Blocked             // a document, crossed out: a file that can't be added
)

var (
	paper = color.NRGBA{0xf7, 0xf7, 0xf7, 0xff}
	light = color.NRGBA{0xdf, 0xdf, 0xdf, 0xff}
	mid   = color.NRGBA{0xbc, 0xbc, 0xbc, 0xff}
	ink   = color.NRGBA{0x8a, 0x8a, 0x8a, 0xff}

	nightCard = color.NRGBA{0x2c, 0x2d, 0x30, 0xff}
	nightLine = color.NRGBA{0x4a, 0x4c, 0x52, 0xff}
)

type pt struct{ x, y float32 }

type pic struct {
	img *image.NRGBA
	z   *vector.Rasterizer
}

func newPic(w, h int) *pic {
	return &pic{img: image.NewNRGBA(image.Rect(0, 0, w, h)), z: vector.NewRasterizer(w, h)}
}

// fill paints the shapes the paths trace in c. A path traced the other
// way round (ccw) cuts a hole in the ones before it.
func (p *pic) fill(c color.NRGBA, paths ...[]pt) {
	b := p.img.Bounds()
	p.z.Reset(b.Dx(), b.Dy())
	for _, ps := range paths {
		if len(ps) < 3 {
			continue
		}
		p.z.MoveTo(ps[0].x, ps[0].y)
		for _, q := range ps[1:] {
			p.z.LineTo(q.x, q.y)
		}
		p.z.ClosePath()
	}
	p.z.Draw(p.img, b, image.NewUniform(c), image.Point{})
}

func (p *pic) png() []byte {
	var b bytes.Buffer
	_ = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, p.img)
	return b.Bytes()
}

func reverse(ps []pt) []pt {
	out := make([]pt, len(ps))
	for i, q := range ps {
		out[len(ps)-1-i] = q
	}
	return out
}

func circle(cx, cy, r float32) []pt {
	const n = 72
	ps := make([]pt, n)
	for i := range ps {
		a := 2 * math.Pi * float64(i) / n
		ps[i] = pt{cx + r*float32(math.Cos(a)), cy + r*float32(math.Sin(a))}
	}
	return ps
}

func rrect(x0, y0, x1, y1, r float32) []pt {
	r = min(r, (x1-x0)/2, (y1-y0)/2)
	const n = 12
	var ps []pt
	corner := func(cx, cy float32, from float64) {
		for i := 0; i <= n; i++ {
			a := from + math.Pi/2*float64(i)/n
			ps = append(ps, pt{cx + r*float32(math.Cos(a)), cy + r*float32(math.Sin(a))})
		}
	}
	corner(x1-r, y0+r, -math.Pi/2)
	corner(x1-r, y1-r, 0)
	corner(x0+r, y1-r, math.Pi/2)
	corner(x0+r, y0+r, math.Pi)
	return ps
}

// bar is a line from a to b, w wide, with round ends.
func (p *pic) bar(c color.NRGBA, a, b pt, w float32) {
	dx, dy := b.x-a.x, b.y-a.y
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l == 0 {
		return
	}
	nx, ny := -dy/l*w/2, dx/l*w/2
	p.fill(c, []pt{{a.x + nx, a.y + ny}, {b.x + nx, b.y + ny}, {b.x - nx, b.y - ny}, {a.x - nx, a.y - ny}},
		circle(a.x, a.y, w/2), circle(b.x, b.y, w/2))
}

// person is a head and shoulders, s across, its top at (x, y).
func (p *pic) person(c color.NRGBA, x, y, s float32) {
	p.fill(c, circle(x+s/2, y+s*0.28, s*0.22))
	p.fill(c, rrect(x+s*0.1, y+s*0.58, x+s*0.9, y+s*1.05, s*0.3))
}

// magnifier is a ring and a handle, its lens r in radius at (cx, cy).
func (p *pic) magnifier(c color.NRGBA, cx, cy, r float32) {
	w := r * 0.28
	p.fill(c, circle(cx, cy, r+w/2), reverse(circle(cx, cy, r-w/2)))
	d := float32(math.Sqrt2) / 2
	p.bar(c, pt{cx + (r+w/2)*d, cy + (r+w/2)*d}, pt{cx + r*1.9*d + r*0.5, cy + r*1.9*d + r*0.5}, w*1.25)
}

// card is a file card in r: a document icon and two lines of text.
func (p *pic) card(r image.Rectangle, night bool) {
	x0, y0, x1, y1 := float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y)
	h := y1 - y0
	body, edge, line := paper, light, mid
	if night {
		body, edge, line = nightCard, nightLine, nightLine
	}
	p.fill(edge, rrect(x0, y0, x1, y1, h*0.08))
	p.fill(body, rrect(x0+2, y0+2, x1-2, y1-2, h*0.07))
	ix, iy, is := x0+h*0.16, y0+h*0.2, h*0.6
	p.fill(line, rrect(ix, iy, ix+is*0.78, iy+is, is*0.1))
	tx := ix + is*0.78 + h*0.14
	p.fill(line, rrect(tx, iy+is*0.15, x1-h*0.2, iy+is*0.32, is*0.08))
	p.fill(edge, rrect(tx, iy+is*0.55, tx+(x1-h*0.2-tx)*0.6, iy+is*0.7, is*0.08))
}

// Cell is one piece of a sprite sheet: a file card (Night: its dark-mode
// version), or two lines of text (Text).
type Cell struct {
	R     image.Rectangle
	Night bool
	Text  bool
}

// Cards is a w×h sprite sheet (PNG) with a card or text drawn in each
// cell, a few pixels inside it, so cells that touch don't run together.
func Cards(w, h int, cells []Cell) []byte {
	p := newPic(w, h)
	for _, c := range cells {
		r := c.R.Inset(6)
		if c.Text {
			x0, y0, x1, y1 := float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y)
			lh := (y1 - y0) / 4
			p.fill(mid, rrect(x0, y0+lh*0.5, x1, y0+lh*1.5, lh/2))
			p.fill(light, rrect(x0, y0+lh*2.5, x0+(x1-x0)*0.6, y0+lh*3.5, lh/2))
			continue
		}
		p.card(r, c.Night)
	}
	return p.png()
}

// Picture is a w×h empty-state picture (PNG).
func Picture(w, h int, k Kind) []byte {
	p := newPic(w, h)
	W, H := float32(w), float32(h)
	s := min(W, H)
	switch k {
	case Search:
		p.card(image.Rect(int(W*0.08), int(H*0.2), int(W*0.72), int(H*0.62)), false)
		p.magnifier(ink, W*0.66, H*0.58, s*0.17)
	case Glass:
		p.magnifier(ink, W*0.44, H*0.42, s*0.24)
	case Folder:
		x0, y0, x1, y1 := W*0.12, H*0.16, W*0.88, H*0.86
		p.fill(mid, rrect(x0, y0, x0+(x1-x0)*0.42, y0+(y1-y0)*0.3, s*0.05))
		p.fill(light, rrect(x0, y0+(y1-y0)*0.14, x1, y1, s*0.06))
		p.fill(paper, rrect(x0+s*0.06, y0+(y1-y0)*0.34, x1-s*0.06, y1-s*0.06, s*0.04))
	case Group:
		p.person(mid, W*0.02, H*0.3, W*0.42)
		p.person(mid, W*0.56, H*0.3, W*0.42)
		p.person(ink, W*0.22, H*0.2, W*0.56)
	case Blocked:
		x0, y0 := W*0.5-H*0.3, H*0.1
		p.fill(light, rrect(x0, y0, x0+H*0.6, y0+H*0.8, H*0.06))
		p.fill(paper, rrect(x0+2, y0+2, x0+H*0.6-2, y0+H*0.8-2, H*0.05))
		cx, cy, r := W*0.5, H*0.5, H*0.24
		p.fill(ink, circle(cx, cy, r+H*0.04), reverse(circle(cx, cy, r-H*0.04)))
		d := (r - H*0.02) * float32(math.Sqrt2) / 2
		p.bar(ink, pt{cx - d, cy + d}, pt{cx + d, cy - d}, H*0.08)
	}
	return p.png()
}

// Avatar is a size×size default avatar (PNG): a head and shoulders on
// grey.
func Avatar(size int) []byte {
	p := newPic(size, size)
	s := float32(size)
	p.fill(light, rrect(0, 0, s, s, s*0.12))
	p.person(ink, s*0.12, s*0.16, s*0.76)
	return p.png()
}

// AppIcon is a size×size default app icon (PNG): four tiles on grey.
func AppIcon(size int) []byte {
	p := newPic(size, size)
	s := float32(size)
	p.fill(light, rrect(0, 0, s, s, s*0.2))
	for i := 0; i < 4; i++ {
		x := s*0.24 + float32(i%2)*s*0.29
		y := s*0.24 + float32(i/2)*s*0.29
		c := mid
		if i == 1 {
			c = ink
		}
		p.fill(c, rrect(x, y, x+s*0.23, y+s*0.23, s*0.05))
	}
	return p.png()
}

// Spinner is an 80×80 loading animation (GIF): eight dots round a circle,
// the darkest one going round, eight frames of 100 ms. Every frame redraws
// every dot, so it plays the same with or without disposal. dark draws it
// for a dark background.
func Spinner(dark bool) []byte {
	const size, dots = 80, 8
	shade := func(i int) color.Color { // 0 = the lead dot
		v := uint8(0x55 + i*0x14)
		if dark {
			v = 0xee - uint8(i*0x14)
		}
		return color.NRGBA{v, v, v, 0xff}
	}
	pal := color.Palette{color.NRGBA{}}
	for i := 0; i < dots; i++ {
		pal = append(pal, shade(i))
	}
	g := &gif.GIF{LoopCount: 0}
	for f := 0; f < dots; f++ {
		img := image.NewPaletted(image.Rect(0, 0, size, size), pal)
		for d := 0; d < dots; d++ {
			a := 2*math.Pi*float64(d)/dots - math.Pi/2
			cx, cy := 40+26*math.Cos(a), 40+26*math.Sin(a)
			idx := uint8(1 + (f-d+dots)%dots)
			for y := int(cy - 7); y <= int(cy+7); y++ {
				for x := int(cx - 7); x <= int(cx+7); x++ {
					if math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy) <= 6.5 {
						img.SetColorIndex(x, y, idx)
					}
				}
			}
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 10)
		g.Disposal = append(g.Disposal, gif.DisposalNone)
	}
	var b bytes.Buffer
	_ = gif.EncodeAll(&b, g)
	return b.Bytes()
}
