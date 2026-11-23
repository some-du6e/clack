package richview

// A data_visualization block: Slack's DataVisualizationBlock (CDN build
// 9f450b8ad3db, canvas-data-visualization-block's v4Jz and mhRP) in its
// legacy layout (bk_refresh_data_visualization_size_variants off), its
// chart ECharts 6.1.0's (package chart), from canvas-data-visualization-
// block.95fde6ac37c02b65.min.css:
//
//   - .scrollContainer: margin 8px 0 (the block's, blocks.go); the wrapper
//     .legacyWrapper is 400–624px wide (narrower panes clip it here; Slack
//     scrolls it sideways);
//   - Slack Kit's entity card (aJif: expanded, full width, auto height):
//     1px otl-ter border, radius 12, base-pry, overflow hidden;
//   - .content: a column, 12px padding, 8px apart: the title (an h3:
//     Slack Kit's base 18px/1.33334, weight 900), then the plot area;
//   - .chartArea: 100% × 360px, the chart's box; a cartesian chart's
//     .yAxisLabel (13px bold content-pry, -90°, centred in an 18px column
//     between the grid's raw top and bottom insets) is inside it, its
//     .xAxisLabel under it (padding 8px 0 0 the grid's raw left inset,
//     centred);
//   - .legend: padding-top 8px, the same left inset (a pie's 0), centred
//     rows 8px apart of a 10px circle in the series' colour, 8px, the
//     name (13px/18px content-pry); the legacy layout always draws it;
//   - .chartActionsGroup: a c-message_actions__group at top 16px, right
//     16px over the card, shown while the wrapper is hovered: More actions
//     (ellipsis-vertical-filled, MessageActionButton smedium).
//
// A render that throws draws the surface's errorFallback instead: in a
// message MessageErrorFallback (AR99, .p-top_level_error_element), in a
// view (FillBlocks: modals, App Home) nothing.
//
// Left out: ECharts' animations (the bars' growth, the lines' reveal, the
// tooltip's slide and fade), the sideways scroll under 400px, and the
// group staying shown while its menu is open (as FileActions).

import (
	"image"
	"image/color"
	"math"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/chart"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
)

var nsBlockKit = i18n.NS("block_kit")

// ChartActionHandler is optional on a Handler: a data_visualization
// block's More actions button was clicked (btn in window coordinates).
type ChartActionHandler interface {
	ChartAction(v *richtext.DataViz, btn image.Rectangle)
}

// chartPrim is a block's chart: its layout (chart px, CSS px) drawn in r.
type chartPrim struct {
	r     image.Rectangle // the chart's box (view px)
	lay   *chart.Layout
	scale float32 // view px per chart px
	pal   chart.Palette
	reg   faceKey // 13px Lato: the axis labels, the tooltip
	black faceKey // 13px Lato Black: the tooltip's values
	bold  faceKey // 13px Lato Bold: the y axis label
	// the y axis label and its column (chart px): .yAxisLabel
	yLabel     string
	yTop, yBot float64
	textCol    color.NRGBA // content-pry
	pointerCol color.NRGBA // the axis pointer's line
	label      string      // the chart's aria-label (role img)
	// card is the entity card round the chart and its radius: overflow
	// hidden, so the tooltip (ECharts' html tooltip, inside the chart's
	// div) is cut at its edges
	card    image.Rectangle
	cardRad int
}

// chartPalette is useChartTokens in the layout's theme.
func (l *lay) chartPalette() chart.Palette {
	var p chart.Palette
	for i := range p.Series {
		p.Series[i] = l.tok(chart.PaletteTokens[0][i])
		p.Area[i] = l.tok(chart.PaletteTokens[1][i])
	}
	p.AxisTick = l.tok("content-ter")
	p.AxisLine = l.tok("otl-ter")
	p.Border = l.tok("base-pry")
	p.Text = l.tok("content-pry")
	return p
}

