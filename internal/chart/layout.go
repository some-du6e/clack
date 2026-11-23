package chart

import (
	"image/color"
	"math"
	"unicode/utf16"
)

// Weight is a font weight the chart measures text in.
type Weight uint8

const (
	Regular Weight = iota // 400: axis labels, the tooltip's names
	Black                 // 900: the tooltip's values
)

// Measure is the platform's measureText: s's width in CSS px at 13px Lato
// (the font every label and the tooltip use: "Lato, Slack-Lato, appleLogo,
// sans-serif" at fontSize 13).
type Measure func(s string, w Weight) float64

// Align is a label's horizontal alignment on its anchor.
type Align uint8

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

// Rect is a box in chart px.
type Rect struct{ X, Y, W, H float64 }

// Contains reports whether (x, y) is in r (edges included).
func (r Rect) Contains(x, y float64) bool {
	return x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H
}

// Seg is a line from (X1, Y1) to (X2, Y2).
type Seg struct{ X1, Y1, X2, Y2 float64 }

// Label is an axis label: Text drawn on its anchor (X, Y), aligned by
// Align, with its line box (18px) centred on Y (Middle) or starting at Y,
// turned by Rotate radians about the anchor (zrender's: positive turns
// counter-clockwise on screen).
type Label struct {
	Text   string
	X, Y   float64
	Align  Align
	Middle bool
	Rotate float64
}

// BarItem is one bar: the rect from (X, Y) by (W, H), H negative upward
// (zrender's shape: its first two radii are at Y, the baseline for a
// negative value).
type BarItem struct {
	X, Y, W, H  float64
	Series, Cat int // Series: index into Spec.Series
	Color       color.NRGBA
	Value       float64
}

// SymbolItem is a line or area point's circle (6px).
type SymbolItem struct {
	X, Y        float64
	Series, Cat int
	Shown       bool // drawn without hover (an isolated point)
	Color       color.NRGBA
}

// PathItem is a series line or area fill.
type PathItem struct {
	Path   []Cmd
	Series int
	Color  color.NRGBA
}

// SectorItem is a pie segment (or the empty circle).
type SectorItem struct {
	CX, CY, R  float64
	Start, End float64 // radians, clockwise from Start (zrender's)
	Index      int     // its segment (-1: the empty circle)
	Fill       color.NRGBA
	StrokeW    float64 // the border (Palette.Border), 2px; 0: none
}

// Layout is a chart laid out in a W×H box (chart px: CSS px).
type Layout struct {
	W, H    float64
	Kind    Kind
	Grid    Rect  // the plot area (after the outer bounds)
	Splits  []Seg // the value axis' split lines, snapped
	AxisLn  *Seg  // the category axis line, snapped (nil: none)
	Labels  []Label
	Bars    []BarItem
	Areas   []PathItem
	Lines   []PathItem
	Symbols []SymbolItem
	Sectors []SectorItem

	spec    *Spec
	measure Measure
	catX    []float64 // each category's coordinate
	band    float64   // a bar band's width (0 off bars)
	ys      valueScale
}

// Lay lays spec out in a w×h box.
func Lay(spec *Spec, w, h float64, m Measure) *Layout {
	if spec == nil {
		return nil
	}
	if spec.Kind == Pie {
		return layPie(spec, w, h, m)
	}
	return layCartesian(spec, w, h, m)
}

// ---- the cartesian chart ----

type axisLabel struct {
	Label
	rect  Rect
	cat   int     // the category (x labels)
	off   bool    // offInterval (x labels)
	value float64 // the tick (y labels)
	show  bool
}

const labelLineH = 18 // axisLabel.lineHeight

