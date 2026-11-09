package demo

// The demo's tiny vector painter: every picture `clack demo` shows (avatars,
// emoji, the files people share) is drawn here at the size asked for, so
// nothing is downloaded or copied and a 24px avatar is as sharp as a 512px
// one. Shapes are flattened to polygons and filled with x/image/vector's
// anti-aliased rasterizer; text uses the Go fonts that ship in x/image.

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strconv"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// cv is a drawing surface whose coordinates are "units": a picture is
// written once in its own unit space (a 100x100 face, a 1000-wide chart)
// and k scales it to the pixels asked for.
type cv struct {
	img *image.RGBA
	k   float64 // pixels per unit
}

// newCV is a w x h pixel surface with k pixels per unit.
func newCV(w, h int, k float64) *cv {
	return &cv{img: image.NewRGBA(image.Rect(0, 0, max(1, w), max(1, h))), k: k}
}

// png encodes the surface.
func (c *cv) png() []byte {
	var b bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	_ = enc.Encode(&b, c.img)
	return b.Bytes()
}

type pt struct{ x, y float64 }

// shape is a set of closed polygons filled together: overlapping parts
// are one union (each polygon is normalised to the same winding) and a
// polygon marked as a hole cuts out of the rest.
type shape struct {
	polys [][]pt
	holes []bool
}

func (s *shape) add(p []pt) *shape {
	if len(p) >= 3 {
		s.polys = append(s.polys, p)
		s.holes = append(s.holes, false)
	}
	return s
}

// hole turns the last polygon added into a hole.
func (s *shape) hole() *shape {
	if n := len(s.holes); n > 0 {
		s.holes[n-1] = true
	}
	return s
}

// segs is how many segments flatten a curve of radius r (units) when
// drawn at k: enough that a 512px avatar's circle stays round.
func segs(r, k float64) int {
	n := int(math.Ceil(math.Sqrt(r*k) * 4))
	return min(max(n, 12), 256)
}

// ellipseRot is an ellipse rotated by a radians.
func (s *shape) ellipseRot(cx, cy, rx, ry, a float64) *shape {
	n := segs(max(rx, ry), 8)
	p := make([]pt, n)
	ca, sa := math.Cos(a), math.Sin(a)
	for i := range p {
		t := 2 * math.Pi * float64(i) / float64(n)
		x, y := rx*math.Cos(t), ry*math.Sin(t)
		p[i] = pt{cx + x*ca - y*sa, cy + x*sa + y*ca}
	}
	return s.add(p)
}

func (s *shape) ellipse(cx, cy, rx, ry float64) *shape { return s.ellipseRot(cx, cy, rx, ry, 0) }
func (s *shape) circle(cx, cy, r float64) *shape       { return s.ellipse(cx, cy, r, r) }

// ring is a circle outline of width w centred on radius r.
func (s *shape) ring(cx, cy, r, w float64) *shape {
	return s.circle(cx, cy, r+w/2).circle(cx, cy, max(0, r-w/2)).hole()
}

func (s *shape) rect(x, y, w, h float64) *shape {
	return s.add([]pt{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}})
}

// rrect is a rectangle with corner radius r.
func (s *shape) rrect(x, y, w, h, r float64) *shape {
	r = min(r, w/2, h/2)
	if r <= 0 {
		return s.rect(x, y, w, h)
	}
	var p []pt
	corner := func(cx, cy, a0 float64) {
		n := max(3, segs(r, 8)/4)
		for i := 0; i <= n; i++ {
			a := a0 + math.Pi/2*float64(i)/float64(n)
			p = append(p, pt{cx + r*math.Cos(a), cy + r*math.Sin(a)})
		}
	}
	corner(x+w-r, y+r, -math.Pi/2)
	corner(x+w-r, y+h-r, 0)
	corner(x+r, y+h-r, math.Pi/2)
	corner(x+r, y+r, math.Pi)
	return s.add(p)
}