// chartMeasure is measureText for the chart: 13px Lato in chart px.
func (l *lay) chartMeasure(reg, black faceKey) chart.Measure {
	return func(s string, w chart.Weight) float64 {
		k := reg
		if w == chart.Black {
			k = black
		}
		return float64(l.measure(s, k)) / float64(l.scale)
	}
}

// dataViz lays out a data_visualization block.
func (l *lay) dataViz(b *richtext.Block, x, y, w int) int {
	dv := b.DataViz
	if dv == nil {
		return 0
	}
	if dv.Err {
		if l.opts.FillBlocks {
			return 0 // a view passes no errorFallback
		}
		return l.topLevelError(x, y, w)
	}
	ww := min(max(w, l.dp(400)), l.dp(624))
	bw := max(1, l.dp(1))
	pad := l.dp(12)
	box0 := len(l.d.boxes)
	ix, iw := x+bw+pad, ww-2*(bw+pad)
	yy := y + bw + pad
	pry := l.tok("content-pry")
	if dv.Title != "" { // an empty h3 has no line box
		fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Black}, size: l.sp(18), lineH: l.dp(24), col: pry, emoji: l.dp(18)}
		l.plainAdd("\n")
		h, _ := l.plainFlow(dv.Title, ix, yy, iw, &fs, -1)
		yy += h
	}
	pal := l.chartPalette()
	if spec := chart.Build(&dv.Chart, &pal); spec != nil {
		yy += l.dp(8) // .content's gap
		reg := l.faceFor(font.Font{Typeface: l.st.Face}, l.sp(13), 0)
		black := l.faceFor(font.Font{Typeface: l.st.Face, Weight: font.Black}, l.sp(13), 0)
		bold := l.faceFor(font.Font{Typeface: l.st.Face, Weight: font.Bold}, l.sp(13), 0)
		ch := l.dp(360)
		lay := chart.Lay(spec, float64(iw)/float64(l.scale), 360, l.chartMeasure(reg, black))
		cp := chartPrim{r: image.Rect(ix, yy, ix+iw, yy+ch), lay: lay, scale: l.scale, pal: pal,
			reg: reg, black: black, bold: bold, textCol: pry, label: dv.AriaLabel, pointerCol: pal.AxisTick}
		if spec.Kind == chart.Bar {
			cp.pointerCol = chart.EChartsBorder // the axis pointer's default line (Slack sets none on bars)
		}
		cartesian := spec.Kind != chart.Pie
		left := 0.0
		if cartesian {
			// getCartesianLayout's insets: the plot area's CSS variables
			top, bottom := 8.0, 26.0
			left = 74
			if spec.Rotate {
				left, bottom = 26, 8
			}
			cp.yLabel, cp.yTop, cp.yBot = dv.Chart.YLabel, top, 360-bottom
		}
		l.d.charts = append(l.d.charts, cp)
		yy += ch
		inset := l.dp(float32(left))
		if cartesian {
			yy += l.dp(8) // .xAxisLabel's padding-top
			if strings.TrimSpace(dv.Chart.XLabel) != "" {
				fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: l.sp(13), lineH: l.dp(18), col: pry, emoji: l.dp(13)}
				l.plainAdd("\n")
				yy += l.centredFlow(dv.Chart.XLabel, ix+inset, yy, iw-inset, &fs)
			}
		}
		yy += l.dp(8) // .legend's padding-top
		yy += l.legend(dv.Chart.Legend, &pal, ix+inset, yy, iw-inset)
	}
	yy += pad + bw
	h := yy - y
	frame := box{kind: bBorder, r: image.Rect(x, y, x+ww, y+h), radius: l.dp(12), fill: l.tok("base-pry"), stroke: l.tok("otl-ter"), bw: bw, target: -1}
	if n := len(l.d.charts); n > 0 && l.d.charts[n-1].r.In(frame.r) {
		l.d.charts[n-1].card, l.d.charts[n-1].cardRad = frame.r, frame.radius
	}
	l.d.boxes = append(l.d.boxes, box{})
	copy(l.d.boxes[box0+1:], l.d.boxes[box0:])
	l.d.boxes[box0] = frame
	if dv.Chart.Supported && !l.opts.NoHoverCards && !l.opts.Canvas {
		l.d.bars = append(l.d.bars, fileBar{card: image.Rect(x, y, x+ww, y+h), viz: dv, inset: 16, group: true,
			btns: []barBtn{{act: FileMore, w: 32, h: 32, icon: "ellipsis-vertical-filled", tip: nsBlockKit.T("More actions")}}})
	}
	return h
}

