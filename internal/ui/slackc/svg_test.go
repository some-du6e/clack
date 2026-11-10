package slackc

import (
	"fmt"
	"image/color"
	"math"
	"runtime"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

func svgTestDoc(t *testing.T, js string) *svgDoc {
	t.Helper()
	nodes, err := ParseTree([]byte(js))
	if err != nil || len(nodes) != 1 {
		t.Fatalf("ParseTree(%s): %v (%d nodes)", js, err, len(nodes))
	}
	return compileSVG(nodes[0])
}

func svgNear(a, b f32.Point) bool {
	return math.Abs(float64(a.X-b.X)) < 1e-3 && math.Abs(float64(a.Y-b.Y)) < 1e-3
}

func svgSegsEqual(t *testing.T, d string, got, want []svgSeg) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%q: got %d segs %v, want %d %v", d, len(got), got, len(want), want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.op != w.op {
			t.Fatalf("%q seg %d: op %c, want %c (%v)", d, i, g.op, w.op, got)
		}
		n := 0
		switch w.op {
		case 'M', 'L':
			n = 1
		case 'C':
			n = 3
		}
		for k := 0; k < n; k++ {
			if !svgNear(g.p[k], w.p[k]) {
				t.Fatalf("%q seg %d pt %d: %v, want %v (%v)", d, i, k, g.p[k], w.p[k], got)
			}
		}
	}
}

func svgTP(x, y float32) f32.Point { return f32.Point{X: x, Y: y} }
func svgTM(x, y float32) svgSeg    { return svgSeg{op: 'M', p: [3]f32.Point{svgTP(x, y)}} }
func svgTL(x, y float32) svgSeg    { return svgSeg{op: 'L', p: [3]f32.Point{svgTP(x, y)}} }
func svgTC(a, b, c, d, e, f float32) svgSeg {
	return svgSeg{op: 'C', p: [3]f32.Point{svgTP(a, b), svgTP(c, d), svgTP(e, f)}}
}

var svgTZ = svgSeg{op: 'Z'}

func TestSVGParsePath(t *testing.T) {
	cases := []struct {
		d    string
		want []svgSeg
	}{
		{"M10 20 L30 40 H50 V60 Z", []svgSeg{svgTM(10, 20), svgTL(30, 40), svgTL(50, 40), svgTL(50, 60), svgTZ}},
		{"m10 20 l5 5 h5 v-5 z m1 1 l1 0", []svgSeg{svgTM(10, 20), svgTL(15, 25), svgTL(20, 25), svgTL(20, 20), svgTZ, svgTM(11, 21), svgTL(12, 21)}},
		// Implicit repeats; extra moveto pairs are linetos.
		{"M0 0 10 0 10 10", []svgSeg{svgTM(0, 0), svgTL(10, 0), svgTL(10, 10)}},
		{"m1 1 10 0 0 10", []svgSeg{svgTM(1, 1), svgTL(11, 1), svgTL(11, 11)}},
		{"M0 0 L1 1 2 2 h1 1 v1 1", []svgSeg{svgTM(0, 0), svgTL(1, 1), svgTL(2, 2), svgTL(3, 2), svgTL(4, 2), svgTL(4, 3), svgTL(4, 4)}},
		// Compact numbers.
		{"M1.5.5-1-2", []svgSeg{svgTM(1.5, .5), svgTL(-1, -2)}},
		{"M1e-3,2E2l+1e1-.5", []svgSeg{svgTM(.001, 200), svgTL(10.001, 199.5)}},
		{"M0,0L10,0,10,10z", []svgSeg{svgTM(0, 0), svgTL(10, 0), svgTL(10, 10), svgTZ}},
		// Cubics and smooth cubics.
		{"M0 0 C1 1 2 1 3 0 S5 -1 6 0", []svgSeg{svgTM(0, 0), svgTC(1, 1, 2, 1, 3, 0), svgTC(4, -1, 5, -1, 6, 0)}},
		{"M0 0 c1 1 2 1 3 0 s2 -1 3 0", []svgSeg{svgTM(0, 0), svgTC(1, 1, 2, 1, 3, 0), svgTC(4, -1, 5, -1, 6, 0)}},
		{"M0 0 S2 2 4 0", []svgSeg{svgTM(0, 0), svgTC(0, 0, 2, 2, 4, 0)}},
		{"M0 0 L1 1 S2 2 4 0", []svgSeg{svgTM(0, 0), svgTL(1, 1), svgTC(1, 1, 2, 2, 4, 0)}},
		// Quadratics, smooth quadratics (T after Q), relative forms.
		{"M0 0 Q2 2 4 0 T8 0", []svgSeg{svgTM(0, 0), svgTC(4./3, 4./3, 8./3, 4./3, 4, 0), svgTC(16./3, -4./3, 20./3, -4./3, 8, 0)}},
		{"M0 0 q2 2 4 0 t4 0", []svgSeg{svgTM(0, 0), svgTC(4./3, 4./3, 8./3, 4./3, 4, 0), svgTC(16./3, -4./3, 20./3, -4./3, 8, 0)}},
		{"M0 0 T3 0", []svgSeg{svgTM(0, 0), svgTC(0, 0, 1, 0, 3, 0)}},
		// A drawing command after Z starts a subpath at the start point.
		{"M5 5 L6 5 Z L5 6", []svgSeg{svgTM(5, 5), svgTL(6, 5), svgTZ, svgTM(5, 5), svgTL(5, 6)}},
		// Errors keep what came before.
		{"M0 0 L1 0 L2", []svgSeg{svgTM(0, 0), svgTL(1, 0)}},
		{"M0 0 L1 0 X 3 3", []svgSeg{svgTM(0, 0), svgTL(1, 0)}},
		{"L1 1", nil},
		{"", nil},
		{"M0 0 A0 5 0 0 1 10 0", []svgSeg{svgTM(0, 0), svgTL(10, 0)}}, // zero radius: a line
		{"M0 0 A5 5 0 0 1 0 0", []svgSeg{svgTM(0, 0)}},                // same endpoints: omitted
	}
	for _, c := range cases {
		svgSegsEqual(t, c.d, svgParsePath(c.d), c.want)
	}
}