// poly is a polygon through x0,y0,x1,y1…
func (s *shape) poly(xy ...float64) *shape {
	var p []pt
	for i := 0; i+1 < len(xy); i += 2 {
		p = append(p, pt{xy[i], xy[i+1]})
	}
	return s.add(p)
}

// line is a stroke from (x1,y1) to (x2,y2) of width w with round caps.
func (s *shape) line(x1, y1, x2, y2, w float64) *shape {
	dx, dy := x2-x1, y2-y1
	l := math.Hypot(dx, dy)
	if l > 0 {
		nx, ny := -dy/l*w/2, dx/l*w/2
		s.add([]pt{{x1 + nx, y1 + ny}, {x2 + nx, y2 + ny}, {x2 - nx, y2 - ny}, {x1 - nx, y1 - ny}})
	}
	s.circle(x1, y1, w/2)
	return s.circle(x2, y2, w/2)
}

// polyline strokes through x0,y0,x1,y1… with round joins.
func (s *shape) polyline(w float64, xy ...float64) *shape {
	for i := 0; i+3 < len(xy); i += 2 {
		s.line(xy[i], xy[i+1], xy[i+2], xy[i+3], w)
	}
	return s
}

// arc strokes a circular arc from a0 to a1 radians (0 is 3 o'clock,
// increasing clockwise on screen).
func (s *shape) arc(cx, cy, r, a0, a1, w float64) *shape {
	n := max(4, int(float64(segs(r, 8))*math.Abs(a1-a0)/(2*math.Pi)))
	var xy []float64
	for i := 0; i <= n; i++ {
		a := a0 + (a1-a0)*float64(i)/float64(n)
		xy = append(xy, cx+r*math.Cos(a), cy+r*math.Sin(a))
	}
	return s.polyline(w, xy...)
}

// pie is a filled sector from a0 to a1.
func (s *shape) pie(cx, cy, r, a0, a1 float64) *shape {
	n := max(4, int(float64(segs(r, 8))*math.Abs(a1-a0)/(2*math.Pi)))
	p := []pt{{cx, cy}}
	for i := 0; i <= n; i++ {
		a := a0 + (a1-a0)*float64(i)/float64(n)
		p = append(p, pt{cx + r*math.Cos(a), cy + r*math.Sin(a)})
	}
	return s.add(p)
}

// quad strokes a quadratic Bézier from (x0,y0) via (cx,cy) to (x1,y1).
func (s *shape) quad(w, x0, y0, cx, cy, x1, y1 float64) *shape {
	var xy []float64
	for i := 0; i <= 24; i++ {
		t := float64(i) / 24
		u := 1 - t
		xy = append(xy, u*u*x0+2*u*t*cx+t*t*x1, u*u*y0+2*u*t*cy+t*t*y1)
	}
	return s.polyline(w, xy...)
}

// blob is a smooth closed shape through the points x0,y0,x1,y1… (a
// Catmull-Rom loop): hair, clouds, a cat's back.
func (s *shape) blob(xy ...float64) *shape {
	var in []pt
	for i := 0; i+1 < len(xy); i += 2 {
		in = append(in, pt{xy[i], xy[i+1]})
	}
	n := len(in)
	if n < 3 {
		return s
	}
	var p []pt
	for i := 0; i < n; i++ {
		p0, p1, p2, p3 := in[(i+n-1)%n], in[i], in[(i+1)%n], in[(i+2)%n]
		for j := 0; j < 12; j++ {
			t := float64(j) / 12
			t2, t3 := t*t, t*t*t
			f := func(a, b, c, d float64) float64 {
				return 0.5 * (2*b + (-a+c)*t + (2*a-5*b+4*c-d)*t2 + (-a+3*b-3*c+d)*t3)
			}
			p = append(p, pt{f(p0.x, p1.x, p2.x, p3.x), f(p0.y, p1.y, p2.y, p3.y)})
		}
	}
	return s.add(p)
}

