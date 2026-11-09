package demo

// The demo's emoji, drawn: simple flat versions of the standard emoji
// people use most (the demo is offline, so Slack's CDN images are never
// fetched), a neutral face for the rest, and Quillfeather's custom emoji.
// Every drawing is in a 100x100 unit space and readable at 22px: bold
// silhouettes, a darker rim, few details.

import (
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"

	"golang.org/x/image/vector"
)

// standardDrawn is every standard emoji name with a drawing of its own.
var standardDrawn = func() []string {
	var out []string
	for n := range stdArt {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}()

// customEmoji are the workspace's custom emoji (emoji.list).
var customEmoji = []string{"quill", "rip-lamby", "prinny", "big-linda", "honey-pot-dale", "mr-pemberton",
	"quillie", "ultra-bright", "yogurt-alert", "safety-whistle", "stonks-paper", "okayest-boss"}

// drawStandardEmoji draws a standard emoji at px; name "" is the
// fallback, tinted by the image stem so different unknown emoji differ.
func drawStandardEmoji(name, stem string, px int) []byte {
	px = min(max(px, 8), maxPx)
	c := newCV(px, px, float64(px)/100)
	if f, ok := stdArt[name]; ok {
		f(c)
	} else {
		fallbackEmoji(c, stem)
	}
	return c.png()
}

// drawCustomEmoji draws a custom emoji at px (false: not one of ours).
func drawCustomEmoji(name string, px int) ([]byte, bool) {
	f, ok := customArt[name]
	if !ok {
		return nil, false
	}
	px = min(max(px, 8), maxPx)
	c := newCV(px, px, float64(px)/100)
	f(c)
	return c.png(), true
}

// ---- palette ----

var (
	featC   = hex("#5b3a0e") // eyes, brows, smile lines
	mouthC  = hex("#6e2412")
	tongueC = hex("#f06a7c")
	tearC   = hex("#4fb3f6")
	handC   = hex("#ffc83d")
	handO   = hex("#d4900a")
	redC    = hex("#e53935")
	greenC  = hex("#3fae49")
	goldC   = hex("#f5b82e")
	goldO   = hex("#c98a07")
	greyC   = hex("#9aa3ad")
	paperC  = hex("#fbfbf8")
	lineC   = hex("#c9ced6")
	outC    = hex("#3a3f47")
	pinkC   = hex("#f7a1c4")
	brownC  = hex("#8b5a2b")
)

// ---- helpers ----

// xf maps a shape built around (0,0) to centre (cx,cy), scaled by sc and
// rotated by rot radians.
func xf(s *shape, cx, cy, sc, rot float64) *shape {
	ca, sa := math.Cos(rot), math.Sin(rot)
	for _, p := range s.polys {
		for i, q := range p {
			p[i] = pt{cx + sc*(q.x*ca-q.y*sa), cy + sc*(q.x*sa+q.y*ca)}
		}
	}
	return s
}

// flipX mirrors a shape about x = 50.
func flipX(s *shape) *shape {
	for _, p := range s.polys {
		for i, q := range p {
			p[i] = pt{100 - q.x, q.y}
		}
	}
	return s
}

// two fills a figure in its outline colour grown by e, then in col: a
// rim that stays outside even where the figure's parts overlap.
func two(c *cv, build func(e float64) *shape, col, rim color.RGBA, e float64) {
	c.fill(rim, build(e))
	c.fill(col, build(0))
}

// halfDown is the lower half of an ellipse: flat edge at y (an open mouth).
func (s *shape) halfDown(cx, y, rx, ry float64) *shape {
	var xy []float64
	for i := 0; i <= 32; i++ {
		t := math.Pi * float64(i) / 32
		xy = append(xy, cx+rx*math.Cos(t), y+ry*math.Sin(t))
	}
	return s.poly(xy...)
}

// drop is a teardrop pointing up with its round end at (cx,cy).
func (s *shape) drop(cx, cy, r float64) *shape {
	s.circle(cx, cy, r)
	return s.poly(cx-r*0.97, cy-r*0.25, cx, cy-r*2.3, cx+r*0.97, cy-r*0.25)
}

// bounds is the shape's pixel bounding box on c.
func (c *cv) bounds(s *shape) image.Rectangle {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range s.polys {
		for _, q := range p {
			minX, minY = min(minX, q.x*c.k), min(minY, q.y*c.k)
			maxX, maxY = max(maxX, q.x*c.k), max(maxY, q.y*c.k)
		}
	}
	if len(s.polys) == 0 {
		return image.Rectangle{}
	}
	return image.Rect(int(math.Floor(minX))-1, int(math.Floor(minY))-1, int(math.Ceil(maxX))+1, int(math.Ceil(maxY))+1).Intersect(c.img.Bounds())
}

// mask rasterizes s over bb into an alpha mask (bb's coordinates).
func (c *cv) mask(s *shape, bb image.Rectangle) *image.Alpha {
	m := image.NewAlpha(bb)
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
	sub := image.NewAlpha(image.Rect(0, 0, bb.Dx(), bb.Dy()))
	z.Draw(sub, sub.Bounds(), image.Opaque, image.Point{})
	copy(m.Pix, sub.Pix)
	return m
}

// fillClip paints s in col only where it overlaps clip (stripes on a
// bee, teeth in a mouth).
func (c *cv) fillClip(col color.Color, s, clip *shape) {
	bb := c.bounds(s).Intersect(c.bounds(clip))
	if bb.Empty() {
		return
	}
	a, b := c.mask(s, bb), c.mask(clip, bb)
	for i := range a.Pix {
		a.Pix[i] = uint8(uint32(a.Pix[i]) * uint32(b.Pix[i]) / 255)
	}
	draw.DrawMask(c.img, bb, image.NewUniform(col), image.Point{}, a, bb.Min, draw.Over)
}

// rotatedArt draws f, then turns the picture by a radians about its
// centre (a rolling or upside-down face).
func rotatedArt(c *cv, a float64, f func(c *cv)) {
	b := c.img.Bounds()
	src := newCV(b.Dx(), b.Dy(), c.k)
	f(src)
	cx, cy := float64(b.Dx())/2, float64(b.Dy())/2
	ca, sa := math.Cos(-a), math.Sin(-a)
	at := func(x, y int) [4]float64 {
		if x < 0 || y < 0 || x >= b.Dx() || y >= b.Dy() {
			return [4]float64{}
		}
		o := src.img.PixOffset(x, y)
		p := src.img.Pix[o : o+4]
		return [4]float64{float64(p[0]), float64(p[1]), float64(p[2]), float64(p[3])}
	}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			sx, sy := cx+dx*ca-dy*sa-0.5, cy+dx*sa+dy*ca-0.5
			x0, y0 := int(math.Floor(sx)), int(math.Floor(sy))
			fx, fy := sx-float64(x0), sy-float64(y0)
			p00, p10, p01, p11 := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
			o := c.img.PixOffset(x, y)
			for i := 0; i < 4; i++ {
				v := (p00[i]*(1-fx)+p10[i]*fx)*(1-fy) + (p01[i]*(1-fx)+p11[i]*fx)*fy
				c.img.Pix[o+i] = uint8(math.Round(v))
			}
		}
	}
}

// ---- faces ----

// yface is the round yellow face every smiley starts from: centre
// (50,51), radius 44.5.
func yface(c *cv) {
	c.fill(goldO, sh().circle(50, 51, 46.5))
	c.fillSrc(c.grad(50, 6, 50, 96, hex("#ffe46b"), hex("#f6ad1b")), sh().circle(50, 51, 44.5))
}

func eyesDot(c *cv, y float64) {
	c.fill(featC, sh().ellipse(35, y, 5, 7.5).ellipse(65, y, 5, 7.5))
}

// eyesHappy are ^ ^ arcs.
func eyesHappy(c *cv, y float64) {
	c.fill(featC, sh().arc(34, y+4, 8, math.Pi*1.12, math.Pi*1.88, 5.2).arc(66, y+4, 8, math.Pi*1.12, math.Pi*1.88, 5.2))
}

// eyesSquint are > < (laughing hard).
func eyesSquint(c *cv, y float64) {
	c.fill(featC, sh().polyline(5.5, 26, y-7, 40, y, 26, y+7).polyline(5.5, 74, y-7, 60, y, 74, y+7))
}

// eyesSad are closed downturned arcs (crying).
func eyesSad(c *cv, y float64) {
	c.fill(featC, sh().arc(34, y-4, 8, math.Pi*0.12, math.Pi*0.88, 5).arc(66, y-4, 8, math.Pi*0.12, math.Pi*0.88, 5))
}

// smileArc is a closed-mouth smile with its lowest point at y.
func smileArc(c *cv, y, r, w float64) {
	c.fill(featC, sh().arc(50, y-r, r, math.Pi*0.2, math.Pi*0.8, w))
}

// openMouth is a D-shaped open mouth with a flat top at y, upper teeth
// and a tongue.
func openMouth(c *cv, y, rx, ry float64, teeth, tongue bool) {
	m := sh().halfDown(50, y, rx, ry)
	c.fill(mouthC, sh().halfDown(50, y, rx, ry))
	if tongue {
		c.fillClip(tongueC, sh().ellipse(50, y+ry*1.02, rx*0.55, ry*0.42), m)
	}
	if teeth {
		c.fillClip(white, sh().rect(50-rx, y-1, 2*rx, ry*0.3+1), m)
	}
}

