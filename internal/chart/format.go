package chart

import (
	"math"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/i18n"
)

// numberLocale is the locale Slack's Intl.NumberFormat(undefined) and
// toLocaleString() format with: the browser's, en-US on Slack's desktop
// app for an English system.
const numberLocale = "en-US"

// FormatYAxisTick is k+pA's formatYAxisTick: from 10,000 up compact
// notation with one fraction digit at most ("12.5K", "1.3M"), else
// toLocaleString() ("1,000", "0.25").
func FormatYAxisTick(v float64) string {
	if math.Abs(v) >= 1e4 {
		return compact(v)
	}
	return i18n.FormatNumber(numberLocale, v)
}

// compact is new Intl.NumberFormat("en-US", {notation: "compact",
// maximumFractionDigits: 1}).format(v) for |v| from 1,000: K, M, B and T
// (no larger unit: 1e15 is "1000T"), rounded half away from zero, a
// rounding that reaches the next unit taking it ("999,950" is "1M").
func compact(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return i18n.FormatNumber(numberLocale, v)
	}
	units := []string{"", "K", "M", "B", "T"}
	a := math.Abs(v)
	u := 0
	for u < len(units)-1 && a >= math.Pow10(3*(u+1)) {
		u++
	}
	r := round1(a / math.Pow10(3*u))
	if r >= 1000 && u < len(units)-1 {
		u++
		r = round1(a / math.Pow10(3*u))
	}
	// compact notation groups from five integer digits (ICU's
	// minimumGroupingDigits 2): "1000T", "25,000T"
	s := strconv.FormatFloat(r, 'f', -1, 64)
	ip, fp, hasFp := strings.Cut(s, ".")
	if len(ip) >= 5 {
		ip = groupThousands(ip)
	}
	s = ip
	if hasFp {
		s += "." + fp
	}
	if v < 0 && r != 0 {
		s = "-" + s
	}
	return s + units[u]
}

// round1 rounds half away from zero to one decimal, on the decimal value
// (ICU rounds the number as printed, not its binary neighbour).
func round1(x float64) float64 {
	return roundTo(math.Floor(roundTo(x*10, 6)+0.5)/10, 1)
}

// AddCommas is ECharts' formatUtil.addCommas on a number's string: commas
// between the integer part's thousands, the decimals untouched.
func AddCommas(v float64) string {
	s := JSNumber(v)
	ip, fp, hasFp := strings.Cut(s, ".")
	ip = groupThousands(ip)
	if hasFp {
		return ip + "." + fp
	}
	return ip
}

// groupThousands is addCommas' /(\d{1,3})(?=(?:\d{3})+(?!\d))/g over the
// integer part: a comma before every three digits that end a digit run.
func groupThousands(s string) string {
	var b strings.Builder
	runStart := -1
	flush := func(end int) {
		run := s[runStart:end]
		for i, c := range run {
			if i > 0 && (len(run)-i)%3 == 0 {
				b.WriteByte(',')
			}
			b.WriteRune(c)
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			if runStart < 0 {
				runStart = i
			}
			continue
		}
		if runStart >= 0 {
			flush(i)
			runStart = -1
		}
		b.WriteByte(c)
	}
	if runStart >= 0 {
		flush(len(s))
	}
	return b.String()
}

// TooltipValue is the tooltip's value for v (makeValueReadable, type
// number): AddCommas, "-" for none.
func TooltipValue(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "-"
	}
	return AddCommas(v)
}
