package i18n

import (
	"math"
	"strconv"
	"strings"
)

// Plural rules: what Intl.PluralRules(locale) answers in Chromium (ICU,
// CLDR 42+) for Slack's 12 locales. golang.org/x/text/feature/plural is
// CLDR 32, before the "many" category fr, es, it and pt got for exact
// millions (1000000, "1 million de …"), so the rules are written out here
// from CLDR's plurals.xml / ordinals.xml.

// operands are CLDR's i (integer digits) and v (visible fraction digits),
// as Intl reads a JS number (no trailing zeros, so w = v).
func operands(n float64) (i int64, v int, f int64) {
	n = math.Abs(n)
	s := strconv.FormatFloat(n, 'f', -1, 64)
	ip, fp, _ := strings.Cut(s, ".")
	i, _ = strconv.ParseInt(ip, 10, 64)
	v = len(fp)
	if fp != "" {
		f, _ = strconv.ParseInt(fp, 10, 64)
	}
	return
}

func lang(loc string) string { l, _, _ := strings.Cut(loc, "-"); return l }

// Cardinal is the plural category of n in loc ("one", "many", "other").
func Cardinal(loc string, n float64) string {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return "other"
	}
	i, v, _ := operands(n)
	million := v == 0 && i != 0 && i%1000000 == 0
	switch lang(loc) {
	case "en", "de":
		if i == 1 && v == 0 {
			return "one"
		}
	case "es":
		if math.Abs(n) == 1 {
			return "one"
		}
		if million {
			return "many"
		}
	case "it":
		if i == 1 && v == 0 {
			return "one"
		}
		if million {
			return "many"
		}
	case "fr":
		if i == 0 || i == 1 {
			return "one"
		}
		if million {
			return "many"
		}
	case "pt": // pt-BR ("pt"): i = 0..1
		if i == 0 || i == 1 {
			return "one"
		}
		if million {
			return "many"
		}
	}
	return "other" // ja, ko, zh
}

// Ordinal is the ordinal category of n (selectordinal).
func Ordinal(loc string, n float64) string {
	if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
		return "other"
	}
	k := int64(math.Abs(n))
	switch lang(loc) {
	case "en":
		switch {
		case k%10 == 1 && k%100 != 11:
			return "one"
		case k%10 == 2 && k%100 != 12:
			return "two"
		case k%10 == 3 && k%100 != 13:
			return "few"
		}
	case "fr":
		if k == 1 {
			return "one"
		}
	case "it":
		if k == 11 || k == 8 || k == 80 || k == 800 {
			return "many"
		}
	}
	return "other"
}
