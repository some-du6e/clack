package demo

// The pictures people share in the demo's history: a sales chart, the
// late laminator, a cake, the warehouse whiteboard, Mr. Pemberton… Each is
// written once in a unit space 1000 units wide (its height follows the
// requested aspect) and drawn at whatever size the client asks for, so a
// 360px thumb and the 1600px original are the same picture.

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/vector"
)

// fileSizes are the originals' pixel sizes (Build writes these into the
// files' original_w/h and thumbs).
var fileSizes = map[string][2]int{
	"sales-chart":  {1600, 1000},
	"laminator":    {1200, 900},
	"cake":         {1200, 1200},
	"whiteboard":   {1600, 1200},
	"pemberton":    {1200, 1500},
	"big-linda":    {1600, 1200},
	"yogurt-note":  {1000, 1000},
	"ultra-bright": {1200, 900},
	"prinny-error": {1200, 900},
	"honey":        {1200, 900},
	"portal-bug":   {1600, 1000},
	"training-pdf": {612, 792},
}

// fileKeys are the pictures drawFile knows, in a stable order.
var fileKeys = []string{"sales-chart", "laminator", "cake", "whiteboard", "pemberton", "big-linda",
	"yogurt-note", "ultra-bright", "prinny-error", "honey", "portal-bug", "training-pdf"}

var fileArt = map[string]func(c *cv, W, H float64){
	"sales-chart":  artSalesChart,
	"laminator":    artLaminator,
	"cake":         artCake,
	"whiteboard":   artWhiteboard,
	"pemberton":    artPemberton,
	"big-linda":    artBigLinda,
	"yogurt-note":  artYogurtNote,
	"ultra-bright": artUltraBright,
	"prinny-error": artPrinny,
	"honey":        artHoney,
	"portal-bug":   artPortalBug,
	"training-pdf": artTrainingPDF,
}

// drawFile draws picture key at w x h pixels.
func drawFile(key string, w, h int) ([]byte, bool) {
	f, ok := fileArt[key]
	if !ok || w <= 0 || h <= 0 {
		return nil, false
	}
	k := float64(w) / 1000
	c := newCV(w, h, k)
	f(c, 1000, float64(h)/k)
	return c.png(), true
}

// ---- shared bits ----

// rot is a w x h rectangle centred on cx,cy turned by a radians.
func rot(cx, cy, w, h, a float64) *shape {
	ca, sa := math.Cos(a), math.Sin(a)
	p := func(x, y float64) (float64, float64) { return cx + x*ca - y*sa, cy + x*sa + y*ca }
	x0, y0 := p(-w/2, -h/2)
	x1, y1 := p(w/2, -h/2)
	x2, y2 := p(w/2, h/2)
	x3, y3 := p(-w/2, h/2)
	return sh().poly(x0, y0, x1, y1, x2, y2, x3, y3)
}

// gfill fills s with a linear gradient from (x0,y0) to (x1,y1) in units.
// It rasterizes the shape into an alpha mask and composites a gradient
// image through it, which image/draw does on its fast RGBA paths; going
// through fillSrc with a gradient image.Image costs an interface call per
// pixel, and a full-size background took a few hundred milliseconds.
func gfill(c *cv, x0, y0, x1, y1 float64, from, to color.RGBA, s *shape) {
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
	if gradRect(c, s, x0, y0, x1, y1, from, to) {
		return
	}
	z := vector.NewRasterizer(bb.Dx(), bb.Dy())
	ox, oy := float64(bb.Min.X), float64(bb.Min.Y)
	for i, p := range s.polys {
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
	mask := image.NewAlpha(image.Rect(0, 0, bb.Dx(), bb.Dy()))
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	g := image.NewRGBA(bb)
	gx0, gy0, dx, dy := x0*c.k, y0*c.k, (x1-x0)*c.k, (y1-y0)*c.k
	d := dx*dx + dy*dy
	var lut [257]color.RGBA
	for i := range lut {
		lut[i] = mix(from, to, float64(i)/256)
	}
	if d == 0 {
		d = 1
	}
	for y := bb.Min.Y; y < bb.Max.Y; y++ {
		i := g.PixOffset(bb.Min.X, y)
		for x := bb.Min.X; x < bb.Max.X; x++ {
			t := ((float64(x)+0.5-gx0)*dx + (float64(y)+0.5-gy0)*dy) / d
			col := lut[int(min(1, max(0, t))*256)]
			g.Pix[i], g.Pix[i+1], g.Pix[i+2], g.Pix[i+3] = col.R, col.G, col.B, col.A
			i += 4
		}
	}
	draw.DrawMask(c.img, bb, g, bb.Min, mask, image.Point{}, draw.Over)
}

// gradRect is gfill's shortcut for the common case, an opaque gradient
// on one axis-aligned rectangle (a background): it writes the pixels
// directly, with no mask.
func gradRect(c *cv, s *shape, x0, y0, x1, y1 float64, from, to color.RGBA) bool {
	if len(s.polys) != 1 || len(s.polys[0]) != 4 || from.A != 255 || to.A != 255 {
		return false
	}
	p := s.polys[0]
	if p[0].y != p[1].y || p[1].x != p[2].x || p[2].y != p[3].y || p[3].x != p[0].x {
		return false
	}
	r := image.Rect(int(math.Round(min(p[0].x, p[2].x)*c.k)), int(math.Round(min(p[0].y, p[2].y)*c.k)),
		int(math.Round(max(p[0].x, p[2].x)*c.k)), int(math.Round(max(p[0].y, p[2].y)*c.k))).Intersect(c.img.Bounds())
	var lut [257]color.RGBA
	for i := range lut {
		lut[i] = mix(from, to, float64(i)/256)
	}
	gx0, gy0, dx, dy := x0*c.k, y0*c.k, (x1-x0)*c.k, (y1-y0)*c.k
	d := dx*dx + dy*dy
	if d == 0 {
		d = 1
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := c.img.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			t := ((float64(x)+0.5-gx0)*dx + (float64(y)+0.5-gy0)*dy) / d
			col := lut[int(min(1, max(0, t))*256)]
			c.img.Pix[i], c.img.Pix[i+1], c.img.Pix[i+2], c.img.Pix[i+3] = col.R, col.G, col.B, 255
			i += 4
		}
	}
	return true
}

// softShadow is a blurred-looking ellipse of shadow under something.
func softShadow(c *cv, cx, cy, rx, ry, a float64) { radial(c, cx, cy, rx, ry, black, a) }

// glow is a soft round halo.
func glow(c *cv, cx, cy, r float64, col color.RGBA, a float64) { radial(c, cx, cy, r, r, col, a) }

// radial blends col over an ellipse, opacity a at the centre fading
// smoothly to nothing at its edge.
func radial(c *cv, cx, cy, rx, ry float64, col color.RGBA, a float64) {
	pcx, pcy, prx, pry := cx*c.k, cy*c.k, rx*c.k, ry*c.k
	if prx <= 0 || pry <= 0 {
		return
	}
	bb := image.Rect(int(pcx-prx)-1, int(pcy-pry)-1, int(pcx+prx)+2, int(pcy+pry)+2).Intersect(c.img.Bounds())
	for y := bb.Min.Y; y < bb.Max.Y; y++ {
		for x := bb.Min.X; x < bb.Max.X; x++ {
			ddx, ddy := (float64(x)+0.5-pcx)/prx, (float64(y)+0.5-pcy)/pry
			d := ddx*ddx + ddy*ddy
			if d >= 1 {
				continue
			}
			f := 1 - math.Sqrt(d)
			t := a * f * f * (3 - 2*f) // smoothstep
			i := c.img.PixOffset(x, y)
			p := c.img.Pix[i : i+4 : i+4]
			p[0] = uint8(float64(p[0])*(1-t) + float64(col.R)*t)
			p[1] = uint8(float64(p[1])*(1-t) + float64(col.G)*t)
			p[2] = uint8(float64(p[2])*(1-t) + float64(col.B)*t)
			p[3] = uint8(float64(p[3])*(1-t) + 255*t)
		}
	}
}

