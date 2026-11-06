package icons

import (
	"math"
	"strconv"

	"gioui.org/f32"
)

// seg is one absolute path segment in viewbox units.
type seg struct {
	op byte // 'M', 'L', 'C', 'Z'
	p  [3]f32.Point
}

// parsePath parses a subset of SVG path data (M L H V C S Q T A Z, both
// absolute and relative) into absolute moves, lines and cubics.
func parsePath(d string) []seg {
	t := tokenizer{s: d}
	var out []seg
	var cur, start, lastCtrl f32.Point
	var cmd, prev byte
	for t.more() {
		if c, ok := t.cmd(); ok {
			cmd = c
		} else if cmd == 0 || cmd&^0x20 == 'Z' {
			break // stray numbers
		}
		rel := cmd >= 'a'
		up := cmd &^ 0x20
		pt := func() f32.Point {
			x, y := t.num(), t.num()
			if rel {
				return f32.Pt(cur.X+x, cur.Y+y)
			}
			return f32.Pt(x, y)
		}
		switch up {
		case 'M':
			cur = pt()
			start = cur
			out = append(out, seg{op: 'M', p: [3]f32.Point{cur}})
			// subsequent pairs are implicit linetos
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
		case 'L':
			cur = pt()
			out = append(out, seg{op: 'L', p: [3]f32.Point{cur}})
		case 'H':
			x := t.num()
			if rel {
				x += cur.X
			}
			cur = f32.Pt(x, cur.Y)
			out = append(out, seg{op: 'L', p: [3]f32.Point{cur}})
		case 'V':
			y := t.num()
			if rel {
				y += cur.Y
			}
			cur = f32.Pt(cur.X, y)
			out = append(out, seg{op: 'L', p: [3]f32.Point{cur}})
		case 'C':
			c1, c2, to := pt(), pt(), pt()
			out = append(out, seg{op: 'C', p: [3]f32.Point{c1, c2, to}})
			lastCtrl, cur = c2, to
		case 'S':
			c1 := cur
			if p := prev &^ 0x20; p == 'C' || p == 'S' {
				c1 = cur.Mul(2).Sub(lastCtrl)
			}
			c2, to := pt(), pt()
			out = append(out, seg{op: 'C', p: [3]f32.Point{c1, c2, to}})
			lastCtrl, cur = c2, to
		case 'Q':
			q, to := pt(), pt()
			out = append(out, quad(cur, q, to))
			lastCtrl, cur = q, to
		case 'T':
			q := cur
			if p := prev &^ 0x20; p == 'Q' || p == 'T' {
				q = cur.Mul(2).Sub(lastCtrl)
			}
			to := pt()
			out = append(out, quad(cur, q, to))
			lastCtrl, cur = q, to
		case 'A':
			rx, ry, rot := t.num(), t.num(), t.num()
			large, sweep := t.num() != 0, t.num() != 0
			to := pt()
			out = append(out, arc(cur, to, rx, ry, rot, large, sweep)...)
			cur = to
		case 'Z':
			out = append(out, seg{op: 'Z'})
			cur = start
		default:
			return out
		}
		prev = cmd
	}
	return out
}

func quad(from, q, to f32.Point) seg {
	c1 := from.Add(q.Sub(from).Mul(2.0 / 3))
	c2 := to.Add(q.Sub(to).Mul(2.0 / 3))
	return seg{op: 'C', p: [3]f32.Point{c1, c2, to}}
}

