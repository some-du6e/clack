package imgcache

// Slack's bundled SVG assets (internal/slackasset), rasterized for the
// cache. Slack's web client draws them with <img src="….svg">, so Chromium
// rasterizes the vector at the size the box is drawn, not a fixed bitmap:
// rasterSVG draws it at the size the cache was asked for (Get's sizePx),
// coverage-antialiased (x/image/vector), each element painted over the last.
//
// Only bytes Clack ships reach it: decodeLoad hands it an asset's bytes
// when the URL is a bundled SVG (slackasset.IsSVG) and its bytes are the
// embedded ones. Any other SVG, from any host, stays undecodable (formatOf
// has no SVG), as before.
//
// It reads the subset those assets use and refuses the rest rather than
// drawing it differently: an <svg> root with width/height and viewBox
// (preserveAspectRatio's default, xMidYMid meet), <g> and <path> with a
// d, and fill as #rgb, #rrggbb or none (inherited, black by default) under
// the nonzero rule. Any other element or attribute that changes the
// picture (transform, stroke, opacity, style, class, fill-rule evenodd, a
// url() or named fill) is an error.

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"strconv"
	"strings"

	"golang.org/x/image/vector"
)

// svgMaxSide bounds a rasterized asset (ScaleBudget's default bound).
const svgMaxSide = 2048

var errSVG = errors.New("imgcache: unsupported SVG")

func svgErr(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errSVG}, a...)...)
}

// svgFill is a fill: none, or a straight-alpha color.
type svgFill struct {
	none bool
	c    color.NRGBA
}

type svgShape struct {
	d    []svgSeg
	fill svgFill
}

// svgSeg is one drawing command in user units, absolute: 'M' (p0), 'L'
// (p0), 'C' (p0 p1 p2), 'Z'.
type svgSeg struct {
	op         byte
	p0, p1, p2 [2]float64
}

type svgDoc struct {
	w, h   float64    // intrinsic size, CSS px
	vb     [4]float64 // viewBox
	shapes []svgShape
}

// rasterSVG draws data at size px on its longer side (its intrinsic size
// when size is 0), everything it allocates charged to b.
func rasterSVG(data []byte, size int, b *Budget) (*image.RGBA, error) {
	doc, err := parseSVG(data)
	if err != nil {
		return nil, err
	}
	w, h := doc.w, doc.h
	if size > 0 {
		k := float64(size) / math.Max(w, h)
		w, h = w*k, h*k
	}
	iw, ih := int(math.Round(w)), int(math.Round(h))
	if iw < 1 {
		iw = 1
	}
	if ih < 1 {
		ih = 1
	}
	if iw > svgMaxSide || ih > svgMaxSide {
		return nil, ErrTooBig
	}
	// the picture, and the rasterizer's accumulation buffer (a float32 or
	// uint32 a pixel)
	if err := b.Charge(2 * rgbaBytes(image.Pt(iw, ih))); err != nil {
		return nil, budgetErr(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, iw, ih))
	// viewBox → viewport, xMidYMid meet
	k := math.Min(float64(iw)/doc.vb[2], float64(ih)/doc.vb[3])
	tx := (float64(iw)-doc.vb[2]*k)/2 - doc.vb[0]*k
	ty := (float64(ih)-doc.vb[3]*k)/2 - doc.vb[1]*k
	pt := func(p [2]float64) (float32, float32) {
		return float32(p[0]*k + tx), float32(p[1]*k + ty)
	}
	z := vector.NewRasterizer(iw, ih)
	z.DrawOp = draw.Over
	for _, s := range doc.shapes {
		if s.fill.none || s.fill.c.A == 0 || len(s.d) == 0 {
			continue
		}
		z.Reset(iw, ih)
		open := false
		for _, g := range s.d {
			switch g.op {
			case 'M':
				if open {
					z.ClosePath()
				}
				z.MoveTo(pt(g.p0))
				open = true
			case 'L':
				z.LineTo(pt(g.p0))
			case 'C':
				ax, ay := pt(g.p0)
				bx, by := pt(g.p1)
				cx, cy := pt(g.p2)
				z.CubeTo(ax, ay, bx, by, cx, cy)
			case 'Z':
				if open {
					z.ClosePath()
				}
				open = false
			}
		}
		if open {
			z.ClosePath() // a fill closes every subpath
		}
		z.Draw(dst, dst.Bounds(), image.NewUniform(s.fill.c), image.Point{})
	}
	return dst, nil
}