// topLevelError is MessageErrorFallback: .p-top_level_error_element
// (sk_foreground_min_solid, radius 3, 10px padding) reading "We can’t
// fully display this message." in sk_foreground_high_solid.
func (l *lay) topLevelError(x, y, w int) int {
	pad := l.dp(10)
	fs := l.bodyStyle()
	fs.col = l.skRGBA("sk_foreground_high_solid", 1)
	box0 := len(l.d.boxes)
	l.plainAdd("\n")
	h, _ := l.plainFlow(nsBlockKit.T("We can’t fully display this message."), x+pad, y+pad, w-2*pad, &fs, -1)
	h += 2 * pad
	bg := box{kind: bFill, r: image.Rect(x, y, x+w, y+h), radius: l.dp(3), fill: l.skRGBA("sk_foreground_min_solid", 1), target: -1}
	l.d.boxes = append(l.d.boxes, box{})
	copy(l.d.boxes[box0+1:], l.d.boxes[box0:])
	l.d.boxes[box0] = bg
	return h
}

// centredFlow lays s out across w from x, each line centred
// (text-align: center), and returns its height.
func (l *lay) centredFlow(s string, x, y, w int, fs *flowStyle) int {
	f0, n0 := len(l.d.frags), len(l.d.lines)
	h, _ := l.plainFlow(s, x, y, w, fs, -1)
	for li := n0; li < len(l.d.lines); li++ {
		ln := &l.d.lines[li]
		a, b := max(ln.f0, f0), min(ln.f1, len(l.d.frags))
		if a >= b {
			continue
		}
		x0, x1 := float32(math.Inf(1)), float32(math.Inf(-1))
		for i := a; i < b; i++ {
			f := &l.d.frags[i]
			x0 = min(x0, f.x)
			x1 = max(x1, f.x+f.w)
		}
		dx := float32(x) + (float32(w)-(x1-x0))/2 - x0
		for i := a; i < b; i++ {
			l.d.frags[i].x += dx
		}
	}
	return h
}

// legend lays out DataVisualizationLegend: items of a 10px swatch, 8px,
// the label, in rows 8px apart (flex-wrap), each row centred, 18px tall.
// It returns its height (0 for no items).
func (l *lay) legend(labels []string, pal *chart.Palette, x, y, w int) int {
	if len(labels) == 0 {
		return 0
	}
	fs := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: l.dp(18), col: l.tok("content-pry"), emoji: l.dp(13)}
	k := l.faceFor(fs.face, fs.size, 0)
	sw, gap, lh := l.dp(10), l.dp(8), l.dp(18)
	widths := make([]int, len(labels))
	for i, s := range labels {
		widths[i] = sw
		if s != "" {
			widths[i] += gap + int(math.Ceil(float64(l.measure(s, k))))
		}
	}
	type row struct{ from, to, w int }
	var rows []row
	cur := row{}
	for i, iw := range widths {
		if cur.to > cur.from && cur.w+gap+iw > w {
			rows = append(rows, cur)
			cur = row{from: i, to: i}
		}
		if cur.to > cur.from {
			cur.w += gap
		}
		cur.w += iw
		cur.to = i + 1
	}
	rows = append(rows, cur)
	yy := y
	for ri, r := range rows {
		if ri > 0 {
			yy += gap
		}
		xx := x + (w-r.w)/2
		for i := r.from; i < r.to; i++ {
			c := pal.Series[i%len(pal.Series)]
			sy := yy + (lh-sw)/2
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(xx, sy, xx+sw, sy+sw), radius: sw / 2, fill: c, target: -1})
			if labels[i] != "" {
				l.plainAdd(" ")
				l.plainFlow(labels[i], xx+sw+gap, yy, max(widths[i]-sw-gap, 1)+l.dp(1), &fs, -1)
			}
			xx += widths[i] + gap
		}
		yy += lh
	}
	return yy - y
}

