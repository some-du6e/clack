package chart

import (
	"image/color"
	"math"
	"strings"
)

// Hover is what the pointer points at: a category on a cartesian chart
// (the tooltip's trigger "axis"), a segment on a pie (trigger "item");
// -1 for nothing.
//
// On a cartesian chart the pointer must be inside the grid (the axis
// pointer's containPoint). The category axis snaps: bars take the band
// the pointer is in, points the nearest.
func (l *Layout) HoverAt(x, y float64) int {
	if l == nil {
		return -1
	}
	if l.Kind == Pie {
		return l.sectorAt(x, y, -1)
	}
	n := len(l.spec.Categories)
	if n == 0 || !l.Grid.Contains(x, y) {
		return -1
	}
	g := l.Grid
	var i int
	switch {
	case l.Kind == Bar:
		i = int(math.Floor((x - g.X) / (g.W / float64(n))))
	case n > 1:
		i = int(jsRound((x - g.X) / (g.W / float64(n-1))))
	}
	return min(max(i, 0), n-1)
}

// sectorAt is the segment whose sector holds (x, y); hovered is the one
// drawn emphasized (5px larger).
func (l *Layout) sectorAt(x, y float64, hovered int) int {
	for _, s := range l.Sectors {
		if s.Index < 0 {
			continue
		}
		r := s.R
		if s.Index == hovered {
			r += pieScaleSize
		}
		dx, dy := x-s.CX, y-s.CY
		if dx*dx+dy*dy > r*r {
			continue
		}
		a := math.Atan2(dy, dx)
		for a < s.Start {
			a += 2 * math.Pi
		}
		for a >= s.Start+2*math.Pi {
			a -= 2 * math.Pi
		}
		if a < s.End {
			return s.Index
		}
	}
	return -1
}

// HoverAtHeld is HoverAt for a pie whose segment held is hovered now (its
// sector has grown, so the pointer may stay on its rim).
func (l *Layout) HoverAtHeld(x, y float64, held int) int {
	if l != nil && l.Kind == Pie {
		return l.sectorAt(x, y, held)
	}
	return l.HoverAt(x, y)
}

// pieScaleSize is the pie's emphasis scaleSize: a hovered sector's radius
// grows by 5px.
const pieScaleSize = 5

// PointerLine is the axis pointer at hover h: a dashed vertical line
// through the category, across the grid (nil for a pie or no hover).
// Bars get the axis pointer's own line (tokens.color.border, #b7b9be);
// lines and areas Slack's content.ter one.
func (l *Layout) PointerLine(h int) *Seg {
	if l == nil || l.Kind == Pie || h < 0 || h >= len(l.catX) {
		return nil
	}
	x := subPixel(l.catX[h])
	return &Seg{x, l.Grid.Y, x, l.Grid.Y + l.Grid.H}
}

// PointerDash is a dashed line's pattern: [4, 2] times its 1px width.
var PointerDash = [2]float64{4, 2}

// EChartsBorder is ECharts' tokens.color.border (neutral30): the default
// axis pointer's colour, which Slack's bar charts keep.
var EChartsBorder = color.NRGBA{0xb7, 0xb9, 0xbe, 0xff}

// Lift is zrender's lift(color, -0.1), the colour an emphasized element
// takes when its emphasis style has none: each channel times 1.1,
// truncated, at most 255.
func Lift(c color.NRGBA) color.NRGBA {
	l := func(v uint8) uint8 {
		return uint8(min(255, int(float64(v)*1.1)))
	}
	return color.NRGBA{l(c.R), l(c.G), l(c.B), c.A}
}

// SymbolHoverScale is a hovered symbol's scale: max(1.1, 3 / (size/2)) for
// its 6px.
const SymbolHoverScale = 1.1

// Tooltip is the tooltip's content at a hover.
type Tooltip struct {
	Header string // the category ("" none: noHeader)
	Rows   []TipRow
}

// TipRow is one name and value with its marker.
type TipRow struct {
	Marker      color.NRGBA
	Name, Value string
	NoName      bool
}