// hand: an open palm with four fingers up and the thumb to the left,
// centred on the palm; e grows every part (the rim pass).
func openHand(e float64) *shape {
	s := sh().rrect(-17-e, -8-e, 34+2*e, 34+2*e, 13+e)
	for i, top := range []float64{-30, -36, -35, -28} {
		x := -12.6 + 8.4*float64(i)
		s.line(x, 0, x, top, 7.2+2*e)
	}
	return s.line(-13, 14, -28, -4, 9+2*e)
}

// drawHand draws build at (cx,cy), scaled and turned.
func drawHand(c *cv, build func(e float64) *shape, cx, cy, sc, rot float64, mirror bool) {
	mk := func(e float64) *shape {
		s := build(e)
		if mirror {
			for _, p := range s.polys {
				for i, q := range p {
					p[i] = pt{-q.x, q.y}
				}
			}
		}
		return xf(s, cx, cy, sc, rot)
	}
	two(c, mk, handC, handO, 2.2/sc)
}

// fist with the index finger pointing up.
func pointHand(e float64) *shape {
	s := sh().rrect(-17-e, -4-e, 34+2*e, 30+2*e, 11+e)
	for i := 0; i < 3; i++ {
		y := 2 + 8*float64(i)
		s.line(-4, y, 15, y, 7.5+2*e)
	}
	s.line(-11, 0, -11, -36, 8.5+2*e)
	return s.line(-15, 16, -4, 6, 9+2*e)
}

// thumbHand is a thumbs up: fist with the thumb on top.
func thumbHand(e float64) *shape {
	s := sh().rrect(-22-e, -4-e, 40+2*e, 38+2*e, 11+e)
	for i := 0; i < 4; i++ {
		y := 2 + 8.6*float64(i)
		s.line(-6, y, 20, y, 7.6+2*e)
	}
	return s.line(-15, 2, -10, -36, 11+2*e)
}

// ---- the standard emoji ----