// ---- paint ----

// at maps chart px to view px.
func (c *chartPrim) at(x, y float64) f32.Point {
	return f32.Pt(float32(c.r.Min.X)+float32(x)*c.scale, float32(c.r.Min.Y)+float32(y)*c.scale)
}

// paintCharts draws the charts' static parts (the recording).
func (v *View) paintCharts(ops *op.Ops, st *Style) {
	for i := range v.d.charts {
		v.paintChart(ops, st, &v.d.charts[i])
	}
}

func (v *View) paintChart(ops *op.Ops, st *Style, c *chartPrim) {
	l := c.lay
	if l == nil {
		return
	}
	px := func(f float64) float32 { return float32(f) * c.scale }
	lw := max(1, px(1))
	// the split lines and the category axis line (1px, snapped)
	for _, s := range l.Splits {
		c.hline(ops, s, lw, c.pal.AxisLine)
	}
	if l.AxisLn != nil {
		c.hline(ops, *l.AxisLn, lw, c.pal.AxisLine)
	}
	// the series: areas, bars, lines, the symbols drawn without hover
	for _, a := range l.Areas {
		col := a.Color
		col.A = uint8(float32(col.A)*0.7 + 0.5) // areaStyle's default opacity
		paint.FillShape(ops, col, clip.Outline{Path: c.path(ops, a.Path, true)}.Op())
	}
	for _, b := range l.Bars {
		c.bar(ops, b, b.Color)
	}
	for _, ln := range l.Lines {
		paint.FillShape(ops, ln.Color, clip.Stroke{Path: c.path(ops, ln.Path, false), Width: px(2)}.Op())
	}
	for _, s := range l.Symbols {
		if s.Shown {
			c.symbol(ops, s.X, s.Y, 3, s.Color)
		}
	}
	for _, s := range l.Sectors {
		c.sector(ops, s, 0, s.Fill)
	}
	// the axis labels, then the y axis label
	for _, lb := range l.Labels {
		v.chartLabel(ops, st, c, lb)
	}
	if strings.TrimSpace(c.yLabel) != "" {
		v.yAxisLabel(ops, st, c)
	}
}

// hline draws a snapped horizontal line.
func (c *chartPrim) hline(ops *op.Ops, s chart.Seg, lw float32, col color.NRGBA) {
	p0, p1 := c.at(s.X1, s.Y1), c.at(s.X2, s.Y2)
	y := float32(math.Round(float64(p0.Y-lw/2))) + lw/2 // keep the 1px line on whole pixels at any scale
	r := image.Rect(int(math.Round(float64(p0.X))), int(y-lw/2+0.5), int(math.Round(float64(p1.X))), int(y-lw/2+0.5)+int(lw+0.5))
	paint.FillShape(ops, col, clip.Rect(r).Op())
}

// path builds cmds (chart px) as a Gio path.
func (c *chartPrim) path(ops *op.Ops, cmds []chart.Cmd, fill bool) clip.PathSpec {
	var p clip.Path
	p.Begin(ops)
	open := false
	for _, cmd := range cmds {
		switch cmd.Op {
		case 'M':
			if open && fill {
				p.Close()
			}
			p.MoveTo(c.at(cmd.P[0], cmd.P[1]))
			open = true
		case 'L':
			p.LineTo(c.at(cmd.P[0], cmd.P[1]))
		case 'C':
			p.CubeTo(c.at(cmd.P[0], cmd.P[1]), c.at(cmd.P[2], cmd.P[3]), c.at(cmd.P[4], cmd.P[5]))
		case 'Z':
			if open {
				p.Close()
				open = false
			}
		}
	}
	if open && fill {
		p.Close()
	}
	return p.End()
}