func TestSVGParseArc(t *testing.T) {
	check := func(d string, end f32.Point, n int, center f32.Point, r float32) []svgSeg {
		t.Helper()
		segs := svgParsePath(d)
		if len(segs) != n+1 {
			t.Fatalf("%q: %d segs, want M + %d cubics: %v", d, len(segs), n, segs)
		}
		last := segs[len(segs)-1]
		if last.op != 'C' || last.p[2] != end {
			t.Fatalf("%q: last seg %v, want a cubic ending exactly at %v", d, last, end)
		}
		for _, s := range segs[1:] {
			e := s.p[2]
			if dist := math.Hypot(float64(e.X-center.X), float64(e.Y-center.Y)); math.Abs(dist-float64(r)) > 1e-3 {
				t.Fatalf("%q: point %v at %v from %v, want %v", d, e, dist, center, r)
			}
		}
		return segs
	}
	// Half circle, sweep=1: through (5,-5).
	segs := check("M0 0 A5 5 0 0 1 10 0", svgTP(10, 0), 2, svgTP(5, 0), 5)
	if !svgNear(segs[1].p[2], svgTP(5, -5)) {
		t.Fatalf("sweep=1 arc passes %v, want (5,-5)", segs[1].p[2])
	}
	segs = check("M0 0 A5 5 0 0 0 10 0", svgTP(10, 0), 2, svgTP(5, 0), 5)
	if !svgNear(segs[1].p[2], svgTP(5, 5)) {
		t.Fatalf("sweep=0 arc passes %v, want (5,5)", segs[1].p[2])
	}
	// Flags without separators, relative.
	check("M0 0a5 5 0 0110 0", svgTP(10, 0), 2, svgTP(5, 0), 5)
	check("M0,0a5,5,0,0,1,10,0", svgTP(10, 0), 2, svgTP(5, 0), 5)
	// Too-small radii scale up to fit (F.6.6).
	check("M0 0 A1 1 0 0 1 10 0", svgTP(10, 0), 2, svgTP(5, 0), 5)
	check("M0 0 A-1 -1 0 0 1 10 0", svgTP(10, 0), 2, svgTP(5, 0), 5)
	// Large arc: 270° around (0,5) from (0,0) to (5,5) needs 3 cubics.
	check("M0 0 A5 5 0 1 0 5 5", svgTP(5, 5), 3, svgTP(0, 5), 5)
	// Small arc, same endpoints and flags otherwise: 90° around (5,0)?
	check("M0 0 A5 5 0 0 0 5 5", svgTP(5, 5), 1, svgTP(5, 0), 5)
	// Rotated ellipse endpoint is exact.
	segs = svgParsePath("M3 4 A10 5 30 1 1 -7.25 12.5 L0 0")
	if segs[len(segs)-2].p[2] != svgTP(-7.25, 12.5) {
		t.Fatalf("rotated arc ends at %v", segs[len(segs)-2].p[2])
	}
	// Arc then implicit repeat.
	segs = svgParsePath("M0 0 a5 5 0 0 1 10 0 5 5 0 0 1 10 0")
	if n := len(segs); n != 5 || segs[4].p[2] != svgTP(20, 0) {
		t.Fatalf("repeated arc: %v", segs)
	}
}