var stdArt = map[string]func(c *cv){
	"grinning": func(c *cv) {
		yface(c)
		eyesDot(c, 38)
		openMouth(c, 58, 28, 24, true, false)
	},
	"smile": func(c *cv) {
		yface(c)
		eyesHappy(c, 38)
		openMouth(c, 56, 29, 26, true, true)
	},
	"laughing": func(c *cv) {
		yface(c)
		eyesSquint(c, 37)
		openMouth(c, 54, 29, 28, true, true)
	},
	"sweat_smile": func(c *cv) {
		yface(c)
		eyesHappy(c, 40)
		openMouth(c, 58, 27, 22, true, false)
		c.fill(shade(tearC, -0.2), sh().drop(80, 26, 8.5))
		c.fill(tearC, sh().drop(80, 26, 6.5))
	},
	"joy":                           joyFace,
	"rolling_on_the_floor_laughing": func(c *cv) { rotatedArt(c, -0.45, joyFace) },
	"wink": func(c *cv) {
		yface(c)
		c.fill(featC, sh().ellipse(35, 38, 5, 7.5).arc(66, 42, 8, math.Pi*1.12, math.Pi*1.88, 5.2))
		smileArc(c, 76, 24, 5.5)
	},
	"slightly_smiling_face": slightSmile,
	"upside_down_face":      func(c *cv) { rotatedArt(c, math.Pi, slightSmile) },
	"thinking_face": func(c *cv) {
		yface(c)
		c.fill(featC, sh().ellipse(36, 40, 4.8, 7).ellipse(64, 40, 4.8, 7).
			quad(4, 24, 26, 33, 20, 43, 25).quad(4, 56, 24, 66, 28, 74, 30).line(38, 66, 60, 62, 5))
		drawHand(c, pointHand, 42, 84, 0.62, 1.2, false)
	},
	"face_palm": func(c *cv) {
		yface(c)
		eyesSad(c, 46)
		c.fill(featC, sh().line(42, 76, 60, 74, 4.5))
		drawHand(c, openHand, 58, 36, 0.78, 0.5, false)
	},
	"shrug": func(c *cv) {
		c.fill(goldO, sh().circle(50, 42, 30))
		c.fillSrc(c.grad(50, 12, 50, 72, hex("#ffe46b"), hex("#f6ad1b")), sh().circle(50, 42, 28.5))
		c.fill(featC, sh().ellipse(41, 38, 3.5, 5).ellipse(59, 38, 3.5, 5).line(42, 54, 58, 54, 4).
			line(30, 26, 38, 28, 3.5).line(62, 28, 70, 26, 3.5))
		drawHand(c, openHand, 14, 74, 0.42, -1.25, false)
		drawHand(c, openHand, 86, 74, 0.42, 1.25, true)
		c.fill(featC, sh().polyline(3.5, 18, 88, 30, 82).polyline(3.5, 82, 88, 70, 82))
	},
	"sunglasses": func(c *cv) {
		yface(c)
		g := sh().rrect(14, 30, 32, 20, 9).rrect(54, 30, 32, 20, 9).rect(44, 33, 12, 5).line(8, 34, 16, 34, 4).line(84, 34, 92, 34, 4)
		c.fill(hex("#1e2228"), g)
		c.fill(alpha(white, 0.35), sh().line(20, 36, 28, 36, 3).line(60, 36, 68, 36, 3))
		smileArc(c, 76, 22, 5.5)
	},
	"sob": func(c *cv) {
		yface(c)
		c.fillClip(tearC, sh().rrect(23, 44, 16, 60, 8).rrect(61, 44, 16, 60, 8), sh().circle(50, 51, 44.5))
		eyesSad(c, 44)
		openMouth(c, 64, 18, 20, false, true)
	},
	"scream": func(c *cv) {
		c.fill(goldO, sh().circle(50, 51, 46.5))
		c.fillSrc(c.grad(50, 6, 50, 96, hex("#8fb6e8"), hex("#f6c443")), sh().circle(50, 51, 44.5))
		c.fill(white, sh().ellipse(34, 38, 9, 11).ellipse(66, 38, 9, 11))
		c.fill(featC, sh().circle(34, 40, 3.5).circle(66, 40, 3.5))
		c.fill(mouthC, sh().ellipse(50, 70, 9, 15))
		drawHand(c, openHand, 12, 66, 0.52, -0.15, false)
		drawHand(c, openHand, 88, 66, 0.52, 0.15, true)
	},
	"grimacing": func(c *cv) {
		yface(c)
		eyesDot(c, 38)
		m := sh().rrect(22, 56, 56, 22, 10)
		c.fill(featC, sh().rrect(20, 54, 60, 26, 12))
		c.fill(white, m)
		c.fillClip(featC, sh().rect(22, 66, 56, 2.2).rect(35, 56, 2.2, 22).rect(49, 56, 2.2, 22).rect(63, 56, 2.2, 22), m)
	},
	"skull": func(c *cv) {
		s := func(e float64) *shape {
			return sh().ellipse(50, 42, 38+e, 34+e).rrect(30-e, 56-e, 40+2*e, 32+2*e, 9+e)
		}
		two(c, s, hex("#eef0f2"), hex("#8a939e"), 3)
		c.fill(hex("#3a3f47"), sh().ellipse(34, 46, 10, 11).ellipse(66, 46, 10, 11).poly(50, 56, 44, 68, 56, 68))
		c.fill(hex("#8a939e"), sh().rect(40, 76, 2.5, 12).rect(49, 76, 2.5, 12).rect(58, 76, 2.5, 12))
	},
	"see_no_evil": func(c *cv) {
		fur := hex("#a0663a")
		c.fill(shade(fur, -0.25), sh().circle(12, 46, 13).circle(88, 46, 13).circle(50, 50, 41))
		c.fill(fur, sh().circle(12, 46, 11).circle(88, 46, 11).circle(50, 50, 39))
		c.fill(hex("#f0c9a0"), sh().circle(12, 46, 6).circle(88, 46, 6).ellipse(50, 58, 30, 28))
		c.fill(featC, sh().circle(45, 66, 2).circle(55, 66, 2).arc(50, 66, 12, math.Pi*0.25, math.Pi*0.75, 3.5))
		hand := func(e float64) *shape { return sh().ellipse(0, 0, 16+e, 12+e) }
		two(c, func(e float64) *shape { return xf(hand(e), 34, 42, 1, -0.25) }, hex("#e8b98a"), shade(fur, -0.25), 2.5)
		two(c, func(e float64) *shape { return xf(hand(e), 66, 42, 1, 0.25) }, hex("#e8b98a"), shade(fur, -0.25), 2.5)
	},
	"eyes": func(c *cv) {
		for _, x := range []float64{30, 70} {
			c.fill(hex("#a8b0ba"), sh().ellipse(x, 50, 19, 29))
			c.fill(white, sh().ellipse(x, 50, 17, 27))
			c.fill(hex("#2b2f36"), sh().ellipse(x-6, 54, 8, 11))
			c.fill(white, sh().circle(x-8, 50, 2.5))
		}
	},
	"nerd_face": func(c *cv) {
		yface(c)
		c.fill(hex("#2b2f36"), sh().ring(32, 40, 13, 5).ring(68, 40, 13, 5).line(44, 38, 56, 38, 4).line(8, 36, 19, 38, 4).line(81, 38, 92, 36, 4))
		c.fill(featC, sh().circle(32, 41, 4.5).circle(68, 41, 4.5))
		openMouth(c, 64, 20, 14, false, false)
		c.fill(white, sh().rect(42, 63, 7, 8).rect(51, 63, 7, 8))
	},
	"partying_face": func(c *cv) {
		yface(c)
		eyesHappy(c, 44)
		hat := sh().poly(10, 30, 42, 4, 40, 34)
		c.fill(hex("#7b4fd6"), hat)
		c.fillClip(hex("#ffd93b"), sh().line(14, 22, 38, 24, 4).line(24, 12, 41, 15, 4), hat)
		c.fill(hex("#ff5a8a"), sh().circle(42, 4, 4.5))
		smileArc(c, 74, 18, 5)
		c.fill(hex("#2f8de4"), sh().poly(58, 68, 86, 58, 88, 66))
		c.fill(hex("#ff5a8a"), sh().circle(88, 62, 5))
		confetti(c, [][3]float64{{80, 18, 0}, {90, 36, 1}, {14, 64, 2}, {70, 8, 3}})
	},
	"saluting_face": func(c *cv) {
		yface(c)
		eyesDot(c, 42)
		smileArc(c, 76, 20, 5.5)
		drawHand(c, openHand, 72, 22, 0.55, 0.9, true)
	},
	"melting_face": func(c *cv) {
		m := func(e float64) *shape {
			return sh().pie(50, 50, 40+e, math.Pi, 2*math.Pi).rect(10-e, 49, 80+2*e, 22).
				ellipse(50, 84, 47+e, 11+e).ellipse(22, 74, 9+e, 14+e).ellipse(76, 72, 10+e, 16+e)
		}
		c.fill(goldO, m(2))
		c.fillSrc(c.grad(50, 8, 50, 94, hex("#ffe46b"), hex("#f6ad1b")), m(0))
		c.fill(featC, sh().ellipse(37, 40, 4.5, 7).ellipse(63, 40, 4.5, 7))
		smileArc(c, 64, 20, 5)
		c.fill(alpha(white, 0.35), sh().ellipse(36, 86, 10, 3))
	},
	"pleading_face": func(c *cv) {
		yface(c)
		for _, x := range []float64{33, 67} {
			c.fill(white, sh().circle(x, 46, 13))
			c.fill(featC, sh().circle(x, 47, 11))
			c.fill(white, sh().circle(x-4, 42, 4).circle(x+4, 51, 2))
		}
		c.fill(featC, sh().quad(4, 20, 30, 28, 22, 38, 24).quad(4, 62, 24, 72, 22, 80, 30).arc(50, 82, 10, math.Pi*1.2, math.Pi*1.8, 4.5))
	},
	"heart": func(c *cv) {
		c.fill(hex("#b5182c"), sh().heart(50, 54, 46))
		c.fillSrc(c.grad(50, 10, 50, 96, hex("#ff5465"), hex("#d81f37")), sh().heart(50, 54, 43))
		c.fill(alpha(white, 0.5), sh().ellipseRot(28, 34, 9, 5, -0.7))
	},
	"+1": func(c *cv) { drawHand(c, thumbHand, 52, 56, 1.15, 0, false) },
	"-1": func(c *cv) { drawHand(c, thumbHand, 48, 46, 1.15, math.Pi, false) },
	"clap": func(c *cv) {
		drawHand(c, openHand, 44, 60, 0.95, -0.45, false)
		drawHand(c, openHand, 58, 56, 0.95, 0.35, true)
		c.fill(goldO, sh().line(14, 22, 20, 30, 4).line(24, 12, 28, 22, 4).line(36, 8, 36, 18, 4))
	},
	"raised_hands": func(c *cv) {
		drawHand(c, openHand, 28, 58, 0.8, -0.25, true)
		drawHand(c, openHand, 72, 58, 0.8, 0.25, false)
		c.fill(goldO, sh().line(50, 8, 50, 20, 4).line(38, 12, 42, 22, 4).line(62, 12, 58, 22, 4))
	},
	"pray": func(c *cv) {
		half := func(e float64) *shape {
			return sh().blob(50+e*0.3, 8-e, 40-e, 30, 30-e, 64, 34-e, 88+e, 50+e*0.3, 90+e)
		}
		two(c, half, handC, handO, 2.4)
		two(c, func(e float64) *shape { return flipX(half(e)) }, handC, handO, 2.4)
		c.fill(handO, sh().line(50, 12, 50, 86, 2.5))
		c.fill(hex("#5b8def"), sh().rrect(24, 86, 22, 12, 3).rrect(54, 86, 22, 12, 3))
		c.fill(goldO, sh().line(18, 20, 24, 28, 3.5).line(82, 20, 76, 28, 3.5))
	},
	"wave": func(c *cv) {
		drawHand(c, openHand, 52, 58, 1.05, 0.35, false)
		c.fill(hex("#5bb5f0"), sh().arc(52, 58, 44, math.Pi*1.55, math.Pi*1.8, 4).arc(52, 58, 36, math.Pi*0.6, math.Pi*0.85, 4))
	},
	"muscle": func(c *cv) {
		arm := func(e float64) *shape {
			return sh().line(4, 82, 60, 82, 22+2*e).ellipse(32, 64, 22+e, 17+e).line(60, 82, 70, 36, 19+2*e).
				circle(64, 24, 15+e).line(52, 16, 56, 34, 9+2*e)
		}
		two(c, arm, handC, handO, 2.4)
		c.fill(handO, sh().quad(2.6, 50, 70, 46, 54, 34, 47).line(56, 20, 70, 18, 2.4).line(57, 28, 72, 27, 2.4))
	},
	"ok_hand": func(c *cv) {
		h := func(e float64) *shape {
			s := sh().rrect(-14-e, -2-e, 32+2*e, 32+2*e, 13+e)
			for i, top := range []float64{-34, -36, -28} {
				x := -3 + 8.4*float64(i)
				s.line(x, 6, x, top, 7.2+2*e)
			}
			return s.ring(-18, 0, 10, 7+2*e)
		}
		drawHand(c, h, 54, 58, 1.1, 0.15, false)
	},
	"point_up": func(c *cv) { drawHand(c, pointHand, 56, 58, 1.2, 0, false) },
	"tada":     tada,
	"confetti_ball": func(c *cv) {
		top := func(e float64) *shape { return xf(sh().pie(0, 0, 28+e, math.Pi, 2*math.Pi), 46, 36, 1, -0.25) }
		bot := func(e float64) *shape { return xf(sh().pie(0, 0, 28+e, 0, math.Pi), 54, 44, 1, 0.35) }
		two(c, bot, hex("#e8a317"), goldO, 2.5)
		c.fill(hex("#ff5a8a"), sh().quad(3, 34, 62, 26, 78, 36, 92))
		c.fill(hex("#2f8de4"), sh().quad(3, 50, 70, 54, 84, 46, 96))
		c.fill(hex("#3fae49"), sh().quad(3, 66, 62, 74, 76, 68, 92))
		two(c, top, goldC, goldO, 2.5)
		confetti(c, [][3]float64{{12, 70, 0}, {88, 70, 1}, {20, 88, 2}, {82, 90, 3}, {86, 12, 1}})
	},
	"balloon": func(c *cv) {
		c.fill(featC, sh().quad(2.5, 52, 72, 40, 84, 54, 98))
		c.fill(hex("#b5182c"), sh().ellipse(52, 38, 31, 35).poly(46, 76, 58, 76, 52, 68))
		c.fillSrc(c.grad(30, 10, 70, 70, hex("#ff6b6b"), hex("#d81f37")), sh().ellipse(52, 38, 29, 33))
		c.fill(alpha(white, 0.55), sh().ellipseRot(38, 24, 6, 10, 0.5))
	},
	"gift": func(c *cv) {
		c.fill(hex("#b5182c"), sh().rrect(14, 42, 72, 52, 5).rrect(8, 28, 84, 18, 5))
		c.fill(hex("#e8394d"), sh().rrect(16, 44, 68, 48, 4).rrect(10, 30, 80, 14, 4))
		c.fill(hex("#ffd93b"), sh().rect(44, 30, 12, 62).rect(10, 34, 80, 6))
		c.fill(hex("#e6b800"), sh().ellipseRot(38, 20, 13, 7, 0.5).ellipseRot(62, 20, 13, 7, -0.5))
		c.fill(hex("#ffd93b"), sh().ellipseRot(38, 20, 10, 4.5, 0.5).ellipseRot(62, 20, 10, 4.5, -0.5).circle(50, 26, 5))
	},
	"fire": func(c *cv) {
		c.fill(hex("#e8461c"), sh().blob(50, 4, 66, 26, 84, 44, 86, 68, 74, 90, 50, 97, 26, 90, 14, 68, 18, 44, 30, 54, 34, 30))
		c.fill(hex("#ff9a1f"), sh().blob(52, 34, 66, 52, 72, 72, 62, 88, 48, 92, 32, 86, 28, 70, 36, 56, 42, 64))
		c.fill(hex("#ffd84a"), sh().blob(50, 60, 60, 74, 58, 88, 50, 92, 40, 88, 40, 76))
	},
	"100": func(c *cv) {
		c.text(50, 64, 50, hex("#e0202f"), "100", alignCenter, true)
		c.fill(hex("#e0202f"), sh().line(10, 76, 90, 72, 5).line(14, 86, 84, 83, 5))
	},
	"rocket": func(c *cv) {
		r := func(s *shape) *shape { return xf(s, 52, 48, 1, math.Pi/4) }
		c.fill(hex("#ff7a1f"), r(sh().poly(-10, 30, 10, 30, 0, 52)))
		c.fill(hex("#ffd84a"), r(sh().poly(-6, 30, 6, 30, 0, 44)))
		c.fill(hex("#d6343f"), r(sh().poly(-14, 6, -28, 30, -12, 30).poly(14, 6, 28, 30, 12, 30)))
		c.fill(hex("#8a939e"), r(sh().ellipse(0, -2, 16, 36)))
		c.fill(hex("#e9edf1"), r(sh().ellipse(0, -2, 14, 34)))
		c.fill(hex("#d6343f"), r(sh().ellipse(0, -26, 9, 10).rect(-12, 22, 24, 8)))
		c.fill(hex("#3a3f47"), r(sh().circle(0, -2, 8)))
		c.fill(hex("#5bc0f8"), r(sh().circle(0, -2, 5.5)))
	},
	"sparkles": func(c *cv) {
		sp := func(x, y, r float64) {
			c.fill(goldO, sh().star(x, y, r+2, r*0.28+1.5, 4, 0))
			c.fill(hex("#ffd93b"), sh().star(x, y, r, r*0.28, 4, 0))
		}
		sp(40, 58, 34)
		sp(78, 22, 16)
		sp(80, 80, 11)
	},
	"star": func(c *cv) {
		c.fill(goldO, sh().star(50, 54, 48, 21, 5, 0))
		c.fillSrc(c.grad(50, 10, 50, 90, hex("#ffe14d"), hex("#f6a91b")), sh().star(50, 54, 43, 18.5, 5, 0))
	},
	"white_check_mark": func(c *cv) {
		c.fill(hex("#2e8b3a"), sh().rrect(6, 6, 88, 88, 18))
		c.fill(hex("#43b649"), sh().rrect(8, 8, 84, 84, 16))
		c.fill(white, sh().polyline(12, 26, 52, 42, 68, 74, 32))
	},
	"heavy_check_mark": func(c *cv) {
		c.fill(hex("#3a3f47"), sh().polyline(16, 16, 52, 38, 76, 86, 22))
	},
	"x": func(c *cv) {
		c.fill(hex("#b5182c"), sh().line(18, 18, 82, 82, 22).line(82, 18, 18, 82, 22))
		c.fill(hex("#e8394d"), sh().line(18, 18, 82, 82, 17).line(82, 18, 18, 82, 17))
	},
	"warning": func(c *cv) {
		tri := func(e float64) *shape {
			return sh().polyline(14+2*e, 50, 14, 12, 84, 88, 84, 50, 14).poly(50, 14, 12, 84, 88, 84)
		}
		two(c, tri, hex("#ffcc33"), hex("#d99a0b"), 2)
		c.fill(hex("#2b2f36"), sh().line(50, 36, 50, 62, 8).circle(50, 74, 5))
	},
	"rotating_light": func(c *cv) {
		c.fill(hex("#ff5a5a"), sh().line(10, 30, 20, 38, 4).line(90, 30, 80, 38, 4).line(50, 4, 50, 14, 4).line(22, 12, 28, 22, 4).line(78, 12, 72, 22, 4))
		c.fill(hex("#5c6670"), sh().rrect(16, 74, 68, 18, 5))
		c.fill(hex("#b5182c"), sh().halfDown(50, 74, 26, -48).rect(24, 66, 52, 9))
		c.fill(hex("#ef3b3b"), sh().halfDown(50, 72, 23, -44))
		c.fill(alpha(white, 0.6), sh().ellipseRot(40, 46, 4, 10, 0.2))
	},
	"bulb": func(c *cv) {
		c.fill(hex("#ffd93b"), sh().line(10, 40, 18, 40, 4).line(90, 40, 82, 40, 4).line(50, 0, 50, 6, 4).line(20, 12, 25, 17, 4).line(80, 12, 75, 17, 4))
		c.fill(goldO, sh().circle(50, 40, 29).poly(34, 58, 66, 58, 60, 74, 40, 74))
		c.fill(hex("#ffe14d"), sh().circle(50, 40, 26.5).poly(36, 56, 64, 56, 58, 72, 42, 72))
		c.fill(alpha(white, 0.6), sh().ellipseRot(40, 30, 5, 9, 0.6))
		c.fill(hex("#8a939e"), sh().rrect(38, 72, 24, 18, 4))
		c.fill(hex("#c3cad2"), sh().rect(38, 77, 24, 3).rect(38, 83, 24, 3))
		c.fill(hex("#5c6670"), sh().rrect(44, 89, 12, 6, 3))
	},
	"coffee": func(c *cv) {
		c.fill(hex("#a8b0ba"), sh().quad(4, 38, 30, 32, 20, 40, 8).quad(4, 56, 30, 50, 18, 58, 6))
		c.fill(hex("#8a939e"), sh().ellipse(48, 86, 40, 8).ring(78, 56, 10, 7))
		c.fill(hex("#eef1f4"), sh().ellipse(48, 84, 37, 6).ring(78, 56, 10, 4))
		c.fill(hex("#8a939e"), sh().rrect(16, 34, 64, 50, 16))
		c.fill(white, sh().rrect(18, 36, 60, 46, 14))
		c.fill(hex("#6b3d1f"), sh().ellipse(48, 40, 28, 5))
	},
	"pizza": func(c *cv) {
		c.fill(hex("#c47a2c"), sh().line(14, 22, 86, 22, 16))
		c.fill(hex("#e6a04a"), sh().line(14, 22, 86, 22, 12))
		c.fill(hex("#f6c443"), sh().poly(12, 28, 88, 28, 50, 96))
		c.fill(hex("#f9d965"), sh().poly(24, 30, 76, 30, 50, 82))
		c.fill(hex("#d6343f"), sh().circle(34, 40, 8).circle(62, 44, 8).circle(48, 66, 7))
	},
	"birthday": func(c *cv) {
		c.fill(hex("#c47a2c"), sh().rrect(10, 60, 80, 32, 6))
		c.fill(hex("#f7c9a0"), sh().rrect(12, 62, 76, 28, 5))
		c.fill(pinkC, sh().rrect(10, 56, 80, 10, 5).circle(22, 66, 5).circle(38, 67, 5).circle(54, 66, 5).circle(70, 67, 5).circle(84, 65, 4))
		c.fill(hex("#c47a2c"), sh().rrect(22, 36, 56, 24, 5))
		c.fill(hex("#f7c9a0"), sh().rrect(24, 38, 52, 20, 4))
		c.fill(pinkC, sh().rrect(22, 34, 56, 9, 4).circle(32, 43, 4).circle(46, 44, 4).circle(60, 43, 4).circle(72, 42, 3))
		for _, x := range []float64{36, 50, 64} {
			c.fill(hex("#5b8def"), sh().rrect(x-2.5, 18, 5, 17, 2))
			c.fill(hex("#ff9a1f"), sh().drop(x, 12, 3.5))
		}
	},
	"cake": func(c *cv) {
		c.fill(hex("#c47a2c"), sh().poly(10, 50, 90, 32, 90, 82, 10, 82))
		c.fill(hex("#fff4e4"), sh().poly(12, 50, 88, 34, 88, 80, 12, 80))
		c.fill(hex("#f7c9a0"), sh().poly(12, 56, 88, 42, 88, 52, 12, 64).poly(12, 68, 88, 60, 88, 68, 12, 74))
		c.fill(hex("#fff4e4"), sh().poly(10, 46, 90, 28, 90, 36, 10, 52))
		c.fill(hex("#d6343f"), sh().blob(64, 14, 74, 16, 76, 26, 66, 34, 56, 26, 58, 16))
		c.fill(greenC, sh().poly(60, 14, 66, 8, 72, 14, 66, 17))
	},
	"doughnut": func(c *cv) {
		c.fill(hex("#b5743a"), sh().circle(50, 52, 42).circle(50, 52, 13).hole())
		icing := sh().blob(14, 46, 26, 18, 50, 12, 76, 18, 88, 44, 82, 66, 66, 74, 50, 70, 32, 76, 18, 64).circle(50, 50, 15).hole()
		c.fill(hex("#f57bb0"), icing)
		cols := []color.RGBA{hex("#ffd93b"), hex("#5bc0f8"), white, hex("#3fae49")}
		for i, p := range [][2]float64{{30, 30}, {50, 22}, {68, 30}, {76, 48}, {24, 52}, {38, 64}, {64, 62}, {40, 36}} {
			c.fill(cols[i%4], xf(sh().line(-4, 0, 4, 0, 3), p[0], p[1], 1, float64(i)*0.9))
		}
	},
	"taco": func(c *cv) {
		lettuce := sh()
		for i := 0; i <= 10; i++ {
			a := math.Pi + math.Pi*float64(i)/10
			lettuce.circle(50+41*math.Cos(a), 86+41*math.Sin(a), 8)
		}
		c.fill(hex("#3fae49"), lettuce)
		c.fill(hex("#d6343f"), sh().circle(22, 58, 6).circle(50, 46, 6).circle(78, 58, 6))
		c.fill(hex("#c98a07"), sh().pie(50, 86, 42, math.Pi, 2*math.Pi))
		c.fill(hex("#f6c443"), sh().pie(50, 86, 39, math.Pi, 2*math.Pi))
		c.fill(hex("#e6a92e"), sh().circle(36, 74, 2.5).circle(56, 64, 2.5).circle(66, 78, 2.5).circle(46, 82, 2.5).circle(30, 62, 2.5))
	},
	"sandwich": func(c *cv) {
		c.fill(hex("#c47a2c"), sh().poly(10, 78, 86, 20, 92, 80))
		c.fill(hex("#3fae49"), sh().polyline(6, 14, 72, 86, 26).polyline(5, 12, 70, 20, 64, 28, 70, 36, 64, 44, 70))
		c.fill(hex("#ffd93b"), sh().poly(14, 70, 84, 24, 86, 34, 24, 74))
		c.fill(hex("#c47a2c"), sh().poly(10, 62, 82, 12, 92, 70))
		c.fill(hex("#f7dcae"), sh().poly(18, 62, 82, 18, 86, 66))
	},
	"popcorn": func(c *cv) {
		c.fill(hex("#fff3c4"), sh().circle(30, 32, 12).circle(46, 24, 13).circle(62, 28, 12).circle(74, 38, 10).circle(24, 44, 10).circle(52, 38, 12))
		c.fill(hex("#f6d77a"), sh().circle(40, 36, 4).circle(60, 30, 4).circle(30, 30, 3))
		cup := sh().poly(18, 44, 82, 44, 72, 96, 28, 96)
		c.fill(white, cup)
		c.fillClip(hex("#e53935"), sh().poly(24, 40, 36, 40, 40, 100, 30, 100).poly(46, 40, 56, 40, 55, 100, 46, 100).poly(64, 40, 76, 40, 70, 100, 61, 100), cup)
	},
	"bee": func(c *cv) {
		c.fill(alpha(hex("#bfe3ff"), 0.9), sh().ellipseRot(40, 24, 13, 18, -0.4).ellipseRot(60, 22, 12, 17, 0.4))
		c.fill(hex("#5d9fd6"), sh().ring(40, 24, 13, 2).ring(60, 22, 12, 2))
		body := sh().ellipse(50, 58, 34, 24)
		c.fill(hex("#2b2f36"), sh().ellipse(50, 58, 36, 26).poly(84, 54, 98, 58, 84, 64))
		c.fill(hex("#ffc61a"), body)
		c.fillClip(hex("#2b2f36"), sh().rect(40, 30, 9, 60).rect(58, 30, 9, 60).rect(74, 30, 9, 60), body)
		c.fill(hex("#2b2f36"), sh().circle(22, 52, 3.5))
		c.fill(hex("#2b2f36"), sh().quad(2.5, 20, 40, 14, 28, 8, 24).quad(2.5, 26, 38, 26, 26, 22, 18))
	},
	"honey_pot": honeyPot,
	"cat": func(c *cv) {
		catFace(c, hex("#f6b04a"), hex("#c4781e"), hex("#fde2b8"))
	},
	"printer": func(c *cv) {
		c.fill(outC, sh().rrect(24, 8, 52, 34, 3))
		c.fill(white, sh().rect(26, 10, 48, 32))
		c.fill(lineC, sh().rect(32, 18, 36, 3).rect(32, 26, 28, 3))
		c.fill(hex("#5c6670"), sh().rrect(6, 36, 88, 42, 9))
		c.fill(hex("#8a939e"), sh().rrect(8, 38, 84, 30, 8))
		c.fill(hex("#3fae49"), sh().circle(80, 48, 4))
		c.fill(outC, sh().rect(22, 62, 56, 6))
		c.fill(outC, sh().rect(24, 64, 52, 30))
		c.fill(white, sh().rect(26, 64, 48, 28))
		c.fill(lineC, sh().rect(32, 72, 36, 3).rect(32, 80, 30, 3))
	},
	"chart_with_upwards_trend":   func(c *cv) { chart(c, true) },
	"chart_with_downwards_trend": func(c *cv) { chart(c, false) },
	"bar_chart":                  barChart, // Pulse's poll title
	"trophy":                     trophy,
	"crown": func(c *cv) {
		cr := func(e float64) *shape {
			return sh().poly(10-e, 30-e, 30, 54, 50, 18-e, 70, 54, 90+e, 30-e, 82+e, 80+e, 18-e, 80+e).rrect(16-e, 72-e, 68+2*e, 16+2*e, 4)
		}
		two(c, cr, goldC, goldO, 2.5)
		c.fill(goldO, sh().circle(10, 28, 6).circle(50, 16, 6).circle(90, 28, 6))
		c.fill(hex("#ffe14d"), sh().circle(10, 28, 4.5).circle(50, 16, 4.5).circle(90, 28, 4.5))
		c.fill(hex("#d6343f"), sh().circle(50, 80, 5))
		c.fill(hex("#2f8de4"), sh().circle(30, 80, 4).circle(70, 80, 4))
	},
	"moneybag": func(c *cv) {
		bag := func(e float64) *shape {
			return sh().blob(50, 34-e, 80+e, 52, 86+e, 78, 70, 94+e, 30, 94+e, 14-e, 78, 20-e, 52)
		}
		two(c, bag, hex("#d9a95b"), hex("#9c6a2a"), 2.5)
		two(c, func(e float64) *shape { return sh().poly(36-e, 8-e, 64+e, 8-e, 56+e, 32, 44-e, 32) }, hex("#d9a95b"), hex("#9c6a2a"), 2.5)
		c.fill(hex("#9c6a2a"), sh().rrect(38, 30, 24, 6, 3))
		c.text(50, 82, 34, hex("#3f7a2b"), "$", alignCenter, true)
	},
	"pushpin": func(c *cv) {
		c.fill(hex("#8a939e"), sh().line(30, 70, 10, 92, 4))
		p := func(s *shape) *shape { return xf(s, 56, 42, 1, math.Pi/4) }
		c.fill(hex("#a5121f"), p(sh().rrect(-20, -36, 40, 14, 6).poly(-10, -24, 10, -24, 14, 6, -14, 6).rrect(-26, 4, 52, 14, 6)))
		c.fill(hex("#e8394d"), p(sh().rrect(-18, -34, 36, 10, 5).poly(-8, -24, 8, -24, 11, 6, -11, 6).rrect(-24, 6, 48, 10, 5)))
		c.fill(alpha(white, 0.5), p(sh().rect(-4, -20, 4, 22)))
	},
	"memo": func(c *cv) {
		paper(c, 12, 8, 62, 84)
		c.fill(lineC, sh().rect(20, 28, 44, 3).rect(20, 40, 44, 3).rect(20, 52, 38, 3).rect(20, 64, 30, 3))
		p := func(s *shape) *shape { return xf(s, 66, 56, 1, math.Pi/4) }
		c.fill(hex("#c98a07"), p(sh().rect(-6, -34, 12, 50)))
		c.fill(hex("#ffc61a"), p(sh().rect(-5, -33, 10, 48)))
		c.fill(pinkC, p(sh().rrect(-6, -42, 12, 9, 3)))
		c.fill(hex("#a8b0ba"), p(sh().rect(-6, -35, 12, 4)))
		c.fill(hex("#f7dcae"), p(sh().poly(-6, 16, 6, 16, 0, 28)))
		c.fill(outC, p(sh().poly(-2, 24, 2, 24, 0, 28)))
	},
	"calendar": func(c *cv) {
		c.fill(hex("#a8b0ba"), sh().rrect(10, 12, 80, 80, 10))
		c.fill(white, sh().rrect(12, 14, 76, 76, 9))
		c.fill(hex("#e8394d"), sh().rrect(12, 14, 76, 24, 9).rect(12, 30, 76, 8))
		c.text(50, 32, 15, white, "NOV", alignCenter, true)
		c.text(50, 80, 40, outC, "17", alignCenter, true)
	},
	"spiral_calendar_pad": func(c *cv) {
		c.fill(hex("#a8b0ba"), sh().rrect(10, 14, 80, 78, 8))
		c.fill(white, sh().rrect(12, 16, 76, 74, 7))
		c.fill(hex("#5b8def"), sh().rect(12, 16, 76, 16))
		for i := 0; i < 4; i++ {
			for j := 0; j < 5; j++ {
				c.fill(lineC, sh().rect(18+float64(j)*14, 40+float64(i)*12, 9, 7))
			}
		}
		c.fill(hex("#e8394d"), sh().rect(46, 52, 9, 7))
		for i := 0; i < 6; i++ {
			c.fill(outC, sh().rrect(20+float64(i)*12, 8, 4, 14, 2))
		}
	},
	"package": func(c *cv) {
		c.fill(hex("#9c6a2a"), sh().poly(50, 8, 92, 26, 92, 74, 50, 94, 8, 74, 8, 26))
		c.fill(hex("#d9a95b"), sh().poly(50, 46, 90, 28, 90, 72, 50, 91))
		c.fill(hex("#c9924a"), sh().poly(50, 46, 10, 28, 10, 72, 50, 91))
		c.fill(hex("#e9c48a"), sh().poly(50, 11, 89, 27, 50, 44, 11, 27))
		c.fill(hex("#f3e1bd"), sh().poly(30, 19, 40, 15, 79, 32, 69, 36).poly(69, 36, 79, 32, 79, 50, 69, 54))
	},
	"page_facing_up": func(c *cv) {
		paper(c, 16, 6, 68, 88)
		c.fill(lineC, sh().rect(26, 30, 48, 3).rect(26, 42, 48, 3).rect(26, 54, 48, 3).rect(26, 66, 34, 3))
	},
	"paperclip": func(c *cv) {
		cl := func(w float64, col color.RGBA) {
			s := sh().polyline(w, 40, 64, 40, 26).arc(52, 26, 12, math.Pi, 2*math.Pi, w).polyline(w, 64, 26, 64, 74).
				arc(46, 74, 18, 0, math.Pi, w).polyline(w, 28, 74, 28, 34)
			c.fill(col, xf(s, 6, -6, 1, 0.35))
		}
		cl(9, hex("#5c6670"))
		cl(5.5, hex("#c3cad2"))
	},
	"computer": func(c *cv) {
		c.fill(hex("#5c6670"), sh().rrect(16, 18, 68, 50, 5))
		c.fill(hex("#5bc0f8"), sh().rect(20, 22, 60, 42))
		c.fill(alpha(white, 0.35), sh().poly(20, 22, 50, 22, 20, 52))
		c.fill(hex("#8a939e"), sh().poly(10, 70, 90, 70, 96, 84, 4, 84))
		c.fill(hex("#c3cad2"), sh().poly(14, 72, 86, 72, 90, 80, 10, 80))
		c.fill(hex("#5c6670"), sh().rect(40, 76, 20, 3))
	},
	"telephone_receiver": func(c *cv) {
		h := func(e float64) *shape {
			b := sh().quad(15+2*e, -26, 2, 0, -14, 26, 2).rrect(-44-e, -2-e, 26+2*e, 20+2*e, 8+e).rrect(18-e, -2-e, 26+2*e, 20+2*e, 8+e)
			return xf(b, 50, 50, 1.05, -math.Pi/4)
		}
		two(c, h, hex("#e8394d"), hex("#a5121f"), 2.5)
	},
	"headphones": func(c *cv) {
		c.fill(hex("#3a3f47"), sh().arc(50, 56, 34, math.Pi, 2*math.Pi, 9))
		c.fill(hex("#3a3f47"), sh().rrect(10, 52, 20, 36, 9).rrect(70, 52, 20, 36, 9))
		c.fill(hex("#e8394d"), sh().rrect(14, 56, 13, 28, 6).rrect(73, 56, 13, 28, 6))
	},
	"zzz": func(c *cv) {
		z := func(x, y, s float64) {
			c.fill(hex("#2f6fd6"), sh().polyline(s*0.22, x, y, x+s, y, x, y+s, x+s, y+s))
		}
		z(48, 8, 34)
		z(22, 50, 24)
		z(6, 80, 14)
	},
	"palm_tree": func(c *cv) {
		c.fill(hex("#9c6a2a"), sh().quad(10, 58, 34, 46, 64, 52, 96))
		c.fill(hex("#c47a2c"), sh().quad(6, 58, 34, 46, 64, 52, 96))
		leaf := hex("#3fae49")
		for _, a := range []float64{-2.8, -2.0, -1.2, -0.4, 0.3} {
			c.fill(shade(leaf, -0.2), xf(sh().ellipseRot(22, 0, 24, 9, 0), 58, 32, 1, a))
			c.fill(leaf, xf(sh().ellipseRot(22, 0, 22, 7, 0), 58, 32, 1, a))
		}
		c.fill(hex("#8a5a2b"), sh().circle(52, 38, 5).circle(62, 40, 5))
	},
	"house_with_garden": func(c *cv) {
		c.fill(hex("#3fae49"), sh().rect(0, 86, 100, 14))
		c.fill(hex("#2e8b3a"), sh().circle(84, 52, 14))
		c.fill(hex("#8a5a2b"), sh().rect(82, 60, 5, 28))
		c.fill(hex("#e9d3a8"), sh().rect(16, 46, 52, 42))
		c.fill(hex("#c0392b"), sh().poly(8, 50, 42, 18, 76, 50))
		c.fill(hex("#6b3d1f"), sh().rect(34, 62, 14, 26))
		c.fill(hex("#5bc0f8"), sh().rect(54, 58, 10, 10).rect(20, 58, 10, 10))
	},
	"car": func(c *cv) {
		c.fill(hex("#a5121f"), sh().rrect(6, 46, 88, 30, 10).poly(24, 48, 34, 24, 66, 24, 80, 48))
		c.fill(hex("#e8394d"), sh().rrect(8, 48, 84, 26, 9).poly(27, 50, 36, 27, 64, 27, 77, 50))
		c.fill(hex("#bfe3ff"), sh().poly(32, 48, 39, 31, 49, 31, 49, 48).poly(53, 48, 53, 31, 62, 31, 72, 48))
		c.fill(hex("#2b2f36"), sh().circle(28, 76, 11).circle(72, 76, 11))
		c.fill(hex("#c3cad2"), sh().circle(28, 76, 5).circle(72, 76, 5))
		c.fill(hex("#ffd93b"), sh().rrect(86, 54, 7, 6, 2))
	},
	"snowflake": func(c *cv) {
		col := hex("#5bc0f8")
		s := sh()
		for i := 0; i < 6; i++ {
			a := math.Pi / 3 * float64(i)
			ca, sa := math.Cos(a), math.Sin(a)
			s.line(50, 50, 50+42*ca, 50+42*sa, 6)
			for _, d := range []float64{20, 32} {
				bx, by := 50+d*ca, 50+d*sa
				for _, side := range []float64{-1, 1} {
					b := a + side*math.Pi/4
					s.line(bx, by, bx+11*math.Cos(b), by+11*math.Sin(b), 5)
				}
			}
		}
		c.fill(col, s)
	},
	"christmas_tree": func(c *cv) {
		c.fill(hex("#8a5a2b"), sh().rect(44, 80, 12, 14))
		g := hex("#2e8b3a")
		c.fill(g, sh().poly(50, 12, 74, 42, 26, 42).poly(50, 28, 82, 62, 18, 62).poly(50, 44, 90, 84, 10, 84))
		c.fill(hex("#ffd93b"), sh().star(50, 12, 11, 4.5, 5, 0))
		c.fill(hex("#e8394d"), sh().circle(40, 38, 4).circle(62, 56, 4).circle(32, 76, 4).circle(70, 78, 4))
		c.fill(hex("#5bc0f8"), sh().circle(56, 34, 3.5).circle(38, 56, 3.5).circle(52, 72, 3.5))
	},
	"hourglass_flowing_sand": func(c *cv) {
		c.fill(hex("#bfe3ff"), sh().poly(24, 14, 76, 14, 54, 50, 76, 86, 24, 86, 46, 50))
		c.fill(hex("#f6c443"), sh().poly(34, 28, 66, 28, 52, 46, 48, 46).poly(30, 82, 70, 82, 50, 64).rect(49, 46, 2, 30))
		c.fill(hex("#9c6a2a"), sh().rrect(16, 6, 68, 10, 4).rrect(16, 84, 68, 10, 4))
	},
	"speech_balloon": func(c *cv) {
		b := func(e float64) *shape {
			return sh().ellipse(50, 44, 42+e, 32+e).poly(22-e, 60, 40, 70, 14-e, 92+e)
		}
		two(c, b, white, hex("#8a939e"), 3)
		c.fill(hex("#8a939e"), sh().circle(32, 44, 4.5).circle(50, 44, 4.5).circle(68, 44, 4.5))
	},
	"lock": func(c *cv) {
		c.fill(hex("#8a939e"), sh().arc(50, 42, 20, math.Pi, 2*math.Pi, 9).line(30, 42, 30, 50, 9).line(70, 42, 70, 50, 9))
		two(c, func(e float64) *shape { return sh().rrect(18-e, 46-e, 64+2*e, 46+2*e, 8+e) }, goldC, goldO, 2.5)
		c.fill(hex("#7a5306"), sh().circle(50, 64, 6).rect(47.5, 64, 5, 14))
	},
	"hammer_and_wrench": func(c *cv) {
		w := func(s *shape) *shape { return xf(s, 50, 50, 1, math.Pi/4) }
		c.fill(hex("#5c6670"), w(sh().rrect(-5, -26, 10, 64, 4).circle(0, -32, 14).rect(-4, -48, 8, 18).hole()))
		c.fill(hex("#c3cad2"), w(sh().rrect(-3.5, -24, 7, 60, 3).circle(0, -32, 12).rect(-3, -48, 6, 18).hole()))
		h := func(s *shape) *shape { return xf(s, 50, 50, 1, -math.Pi/4) }
		c.fill(hex("#9c6a2a"), h(sh().rrect(-5, -16, 10, 56, 4)))
		c.fill(hex("#c47a2c"), h(sh().rrect(-3.5, -14, 7, 52, 3)))
		c.fill(hex("#3a3f47"), h(sh().rrect(-20, -34, 40, 18, 4)))
		c.fill(hex("#5c6670"), h(sh().rrect(-18, -32, 36, 14, 3)))
	},
}