// sparkle is a four-pointed twinkle.
func sparkle(c *cv, cx, cy, r float64, col color.RGBA) {
	c.fill(col, sh().star(cx, cy, r, r*0.22, 4, 0))
}

// quill is a feather from its nib (x0,y0) to the top of its plume
// (x1,y1), wd wide at its widest.
func quill(c *cv, x0, y0, x1, y1, wd float64, col color.RGBA) {
	dx, dy := x1-x0, y1-y0
	l := math.Hypot(dx, dy)
	ux, uy := dx/l, dy/l
	nx, ny := -uy, ux
	at := func(t, off float64) (float64, float64) { return x0 + dx*t + nx*off, y0 + dy*t + ny*off }
	var xy []float64
	const n = 28
	for i := 0; i <= n; i++ { // one side, nib to top
		t := 0.22 + 0.78*float64(i)/n
		w := wd * math.Pow(math.Sin(math.Pi*(t-0.22)/0.78), 0.65)
		if t > 0.95 {
			w *= 0.8
		}
		x, y := at(t, w)
		xy = append(xy, x, y)
	}
	for i := n; i >= 0; i-- { // the other, narrower, back down
		t := 0.22 + 0.78*float64(i)/n
		w := 0.62 * wd * math.Pow(math.Sin(math.Pi*(t-0.22)/0.78), 0.8)
		x, y := at(t, -w)
		xy = append(xy, x, y)
	}
	gfill(c, x0, y0, x1, y1, shade(col, -0.15), shade(col, 0.25), sh().poly(xy...))
	// barbs
	for i := 1; i < 9; i++ {
		t := 0.3 + 0.08*float64(i)
		ax, ay := at(t, 0)
		bx, by := at(t+0.06, wd*0.85*math.Sin(math.Pi*(t-0.22)/0.78))
		c.fill(alpha(shade(col, -0.35), 0.5), sh().line(ax, ay, bx, by, wd*0.05))
	}
	// shaft and nib
	sx, sy := at(0.02, 0)
	ex, ey := at(0.98, 0)
	c.fill(shade(col, -0.45), sh().line(sx, sy, ex, ey, wd*0.09))
	c.fill(shade(col, -0.6), sh().poly(x0, y0, x0+dx*0.1+nx*wd*0.07, y0+dy*0.1+ny*wd*0.07, x0+dx*0.1-nx*wd*0.07, y0+dy*0.1-ny*wd*0.07))
}

// textLines draws lines lh apart from baseline y.
func textLines(c *cv, x, y, size, lh float64, col color.Color, align int, bold bool, lines ...string) {
	for i, s := range lines {
		c.text(x, y+float64(i)*lh, size, col, s, align, bold)
	}
}

// strike crosses out s drawn centred at x (marker style).
func strike(c *cv, x, y, size float64, col color.RGBA, s string) {
	w := c.textWidth(size, s, true)
	c.fill(col, sh().line(x-w/2-8, y-size*0.25, x+w/2+8, y-size*0.45, size*0.09))
}

// greyLines are placeholder paragraph lines (a document's body text).
func greyLines(c *cv, x, y, w, lh float64, n int, col color.RGBA) {
	for i := 0; i < n; i++ {
		lw := w
		if i == n-1 {
			lw = w * 0.6
		} else if i%3 == 1 {
			lw = w * 0.92
		}
		c.fill(col, sh().rrect(x, y+float64(i)*lh, lw, lh*0.42, lh*0.2))
	}
}

// woodDesk fills from y down with a wood desk top.
func woodDesk(c *cv, W, H, y float64, col color.RGBA) {
	gfill(c, 0, y, 0, H, shade(col, 0.08), shade(col, -0.25), sh().rect(0, y, W, H-y))
	for i := 0; i < 7; i++ {
		yy := y + 14 + float64(i)*(H-y)/7
		c.fill(alpha(shade(col, -0.35), 0.22), sh().quad(3, 0, yy, W/2, yy+9*math.Sin(float64(i)), W, yy-4))
	}
	c.fill(alpha(white, 0.18), sh().rect(0, y, W, 4))
}

// ---- sales-chart ----