// bar fills a bar with zrender's buildPath for a round rect: the shape
// normalized to positive size, its top corners radius 4 (shrunk to fit,
// as zrender does when the rect is smaller than its radii).
func (c *chartPrim) bar(ops *op.Ops, b chart.BarItem, col color.NRGBA) {
	x, y, w, h := b.X, b.Y, b.W, b.H
	if w < 0 {
		x, w = x+w, -w
	}
	if h < 0 {
		y, h = y+h, -h
	}
	if w <= 0 || h <= 0 {
		return
	}
	r1, r2 := 4.0, 4.0 // tl, tr; br, bl 0
	if r1+r2 > w {
		t := r1 + r2
		r1, r2 = r1*w/t, r2*w/t
	}
	r1 = math.Min(r1, h) // r1 + r4 (0) > h
	r2 = math.Min(r2, h) // r2 + r3 (0) > h
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(c.at(x+r1, y))
	p.LineTo(c.at(x+w-r2, y))
	if r2 > 0 {
		arcTo(&p, c, x+w-r2, y+r2, r2, -math.Pi/2, 0)
	}
	p.LineTo(c.at(x+w, y+h))
	p.LineTo(c.at(x, y+h))
	p.LineTo(c.at(x, y+r1))
	if r1 > 0 {
		arcTo(&p, c, x+r1, y+r1, r1, math.Pi, 3*math.Pi/2)
	}
	p.Close()
	paint.FillShape(ops, col, clip.Outline{Path: p.End()}.Op())
}

// arcTo adds the circle arc about (cx, cy) radius r from angle a0 to a1
// (radians, y down: increasing turns clockwise on screen) as cubics.
func arcTo(p *clip.Path, c *chartPrim, cx, cy, r, a0, a1 float64) {
	n := int(math.Ceil(math.Abs(a1-a0) / (math.Pi / 2)))
	if n == 0 {
		return
	}
	step := (a1 - a0) / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	for i := 0; i < n; i++ {
		t0, t1 := a0+float64(i)*step, a0+float64(i+1)*step
		s0, c0 := math.Sincos(t0)
		s1, c1 := math.Sincos(t1)
		p.CubeTo(c.at(cx+r*(c0-k*s0), cy+r*(s0+k*c0)), c.at(cx+r*(c1+k*s1), cy+r*(s1-k*c1)), c.at(cx+r*c1, cy+r*s1))
	}
}

// symbol fills a symbol's circle, radius r chart px.
func (c *chartPrim) symbol(ops *op.Ops, x, y, r float64, col color.NRGBA) {
	paint.FillShape(ops, col, clip.Outline{Path: c.circle(ops, x, y, r)}.Op())
}

// circle is a circle about (x, y) radius r (chart px), at sub-pixel
// precision (clip.Ellipse takes whole pixels).
func (c *chartPrim) circle(ops *op.Ops, x, y, r float64) clip.PathSpec {
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(c.at(x+r, y))
	arcTo(&p, c, x, y, r, 0, 2*math.Pi)
	p.Close()
	return p.End()
}

// sector fills a pie sector grown by grow chart px, with its 2px
// border in base-pry (the empty circle has none).
func (c *chartPrim) sector(ops *op.Ops, s chart.SectorItem, grow float64, fill color.NRGBA) {
	if math.IsNaN(s.Start) || math.IsNaN(s.End) {
		return
	}
	r := s.R + grow
	build := func() clip.PathSpec {
		var p clip.Path
		p.Begin(ops)
		full := s.End-s.Start >= 2*math.Pi-1e-9
		if full {
			p.MoveTo(c.at(s.CX+r*math.Cos(s.Start), s.CY+r*math.Sin(s.Start)))
			arcTo(&p, c, s.CX, s.CY, r, s.Start, s.Start+2*math.Pi)
		} else {
			p.MoveTo(c.at(s.CX+r*math.Cos(s.Start), s.CY+r*math.Sin(s.Start)))
			arcTo(&p, c, s.CX, s.CY, r, s.Start, s.End)
			p.LineTo(c.at(s.CX, s.CY))
		}
		p.Close()
		return p.End()
	}
	paint.FillShape(ops, fill, clip.Outline{Path: build()}.Op())
	if s.StrokeW > 0 {
		paint.FillShape(ops, c.pal.Border, clip.Stroke{Path: build(), Width: float32(s.StrokeW) * c.scale}.Op())
	}
}

