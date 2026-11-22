package chart

import (
	"image/color"
	"math"
	"sort"
)

// Kind is a chart type.
type Kind uint8

const (
	Bar Kind = iota + 1
	Line
	Area
	Pie
)

// Spec is Slack's built option (boot-shared k+pA buildCartesianOption,
// mhRP buildPieOption): what ECharts gets to lay out.
type Spec struct {
	Kind       Kind
	Categories []string // xAxis.data
	// Rotate is needsRotation: 45° labels, truncated at 75px, the grid's
	// insets 26/0/8/8 and containLabel.
	Rotate   bool
	Series   []Series  // in draw order (areas by descending peak)
	Segments []Segment // pie
}

// Series is one series of the option.
type Series struct {
	Name     string
	Values   []float64 // per category, NaN: null
	Isolated []bool    // markIsolatedPoints: a symbol shown without hover
	Color    color.NRGBA
	Area     color.NRGBA // areaStyle.color (area charts)
	Index    int         // its place in the block's series (palette, legend)
}

// Segment is one pie segment of the option.
type Segment struct {
	Label string
	Value float64 // NaN: "-"
	Color color.NRGBA
}

// Palette is useChartTokens' colours (boot-shared E1b5) in the client's
// theme: the --dt_color tokens they name.
type Palette struct {
	Series   [12]color.NRGBA // palette: plt tangerine 50 … indigo 70
	Area     [12]color.NRGBA // areaFillPalette: plt tangerine 0 … indigo 10
	AxisTick color.NRGBA     // content.ter: axis labels, the line chart's pointer
	AxisLine color.NRGBA     // otl.ter: the axis line, split lines
	Border   color.NRGBA     // base.pry: pie borders, the tooltip's box
	Text     color.NRGBA     // content.pry: the tooltip's text
}

// PaletteTokens is E1b5's token list: the palette's then the area
// palette's --dt_color names (without the prefix).
var PaletteTokens = [2][12]string{
	{"plt-tangerine-50", "plt-jade-50", "plt-aubergine-50", "plt-lagoon-50", "plt-flamingo-50", "plt-indigo-50",
		"plt-tangerine-70", "plt-jade-80", "plt-aubergine-80", "plt-lagoon-70", "plt-flamingo-80", "plt-indigo-70"},
	{"plt-tangerine-0", "plt-jade-0", "plt-aubergine-0", "plt-lagoon-0", "plt-flamingo-0", "plt-indigo-0",
		"plt-tangerine-60", "plt-jade-10", "plt-aubergine-10", "plt-lagoon-10", "plt-flamingo-10", "plt-indigo-10"},
}

// Build is the option Slack builds for d (nil for a chart with no option:
// an unsupported type, or a render that throws).
func Build(d *Data, pal *Palette) *Spec {
	if d == nil || d.Err != nil || !d.Supported {
		return nil
	}
	if d.Type == "pie" {
		s := &Spec{Kind: Pie}
		for i, g := range d.Segments {
			s.Segments = append(s.Segments, Segment{Label: g.Label, Value: g.Value, Color: pal.Series[i%len(pal.Series)]})
		}
		return s
	}
	s := &Spec{Categories: d.Categories, Rotate: d.Rotate}
	switch d.Type {
	case "bar":
		s.Kind = Bar
	case "line":
		s.Kind = Line
	default:
		s.Kind = Area
	}
	for i, ds := range d.Series {
		s.Series = append(s.Series, Series{Name: ds.Name, Values: ds.Values, Isolated: ds.Isolated,
			Color: pal.Series[i%len(pal.Series)], Area: pal.Area[i%len(pal.Area)], Index: i})
	}
	if s.Kind == Area {
		// sortAreaSeriesByDescendingPeak: [...e].sort((e, t) => peak(t) - peak(e)),
		// stable, a NaN difference (two series with no values) a tie
		sort.SliceStable(s.Series, func(i, j int) bool { return peak(s.Series[j].Values)-peak(s.Series[i].Values) < 0 })
	}
	return s
}

func peak(vs []float64) float64 {
	t := math.Inf(-1)
	for _, v := range vs {
		if !math.IsNaN(v) && v > t {
			t = v
		}
	}
	return t
}
