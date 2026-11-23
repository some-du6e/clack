package chart

import (
	"math"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/i18n"
)

// nsBlockKit is Slack's new I18n("block_kit"): the chart's names, its
// description and its menu.
var nsBlockKit = i18n.NS("block_kit")

// TypeName is getChartTypeName (client-boot /2Vi).
func TypeName(typ string) string {
	switch typ {
	case "line":
		return nsBlockKit.T("Line chart")
	case "bar":
		return nsBlockKit.T("Bar chart")
	case "area":
		return nsBlockKit.T("Area chart")
	case "pie":
		return nsBlockKit.T("Pie chart")
	}
	return nsBlockKit.T("Chart")
}

// SummaryText is convertChartToPlainText (client-boot 5Sel), the block's
// text in a message summary: "{chartType}: {title}".
func SummaryText(typ, title string) string {
	return nsBlockKit.T("{chartType}: {title}", "chartType", TypeName(typ), "title", title)
}

// descInput is what buildChartDescription reads besides the title.
type descInput struct {
	xLabel, yLabel any
	hasX, hasY     bool // Vz: null and undefined stay undefined
	names          []jsName
	count          int // series.length or segments.length
}

// Description is buildChartDescription (mhRP): the chart's aria-label
// unless the block brings its own. title is the block's raw title.
func (d *Data) Description(title any, hasTitle bool) string {
	t := jsString(title, hasTitle) // SafeString: `${title}`
	switch d.Type {
	case "pie":
		return d.pieDescription(t)
	case "line", "bar", "area":
		list := formatSeriesList(d.desc.names)
		args := []any{"chartType", TypeName(d.Type), "title", t, "numSeries", d.desc.count, "seriesList", list}
		if d.desc.hasX {
			args = append(args, "xLabel", jsString(d.desc.xLabel, true))
		}
		if d.desc.hasY {
			args = append(args, "yLabel", jsString(d.desc.yLabel, true))
		}
		if d.desc.count == 1 {
			return nsBlockKit.T("{chartType}: {title}. The X axis is {xLabel}, the Y axis is {yLabel}. There is {numSeries} series: {seriesList}.", args...)
		}
		return nsBlockKit.T("{chartType}: {title}. The X axis is {xLabel}, the Y axis is {yLabel}. There are {numSeries} series: {seriesList}.", args...)
	}
	return t
}

// formatSeriesList is mhRP's formatSeriesList over the raw names: one as
// it is, two "{first} and {second}", more "{list}, and {last}" (the list
// joined with ", ", where null and undefined are empty).
func formatSeriesList(names []jsName) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return jsString(names[0].v, names[0].ok)
	case 2:
		return nsBlockKit.T("{first} and {second}", "first", jsString(names[0].v, names[0].ok), "second", jsString(names[1].v, names[1].ok))
	}
	parts := make([]string, len(names)-1)
	for i, n := range names[:len(names)-1] {
		if n.ok && n.v != nil {
			parts[i] = jsString(n.v, true)
		}
	}
	last := names[len(names)-1]
	return nsBlockKit.T("{list}, and {last}", "list", strings.Join(parts, ", "), "last", jsString(last.v, last.ok))
}

func formatList(items []string) string {
	names := make([]jsName, len(items))
	for i, s := range items {
		names[i] = jsName{v: s, ok: true}
	}
	return formatSeriesList(names)
}

func (d *Data) pieDescription(title string) string {
	n := d.desc.count
	base := nsBlockKit.T("{chartType}: {title}, with {numSegments, plural, one {# segment} other {# segments}}.",
		"chartType", TypeName("pie"), "title", title, "numSegments", n)
	segs := d.raw.segments
	if n > 1 && n <= 4 {
		sum := segs.sum()
		items := make([]string, len(segs))
		for i, s := range segs {
			items[i] = jsString(s.label, s.hasLabel) + " (" + JSNumber(percentOf(s, sum)) + "%)"
		}
		return nsBlockKit.T("{baseDescription} Segments: {segmentList}.", "baseDescription", base, "segmentList", formatList(items))
	}
	if n > 4 {
		sum := segs.sum()
		hi, lo := segs[0], segs[0]
		for _, s := range segs {
			if jsGreater(s.jv(), hi.jv()) {
				hi = s
			}
			if jsGreater(lo.jv(), s.jv()) {
				lo = s
			}
		}
		more := nsBlockKit.T("The largest segment is {largestLabel} at {largestPercent}%, the smallest is {smallestLabel} at {smallestPercent}%.",
			"largestLabel", jsString(hi.label, hi.hasLabel), "largestPercent", JSNumber(percentOf(hi, sum)),
			"smallestLabel", jsString(lo.label, lo.hasLabel), "smallestPercent", JSNumber(percentOf(lo, sum)))
		return base + " " + more
	}
	return base
}