func joyFace(c *cv) {
	yface(c)
	c.fill(featC, sh().arc(33, 42, 8, math.Pi*1.05, math.Pi*1.85, 5.2).arc(67, 42, 8, math.Pi*1.15, math.Pi*1.95, 5.2))
	openMouth(c, 56, 28, 26, true, true)
	c.fill(shade(tearC, -0.25), sh().drop(12, 52, 11).drop(88, 52, 11))
	c.fill(tearC, sh().drop(12, 52, 9).drop(88, 52, 9))
}

func slightSmile(c *cv) {
	yface(c)
	eyesDot(c, 40)
	smileArc(c, 74, 22, 5.5)
}

// confetti scatters small pieces: x, y, colour index.
func confetti(c *cv, at [][3]float64) {
	cols := []color.RGBA{hex("#ff5a8a"), hex("#2f8de4"), hex("#3fae49"), hex("#ffd93b")}
	for i, p := range at {
		c.fill(cols[int(p[2])%4], xf(sh().rect(-4, -2, 8, 4), p[0], p[1], 1, float64(i)*1.3+0.4))
	}
}

func tada(c *cv) {
	cone := sh().poly(6, 94, 30, 34, 66, 70)
	c.fill(hex("#e8a317"), cone)
	c.fillClip(hex("#e8394d"), sh().line(10, 64, 40, 94, 7).line(22, 42, 58, 78, 7), cone)
	c.fill(hex("#c98a07"), sh().ellipseRot(48, 52, 26, 8, math.Pi/4))
	c.fill(hex("#ffd93b"), sh().ellipseRot(48, 52, 23, 5.5, math.Pi/4))
	c.fill(hex("#2f8de4"), sh().quad(4, 52, 44, 60, 16, 80, 12))
	c.fill(hex("#ff5a8a"), sh().quad(4, 56, 48, 80, 34, 92, 46))
	c.fill(hex("#3fae49"), sh().quad(4, 50, 40, 44, 22, 34, 10))
	confetti(c, [][3]float64{{70, 6, 0}, {92, 24, 3}, {72, 60, 1}, {88, 70, 2}, {24, 18, 1}})
}

