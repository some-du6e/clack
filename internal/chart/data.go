package chart

import (
	"encoding/json"
	"math"
)

// Data is a data_visualization block's chart as Slack's renderable reads
// it (boot-shared vxqh's processProps, toCamelCaseProps; then
// canvas-data-visualization-block's DataVisualizationBlock and mhRP's
// DataVisualizationChart, build 9f450b8ad3db), step by step, with
// JavaScript's semantics for whatever the payload holds.
//
// Err is set when that render throws: a missing chart or axis_config, a
// categories/series/data/segments that isn't a list, a duplicate category,
// series label or segment label, an object where React draws a child
// (the title, an axis label, a legend label). The block's SUIErrorBoundary
// then draws the surface's errorFallback ("We can’t fully display this
// message." in messages, nothing in modals and App Home). Supported is
// false for a chart type Slack has no option for: the card draws its
// title and no chart.
type Data struct {
	Type      string // chart.type ("" unless a string)
	Supported bool   // isSupportedChart: pie, bar, line or area
	Err       error

	// cartesian charts
	Categories []string // axisConfig.categories.map(String)
	XLabel     string   // axisConfig.x_label as React draws it
	YLabel     string
	Rotate     bool // categoriesNeedRotation(categories) at legacy width (624)
	Series     []DataSeries

	// pie charts
	Segments []DataSegment

	// Legend is the legend's labels as React draws them (legendLabels):
	// series names, or segment labels.
	Legend []string

	desc descInput // buildChartDescription's inputs
	raw  rawChart  // the table's and TSV's raw values (F8Uw)
}

// DataSeries is one series after alignSeriesToCategories: its value at
// each category (NaN for none), and markIsolatedPoints' flags.
type DataSeries struct {
	Name     string    // toChartString(name)
	Values   []float64 // per category, NaN: null
	Isolated []bool    // drawn without hover (line and area charts)
}

// DataSegment is one pie segment: toChartString(label), toChartNumber
// (value) (NaN for "-").
type DataSegment struct {
	Label string
	Value float64
}

// rawChart keeps the values convertDataVisualizationToTable reads raw.
type rawChart struct {
	categories []any
	series     []rawSeries
	segments   rawPoints
}

type rawSeries struct {
	name    any
	hasName bool
	points  []rawPoint
}

type rawPoint struct {
	label, value       any
	hasLabel, hasValue bool
}

// ParseChart reads a block's "chart" (nil raw: no chart).
func ParseChart(raw json.RawMessage) Data {
	var v any
	present := len(raw) > 0
	if present && json.Unmarshal(raw, &v) != nil {
		v, present = nil, false
	}
	return parseChart(v, present)
}

func parseChart(c any, present bool) (d Data) {
	// DataVisualizationBlock: isSupportedChart(chart) reads chart.type
	t, hasType, err := field(c, present, "type")
	if err != nil {
		d.Err = err
		return
	}
	if s, ok := t.(string); ok && hasType {
		d.Type = s
	}
	switch d.Type {
	case "pie":
		d.Supported = true
		d.Err = d.parsePie(c)
	case "bar", "line", "area":
		d.Supported = true
		d.Err = d.parseCartesian(c)
	}
	return
}