// parseSVG reads the subset rasterSVG draws (see the top of this file).
func parseSVG(data []byte) (*svgDoc, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	var (
		doc   *svgDoc
		fills []svgFill // the inherited fill at each open element
		depth int
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, svgErr("%v", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg" {
				return nil, svgErr("element %s:%s", t.Name.Space, t.Name.Local)
			}
			parent := svgFill{c: color.NRGBA{A: 0xff}} // fill's initial value: black
			if len(fills) > 0 {
				parent = fills[len(fills)-1]
			}
			attrs := map[string]string{}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					continue
				}
				if a.Name.Space != "" {
					return nil, svgErr("attribute %s:%s", a.Name.Space, a.Name.Local)
				}
				attrs[a.Name.Local] = a.Value
			}
			f, err := svgFillOf(attrs, parent)
			if err != nil {
				return nil, err
			}
			switch {
			case depth == 0 && t.Name.Local == "svg":
				if doc, err = svgRoot(attrs); err != nil {
					return nil, err
				}
			case depth > 0 && t.Name.Local == "g":
				if err := svgOnly(attrs, "fill"); err != nil {
					return nil, err
				}
			case depth > 0 && t.Name.Local == "path":
				if err := svgOnly(attrs, "fill", "d", "fill-rule"); err != nil {
					return nil, err
				}
				if r, ok := attrs["fill-rule"]; ok && strings.TrimSpace(r) != "nonzero" {
					return nil, svgErr("fill-rule %q", r)
				}
				d, err := parsePathData(attrs["d"])
				if err != nil {
					return nil, err
				}
				doc.shapes = append(doc.shapes, svgShape{d: d, fill: f})
			default:
				return nil, svgErr("element <%s>", t.Name.Local)
			}
			fills = append(fills, f)
			depth++
		case xml.EndElement:
			depth--
			fills = fills[:len(fills)-1]
		case xml.CharData:
			if len(bytes.TrimSpace(t)) != 0 {
				return nil, svgErr("text content")
			}
		case xml.ProcInst, xml.Comment:
		default: // a DOCTYPE (entities) or other directive
			return nil, svgErr("directive")
		}
	}
	if doc == nil {
		return nil, svgErr("no <svg> root")
	}
	return doc, nil
}

// svgOnly refuses attributes other than allowed (and the fill every
// element reads).
func svgOnly(attrs map[string]string, allowed ...string) error {
	for k := range attrs {
		ok := false
		for _, a := range allowed {
			ok = ok || k == a
		}
		if !ok {
			return svgErr("attribute %s", k)
		}
	}
	return nil
}

func svgRoot(attrs map[string]string) (*svgDoc, error) {
	if err := svgOnly(attrs, "width", "height", "viewBox", "fill", "version"); err != nil {
		return nil, err
	}
	doc := &svgDoc{}
	vb := strings.FieldsFunc(attrs["viewBox"], func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r' })
	if len(vb) != 4 {
		return nil, svgErr("viewBox %q", attrs["viewBox"])
	}
	for i, s := range vb {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, svgErr("viewBox %q", attrs["viewBox"])
		}
		doc.vb[i] = v
	}
	if doc.vb[2] <= 0 || doc.vb[3] <= 0 {
		return nil, svgErr("viewBox %q", attrs["viewBox"])
	}
	length := func(k string, def float64) (float64, error) {
		s, ok := attrs[k]
		if !ok {
			return def, nil
		}
		v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "px"), 64)
		if err != nil || v <= 0 || math.IsInf(v, 0) || v > svgMaxSide {
			return 0, svgErr("%s %q", k, s)
		}
		return v, nil
	}
	var err error
	if doc.w, err = length("width", doc.vb[2]); err != nil {
		return nil, err
	}
	if doc.h, err = length("height", doc.vb[3]); err != nil {
		return nil, err
	}
	return doc, nil
}

// svgFillOf is an element's fill: its own attribute, else parent's.
func svgFillOf(attrs map[string]string, parent svgFill) (svgFill, error) {
	s, ok := attrs["fill"]
	if !ok {
		return parent, nil
	}
	s = strings.TrimSpace(s)
	if s == "none" {
		return svgFill{none: true}, nil
	}
	hex := func(s string) (uint8, bool) {
		v, err := strconv.ParseUint(s, 16, 8)
		return uint8(v), err == nil
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) == 6 {
			r, ok1 := hex(h[0:2])
			g, ok2 := hex(h[2:4])
			b, ok3 := hex(h[4:6])
			if ok1 && ok2 && ok3 {
				return svgFill{c: color.NRGBA{R: r, G: g, B: b, A: 0xff}}, nil
			}
		}
	}
	return svgFill{}, svgErr("fill %q", s)
}

// svgMaxSegs bounds one path's commands (the assets have a few dozen).
const svgMaxSegs = 1 << 14

