package slackc

// Inline <svg> rendering for host trees.
//
// compileSVG turns an <svg> subtree into an svgDoc once (trees are
// immutable); paint replays it into Gio ops each frame without allocating
// beyond what clip.Path itself writes into the op list.
//
// Supported: svg (outer and nested, with x/y/width/height/viewBox and
// preserveAspectRatio), g, a, switch (both drawn like g), defs, symbol (via
// use), use (local #id refs with x/y and width/height for symbol/svg
// targets; cyclic references draw nothing), path (the full path grammar),
// rect (rx/ry rules), circle, ellipse, line, polyline, polygon, transform
// lists, presentation attributes and inline style with SVG inheritance,
// currentColor, opacity, fill-opacity, stroke-opacity, fill-rule,
// stroke-linecap, stroke-linejoin and stroke-miterlimit. Unknown elements
// (text, image, clipPath, mask, pattern, filter, marker, foreignObject…)
// are skipped with their subtrees.
//
// Approximations, all forced by Gio v0.10.3:
//   - clip.Outline only rasterizes the nonzero rule. evenodd is emulated at
//     compile time: each subpath's nesting depth (how many other subpaths
//     contain it) decides its orientation, even depths one way and odd depths
//     the other, so nonzero filling then matches evenodd. That is exact for
//     nested, non-intersecting contours (rings, cut-outs, the usual icon
//     case) and wrong only for self-intersecting or overlapping contours.
//   - clip.Stroke draws round caps and round joins only (internal/stroke),
//     with its width in the path's own (user) units: the path is stroked
//     first and transformed after, so stroke widths scale with viewBox and
//     transforms like SVG, non-uniform scales included. When the requested
//     cap and join are both round we use clip.Stroke. Otherwise svgStroker
//     builds the outline itself (butt/square/round caps, miter/bevel/round
//     joins with miterlimit, dots for zero-length subpaths) from flattened
//     segments: one consistently oriented polygon per segment, join and cap,
//     unioned by the nonzero fill. Curves are flattened to ~0.25 device px.
//   - opacity is multiplied into the fill and stroke alpha of the element
//     and its descendants (no group compositing), so overlapping children of
//     a translucent group show through each other.
//   - Gradients (linearGradient/radialGradient via fill="url(#id)") paint the
//     average color of their stops; patterns and other paint servers fall
//     back to the url()'s fallback color, else none.
//   - visibility:hidden hides the subtree (no visible-child override).
//   - Percent lengths resolve against the nearest viewport; em/ex use a
//     16px font.

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// svgPaint is the inherited paint state at the <svg> root, from the svg
// element's computed CSS.
type svgPaint struct {
	Color       color.NRGBA // CSS `color`, the value of currentColor
	Fill        color.NRGBA
	FillNone    bool
	Stroke      color.NRGBA
	StrokeNone  bool
	StrokeWidth float32 // user units
	Opacity     float32 // multiplied into everything, 1 = opaque
}

// svgCSS is what CSS rules set directly on one descendant node (fill/stroke
// set by a matching rule beat presentation attributes; unset fields fall
// back to attributes, then inheritance).
type svgCSS struct {
	Color                                color.NRGBA
	ColorSet                             bool
	Fill                                 color.NRGBA
	FillSet, FillNone, FillCurrent       bool
	Stroke                               color.NRGBA
	StrokeSet, StrokeNone, StrokeCurrent bool
	StrokeWidth                          float32
	StrokeWidthSet                       bool
	Opacity                              float32
	OpacitySet                           bool
	Hidden                               bool // display:none or visibility:hidden
}

// svgDoc is an <svg> subtree compiled once per tree.
type svgDoc struct {
	root   *svgEl
	ids    map[string]*svgEl
	rootOp float32 // the root's opacity attribute (CSS opacity is in svgPaint)
}

// Element kinds.
const (
	svgKGroup uint8 = iota // g, a, switch
	svgKSVG
	svgKSymbol
	svgKDefs
	svgKPath // path, polyline, polygon
	svgKRect
	svgKCircle
	svgKEllipse
	svgKLine
	svgKUse
	svgKGrad
)

// Length slots in svgEl.len.
//
//	svg, use: x y width height
//	rect:     x y width height rx ry
//	circle:   cx cy r
//	ellipse:  cx cy _ _ rx ry
//	line:     x1 y1 x2 y2
const (
	svgLX = iota
	svgLY
	svgLW
	svgLH
	svgLRX
	svgLRY
)

type svgEl struct {
	n     *Node
	kind  uint8
	kids  []*svgEl
	xf    svgMat
	hasXf bool
	pres  svgPres

	segs   []svgSeg // path data, user units
	eoSegs []svgSeg // segs reoriented for evenodd filling; nil when identical

	len   [6]svgLen
	lenOK uint8 // bit i: len[i] was given

	vb    [4]float32
	hasVB bool
	par   svgPAR

	ref    *svgEl // use target
	href   string
	stops  []svgStop // gradients
	closed bool      // polygon
}

// svgLen is a length in user units or a percentage of the viewport.
type svgLen struct {
	v   float32
	pct bool
}

func (l svgLen) px(ref float32) float32 {
	if l.pct {
		return l.v / 100 * ref
	}
	return l.v
}

// svgPAR is a parsed preserveAspectRatio.
type svgPAR struct {
	ax, ay float32 // 0 min, .5 mid, 1 max
	none   bool
	slice  bool
}

var svgPARDefault = svgPAR{ax: .5, ay: .5}

type svgStop struct {
	off float32
	c   svgPaintVal // svgPColor or svgPCurrent
	op  float32
}

// svgSeg is one absolute segment: 'M' p0, 'L' p0, 'C' p0 p1 p2 (controls,
// end), 'Z'. A subpath that continues after Z starts with an explicit 'M'.
type svgSeg struct {
	op byte
	p  [3]f32.Point
}

// Paint kinds.
const (
	svgPNone uint8 = iota + 1
	svgPColor
	svgPCurrent
	svgPURL
)

type svgPaintVal struct {
	kind uint8
	c    color.NRGBA // the color, or the url() fallback color
	fb   uint8       // url(): fallback kind, 0 = none
	ref  *svgEl      // url(): resolved gradient
	id   string      // url(): referenced id
}

// Presentation property bits.
const (
	svgSetColor uint16 = 1 << iota
	svgSetFill
	svgSetStroke
	svgSetSW
	svgSetOpacity
	svgSetFO
	svgSetSO
	svgSetRule
	svgSetCap
	svgSetJoin
	svgSetMiter
	svgSetVis
)

const (
	svgRuleNonzero uint8 = iota
	svgRuleEvenOdd
)

const (
	svgCapButt uint8 = iota
	svgCapRound
	svgCapSquare
)

const (
	svgJoinMiter uint8 = iota
	svgJoinRound
	svgJoinBevel
)

// svgPres is what an element's presentation attributes and inline style set.
type svgPres struct {
	set          uint16
	color        color.NRGBA
	fill, stroke svgPaintVal
	sw           svgLen
	opacity      float32
	fo, so       float32
	miter        float32
	rule         uint8
	cap, join    uint8
	visible      bool
	hidden       bool // display:none
}

// svgState is the inherited paint state while drawing.
type svgState struct {
	color        color.NRGBA
	fill, stroke svgPaintVal
	sw           float32
	fo, so       float32
	op           float32 // accumulated opacity
	miter        float32
	rule         uint8
	cap, join    uint8
	vis          bool
	vw, vh       float32 // nearest viewport size, user units
	scale        float32 // user unit → device px (sqrt|det|)
}

func (st *svgState) apply(p *svgPres) {
	s := p.set
	if s == 0 {
		return
	}
	if s&svgSetColor != 0 {
		st.color = p.color
	}
	if s&svgSetFill != 0 {
		st.fill = p.fill
	}
	if s&svgSetStroke != 0 {
		st.stroke = p.stroke
	}
	if s&svgSetSW != 0 {
		if p.sw.pct {
			st.sw = p.sw.px(float32(math.Sqrt(float64(st.vw*st.vw+st.vh*st.vh) / 2)))
		} else {
			st.sw = p.sw.v
		}
	}
	if s&svgSetFO != 0 {
		st.fo = p.fo
	}
	if s&svgSetSO != 0 {
		st.so = p.so
	}
	if s&svgSetMiter != 0 {
		st.miter = p.miter
	}
	if s&svgSetRule != 0 {
		st.rule = p.rule
	}
	if s&svgSetCap != 0 {
		st.cap = p.cap
	}
	if s&svgSetJoin != 0 {
		st.join = p.join
	}
	if s&svgSetVis != 0 {
		st.vis = p.visible
	}
}

func (st *svgState) applyCSS(c *svgCSS) {
	if c.ColorSet {
		st.color = c.Color
	}
	if c.FillSet {
		switch {
		case c.FillNone:
			st.fill = svgPaintVal{kind: svgPNone}
		case c.FillCurrent:
			st.fill = svgPaintVal{kind: svgPCurrent}
		default:
			st.fill = svgPaintVal{kind: svgPColor, c: c.Fill}
		}
	}
	if c.StrokeSet {
		switch {
		case c.StrokeNone:
			st.stroke = svgPaintVal{kind: svgPNone}
		case c.StrokeCurrent:
			st.stroke = svgPaintVal{kind: svgPCurrent}
		default:
			st.stroke = svgPaintVal{kind: svgPColor, c: c.Stroke}
		}
	}
	if c.StrokeWidthSet && c.StrokeWidth >= 0 {
		st.sw = c.StrokeWidth
	}
}

// ---------------------------------------------------------------------------
// Compilation