func artSalesChart(c *cv, W, H float64) {
	c.bg(hex("#f7f8fa"))
	c.fill(white, sh().rrect(24, 24, W-48, H-48, 18))
	c.text(64, 88, 34, ink, "Reams sold this month", alignLeft, true)
	c.text(64, 122, 19, hex("#6b6f76"), "Wexley Falls branch · inside + outside sales · updated hourly", alignLeft, false)
	x0, x1, y0, y1 := 150.0, 940.0, 230.0, H-120
	top := 450.0
	ty := func(v float64) float64 { return y1 - (y1-y0)*v/top }
	for v := 0.0; v <= 400; v += 100 {
		y := ty(v)
		c.fill(hex("#e6e8ec"), sh().rect(x0, y-0.75, x1-x0, 1.5))
		c.text(x0-14, y+7, 18, hex("#6b6f76"), itoa(int(v)), alignRight, false)
	}
	c.fill(hex("#9aa0a8"), sh().rect(x0, y1-1, x1-x0, 2))
	type bar struct {
		name, note string
		v          float64
	}
	bars := []bar{{"Priya", "", 412}, {"Dale", "", 409}, {"Nadia", "phone orders", 96}, {"Gordon", "does mentoring count", 37}, {"Jordan", "week 2!", 18}}
	slot := (x1 - x0) / float64(len(bars))
	accent := hex("#2f6fde")
	for i, b := range bars {
		cx := x0 + slot*(float64(i)+0.5)
		bw := slot * 0.56
		col := hex("#c3c9d2")
		lab := hex("#4a4f57")
		if i == 0 {
			col, lab = accent, shade(accent, -0.25)
		}
		y := ty(b.v)
		c.fill(col, sh().rrect(cx-bw/2, y, bw, y1-y, 6).rect(cx-bw/2, y1-8, bw, 8))
		c.text(cx, y-12, 22, lab, itoa(int(b.v)), alignCenter, true)
		c.text(cx, y1+34, 21, ink, b.name, alignCenter, i == 0)
		if b.note != "" {
			c.text(cx, y1+60, 16, hex("#80858c"), "("+b.note+")", alignCenter, false)
		}
	}
	// the gap that matters
	px := x0 + slot*0.5
	c.fill(accent, sh().rrect(px-70, ty(412)-84, 140, 32, 16))
	c.text(px, ty(412)-62, 16, white, "lead: 3 reams", alignCenter, true)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// ---- laminator ----

func artLaminator(c *cv, W, H float64) {
	gfill(c, 0, 0, 0, H*0.7, hex("#efe6d6"), hex("#d9ccb6"), sh().rect(0, 0, W, H))
	deskY := H * 0.7
	woodDesk(c, W, H, deskY, hex("#a8754a"))
	// the sign, taped to the wall
	sx, sy, sw, shh := 610.0, 70.0, 300.0, 210.0
	c.fill(alpha(black, 0.12), sh().rect(sx+6, sy+8, sw, shh))
	c.fill(hex("#fbfaf6"), sh().rect(sx, sy, sw, shh))
	c.text(sx+sw/2, sy+88, 46, hex("#2b2b2b"), "RIP LAMBY", alignCenter, true)
	c.text(sx+sw/2, sy+140, 30, hex("#555555"), "2019 - 2026", alignCenter, false)
	c.text(sx+sw/2, sy+182, 18, hex("#777777"), "it laminated so we didn't have to", alignCenter, false)
	tape := alpha(hex("#e8dcb0"), 0.85)
	c.fill(tape, rot(sx+8, sy+6, 80, 26, -0.6))
	c.fill(tape, rot(sx+sw-8, sy+6, 80, 26, 0.6))
	// the laminator
	lx, ly, lw, lh := 220.0, deskY-150, 470.0, 150.0
	softShadow(c, lx+lw/2, deskY+6, lw*0.58, 26, 0.5)
	body := hex("#d7d2c8")
	gfill(c, 0, ly, 0, ly+lh, shade(body, 0.2), shade(body, -0.25), sh().rrect(lx, ly+20, lw, lh-20, 26))
	gfill(c, 0, ly-10, 0, ly+50, shade(body, 0.3), body, sh().rrect(lx+20, ly-10, lw-40, 64, 30))
	// feed slot and the certificate it ate
	c.fill(hex("#2c2a28"), sh().rrect(lx+70, ly+6, lw-140, 12, 6))
	cert := hex("#fffdf4")
	c.fill(alpha(black, 0.15), sh().poly(lx+120, ly+12, lx+150, ly-150, lx+372, ly-128, lx+352, ly+12))
	c.fill(cert, sh().poly(lx+112, ly+12, lx+140, ly-158, lx+366, ly-138, lx+346, ly+12))
	gold := hex("#c99a2e")
	c.fill(gold, sh().poly(lx+146, ly-146, lx+358, ly-128, lx+356, ly-118, lx+145, ly-136))
	c.text(lx+250, ly-96, 20, hex("#5b4a20"), "CERTIFICATE", alignCenter, true)
	c.text(lx+250, ly-70, 13, hex("#7a6a40"), "of Quillie excellence", alignCenter, false)
	c.fill(gold, sh().circle(lx+300, ly-36, 18))
	c.fill(shade(gold, -0.2), sh().ring(lx+300, ly-36, 13, 3))
	// crumple where it went in
	c.fill(hex("#ece6d4"), sh().poly(lx+112, ly+12, lx+130, ly-14, lx+160, ly+2, lx+200, ly-18, lx+240, ly, lx+280, ly-20, lx+320, ly-2, lx+346, ly+12))
	// front panel: dead light, switch, the brand
	c.fill(hex("#3b3a38"), sh().rrect(lx+40, ly+80, 120, 36, 10))
	c.fill(hex("#5a1d1d"), sh().circle(lx+70, ly+98, 9))
	c.fill(hex("#2a2a2a"), sh().rrect(lx+96, ly+90, 46, 16, 5))
	c.text(lx+lw-40, ly+110, 20, shade(body, -0.45), "LAMINATRIX 3000", alignRight, true)
	c.fill(hex("#2c2a28"), sh().rrect(lx+90, ly+lh-22, lw-180, 8, 4))
	// smoke
	for i := 0; i < 3; i++ {
		f := float64(i)
		x := lx + 390 + f*26
		smoke := sh().quad(16-f*3, x, ly+2, x+40, ly-60, x, ly-120).quad(14-f*3, x, ly-120, x-40, ly-180, x-6, ly-250)
		c.fill(alpha(hex("#7d7d7d"), 0.28-0.07*f), smoke)
	}
	// a single flower in a mug, as one does
	mx, my := 790.0, deskY-110
	softShadow(c, mx+45, deskY+4, 60, 12, 0.4)
	c.fill(hex("#2e7d32"), sh().quad(5, mx+45, my, mx+30, my-60, mx+55, my-120))
	c.fill(hex("#43a047"), sh().ellipseRot(mx+34, my-60, 22, 9, -0.6))
	for i := 0; i < 6; i++ {
		a := float64(i) * math.Pi / 3
		c.fill(hex("#f6f1f1"), sh().ellipseRot(mx+55+18*math.Cos(a), my-120+18*math.Sin(a), 16, 9, a))
	}
	c.fill(hex("#f2c230"), sh().circle(mx+55, my-120, 10))
	gfill(c, mx, 0, mx+90, 0, hex("#2f5d8c"), hex("#4b7fb5"), sh().rrect(mx, my, 90, 110, 12))
	c.fill(hex("#2f5d8c"), sh().ring(mx+98, my+50, 22, 10))
}

// ---- cake ----

func artCake(c *cv, W, H float64) {
	c.bg(hex("#e9f1f7"))
	for x := 0.0; x < W; x += 80 { // a gingham tablecloth
		c.fill(alpha(hex("#9fc3e0"), 0.35), sh().rect(x, 0, 40, H))
	}
	for y := 0.0; y < H; y += 80 {
		c.fill(alpha(hex("#9fc3e0"), 0.35), sh().rect(0, y, W, 40))
	}
	cx, cy := W/2, H*0.5
	softShadow(c, cx, cy+330, 470, 70, 0.45)
	c.fill(hex("#d9dde2"), sh().rrect(cx-460, cy-310, 920, 660, 30))
	c.fill(hex("#f4f6f8"), sh().rrect(cx-450, cy-318, 900, 640, 28))
	// the cake: a side, then the top
	sideC := hex("#f0d9b5")
	gfill(c, 0, cy+170, 0, cy+260, sideC, shade(sideC, -0.2), sh().rrect(cx-400, cy-250, 800, 510, 40))
	top := hex("#fffaf0")
	c.fill(top, sh().rrect(cx-400, cy-260, 800, 440, 40))
	// piping all round
	pip := hex("#7fb8e6")
	for x := cx - 370; x <= cx+370; x += 37 {
		c.fill(pip, sh().circle(x, cy-250, 15))
		c.fill(pip, sh().circle(x, cy+172, 15))
	}
	for y := cy - 215; y <= cy+140; y += 37 {
		c.fill(pip, sh().circle(cx-392, y, 15))
		c.fill(pip, sh().circle(cx+392, y, 15))
	}
	c.fill(alpha(white, 0.5), sh().circle(cx-370, cy-256, 5))
	// writing
	pink := hex("#d6457f")
	c.text(cx, cy-40, 74, pink, "CONGRATS", alignCenter, true)
	c.text(cx, cy+50, 56, hex("#3b6fb5"), "QUILLIE WINNERS", alignCenter, true)
	// rosettes
	for _, p := range [][2]float64{{cx - 310, cy - 170}, {cx + 310, cy - 170}, {cx - 310, cy + 110}} {
		c.fill(hex("#f39bbd"), sh().star(p[0], p[1], 30, 20, 8, 0))
		c.fill(hex("#e2669b"), sh().circle(p[0], p[1], 10))
	}
	// the gold quill
	quill(c, cx+100, cy+165, cx+375, cy-5, 32, hex("#d9a92e"))
	// candles
	for i := 0; i < 5; i++ {
		x := cx - 200 + float64(i)*100
		y := cy - 130
		cols := []string{"#e57373", "#64b5f6", "#81c784", "#ffd54f", "#ba68c8"}
		col := hex(cols[i])
		c.fill(alpha(black, 0.12), sh().ellipse(x+6, y+6, 16, 7))
		gfill(c, x-10, 0, x+10, 0, shade(col, 0.15), shade(col, -0.15), sh().rect(x-10, y-110, 20, 110))
		for s := 0.0; s < 110; s += 26 {
			c.fill(alpha(white, 0.65), sh().poly(x-10, y-s-6, x+10, y-s-16, x+10, y-s-24, x-10, y-s-14))
		}
		c.fill(hex("#333333"), sh().line(x, y-110, x, y-122, 2.5))
		glow(c, x, y-138, 34, hex("#ffcc66"), 0.5)
		c.fill(hex("#ffb300"), sh().blob(x, y-162, x+10, y-138, x, y-122, x-10, y-138))
		c.fill(hex("#fff3c4"), sh().blob(x, y-148, x+5, y-134, x, y-126, x-5, y-134))
	}
}

// ---- whiteboard ----

func artWhiteboard(c *cv, W, H float64) {
	gfill(c, 0, 0, 0, H, hex("#9aa2a8"), hex("#7d858b"), sh().rect(0, 0, W, H))
	bx, by, bw, bh := 40.0, 36.0, W-80, H-100
	c.fill(alpha(black, 0.25), sh().rrect(bx+6, by+10, bw, bh, 10))
	gfill(c, bx, by, bx+bw, by+bh, hex("#d9dde0"), hex("#a9afb4"), sh().rrect(bx, by, bw, bh, 10))
	gfill(c, bx, by, bx+bw, by+bh, hex("#ffffff"), hex("#eef1f3"), sh().rect(bx+14, by+14, bw-28, bh-28))
	c.fill(alpha(white, 0.6), sh().poly(bx+600, by+14, bx+700, by+14, bx+420, by+bh-14, bx+320, by+bh-14))
	blue, red, blk, grn := hex("#1f4fbf"), hex("#d23a2a"), hex("#222222"), hex("#1f8a4c")
	c.text(80, 112, 32, blk, "DAYS SINCE LAST FORKLIFT INCIDENT:", alignLeft, true)
	nums := []string{"14", "6", "11", "0"}
	for i, n := range nums {
		x := 110 + float64(i)*86
		c.text(x, 214, 50, blk, n, alignCenter, true)
		strike(c, x, 214, 50, red, n)
	}
	c.text(520, 238, 104, grn, "3", alignCenter, true)
	c.fill(grn, sh().ring(522, 202, 58, 6))
	c.text(600, 214, 22, grn, "(Big Linda was", alignLeft, false)
	c.text(600, 240, 22, grn, " not involved)", alignLeft, false)
	// schedule
	c.text(80, 312, 30, blue, "SHIFT SCHEDULE", alignLeft, true)
	c.fill(blue, sh().line(80, 322, 330, 320, 4))
	rows := [][2]string{{"MON", "Wendell / Dez"}, {"TUE", "Wendell / Rosa"}, {"WED", "Dez / Rosa"}, {"THU", "ALL HANDS (inventory)"}, {"FRI", "Wendell + DENISE VISIT ??"}}
	for i, r := range rows {
		y := 370 + float64(i)*50
		c.text(84, y, 24, blk, r[0], alignLeft, true)
		c.text(170, y, 24, blk, r[1], alignLeft, false)
		c.fill(alpha(blk, 0.5), sh().line(80, y+14, 520, y+12, 1.6))
	}
	c.fill(alpha(blk, 0.5), sh().line(156, 340, 156, 604, 1.6))
	// Big Linda's service
	c.fill(red, sh().rrect(620, 290, 300, 92, 8).rrect(626, 296, 288, 80, 6).hole())
	c.text(770, 328, 26, red, "BIG LINDA", alignCenter, true)
	c.text(770, 362, 24, red, "SERVICE THURS", alignCenter, true)
	// a message upstairs
	c.text(620, 460, 26, blue, "UPSTAIRS:", alignLeft, true)
	c.text(620, 494, 24, blue, "STOP TAKING", alignLeft, true)
	c.text(620, 526, 24, blue, "OUR PALLETS", alignLeft, true)
	c.fill(blue, sh().line(620, 538, 790, 536, 3.5))
	c.fill(blue, sh().line(624, 548, 780, 547, 3.5))
	// marker forklift
	fx, fy, q := 800.0, 600.0, 0.62
	pts := func(xy ...float64) []float64 {
		out := make([]float64, len(xy))
		for i := range xy {
			if i%2 == 0 {
				out[i] = fx + xy[i]*q
			} else {
				out[i] = fy + xy[i]*q
			}
		}
		return out
	}
	m := sh()
	m.polyline(3.5, pts(40, 80, 40, 20, 80, 20, 100, 50, 170, 50, 170, 80, 40, 80)...)
	m.polyline(3.5, pts(50, 20, 56, -20, 96, -20, 100, 50)...)
	m.polyline(3.5, pts(20, -30, 20, 92, -30, 92)...)
	w1, w2 := pts(70, 92, 150, 92), 16*q
	m.ring(w1[0], w1[1], w2, 3.5).ring(w1[2], w1[3], w2, 3.5)
	c.fill(blk, m)
	c.fill(red, sh().heart(fx+150, fy-20, 16))
	// tray and markers
	c.fill(hex("#b9bfc4"), sh().rrect(bx+120, by+bh-6, bw-240, 22, 6))
	for i, col := range []color.RGBA{blue, red, blk, grn} {
		x := bx + 240 + float64(i)*120
		c.fill(col, sh().rrect(x, by+bh-14, 90, 16, 7))
		c.fill(shade(col, 0.6), sh().rrect(x+50, by+bh-14, 22, 16, 3))
	}
}

// ---- pemberton ----

func artPemberton(c *cv, W, H float64) {
	gfill(c, 0, 0, 0, H*0.72, hex("#ece6db"), hex("#dcd3c4"), sh().rect(0, 0, W, H))
	floorY := H * 0.72
	gfill(c, 0, floorY, 0, H, hex("#8f8a80"), hex("#6f6a62"), sh().rect(0, floorY, W, H-floorY))
	for x := -100.0; x < W+100; x += 40 { // carpet texture
		c.fill(alpha(black, 0.05), sh().line(x, floorY, x+60, H, 3))
	}
	c.fill(hex("#c8bfae"), sh().rect(0, floorY-18, W, 18))
	cx := W / 2
	boxTop, boxBot := 760.0, 1130.0
	softShadow(c, cx, boxBot+10, 380, 46, 0.5)
	card := hex("#c9995d")
	// back of the box and its flaps
	c.fill(shade(card, -0.35), sh().poly(cx-300, boxTop, cx+300, boxTop, cx+260, boxTop-40, cx-260, boxTop-40))
	c.fill(shade(card, 0.05), sh().poly(cx-300, boxTop, cx-260, boxTop-40, cx-420, boxTop-90, cx-430, boxTop+30))
	c.fill(shade(card, -0.08), sh().poly(cx+300, boxTop, cx+260, boxTop-40, cx+420, boxTop-90, cx+430, boxTop+30))
	grey := hex("#8e9196")
	dark := hex("#5b5e63")
	// tail curling over the side
	c.fill(grey, sh().quad(46, cx+250, boxTop+10, cx+420, boxTop-60, cx+380, boxTop-230))
	for i := 0; i < 4; i++ {
		t := 0.3 + 0.18*float64(i)
		x := (1-t)*(1-t)*(cx+250) + 2*(1-t)*t*(cx+420) + t*t*(cx+380)
		y := (1-t)*(1-t)*(boxTop+10) + 2*(1-t)*t*(boxTop-60) + t*t*(boxTop-230)
		c.fill(dark, sh().ellipseRot(x, y, 24, 7, -0.3))
	}
	// body
	gfill(c, 0, 470, 0, boxTop, shade(grey, 0.12), shade(grey, -0.15), sh().blob(cx-250, boxTop+20, cx-240, 560, cx, 470, cx+240, 560, cx+250, boxTop+20))
	c.fill(hex("#d9d6d0"), sh().ellipse(cx, 640, 120, 130)) // chest fluff
	for i := 0; i < 3; i++ {
		y := 560.0 + float64(i)*50
		c.fill(alpha(dark, 0.7), sh().quad(16, cx-235, y+10, cx-200, y-10, cx-150, y+20))
		c.fill(alpha(dark, 0.7), sh().quad(16, cx+235, y+10, cx+200, y-10, cx+150, y+20))
	}
	// front of the box
	gfill(c, 0, boxTop, 0, boxBot, card, shade(card, -0.18), sh().rect(cx-300, boxTop, 600, boxBot-boxTop))
	c.fill(shade(card, 0.15), sh().rect(cx-300, boxTop, 600, 10))
	c.fill(alpha(hex("#2f5d8c"), 0.75), sh().rect(cx-300, boxTop+120, 600, 70))
	c.text(cx, boxTop+168, 40, alpha(white, 0.92), "ULTRA BRIGHT 24 lb", alignCenter, true)
	c.text(cx, boxTop+250, 26, alpha(hex("#4a3520"), 0.7), "QUILLFEATHER PAPER CO.", alignCenter, true)
	c.text(cx, boxTop+290, 22, alpha(hex("#4a3520"), 0.6), "10 REAMS · THIS SIDE UP", alignCenter, false)
	c.fill(alpha(hex("#4a3520"), 0.55), sh().poly(cx-240, boxTop+310, cx-220, boxTop+280, cx-200, boxTop+310).rect(cx-226, boxTop+305, 12, 30))
	// paws on the rim
	for _, x := range []float64{cx - 110, cx + 110} {
		c.fill(grey, sh().ellipse(x, boxTop+4, 58, 34))
		for j := -1; j <= 1; j++ {
			c.fill(alpha(dark, 0.6), sh().line(x+float64(j)*20, boxTop+20, x+float64(j)*20, boxTop+34, 4))
		}
	}
	// head
	hx, hy := cx, 420.0
	c.fill(grey, sh().poly(hx-170, hy-60, hx-150, hy-230, hx-50, hy-140))
	c.fill(grey, sh().poly(hx+170, hy-60, hx+150, hy-230, hx+50, hy-140))
	c.fill(hex("#d9a3a0"), sh().poly(hx-150, hy-90, hx-140, hy-195, hx-80, hy-140))
	c.fill(hex("#d9a3a0"), sh().poly(hx+150, hy-90, hx+140, hy-195, hx+80, hy-140))
	gfill(c, 0, hy-170, 0, hy+170, shade(grey, 0.15), shade(grey, -0.08), sh().blob(hx-200, hy+40, hx-170, hy-120, hx, hy-175, hx+170, hy-120, hx+200, hy+40, hx+120, hy+150, hx, hy+165, hx-120, hy+150))
	// forehead stripes
	c.fill(dark, sh().quad(12, hx-50, hy-160, hx-40, hy-110, hx-22, hy-80))
	c.fill(dark, sh().quad(12, hx, hy-172, hx+2, hy-120, hx, hy-86))
	c.fill(dark, sh().quad(12, hx+50, hy-160, hx+40, hy-110, hx+22, hy-80))
	c.fill(dark, sh().quad(9, hx-196, hy+50, hx-170, hy+40, hx-150, hy+58))
	c.fill(dark, sh().quad(9, hx+196, hy+50, hx+170, hy+40, hx+150, hy+58))
	// muzzle
	c.fill(hex("#e2dfda"), sh().ellipse(hx-45, hy+70, 62, 48).ellipse(hx+45, hy+70, 62, 48))
	// smug, half-closed eyes
	for _, s := range []float64{-1, 1} {
		ex, ey := hx+s*78, hy-10
		c.fill(hex("#9cc23a"), sh().ellipse(ex, ey, 40, 26))
		c.fill(hex("#1b1b1b"), sh().ellipse(ex, ey+2, 7, 22))
		// the heavy upper lid, in the head's own gradient
		gfill(c, 0, hy-170, 0, hy+170, shade(grey, 0.15), shade(grey, -0.08), sh().rect(ex-46, ey-34, 92, 34))
		c.fill(dark, sh().line(ex-40, ey, ex+40, ey-2, 6))
	}
	// nose and a smirk
	c.fill(hex("#c97b84"), sh().poly(hx-20, hy+30, hx+20, hy+30, hx, hy+52))
	m := sh().quad(5, hx, hy+52, hx-10, hy+82, hx-38, hy+76).quad(5, hx, hy+52, hx+14, hy+80, hx+46, hy+68)
	c.fill(dark, m)
	for _, s := range []float64{-1, 1} {
		for j := 0; j < 3; j++ {
			y := hy + 60 + float64(j)*16
			c.fill(alpha(white, 0.85), sh().line(hx+s*90, y, hx+s*250, y-30+float64(j)*22, 2.5))
		}
	}
}

// ---- big-linda ----

func artBigLinda(c *cv, W, H float64) {
	floorY := H * 0.7
	gfill(c, 0, 0, 0, floorY, hex("#55606b"), hex("#3e4750"), sh().rect(0, 0, W, floorY))
	// racks of paper behind
	rack := hex("#d9662b")
	for _, ry := range []float64{170, 330, 490} {
		c.fill(rack, sh().rect(0, ry, W, 14))
	}
	for x := 20.0; x < W; x += 240 {
		c.fill(hex("#2f6fa8"), sh().rect(x, 40, 14, floorY-40))
		for _, ry := range []float64{170, 330, 490} {
			for b := 0; b < 4; b++ {
				bx := x + 26 + float64(b)*50
				if bx+46 > x+240 {
					break
				}
				box := hex("#c9995d")
				if (b+int(ry))%3 == 0 {
					box = hex("#e8e4dc")
				}
				c.fill(shade(box, -0.05), sh().rect(bx, ry-110, 46, 110))
				c.fill(shade(box, -0.25), sh().rect(bx, ry-110, 46, 10))
			}
		}
	}
	c.fill(alpha(black, 0.25), sh().rect(0, 0, W, floorY))
	// floor
	gfill(c, 0, floorY, 0, H, hex("#a7a9aa"), hex("#8a8c8d"), sh().rect(0, floorY, W, H-floorY))
	c.fill(hex("#e2c02c"), sh().poly(0, floorY+120, W, floorY+90, W, floorY+104, 0, floorY+136))
	for i := 0; i < 8; i++ {
		c.fill(alpha(black, 0.05), sh().ellipse(float64(i)*140+60, floorY+60+float64(i%3)*40, 70, 10))
	}
	// the forklift (facing left)
	yel := hex("#f2b705")
	softShadow(c, 480, floorY+80, 360, 40, 0.6)
	// mast and forks
	c.fill(hex("#3a3a3a"), sh().rect(216, 170, 22, floorY-110).rect(252, 170, 22, floorY-110))
	c.fill(hex("#555555"), sh().rect(214, 330, 62, 14).rect(214, 200, 62, 14))
	c.fill(hex("#2a2a2a"), sh().rect(90, floorY+30, 160, 16).rect(232, floorY-60, 18, 106))
	// body
	gfill(c, 0, 400, 0, floorY+60, shade(yel, 0.15), shade(yel, -0.2),
		sh().poly(290, floorY+50, 290, 420, 440, 400, 560, 420, 760, 430, 790, 470, 790, floorY+50))
	c.fill(shade(yel, -0.25), sh().rrect(640, 420, 160, floorY+30-420, 24)) // counterweight
	c.fill(hex("#222222"), sh().rect(652, 440, 136, 30))
	for i := 0; i < 5; i++ {
		x := 652 + float64(i)*28
		c.fill(yel, sh().poly(x, 470, x+14, 470, x+28, 440, x+14, 440))
	}
	c.text(470, 470, 32, hex("#2a2a2a"), "BIG LINDA", alignCenter, true)
	// overhead guard, seat, steering
	g := hex("#2b2b2b")
	c.fill(g, sh().line(320, 420, 340, 190, 12).line(620, 425, 600, 190, 12).line(330, 190, 612, 190, 16))
	c.fill(hex("#262626"), sh().rrect(545, 320, 52, 96, 16).rrect(500, 392, 110, 26, 10))
	c.fill(hex("#1f1f1f"), sh().line(400, 404, 430, 320, 9))
	c.fill(hex("#333333"), sh().ellipseRot(430, 318, 34, 10, -0.3))
	// a pallet of paper on the forks
	c.fill(hex("#a77b4f"), sh().rect(96, floorY-2, 136, 10).rect(96, floorY+20, 136, 10).rect(98, floorY+6, 16, 16).rect(156, floorY+6, 16, 16).rect(214, floorY+6, 16, 16))
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			bx, by := 100+float64(i)*66, floorY-58-float64(j)*58
			box := hex("#e8e4dc")
			if (i+j)%2 == 1 {
				box = hex("#c9995d")
			}
			c.fill(box, sh().rect(bx, by, 62, 56))
			c.fill(shade(box, -0.2), sh().rect(bx, by, 62, 8))
			c.fill(alpha(hex("#2f6fde"), 0.8), sh().rect(bx, by+24, 62, 10))
		}
	}
	// wheels
	for _, w := range [][3]float64{{370, floorY + 40, 80}, {700, floorY + 50, 66}} {
		c.fill(hex("#1a1a1a"), sh().circle(w[0], w[1], w[2]))
		c.fill(hex("#9a9a9a"), sh().circle(w[0], w[1], w[2]*0.5))
		c.fill(hex("#cfcfcf"), sh().circle(w[0], w[1], w[2]*0.22))
	}
	// the big red bow on the roof
	bx, by := 470.0, 150.0
	red := hex("#d62828")
	c.fill(shade(red, -0.2), sh().poly(bx-10, by+10, bx-80, by+90, bx-50, by+96, bx-2, by+20))
	c.fill(shade(red, -0.2), sh().poly(bx+10, by+10, bx+80, by+90, bx+50, by+96, bx+2, by+20))
	c.fill(red, sh().ellipseRot(bx-70, by-20, 76, 38, -0.35).ellipseRot(bx+70, by-20, 76, 38, 0.35))
	c.fill(shade(red, -0.35), sh().ellipseRot(bx-70, by-20, 40, 16, -0.35).ellipseRot(bx+70, by-20, 40, 16, 0.35))
	c.fill(shade(red, -0.1), sh().circle(bx, by-6, 26))
	c.fill(alpha(white, 0.3), sh().ellipseRot(bx-90, by-38, 24, 8, -0.35))
	c.fill(red, sh().rect(330, by+30, 280, 16))
}