// parsePathData reads SVG path data (the full grammar: M L H V C S Q T A Z,
// absolute and relative, implicit repeats) into absolute M/L/C/Z
// segments: quadratics are raised to cubics, arcs split into cubics of at
// most 90° each.
func parsePathData(d string) ([]svgSeg, error) {
	p := pathLexer{s: d}
	var (
		out      []svgSeg
		cur, sub [2]float64 // pen, subpath start
		lastCtl  [2]float64 // the previous command's last control point
		lastOp   byte
		cmd      byte
		started  bool
	)
	add := func(s svgSeg) error {
		if len(out) >= svgMaxSegs {
			return svgErr("path too long")
		}
		out = append(out, s)
		return nil
	}
	for {
		p.skipSep()
		if p.done() {
			break
		}
		if c := p.s[p.i]; isPathCmd(c) {
			cmd = c
			p.i++
		} else if cmd == 0 {
			return nil, svgErr("path data %q", d)
		}
		rel := cmd >= 'a'
		up := cmd &^ 0x20
		if !started && up != 'M' {
			return nil, svgErr("path data must start with M")
		}
		abs := func(x, y float64) [2]float64 {
			if rel {
				return [2]float64{cur[0] + x, cur[1] + y}
			}
			return [2]float64{x, y}
		}
		var err error
		switch up {
		case 'Z':
			if err = add(svgSeg{op: 'Z'}); err != nil {
				return nil, err
			}
			cur = sub
			lastOp, lastCtl = 'Z', cur
			cmd = 0 // Z takes no arguments: what follows needs its own command
			continue
		case 'M':
			var x, y float64
			if x, y, err = p.pair(); err != nil {
				return nil, err
			}
			cur = abs(x, y)
			sub = cur
			started = true
			if err = add(svgSeg{op: 'M', p0: cur}); err != nil {
				return nil, err
			}
			// later pairs are implicit lineto commands
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
			lastOp, lastCtl = 'M', cur
			continue
		case 'L':
			var x, y float64
			if x, y, err = p.pair(); err != nil {
				return nil, err
			}
			cur = abs(x, y)
			err = add(svgSeg{op: 'L', p0: cur})
			lastCtl = cur
		case 'H', 'V':
			var v float64
			if v, err = p.number(); err != nil {
				return nil, err
			}
			n := cur
			i := 0
			if up == 'V' {
				i = 1
			}
			if rel {
				n[i] += v
			} else {
				n[i] = v
			}
			cur = n
			err = add(svgSeg{op: 'L', p0: cur})
			lastCtl = cur
		case 'C', 'S':
			var c1, c2, e [2]float64
			if up == 'C' {
				var x, y float64
				if x, y, err = p.pair(); err != nil {
					return nil, err
				}
				c1 = abs(x, y)
			} else if lastOp == 'C' || lastOp == 'S' {
				c1 = [2]float64{2*cur[0] - lastCtl[0], 2*cur[1] - lastCtl[1]}
			} else {
				c1 = cur
			}
			var x, y float64
			if x, y, err = p.pair(); err != nil {
				return nil, err
			}
			c2 = abs(x, y)
			if x, y, err = p.pair(); err != nil {
				return nil, err
			}
			e = abs(x, y)
			err = add(svgSeg{op: 'C', p0: c1, p1: c2, p2: e})
			cur, lastCtl = e, c2
		case 'Q', 'T':
			var q [2]float64
			if up == 'Q' {
				var x, y float64
				if x, y, err = p.pair(); err != nil {
					return nil, err
				}
				q = abs(x, y)
			} else if lastOp == 'Q' || lastOp == 'T' {
				q = [2]float64{2*cur[0] - lastCtl[0], 2*cur[1] - lastCtl[1]}
			} else {
				q = cur
			}
			x, y, perr := p.pair()
			if perr != nil {
				return nil, perr
			}
			e := abs(x, y)
			c1 := [2]float64{cur[0] + 2.0/3*(q[0]-cur[0]), cur[1] + 2.0/3*(q[1]-cur[1])}
			c2 := [2]float64{e[0] + 2.0/3*(q[0]-e[0]), e[1] + 2.0/3*(q[1]-e[1])}
			err = add(svgSeg{op: 'C', p0: c1, p1: c2, p2: e})
			cur, lastCtl = e, q
		case 'A':
			var rx, ry, rot float64
			var large, sweep bool
			if rx, err = p.number(); err == nil {
				if ry, err = p.number(); err == nil {
					if rot, err = p.number(); err == nil {
						if large, err = p.flag(); err == nil {
							sweep, err = p.flag()
						}
					}
				}
			}
			if err != nil {
				return nil, err
			}
			x, y, perr := p.pair()
			if perr != nil {
				return nil, perr
			}
			e := abs(x, y)
			for _, s := range arcToCubics(cur, e, rx, ry, rot, large, sweep) {
				if err = add(s); err != nil {
					return nil, err
				}
			}
			cur, lastCtl = e, e
		}
		if err != nil {
			return nil, err
		}
		lastOp = up
	}
	return out, nil
}