func honeyPot(c *cv) {
	pot := func(e float64) *shape {
		return sh().blob(20-e, 40, 50, 30-e, 80+e, 40, 88+e, 66, 72, 92+e, 28, 92+e, 12-e, 66)
	}
	two(c, pot, hex("#c47a2c"), hex("#7a4a1a"), 2.5)
	c.fill(hex("#7a4a1a"), sh().rrect(22, 24, 56, 14, 6))
	c.fill(hex("#a8642a"), sh().rrect(24, 26, 52, 10, 5))
	c.fill(hex("#ffb81c"), sh().blob(26, 36, 74, 36, 72, 44, 66, 46, 64, 60, 60, 46, 40, 46, 36, 54, 32, 46))
	c.fill(hex("#fff1c9"), sh().rrect(30, 56, 40, 22, 4))
	c.text(50, 72, 11, hex("#7a4a1a"), "HONEY", alignCenter, true)
	c.fill(hex("#c98a07"), sh().line(70, 6, 52, 30, 4))
	c.fill(hex("#9c6a2a"), sh().rrect(64, 0, 14, 10, 4))
}

func catFace(c *cv, fur, dark, light color.RGBA) {
	head := func(e float64) *shape {
		return sh().ellipse(50, 56, 42+e, 36+e).poly(12-e, 46, 16-e, 8-e, 42, 26).poly(88+e, 46, 84+e, 8-e, 58, 26)
	}
	two(c, head, fur, dark, 2.5)
	c.fill(pinkC, sh().poly(20, 36, 22, 16, 36, 28).poly(80, 36, 78, 16, 64, 28))
	c.fill(light, sh().ellipse(50, 70, 22, 15))
	c.fill(featC, sh().ellipse(34, 52, 5, 7).ellipse(66, 52, 5, 7))
	c.fill(hex("#e2687a"), sh().poly(45, 62, 55, 62, 50, 68))
	c.fill(featC, sh().arc(44, 68, 6, math.Pi*0.1, math.Pi*0.9, 2.5).arc(56, 68, 6, math.Pi*0.1, math.Pi*0.9, 2.5))
	c.fill(dark, sh().line(4, 62, 26, 66, 2.2).line(6, 74, 26, 72, 2.2).line(96, 62, 74, 66, 2.2).line(94, 74, 74, 72, 2.2))
}