type rawPoints []rawPoint

func (p rawPoint) jv() jsv { return jsv{p.value, p.hasValue} }

// sum is segments.reduce((e, a) => e + a.value, 0), with JS's +.
func (ps rawPoints) sum() jsv {
	acc := jsv{float64(0), true}
	for _, p := range ps {
		acc = jsAdd(acc, p.jv())
	}
	return acc
}

// percentOf is `e > 0 ? Math.round(a.value / e * 100) : 0`.
func percentOf(p rawPoint, sum jsv) float64 {
	if !jsGreater(sum, jsv{float64(0), true}) {
		return 0
	}
	return jsRound(jsToNumber(p.jv()) / jsToNumber(sum) * 100)
}

// jsv is a JS value: a JSON value, or undefined (ok false).
type jsv struct {
	v  any
	ok bool
}

// jsPrimitive is ToPrimitive for JSON values: arrays and objects become
// their String().
func jsPrimitive(x jsv) jsv {
	switch x.v.(type) {
	case []any, map[string]any:
		return jsv{jsString(x.v, true), true}
	}
	return x
}

// jsToNumber is ToNumber.
func jsToNumber(x jsv) float64 {
	x = jsPrimitive(x)
	if !x.ok {
		return math.NaN()
	}
	switch t := x.v.(type) {
	case nil:
		return 0
	case bool:
		if t {
			return 1
		}
		return 0
	case float64:
		return t
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			if n, err := strconv.ParseUint(s[2:], 16, 64); err == nil {
				return float64(n)
			}
		}
		switch s {
		case "Infinity", "+Infinity":
			return math.Inf(1)
		case "-Infinity":
			return math.Inf(-1)
		}
		return math.NaN()
	}
	return math.NaN()
}

// jsAdd is a + b.
func jsAdd(a, b jsv) jsv {
	a, b = jsPrimitive(a), jsPrimitive(b)
	_, as := a.v.(string)
	_, bs := b.v.(string)
	if (as && a.ok) || (bs && b.ok) {
		return jsv{jsString(a.v, a.ok) + jsString(b.v, b.ok), true}
	}
	return jsv{jsToNumber(a) + jsToNumber(b), true}
}

// jsGreater is a > b.
func jsGreater(a, b jsv) bool {
	a, b = jsPrimitive(a), jsPrimitive(b)
	as, aok := a.v.(string)
	bs, bok := b.v.(string)
	if aok && bok && a.ok && b.ok {
		return utf16Less(bs, as)
	}
	x, y := jsToNumber(a), jsToNumber(b)
	return x > y // NaN compares false
}

func utf16Less(a, b string) bool {
	ua, ub := []rune(a), []rune(b)
	for i := 0; i < len(ua) && i < len(ub); i++ {
		ca, cb := utf16Units(ua[i]), utf16Units(ub[i])
		for j := 0; j < len(ca) && j < len(cb); j++ {
			if ca[j] != cb[j] {
				return ca[j] < cb[j]
			}
		}
		if len(ca) != len(cb) {
			return len(ca) < len(cb)
		}
	}
	return len(ua) < len(ub)
}

func utf16Units(r rune) []uint16 {
	if r < 0x10000 {
		return []uint16{uint16(r)}
	}
	r -= 0x10000
	return []uint16{uint16(0xd800 + (r >> 10)), uint16(0xdc00 + (r & 0x3ff))}
}

// jsRound is Math.round: halves round up.
func jsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	return math.Floor(x + 0.5)
}