// chartLabel draws an axis label: 13px Lato in the axis tick colour, its
// 18px line box on the anchor (middle) or from it (top), aligned, turned
// by its rotation about the anchor.
func (v *View) chartLabel(ops *op.Ops, st *Style, c *chartPrim, lb chart.Label) {
	w := float64(textWidth(st, c.reg, lb.Text)) / float64(c.scale)
	x := 0.0
	switch lb.Align {
	case chart.AlignRight:
		x = -w
	case chart.AlignCenter:
		x = -w / 2
	}
	top := 0.0
	if lb.Middle {
		top = -9
	}
	anchor := c.at(lb.X, lb.Y)
	var t op.TransformStack
	if lb.Rotate != 0 {
		// zrender's rotation: positive turns counter-clockwise on screen
		t = op.Affine(f32.Affine2D{}.Rotate(anchor, -float32(lb.Rotate))).Push(ops)
	}
	v.chartText(ops, st, c.reg, lb.Text, anchor.X+float32(x)*c.scale, anchor.Y+float32(top)*c.scale, 18*c.scale, c.pal.AxisTick)
	if lb.Rotate != 0 {
		t.Pop()
	}
}

// yAxisLabel draws .yAxisLabel: 13px bold content-pry turned -90° in the
// 18px column at the chart's left, centred between its top and bottom.
func (v *View) yAxisLabel(ops *op.Ops, st *Style, c *chartPrim) {
	w := textWidth(st, c.bold, c.yLabel)
	ctr := c.at(9, (c.yTop+c.yBot)/2)
	t := op.Affine(f32.Affine2D{}.Rotate(ctr, -math.Pi/2)).Push(ops)
	v.chartText(ops, st, c.bold, c.yLabel, ctr.X-w/2, ctr.Y-9*c.scale, 18*c.scale, c.textCol)
	t.Pop()
}

// textWidth is s's width in face k (view px).
func textWidth(st *Style, k faceKey, s string) float32 {
	sp := st.spaceOf(k).adv
	var w float32
	for _, part := range strings.SplitAfter(s, " ") {
		trim := strings.TrimSuffix(part, " ")
		if trim != "" {
			w += st.word(k, trim).adv
		}
		if len(trim) != len(part) {
			w += sp
		}
	}
	return w
}

// chartText draws s from x in face k, its line box (lineH view px) from
// top: the baseline where a flow puts it.
func (v *View) chartText(ops *op.Ops, st *Style, k faceKey, s string, x, top, lineH float32, col color.NRGBA) {
	m := st.spaceOf(k)
	base := top + (lineH-(m.asc+m.desc))/2 + m.asc
	pen := x
	for _, part := range strings.SplitAfter(s, " ") {
		trim := strings.TrimSuffix(part, " ")
		if trim != "" {
			sh := st.word(k, trim)
			f := frag{sh: sh, x: pen, y: int(base + 0.5)}
			v.paintGlyphs(ops, st, &f, col)
			pen += sh.adv
		}
		if len(trim) != len(part) {
			pen += m.adv
		}
	}
}

// ---- hover ----

// chartAt is the chart under p (view coordinates) and what it points at:
// -1, -1 for none.
func (v *View) chartAt(p f32.Point) (int, int) {
	for i := range v.d.charts {
		c := &v.d.charts[i]
		if !image.Pt(int(p.X), int(p.Y)).In(c.r) || c.lay == nil {
			continue
		}
		x := float64((p.X - float32(c.r.Min.X)) / c.scale)
		y := float64((p.Y - float32(c.r.Min.Y)) / c.scale)
		held := -1
		if v.vizHot == i+1 {
			held = v.vizIdx
		}
		if h := c.lay.HoverAtHeld(x, y, held); h >= 0 {
			return i, h
		}
		return -1, -1
	}
	return -1, -1
}