// TooltipAt is the tooltip ECharts' default formatter writes for hover h:
// the axis tooltip's category, then each series' name and value at it
// ("-" for none); a pie's segment label and value (the series has no
// name, so no header).
func (l *Layout) TooltipAt(h int) (Tooltip, bool) {
	if l == nil || h < 0 {
		return Tooltip{}, false
	}
	if l.Kind == Pie {
		if h >= len(l.spec.Segments) {
			return Tooltip{}, false
		}
		s := l.spec.Segments[h]
		return Tooltip{Rows: []TipRow{{Marker: s.Color, Name: s.Label, NoName: strings.TrimSpace(s.Label) == "", Value: TooltipValue(s.Value)}}}, true
	}
	if h >= len(l.spec.Categories) {
		return Tooltip{}, false
	}
	t := Tooltip{Header: l.spec.Categories[h]}
	if strings.TrimSpace(t.Header) == "" {
		t.Header = ""
	}
	for _, s := range l.spec.Series {
		v := math.NaN()
		if h < len(s.Values) {
			v = s.Values[h]
		}
		t.Rows = append(t.Rows, TipRow{Marker: s.Color, Name: s.Name, NoName: strings.TrimSpace(s.Name) == "", Value: TooltipValue(v)})
	}
	return t, true
}

// TipLayout is the tooltip's box (TooltipHTMLContent) at a size: CSS px,
// the box at (0, 0).
type TipLayout struct {
	W, H   float64
	Header *TipText
	Rows   []TipRowLayout
}

// TipText is a line of the tooltip: its text's left edge and its line
// box's top (13px: line-height 1).
type TipText struct {
	X, Top float64
	Text   string
	Black  bool // the value's font-weight 900
}

// TipRowLayout is a row: its marker (10px circle) and texts.
type TipRowLayout struct {
	MarkerX, MarkerY float64 // the circle's top-left
	Marker           color.NRGBA
	Name, Value      *TipText
}

// Tooltip box metrics (TooltipModel, tooltipMarkup).
const (
	TipPadding = 10 // getPaddingFromTooltipModel: html 10
	TipBorder  = 1  // borderWidth
	TipRadius  = 4  // borderRadius
	tipLineH   = 13 // line-height:1 at 13px
	tipGap     = 10 // HTML_GAPS[1]
	tipMarker  = 10
)

// TipAscent is where a 13px Lato line's baseline sits in its 13px line
// box (line-height 1): the half-leading of Lato's 0.987 + 0.213em content
// area, then its ascent.
const TipAscent = (tipLineH-13*(0.987+0.213))/2 + 13*0.987

// LayTooltip lays t out.
func LayTooltip(t Tooltip, m Measure) TipLayout {
	var tl TipLayout
	content := 0.0
	y := 0.0
	if t.Header != "" {
		tl.Header = &TipText{X: 0, Top: 0, Text: t.Header}
		content = m(t.Header, Regular)
		y = tipLineH + tipGap
	}
	for i, r := range t.Rows {
		if i > 0 {
			y += tipGap
		}
		x := 0.0
		row := TipRowLayout{MarkerX: 0, MarkerY: y + TipAscent - tipMarker, Marker: r.Marker}
		x += tipMarker + 4 // margin-right 4
		w := x
		if !r.NoName {
			x += 2 // the name's margin-left
			nw := m(r.Name, Regular)
			row.Name = &TipText{X: x, Top: y, Text: r.Name}
			x += nw
			w = x
		}
		gap := 20.0 // the value's margin-left: 20px, 10px next to the marker
		if r.NoName {
			gap = 10
		}
		vw := m(r.Value, Black)
		row.Value = &TipText{Top: y, Text: r.Value, Black: true, X: vw} // X: its width until the box is known
		w += gap + vw
		content = math.Max(content, w)
		tl.Rows = append(tl.Rows, row)
		y += tipLineH
	}
	// the values float right: against the content box's right edge
	for i := range tl.Rows {
		v := tl.Rows[i].Value
		v.X = content - v.X
	}
	inset := float64(TipPadding + TipBorder)
	tl.W = content + 2*inset
	tl.H = y + 2*inset
	shift := func(tt *TipText) {
		if tt != nil {
			tt.X += inset
			tt.Top += inset
		}
	}
	shift(tl.Header)
	for i := range tl.Rows {
		r := &tl.Rows[i]
		r.MarkerX += inset
		r.MarkerY += inset
		shift(r.Name)
		shift(r.Value)
	}
	return tl
}

// TipPlace is refixTooltipPosition: 20px right of and below the pointer
// (x, y), on its left or above it when the chart's box (w × h) has no
// room on that side (2px more kept on the right for the floated values),
// rounded as the box's transform is (toFixed(0)).
func TipPlace(x, y, tipW, tipH, w, h float64) (float64, float64) {
	const gap = 20
	if x+tipW+gap+2 > w {
		x -= tipW + gap
	} else {
		x += gap
	}
	if y+tipH+gap > h {
		y -= tipH + gap
	} else {
		y += gap
	}
	return jsRound(x), jsRound(y)
}