// star is a five-pointed (or n-pointed) star.
func (s *shape) star(cx, cy, r, inner float64, n int, rot float64) *shape {
	var p []pt
	for i := 0; i < 2*n; i++ {
		rr := r
		if i%2 == 1 {
			rr = inner
		}
		a := rot - math.Pi/2 + math.Pi*float64(i)/float64(n)
		p = append(p, pt{cx + rr*math.Cos(a), cy + rr*math.Sin(a)})
	}
	return s.add(p)
}

// heart is a heart of width ~2r centred on cx,cy.
func (s *shape) heart(cx, cy, r float64) *shape {
	var p []pt
	for i := 0; i < 96; i++ {
		t := 2 * math.Pi * float64(i) / 96
		x := 16 * math.Pow(math.Sin(t), 3)
		y := 13*math.Cos(t) - 5*math.Cos(2*t) - 2*math.Cos(3*t) - math.Cos(4*t)
		p = append(p, pt{cx + x*r/16, cy - y*r/16 - r*0.1})
	}
	return s.add(p)
}

func area(p []pt) float64 {
	a := 0.0
	for i := range p {
		j := (i + 1) % len(p)
		a += p[i].x*p[j].y - p[j].x*p[i].y
	}
	return a
}

// fill paints the shape in col.
func (c *cv) fill(col color.Color, s *shape) { c.fillSrc(image.NewUniform(col), s) }

// fillSrc paints the shape with src (a gradient, a pattern) in pixel
// space. Only the shape's bounding box is rasterized.
func (c *cv) fillSrc(src image.Image, s *shape) {
	if len(s.polys) == 0 {
		return
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range s.polys {
		for _, q := range p {
			minX, minY = min(minX, q.x*c.k), min(minY, q.y*c.k)
			maxX, maxY = max(maxX, q.x*c.k), max(maxY, q.y*c.k)
		}
	}
	bb := image.Rect(int(math.Floor(minX))-1, int(math.Floor(minY))-1, int(math.Ceil(maxX))+1, int(math.Ceil(maxY))+1).Intersect(c.img.Bounds())
	if bb.Empty() {
		return
	}
	z := vector.NewRasterizer(bb.Dx(), bb.Dy())
	ox, oy := float64(bb.Min.X), float64(bb.Min.Y)
	for i, p := range s.polys {
		// every polygon winds the same way so overlaps add up to a union
		// (the rasterizer clamps |coverage|); holes wind the other way
		rev := (area(p) < 0) != s.holes[i]
		at := func(j int) pt {
			if rev {
				return p[len(p)-1-j]
			}
			return p[j]
		}
		q := at(0)
		z.MoveTo(float32(q.x*c.k-ox), float32(q.y*c.k-oy))
		for j := 1; j < len(p); j++ {
			q = at(j)
			z.LineTo(float32(q.x*c.k-ox), float32(q.y*c.k-oy))
		}
		z.ClosePath()
	}
	z.Draw(c.img, bb, src, bb.Min)
}

// sh starts a shape: c.fill(col, sh().circle(…).rect(…)).
func sh() *shape { return &shape{} }

// bg fills the whole surface.
func (c *cv) bg(col color.Color) {
	draw.Draw(c.img, c.img.Bounds(), image.NewUniform(col), image.Point{}, draw.Src)
}

// grad is a linear gradient between two points in units, for fillSrc.
func (c *cv) grad(x0, y0, x1, y1 float64, from, to color.RGBA) image.Image {
	return &linGrad{x0 * c.k, y0 * c.k, x1 * c.k, y1 * c.k, from, to}
}

type linGrad struct {
	x0, y0, x1, y1 float64
	from, to       color.RGBA
}

func (g *linGrad) ColorModel() color.Model { return color.RGBAModel }
func (g *linGrad) Bounds() image.Rectangle {
	return image.Rect(-1e9, -1e9, 1e9, 1e9)
}
func (g *linGrad) At(x, y int) color.Color {
	dx, dy := g.x1-g.x0, g.y1-g.y0
	t := 0.0
	if d := dx*dx + dy*dy; d > 0 {
		t = ((float64(x)+0.5-g.x0)*dx + (float64(y)+0.5-g.y0)*dy) / d
	}
	return mix(g.from, g.to, min(1, max(0, t)))
}

// ---- colours ----

// hex reads "#rrggbb" (or "rrggbb"); anything else is grey.
func hex(s string) color.RGBA {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 6 {
		return color.RGBA{128, 128, 128, 255}
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
}

// mix blends a toward b by t (0..1).
func mix(a, b color.RGBA, t float64) color.RGBA {
	f := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*(1-t) + float64(y)*t)) }
	return color.RGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), f(a.A, b.A)}
}