func isPathCmd(c byte) bool { return strings.IndexByte("MmLlHhVvCcSsQqTtAaZz", c) >= 0 }

// pathLexer reads path data's numbers and flags.
type pathLexer struct {
	s string
	i int
}

func (p *pathLexer) done() bool { return p.i >= len(p.s) }

// skipSep skips whitespace and at most one comma.
func (p *pathLexer) skipSep() {
	comma := false
	for p.i < len(p.s) {
		switch c := p.s[p.i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
			p.i++
		case c == ',' && !comma:
			comma = true
			p.i++
		default:
			return
		}
	}
}

func (p *pathLexer) number() (float64, error) {
	p.skipSep()
	st := p.i
	if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
		p.i++
	}
	digits, dot := 0, false
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c >= '0' && c <= '9' {
			digits++
		} else if c == '.' && !dot {
			dot = true
		} else {
			break
		}
		p.i++
	}
	if digits == 0 {
		return 0, svgErr("path data: number expected at %d", st)
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		j := p.i + 1
		if j < len(p.s) && (p.s[j] == '+' || p.s[j] == '-') {
			j++
		}
		k := j
		for k < len(p.s) && p.s[k] >= '0' && p.s[k] <= '9' {
			k++
		}
		if k > j {
			p.i = k
		}
	}
	v, err := strconv.ParseFloat(p.s[st:p.i], 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, svgErr("path data: number %q", p.s[st:p.i])
	}
	return v, nil
}

func (p *pathLexer) pair() (float64, float64, error) {
	x, err := p.number()
	if err != nil {
		return 0, 0, err
	}
	y, err := p.number()
	return x, y, err
}

// flag reads an arc flag: one 0 or 1, which needs no separator after it.
func (p *pathLexer) flag() (bool, error) {
	p.skipSep()
	if p.i < len(p.s) && (p.s[p.i] == '0' || p.s[p.i] == '1') {
		p.i++
		return p.s[p.i-1] == '1', nil
	}
	return false, svgErr("path data: arc flag expected at %d", p.i)
}

// arcToCubics is SVG's elliptical arc (implementation notes F.6.5 and
// F.6.6: zero radii are a line, radii too small are scaled up) as cubic
// Béziers of at most 90° each.
func arcToCubics(p0, p1 [2]float64, rx, ry, rotDeg float64, large, sweep bool) []svgSeg {
	if p0 == p1 {
		return nil
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		return []svgSeg{{op: 'L', p0: p1}}
	}
	phi := rotDeg * math.Pi / 180
	cos, sin := math.Cos(phi), math.Sin(phi)
	dx, dy := (p0[0]-p1[0])/2, (p0[1]-p1[1])/2
	x1 := cos*dx + sin*dy
	y1 := -sin*dx + cos*dy
	if l := x1*x1/(rx*rx) + y1*y1/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1*y1 - ry*ry*x1*x1
	den := rx*rx*y1*y1 + ry*ry*x1*x1
	co := 0.0
	if num > 0 && den > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cx1 := co * rx * y1 / ry
	cy1 := -co * ry * x1 / rx
	cx := cos*cx1 - sin*cy1 + (p0[0]+p1[0])/2
	cy := sin*cx1 + cos*cy1 + (p0[1]+p1[1])/2
	ang := func(ux, uy, vx, vy float64) float64 {
		return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	}
	th1 := ang(1, 0, (x1-cx1)/rx, (y1-cy1)/ry)
	dth := ang((x1-cx1)/rx, (y1-cy1)/ry, (-x1-cx1)/rx, (-y1-cy1)/ry)
	if !sweep && dth > 0 {
		dth -= 2 * math.Pi
	} else if sweep && dth < 0 {
		dth += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dth) / (math.Pi / 2)))
	if n < 1 {
		n = 1
	}
	step := dth / float64(n)
	t := 4.0 / 3 * math.Tan(step/4)
	at := func(th float64) (x, y, ex, ey float64) {
		c, s := math.Cos(th), math.Sin(th)
		x = cx + rx*c*cos - ry*s*sin
		y = cy + rx*c*sin + ry*s*cos
		ex = -rx*s*cos - ry*c*sin // the derivative
		ey = -rx*s*sin + ry*c*cos
		return
	}
	out := make([]svgSeg, 0, n)
	th := th1
	ax, ay, adx, ady := at(th)
	for i := 0; i < n; i++ {
		bx, by, bdx, bdy := at(th + step)
		end := [2]float64{bx, by}
		if i == n-1 {
			end = p1
		}
		out = append(out, svgSeg{op: 'C',
			p0: [2]float64{ax + t*adx, ay + t*ady},
			p1: [2]float64{bx - t*bdx, by - t*bdy},
			p2: end,
		})
		th += step
		ax, ay, adx, ady = bx, by, bdx, bdy
	}
	return out
}