// drawChartHover draws the hovered chart's axis pointer and emphasis, and
// defers its tooltip over the window.
func (v *View) drawChartHover(gtx layout.Context, st *Style) {
	if !v.hovering || v.vizHot <= 0 || v.vizHot > len(v.d.charts) {
		return
	}
	c := &v.d.charts[v.vizHot-1]
	l, h := c.lay, v.vizIdx
	if l == nil || h < 0 {
		return
	}
	ops := gtx.Ops
	switch l.Kind {
	case chart.Bar:
		for _, b := range l.Bars {
			if b.Cat == h {
				c.bar(ops, b, chart.Lift(b.Color))
			}
		}
	case chart.Line, chart.Area:
		// the hovered category's highlight puts every series' line in
		// emphasis (its stroke lifted), and shows its symbols at 1.1×
		for _, ln := range l.Lines {
			paint.FillShape(ops, chart.Lift(ln.Color), clip.Stroke{Path: c.path(ops, ln.Path, false), Width: 2 * c.scale}.Op())
		}
		for _, s := range l.Symbols {
			if s.Shown && s.Cat != h {
				c.symbol(ops, s.X, s.Y, 3, s.Color)
			}
		}
		for _, s := range l.Symbols {
			if s.Cat == h {
				c.symbol(ops, s.X, s.Y, 3*chart.SymbolHoverScale, chart.Lift(s.Color))
			}
		}
	case chart.Pie:
		for _, s := range l.Sectors {
			if s.Index == h {
				c.sector(ops, s, 5, chart.Lift(s.Fill))
			}
		}
	}
	if seg := l.PointerLine(h); seg != nil {
		c.dashedV(ops, *seg, c.pointerCol)
	}
	t, ok := l.TooltipAt(h)
	if !ok {
		return
	}
	v.chartTooltip(gtx, st, c, t)
}

// dashedV draws a vertical dashed 1px line ([4, 2]).
func (c *chartPrim) dashedV(ops *op.Ops, s chart.Seg, col color.NRGBA) {
	p0 := c.at(s.X1, s.Y1)
	p1 := c.at(s.X2, s.Y2)
	lw := max(1, c.scale)
	x := float32(math.Round(float64(p0.X - lw/2)))
	on, off := chart.PointerDash[0]*float64(c.scale), chart.PointerDash[1]*float64(c.scale)
	for y := float64(p0.Y); y < float64(p1.Y); y += on + off {
		y1 := math.Min(y+on, float64(p1.Y))
		r := image.Rect(int(x), int(math.Round(y)), int(x+lw+0.5), int(math.Round(y1)))
		paint.FillShape(ops, col, clip.Rect(r).Op())
	}
}

