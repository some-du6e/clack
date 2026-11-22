package chart

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// The y axis labels and the tooltip's values against Node's Intl (en-US)
// running Slack's formatYAxisTick, String(n) and ECharts' addCommas.
func TestNumberFormatsMatchSlack(t *testing.T) {
	for _, c := range []struct {
		v                      float64
		tick, str, tooltipText string
	}{
		{0, "0", "0", "0"},
		{1, "1", "1", "1"},
		{-1, "-1", "-1", "-1"},
		{0.5, "0.5", "0.5", "0.5"},
		{0.1 + 0.2, "0.3", "0.30000000000000004", "0.30000000000000004"},
		{0.0005, "0.001", "0.0005", "0.0005"},
		{0.00049, "0", "0.00049", "0.00049"},
		{2.5, "2.5", "2.5", "2.5"},
		{1000, "1,000", "1000", "1,000"},
		{1234.5678, "1,234.568", "1234.5678", "1,234.5678"},
		{9999, "9,999", "9999", "9,999"},
		{9999.5, "9,999.5", "9999.5", "9,999.5"},
		{10000, "10K", "10000", "10,000"},
		{12345, "12.3K", "12345", "12,345"},
		{12350, "12.4K", "12350", "12,350"},
		{12450, "12.5K", "12450", "12,450"},
		{-12345, "-12.3K", "-12345", "-12,345"},
		{99999, "100K", "99999", "99,999"},
		{99950, "100K", "99950", "99,950"},
		{123456, "123.5K", "123456", "123,456"},
		{999949, "999.9K", "999949", "999,949"},
		{999950, "1M", "999950", "999,950"},
		{999999, "1M", "999999", "999,999"},
		{1e6, "1M", "1000000", "1,000,000"},
		{1250000, "1.3M", "1250000", "1,250,000"},
		{2400000, "2.4M", "2400000", "2,400,000"},
		{1e9, "1B", "1000000000", "1,000,000,000"},
		{1.25e9, "1.3B", "1250000000", "1,250,000,000"},
		{1e12, "1T", "1000000000000", "1,000,000,000,000"},
		{3.5e12, "3.5T", "3500000000000", "3,500,000,000,000"},
		{1e15, "1000T", "1000000000000000", "1,000,000,000,000,000"},
		{2.5e16, "25,000T", "25000000000000000", "25,000,000,000,000,000"},
		{-1e6, "-1M", "-1000000", "-1,000,000"},
		{1e21, "1,000,000,000T", "1e+21", "1e+21"},
		{5e-7, "0", "5e-7", "5e-7"},
		{1e-7, "0", "1e-7", "1e-7"},
		{123456789.123, "123.5M", "123456789.123", "123,456,789.123"},
	} {
		if got := FormatYAxisTick(c.v); got != c.tick {
			t.Errorf("FormatYAxisTick(%v) = %q, want %q", c.v, got, c.tick)
		}
		if got := JSNumber(c.v); got != c.str {
			t.Errorf("JSNumber(%v) = %q, want %q", c.v, got, c.str)
		}
		if got := TooltipValue(c.v); got != c.tooltipText {
			t.Errorf("TooltipValue(%v) = %q, want %q", c.v, got, c.tooltipText)
		}
	}
	if got := TooltipValue(math.NaN()); got != "-" {
		t.Errorf("TooltipValue(NaN) = %q, want -", got)
	}
}

const karimChart = `{"type":"bar","series":[{"name":"time to window (ms)","data":[{"label":"Slack","value":1059},{"label":"Slick","value":1816},{"label":"Taut","value":2579},{"label":"Clack","value":212}]}],"axis_config":{"categories":["Slack","Slick","Taut","Clack"],"x_label":"client","y_label":"time to window (ms)"}}`