// compileSVG builds the doc for an <svg> node. Never panics on bad input.
func compileSVG(n *Node) (d *svgDoc) {
	d = &svgDoc{rootOp: 1}
	if n == nil {
		return d
	}
	defer func() {
		if recover() != nil {
			d = &svgDoc{rootOp: 1}
		}
	}()
	d.ids = map[string]*svgEl{}
	var uses []*svgEl
	var all []*svgEl
	root := d.build(n, 0, &uses, &all)
	if root == nil {
		return d
	}
	if root.kind != svgKSVG {
		// Not an <svg>: render it inside an implicit one.
		root = &svgEl{n: n, kind: svgKSVG, kids: []*svgEl{root}}
	}
	d.root = root
	if v, ok := n.AttrString("opacity"); ok {
		if f, ok := svgParseNumPct(v); ok {
			d.rootOp = svgClamp01(f)
		}
	}
	for _, u := range uses {
		u.ref = d.ids[u.href]
	}
	// A use whose target contains itself (directly or through other uses)
	// is an error in SVG and draws nothing.
	seen := map[*svgEl]bool{}
	for _, u := range uses {
		if u.ref == nil {
			continue
		}
		clear(seen)
		if svgReaches(u.ref, u, seen) {
			u.ref = nil
		}
	}
	for _, e := range all {
		d.resolvePaint(&e.pres.fill)
		d.resolvePaint(&e.pres.stroke)
		if e.kind == svgKGrad && len(e.stops) == 0 && e.href != "" {
			g := e
			for i := 0; i < 8 && g != nil && len(g.stops) == 0 && g.href != ""; i++ {
				g = d.ids[g.href]
				if g != nil && g.kind != svgKGrad {
					g = nil
				}
			}
			if g != nil {
				e.stops = g.stops
			}
		}
	}
	return d
}

func svgReaches(e, target *svgEl, seen map[*svgEl]bool) bool {
	if e == target {
		return true
	}
	if seen[e] {
		return false
	}
	seen[e] = true
	for _, k := range e.kids {
		if svgReaches(k, target, seen) {
			return true
		}
	}
	if e.kind == svgKUse && e.ref != nil {
		return svgReaches(e.ref, target, seen)
	}
	return false
}

func (d *svgDoc) resolvePaint(p *svgPaintVal) {
	if p.kind != svgPURL {
		return
	}
	if g := d.ids[p.id]; g != nil && g.kind == svgKGrad {
		p.ref = g
		return
	}
	// Unknown or unsupported paint server: the fallback, else none.
	if p.fb != 0 {
		p.kind = p.fb
	} else {
		p.kind = svgPNone
	}
}

// svgAttr reads an attribute under its React name, then its SVG name.
func svgAttr(n *Node, react, svg string) (string, bool) {
	if v, ok := n.AttrString(react); ok {
		return v, true
	}
	if svg != "" && svg != react {
		return n.AttrString(svg)
	}
	return "", false
}

func (d *svgDoc) build(n *Node, depth int, uses, all *[]*svgEl) *svgEl {
	if n == nil || n.T == "#" || n.T == "@" || depth > 64 {
		return nil
	}
	e := &svgEl{n: n}
	kids := false
	switch strings.ToLower(n.T) {
	case "svg":
		e.kind = svgKSVG
		d.lengths(e, "x", "y", "width", "height")
		e.vb, e.hasVB = svgViewBox(n)
		e.par = svgParsePAR(n)
		kids = true
	case "g", "a", "switch":
		e.kind = svgKGroup
		kids = true
	case "defs":
		e.kind = svgKDefs
		kids = true
	case "symbol":
		e.kind = svgKSymbol
		e.vb, e.hasVB = svgViewBox(n)
		e.par = svgParsePAR(n)
		kids = true
	case "path":
		e.kind = svgKPath
		s, _ := n.AttrString("d")
		e.segs = svgParsePath(s)
	case "polyline", "polygon":
		e.kind = svgKPath
		s, _ := n.AttrString("points")
		e.closed = strings.EqualFold(n.T, "polygon")
		e.segs = svgPolySegs(s, e.closed)
	case "rect":
		e.kind = svgKRect
		d.lengths(e, "x", "y", "width", "height", "rx", "ry")
	case "circle":
		e.kind = svgKCircle
		d.lengths(e, "cx", "cy", "r")
	case "ellipse":
		e.kind = svgKEllipse
		d.lengths(e, "cx", "cy", "", "", "rx", "ry")
	case "line":
		e.kind = svgKLine
		d.lengths(e, "x1", "y1", "x2", "y2")
	case "use":
		e.kind = svgKUse
		d.lengths(e, "x", "y", "width", "height")
		h, ok := svgAttr(n, "href", "")
		if !ok {
			h, ok = svgAttr(n, "xlinkHref", "xlink:href")
		}
		h = strings.TrimSpace(h)
		if ok && strings.HasPrefix(h, "#") {
			e.href = h[1:]
			*uses = append(*uses, e)
		}
	case "lineargradient", "radialgradient":
		e.kind = svgKGrad
		h, ok := svgAttr(n, "href", "")
		if !ok {
			h, _ = svgAttr(n, "xlinkHref", "xlink:href")
		}
		if h = strings.TrimSpace(h); strings.HasPrefix(h, "#") {
			e.href = h[1:]
		}
		e.stops = svgStops(n)
	default:
		return nil // title, desc, metadata, style, text, clipPath, mask, …
	}
	e.pres = svgPresOf(n)
	if t, ok := n.AttrString("transform"); ok {
		if m, ok := svgParseTransform(t); ok && m != svgIdent {
			e.xf, e.hasXf = m, true
		}
	}
	if e.kind == svgKPath && len(e.segs) > 0 {
		e.eoSegs = svgEvenOddSegs(e.segs)
	}
	if n.ID != "" {
		if _, dup := d.ids[n.ID]; !dup {
			d.ids[n.ID] = e
		}
	}
	*all = append(*all, e)
	if kids {
		for _, c := range n.C {
			if k := d.build(c, depth+1, uses, all); k != nil {
				e.kids = append(e.kids, k)
			}
		}
	}
	return e
}

// lengths parses length attributes into e.len (an empty name skips a slot).
func (d *svgDoc) lengths(e *svgEl, names ...string) {
	for i, name := range names {
		if name == "" {
			continue
		}
		if v, ok := e.n.AttrString(name); ok {
			if l, ok := svgParseLen(v); ok {
				e.len[i] = l
				e.lenOK |= 1 << i
			}
		}
	}
}

func (e *svgEl) has(i int) bool { return e.lenOK&(1<<i) != 0 }

func svgViewBox(n *Node) ([4]float32, bool) {
	var vb [4]float32
	s, ok := svgAttr(n, "viewBox", "viewbox")
	if !ok {
		return vb, false
	}
	t := svgTok{s: s}
	for i := range vb {
		v, ok := t.num()
		if !ok {
			return [4]float32{}, false
		}
		vb[i] = v
	}
	if t.sep(); !t.done() || !(vb[2] > 0) || !(vb[3] > 0) {
		return [4]float32{}, false
	}
	return vb, true
}

func svgParsePAR(n *Node) svgPAR {
	s, ok := svgAttr(n, "preserveAspectRatio", "preserveaspectratio")
	if !ok {
		return svgPARDefault
	}
	f := strings.Fields(s)
	if len(f) > 0 && f[0] == "defer" {
		f = f[1:]
	}
	if len(f) == 0 {
		return svgPARDefault
	}
	p := svgPARDefault
	a := f[0]
	switch {
	case a == "none":
		p.none = true
	case len(a) == 8 && a[0] == 'x' && a[4] == 'Y':
		v := func(s string) (float32, bool) {
			switch s {
			case "Min":
				return 0, true
			case "Mid":
				return .5, true
			case "Max":
				return 1, true
			}
			return 0, false
		}
		ax, ok1 := v(a[1:4])
		ay, ok2 := v(a[5:8])
		if !ok1 || !ok2 {
			return svgPARDefault
		}
		p.ax, p.ay = ax, ay
	default:
		return svgPARDefault
	}
	if len(f) > 1 {
		switch f[1] {
		case "slice":
			p.slice = true
		case "meet":
		default:
			return svgPARDefault
		}
	}
	return p
}

func svgStops(g *Node) []svgStop {
	var out []svgStop
	last := float32(0)
	for _, s := range g.C {
		if s == nil || !strings.EqualFold(s.T, "stop") {
			continue
		}
		st := svgStop{c: svgPaintVal{kind: svgPColor, c: color.NRGBA{A: 255}}, op: 1}
		if v, ok := s.AttrString("offset"); ok {
			if f, ok := svgParseNumPct(v); ok {
				st.off = svgClamp01(f)
			}
		}
		set := func(prop, v string) {
			v = strings.TrimSpace(v)
			switch prop {
			case "stop-color":
				if strings.EqualFold(v, "currentcolor") {
					st.c = svgPaintVal{kind: svgPCurrent}
				} else if c, ok := svgParseColor(v); ok {
					st.c = svgPaintVal{kind: svgPColor, c: c}
				}
			case "stop-opacity":
				if f, ok := svgParseNumPct(v); ok {
					st.op = svgClamp01(f)
				}
			}
		}
		if v, ok := svgAttr(s, "stopColor", "stop-color"); ok {
			set("stop-color", v)
		}
		if v, ok := svgAttr(s, "stopOpacity", "stop-opacity"); ok {
			set("stop-opacity", v)
		}
		for _, dcl := range s.Style {
			set(dcl.Prop, dcl.Value)
		}
		if st.off < last {
			st.off = last
		}
		last = st.off
		out = append(out, st)
	}
	return out
}

// svgPresAttrs maps React attribute names to SVG property names.
var svgPresAttrs = [...][2]string{
	{"color", "color"},
	{"fill", "fill"},
	{"stroke", "stroke"},
	{"strokeWidth", "stroke-width"},
	{"opacity", "opacity"},
	{"fillOpacity", "fill-opacity"},
	{"strokeOpacity", "stroke-opacity"},
	{"fillRule", "fill-rule"},
	{"strokeLinecap", "stroke-linecap"},
	{"strokeLinejoin", "stroke-linejoin"},
	{"strokeMiterlimit", "stroke-miterlimit"},
	{"display", "display"},
	{"visibility", "visibility"},
}

func svgPresOf(n *Node) svgPres {
	var p svgPres
	for _, a := range svgPresAttrs {
		if v, ok := svgAttr(n, a[0], a[1]); ok {
			p.setProp(a[1], v)
		}
	}
	for _, dcl := range n.Style {
		p.setProp(dcl.Prop, dcl.Value)
	}
	return p
}