func TestSVGParseTransform(t *testing.T) {
	cases := []struct {
		s       string
		in, out f32.Point
	}{
		{"translate(10,20)", svgTP(1, 1), svgTP(11, 21)},
		{"translate(10)", svgTP(1, 1), svgTP(11, 1)},
		{"scale(2)", svgTP(1, 1), svgTP(2, 2)},
		{"scale(2 3)", svgTP(1, 1), svgTP(2, 3)},
		{"rotate(90)", svgTP(1, 0), svgTP(0, 1)},
		{"rotate(90 10 10)", svgTP(20, 10), svgTP(10, 20)},
		{"rotate(-90,10,10)", svgTP(20, 10), svgTP(10, 0)},
		{"matrix(1 2 3 4 5 6)", svgTP(1, 1), svgTP(9, 12)},
		{"matrix(1,2,3,4,5,6)", svgTP(1, 1), svgTP(9, 12)},
		{"skewX(45)", svgTP(0, 1), svgTP(1, 1)},
		{"skewY(45)", svgTP(1, 0), svgTP(1, 1)},
		// Lists apply right to left.
		{"translate(10) scale(2)", svgTP(1, 1), svgTP(12, 2)},
		{"scale(2),translate(10)", svgTP(1, 1), svgTP(22, 2)},
		{"  translate( 1 , 2 )  rotate(180)  ", svgTP(1, 0), svgTP(0, 2)},
		{"", svgTP(3, 4), svgTP(3, 4)},
	}
	for _, c := range cases {
		m, ok := svgParseTransform(c.s)
		if !ok {
			t.Fatalf("%q: not ok", c.s)
		}
		if got := m.apply(c.in); !svgNear(got, c.out) {
			t.Fatalf("%q: %v -> %v, want %v", c.s, c.in, got, c.out)
		}
		// The Gio affine agrees.
		if got := m.aff().Transform(c.in); !svgNear(got, c.out) {
			t.Fatalf("%q: Affine2D %v -> %v, want %v", c.s, c.in, got, c.out)
		}
	}
	for _, bad := range []string{"translate(10", "foo(1)", "scale()", "rotate(1 2)", "matrix(1 2 3)", "translate(1) x", "translate(1 2 3)"} {
		if _, ok := svgParseTransform(bad); ok {
			t.Fatalf("%q parsed", bad)
		}
	}
}

func TestSVGParseColor(t *testing.T) {
	rgba := func(r, g, b, a uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: a} }
	cases := []struct {
		s    string
		want color.NRGBA
	}{
		{"#f00", rgba(255, 0, 0, 255)},
		{"#F008", rgba(255, 0, 0, 0x88)},
		{"#ff8000", rgba(255, 128, 0, 255)},
		{"#ff800080", rgba(255, 128, 0, 128)},
		{"rgb(255, 128, 0)", rgba(255, 128, 0, 255)},
		{"rgb(100%, 50%, 0%)", rgba(255, 128, 0, 255)},
		{"rgba(255,0,0,.5)", rgba(255, 0, 0, 128)},
		{"rgba(255,0,0,50%)", rgba(255, 0, 0, 128)},
		{"rgb(255 0 0 / 50%)", rgba(255, 0, 0, 128)},
		{"rgb(29 28 29)", rgba(29, 28, 29, 255)},
		{"RGB(300, -5, 0)", rgba(255, 0, 0, 255)},
		{"hsl(120, 100%, 50%)", rgba(0, 255, 0, 255)},
		{"hsl(240deg 100% 50%)", rgba(0, 0, 255, 255)},
		{"hsla(0 100% 50% / 0.25)", rgba(255, 0, 0, 64)},
		{"hsl(0, 0%, 100%)", rgba(255, 255, 255, 255)},
		{"RebeccaPurple", rgba(0x66, 0x33, 0x99, 255)},
		{"black", rgba(0, 0, 0, 255)},
		{" white ", rgba(255, 255, 255, 255)},
		{"transparent", rgba(0, 0, 0, 0)},
	}
	for _, c := range cases {
		got, ok := svgParseColor(c.s)
		if !ok || got != c.want {
			t.Fatalf("%q: %v %v, want %v", c.s, got, ok, c.want)
		}
	}
	for _, bad := range []string{"", "#ggg", "#12345", "rgb(1,2)", "rgb(1 2 3 4)", "notacolor", "hsl(1,2,3", "currentColor"} {
		if c, ok := svgParseColor(bad); ok {
			t.Fatalf("%q parsed as %v", bad, c)
		}
	}
	if n := len(svgNamedColors); n != 148 {
		t.Fatalf("%d named colors, want 148", n)
	}
}