// What Slack's render does with each shape of chart: a throw (the
// errorFallback), no chart (the title alone), or a chart.
func TestParseChartLikeSlacksRender(t *testing.T) {
	for _, c := range []struct {
		name, chart    string
		err, supported bool
	}{
		{"karim", karimChart, false, true},
		{"no chart", ``, true, false},
		{"null chart", `null`, true, false},
		{"a string", `"bar"`, false, false},
		{"unknown type", `{"type":"scatter"}`, false, false},
		{"type not a string", `{"type":1}`, false, false},
		{"no axis_config", `{"type":"bar","series":[]}`, true, true},
		{"no categories", `{"type":"line","series":[],"axis_config":{}}`, true, true},
		{"categories a string", `{"type":"bar","series":[],"axis_config":{"categories":"ab"}}`, true, true},
		{"a null category", `{"type":"bar","series":[],"axis_config":{"categories":["a",null]}}`, true, true},
		{"one null category", `{"type":"bar","series":[],"axis_config":{"categories":[null]}}`, false, true},
		{"duplicate category", `{"type":"bar","series":[],"axis_config":{"categories":["a","a"]}}`, true, true},
		{"1 and \"1\" differ", `{"type":"bar","series":[],"axis_config":{"categories":["1",1]}}`, false, true},
		{"no series", `{"type":"bar","axis_config":{"categories":["a"]}}`, true, true},
		{"empty series", `{"type":"bar","series":[],"axis_config":{"categories":["a"]}}`, false, true},
		{"a series without data", `{"type":"bar","series":[{"name":"s"}],"axis_config":{"categories":["a"]}}`, true, true},
		{"a null series", `{"type":"bar","series":[null],"axis_config":{"categories":["a"]}}`, true, true},
		{"a null point", `{"type":"bar","series":[{"name":"s","data":[null]}],"axis_config":{"categories":["a"]}}`, true, true},
		{"duplicate label", `{"type":"bar","series":[{"name":"s","data":[{"label":"a","value":1},{"label":"a","value":2}]}],"axis_config":{"categories":["a"]}}`, true, true},
		{"labels String()ed alike", `{"type":"bar","series":[{"name":"s","data":[{"label":1,"value":1},{"label":"1","value":2}]}],"axis_config":{"categories":["a"]}}`, true, true},
		{"an object label in the legend", `{"type":"bar","series":[{"name":{"x":1},"data":[]}],"axis_config":{"categories":["a"]}}`, true, true},
		{"an object x_label", `{"type":"bar","series":[],"axis_config":{"categories":["a"],"x_label":{}}}`, true, true},
		{"data a one-character string", `{"type":"bar","series":[{"name":"s","data":"x"}],"axis_config":{"categories":["a"]}}`, false, true},
		{"data a longer string", `{"type":"bar","series":[{"name":"s","data":"xy"}],"axis_config":{"categories":["a"]}}`, true, true},
		{"no segments", `{"type":"pie"}`, true, true},
		{"empty pie", `{"type":"pie","segments":[]}`, false, true},
		{"duplicate segment", `{"type":"pie","segments":[{"label":"a","value":1},{"label":"a","value":2}]}`, true, true},
		{"segments a string", `{"type":"pie","segments":"ab"}`, true, true},
		{"camelCase keys", `{"type":"line","series":[],"axisConfig":{"categories":["a"],"xLabel":"x"}}`, false, true},
	} {
		d := ParseChart(json.RawMessage(c.chart))
		if (d.Err != nil) != c.err || d.Supported != c.supported {
			t.Errorf("%s: err %v supported %v, want err %v supported %v", c.name, d.Err, d.Supported, c.err, c.supported)
		}
	}
}

// alignSeriesToCategories and markIsolatedPoints: a point for each
// category by its label, non-numbers none, labels not in the categories
// dropped; a symbol drawn only where no neighbour is.
func TestParseChartAlignsSeries(t *testing.T) {
	d := ParseChart(json.RawMessage(`{"type":"line","series":[{"name":7,"data":[{"label":"b","value":"5"},{"label":"a","value":1},{"label":"z","value":3},{"label":"c","value":2},{"label":"e","value":4}]}],"axis_config":{"categories":["a","b","c","d","e"],"x_label":"x","y_label":"y"}}`))
	if d.Err != nil {
		t.Fatal(d.Err)
	}
	s := d.Series[0]
	if s.Name != "7" {
		t.Errorf("name %q, want String(7)", s.Name)
	}
	want := []float64{1, math.NaN(), 2, math.NaN(), 4}
	for i, v := range want {
		if !(math.IsNaN(v) && math.IsNaN(s.Values[i]) || v == s.Values[i]) {
			t.Errorf("value %d = %v, want %v", i, s.Values[i], v)
		}
	}
	iso := []bool{true, false, true, false, true}
	for i, v := range iso {
		if s.Isolated[i] != v {
			t.Errorf("isolated %d = %v, want %v", i, s.Isolated[i], v)
		}
	}
	if len(d.Legend) != 1 || d.Legend[0] != "7" || d.XLabel != "x" || d.YLabel != "y" {
		t.Errorf("legend %q, labels %q %q", d.Legend, d.XLabel, d.YLabel)
	}
}

// The chart's aria-label (buildChartDescription).
func TestDescriptionLikeSlack(t *testing.T) {
	d := ParseChart(json.RawMessage(karimChart))
	if got, want := d.Description("startup loading time", true), "Bar chart: startup loading time. The X axis is client, the Y axis is time to window (ms). There is 1 series: time to window (ms)."; got != want {
		t.Errorf("bar: %q, want %q", got, want)
	}
	line := ParseChart(json.RawMessage(`{"type":"line","series":[{"name":"a","data":[]},{"name":"b","data":[]},{"name":"c","data":[]}],"axis_config":{"categories":["x"]}}`))
	if got, want := line.Description("T", true), "Line chart: T. The X axis is undefined, the Y axis is undefined. There are 3 series: a, b, and c."; got != want {
		t.Errorf("line: %q, want %q", got, want)
	}
	pie := ParseChart(json.RawMessage(`{"type":"pie","segments":[{"label":"Kit Kat","value":45},{"label":"Twix","value":28},{"label":"Crunch","value":18},{"label":"Milky Way","value":9}]}`))
	if got, want := pie.Description("My Favorite Candy Bars", true), "Pie chart: My Favorite Candy Bars, with 4 segments. Segments: Kit Kat (45%), Twix (28%), Crunch (18%), and Milky Way (9%)."; got != want {
		t.Errorf("pie: %q, want %q", got, want)
	}
	big := ParseChart(json.RawMessage(`{"type":"pie","segments":[{"label":"a","value":1},{"label":"b","value":2},{"label":"c","value":3},{"label":"d","value":4},{"label":"e","value":10}]}`))
	if got, want := big.Description("five", true), "Pie chart: five, with 5 segments. The largest segment is e at 50%, the smallest is a at 5%."; got != want {
		t.Errorf("pie of five: %q, want %q", got, want)
	}
	one := ParseChart(json.RawMessage(`{"type":"pie","segments":[{"label":"all","value":1}]}`))
	if got, want := one.Description("one", true), "Pie chart: one, with 1 segment."; got != want {
		t.Errorf("pie of one: %q, want %q", got, want)
	}
	if got, want := SummaryText("area", "Daily Active Users"), "Area chart: Daily Active Users"; got != want {
		t.Errorf("summary %q, want %q", got, want)
	}
}