// setProp applies one presentation property; invalid values and
// "inherit"/"unset" leave it unset.
func (p *svgPres) setProp(prop, v string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	lv := strings.ToLower(v)
	if lv == "inherit" || lv == "unset" || lv == "initial" || lv == "revert" {
		return
	}
	switch prop {
	case "color":
		if c, ok := svgParseColor(v); ok {
			p.color = c
			p.set |= svgSetColor
		}
	case "fill", "stroke":
		pv, ok := svgParsePaintVal(v)
		if !ok {
			return
		}
		if prop == "fill" {
			p.fill = pv
			p.set |= svgSetFill
		} else {
			p.stroke = pv
			p.set |= svgSetStroke
		}
	case "stroke-width":
		if l, ok := svgParseLen(v); ok && l.v >= 0 {
			p.sw = l
			p.set |= svgSetSW
		}
	case "opacity", "fill-opacity", "stroke-opacity":
		f, ok := svgParseNumPct(v)
		if !ok {
			return
		}
		f = svgClamp01(f)
		switch prop {
		case "opacity":
			p.opacity = f
			p.set |= svgSetOpacity
		case "fill-opacity":
			p.fo = f
			p.set |= svgSetFO
		default:
			p.so = f
			p.set |= svgSetSO
		}
	case "fill-rule":
		switch lv {
		case "nonzero":
			p.rule = svgRuleNonzero
		case "evenodd":
			p.rule = svgRuleEvenOdd
		default:
			return
		}
		p.set |= svgSetRule
	case "stroke-linecap":
		switch lv {
		case "butt":
			p.cap = svgCapButt
		case "round":
			p.cap = svgCapRound
		case "square":
			p.cap = svgCapSquare
		default:
			return
		}
		p.set |= svgSetCap
	case "stroke-linejoin":
		switch lv {
		case "miter", "miter-clip", "arcs":
			p.join = svgJoinMiter
		case "round":
			p.join = svgJoinRound
		case "bevel":
			p.join = svgJoinBevel
		default:
			return
		}
		p.set |= svgSetJoin
	case "stroke-miterlimit":
		if f, err := strconv.ParseFloat(v, 32); err == nil && f >= 1 {
			p.miter = float32(f)
			p.set |= svgSetMiter
		}
	case "display":
		p.hidden = lv == "none"
	case "visibility":
		switch lv {
		case "visible":
			p.visible = true
		case "hidden", "collapse":
			p.visible = false
		default:
			return
		}
		p.set |= svgSetVis
	}
}

func svgParsePaintVal(v string) (svgPaintVal, bool) {
	lv := strings.ToLower(v)
	switch {
	case lv == "none":
		return svgPaintVal{kind: svgPNone}, true
	case lv == "currentcolor":
		return svgPaintVal{kind: svgPCurrent}, true
	case strings.HasPrefix(lv, "url("):
		end := strings.IndexByte(v, ')')
		if end < 0 {
			return svgPaintVal{}, false
		}
		id := strings.Trim(strings.TrimSpace(v[4:end]), `"'`)
		if !strings.HasPrefix(id, "#") {
			return svgPaintVal{}, false
		}
		pv := svgPaintVal{kind: svgPURL, id: id[1:]}
		if fb := strings.TrimSpace(v[end+1:]); fb != "" {
			if f, ok := svgParsePaintVal(fb); ok && f.kind != svgPURL {
				pv.fb, pv.c = f.kind, f.c
			}
		}
		return pv, true
	}
	if c, ok := svgParseColor(v); ok {
		return svgPaintVal{kind: svgPColor, c: c}, true
	}
	return svgPaintVal{}, false
}

// svgParseLen parses a length: a number with an optional unit or a
// percentage. "auto" and garbage fail.
func svgParseLen(s string) (svgLen, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return svgLen{}, false
	}
	t := svgTok{s: s}
	v, ok := t.num()
	if !ok {
		return svgLen{}, false
	}
	u := strings.ToLower(strings.TrimSpace(s[t.i:]))
	switch u {
	case "", "px":
	case "%":
		return svgLen{v: v, pct: true}, true
	case "pt":
		v *= 4. / 3
	case "pc":
		v *= 16
	case "in":
		v *= 96
	case "cm":
		v *= 96 / 2.54
	case "mm":
		v *= 96 / 25.4
	case "q":
		v *= 96 / 101.6
	case "em", "rem":
		v *= 16
	case "ex":
		v *= 8
	default:
		return svgLen{}, false
	}
	return svgLen{v: v}, true
}