// ---- yogurt-note ----

func artYogurtNote(c *cv, W, H float64) {
	gfill(c, 0, 0, W, H, hex("#f2f3f1"), hex("#d6d9d6"), sh().rect(0, 0, W, H))
	c.fill(alpha(white, 0.5), sh().poly(120, 0, 260, 0, 40, H, -100, H))
	c.fill(hex("#b9bdb9"), sh().rect(0, 60, W, 6))
	gfill(c, 890, 0, 950, 0, hex("#c8ccc8"), hex("#8f948f"), sh().rrect(890, 140, 50, 640, 25))
	// other magnets
	c.fill(hex("#2f78c4"), sh().circle(150, 160, 30))
	c.fill(hex("#3fa34d"), sh().rrect(100, 820, 220, 90, 10))
	c.text(210, 860, 22, white, "WEXLEY FALLS", alignCenter, true)
	c.text(210, 890, 20, white, "PIZZA  555-0142", alignCenter, false)
	// the note
	nx, ny, nw := 240.0, 230.0, 520.0
	c.fill(alpha(black, 0.18), sh().poly(nx+14, ny+18, nx+nw+16, ny+12, nx+nw+8, ny+nw+20, nx+18, ny+nw+26))
	note := hex("#ffe55c")
	gfill(c, 0, ny, 0, ny+nw, shade(note, 0.15), note, sh().poly(nx, ny, nx+nw, ny, nx+nw, ny+nw-60, nx+nw-60, ny+nw, nx, ny+nw))
	c.fill(shade(note, -0.12), sh().poly(nx+nw, ny+nw-60, nx+nw-60, ny+nw, nx+nw-50, ny+nw-50))
	c.fill(alpha(shade(note, -0.25), 0.4), sh().rect(nx, ny, nw, 40))
	mk := hex("#1c1c1c")
	textLines(c, nx+nw/2, ny+130, 46, 64, mk, alignCenter, true, "WHOEVER TOOK", "MY STRAWBERRY", "YOGURT:")
	c.text(nx+nw/2, ny+352, 70, hex("#c62828"), "I KNOW.", alignCenter, true)
	c.fill(hex("#c62828"), sh().line(nx+110, ny+372, nx+410, ny+366, 6))
	c.text(nx+nw-70, ny+440, 38, mk, "— N", alignRight, true)
	// the magnet holding it
	glow(c, nx+nw/2+6, ny+16, 60, black, 0.25)
	c.fill(hex("#e53935"), sh().circle(nx+nw/2, ny+6, 44))
	c.fill(shade(hex("#e53935"), -0.2), sh().ring(nx+nw/2, ny+6, 40, 6))
	c.fill(alpha(white, 0.55), sh().ellipseRot(nx+nw/2-16, ny-10, 16, 9, -0.6))
}

