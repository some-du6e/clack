// Package acrank is Slack's autocomplete ranking for the composer's @ # :
// and / lists: the Searcher's matcher, sorter, frecency and linear model,
// ported from the web client (docs/AUTOCOMPLETE-RULES.md has the module
// ids and the evidence for every rule). It is pure: callers pass the
// candidates, the frecency cache and the model weights.
package acrank

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Normalize is normalizeStringForMatching (pRCT): strings with no
// non-ASCII, uppercase, leading/trailing or double spaces come back as
// they are; others are trimmed, lowercased, deburred (i18n_deburr: Latin
// accents, Cyrillic transliterated) and whitespace runs collapse to one
// space.
func Normalize(s string) string {
	if s == "" {
		return ""
	}
	if !needsNormalize(s) {
		return s
	}
	t := strings.TrimFunc(s, unicode.IsSpace)
	if t == "" {
		return ""
	}
	t = Deburr(strings.ToLower(t))
	var b strings.Builder
	space := false
	for _, r := range t {
		if unicode.IsSpace(r) {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// needsNormalize is pRCT's /[^\x20-\x7f]|[A-Z]|^ | $| {2}/.
func needsNormalize(s string) bool {
	if s[0] == ' ' || s[len(s)-1] == ' ' || strings.Contains(s, "  ") {
		return true
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7f || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// FormatQuery is formatQuery (ZNeq) with every trim on: a leading
// ":" "@" "#" "/" or "+" (and their full-width forms) is dropped.
func FormatQuery(s string) string { return formatQuery(s, true, true) }

// formatQuery: emoji trims ":", doc trims "+"; members, channels and
// commands are always trimmed (the Searcher passes shouldTrimEmoji and
// shouldTrimDocument false).
func formatQuery(s string, emoji, doc bool) string {
	if s == "" {
		return ""
	}
	r, n := utf8.DecodeRuneInString(s)
	switch r {
	case '@', '＠', '#', '＃', '/', '／':
		return s[n:]
	case ':':
		if emoji {
			return s[n:]
		}
	case '+', '＋':
		if doc {
			return s[n:]
		}
	}
	return s
}

// SearcherQuery is what the Searcher matches: formatQuery without the
// emoji and document trims (Searcher.search).
func SearcherQuery(s string) string { return formatQuery(s, false, false) }

// Deburr is i18n_deburr (tsv0): lodash deburr (Latin-1 Supplement and
// Latin Extended-A letters to ASCII, combining marks dropped) then
// Cyrillic to Latin.
func Deburr(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 0x300 && r <= 0x36f, r >= 0xfe20 && r <= 0xfe2f, r >= 0x20d0 && r <= 0x20ff:
			continue // combining marks (lodash reComboMark)
		case r >= 0xc0 && r <= 0x17f:
			if m, ok := deburred[r]; ok {
				b.WriteString(m)
				continue
			}
		case r >= 0x400 && r <= 0x4ff:
			if m, ok := cyrillic[r]; ok {
				b.WriteString(m)
				continue
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// deburred is lodash's deburredLetters.
var deburred = func() map[rune]string {
	m := map[rune]string{}
	add := func(to string, from string) {
		for _, r := range from {
			m[r] = to
		}
	}
	add("A", "ÀÁÂÃÄÅĀĂĄ")
	add("a", "àáâãäåāăą")
	add("C", "ÇĆĈĊČ")
	add("c", "çćĉċč")
	add("D", "ÐĎĐ")
	add("d", "ðďđ")
	add("E", "ÈÉÊËĒĔĖĘĚ")
	add("e", "èéêëēĕėęě")
	add("I", "ÌÍÎÏĨĪĬĮİ")
	add("i", "ìíîïĩīĭįı")
	add("N", "ÑŃŅŇ")
	add("n", "ñńņňŉ")
	add("O", "ÒÓÔÕÖØŌŎŐ")
	add("o", "òóôõöøōŏő")
	add("U", "ÙÚÛÜŨŪŬŮŰŲ")
	add("u", "ùúûüũūŭůűų")
	add("Y", "ÝŶŸ")
	add("y", "ýÿŷ")
	add("Ae", "Æ")
	add("ae", "æ")
	add("Th", "Þ")
	add("th", "þ")
	add("ss", "ß")
	add("G", "ĜĞĠĢ")
	add("g", "ĝğġģ")
	add("H", "ĤĦ")
	add("h", "ĥħ")
	add("J", "Ĵ")
	add("j", "ĵ")
	add("K", "Ķ")
	add("k", "ķĸ")
	add("L", "ĹĻĽĿŁ")
	add("l", "ĺļľŀł")
	add("R", "ŔŖŘ")
	add("r", "ŕŗř")
	add("S", "ŚŜŞŠ")
	add("s", "śŝşšſ")
	add("T", "ŢŤŦ")
	add("t", "ţťŧ")
	add("W", "Ŵ")
	add("w", "ŵ")
	add("Z", "ŹŻŽ")
	add("z", "źżž")
	add("IJ", "Ĳ")
	add("ij", "ĳ")
	add("Oe", "Œ")
	add("oe", "œ")
	return m
}()

// cyrillic is tsv0's cyrillicToLatin.
var cyrillic = func() map[rune]string {
	up := []string{"A", "B", "V", "G", "D", "E", "Zh", "Z", "I", "J", "K", "L", "M", "N", "O", "P", "R", "S", "T", "U", "F", "H", "C", "Ch", "Sh", "Sch", "", "Y", "", "Je", "Ju", "Ja"}
	m := map[rune]string{'Ё': "Jo", 'ё': "jo"}
	for i, s := range up {
		m['А'+rune(i)] = s
		m['а'+rune(i)] = strings.ToLower(s)
	}
	return m
}()