func layCartesian(spec *Spec, W, H float64, m Measure) *Layout {
	l := &Layout{W: W, H: H, Kind: spec.Kind, spec: spec, measure: m}
	cats := spec.Categories
	n := len(cats)
	rot := spec.Rotate
	left, top, bottom := 74.0, 8.0, 26.0 // getCartesianLayout
	if rot {
		left, bottom = 26, 8
	}
	isBar := spec.Kind == Bar
	var all []float64
	for _, s := range spec.Series {
		all = append(all, s.Values...)
	}
	l.ys = newValueScale(all, isBar)
	raw := Rect{left, top, W - left, H - top - bottom}
	outer := Rect{0, 0, W, H}
	if rot { // containLabel without LegacyGridContainLabel: the labels inside the grid's own rect
		outer = raw
	}
	clamp := [2]float64{raw.W * 0.25, raw.H * 0.25} // OUTER_BOUNDS_CLAMP_DEFAULT

	// estimate, shrink to the outer bounds, determine
	g := raw
	{
		xl, yl := l.xLabels(g), l.yLabels(g)
		var margin [4]float64 // top right bottom left
		fill := func(r Rect, xy int, prop float64) {
			var o1, o2 float64
			if xy == 0 {
				o1, o2 = outer.X-r.X, (r.X+r.W)-(outer.X+outer.W)
			} else {
				o1, o2 = outer.Y-r.Y, (r.Y+r.H)-(outer.Y+outer.H)
			}
			ap := func(o, p float64) float64 {
				if o > 0 && !math.IsNaN(p) && p > 1e-4 {
					return o / p
				}
				return o
			}
			o1, o2 = ap(o1, 1-prop), ap(o2, prop)
			mi, ma := 3, 1
			if xy == 1 {
				mi, ma = 0, 2
			}
			margin[mi] = math.Max(margin[mi], o1)
			margin[ma] = math.Max(margin[ma], o2)
		}
		for _, a := range xl {
			if !a.show {
				continue
			}
			prop := 0.0
			if n > 1 {
				prop = float64(a.cat) / float64(n-1) // the ordinal scale's normalize
			}
			fill(a.rect, 0, prop)
			fill(a.rect, 1, math.NaN())
		}
		for _, a := range yl {
			if !a.show {
				continue
			}
			prop := 1 - (a.value-l.ys.ext[0])/(l.ys.ext[1]-l.ys.ext[0])
			fill(a.rect, 1, prop)
			fill(a.rect, 0, math.NaN())
		}
		fill(g, 0, math.NaN())
		fill(g, 1, math.NaN())
		g = shrink(g, margin, clamp)
	}
	l.Grid = g
	xl, yl := l.xLabels(g), l.yLabels(g)
	l.catX = make([]float64, n)
	for i := range cats {
		l.catX[i] = l.xCoord(g, i)
	}
	if isBar && n > 0 {
		l.band = g.W / float64(n)
	}

	// split lines at the value ticks, the category axis line on zero
	if !l.ys.blank {
		for _, v := range l.ys.ticks {
			y := subPixel(l.yCoord(g, v))
			l.Splits = append(l.Splits, Seg{g.X, y, g.X + g.W, y})
		}
	}
	zy := g.Y + g.H
	if l.ys.ext[0] <= 0 && 0 <= l.ys.ext[1] {
		zy = l.yCoord(g, 0)
	}
	ay := subPixel(zy)
	l.AxisLn = &Seg{g.X, ay, g.X + g.W, ay}
	for _, a := range yl {
		if a.show {
			l.Labels = append(l.Labels, a.Label)
		}
	}
	for _, a := range xl {
		if a.show {
			l.Labels = append(l.Labels, a.Label)
		}
	}

	ns := len(spec.Series)
	if isBar {
		if n == 0 {
			return l
		}
		band := g.W / float64(n)
		gap := math.Max(35-float64(ns)*4, 15) / 100 // barCategoryGap's default
		const barGap = 0.1                          // defaultBarGap
		w := math.Max((band-band*gap)/(float64(ns)+float64(ns-1)*barGap), 0)
		sum := 0.0
		for i := 0; i < ns; i++ {
			sum += w * (1 + barGap)
		}
		if ns > 0 {
			sum -= w * barGap
		}
		base := l.yCoord(g, 0)
		for si, s := range spec.Series {
			off := -sum/2 + float64(si)*w*(1+barGap)
			for j, v := range s.Values {
				if math.IsNaN(v) {
					continue
				}
				l.Bars = append(l.Bars, BarItem{X: l.catX[j] + off, Y: base, W: w, H: l.yCoord(g, v) - base,
					Series: si, Cat: j, Color: s.Color, Value: v})
			}
		}
		return l
	}
	pts := func(s *Series) []float64 {
		p := make([]float64, 0, 2*n)
		for j, v := range s.Values {
			y := math.NaN()
			if !math.IsNaN(v) {
				y = l.yCoord(g, v)
			}
			p = append(p, l.catX[j], y)
		}
		return p
	}
	if spec.Kind == Area {
		zv := math.Max(l.ys.ext[0], math.Min(l.ys.ext[1], 0))
		zy := l.yCoord(g, zv)
		for si := range spec.Series {
			s := &spec.Series[si]
			p := pts(s)
			base := make([]float64, len(p))
			for j := 0; j < len(p); j += 2 {
				base[j], base[j+1] = p[j], zy
			}
			l.Areas = append(l.Areas, PathItem{Path: polygonPath(p, base, 0.5, 0), Series: si, Color: s.Area})
		}
	}
	for si := range spec.Series {
		s := &spec.Series[si]
		l.Lines = append(l.Lines, PathItem{Path: polylinePath(pts(s), 0.5), Series: si, Color: s.Color})
	}
	for si := range spec.Series {
		s := &spec.Series[si]
		for j, v := range s.Values {
			if math.IsNaN(v) {
				continue
			}
			l.Symbols = append(l.Symbols, SymbolItem{X: l.catX[j], Y: l.yCoord(g, v), Series: si, Cat: j,
				Shown: j < len(s.Isolated) && s.Isolated[j], Color: s.Color})
		}
	}
	return l
}