func chart(c *cv, up bool) {
	c.fill(hex("#a8b0ba"), sh().rrect(6, 6, 88, 88, 8))
	c.fill(white, sh().rrect(8, 8, 84, 84, 7))
	for i := 0; i < 4; i++ {
		c.fill(hex("#e3e7ec"), sh().rect(14, 20+float64(i)*18, 72, 2).rect(20+float64(i)*18, 14, 2, 72))
	}
	if up {
		c.fill(hex("#2e9e45"), sh().polyline(7, 16, 76, 38, 56, 54, 64, 82, 26).poly(70, 22, 88, 18, 86, 36))
	} else {
		c.fill(hex("#e0202f"), sh().polyline(7, 16, 26, 38, 46, 54, 38, 82, 72).poly(70, 76, 88, 80, 86, 62))
	}
}

// barChart is a card with three bars (Pulse's polls use it).
func barChart(c *cv) {
	c.fill(hex("#a8b0ba"), sh().rrect(6, 6, 88, 88, 8))
	c.fill(white, sh().rrect(8, 8, 84, 84, 7))
	c.fill(hex("#e3e7ec"), sh().rect(14, 84, 72, 2))
	c.fill(hex("#e0202f"), sh().rect(20, 50, 16, 34))
	c.fill(hex("#2f8de4"), sh().rect(42, 24, 16, 60))
	c.fill(hex("#2e9e45"), sh().rect(64, 40, 16, 44))
}

