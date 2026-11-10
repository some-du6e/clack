// Package i18n is Slack's string catalog: the per-locale translation
// bundles Slack's web client loads (gantry-v2-translations_<locale>.json on
// the public CDN), looked up the way Slack's I18n class does it (client-boot
// module C91c), and formatted with the ICU MessageFormat subset Slack's
// strings use (format-message, vendors module YZtm), plus Slack's per-locale
// number and date formatting.
//
// Clack carries only the English source strings (they are the keys); the
// translations are fetched without cookies from Slack's CDN and cached
// under <user cache>/clack/i18n. See docs/I18N.md.
package i18n

import "sort"

// EnUS is Slack's default locale (3t/7 Xn): its strings are the source
// text itself, so it has no bundle to load.
const EnUS = "en-US"

// Locales are Slack's 12 locales in its own order (3t/7 W_).
var Locales = []string{"de-DE", "en-GB", EnUS, "es-ES", "es-LA", "fr-FR", "ja-JP", "pt-BR", "ko-KR", "it-IT", "zh-CN", "zh-TW"}

// labels are the Language select's labels (3t/7 K4), each in its own
// language.
var labels = map[string]string{
	"de-DE": "Deutsch (Deutschland)",
	"en-GB": "English (UK)",
	"en-US": "English (US)",
	"es-ES": "Español (España)",
	"es-LA": "Español (Latinoamérica)",
	"fr-FR": "Français (France)",
	"ja-JP": "日本語",
	"pt-BR": "Português (Brasil)",
	"ko-KR": "한국어",
	"it-IT": "Italiano (Italia)",
	"zh-CN": "简体中文",
	"zh-TW": "繁體中文",
}

// Label is a locale's label in the Language select ("" if Slack has no
// such locale).
func Label(loc string) string { return labels[loc] }

// Valid reports whether loc is one of Slack's locales (3t/7 YI).
func Valid(loc string) bool { _, ok := labels[loc]; return ok }

// IsCJK reports Slack's CJK locales (3t/7 DU: ja-JP, zh-CN, zh-TW, ko-KR),
// whose strings get word joiners inside [[[…]]] groups.
func IsCJK(loc string) bool {
	switch loc {
	case "ja-JP", "zh-CN", "zh-TW", "ko-KR":
		return true
	}
	return false
}

// Option is one row of the Language select.
type Option struct{ Code, Label string }

// Options is the Language select's list the way LocalePref builds it:
// every locale as {label, value}, sorted by label (lodash sortBy, a
// UTF-16 code unit compare, which for these labels is byte order).
func Options() []Option {
	out := make([]Option, 0, len(Locales))
	for _, c := range Locales {
		out = append(out, Option{c, labels[c]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// Normalize maps a locale tag to one of Slack's: "es-419" and "es" are
// es-LA (3t/7 gB), anything else unknown is en-US.
func Normalize(loc string) string {
	switch loc {
	case "es-419", "es":
		return "es-LA"
	}
	if Valid(loc) {
		return loc
	}
	return EnUS
}