// shade darkens (f<0) or lightens (f>0) a colour.
func shade(c color.RGBA, f float64) color.RGBA {
	if f < 0 {
		return mix(c, color.RGBA{0, 0, 0, c.A}, -f)
	}
	return mix(c, color.RGBA{255, 255, 255, c.A}, f)
}

// alpha is c at opacity a (premultiplied, as image/color wants).
func alpha(c color.RGBA, a float64) color.RGBA {
	m := func(x uint8) uint8 { return uint8(math.Round(float64(x) * a)) }
	return color.RGBA{m(c.R), m(c.G), m(c.B), m(c.A)}
}

var (
	white = color.RGBA{255, 255, 255, 255}
	black = color.RGBA{0, 0, 0, 255}
	ink   = color.RGBA{0x1d, 0x1c, 0x1d, 255} // Slack's text colour
)

// ---- text ----

var (
	fontOnce        sync.Once
	fontReg, fontBd *opentype.Font
	faceMu          sync.Mutex
	faces           = map[faceKey]font.Face{}
)

type faceKey struct {
	bold bool
	px   int
}

func face(bold bool, px float64) font.Face {
	fontOnce.Do(func() {
		fontReg, _ = opentype.Parse(goregular.TTF)
		fontBd, _ = opentype.Parse(gobold.TTF)
	})
	k := faceKey{bold, max(1, int(math.Round(px)))}
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faces[k]; ok {
		return f
	}
	f := fontReg
	if bold {
		f = fontBd
	}
	fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(k.px), DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil
	}
	faces[k] = fc
	return fc
}

// Text alignments for cv.text.
const (
	alignLeft = iota
	alignCenter
	alignRight
)

// text draws s with its baseline at y (units), size in units. Faces are
// shared between goroutines, so drawing holds faceMu.
func (c *cv) text(x, y, size float64, col color.Color, s string, align int, bold bool) {
	f := face(bold, size*c.k)
	if f == nil || s == "" {
		return
	}
	faceMu.Lock()
	defer faceMu.Unlock()
	d := font.Drawer{Dst: c.img, Src: image.NewUniform(col), Face: f}
	w := d.MeasureString(s)
	px := x * c.k
	switch align {
	case alignCenter:
		px -= float64(w) / 128
	case alignRight:
		px -= float64(w) / 64
	}
	d.Dot = fixed.Point26_6{X: fixed.Int26_6(px * 64), Y: fixed.Int26_6(y * c.k * 64)}
	d.DrawString(s)
}

// textWidth is s's advance in units at size.
func (c *cv) textWidth(size float64, s string, bold bool) float64 {
	f := face(bold, size*c.k)
	if f == nil {
		return 0
	}
	faceMu.Lock()
	defer faceMu.Unlock()
	return float64(font.MeasureString(f, s)) / 64 / c.k
}

// ---- cache ----

var (
	artMu    sync.Mutex
	artCache = map[string][]byte{}
)

// cached renders key once.
func cached(key string, render func() []byte) []byte {
	artMu.Lock()
	b, ok := artCache[key]
	artMu.Unlock()
	if ok {
		return b
	}
	b = render()
	artMu.Lock()
	artCache[key] = b
	artMu.Unlock()
	return b
}