func TestSVGRectRadii(t *testing.T) {
	cases := []struct {
		props  string
		rx, ry float32
	}{
		{`"width":10,"height":4`, 0, 0},
		{`"width":10,"height":4,"rx":1`, 1, 1},
		{`"width":10,"height":4,"ry":1.5`, 1.5, 1.5},
		{`"width":10,"height":4,"rx":3`, 3, 2},         // ry = rx, then clamped to h/2
		{`"width":10,"height":4,"rx":20,"ry":1`, 5, 1}, // rx clamped to w/2
		{`"width":10,"height":4,"rx":-1,"ry":1`, 1, 1}, // negative is auto
		{`"width":10,"height":4,"rx":"auto","ry":1`, 1, 1},
		{`"width":10,"height":4,"rx":"50%"`, 5, 2}, // % of the viewport width (10 here)
	}
	for _, c := range cases {
		d := svgTestDoc(t, `{"t":"svg","p":{"viewBox":"0 0 10 10"},"c":[{"t":"rect","p":{`+c.props+`}}]}`)
		e := d.root.kids[0]
		rx, ry := svgRectRadii(e, 10, 10, 10, 4)
		if rx != c.rx || ry != c.ry {
			t.Fatalf("%s: rx,ry = %v,%v, want %v,%v", c.props, rx, ry, c.rx, c.ry)
		}
	}
	// Geometry: a sharp rect is a closed quad, a rounded one has 4 corner cubics.
	d := svgTestDoc(t, `{"t":"svg","c":[{"t":"rect","p":{"x":1,"y":2,"width":10,"height":4}},{"t":"rect","p":{"width":10,"height":4,"rx":1}},{"t":"rect","p":{"width":0,"height":4}}]}`)
	var c svgCtx
	st := svgState{vw: 100, vh: 100}
	svgSegsEqual(t, "rect", c.shapeSegs(d.root.kids[0], &st), []svgSeg{svgTM(1, 2), svgTL(11, 2), svgTL(11, 6), svgTL(1, 6), svgTZ})
	if segs := c.shapeSegs(d.root.kids[1], &st); len(segs) != 10 || segs[0].p[0] != svgTP(1, 0) {
		t.Fatalf("rounded rect: %v", segs)
	}
	if segs := c.shapeSegs(d.root.kids[2], &st); segs != nil {
		t.Fatalf("zero-width rect: %v", segs)
	}
}

func TestSVGShapes(t *testing.T) {
	d := svgTestDoc(t, `{"t":"svg","c":[
		{"t":"circle","p":{"cx":5,"cy":5,"r":5}},
		{"t":"ellipse","p":{"cx":5,"cy":5,"rx":4}},
		{"t":"line","p":{"x1":1,"y1":2,"x2":3,"y2":4}},
		{"t":"polyline","p":{"points":"0,0 10,0 10,10 5"}},
		{"t":"polygon","p":{"points":"0 0 10 0 10 10"}},
		{"t":"circle","p":{"cx":5,"cy":5,"r":"50%"}}
	]}`)
	var c svgCtx
	st := svgState{vw: 20, vh: 20}
	k := d.root.kids
	if segs := c.shapeSegs(k[0], &st); len(segs) != 6 || segs[0].p[0] != svgTP(10, 5) || segs[2].p[2] != svgTP(0, 5) {
		t.Fatalf("circle: %v", segs)
	}
	if segs := c.shapeSegs(k[1], &st); len(segs) != 6 || segs[1].p[2] != svgTP(5, 9) {
		t.Fatalf("ellipse with ry auto: %v", segs)
	}
	svgSegsEqual(t, "line", c.shapeSegs(k[2], &st), []svgSeg{svgTM(1, 2), svgTL(3, 4)})
	svgSegsEqual(t, "polyline", c.shapeSegs(k[3], &st), []svgSeg{svgTM(0, 0), svgTL(10, 0), svgTL(10, 10)})
	svgSegsEqual(t, "polygon", c.shapeSegs(k[4], &st), []svgSeg{svgTM(0, 0), svgTL(10, 0), svgTL(10, 10), svgTZ})
	if segs := c.shapeSegs(k[5], &st); len(segs) != 6 || segs[0].p[0] != svgTP(15, 5) {
		t.Fatalf("circle r=50%% of 20x20: %v", segs)
	}
}

type svgDraw struct {
	n      *Node
	stroke bool
	c      color.NRGBA
	w      float32
}

func svgTestPaint(d *svgDoc, root svgPaint, sty func(*Node) *svgCSS) []svgDraw {
	var out []svgDraw
	ops := new(op.Ops)
	d.paintWith(ops, f32.Point{X: 24, Y: 24}, root, sty, nil, func(n *Node, stroke bool, c color.NRGBA, w float32) {
		out = append(out, svgDraw{n, stroke, c, w})
	})
	return out
}

var svgBlackRoot = svgPaint{Color: color.NRGBA{A: 255}, Fill: color.NRGBA{A: 255}, StrokeNone: true, StrokeWidth: 1, Opacity: 1}