func trophy(c *cv) {
	cup := func(e float64) *shape {
		return sh().halfDown(50, 12-e, 30+e, 46+2*e).ring(22, 32, 12, 6+2*e).ring(78, 32, 12, 6+2*e).
			rect(44-e, 54, 12+2*e, 20).rrect(28-e, 72-e, 44+2*e, 10+2*e, 3).rrect(22-e, 80-e, 56+2*e, 14+2*e, 3)
	}
	two(c, cup, goldC, goldO, 2.5)
	c.fill(hex("#7a5306"), sh().rrect(32, 83, 36, 8, 2))
	c.fill(alpha(white, 0.45), sh().ellipseRot(36, 26, 5, 12, 0.1))
}

// paper is a sheet with a folded corner.
func paper(c *cv, x, y, w, h float64) {
	f := 16.0
	c.fill(hex("#a8b0ba"), sh().poly(x-2, y-2, x+w-f+1, y-2, x+w+2, y+f-1, x+w+2, y+h+2, x-2, y+h+2))
	c.fill(paperC, sh().poly(x, y, x+w-f, y, x+w, y+f, x+w, y+h, x, y+h))
	c.fill(hex("#dfe3e8"), sh().poly(x+w-f, y, x+w-f, y+f, x+w, y+f))
}

// fallbackEmoji is the neutral stand-in for an emoji without a drawing:
// a soft round face in a pastel picked by the stem.
func fallbackEmoji(c *cv, stem string) {
	h := fnv.New32a()
	h.Write([]byte(stem))
	pastels := []string{"#a7c7f2", "#b8e0c2", "#f5c6a5", "#d9c2f0", "#f7d488", "#f4b6c2", "#a9dfe0"}
	base := hex(pastels[h.Sum32()%uint32(len(pastels))])
	c.fill(shade(base, -0.25), sh().circle(50, 51, 44))
	c.fillSrc(c.grad(50, 9, 50, 93, shade(base, 0.35), base), sh().circle(50, 51, 42))
	d := shade(base, -0.55)
	c.fill(d, sh().circle(36, 44, 4.5).circle(64, 44, 4.5))
	c.fill(d, sh().arc(50, 52, 15, math.Pi*0.25, math.Pi*0.75, 4.5))
}

// ---- Quillfeather's custom emoji ----

