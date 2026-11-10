package i18n

import (
	"math"
	"strconv"
	"strings"
)

// numberSyms is what Intl.NumberFormat(locale) uses by default (CLDR
// latn symbols, as Chromium's ICU has them): the group and decimal
// separators and minimumGroupingDigits (es: "1234" but "12.345").
type numberSyms struct {
	group, decimal string
	minGroup       int
	percent        string // between the number and %
}

func symsFor(loc string) numberSyms {
	switch lang(loc) {
	case "de", "it", "pt":
		return numberSyms{".", ",", 1, " "}
	case "es":
		return numberSyms{".", ",", 2, " "}
	case "fr":
		return numberSyms{" ", ",", 1, " "}
	}
	return numberSyms{",", ".", 1, ""} // en, ja, ko, zh
}

// FormatNumber is new Intl.NumberFormat(loc).format(v): grouped, at most
// three fraction digits, rounded half away from zero.
func FormatNumber(loc string, v float64) string { return formatDecimal(symsFor(loc), v, 3) }

func formatDecimal(s numberSyms, v float64, maxFrac int) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "∞"
	case math.IsInf(v, -1):
		return "-∞"
	}
	neg := v < 0
	v = math.Abs(v)
	p := math.Pow(10, float64(maxFrac))
	v = math.Floor(v*p+0.5) / p
	str := strconv.FormatFloat(v, 'f', -1, 64)
	ip, fp, _ := strings.Cut(str, ".")
	if len(fp) > maxFrac {
		fp = fp[:maxFrac]
	}
	fp = strings.TrimRight(fp, "0")
	var b strings.Builder
	if neg && (ip != "0" || fp != "") {
		b.WriteByte('-')
	}
	if len(ip) >= 4 && len(ip) >= 3+s.minGroup {
		first := len(ip) % 3
		if first == 0 {
			first = 3
		}
		b.WriteString(ip[:first])
		for i := first; i < len(ip); i += 3 {
			b.WriteString(s.group)
			b.WriteString(ip[i : i+3])
		}
	} else {
		b.WriteString(ip)
	}
	if fp != "" {
		b.WriteString(s.decimal)
		b.WriteString(fp)
	}
	return b.String()
}

// formatNumberStyle is {v, number[, style]}: integer rounds, percent
// multiplies by 100 (no fraction digits).
func formatNumberStyle(loc string, v float64, style string) string {
	s := symsFor(loc)
	switch style {
	case "integer":
		return formatDecimal(s, v, 0)
	case "percent":
		return formatDecimal(s, v*100, 0) + s.percent + "%"
	}
	return formatDecimal(s, v, 3)
}