// svgParseNumPct parses a number or a percentage (as a fraction).
func svgParseNumPct(s string) (float32, bool) {
	s = strings.TrimSpace(s)
	pct := strings.HasSuffix(s, "%")
	if pct {
		s = s[:len(s)-1]
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	if err != nil || math.IsNaN(f) {
		return 0, false
	}
	if pct {
		f /= 100
	}
	return float32(f), true
}

func svgClamp01(f float32) float32 {
	if !(f > 0) {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func svgPolySegs(s string, closed bool) []svgSeg {
	t := svgTok{s: s}
	var out []svgSeg
	for {
		x, ok1 := t.num()
		if !ok1 {
			break
		}
		y, ok2 := t.num()
		if !ok2 {
			break // an odd coordinate count drops the last one
		}
		op := byte('L')
		if len(out) == 0 {
			op = 'M'
		}
		out = append(out, svgSeg{op: op, p: [3]f32.Point{{X: x, Y: y}}})
	}
	if closed && len(out) > 0 {
		out = append(out, svgSeg{op: 'Z'})
	}
	return out
}

// ---------------------------------------------------------------------------
// Numbers and path data

type svgTok struct {
	s string
	i int
}

func (t *svgTok) done() bool { return t.i >= len(t.s) }

func svgIsWS(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func svgIsDigit(c byte) bool { return c >= '0' && c <= '9' }

// ws skips whitespace.
func (t *svgTok) ws() {
	for t.i < len(t.s) && svgIsWS(t.s[t.i]) {
		t.i++
	}
}

// sep skips whitespace and commas.
func (t *svgTok) sep() {
	for t.i < len(t.s) && (svgIsWS(t.s[t.i]) || t.s[t.i] == ',') {
		t.i++
	}
}

// numStart reports whether a number starts at the next token.
func (t *svgTok) numStart() bool {
	t.sep()
	if t.i >= len(t.s) {
		return false
	}
	c := t.s[t.i]
	return svgIsDigit(c) || c == '.' || c == '-' || c == '+'
}

// num reads one number: [+-] digits [. digits] [e [+-] digits], so "1.5.5"
// is 1.5 then .5 and "-1-2" is -1 then -2. On failure only separators are
// consumed.
func (t *svgTok) num() (float32, bool) {
	t.sep()
	s := t.s
	st, i := t.i, t.i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := 0
	for i < len(s) && svgIsDigit(s[i]) {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		j := i + 1
		frac := 0
		for j < len(s) && svgIsDigit(s[j]) {
			j++
			frac++
		}
		if frac > 0 || digits > 0 {
			i = j
			digits += frac
		}
	}
	if digits == 0 {
		return 0, false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && svgIsDigit(s[j]) {
			for j < len(s) && svgIsDigit(s[j]) {
				j++
			}
			i = j
		}
	}
	v, err := strconv.ParseFloat(s[st:i], 64)
	if err != nil || math.IsNaN(v) {
		return 0, false
	}
	t.i = i
	const lim = 1e7
	if v > lim {
		v = lim
	} else if v < -lim {
		v = -lim
	}
	return float32(v), true
}

// flag reads an arc flag: a single 0 or 1, separators optional.
func (t *svgTok) flag() (bool, bool) {
	t.sep()
	if t.i < len(t.s) {
		switch t.s[t.i] {
		case '0':
			t.i++
			return false, true
		case '1':
			t.i++
			return true, true
		}
	}
	return false, false
}

// svgParsePath parses SVG path data into absolute M/L/C/Z segments (H, V
// become L; Q, T, S and A become cubics). On an error it keeps everything
// before the bad segment, like browsers.
func svgParsePath(d string) []svgSeg {
	t := svgTok{s: d}
	var out []svgSeg
	var cur, start, lastC, lastQ f32.Point
	var prev byte // previous command, uppercased
	needM := false
	first := true
	for {
		t.sep()
		if t.done() {
			break
		}
		c := t.s[t.i]
		if !strings.ContainsRune("MmLlHhVvCcSsQqTtAaZz", rune(c)) {
			break
		}
		t.i++
		cmd := c &^ 0x20
		rel := c >= 'a'
		if first && cmd != 'M' {
			break
		}
		first = false
		if cmd == 'Z' {
			if len(out) > 0 && out[len(out)-1].op != 'Z' {
				out = append(out, svgSeg{op: 'Z'})
			}
			cur = start
			needM = true
			prev = 'Z'
			continue
		}
		ok := true
		for n := 0; ok; n++ {
			if n > 0 && !t.numStart() {
				break
			}
			var base f32.Point
			if rel {
				base = cur
			}
			pt := func() (f32.Point, bool) {
				x, ok1 := t.num()
				y, ok2 := t.num()
				return f32.Point{X: base.X + x, Y: base.Y + y}, ok1 && ok2
			}
			if cmd != 'M' && needM {
				out = append(out, svgSeg{op: 'M', p: [3]f32.Point{cur}})
				needM = false
			}
			op := cmd
			if cmd == 'M' && n > 0 {
				op = 'L' // implicit lineto after moveto
			}
			switch op {
			case 'M':
				p, k := pt()
				if !k {
					ok = false
					break
				}
				out = append(out, svgSeg{op: 'M', p: [3]f32.Point{p}})
				cur, start = p, p
				needM = false
			case 'L':
				p, k := pt()
				if !k {
					ok = false
					break
				}
				out = append(out, svgSeg{op: 'L', p: [3]f32.Point{p}})
				cur = p
			case 'H':
				x, k := t.num()
				if !k {
					ok = false
					break
				}
				cur = f32.Point{X: base.X + x, Y: cur.Y}
				out = append(out, svgSeg{op: 'L', p: [3]f32.Point{cur}})
			case 'V':
				y, k := t.num()
				if !k {
					ok = false
					break
				}
				cur = f32.Point{X: cur.X, Y: base.Y + y}
				out = append(out, svgSeg{op: 'L', p: [3]f32.Point{cur}})
			case 'C':
				c1, k1 := pt()
				c2, k2 := pt()
				p, k3 := pt()
				if !k1 || !k2 || !k3 {
					ok = false
					break
				}
				out = append(out, svgSeg{op: 'C', p: [3]f32.Point{c1, c2, p}})
				lastC, cur = c2, p
			case 'S':
				c1 := cur
				if prev == 'C' || prev == 'S' {
					c1 = cur.Mul(2).Sub(lastC)
				}
				c2, k1 := pt()
				p, k2 := pt()
				if !k1 || !k2 {
					ok = false
					break
				}
				out = append(out, svgSeg{op: 'C', p: [3]f32.Point{c1, c2, p}})
				lastC, cur = c2, p
			case 'Q':
				q, k1 := pt()
				p, k2 := pt()
				if !k1 || !k2 {
					ok = false
					break
				}
				out = append(out, svgQuad(cur, q, p))
				lastQ, cur = q, p
			case 'T':
				q := cur
				if prev == 'Q' || prev == 'T' {
					q = cur.Mul(2).Sub(lastQ)
				}
				p, k := pt()
				if !k {
					ok = false
					break
				}
				out = append(out, svgQuad(cur, q, p))
				lastQ, cur = q, p
			case 'A':
				rx, k1 := t.num()
				ry, k2 := t.num()
				rot, k3 := t.num()
				large, k4 := t.flag()
				sweep, k5 := t.flag()
				p, k6 := pt()
				if !(k1 && k2 && k3 && k4 && k5 && k6) {
					ok = false
					break
				}
				out = svgArc(out, cur, p, rx, ry, rot, large, sweep)
				cur = p
			}
			if ok {
				prev = op
			}
		}
		if !ok {
			break
		}
	}
	return out
}

func svgQuad(from, q, to f32.Point) svgSeg {
	c1 := from.Add(q.Sub(from).Mul(2.0 / 3))
	c2 := to.Add(q.Sub(to).Mul(2.0 / 3))
	return svgSeg{op: 'C', p: [3]f32.Point{c1, c2, to}}
}

// svgArc appends an elliptical arc from p0 to p1 as cubics, following the
// SVG implementation notes (F.6.5 conversion, F.6.6 radius correction). The
// last cubic ends exactly at p1.
func svgArc(out []svgSeg, p0, p1 f32.Point, rx, ry, rotDeg float32, large, sweep bool) []svgSeg {
	if p0 == p1 {
		return out // omitted
	}
	if rx == 0 || ry == 0 {
		return append(out, svgSeg{op: 'L', p: [3]f32.Point{p1}})
	}
	fx, fy := math.Abs(float64(rx)), math.Abs(float64(ry))
	phi := math.Mod(float64(rotDeg), 360) * math.Pi / 180
	sp, cp := math.Sincos(phi)
	dx, dy := float64(p0.X-p1.X)/2, float64(p0.Y-p1.Y)/2
	x1 := cp*dx + sp*dy
	y1 := -sp*dx + cp*dy
	if lam := x1*x1/(fx*fx) + y1*y1/(fy*fy); lam > 1 {
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
		return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	}
	ux, uy := (x1-cx1)/fx, (y1-cy1)/fy
	vx, vy := (-x1-cx1)/fx, (-y1-cy1)/fy
	th1 := ang(1, 0, ux, uy)
	dth := ang(ux, uy, vx, vy)
	if !sweep && dth > 0 {
		dth -= 2 * math.Pi
	} else if sweep && dth < 0 {
		dth += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dth)/(math.Pi/2) - 1e-9))
	if n < 1 {
		n = 1
	}
	step := dth / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	pt := func(t float64) (float64, float64) {
		st, ct := math.Sincos(t)
		x, y := fx*ct, fy*st
		return cp*x - sp*y + cx, sp*x + cp*y + cy
	}
	der := func(t float64) (float64, float64) {
		st, ct := math.Sincos(t)
		x, y := -fx*st, fy*ct
		return cp*x - sp*y, sp*x + cp*y
	}
	t := th1
	ax, ay := float64(p0.X), float64(p0.Y)
	for i := 0; i < n; i++ {
		adx, ady := der(t)
		bx, by := pt(t + step)
		bdx, bdy := der(t + step)
		end := f32.Point{X: float32(bx), Y: float32(by)}
		if i == n-1 {
			end = p1
		}
		out = append(out, svgSeg{op: 'C', p: [3]f32.Point{
			{X: float32(ax + k*adx), Y: float32(ay + k*ady)},
			{X: float32(bx - k*bdx), Y: float32(by - k*bdy)},
			end,
		}})
		ax, ay = bx, by
		t += step
	}
	return out
}

// svgEvenOddSegs reorients subpaths so that nonzero filling gives the
// evenodd result for nested, non-intersecting contours: a subpath inside an
// odd number of others winds opposite to one inside an even number. It
// returns nil when no subpath needs flipping.
func svgEvenOddSegs(segs []svgSeg) []svgSeg {
	type sub struct {
		a, b int // segs[a:b]
		poly []f32.Point
		area float32
	}
	var subs []sub
	for i := 0; i < len(segs); {
		if segs[i].op != 'M' {
			i++
			continue
		}
		j := i + 1
		for j < len(segs) && segs[j].op != 'M' {
			j++
		}
		s := sub{a: i, b: j}
		cur := segs[i].p[0]
		s.poly = append(s.poly, cur)
		for _, g := range segs[i+1 : j] {
			switch g.op {
			case 'L':
				cur = g.p[0]
				s.poly = append(s.poly, cur)
			case 'C':
				for k := 1; k <= 8; k++ {
					s.poly = append(s.poly, svgCubicAt(cur, g.p[0], g.p[1], g.p[2], float32(k)/8))
				}
				cur = g.p[2]
			}
		}
		s.area = svgPolyArea(s.poly)
		if len(s.poly) >= 3 && s.area != 0 {
			subs = append(subs, s)
		}
		i = j
	}
	if len(subs) < 2 {
		return nil
	}
	flip := make([]bool, len(subs))
	flipped := false
	for i := range subs {
		depth := 0
		p := subs[i].poly[0]
		for j := range subs {
			if i != j && svgInPoly(p, subs[j].poly) {
				depth++
			}
		}
		want := depth%2 == 0 // positive orientation wanted
		if (subs[i].area > 0) != want {
			flip[i] = true
			flipped = true
		}
	}
	if !flipped {
		return nil
	}
	out := make([]svgSeg, 0, len(segs)+len(subs))
	next := 0
	for i, s := range subs {
		out = append(out, segs[next:s.a]...) // skipped degenerate subpaths
		next = s.b
		if !flip[i] {
			out = append(out, segs[s.a:s.b]...)
			continue
		}
		out = svgReverseSub(out, segs[s.a:s.b])
	}
	return append(out, segs[next:]...)
}

// svgReverseSub appends the subpath sub (starting with 'M') reversed.
func svgReverseSub(out, sub []svgSeg) []svgSeg {
	closed := sub[len(sub)-1].op == 'Z'
	// End point of each segment.
	endOf := func(i int) f32.Point {
		switch sub[i].op {
		case 'C':
			return sub[i].p[2]
		}
		return sub[i].p[0]
	}
	last := 0
	for i := range sub {
		if sub[i].op != 'Z' {
			last = i
		}
	}
	out = append(out, svgSeg{op: 'M', p: [3]f32.Point{endOf(last)}})
	for i := last; i >= 1; i-- {
		prev := endOf(i - 1)
		switch sub[i].op {
		case 'L':
			out = append(out, svgSeg{op: 'L', p: [3]f32.Point{prev}})
		case 'C':
			out = append(out, svgSeg{op: 'C', p: [3]f32.Point{sub[i].p[1], sub[i].p[0], prev}})
		}
	}
	if closed {
		out = append(out, svgSeg{op: 'Z'})
	}
	return out
}

func svgPolyArea(p []f32.Point) float32 {
	var a float32
	for i := range p {
		q := p[(i+1)%len(p)]
		a += p[i].X*q.Y - q.X*p[i].Y
	}
	return a / 2
}

func svgInPoly(pt f32.Point, poly []f32.Point) bool {
	in := false
	j := len(poly) - 1
	for i := range poly {
		a, b := poly[i], poly[j]
		if (a.Y > pt.Y) != (b.Y > pt.Y) && pt.X < (b.X-a.X)*(pt.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
		j = i
	}
	return in
}

func svgCubicAt(p0, p1, p2, p3 f32.Point, t float32) f32.Point {
	u := 1 - t
	a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
	return f32.Point{
		X: a*p0.X + b*p1.X + c*p2.X + d*p3.X,
		Y: a*p0.Y + b*p1.Y + c*p2.Y + d*p3.Y,
	}
}

// ---------------------------------------------------------------------------
// Transforms

// svgMat is an SVG matrix(a b c d e f): x' = a x + c y + e, y' = b x + d y + f.
type svgMat [6]float32

var svgIdent = svgMat{1, 0, 0, 1, 0, 0}

// mul returns m·n (n applied first).
func (m svgMat) mul(n svgMat) svgMat {
	return svgMat{
		m[0]*n[0] + m[2]*n[1],
		m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3],
		m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4],
		m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

func (m svgMat) apply(p f32.Point) f32.Point {
	return f32.Point{X: m[0]*p.X + m[2]*p.Y + m[4], Y: m[1]*p.X + m[3]*p.Y + m[5]}
}

func (m svgMat) aff() f32.Affine2D {
	return f32.NewAffine2D(m[0], m[2], m[4], m[1], m[3], m[5])
}

// scale is the transform's average linear scale, sqrt|det|.
func (m svgMat) scale() float32 {
	return float32(math.Sqrt(math.Abs(float64(m[0]*m[3] - m[1]*m[2]))))
}

// svgParseTransform parses a transform list. An invalid list is ignored
// entirely (ok=false), as in browsers.
func svgParseTransform(s string) (svgMat, bool) {
	t := svgTok{s: s}
	m := svgIdent
	for {
		t.sep()
		if t.done() {
			return m, true
		}
		st := t.i
		for t.i < len(t.s) && (t.s[t.i]|0x20 >= 'a' && t.s[t.i]|0x20 <= 'z') {
			t.i++
		}
		name := t.s[st:t.i]
		t.ws()
		if t.done() || t.s[t.i] != '(' {
			return svgIdent, false
		}
		t.i++
		var a [6]float32
		n := 0
		for {
			t.sep()
			if t.done() {
				return svgIdent, false
			}
			if t.s[t.i] == ')' {
				t.i++
				break
			}
			if n == len(a) {
				return svgIdent, false
			}
			v, ok := t.num()
			if !ok {
				return svgIdent, false
			}
			a[n] = v
			n++
		}
		var x svgMat
		switch name {
		case "matrix":
			if n != 6 {
				return svgIdent, false
			}
			x = svgMat(a)
		case "translate":
			if n != 1 && n != 2 {
				return svgIdent, false
			}
			x = svgMat{1, 0, 0, 1, a[0], a[1]}
		case "scale":
			if n != 1 && n != 2 {
				return svgIdent, false
			}
			sy := a[1]
			if n == 1 {
				sy = a[0]
			}
			x = svgMat{a[0], 0, 0, sy, 0, 0}
		case "rotate":
			if n != 1 && n != 3 {
				return svgIdent, false
			}
			sn, cs := math.Sincos(float64(a[0]) * math.Pi / 180)
			s, c := float32(sn), float32(cs)
			x = svgMat{c, s, -s, c, 0, 0}
			if n == 3 {
				x = svgMat{1, 0, 0, 1, a[1], a[2]}.mul(x).mul(svgMat{1, 0, 0, 1, -a[1], -a[2]})
			}
		case "skewX":
			if n != 1 {
				return svgIdent, false
			}
			x = svgMat{1, 0, float32(math.Tan(float64(a[0]) * math.Pi / 180)), 1, 0, 0}
		case "skewY":
			if n != 1 {
				return svgIdent, false
			}
			x = svgMat{1, float32(math.Tan(float64(a[0]) * math.Pi / 180)), 0, 1, 0, 0}
		default:
			return svgIdent, false
		}
		m = m.mul(x)
	}
}

// svgViewBoxMat maps viewBox vb into the viewport (x, y, w, h).
func svgViewBoxMat(vb [4]float32, par svgPAR, x, y, w, h float32) svgMat {
	sx, sy := w/vb[2], h/vb[3]
	if par.none {
		return svgMat{sx, 0, 0, sy, x - vb[0]*sx, y - vb[1]*sy}
	}
	s := min(sx, sy)
	if par.slice {
		s = max(sx, sy)
	}
	tx := x - vb[0]*s + par.ax*(w-vb[2]*s)
	ty := y - vb[1]*s + par.ay*(h-vb[3]*s)
	return svgMat{s, 0, 0, s, tx, ty}
}

// ---------------------------------------------------------------------------
// Colors

// svgParseColor parses a CSS color: #rgb #rgba #rrggbb #rrggbbaa, rgb[a]()
// and hsl[a]() in comma or space syntax (numbers or percentages, optional
// "/ alpha"), named colors and transparent.
func svgParseColor(s string) (color.NRGBA, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return color.NRGBA{}, false
	}
	if s[0] == '#' {
		return svgParseHex(s[1:])
	}
	ls := strings.ToLower(s)
	if i := strings.IndexByte(ls, '('); i > 0 && strings.HasSuffix(ls, ")") {
		fn := strings.TrimSpace(ls[:i])
		args, form, ok := svgColorArgs(ls[i+1 : len(ls)-1])
		if !ok {
			return color.NRGBA{}, false
		}
		n := len(args)
		switch form {
		case svgArgsSpace:
			ok = n == 3
		case svgArgsSlash:
			ok = n == 4
		default:
			ok = n == 3 || n == 4
		}
		if !ok {
			return color.NRGBA{}, false
		}
		alpha := float32(1)
		if n == 4 {
			a, ok := svgColorAlpha(args[3])
			if !ok {
				return color.NRGBA{}, false
			}
			alpha = a
		}
		var r, g, b float32
		switch fn {
		case "rgb", "rgba":
			var c [3]float32
			for k := 0; k < 3; k++ {
				v := args[k]
				if strings.HasSuffix(v, "%") {
					f, err := strconv.ParseFloat(v[:len(v)-1], 32)
					if err != nil {
						return color.NRGBA{}, false
					}
					c[k] = float32(f) / 100
				} else {
					f, err := strconv.ParseFloat(v, 32)
					if err != nil {
						return color.NRGBA{}, false
					}
					c[k] = float32(f) / 255
				}
			}
			r, g, b = c[0], c[1], c[2]
		case "hsl", "hsla":
			h, err := strconv.ParseFloat(strings.TrimSuffix(args[0], "deg"), 64)
			if err != nil {
				return color.NRGBA{}, false
			}
			sat, ok1 := svgPct(args[1])
			lig, ok2 := svgPct(args[2])
			if !ok1 || !ok2 {
				return color.NRGBA{}, false
			}
			r, g, b = svgHSL(float32(h), sat, lig)
		default:
			return color.NRGBA{}, false
		}
		return color.NRGBA{R: svgByte(r), G: svgByte(g), B: svgByte(b), A: svgByte(alpha)}, true
	}
	if ls == "transparent" {
		return color.NRGBA{}, true
	}
	if v, ok := svgNamedColors[ls]; ok {
		return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}, true
	}
	return color.NRGBA{}, false
}

func svgParseHex(h string) (color.NRGBA, bool) {
	var d [8]uint8
	if len(h) != 3 && len(h) != 4 && len(h) != 6 && len(h) != 8 {
		return color.NRGBA{}, false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		switch {
		case c >= '0' && c <= '9':
			d[i] = c - '0'
		case c >= 'a' && c <= 'f':
			d[i] = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d[i] = c - 'A' + 10
		default:
			return color.NRGBA{}, false
		}
	}
	switch len(h) {
	case 3:
		return color.NRGBA{R: d[0] * 17, G: d[1] * 17, B: d[2] * 17, A: 255}, true
	case 4:
		return color.NRGBA{R: d[0] * 17, G: d[1] * 17, B: d[2] * 17, A: d[3] * 17}, true
	case 6:
		return color.NRGBA{R: d[0]<<4 | d[1], G: d[2]<<4 | d[3], B: d[4]<<4 | d[5], A: 255}, true
	}
	return color.NRGBA{R: d[0]<<4 | d[1], G: d[2]<<4 | d[3], B: d[4]<<4 | d[5], A: d[6]<<4 | d[7]}, true
}

const (
	svgArgsSpace = iota // "1 2 3"
	svgArgsComma        // "1, 2, 3, .5"
	svgArgsSlash        // "1 2 3 / 50%"
)

// svgColorArgs splits a color function's arguments.
func svgColorArgs(s string) (args []string, form int, ok bool) {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		if strings.Contains(s, ",") {
			return nil, 0, false
		}
		b := strings.TrimSpace(s[i+1:])
		if b == "" || strings.ContainsAny(b, "/ ") {
			return nil, 0, false
		}
		return append(strings.Fields(s[:i]), b), svgArgsSlash, true
	}
	if strings.Contains(s, ",") {
		for _, p := range strings.Split(s, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				return nil, 0, false
			}
			args = append(args, p)
		}
		return args, svgArgsComma, true
	}
	return strings.Fields(s), svgArgsSpace, true
}