// ---- ultra-bright ----

func artUltraBright(c *cv, W, H float64) {
	gfill(c, 0, 0, 0, H, hex("#1c3d6e"), hex("#0f2342"), sh().rect(0, 0, W, H))
	for i := 0; i < 16; i++ { // rays
		a := float64(i) * math.Pi / 8
		c.fill(alpha(hex("#8fc1ff"), 0.08), sh().pie(500, 400, 900, a, a+0.12))
	}
	glow(c, 500, 410, 420, hex("#fff7d6"), 0.75)
	softShadow(c, 540, 650, 380, 46, 0.6)
	// the ream: front, top and side
	fx0, fy0, fx1, fy1 := 210.0, 280.0, 700.0, 640.0
	dx, dy := 110.0, -80.0
	c.fill(hex("#f6f6f2"), sh().poly(fx0, fy0, fx1, fy0, fx1+dx, fy0+dy, fx0+dx, fy0+dy))
	gfill(c, fx1, 0, fx1+dx, 0, hex("#d8dbe0"), hex("#bfc4cc"), sh().poly(fx1, fy0, fx1+dx, fy0+dy, fx1+dx, fy1+dy, fx1, fy1))
	gfill(c, 0, fy0, 0, fy1, hex("#ffffff"), hex("#e9ecef"), sh().rect(fx0, fy0, fx1-fx0, fy1-fy0))
	// wrapper fold on top
	c.fill(alpha(hex("#9aa3ad"), 0.6), sh().line(fx0+dx/2, fy0+dy/2, fx1+dx/2, fy0+dy/2, 3))
	band := hex("#2f6fde")
	c.fill(band, sh().rect(fx0, fy0+50, fx1-fx0, 104))
	c.fill(shade(band, -0.3), sh().poly(fx1, fy0+50, fx1+dx, fy0+50+dy, fx1+dx, fy0+154+dy, fx1, fy0+154))
	c.text((fx0+fx1)/2, fy0+122, 56, white, "ULTRA BRIGHT", alignCenter, true)
	c.text((fx0+fx1)/2, fy0+214, 44, hex("#1d2b45"), "24 lb", alignCenter, true)
	c.text((fx0+fx1)/2, fy0+252, 20, hex("#4a5568"), "8.5 x 11 in · 500 sheets · 99 bright", alignCenter, false)
	c.fill(hex("#e3e7ee"), sh().rect(fx0, fy1-74, fx1-fx0, 2))
	quill(c, fx0+120, fy1-18, fx0+156, fy1-62, 14, hex("#2f6fde"))
	c.text(fx0+170, fy1-28, 22, hex("#1d2b45"), "QUILLFEATHER PAPER CO.", alignLeft, true)
	// sparkle
	for _, s := range [][3]float64{{180, 230, 40}, {820, 200, 54}, {860, 560, 30}, {140, 600, 26}, {690, 140, 24}, {360, 200, 18}} {
		sparkle(c, s[0], s[1], s[2], hex("#fffbe6"))
	}
}