func TestSVGUseSymbol(t *testing.T) {
	d := svgTestDoc(t, `{"t":"svg","p":{"viewBox":"0 0 24 24"},"c":[
		{"t":"defs","c":[
			{"t":"symbol","p":{"id":"s","viewBox":"0 0 1 1"},"c":[{"t":"rect","p":{"id":"r","width":1,"height":1,"fill":"red"}}]},
			{"t":"path","p":{"id":"p","d":"M0 0h1v1z"}}
		]},
		{"t":"use","p":{"href":"#s","width":5,"height":5}},
		{"t":"use","p":{"xlinkHref":"#p","x":3,"fill":"blue"}},
		{"t":"use","p":{"href":"#missing"}},
		{"t":"g","p":{"id":"a"},"c":[{"t":"circle","p":{"id":"c","r":1}},{"t":"use","p":{"id":"self","href":"#a"}}]},
		{"t":"use","p":{"id":"u1","href":"#u2"}},
		{"t":"use","p":{"id":"u2","href":"#u1"}},
		{"t":"symbol","p":{"id":"lonely"},"c":[{"t":"rect","p":{"width":1,"height":1}}]}
	]}`)
	if d.ids["self"].ref != nil {
		t.Fatal("self-referencing use kept its target")
	}
	if d.ids["u1"].ref != nil && d.ids["u2"].ref != nil && d.ids["u2"].ref.ref != nil {
		t.Fatal("use cycle u1<->u2 survived")
	}
	draws := svgTestPaint(d, svgBlackRoot, nil)
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	var got []string
	for _, dr := range draws {
		id := dr.n.ID
		switch {
		case id == "r" && dr.c == red:
		case id == "p" && dr.c == blue: // use's fill inherits into the target
		case id == "c" && dr.c == (color.NRGBA{A: 255}):
		default:
			t.Fatalf("unexpected draw %+v", dr)
		}
		got = append(got, id)
	}
	if len(got) != 3 {
		t.Fatalf("draws %v, want r, p, c once each (defs and lone symbols not drawn)", got)
	}
}

func TestSVGIntrinsic(t *testing.T) {
	d := svgTestDoc(t, `{"t":"svg","p":{"width":"24","height":"50%","viewBox":"0 0 24 12"}}`)
	w, h, wok, hok, vb, hasVB := d.intrinsic()
	if w != 24 || !wok || hok || h != 0 || !hasVB || vb != [4]float32{0, 0, 24, 12} {
		t.Fatalf("got %v %v %v %v %v %v", w, h, wok, hok, vb, hasVB)
	}
	d = svgTestDoc(t, `{"t":"svg","p":{"width":"2em","height":16,"viewBox":"0,0,-1,5"}}`)
	w, h, wok, hok, _, hasVB = d.intrinsic()
	if w != 32 || h != 16 || !wok || !hok || hasVB {
		t.Fatalf("got %v %v %v %v %v", w, h, wok, hok, hasVB)
	}
	d = svgTestDoc(t, `{"t":"svg","p":{"width":"auto","viewBox":"0 0 24"}}`)
	if _, _, wok, hok, _, hasVB = d.intrinsic(); wok || hok || hasVB {
		t.Fatal("auto width or short viewBox accepted")
	}
	var nilDoc *svgDoc
	nilDoc.intrinsic()
	nilDoc.paint(new(op.Ops), f32.Point{X: 1, Y: 1}, svgBlackRoot, nil, nil)
	compileSVG(nil).paint(new(op.Ops), f32.Point{X: 1, Y: 1}, svgBlackRoot, nil, nil)
}

func TestSVGViewBoxMat(t *testing.T) {
	vb := [4]float32{0, 0, 10, 20}
	cases := []struct {
		par  svgPAR
		in   f32.Point
		want f32.Point
	}{
		{svgPARDefault, svgTP(0, 0), svgTP(25, 0)},                        // meet: scale 5, centered in 100 wide
		{svgPARDefault, svgTP(10, 20), svgTP(75, 100)},                    //
		{svgPAR{ax: 0, ay: 0}, svgTP(0, 0), svgTP(0, 0)},                  // xMinYMin
		{svgPAR{ax: 1, ay: 1}, svgTP(0, 0), svgTP(50, 0)},                 // xMaxYMax
		{svgPAR{none: true}, svgTP(10, 20), svgTP(100, 100)},              // stretch
		{svgPAR{ax: .5, ay: .5, slice: true}, svgTP(0, 0), svgTP(0, -50)}, // slice: scale 10
	}
	for _, c := range cases {
		m := svgViewBoxMat(vb, c.par, 0, 0, 100, 100)
		if got := m.apply(c.in); !svgNear(got, c.want) {
			t.Fatalf("%+v: %v -> %v, want %v", c.par, c.in, got, c.want)
		}
	}
	p := svgParsePAR(&Node{P: Props{"preserveAspectRatio": "xMaxYMin slice"}})
	if p != (svgPAR{ax: 1, ay: 0, slice: true}) {
		t.Fatalf("parsed %+v", p)
	}
	if p := svgParsePAR(&Node{P: Props{"preserveAspectRatio": "bogus"}}); p != svgPARDefault {
		t.Fatalf("bogus parsed %+v", p)
	}
}