func svgColorAlpha(s string) (float32, bool) {
	f, ok := svgParseNumPct(s)
	return svgClamp01(f), ok
}

func svgPct(s string) (float32, bool) {
	s = strings.TrimSuffix(s, "%")
	f, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0, false
	}
	return svgClamp01(float32(f) / 100), true
}

func svgHSL(h, s, l float32) (r, g, b float32) {
	h = float32(math.Mod(float64(h), 360))
	if h < 0 {
		h += 360
	}
	h /= 360
	var q float32
	if l < .5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	hue := func(t float32) float32 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1./6:
			return p + (q-p)*6*t
		case t < .5:
			return q
		case t < 2./3:
			return p + (q-p)*(2./3-t)*6
		}
		return p
	}
	return hue(h + 1./3), hue(h), hue(h - 1./3)
}

func svgByte(f float32) uint8 {
	f = svgClamp01(f)
	return uint8(f*255 + .5)
}

// svgNamedColors are the 148 CSS named colors.
var svgNamedColors = map[string]uint32{
	"aliceblue": 0xf0f8ff, "antiquewhite": 0xfaebd7, "aqua": 0x00ffff, "aquamarine": 0x7fffd4,
	"azure": 0xf0ffff, "beige": 0xf5f5dc, "bisque": 0xffe4c4, "black": 0x000000,
	"blanchedalmond": 0xffebcd, "blue": 0x0000ff, "blueviolet": 0x8a2be2, "brown": 0xa52a2a,
	"burlywood": 0xdeb887, "cadetblue": 0x5f9ea0, "chartreuse": 0x7fff00, "chocolate": 0xd2691e,
	"coral": 0xff7f50, "cornflowerblue": 0x6495ed, "cornsilk": 0xfff8dc, "crimson": 0xdc143c,
	"cyan": 0x00ffff, "darkblue": 0x00008b, "darkcyan": 0x008b8b, "darkgoldenrod": 0xb8860b,
	"darkgray": 0xa9a9a9, "darkgreen": 0x006400, "darkgrey": 0xa9a9a9, "darkkhaki": 0xbdb76b,
	"darkmagenta": 0x8b008b, "darkolivegreen": 0x556b2f, "darkorange": 0xff8c00, "darkorchid": 0x9932cc,
	"darkred": 0x8b0000, "darksalmon": 0xe9967a, "darkseagreen": 0x8fbc8f, "darkslateblue": 0x483d8b,
	"darkslategray": 0x2f4f4f, "darkslategrey": 0x2f4f4f, "darkturquoise": 0x00ced1, "darkviolet": 0x9400d3,
	"deeppink": 0xff1493, "deepskyblue": 0x00bfff, "dimgray": 0x696969, "dimgrey": 0x696969,
	"dodgerblue": 0x1e90ff, "firebrick": 0xb22222, "floralwhite": 0xfffaf0, "forestgreen": 0x228b22,
	"fuchsia": 0xff00ff, "gainsboro": 0xdcdcdc, "ghostwhite": 0xf8f8ff, "gold": 0xffd700,
	"goldenrod": 0xdaa520, "gray": 0x808080, "green": 0x008000, "greenyellow": 0xadff2f,
	"grey": 0x808080, "honeydew": 0xf0fff0, "hotpink": 0xff69b4, "indianred": 0xcd5c5c,
	"indigo": 0x4b0082, "ivory": 0xfffff0, "khaki": 0xf0e68c, "lavender": 0xe6e6fa,
	"lavenderblush": 0xfff0f5, "lawngreen": 0x7cfc00, "lemonchiffon": 0xfffacd, "lightblue": 0xadd8e6,
	"lightcoral": 0xf08080, "lightcyan": 0xe0ffff, "lightgoldenrodyellow": 0xfafad2, "lightgray": 0xd3d3d3,
	"lightgreen": 0x90ee90, "lightgrey": 0xd3d3d3, "lightpink": 0xffb6c1, "lightsalmon": 0xffa07a,
	"lightseagreen": 0x20b2aa, "lightskyblue": 0x87cefa, "lightslategray": 0x778899, "lightslategrey": 0x778899,
	"lightsteelblue": 0xb0c4de, "lightyellow": 0xffffe0, "lime": 0x00ff00, "limegreen": 0x32cd32,
	"linen": 0xfaf0e6, "magenta": 0xff00ff, "maroon": 0x800000, "mediumaquamarine": 0x66cdaa,
	"mediumblue": 0x0000cd, "mediumorchid": 0xba55d3, "mediumpurple": 0x9370db, "mediumseagreen": 0x3cb371,
	"mediumslateblue": 0x7b68ee, "mediumspringgreen": 0x00fa9a, "mediumturquoise": 0x48d1cc, "mediumvioletred": 0xc71585,
	"midnightblue": 0x191970, "mintcream": 0xf5fffa, "mistyrose": 0xffe4e1, "moccasin": 0xffe4b5,
	"navajowhite": 0xffdead, "navy": 0x000080, "oldlace": 0xfdf5e6, "olive": 0x808000,
	"olivedrab": 0x6b8e23, "orange": 0xffa500, "orangered": 0xff4500, "orchid": 0xda70d6,
	"palegoldenrod": 0xeee8aa, "palegreen": 0x98fb98, "paleturquoise": 0xafeeee, "palevioletred": 0xdb7093,
	"papayawhip": 0xffefd5, "peachpuff": 0xffdab9, "peru": 0xcd853f, "pink": 0xffc0cb,
	"plum": 0xdda0dd, "powderblue": 0xb0e0e6, "purple": 0x800080, "rebeccapurple": 0x663399,
	"red": 0xff0000, "rosybrown": 0xbc8f8f, "royalblue": 0x4169e1, "saddlebrown": 0x8b4513,
	"salmon": 0xfa8072, "sandybrown": 0xf4a460, "seagreen": 0x2e8b57, "seashell": 0xfff5ee,
	"sienna": 0xa0522d, "silver": 0xc0c0c0, "skyblue": 0x87ceeb, "slateblue": 0x6a5acd,
	"slategray": 0x708090, "slategrey": 0x708090, "snow": 0xfffafa, "springgreen": 0x00ff7f,
	"steelblue": 0x4682b4, "tan": 0xd2b48c, "teal": 0x008080, "thistle": 0xd8bfd8,
	"tomato": 0xff6347, "turquoise": 0x40e0d0, "violet": 0xee82ee, "wheat": 0xf5deb3,
	"white": 0xffffff, "whitesmoke": 0xf5f5f5, "yellow": 0xffff00, "yellowgreen": 0x9acd32,
}