// ---- prinny-error ----

func artPrinny(c *cv, W, H float64) {
	gfill(c, 0, 0, 0, H*0.72, hex("#dfe7e2"), hex("#c7d2cc"), sh().rect(0, 0, W, H))
	deskY := H * 0.72
	gfill(c, 0, deskY, 0, H, hex("#b8bcc0"), hex("#8e9399"), sh().rect(0, deskY, W, H-deskY))
	softShadow(c, 500, deskY+8, 330, 26, 0.5)
	body := hex("#e4e5e7")
	px0, px1, py0 := 220.0, 780.0, 230.0
	gfill(c, 0, py0, 0, deskY, shade(body, 0.1), shade(body, -0.22), sh().rrect(px0, py0, px1-px0, deskY-py0+4, 26))
	c.fill(hex("#3c3f44"), sh().rrect(px0-10, py0-30, px1-px0+20, 60, 18)) // scanner lid
	c.fill(hex("#55595f"), sh().rrect(px0+10, py0-22, px1-px0-20, 10, 5))
	// display
	c.fill(hex("#2a2d31"), sh().rrect(520, 270, 230, 92, 12))
	c.fill(hex("#9fd3c7"), sh().rrect(532, 282, 206, 68, 6))
	c.text(545, 310, 22, hex("#13312b"), "ERROR 7:", alignLeft, true)
	c.text(545, 338, 22, hex("#13312b"), "NOT TODAY", alignLeft, true)
	// the orange light
	glow(c, 480, 300, 46, hex("#ff9800"), 0.6)
	c.fill(hex("#ff9800"), sh().circle(480, 300, 13))
	c.fill(alpha(white, 0.7), sh().circle(476, 296, 4))
	// buttons
	for i := 0; i < 3; i++ {
		c.fill(hex("#9aa0a6"), sh().circle(580+float64(i)*50, 392, 13))
	}
	c.text(260, 300, 34, hex("#6b7078"), "PR-7", alignLeft, true)
	c.fill(hex("#fbfbf7"), sh().rrect(256, 320, 120, 36, 4))
	c.text(316, 346, 22, hex("#2b5fb0"), "Prinny", alignCenter, true)
	// output tray, slot and the jam: paper folded like an accordion
	// hanging out of the slot, with a crumpled corner poking up
	c.fill(hex("#9aa0a6"), sh().rrect(290, 598, 420, 16, 6))
	c.fill(hex("#fafaf6"), sh().rect(320, 592, 360, 6))
	c.fill(hex("#2b2d31"), sh().rrect(270, 440, 460, 18, 8))
	paper := hex("#fafaf6")
	fold := hex("#d9dad4")
	c.fill(paper, sh().poly(400, 452, 410, 414, 432, 430, 450, 404, 470, 426, 492, 408, 510, 436, 520, 452))
	c.fill(fold, sh().poly(432, 430, 450, 404, 456, 440))
	for i := 0; i < 5; i++ {
		f := float64(i)
		y := 452 + f*26
		l, r := 340+float64(i%2)*16-f*6, 660-float64(i%2)*12+f*8
		l2, r2 := 340+float64((i+1)%2)*16-(f+1)*6, 660-float64((i+1)%2)*12+(f+1)*8
		col := paper
		if i%2 == 1 {
			col = fold
		}
		c.fill(alpha(black, 0.12), sh().poly(l+6, y+4, r+6, y+4, r2+6, y+30, l2+6, y+30))
		c.fill(col, sh().poly(l, y, r, y, r2, y+26, l2, y+26))
		if i%2 == 0 {
			c.fill(alpha(hex("#9aa0a6"), 0.45), sh().rect((l+r)/2-90, y+9, 150, 5))
		}
	}
	c.fill(paper, sh().poly(310, 582, 330, 590, 360, 578, 400, 592, 450, 580, 500, 594, 550, 580, 600, 592, 650, 578, 692, 590, 700, 582, 690, 560, 320, 560))
	c.fill(hex("#2b2d31"), sh().rrect(270, 440, 130, 18, 8).rrect(520, 440, 210, 18, 8))
}