func (d *Data) parseCartesian(c any) error {
	ac, hasAC, err := field(c, true, "axis_config")
	if err != nil {
		return err
	}
	// categoriesNeedRotation(axisConfig.categories): .length, then .map
	// unless it has at most one item
	cats, hasCats, err := field(ac, hasAC, "categories")
	if err != nil {
		return err
	}
	if !hasCats || cats == nil {
		return errRender // undefined.length
	}
	a, ok := cats.([]any)
	if !ok {
		// anything but a list throws, here or at categories.map below
		return errRender
	}
	if len(a) > 1 {
		// Math.max(...categories.map(c => c.length), 0)
		mx := math.Inf(-1)
		for _, x := range a {
			switch t := x.(type) {
			case nil:
				return errRender // null.length
			case string:
				mx = math.Max(mx, float64(jsLen(t)))
			case []any:
				mx = math.Max(mx, float64(len(t)))
			default:
				mx = math.NaN() // undefined: Math.max is NaN from then on
			}
		}
		d.Rotate = rotationFor(a, mx)
	}
	// buildCartesianOption: assertUniqueCategories (for...of), then
	// categories.map(String)
	items, err := iterate(cats, true)
	if err != nil {
		return err
	}
	seen := map[jsKey]bool{}
	for i, x := range items {
		k := keyOf(x, true, i)
		if seen[k] {
			return errRender // duplicate category
		}
		seen[k] = true
	}
	ca, err := arrayOf(cats, true)
	if err != nil {
		return err
	}
	d.raw.categories = ca
	d.Categories = make([]string, len(ca))
	for i, x := range ca {
		d.Categories[i] = jsString(x, true)
	}
	ser, hasSer, err := field(c, true, "series")
	if err != nil {
		return err
	}
	sa, err := arrayOf(ser, hasSer)
	if err != nil {
		return err
	}
	for _, s := range sa {
		data, hasData, err := field(s, true, "data")
		if err != nil {
			return err
		}
		pts, err := iterate(data, hasData)
		if err != nil {
			return err
		}
		name, hasName, _ := field(s, true, "name")
		ds := DataSeries{Name: jsString(name, hasName)}
		byLabel := map[string]float64{}
		rs := rawSeries{name: name, hasName: hasName}
		for _, p := range pts {
			l, hasL, err := field(p, true, "label")
			if err != nil {
				return err
			}
			key := jsString(l, hasL)
			if _, dup := byLabel[key]; dup {
				return errRender // duplicate label in a series
			}
			val, hasV, _ := field(p, true, "value")
			byLabel[key] = toChartNumber(val)
			rs.points = append(rs.points, rawPoint{label: l, value: val, hasLabel: hasL, hasValue: hasV})
		}
		ds.Values = make([]float64, len(d.Categories))
		for i, cat := range d.Categories {
			if v, ok := byLabel[cat]; ok {
				ds.Values[i] = v
			} else {
				ds.Values[i] = math.NaN()
			}
		}
		ds.Isolated = isolated(ds.Values)
		d.Series = append(d.Series, ds)
		d.raw.series = append(d.raw.series, rs)
	}
	// buildChartDescription: the axis labels through Vz (null and
	// undefined stay undefined), the series' names
	xl, hasXL, _ := field(ac, true, "x_label")
	yl, hasYL, _ := field(ac, true, "y_label")
	d.desc = descInput{xLabel: xl, hasX: hasXL && xl != nil, yLabel: yl, hasY: hasYL && yl != nil}
	for _, rs := range d.raw.series {
		d.desc.names = append(d.desc.names, jsNameOf(rs))
	}
	d.desc.count = len(sa)
	// the y and x axis labels and the legend, as React draws them
	if d.YLabel, err = reactText(yl, hasYL); err != nil {
		return err
	}
	if d.XLabel, err = reactText(xl, hasXL); err != nil {
		return err
	}
	for _, rs := range d.raw.series {
		s, err := reactText(rs.name, rs.hasName)
		if err != nil {
			return err
		}
		d.Legend = append(d.Legend, s)
	}
	return nil
}

func (d *Data) parsePie(c any) error {
	segs, hasSegs, err := field(c, true, "segments")
	if err != nil {
		return err
	}
	// assertUniqueSegmentLabels: for...of, Set of raw labels
	items, err := iterate(segs, hasSegs)
	if err != nil {
		return err
	}
	seen := map[jsKey]bool{}
	for i, s := range items {
		l, hasL, err := field(s, true, "label")
		if err != nil {
			return err
		}
		k := keyOf(l, hasL, i)
		if seen[k] {
			return errRender // duplicate segment label
		}
		seen[k] = true
	}
	sa, err := arrayOf(segs, true) // segments.map
	if err != nil {
		return err
	}
	for _, s := range sa {
		l, hasL, _ := field(s, true, "label")
		v, hasV, _ := field(s, true, "value")
		d.Segments = append(d.Segments, DataSegment{Label: jsString(l, hasL), Value: toChartNumber(v)})
		d.raw.segments = append(d.raw.segments, rawPoint{label: l, value: v, hasLabel: hasL, hasValue: hasV})
	}
	d.desc.count = len(sa)
	for _, p := range d.raw.segments {
		s, err := reactText(p.label, p.hasLabel)
		if err != nil {
			return err
		}
		d.Legend = append(d.Legend, s)
	}
	return nil
}

// isolated is markIsolatedPoints' rule: a point with no point on either
// side draws its symbol; the rest show it only on hover.
func isolated(vs []float64) []bool {
	out := make([]bool, len(vs))
	for i, v := range vs {
		if math.IsNaN(v) {
			continue
		}
		prev := i > 0 && !math.IsNaN(vs[i-1])
		next := i < len(vs)-1 && !math.IsNaN(vs[i+1])
		out[i] = !prev && !next
	}
	return out
}

// rotationFor is categoriesNeedRotation(categories) at the legacy width:
// 7.5px a character for the longest against 80% of a category's slot of
// (624 - 74)px. mx is Math.max over the categories' .length (NaN when one
// has none).
func rotationFor(cats []any, mx float64) bool {
	if len(cats) <= 1 {
		return false
	}
	mx = math.Max(mx, 0)
	if math.IsNaN(mx) {
		return false
	}
	return 7.5*mx > 0.8*((624-74)/float64(len(cats)))
}

// jsLen is a string's .length: UTF-16 code units.
func jsLen(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// jsNameOf is a series' name as the description lists it (raw).
func jsNameOf(rs rawSeries) jsName { return jsName{v: rs.name, ok: rs.hasName} }

type jsName struct {
	v  any
	ok bool
}