// ---------------------------------------------------------------------------
// Sizing and painting

// intrinsic returns the width/height attributes in CSS px (ok=false for each
// that's missing or a percentage) and the viewBox (minX, minY, w, h;
// hasVB=false if absent/invalid).
func (d *svgDoc) intrinsic() (w, h float32, wok, hok bool, vb [4]float32, hasVB bool) {
	if d == nil || d.root == nil {
		return
	}
	r := d.root
	if r.has(svgLW) && !r.len[svgLW].pct && r.len[svgLW].v >= 0 {
		w, wok = r.len[svgLW].v, true
	}
	if r.has(svgLH) && !r.len[svgLH].pct && r.len[svgLH].v >= 0 {
		h, hok = r.len[svgLH].v, true
	}
	return w, h, wok, hok, r.vb, r.hasVB
}

// svgDrawHook observes every paint (tests).
type svgDrawHook func(n *Node, stroke bool, c color.NRGBA, width float32)

const (
	svgMaxUse    = 16      // nested use depth
	svgBudget    = 1 << 14 // elements drawn per paint
	svgMaxFan    = 66      // points in one round join/cap polygon
	svgShapeSegs = 10
)

type svgCtx struct {
	ops      *op.Ops
	sty      func(*Node) *svgCSS
	mapColor func(color.NRGBA) color.NRGBA
	hook     svgDrawHook
	budget   int
	uses     [svgMaxUse]*svgEl
	nuse     int
	buf      [svgShapeSegs]svgSeg
}

// paint draws the doc into a box of size (px) at the current op origin.
func (d *svgDoc) paint(ops *op.Ops, size f32.Point, root svgPaint, sty func(*Node) *svgCSS, mapColor func(color.NRGBA) color.NRGBA) {
	d.paintWith(ops, size, root, sty, mapColor, nil)
}

func (d *svgDoc) paintWith(ops *op.Ops, size f32.Point, root svgPaint, sty func(*Node) *svgCSS, mapColor func(color.NRGBA) color.NRGBA, hook svgDrawHook) {
	if d == nil || d.root == nil || ops == nil || !(size.X > 0) || !(size.Y > 0) {
		return
	}
	e := d.root
	st := svgState{
		color:  root.Color,
		fill:   svgPaintVal{kind: svgPColor, c: root.Fill},
		stroke: svgPaintVal{kind: svgPColor, c: root.Stroke},
		sw:     root.StrokeWidth,
		fo:     1,
		so:     1,
		op:     svgClamp01(root.Opacity),
		miter:  4,
		vis:    true,
		vw:     size.X,
		vh:     size.Y,
		scale:  1,
	}
	if root.FillNone {
		st.fill = svgPaintVal{kind: svgPNone}
	}
	if root.StrokeNone {
		st.stroke = svgPaintVal{kind: svgPNone}
	}
	if st.sw < 0 {
		st.sw = 1
	}
	if e.pres.hidden {
		return
	}
	st.apply(&e.pres)
	elOp := d.rootOp
	if sty != nil {
		if s := sty(e.n); s != nil {
			if s.Hidden {
				return
			}
			st.applyCSS(s)
			if s.OpacitySet {
				elOp = 1 // CSS opacity beats the attribute and is already in root
			}
		}
	}
	st.op *= elOp
	if st.op <= 0 {
		return
	}
	c := svgCtx{ops: ops, sty: sty, mapColor: mapColor, hook: hook, budget: svgBudget}
	cl := clip.Rect{Max: image.Pt(int(math.Ceil(float64(size.X))), int(math.Ceil(float64(size.Y))))}.Push(ops)
	if e.hasXf {
		t := op.Affine(e.xf.aff()).Push(ops)
		st.scale *= e.xf.scale()
		c.viewport(e, 0, 0, size.X, size.Y, st, false)
		t.Pop()
	} else {
		c.viewport(e, 0, 0, size.X, size.Y, st, false)
	}
	cl.Pop()
}

// viewport draws e's children in the viewport (x, y, w, h) of the current
// user space, mapping e's viewBox into it.
func (c *svgCtx) viewport(e *svgEl, x, y, w, h float32, st svgState, clipIt bool) {
	if !(w > 0) || !(h > 0) {
		return
	}
	var cl clip.Stack
	if clipIt {
		var p clip.Path
		p.Begin(c.ops)
		p.MoveTo(f32.Point{X: x, Y: y})
		p.LineTo(f32.Point{X: x + w, Y: y})
		p.LineTo(f32.Point{X: x + w, Y: y + h})
		p.LineTo(f32.Point{X: x, Y: y + h})
		p.Close()
		cl = clip.Outline{Path: p.End()}.Op().Push(c.ops)
	}
	m := svgMat{1, 0, 0, 1, x, y}
	st.vw, st.vh = w, h
	if e.hasVB {
		m = svgViewBoxMat(e.vb, e.par, x, y, w, h)
		st.vw, st.vh = e.vb[2], e.vb[3]
	}
	st.scale *= m.scale()
	if m != svgIdent {
		t := op.Affine(m.aff()).Push(c.ops)
		c.drawKids(e, &st)
		t.Pop()
	} else {
		c.drawKids(e, &st)
	}
	if clipIt {
		cl.Pop()
	}
}

func (c *svgCtx) drawKids(e *svgEl, st *svgState) {
	for _, k := range e.kids {
		c.draw(k, *st, nil)
	}
}