// arc converts an SVG elliptical arc to cubic Béziers (SVG spec F.6.5).
func arc(p0, p1 f32.Point, rx, ry, rotDeg float32, large, sweep bool) []seg {
	if p0 == p1 {
		return nil
	}
	if rx == 0 || ry == 0 {
		return []seg{{op: 'L', p: [3]f32.Point{p1}}}
	}
	fx, fy := float64(rx), float64(ry)
	if fx < 0 {
		fx = -fx
	}
	if fy < 0 {
		fy = -fy
	}
	phi := float64(rotDeg) * math.Pi / 180
	cp, sp := math.Cos(phi), math.Sin(phi)
	dx, dy := float64(p0.X-p1.X)/2, float64(p0.Y-p1.Y)/2
	x1 := cp*dx + sp*dy
	y1 := -sp*dx + cp*dy
	lam := x1*x1/(fx*fx) + y1*y1/(fy*fy)
	if lam > 1 {
		s := math.Sqrt(lam)
		fx *= s
		fy *= s
	}
	num := fx*fx*fy*fy - fx*fx*y1*y1 - fy*fy*x1*x1
	den := fx*fx*y1*y1 + fy*fy*x1*x1
	co := 0.0
	if den != 0 && num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cx1 := co * fx * y1 / fy
	cy1 := -co * fy * x1 / fx
	cx := cp*cx1 - sp*cy1 + float64(p0.X+p1.X)/2
	cy := sp*cx1 + cp*cy1 + float64(p0.Y+p1.Y)/2
	ang := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	th1 := ang(1, 0, (x1-cx1)/fx, (y1-cy1)/fy)
	dth := ang((x1-cx1)/fx, (y1-cy1)/fy, (-x1-cx1)/fx, (-y1-cy1)/fy)
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
	k := 4.0 / 3 * math.Tan(step/4)
	pt := func(t float64) (float64, float64) {
		x, y := fx*math.Cos(t), fy*math.Sin(t)
		return cp*x - sp*y + cx, sp*x + cp*y + cy
	}
	der := func(t float64) (float64, float64) {
		x, y := -fx*math.Sin(t), fy*math.Cos(t)
		return cp*x - sp*y, sp*x + cp*y
	}
	out := make([]seg, 0, n)
	t := th1
	for i := 0; i < n; i++ {
		ax, ay := pt(t)
		adx, ady := der(t)
		bx, by := pt(t + step)
		bdx, bdy := der(t + step)
		end := f32.Pt(float32(bx), float32(by))
		if i == n-1 {
			end = p1
		}
		out = append(out, seg{op: 'C', p: [3]f32.Point{
			f32.Pt(float32(ax+k*adx), float32(ay+k*ady)),
			f32.Pt(float32(bx-k*bdx), float32(by-k*bdy)),
			end,
		}})
		t += step
	}
	return out
}

type tokenizer struct {
	s string
	i int
}

func (t *tokenizer) skip() {
	for t.i < len(t.s) {
		switch t.s[t.i] {
		case ' ', ',', '\n', '\t', '\r':
			t.i++
		default:
			return
		}
	}
}

func (t *tokenizer) more() bool { t.skip(); return t.i < len(t.s) }

func (t *tokenizer) cmd() (byte, bool) {
	t.skip()
	if t.i >= len(t.s) {
		return 0, false
	}
	c := t.s[t.i]
	if (c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') && c != 'e' && c != 'E' {
		t.i++
		return c, true
	}
	return 0, false
}

func (t *tokenizer) num() float32 {
	t.skip()
	st := t.i
	if t.i < len(t.s) && (t.s[t.i] == '-' || t.s[t.i] == '+') {
		t.i++
	}
	dot := false
	for t.i < len(t.s) {
		c := t.s[t.i]
		if c >= '0' && c <= '9' {
			t.i++
		} else if c == '.' && !dot {
			dot = true
			t.i++
		} else if (c == 'e' || c == 'E') && t.i+1 < len(t.s) {
			t.i++
			if t.s[t.i] == '-' || t.s[t.i] == '+' {
				t.i++
			}
		} else {
			break
		}
	}
	v, err := strconv.ParseFloat(t.s[st:t.i], 32)
	if err != nil {
		if t.i == st { // garbage: consume one byte so parsing terminates
			t.i++
		}
		return 0
	}
	return float32(v)
}