// shift offsets every point of xy by dx,dy.
func shift(xy []float64, dx, dy float64) []float64 {
	out := make([]float64, len(xy))
	for i := range xy {
		if i%2 == 0 {
			out[i] = xy[i] + dx
		} else {
			out[i] = xy[i] + dy
		}
	}
	return out
}

// ---- honey ----

func artHoney(c *cv, W, H float64) {
	gfill(c, 0, 0, 0, H*0.68, hex("#f6ead2"), hex("#ead7b1"), sh().rect(0, 0, W, H))
	deskY := H * 0.68
	woodDesk(c, W, H, deskY, hex("#8a5a35"))
	jar := func(x, w, h float64, label string) {
		y := deskY - h
		softShadow(c, x+w/2, deskY+8, w*0.62, 18, 0.5)
		glass := sh().rrect(x, y+40, w, h-40, w*0.16)
		gfill(c, x, 0, x+w, 0, hex("#e08a12"), hex("#b55d05"), glass)
		c.fill(alpha(hex("#ffd36b"), 0.45), sh().rrect(x+w*0.12, y+60, w*0.14, h-110, 8))
		c.fill(alpha(white, 0.25), sh().rrect(x+w*0.72, y+70, w*0.06, h-150, 6))
		c.fill(hex("#d8c7a0"), sh().rrect(x+6, y+30, w-12, 20, 6)) // the rim
		// gingham lid cloth
		lid := sh().blob(x-14, y+40, x+w/2, y-6, x+w+14, y+40, x+w+4, y+60, x+w/2, y+50, x-4, y+60)
		c.fill(hex("#c62828"), lid)
		for i := 1; i < 5; i++ {
			c.fill(alpha(white, 0.55), sh().circle(x+float64(i)*w/5, y+24+math.Sin(float64(i))*5, 5))
		}
		c.fill(hex("#7a4b1c"), sh().rect(x-2, y+48, w+4, 8))
		// label
		lx, ly, lw, lh := x+w*0.1, y+h*0.45, w*0.8, h*0.36
		c.fill(hex("#fbf5e6"), sh().rrect(lx, ly, lw, lh, 10))
		c.fill(hex("#3d2a12"), sh().rrect(lx+6, ly+6, lw-12, lh-12, 7).rrect(lx+9, ly+9, lw-18, lh-18, 5).hole())
		sz := lw / 10.5
		c.text(lx+lw/2, ly+lh*0.36, sz, hex("#3d2a12"), "HURST FAMILY", alignCenter, true)
		c.text(lx+lw/2, ly+lh*0.36+sz*1.25, sz, hex("#3d2a12"), "APIARY", alignCenter, true)
		c.text(lx+lw/2, ly+lh*0.36+sz*2.5, sz*0.8, hex("#8a6a3a"), label, alignCenter, false)
		// a bee
		bx, by := lx+lw*0.9, ly-14
		c.fill(alpha(hex("#bfe3ff"), 0.9), sh().ellipseRot(bx-4, by-9, 8, 5, -0.5).ellipseRot(bx+5, by-9, 8, 5, 0.5))
		c.fill(hex("#f2b705"), sh().ellipse(bx, by, 10, 7))
		c.fill(hex("#222222"), sh().rect(bx-3, by-7, 3, 14))
	}
	jar(150, 210, 330, "wildflower")
	jar(410, 240, 400, "clover")
	jar(700, 180, 280, "buckwheat")
	// honey dipper
	c.fill(hex("#c4935a"), sh().line(160, H-80, 470, H-140, 14))
	for i := 0; i < 4; i++ {
		c.fill(shade(hex("#c4935a"), -0.15*float64(i%2)), sh().ellipseRot(470+float64(i)*14, H-144-float64(i)*3, 12, 30, -0.2))
	}
	// price tag
	tx, ty := 690.0, H-150
	c.fill(alpha(black, 0.15), rot(tx+6, ty+8, 240, 120, -0.08))
	c.fill(hex("#fffef8"), rot(tx, ty, 240, 120, -0.08))
	c.fill(hex("#8a5a35"), sh().circle(tx-100, ty-2, 7))
	c.fill(hex("#555555"), sh().quad(2, tx-100, ty-2, tx-140, ty-80, tx-60, ty-150))
	pen := hex("#1f3a8a")
	c.text(tx+6, ty-14, 40, pen, "$12", alignCenter, true)
	c.text(tx+6, ty+18, 20, pen, "coworker discount:", alignCenter, false)
	c.text(tx+6, ty+46, 26, pen, "$12", alignCenter, true)
}