// View as table's and Download chart data's table (F8Uw) and TSV.
func TestTableAndTSV(t *testing.T) {
	d := ParseChart(json.RawMessage(`{"type":"bar","series":[{"name":"a\tb","data":[{"label":"x","value":1},{"label":"y","value":"two"}]},{"name":"plain","data":[{"label":"y","value":2.5}]}],"axis_config":{"categories":["x","y \"quoted\""]}}`))
	if d.Err != nil {
		t.Fatal(d.Err)
	}
	want := "Series\tx\t\"y \"\"quoted\"\"\"\n\"a\tb\"\t1\t\nplain\t\t"
	if got := d.TSV(); got != want {
		t.Errorf("TSV %q, want %q", got, want)
	}
	pie := ParseChart(json.RawMessage(`{"type":"pie","segments":[{"label":"a","value":1},{"label":"b"}]}`))
	if got, want := pie.TSV(), "Label\tValue\na\t1\nb\t"; got != want {
		t.Errorf("pie TSV %q, want %q", got, want)
	}
	kd := ParseChart(json.RawMessage(karimChart))
	h, rows := kd.Table()
	if strings.Join(h, ",") != "Series,Slack,Slick,Taut,Clack" || len(rows) != 1 || rows[0][2].Text != "1816" {
		t.Errorf("table %q %+v", h, rows)
	}
	for _, c := range []struct{ in, want string }{
		{"startup loading time", "startup-loading-time"},
		{"  Café — sales / Q4  ", "cafe-sales-q4"},
		{"日本語", "chart-data"},
		{"a--b  c", "a-b-c"},
	} {
		if got := FilenameSlug(c.in, "chart-data"); got != c.want {
			t.Errorf("FilenameSlug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The hover: the band under the pointer, the nearest point, the sector;
// the tooltip's rows.
func TestHoverAndTooltip(t *testing.T) {
	d := ParseChart(json.RawMessage(karimChart))
	var pal Palette
	l := Lay(Build(&d, &pal), 598, 360, func(s string, w Weight) float64 { return 7 * float64(len(s)) })
	if h := l.HoverAt(240, 200); h != 1 {
		t.Errorf("hover at x 240: %d, want 1 (Slick's band)", h)
	}
	if h := l.HoverAt(20, 200); h != -1 {
		t.Errorf("hover left of the grid: %d, want -1", h)
	}
	tip, ok := l.TooltipAt(1)
	if !ok || tip.Header != "Slick" || len(tip.Rows) != 1 || tip.Rows[0].Name != "time to window (ms)" || tip.Rows[0].Value != "1,816" {
		t.Errorf("tooltip %+v", tip)
	}
	tl := LayTooltip(tip, func(s string, w Weight) float64 { return 7 * float64(len(s)) })
	// 10px padding and 1px border each side; the header, a 10px gap, the row
	if tl.H != 2*11+13+10+13 {
		t.Errorf("tooltip height %v", tl.H)
	}
	if x, y := TipPlace(500, 300, tl.W, tl.H, 598, 360); x != 500-tl.W-20 || y != jsRound(300-tl.H-20) {
		t.Errorf("tooltip near the corner at %v,%v: flipped left and up", x, y)
	}
	pie := ParseChart(json.RawMessage(`{"type":"pie","segments":[{"label":"a","value":1},{"label":"b","value":1}]}`))
	pl := Lay(Build(&pie, &pal), 598, 360, nil)
	if h := pl.HoverAt(299+50, 180); h != 0 {
		t.Errorf("right of the pie's centre: segment %d, want 0", h)
	}
	if h := pl.HoverAt(299-50, 180); h != 1 {
		t.Errorf("left of the pie's centre: segment %d, want 1", h)
	}
	if h := pl.HoverAt(299, 180-150); h != -1 {
		t.Errorf("outside the pie: %d", h)
	}
	if h := pl.HoverAtHeld(299+147, 180, 0); h != 0 {
		t.Errorf("on a hovered segment's grown rim: %d, want 0", h)
	}
}