// draw draws one element with the inherited state st; use is the <use>
// instantiating e, if any (symbols draw only through one).
func (c *svgCtx) draw(e *svgEl, st svgState, use *svgEl) {
	if c.budget <= 0 {
		return
	}
	c.budget--
	switch e.kind {
	case svgKDefs, svgKGrad:
		return
	case svgKSymbol:
		if use == nil {
			return
		}
	}
	if e.pres.hidden {
		return
	}
	elOp := float32(1)
	if e.pres.set&svgSetOpacity != 0 {
		elOp = e.pres.opacity
	}
	st.apply(&e.pres)
	if c.sty != nil {
		if s := c.sty(e.n); s != nil {
			if s.Hidden {
				return
			}
			st.applyCSS(s)
			if s.OpacitySet {
				elOp = svgClamp01(s.Opacity)
			}
		}
	}
	st.op *= elOp
	if !(st.op > 0) {
		return
	}
	var t op.TransformStack
	if e.hasXf {
		t = op.Affine(e.xf.aff()).Push(c.ops)
		st.scale *= e.xf.scale()
	}
	switch e.kind {
	case svgKGroup:
		c.drawKids(e, &st)
	case svgKSVG, svgKSymbol:
		var x, y float32
		wl, hl := svgLen{v: 100, pct: true}, svgLen{v: 100, pct: true}
		if e.kind == svgKSVG {
			if e.has(svgLX) {
				x = e.len[svgLX].px(st.vw)
			}
			if e.has(svgLY) {
				y = e.len[svgLY].px(st.vh)
			}
			if e.has(svgLW) {
				wl = e.len[svgLW]
			}
			if e.has(svgLH) {
				hl = e.len[svgLH]
			}
		}
		if use != nil {
			if use.has(svgLW) {
				wl = use.len[svgLW]
			}
			if use.has(svgLH) {
				hl = use.len[svgLH]
			}
		}
		c.viewport(e, x, y, wl.px(st.vw), hl.px(st.vh), st, true)
	case svgKUse:
		c.drawUse(e, st)
	default:
		c.drawShape(e, &st)
	}
	if e.hasXf {
		t.Pop()
	}
}

func (c *svgCtx) drawUse(u *svgEl, st svgState) {
	r := u.ref
	if r == nil || c.nuse >= len(c.uses) {
		return
	}
	for _, a := range c.uses[:c.nuse] {
		if a == r {
			return
		}
	}
	c.uses[c.nuse] = r
	c.nuse++
	var x, y float32
	if u.has(svgLX) {
		x = u.len[svgLX].px(st.vw)
	}
	if u.has(svgLY) {
		y = u.len[svgLY].px(st.vh)
	}
	if x != 0 || y != 0 {
		tt := op.Affine(f32.NewAffine2D(1, 0, x, 0, 1, y)).Push(c.ops)
		c.draw(r, st, u)
		tt.Pop()
	} else {
		c.draw(r, st, u)
	}
	c.nuse--
}

// shapeSegs returns e's geometry in user units.
func (c *svgCtx) shapeSegs(e *svgEl, st *svgState) []svgSeg {
	if e.kind == svgKPath {
		return e.segs
	}
	b := c.buf[:0]
	diag := float32(math.Sqrt(float64(st.vw*st.vw+st.vh*st.vh) / 2))
	lx := func(i int) float32 {
		if !e.has(i) {
			return 0
		}
		return e.len[i].px(st.vw)
	}
	ly := func(i int) float32 {
		if !e.has(i) {
			return 0
		}
		return e.len[i].px(st.vh)
	}
	const k = 0.5522847498 // circle cubic constant
	ellipse := func(cx, cy, rx, ry float32) []svgSeg {
		if !(rx > 0) || !(ry > 0) {
			return nil
		}
		kx, ky := rx*k, ry*k
		p := func(x, y float32) f32.Point { return f32.Point{X: x, Y: y} }
		b = append(b,
			svgSeg{op: 'M', p: [3]f32.Point{p(cx+rx, cy)}},
			svgSeg{op: 'C', p: [3]f32.Point{p(cx+rx, cy+ky), p(cx+kx, cy+ry), p(cx, cy+ry)}},
			svgSeg{op: 'C', p: [3]f32.Point{p(cx-kx, cy+ry), p(cx-rx, cy+ky), p(cx-rx, cy)}},
			svgSeg{op: 'C', p: [3]f32.Point{p(cx-rx, cy-ky), p(cx-kx, cy-ry), p(cx, cy-ry)}},
			svgSeg{op: 'C', p: [3]f32.Point{p(cx+kx, cy-ry), p(cx+rx, cy-ky), p(cx+rx, cy)}},
			svgSeg{op: 'Z'},
		)
		return b
	}
	switch e.kind {
	case svgKRect:
		x, y, w, h := lx(svgLX), ly(svgLY), lx(svgLW), ly(svgLH)
		if !(w > 0) || !(h > 0) {
			return nil
		}
		rx, ry := svgRectRadii(e, st.vw, st.vh, w, h)
		p := func(x, y float32) f32.Point { return f32.Point{X: x, Y: y} }
		if rx == 0 || ry == 0 {
			return append(b,
				svgSeg{op: 'M', p: [3]f32.Point{p(x, y)}},
				svgSeg{op: 'L', p: [3]f32.Point{p(x+w, y)}},
				svgSeg{op: 'L', p: [3]f32.Point{p(x+w, y+h)}},
				svgSeg{op: 'L', p: [3]f32.Point{p(x, y+h)}},
				svgSeg{op: 'Z'},
			)
		}
		kx, ky := rx*(1-k), ry*(1-k)
		r, bt := x+w, y+h
		return append(b,
			svgSeg{op: 'M', p: [3]f32.Point{p(x+rx, y)}},
			svgSeg{op: 'L', p: [3]f32.Point{p(r-rx, y)}},
			svgSeg{op: 'C', p: [3]f32.Point{p(r-kx, y), p(r, y+ky), p(r, y+ry)}},
			svgSeg{op: 'L', p: [3]f32.Point{p(r, bt-ry)}},
			svgSeg{op: 'C', p: [3]f32.Point{p(r, bt-ky), p(r-kx, bt), p(r-rx, bt)}},
			svgSeg{op: 'L', p: [3]f32.Point{p(x+rx, bt)}},
			svgSeg{op: 'C', p: [3]f32.Point{p(x+kx, bt), p(x, bt-ky), p(x, bt-ry)}},
			svgSeg{op: 'L', p: [3]f32.Point{p(x, y+ry)}},
			svgSeg{op: 'C', p: [3]f32.Point{p(x, y+ky), p(x+kx, y), p(x+rx, y)}},
			svgSeg{op: 'Z'},
		)
	case svgKCircle:
		var r float32
		if e.has(svgLW) {
			r = e.len[svgLW].px(diag)
		}
		return ellipse(lx(svgLX), ly(svgLY), r, r)
	case svgKEllipse:
		rx, ry := float32(-1), float32(-1)
		if e.has(svgLRX) {
			rx = e.len[svgLRX].px(st.vw)
		}
		if e.has(svgLRY) {
			ry = e.len[svgLRY].px(st.vh)
		}
		if rx < 0 { // SVG 2: auto takes the other radius
			rx = ry
		}
		if ry < 0 {
			ry = rx
		}
		return ellipse(lx(svgLX), ly(svgLY), rx, ry)
	case svgKLine:
		return append(b,
			svgSeg{op: 'M', p: [3]f32.Point{{X: lx(svgLX), Y: ly(svgLY)}}},
			svgSeg{op: 'L', p: [3]f32.Point{{X: lx(svgLW), Y: ly(svgLH)}}},
		)
	}
	return nil
}

// svgRectRadii applies the rect corner rules: a missing (or negative) rx or
// ry takes the other's value, then each is clamped to half the side.
func svgRectRadii(e *svgEl, vw, vh, w, h float32) (rx, ry float32) {
	hasX := e.has(svgLRX) && e.len[svgLRX].v >= 0
	hasY := e.has(svgLRY) && e.len[svgLRY].v >= 0
	if hasX {
		rx = e.len[svgLRX].px(vw)
	}
	if hasY {
		ry = e.len[svgLRY].px(vh)
	}
	switch {
	case hasX && !hasY:
		ry = rx
	case hasY && !hasX:
		rx = ry
	}
	rx = min(rx, w/2)
	ry = min(ry, h/2)
	return rx, ry
}

func (c *svgCtx) drawShape(e *svgEl, st *svgState) {
	if !st.vis || !(st.scale > 1e-6) {
		return
	}
	segs := c.shapeSegs(e, st)
	if len(segs) < 2 {
		return
	}
	if e.kind != svgKLine {
		if col, ok := c.resolve(&st.fill, st, st.fo*st.op); ok {
			fs := segs
			if st.rule == svgRuleEvenOdd && e.eoSegs != nil {
				fs = e.eoSegs
			}
			paint.FillShape(c.ops, col, clip.Outline{Path: c.pathSpec(fs)}.Op())
			if c.hook != nil {
				c.hook(e.n, false, col, 0)
			}
		}
	}
	if !(st.sw > 0) {
		return
	}
	col, ok := c.resolve(&st.stroke, st, st.so*st.op)
	if !ok {
		return
	}
	if st.cap == svgCapRound && st.join == svgJoinRound {
		paint.FillShape(c.ops, col, clip.Stroke{Path: c.pathSpec(segs), Width: st.sw}.Op())
	} else {
		var p clip.Path
		p.Begin(c.ops)
		var sk svgStroker
		sk.reset(&p, st.sw/2, st.cap, st.join, st.miter, 0.25/st.scale)
		for i := range segs {
			s := &segs[i]
			switch s.op {
			case 'M':
				sk.moveTo(s.p[0])
			case 'L':
				sk.lineTo(s.p[0], false)
			case 'C':
				sk.cubeTo(s.p[0], s.p[1], s.p[2])
			case 'Z':
				sk.close()
			}
		}
		sk.finish()
		spec := p.End()
		if sk.n == 0 {
			return
		}
		paint.FillShape(c.ops, col, clip.Outline{Path: spec}.Op())
	}
	if c.hook != nil {
		c.hook(e.n, true, col, st.sw)
	}
}

func (c *svgCtx) pathSpec(segs []svgSeg) clip.PathSpec {
	var p clip.Path
	p.Begin(c.ops)
	for i := range segs {
		s := &segs[i]
		switch s.op {
		case 'M':
			p.MoveTo(s.p[0])
		case 'L':
			p.LineTo(s.p[0])
		case 'C':
			p.CubeTo(s.p[0], s.p[1], s.p[2])
		case 'Z':
			p.Close()
		}
	}
	return p.End()
}

// resolve turns a paint into a color with alpha multiplied in.
func (c *svgCtx) resolve(pv *svgPaintVal, st *svgState, alpha float32) (color.NRGBA, bool) {
	var col color.NRGBA
	switch pv.kind {
	case svgPColor:
		col = pv.c
	case svgPCurrent:
		col = st.color
	case svgPURL:
		if pv.ref == nil {
			return col, false
		}
		var ok bool
		if col, ok = svgGradAvg(pv.ref, st.color); !ok {
			return col, false
		}
	default:
		return col, false
	}
	a := float32(col.A) * svgClamp01(alpha)
	if a < .5 {
		return col, false
	}
	col.A = uint8(a + .5)
	if c.mapColor != nil {
		col = c.mapColor(col)
		if col.A == 0 {
			return col, false
		}
	}
	return col, true
}