func TestSVGPaintInheritance(t *testing.T) {
	d := svgTestDoc(t, `{"t":"svg","p":{"viewBox":"0 0 24 24","fill":"none","stroke":"currentColor","strokeWidth":2},"c":[
		{"t":"g","p":{"fill":"red","opacity":0.5},"c":[
			{"t":"path","p":{"id":"inh","d":"M0 0h4v4z","fillOpacity":0.5}},
			{"t":"path","p":{"id":"styled","d":"M0 0h4v4z","fill":"green"}},
			{"t":"path","p":{"id":"rule","d":"M0 0h4v4z","fill":"green"}}
		]},
		{"t":"path","p":{"id":"cur","d":"M0 0L4 4","stroke-width":"3","stroke-linecap":"round","strokeLinejoin":"round"}},
		{"t":"path","p":{"id":"hidden","d":"M0 0h4v4z","fill":"red","display":"none"}},
		{"t":"path","p":{"id":"ruleHidden","d":"M0 0h4v4z","fill":"red"}},
		{"t":"g","p":{"color":"#00f"},"c":[{"t":"path","p":{"id":"blue","d":"M0 0h4v4z","fill":"currentColor","stroke":"none"}}]}
	]}`)
	// Inline style beats the attribute.
	d.ids["styled"].n.Style = css.Decls{{Prop: "fill", Value: "#0000ff"}}
	d.ids["styled"].pres = svgPresOf(d.ids["styled"].n)
	sty := func(n *Node) *svgCSS {
		switch n.ID {
		case "rule":
			return &svgCSS{FillSet: true, Fill: color.NRGBA{R: 1, G: 2, B: 3, A: 255}}
		case "ruleHidden":
			return &svgCSS{Hidden: true}
		}
		return nil
	}
	root := svgBlackRoot
	root.Color = color.NRGBA{R: 9, G: 9, B: 9, A: 255}
	draws := svgTestPaint(d, root, sty)
	type key struct {
		id     string
		stroke bool
	}
	got := map[key]svgDraw{}
	for _, dr := range draws {
		got[key{dr.n.ID, dr.stroke}] = dr
	}
	want := map[key]struct {
		c color.NRGBA
		w float32
	}{
		{"inh", false}:    {color.NRGBA{R: 255, A: 64}, 0},
		{"inh", true}:     {color.NRGBA{R: 9, G: 9, B: 9, A: 128}, 2},
		{"styled", false}: {color.NRGBA{B: 255, A: 128}, 0},
		{"styled", true}:  {color.NRGBA{R: 9, G: 9, B: 9, A: 128}, 2},
		{"rule", false}:   {color.NRGBA{R: 1, G: 2, B: 3, A: 128}, 0},
		{"rule", true}:    {color.NRGBA{R: 9, G: 9, B: 9, A: 128}, 2},
		{"cur", true}:     {color.NRGBA{R: 9, G: 9, B: 9, A: 255}, 3},
		{"blue", false}:   {color.NRGBA{B: 255, A: 255}, 0},
	}
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Fatalf("%v not drawn; draws %+v", k, draws)
		}
		if g.c != w.c || g.w != w.w {
			t.Fatalf("%v: color %v width %v, want %v %v", k, g.c, g.w, w.c, w.w)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("draws %+v, want exactly %v", got, want)
	}
}

