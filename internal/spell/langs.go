package spell

import (
	"os"
	"sort"
	"strings"
)

// Labels are the desktop app's language names (Slack 4.53.276
// dist/main.bundle.cjs, the SM table GET_AVAILABLE_LANGUAGES answers
// with: session.availableSpellCheckerLanguages that have a label).
var Labels = map[string]string{
	"af": "Afrikaans", "bg": "Bulgarian - български", "ca": "Catalan - Català", "cs": "Czech - čeština",
	"cy": "Welsh - Cymraeg", "da": "Danish - Dansk", "de": "German - Deutsch", "de-DE": "German - Deutsch (Deutschland)",
	"el": "Greek - ελληνικά", "en": "English", "en-AU": "English (Australian)", "en-CA": "English (Canadian)",
	"en-GB": "English (UK)", "en-GB-oxendict": "English (United Kingdom, Oxford English Dictionary spelling)",
	"en-US": "English (US)", "es": "Spanish - Español", "es-419": "Spanish - Español (America latina)",
	"es-AR": "Spanish - Español (Argentina)", "es-ES": "Spanish - Español (España)", "es-MX": "Spanish - Español (Mexico)",
	"es-US": "Spanish - Español (Estados Unidos)", "et": "Estonian - Eesti Keel", "fa": "Persian - فارسى",
	"fo": "Faroese - Føroyskt", "fr": "French - Français", "fr-FR": "French - Français (France)", "he": "Hebrew - עברי",
	"hi": "Hindi - हिंदी", "hr": "Croatian - Hrvatski", "hu": "Hungarian - Magyar", "hy": "Armenian - Հայերէն",
	"id": "Indonesian - Bahasa Indonesia", "it": "Italian - Italiano", "it-IT": "Italian - Italiano (Italia)",
	"ko": "Korean - 한국어", "lt": "Lithuanian - Lietuvių Kalba", "lv": "Latvian - Latviešu Valoda", "nb": "Norwegian - Norsk",
	"nl": "Dutch - Nederlands", "pl": "Polish - Polski", "pt": "Portuguese - Português", "pt-BR": "Portuguese - Português (Brasil)",
	"pt-PT": "Portuguese - Português (Portugal)", "ro": "Romanian - Română", "ru": "Russian - Русский язык",
	"sh": "Serbo-Croatian - srpskohrvatski", "sk": "Slovak - Slovenčina", "sl": "Slovenian - Slovenščina", "sq": "Albanian - Shqip",
	"sr": "Serbian - cрпски", "sv": "Swedish - Svenska", "ta": "Tamil - தமிழ்", "tg": "Tajik - тоҷики", "tr": "Turkish - Türkçe",
	"uk": "Ukrainian - Українська", "vi": "Vietnamese - tiếng việt",
}

// regions is Chromium's kSupportedSpellCheckerLanguages: each language
// code and the dictionary it uses (components/spellcheck/common/
// spellcheck_common.cc).
var regions = map[string]string{
	"af": "af-ZA", "bg": "bg-BG", "ca": "ca-ES", "cs": "cs-CZ", "cy": "cy-GB", "da": "da-DK", "de": "de-DE", "de-DE": "de-DE",
	"el": "el-GR", "en": "en-US", "en-AU": "en-AU", "en-CA": "en-CA", "en-GB": "en-GB", "en-GB-oxendict": "en-GB-oxendict",
	"en-US": "en-US", "es": "es-ES", "es-419": "es-ES", "es-AR": "es-ES", "es-ES": "es-ES", "es-MX": "es-ES", "es-US": "es-ES",
	"et": "et-EE", "fa": "fa-IR", "fo": "fo-FO", "fr": "fr-FR", "fr-FR": "fr-FR", "he": "he-IL", "hi": "hi-IN", "hr": "hr-HR",
	"hu": "hu-HU", "hy": "hy", "id": "id-ID", "it": "it-IT", "it-IT": "it-IT", "ko": "ko", "lt": "lt-LT", "lv": "lv-LV",
	"nb": "nb-NO", "nl": "nl-NL", "pl": "pl-PL", "pt": "pt-BR", "pt-BR": "pt-BR", "pt-PT": "pt-PT", "ro": "ro-RO", "ru": "ru-RU",
	"sh": "sh", "sk": "sk-SK", "sl": "sl-SI", "sq": "sq", "sr": "sr", "sv": "sv-SE", "ta": "ta-IN", "tg": "tg-TG", "tr": "tr-TR",
	"uk": "uk-UA", "vi": "vi-VN",
}

// versions is GetVersionedFileName's kSpecialVersionString; the rest are
// "-3-0".
var versions = map[string]string{
	"tr-TR": "-4-0", "tg-TG": "-5-0", "cy-GB": "-1-0", "hy": "-1-0", "sh": "-4-0", "sr": "-4-0", "fa-IR": "-9-0",
	"en-AU": "-10-1", "en-CA": "-10-1", "en-GB": "-10-1", "en-GB-oxendict": "-10-1", "en-US": "-10-1", "uk-UA": "-5-0",
}

// DictionaryURL is where Slack's desktop app tells Chromium to download
// Hunspell dictionaries (main.bundle.cjs: setSpellCheckerDictionaryDownloadURL
// with eF.KB for the commercial environment; GovSlack and military
// builds use slack-edge-gov.com and slack-edge.mil).
const DictionaryURL = "https://spellcheck.slack-edge.com/dictionaries_v2/"

// BDicFile is the dictionary file for a language code ("en-US" →
// "en-US-10-1.bdic").
func BDicFile(lang string) string {
	r, ok := regions[lang]
	if !ok {
		r = lang
	}
	v, ok := versions[r]
	if !ok {
		v = "-3-0"
	}
	return r + v + ".bdic"
}

// Supported reports whether lang is a language Slack lists.
func Supported(lang string) bool { _, ok := Labels[lang]; return ok }

// Available is the language list Preferences offers on Windows and Linux:
// every labelled language, as {code, label}, sorted by label like Slack's
// multi-select (sortBy label).
func Available() [][2]string {
	out := make([][2]string, 0, len(Labels))
	for k, v := range Labels {
		out = append(out, [2]string{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][1] < out[j][1] })
	return out
}

// DefaultLanguages is what Chromium enables before the user picks: the
// language of the OS locale (GetCorrespondingSpellCheckLanguage of the
// app locale), else en-US.
func DefaultLanguages() []string {
	return []string{correspond(osLocale())}
}

// correspond maps a locale ("de_DE.UTF-8", "pt-BR", "fr") to a language
// code Chromium supports.
func correspond(loc string) string {
	loc = strings.SplitN(loc, ".", 2)[0]
	loc = strings.SplitN(loc, "@", 2)[0]
	loc = strings.ReplaceAll(loc, "_", "-")
	if loc == "" || loc == "C" || loc == "POSIX" {
		return "en-US"
	}
	if _, ok := regions[loc]; ok {
		return loc
	}
	best := "" // a region ("de-AT" isn't one): the first language using it
	for k, v := range regions {
		if strings.EqualFold(v, loc) && (best == "" || k < best) {
			best = k
		}
	}
	if best != "" {
		return best
	}
	base := strings.SplitN(loc, "-", 2)[0]
	if _, ok := regions[base]; ok {
		return base
	}
	return "en-US"
}

// osLocale is the user's locale: LC_ALL, LC_MESSAGES, LANG (Linux), or
// the platform's own (locale_*.go).
func osLocale() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return platformLocale()
}