var customArt = map[string]func(c *cv){
	"quill": quillEmoji,
	"rip-lamby": func(c *cv) {
		c.fill(hex("#ffd93b"), sh().ring(50, 12, 18, 5).hole())
		c.fill(hex("#ffd93b"), sh().ellipse(50, 12, 20, 6).ellipse(50, 12, 15, 3).hole())
		body := func(e float64) *shape { return sh().rrect(8-e, 30-e, 84+2*e, 44+2*e, 10+e) }
		two(c, body, hex("#b9c0c9"), hex("#5c6670"), 2.5)
		c.fill(hex("#2b2f36"), sh().rrect(16, 60, 68, 7, 3))
		c.fill(paperC, sh().poly(26, 64, 74, 64, 70, 94, 30, 94))
		c.fill(hex("#2b2f36"), sh().line(26, 38, 38, 50, 4.5).line(38, 38, 26, 50, 4.5).line(62, 38, 74, 50, 4.5).line(74, 38, 62, 50, 4.5))
		c.text(50, 88, 15, hex("#d6343f"), "RIP", alignCenter, true)
	},
	"prinny": func(c *cv) {
		c.fill(outC, sh().rrect(26, 4, 48, 26, 3))
		c.fill(white, sh().rect(28, 6, 44, 24))
		body := func(e float64) *shape { return sh().rrect(6-e, 24-e, 88+2*e, 54+2*e, 12+e) }
		two(c, body, hex("#9aa3ad"), hex("#3a3f47"), 2.5)
		c.fill(hex("#2b2f36"), sh().line(22, 36, 42, 42, 4.5).line(58, 42, 78, 36, 4.5).circle(34, 50, 5).circle(66, 50, 5))
		c.fill(hex("#2b2f36"), sh().arc(50, 74, 12, math.Pi*1.2, math.Pi*1.8, 4.5))
		c.fill(hex("#2b2f36"), sh().rect(20, 72, 60, 6))
		c.fill(white, sh().poly(24, 76, 76, 76, 74, 96, 26, 96))
		c.text(50, 92, 13, hex("#d6343f"), "PR-7", alignCenter, true)
	},
	"big-linda": forklift,
	"honey-pot-dale": func(c *cv) {
		honeyPot(c)
		c.fill(hex("#2f6fd6"), sh().polyline(4, 30, 30, 50, 58, 70, 30))
		c.fill(white, sh().rrect(40, 54, 20, 26, 3))
		c.fill(hex("#2f6fd6"), sh().rect(40, 54, 20, 7))
		c.fill(outC, sh().rect(44, 66, 12, 2.5).rect(44, 72, 9, 2.5))
	},
	"mr-pemberton": func(c *cv) {
		catFace(c, hex("#9aa3ad"), hex("#5c6670"), hex("#d5dae0"))
		// smug half-lidded eyes over the plain ones
		c.fill(hex("#9aa3ad"), sh().rect(26, 42, 16, 9).rect(58, 42, 16, 9))
		c.fill(hex("#5c6670"), sh().line(26, 50, 42, 50, 3).line(58, 50, 74, 50, 3))
		c.fill(hex("#2b2f36"), sh().ellipse(50, 30, 10, 3))
		c.fill(hex("#d6343f"), sh().rrect(28, 84, 44, 8, 4))
		c.fill(goldC, sh().circle(50, 92, 5))
	},
	"quillie": func(c *cv) {
		trophy(c)
		c.fill(hex("#127a78"), xf(sh().ellipse(0, -10, 7, 22), 52, 30, 1, 0.4))
		c.fill(hex("#2bb3a8"), xf(sh().ellipse(1, -10, 4, 18), 52, 30, 1, 0.4))
		c.fill(hex("#1d2b36"), xf(sh().line(0, -4, 0, 22, 2.5), 52, 30, 1, 0.4))
	},
	"ultra-bright": func(c *cv) {
		for i := 0; i < 12; i++ {
			a := math.Pi / 6 * float64(i)
			c.fill(hex("#ffd93b"), sh().line(50+30*math.Cos(a), 50+30*math.Sin(a), 50+46*math.Cos(a), 50+46*math.Sin(a), 5))
		}
		c.fill(alpha(hex("#fff6b0"), 0.8), sh().circle(50, 50, 34))
		c.fill(hex("#8a939e"), sh().rrect(20, 30, 60, 40, 4))
		c.fill(white, sh().rrect(22, 32, 56, 36, 3))
		c.fill(hex("#2f6fd6"), sh().rect(22, 44, 56, 12))
		c.text(50, 54, 10, white, "ULTRA", alignCenter, true)
	},
	"yogurt-alert": func(c *cv) {
		c.fill(hex("#ff5a5a"), sh().line(30, 10, 36, 16, 4).line(70, 10, 64, 16, 4).line(50, 2, 50, 9, 4))
		c.fill(hex("#b5182c"), sh().halfDown(50, 30, 14, -18).rect(34, 28, 32, 5))
		c.fill(hex("#ef3b3b"), sh().halfDown(50, 29, 12, -15))
		cup := func(e float64) *shape { return sh().poly(18-e, 38-e, 82+e, 38-e, 72+e, 94+e, 28-e, 94+e) }
		two(c, cup, white, hex("#8a939e"), 2.5)
		c.fill(hex("#8a939e"), sh().rrect(14, 34, 72, 8, 3))
		c.fill(pinkC, sh().rect(24, 54, 52, 22))
		c.fill(hex("#d6343f"), sh().circle(50, 65, 7))
		c.fill(greenC, sh().poly(48, 58, 52, 52, 56, 58))
	},
	"safety-whistle": func(c *cv) {
		w := func(e float64) *shape {
			return sh().circle(40, 58, 28+e).rrect(40, 30-e, 54+e, 22+2*e, 6)
		}
		two(c, w, hex("#ff8a1f"), hex("#b3550a"), 2.5)
		c.fill(hex("#b3550a"), sh().circle(40, 58, 9).rect(84, 36, 6, 10))
		c.fill(alpha(white, 0.5), sh().ellipseRot(30, 46, 6, 10, 0.6))
		c.fill(hex("#2f6fd6"), sh().ring(14, 30, 8, 4))
	},
	"stonks-paper": func(c *cv) {
		paper(c, 12, 8, 62, 84)
		c.fill(lineC, sh().rect(20, 26, 40, 3).rect(20, 36, 34, 3))
		c.fill(hex("#2e8b3a"), sh().polyline(10, 24, 82, 44, 62, 56, 70, 82, 38))
		c.fill(hex("#2e8b3a"), sh().poly(66, 30, 94, 22, 88, 50))
		c.fill(hex("#43b649"), sh().polyline(6, 24, 82, 44, 62, 56, 70, 80, 40))
	},
	"okayest-boss": func(c *cv) {
		c.fill(hex("#a8b0ba"), sh().quad(4, 40, 22, 34, 12, 42, 4).quad(4, 58, 22, 52, 12, 60, 4))
		c.fill(hex("#8a939e"), sh().ring(80, 56, 12, 9))
		c.fill(white, sh().ring(80, 56, 12, 5))
		c.fill(hex("#8a939e"), sh().rrect(10, 26, 66, 68, 10))
		c.fill(white, sh().rrect(12, 28, 62, 64, 9))
		c.text(43, 68, 30, hex("#2f6fd6"), "OK", alignCenter, true)
		c.fill(hex("#e8394d"), sh().line(22, 80, 64, 80, 3.5))
	},
}

func quillEmoji(c *cv) {
	r := func(s *shape) *shape { return xf(s, 52, 46, 1, math.Pi/4) }
	c.fill(hex("#0b5554"), r(sh().blob(0, -46, 15, -30, 18, -4, 10, 22, 0, 34, -10, 22, -18, -4, -15, -30)))
	c.fill(hex("#2bb3a8"), r(sh().blob(0, -42, 12, -28, 14, -4, 8, 18, 0, 28, -8, 18, -14, -4, -12, -28)))
	c.fill(hex("#8fe0d6"), r(sh().blob(-2, -36, -10, -24, -12, -4, -7, 14, -1, 18)))
	c.fill(hex("#0b5554"), r(sh().line(0, -40, 0, 40, 3.5)))
	c.fill(hex("#1d2b36"), r(sh().poly(-3, 36, 3, 36, 0, 52)))
	c.fill(hex("#1d2b36"), sh().ellipse(22, 90, 12, 4))
}

func forklift(c *cv) {
	y := hex("#ffc61a")
	yo := hex("#b88a00")
	c.fill(outC, sh().rect(70, 10, 5, 70).rect(70, 74, 28, 5))
	body := func(e float64) *shape {
		return sh().rrect(8-e, 46-e, 60+2*e, 30+2*e, 6+e).polyline(5+2*e, 20, 46, 24, 16, 54, 16, 62, 46)
	}
	two(c, body, y, yo, 2.5)
	c.fill(hex("#bfe3ff"), sh().poly(26, 44, 29, 21, 52, 21, 58, 44))
	c.fill(hex("#2b2f36"), sh().circle(22, 80, 12).circle(56, 80, 12))
	c.fill(hex("#c3cad2"), sh().circle(22, 80, 5).circle(56, 80, 5))
	c.fill(hex("#2b2f36"), sh().rect(10, 58, 12, 4))
	// Big Linda's bow
	c.fill(hex("#e8394d"), sh().poly(36, 8, 26, 2, 26, 14).poly(36, 8, 46, 2, 46, 14).circle(36, 8, 3.5))
}