func TestSVGEvenOdd(t *testing.T) {
	// Two same-direction squares: evenodd needs the inner one reversed.
	segs := svgParsePath("M0 0h10v10h-10z M2 2h6v6h-6z")
	eo := svgEvenOddSegs(segs)
	if eo == nil {
		t.Fatal("no evenodd variant")
	}
	var areas []float32
	for i := 0; i < len(eo); {
		j := i + 1
		var poly []f32.Point
		poly = append(poly, eo[i].p[0])
		for j < len(eo) && eo[j].op != 'M' {
			if eo[j].op == 'L' {
				poly = append(poly, eo[j].p[0])
			}
			j++
		}
		areas = append(areas, svgPolyArea(poly))
		i = j
	}
	if len(areas) != 2 || areas[0]*areas[1] >= 0 {
		t.Fatalf("areas %v: inner should wind opposite to outer (%v)", areas, eo)
	}
	// Already opposite: no variant needed.
	if eo := svgEvenOddSegs(svgParsePath("M0 0h10v10h-10z M2 2v6h6v-6z")); eo != nil {
		t.Fatalf("variant for an already-alternating path: %v", eo)
	}
	// Three nested: the middle one flips, the innermost keeps the outer's sign.
	eo = svgEvenOddSegs(svgParsePath("M0 0h10v10h-10z M1 1h8v8h-8z M2 2h6v6h-6z"))
	if eo == nil || eo[5].op != 'M' || eo[5].p[0] != svgTP(1, 9) {
		t.Fatalf("nested: %v", eo)
	}
	// Reversal keeps curves exact.
	r := svgReverseSub(nil, []svgSeg{svgTM(0, 0), svgTC(1, 0, 2, 1, 2, 2), svgTL(0, 2), svgTZ})
	svgSegsEqual(t, "reverse", r, []svgSeg{svgTM(0, 2), svgTL(2, 2), svgTC(2, 1, 1, 0, 0, 0), svgTZ})
}

const svgMaterialCheck = `{"t":"svg","p":{"viewBox":"0 0 24 24"},"c":[{"t":"path","p":{"d":"M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z"}}]}`

func TestSVGPaintSmoke(t *testing.T) {
	d := svgTestDoc(t, svgMaterialCheck)
	draws := svgTestPaint(d, svgBlackRoot, nil)
	if len(draws) != 1 || draws[0].stroke || draws[0].c != (color.NRGBA{A: 255}) {
		t.Fatalf("material icon draws %+v", draws)
	}
	// The circle (M, 4 cubics, Z) and the check (M, 6 lines, Z).
	if len(d.root.kids[0].segs) < 10 {
		t.Fatalf("segs %v", d.root.kids[0].segs)
	}

	// Strokes through both stroke paths, every cap/join, nested svg,
	// transforms, gradients, mapColor, all into one Ops.
	docs := []string{
		`{"t":"svg","p":{"viewBox":"0 0 24 24","fill":"none","stroke":"currentColor","strokeWidth":2,"strokeLinecap":"round","strokeLinejoin":"round"},"c":[{"t":"path","p":{"d":"M3 12h18M12 3v18"}},{"t":"circle","p":{"cx":12,"cy":12,"r":9}}]}`,
		`{"t":"svg","p":{"viewBox":"0 0 24 24","fill":"none","stroke":"#123"},"c":[
			{"t":"path","p":{"d":"M2 2L12 20L22 2","strokeLinejoin":"miter","strokeWidth":3}},
			{"t":"path","p":{"d":"M2 2L12 20L22 2","strokeLinejoin":"miter","strokeMiterlimit":1,"strokeWidth":3}},
			{"t":"path","p":{"d":"M2 2L12 20L22 2z","strokeLinejoin":"bevel","strokeLinecap":"square"}},
			{"t":"path","p":{"d":"M2 2L12 20L2 2","strokeLinejoin":"round","strokeLinecap":"butt"}},
			{"t":"path","p":{"d":"M5 5h0M8 8z","strokeLinecap":"round","strokeWidth":4}},
			{"t":"path","p":{"d":"M5 5h0","strokeLinecap":"square","strokeWidth":4}},
			{"t":"path","p":{"d":"M2 12C2 2 22 2 22 12S2 22 2 12","strokeLinecap":"square"}},
			{"t":"rect","p":{"x":4,"y":4,"width":16,"height":16,"rx":4,"transform":"rotate(30 12 12) skewX(10)"}},
			{"t":"line","p":{"x1":0,"y1":0,"x2":24,"y2":24,"strokeWidth":"10%"}}
		]}`,
		`{"t":"svg","p":{"width":24,"height":24},"c":[
			{"t":"svg","p":{"x":2,"y":2,"width":10,"height":"50%","viewBox":"0 0 1 1","preserveAspectRatio":"none"},"c":[{"t":"rect","p":{"width":1,"height":1,"fill":"url(#g)"}}]},
			{"t":"linearGradient","p":{"id":"g"},"c":[{"t":"stop","p":{"offset":"0","stopColor":"red"}},{"t":"stop","p":{"offset":"100%","stopColor":"blue","stopOpacity":0.5}}]},
			{"t":"radialGradient","p":{"id":"g2","href":"#g"}},
			{"t":"circle","p":{"cx":12,"cy":12,"r":4,"fill":"url(#g2)","fillRule":"evenodd"}},
			{"t":"circle","p":{"cx":12,"cy":12,"r":4,"fill":"url(#nope) green"}},
			{"t":"circle","p":{"cx":12,"cy":12,"r":4,"fill":"url(#nope)"}},
			{"t":"path","p":{"d":"M0 0h10v10h-10z M2 2h6v6h-6z","fillRule":"evenodd"}},
			{"t":"text","c":["ignored"]},
			{"t":"switch","c":[{"t":"a","c":[{"t":"ellipse","p":{"cx":3,"cy":3,"rx":2,"ry":1}}]}]}
		]}`,
		`{"t":"svg","p":{"viewBox":"garbage","transform":"scale(0.5)"},"c":[{"t":"path","p":{"d":"M 0 0 L"}},{"t":"path","p":{"d":"nonsense"}},{"t":"rect","p":{"width":"x"}},{"t":"use"},{"t":"g","p":{"transform":"bogus(1)"}}]}`,
		`{"t":"div"}`,
	}
	ops := new(op.Ops)
	gray := func(c color.NRGBA) color.NRGBA {
		y := uint8((int(c.R) + int(c.G) + int(c.B)) / 3)
		return color.NRGBA{R: y, G: y, B: y, A: c.A}
	}
	for i, js := range docs {
		d := svgTestDoc(t, js)
		var n int
		d.paintWith(ops, f32.Point{X: 24, Y: 24}, svgBlackRoot, nil, gray, func(*Node, bool, color.NRGBA, float32) { n++ })
		d.paint(ops, f32.Point{X: 7.5, Y: 31}, svgBlackRoot, nil, nil)
		switch i {
		case 0:
			if n != 2 {
				t.Fatalf("doc 0: %d draws, want 2 strokes", n)
			}
		case 1:
			if n != 9 {
				t.Fatalf("doc 1: %d draws, want 9 strokes", n)
			}
		case 2:
			// gradient rect, gradient circle, green fallback, evenodd path, ellipse
			if n != 5 {
				t.Fatalf("doc 2: %d draws, want 5", n)
			}
		}
	}
}