// svgGradAvg is the average color along a gradient's stops (premultiplied,
// the pads beyond the first and last stop included).
func svgGradAvg(g *svgEl, cur color.NRGBA) (color.NRGBA, bool) {
	if len(g.stops) == 0 {
		return color.NRGBA{}, false
	}
	var acc [4]float32
	stop := func(i int) [4]float32 {
		s := g.stops[i]
		c := s.c.c
		if s.c.kind == svgPCurrent {
			c = cur
		}
		a := float32(c.A) / 255 * s.op
		return [4]float32{float32(c.R) * a, float32(c.G) * a, float32(c.B) * a, a}
	}
	add := func(v [4]float32, w float32) {
		for k := range acc {
			acc[k] += v[k] * w
		}
	}
	n := len(g.stops)
	first, last := stop(0), stop(n-1)
	add(first, g.stops[0].off)
	for i := 0; i+1 < n; i++ {
		w := (g.stops[i+1].off - g.stops[i].off) / 2
		add(stop(i), w)
		add(stop(i+1), w)
	}
	add(last, 1-g.stops[n-1].off)
	if acc[3] <= 0 {
		return color.NRGBA{}, false
	}
	u := func(v float32) uint8 {
		v /= acc[3]
		if v > 255 {
			v = 255
		}
		return uint8(v + .5)
	}
	return color.NRGBA{R: u(acc[0]), G: u(acc[1]), B: u(acc[2]), A: svgByte(acc[3])}, true
}

// ---------------------------------------------------------------------------
// Stroker for the caps and joins clip.Stroke can't draw.

// svgStroker emits a stroke outline as consistently oriented polygons (one
// per segment, join and cap) into a clip.Path; filling it with the nonzero
// rule unions them into the stroke area.
type svgStroker struct {
	p          *clip.Path
	hw, tol    float32
	miter      float32
	cap, join  uint8
	start, cur f32.Point
	d0, dp     f32.Point // first and previous unit directions
	has        bool      // the subpath has a non-degenerate segment
	open       bool      // a subpath is in progress
	n          int       // polygons emitted
	pts        [svgMaxFan]f32.Point
}

func (s *svgStroker) reset(p *clip.Path, hw float32, cp, join uint8, miter, tol float32) {
	s.p, s.hw, s.cap, s.join, s.miter, s.tol = p, hw, cp, join, miter, tol
	if s.miter < 1 {
		s.miter = 4
	}
}

func (s *svgStroker) moveTo(p f32.Point) {
	s.finish()
	s.start, s.cur = p, p
	s.open = true
	s.has = false
}

func svgPerp(d f32.Point, hw float32) f32.Point { return f32.Point{X: -d.Y * hw, Y: d.X * hw} }

func (s *svgStroker) lineTo(p f32.Point, inCurve bool) {
	if !s.open {
		s.moveTo(s.cur)
	}
	v := p.Sub(s.cur)
	l := float32(math.Hypot(float64(v.X), float64(v.Y)))
	if !(l > 1e-6) {
		return
	}
	d := v.Mul(1 / l)
	if !s.has {
		s.d0, s.has = d, true
	} else {
		j := s.join
		if inCurve {
			j = svgJoinBevel
		}
		s.joinAt(s.cur, s.dp, d, j)
	}
	n := svgPerp(d, s.hw)
	a, b := s.cur, p
	s.pts[0], s.pts[1], s.pts[2], s.pts[3] = a.Add(n), b.Add(n), b.Sub(n), a.Sub(n)
	s.poly(s.pts[:4])
	s.dp, s.cur = d, p
}

func (s *svgStroker) cubeTo(c1, c2, p3 f32.Point) {
	p0 := s.cur
	d1 := p0.Sub(c1.Mul(2)).Add(c2)
	d2 := c1.Sub(c2.Mul(2)).Add(p3)
	dd := max(math.Hypot(float64(d1.X), float64(d1.Y)), math.Hypot(float64(d2.X), float64(d2.Y)))
	n := int(math.Ceil(math.Sqrt(0.75 * dd / float64(s.tol))))
	n = max(1, min(n, 128))
	inner := false // joins inside the curve are bevels
	for i := 1; i <= n; i++ {
		q := p3
		if i < n {
			q = svgCubicAt(p0, c1, c2, p3, float32(i)/float32(n))
		}
		before := s.cur
		s.lineTo(q, inner)
		if s.cur != before {
			inner = true
		}
	}
}

func (s *svgStroker) close() {
	if !s.open {
		return
	}
	if s.cur != s.start {
		s.lineTo(s.start, false)
	}
	if s.has {
		s.joinAt(s.start, s.dp, s.d0, s.join)
	} else {
		s.dot(s.start)
	}
	s.open, s.has = false, false
	s.cur = s.start
}

// finish caps the open subpath in progress.
func (s *svgStroker) finish() {
	if !s.open {
		return
	}
	if s.has {
		s.capAt(s.start, s.d0.Mul(-1))
		s.capAt(s.cur, s.dp)
	} else {
		s.dot(s.start)
	}
	s.open, s.has = false, false
}

// dot draws a zero-length subpath: nothing for butt caps.
func (s *svgStroker) dot(p f32.Point) {
	switch s.cap {
	case svgCapRound:
		s.fan(p, f32.Point{X: s.hw}, 2*math.Pi)
	case svgCapSquare:
		h := s.hw
		s.pts[0] = p.Add(f32.Point{X: -h, Y: -h})
		s.pts[1] = p.Add(f32.Point{X: h, Y: -h})
		s.pts[2] = p.Add(f32.Point{X: h, Y: h})
		s.pts[3] = p.Add(f32.Point{X: -h, Y: h})
		s.poly(s.pts[:4])
	}
}

// capAt caps an end at p facing the outward unit direction d.
func (s *svgStroker) capAt(p, d f32.Point) {
	n := svgPerp(d, s.hw)
	switch s.cap {
	case svgCapSquare:
		e := d.Mul(s.hw)
		s.pts[0], s.pts[1], s.pts[2], s.pts[3] = p.Add(n), p.Add(n).Add(e), p.Sub(n).Add(e), p.Sub(n)
		s.poly(s.pts[:4])
	case svgCapRound:
		// From n through d to -n: rotating n by -π/2 gives d.
		s.fan(p, n, -math.Pi)
	}
}

// joinAt fills the outer wedge between incoming d0 and outgoing d1 at p.
func (s *svgStroker) joinAt(p, d0, d1 f32.Point, join uint8) {
	cross := d0.X*d1.Y - d0.Y*d1.X
	dot := d0.X*d1.X + d0.Y*d1.Y
	if cross > -1e-6 && cross < 1e-6 && dot > 0 {
		return // straight on
	}
	sgn := float32(1)
	if cross > 0 {
		sgn = -1
	}
	a := svgPerp(d0, s.hw*sgn)
	b := svgPerp(d1, s.hw*sgn)
	switch join {
	case svgJoinRound:
		ang := math.Atan2(float64(a.X*b.Y-a.Y*b.X), float64(a.X*b.X+a.Y*b.Y))
		if cross > -1e-6 && cross < 1e-6 {
			// U-turn: go around through d0 (a rotated by -π/2 when sgn=1).
			ang = -math.Pi * float64(sgn)
		}
		s.fan(p, a, ang)
		return
	case svgJoinMiter:
		c2 := (1 + dot) / 2 // cos²(α/2), α the turn angle
		if c2 > 1e-9 {
			ratio := 1 / float32(math.Sqrt(float64(c2)))
			if ratio <= s.miter {
				m := a.Add(b)
				ml := float32(math.Hypot(float64(m.X), float64(m.Y)))
				if ml > 1e-9 {
					tip := p.Add(m.Mul(s.hw * ratio / ml))
					s.pts[0], s.pts[1], s.pts[2], s.pts[3] = p, p.Add(a), tip, p.Add(b)
					s.poly(s.pts[:4])
					return
				}
			}
		}
	}
	s.pts[0], s.pts[1], s.pts[2] = p, p.Add(a), p.Add(b)
	s.poly(s.pts[:3])
}

// fan emits the pie slice at p from radius vector a turning ang radians.
func (s *svgStroker) fan(p, a f32.Point, ang float64) {
	step := math.Pi / 2
	if hw := float64(s.hw); float64(s.tol) < hw {
		step = 2 * math.Acos(1-float64(s.tol)/hw)
	}
	n := int(math.Ceil(math.Abs(ang) / step))
	full := math.Abs(ang) >= 2*math.Pi-1e-9
	limit := len(s.pts) - 2
	n = max(2, min(n, limit))
	if full {
		n = max(n, 8)
	}
	sn, cs := math.Sincos(ang / float64(n))
	sf, cf := float32(sn), float32(cs)
	k := 0
	if !full {
		s.pts[k] = p
		k++
	}
	v := a
	for i := 0; i <= n; i++ {
		if full && i == n {
			break
		}
		s.pts[k] = p.Add(v)
		k++
		v = f32.Point{X: v.X*cf - v.Y*sf, Y: v.X*sf + v.Y*cf}
	}
	s.poly(s.pts[:k])
}

// poly emits a closed polygon with positive orientation.
func (s *svgStroker) poly(pts []f32.Point) {
	var area float32
	for i := range pts {
		q := pts[(i+1)%len(pts)]
		area += pts[i].X*q.Y - q.X*pts[i].Y
	}
	if area == 0 || math.IsNaN(float64(area)) {
		return
	}
	p := s.p
	if area > 0 {
		p.MoveTo(pts[0])
		for _, q := range pts[1:] {
			p.LineTo(q)
		}
	} else {
		p.MoveTo(pts[len(pts)-1])
		for i := len(pts) - 2; i >= 0; i-- {
			p.LineTo(pts[i])
		}
	}
	p.Close()
	s.n++
}