// ---- portal-bug ----

func artPortalBug(c *cv, W, H float64) {
	c.bg(hex("#cfd5dc"))
	// window chrome
	c.fill(alpha(black, 0.2), sh().rrect(26, 30, W-48, H-50, 14))
	c.fill(hex("#eceef1"), sh().rrect(20, 20, W-40, H-40, 14))
	for i, col := range []string{"#ee6a5f", "#f5bd4f", "#61c454"} {
		c.fill(hex(col), sh().circle(48+float64(i)*24, 46, 7))
	}
	c.fill(white, sh().rrect(150, 32, 700, 28, 14))
	c.text(170, 52, 15, hex("#5f6670"), "orders.quillfeather.example/cart", alignLeft, false)
	page := sh().rect(20, 72, W-40, H-92)
	c.fill(hex("#f6f7f9"), page)
	// the site's header
	navy := hex("#1d3557")
	c.fill(navy, sh().rect(20, 72, W-40, 56))
	quill(c, 44, 118, 66, 82, 10, hex("#f1c453"))
	c.text(80, 108, 20, white, "Quillfeather Order Portal", alignLeft, true)
	for i, s := range []string{"Catalog", "Orders", "Account"} {
		c.text(700+float64(i)*90, 106, 16, alpha(white, 0.85), s, alignLeft, false)
	}
	c.text(60, 172, 28, ink, "Your cart", alignLeft, true)
	// the line item
	card := func(x, y, w, h float64) {
		c.fill(hex("#e1e4e8"), sh().rrect(x-1, y-1, w+2, h+2, 10))
		c.fill(white, sh().rrect(x, y, w, h, 10))
	}
	card(60, 195, 520, 150)
	c.fill(hex("#eef3fb"), sh().rrect(80, 215, 110, 110, 8))
	c.fill(white, sh().rect(100, 245, 70, 54))
	c.fill(hex("#2f6fde"), sh().rect(100, 258, 70, 16))
	c.text(210, 245, 19, ink, "Ultra Bright 24 lb — 12 reams", alignLeft, true)
	c.text(210, 272, 15, hex("#6b7280"), "8.5 x 11 in · 500 sheets per ream · SKU QF-UB24", alignLeft, false)
	c.text(210, 305, 16, hex("#374151"), "Qty 12  x  $8.95", alignLeft, false)
	c.text(560, 305, 19, ink, "$107.40", alignRight, true)
	card(60, 360, 520, 70)
	c.text(80, 402, 15, hex("#6b7280"), "Ordering 10+ reams? Bulk pricing applies automatically: 7.5% off.", alignLeft, false)
	// order summary
	card(620, 160, 320, 330)
	c.text(645, 200, 20, ink, "Order summary", alignLeft, true)
	row := func(y float64, l, r string, col color.RGBA, bold bool) {
		c.text(645, y, 16, hex("#374151"), l, alignLeft, bold)
		c.text(915, y, 16, col, r, alignRight, bold)
	}
	row(245, "Subtotal", "$107.40", hex("#374151"), false)
	row(280, "Bulk discount (10+)", "-$8.05", hex("#15803d"), false)
	row(315, "Shipping", "FREE", hex("#374151"), false)
	c.fill(hex("#e5e7eb"), sh().rect(645, 335, 270, 2))
	row(370, "Total", "$99.35", ink, true)
	c.fill(hex("#2f6fde"), sh().rrect(645, 400, 270, 46, 8))
	c.text(780, 430, 17, white, "Place order", alignCenter, true)
	// someone's red pen
	red := hex("#e0262a")
	c.fill(red, sh().ellipseRot(880, 365, 56, 26, -0.08).ellipseRot(880, 365, 51, 22, -0.08).hole())
	c.text(943, 356, 26, red, "?!", alignLeft, true)
	c.fill(red, sh().quad(3.5, 930, 388, 968, 455, 905, 520))
	c.fill(red, sh().poly(895, 528, 915, 508, 920, 530))
	c.text(615, 545, 24, red, "should be $99.34 (a whole cent!)", alignLeft, true)
}

// ---- training-pdf ----

func artTrainingPDF(c *cv, W, H float64) {
	c.bg(white)
	coral, teal := hex("#ff7a59"), hex("#2bb3a3")
	gfill(c, 0, 0, W, 240, coral, hex("#ffb347"), sh().rect(0, 0, W, 240))
	for _, d := range [][4]float64{{80, 60, 22, 0}, {900, 50, 30, 1}, {820, 190, 16, 2}, {180, 200, 12, 1}, {600, 40, 14, 2}, {940, 150, 10, 0}} {
		cols := []color.RGBA{white, teal, hex("#ffe066")}
		c.fill(alpha(cols[int(d[3])], 0.8), sh().circle(d[0], d[1], d[2]))
	}
	c.fill(white, sh().blob(0, 240, 250, 200, 520, 250, 800, 205, 1000, 240, 1000, 290, 0, 290))
	c.text(70, 130, 26, white, "PEOPLE & CULTURE  ·  QUILLFEATHER PAPER CO.", alignLeft, true)
	c.text(70, 340, 74, hex("#23303f"), "Feelings & Forklifts", alignLeft, true)
	c.text(70, 400, 36, teal, "Module 3: Safe Spaces, Safe Aisles", alignLeft, true)
	c.text(70, 448, 26, hex("#7a8592"), "Presented by Ruth Abernathy, People & Culture", alignLeft, false)
	// clip art: a forklift with a heart
	cx, cy := 500.0, 700.0
	c.fill(hex("#eef9f7"), sh().circle(cx, cy, 170))
	yel := hex("#f2b705")
	c.fill(hex("#3a3a3a"), sh().rect(cx-150, cy-110, 14, 190).rect(cx-180, cy+68, 60, 10))
	c.fill(yel, sh().rrect(cx-120, cy-10, 230, 90, 18))
	c.fill(hex("#2b2b2b"), sh().line(cx-100, cy-10, cx-90, cy-110, 8).line(cx+40, cy-10, cx+30, cy-110, 8).line(cx-92, cy-110, cx+34, cy-110, 10))
	c.fill(shade(yel, -0.25), sh().rrect(cx+60, cy-20, 70, 100, 14))
	for _, w := range [][2]float64{{cx - 70, cy + 90}, {cx + 70, cy + 90}} {
		c.fill(hex("#1f1f1f"), sh().circle(w[0], w[1], 32))
		c.fill(hex("#b0b0b0"), sh().circle(w[0], w[1], 12))
	}
	c.fill(coral, sh().heart(cx-25, cy-170, 46))
	// body text
	grey := hex("#e3e7ec")
	c.text(70, 900, 30, hex("#23303f"), "Today we'll cover", alignLeft, true)
	for i, s := range []string{"Naming your feelings (before the loading dock)", "Horn etiquette and inner calm", "When to yield, emotionally and in aisle 4"} {
		y := 950 + float64(i)*48
		c.fill(teal, sh().rrect(74, y-22, 26, 26, 6))
		c.fill(white, sh().polyline(4, 80, y-9, 86, y-3, 95, y-16))
		c.text(118, y, 24, hex("#3b4652"), s, alignLeft, false)
	}
	greyLines(c, 70, 1100, 860, 34, 3, grey)
	c.fill(hex("#eef0f3"), sh().rect(0, H-70, W, 70))
	c.text(70, H-28, 20, hex("#8a94a0"), "Wexley Falls branch · Mandatory · due Friday", alignLeft, false)
	c.text(W-70, H-28, 20, hex("#8a94a0"), "Page 1 of 12", alignRight, false)
}