func TestSVGStrokerCapsJoins(t *testing.T) {
	// Every cap/join combination emits polygons for open, closed and curved
	// subpaths.
	ops := new(op.Ops)
	for _, cp := range []uint8{svgCapButt, svgCapRound, svgCapSquare} {
		for _, join := range []uint8{svgJoinMiter, svgJoinRound, svgJoinBevel} {
			var s svgStroker
			s.reset(ops, 1, cp, join, 4, 0.05)
			for _, seg := range svgParsePath("M0 0L10 0L10 10L0 10M20 20C30 20 30 30 20 30L20 20z") {
				switch seg.op {
				case 'M':
					s.moveTo(seg.p[0])
				case 'L':
					s.lineTo(seg.p[0], false)
				case 'C':
					s.cubeTo(seg.p[0], seg.p[1], seg.p[2])
				case 'Z':
					s.close()
				}
			}
			s.finish()
			s.p.End()
			if s.n < 8 {
				t.Fatalf("cap %d join %d: only %d polygons", cp, join, s.n)
			}
		}
	}
}

func TestSVGPaintAllocs(t *testing.T) {
	d := svgTestDoc(t, `{"t":"svg","p":{"viewBox":"0 0 24 24"},"c":[
		{"t":"path","p":{"d":"M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2z","fillRule":"evenodd"}},
		{"t":"rect","p":{"x":2,"y":2,"width":4,"height":4,"rx":1,"stroke":"red"}},
		{"t":"g","p":{"transform":"rotate(45 12 12)"},"c":[{"t":"circle","p":{"cx":12,"cy":12,"r":3,"stroke":"blue","strokeLinecap":"round","strokeLinejoin":"round"}}]}
	]}`)
	ops := new(op.Ops)
	run := func() {
		ops.Reset()
		d.paint(ops, f32.Point{X: 24, Y: 24}, svgBlackRoot, nil, nil)
	}
	run()
	if n := testing.AllocsPerRun(50, run); n > 0 {
		old := runtime.MemProfileRate
		runtime.MemProfileRate = 1
		for i := 0; i < 20; i++ {
			run()
		}
		runtime.GC()
		runtime.GC()
		runtime.MemProfileRate = old
		recs := make([]runtime.MemProfileRecord, 4096)
		k, _ := runtime.MemProfile(recs, true)
		for _, r := range recs[:min(k, len(recs))] {
			var b strings.Builder
			fr := runtime.CallersFrames(r.Stack())
			hit := false
			for {
				f, more := fr.Next()
				if strings.Contains(f.Function, "svg") {
					hit = true
				}
				fmt.Fprintf(&b, "\n\t%s %s:%d", f.Function, f.File, f.Line)
				if !more {
					break
				}
			}
			if hit && r.AllocObjects >= 20 {
				t.Logf("%d allocs, %d bytes:%s", r.AllocObjects, r.AllocBytes, b.String())
			}
		}
		t.Fatalf("paint allocates %v times per frame", n)
	}
}