// xCoord is category i's coordinate: a band's centre on bars
// (boundaryGap), else [0, n-1] across the grid.
func (l *Layout) xCoord(g Rect, i int) float64 {
	n := len(l.spec.Categories)
	if l.spec.Kind == Bar {
		band := g.W / float64(n)
		return g.X + band*float64(i) + band/2
	}
	if n > 1 {
		return g.X + g.W*float64(i)/float64(n-1)
	}
	return g.X + g.W/2
}

func (l *Layout) yCoord(g Rect, v float64) float64 {
	e := l.ys.ext
	if e[1] == e[0] { // scaleMapper's normalize: 0.5 for a zero span
		return g.Y + g.H/2
	}
	return g.Y + g.H - (v-e[0])/(e[1]-e[0])*g.H
}

// xLabels is the category axis' labels on grid g: every one under
// rotation (interval 0, truncated to 75px), else calculateCategoryInterval's
// auto interval, then fixMinMaxLabelShow and hideOverlap.
func (l *Layout) xLabels(g Rect) []axisLabel {
	cats := l.spec.Categories
	n := len(cats)
	if n == 0 {
		return nil
	}
	m := l.measure
	rot := l.spec.Rotate
	interval := 0
	if !rot && n > 1 {
		unit := math.Abs(l.xCoord(g, 1) - l.xCoord(g, 0))
		step := 1
		if n > 40 {
			step = max(1, n/40)
		}
		// the widest label (130%, at least 7px) against a category's span;
		// its height divides by span * sin(0) on this unrotated axis, which
		// is Infinity
		maxW := 0.0
		for i := 0; i < n; i += step {
			maxW = math.Max(maxW, math.Max(m(cats[i], Regular)*1.3, 7))
		}
		dw := maxW / unit
		switch {
		case math.IsNaN(dw), dw >= float64(n):
			interval = n // as good as Infinity: the first label and the last (off its interval)
		default:
			interval = max(0, int(math.Floor(dw)))
		}
	}
	step := interval + 1
	type tick struct {
		v   int
		off bool
	}
	var ticks []tick
	t := 0
	for ; t <= n-1; t += step {
		ticks = append(ticks, tick{t, false})
	}
	if t-step != n-1 {
		ticks = append(ticks, tick{n - 1, true})
	}
	ay := g.Y + g.H + 8 // the raw position: labelOffset undoes onZero
	out := make([]axisLabel, 0, len(ticks))
	for _, tk := range ticks {
		text := cats[tk.v]
		ax := l.xCoord(g, tk.v)
		a := axisLabel{cat: tk.v, off: tk.off, show: true}
		if rot {
			text = truncate(text, 75, m)
			// style.width 75 sizes the box, whatever the text's width
			a.Label = Label{Text: text, X: ax, Y: ay, Align: AlignRight, Middle: true, Rotate: math.Pi / 4}
			a.rect = rotatedRect(lineRect(75, AlignRight, true), math.Pi/4, ax, ay)
		} else {
			r := lineRect(m(text, Regular), AlignCenter, false)
			a.Label = Label{Text: text, X: ax, Y: ay, Align: AlignCenter}
			a.rect = Rect{r.X + ax, r.Y + ay, r.W, r.H}
		}
		out = append(out, a)
	}
	if !rot {
		fixMinMax(out, true)
		hideOverlap(out)
	}
	return out
}