// chartTooltip draws the tooltip (TooltipHTMLContent, appended to the
// body: over the window, unclipped) 20px from the pointer.
func (v *View) chartTooltip(gtx layout.Context, st *Style, c *chartPrim, t chart.Tooltip) {
	m := func(s string, w chart.Weight) float64 {
		k := c.reg
		if w == chart.Black {
			k = c.black
		}
		return float64(textWidth(st, k, s)) / float64(c.scale)
	}
	tl := chart.LayTooltip(t, m)
	px := float64((v.ptr.X - float32(c.r.Min.X)) / c.scale)
	py := float64((v.ptr.Y - float32(c.r.Min.Y)) / c.scale)
	tx, ty := chart.TipPlace(px, py, tl.W, tl.H, c.lay.W, c.lay.H)
	rec := op.Record(gtx.Ops)
	ops := gtx.Ops
	// the card's overflow: hidden (popped before the macro ends)
	var cs clip.Stack
	if !c.card.Empty() {
		cs = clip.UniformRRect(c.card, c.cardRad).Push(ops)
	}
	o := c.at(tx, ty)
	sc := c.scale
	box := image.Rect(int(o.X+0.5), int(o.Y+0.5), int(o.X+float32(tl.W)*sc+0.5), int(o.Y+float32(tl.H)*sc+0.5))
	rad := int(chart.TipRadius*sc + 0.5)
	// box-shadow: 1px 2px 10px rgba(0, 0, 0, .2)
	drawShadow(ops, box.Add(image.Pt(int(sc+0.5), int(2*sc+0.5))), rad, 10*sc, color.NRGBA{A: 51})
	paint.FillShape(ops, c.pal.Border, clip.UniformRRect(box, rad).Op(ops)) // background and border: base-pry
	at := func(x, y float64) (float32, float32) { return o.X + float32(x)*sc, o.Y + float32(y)*sc }
	lineH := float32(13) * sc
	if hd := tl.Header; hd != nil {
		x, y := at(hd.X, hd.Top)
		v.chartText(ops, st, c.reg, hd.Text, x, y, lineH, c.pal.Text)
	}
	for _, r := range tl.Rows {
		paint.FillShape(ops, r.Marker, clip.Outline{Path: c.circle(ops, tx+r.MarkerX+5, ty+r.MarkerY+5, 5)}.Op())
		if r.Name != nil {
			x, y := at(r.Name.X, r.Name.Top)
			v.chartText(ops, st, c.reg, r.Name.Text, x, y, lineH, c.pal.Text)
		}
		if r.Value != nil {
			x, y := at(r.Value.X, r.Value.Top)
			v.chartText(ops, st, c.black, r.Value.Text, x, y, lineH, c.pal.Text)
		}
	}
	if !c.card.Empty() {
		cs.Pop()
	}
	op.Defer(gtx.Ops, rec.Stop())
}

// drawShadow is a box-shadow's blur: rings of decreasing alpha around r.
func drawShadow(ops *op.Ops, r image.Rectangle, rad int, blur float32, col color.NRGBA) {
	n := int(blur/2 + 0.5)
	if n < 1 {
		n = 1
	}
	for i := n; i >= 1; i-- {
		a := float32(col.A) * (1 - float32(i)/float32(n+1)) / float32(n)
		c := col
		c.A = uint8(a*1.6 + 0.5)
		paint.FillShape(ops, c, clip.UniformRRect(r.Inset(-i), rad+i).Op(ops))
	}
}

// chartSemantics gives each chart's box its accessible name, as Slack's
// chart div is role="img" with an aria-label.
func (v *View) chartSemantics(gtx layout.Context) {
	for i := range v.d.charts {
		c := &v.d.charts[i]
		if c.label == "" {
			continue
		}
		s := clip.Rect(c.r).Push(gtx.Ops)
		semantic.LabelOp(c.label).Add(gtx.Ops)
		s.Pop()
	}
}

// ChartRect is a chart's box (view coordinates) and its block, for tests
// and owners.
type ChartRect struct {
	Viz *richtext.DataViz
	R   image.Rectangle
}

// ChartRects lists the laid-out charts.
func (v *View) ChartRects() []ChartRect {
	out := make([]ChartRect, 0, len(v.d.charts))
	for i := range v.d.charts {
		out = append(out, ChartRect{R: v.d.charts[i].r})
	}
	j := 0
	for i := range v.d.bars {
		if b := &v.d.bars[i]; b.viz != nil && j < len(out) {
			out[j].Viz = b.viz
			j++
		}
	}
	return out
}

// ChartHover is the hovered chart and what it points at (-1, -1: none).
func (v *View) ChartHover() (int, int) {
	if !v.hovering || v.vizHot <= 0 {
		return -1, -1
	}
	return v.vizHot - 1, v.vizIdx
}

// ChartLayout is chart i's layout (chart px), for tests.
func (v *View) ChartLayout(i int) *chart.Layout {
	if i < 0 || i >= len(v.d.charts) {
		return nil
	}
	return v.d.charts[i].lay
}

// ChartLabel is chart i's accessible name.
func (v *View) ChartLabel(i int) string {
	if i < 0 || i >= len(v.d.charts) {
		return ""
	}
	return v.d.charts[i].label
}