// yLabels is the value axis' labels on grid g.
func (l *Layout) yLabels(g Rect) []axisLabel {
	if l.ys.blank {
		return nil
	}
	out := make([]axisLabel, 0, len(l.ys.ticks))
	for _, v := range l.ys.ticks {
		text := FormatYAxisTick(v)
		ax, ay := g.X-8, l.yCoord(g, v)
		r := lineRect(l.measure(text, Regular), AlignRight, true)
		out = append(out, axisLabel{Label: Label{Text: text, X: ax, Y: ay, Align: AlignRight, Middle: true},
			rect: Rect{r.X + ax, r.Y + ay, r.W, r.H}, value: v, show: true})
	}
	fixMinMax(out, false)
	return out
}

// lineRect is a label's box on its anchor: its 18px line box, then the
// axis label's textMargin [0, 3] (3px left and right).
func lineRect(w float64, al Align, middle bool) Rect {
	x := 0.0
	switch al {
	case AlignRight:
		x = -w
	case AlignCenter:
		x = -w / 2
	}
	y := 0.0
	if middle {
		y = -labelLineH / 2
	}
	return Rect{x - 3, y, w + 6, labelLineH}
}

// rotatedRect is r turned by rot (zrender's matrix [cos, -sin, sin, cos])
// about the anchor (tx, ty): its axis-aligned bounds.
func rotatedRect(r Rect, rot, tx, ty float64) Rect {
	c, s := math.Cos(rot), math.Sin(rot)
	pts := [4][2]float64{{r.X, r.Y}, {r.X + r.W, r.Y}, {r.X + r.W, r.Y + r.H}, {r.X, r.Y + r.H}}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range pts {
		X := c*p[0] + s*p[1] + tx
		Y := -s*p[0] + c*p[1] + ty
		x0, y0, x1, y1 = math.Min(x0, X), math.Min(y0, Y), math.Max(x1, X), math.Max(y1, Y)
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// intersects is BoundingRect.intersect with a touch threshold t.
func intersects(a, b Rect, t float64) bool {
	ax0, ax1, ay0, ay1 := a.X+t, a.X+a.W-t, a.Y+t, a.Y+a.H-t
	bx0, bx1, by0, by1 := b.X+t, b.X+b.W-t, b.Y+t, b.Y+b.H-t
	if ax0 > ax1 || ay0 > ay1 || bx0 > bx1 || by0 > by1 {
		return false
	}
	return !(ax1 < bx0 || bx1 < ax0 || ay1 < by0 || by1 < ay0)
}

// fixMinMax is fixMinMaxLabelShow with showMinLabel/showMaxLabel unset:
// an ordinal axis drops an outermost label off its interval, and an
// outermost label that touches its neighbour goes.
func fixMinMax(ls []axisLabel, ordinal bool) {
	n := len(ls)
	if n < 2 {
		if n == 1 && ordinal && ls[0].off {
			ls[0].show = false
		}
		return
	}
	deal := func(o, in int) {
		if ordinal && ls[o].off {
			ls[o].show = false
			return
		}
		if !ls[o].show || !ls[in].show {
			return
		}
		if intersects(ls[o].rect, ls[in].rect, 0.1) {
			ls[o].show = false
		}
	}
	deal(0, 1)
	deal(n-1, n-2)
}

// hideOverlap hides a label that overlaps one already shown, in order.
func hideOverlap(ls []axisLabel) {
	var shown []int
	for i := range ls {
		if !ls[i].show {
			continue
		}
		over := false
		for _, j := range shown {
			if intersects(ls[i].rect, ls[j].rect, 0.05) {
				over = true
				break
			}
		}
		if over {
			ls[i].show = false
		} else {
			shown = append(shown, i)
		}
	}
}

// shrink is expandOrShrinkRect(rect, margin, shrink, noNegative, clamp).
func shrink(g Rect, margin [4]float64, clamp [2]float64) Rect {
	r := g
	var d [4]float64
	for i, m := range margin {
		d[i] = -math.Max(0, m)
	}
	dim := func(pos, size *float64, lt, rb int, minSize float64) {
		sum := d[rb] + d[lt]
		old := *size
		*size += sum
		minSize = math.Max(0, math.Min(minSize, old))
		if *size < minSize {
			*size = minSize
			switch {
			case d[lt] >= 0:
				*pos -= d[lt]
			case d[rb] >= 0:
				*pos += old + d[rb]
			case math.Abs(sum) > 1e-8:
				*pos += (old - minSize) * d[lt] / sum
			}
		} else {
			*pos -= d[lt]
		}
	}
	dim(&r.X, &r.W, 3, 1, clamp[0])
	dim(&r.Y, &r.H, 0, 2, clamp[1])
	return r
}

// subPixel is subPixelOptimize for a 1px line: onto the half pixel.
func subPixel(p float64) float64 {
	d := jsRound(p * 2)
	if int64(d+1)%2 == 0 {
		return d / 2
	}
	return (d + 1) / 2
}

// truncate is zrender's truncateText at containerWidth w with "..." (two
// iterations, the first estimating from character widths: ASCII ones
// measured alone, any other as wide as U+56FD), in UTF-16 units as
// String.prototype.substr counts them.
func truncate(text string, w float64, m Measure) string {
	if w == 0 {
		return ""
	}
	wide := m("\u56fd", Regular)
	charW := func(c uint16) float64 {
		if c <= 127 {
			return m(string(rune(c)), Regular)
		}
		return wide
	}
	container := math.Max(0, w-1)
	ell, ellW := "...", m("...", Regular)
	if ellW > container {
		ell, ellW = "", 0
	}
	content := container - ellW
	units := utf16.Encode([]rune(text))
	line := units
	lw := m(text, Regular)
	if lw <= container {
		return text
	}
	for j := 0; ; j++ {
		if lw <= content || j >= 2 {
			break
		}
		var sub int
		if j == 0 {
			width, i := 0.0, 0
			for ; i < len(line) && width < content; i++ {
				width += charW(line[i])
			}
			sub = i
		} else if lw > 0 {
			sub = int(math.Floor(float64(len(line)) * content / lw))
		}
		line = line[:min(sub, len(line))]
		lw = m(string(utf16.Decode(line)), Regular)
	}
	return string(utf16.Decode(line)) + ell
}

// ---- the pie ----

func layPie(spec *Spec, W, H float64, _ Measure) *Layout {
	l := &Layout{W: W, H: H, Kind: Pie, spec: spec}
	cx, cy := W/2, H/2
	r := math.Min(W, H) / 2 * 0.8 // radius "80%"
	// negativeDataFilter: a negative value isn't data (its colour stays
	// its own); NaN values are data that draws nothing
	valid, count := 0, 0
	sum := 0.0
	for _, s := range spec.Segments {
		if !math.IsNaN(s.Value) && s.Value < 0 {
			continue
		}
		count++
		if !math.IsNaN(s.Value) {
			valid++
			sum += s.Value
		}
	}
	start := 3 * math.Pi / 2 // startAngle 90, normalized
	if count == 0 {
		// showEmptyCircle: the whole circle in emptyCircleStyle (lightgray)
		l.Sectors = append(l.Sectors, SectorItem{CX: cx, CY: cy, R: r, Start: start, End: start + 2*math.Pi, Index: -1,
			Fill: color.NRGBA{0xd3, 0xd3, 0xd3, 0xff}})
		return l
	}
	unit := 2 * math.Pi
	if sum != 0 {
		unit /= sum
	} else {
		unit /= float64(valid)
	}
	cur := start
	for i, s := range spec.Segments {
		if !math.IsNaN(s.Value) && s.Value < 0 {
			continue
		}
		if math.IsNaN(s.Value) {
			continue // a sector with no angles
		}
		a := s.Value * unit
		if sum == 0 { // stillShowZeroSum
			a = unit
		}
		l.Sectors = append(l.Sectors, SectorItem{CX: cx, CY: cy, R: r, Start: cur, End: cur + a, Index: i, Fill: s.Color, StrokeW: 2})
		cur += a
	}
	return l
}
