package spell

import (
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Word is a word of a text: [Start, End) in runes, and its normalized form
// (what gets checked).
type Word struct {
	Start, End int
	Text       string
	// Parts are the pieces between contraction characters ("hello.world"
	// is hello and world): a word that's misspelled as a whole is still
	// correct when every part is (Chromium's IsValidContraction).
	Parts []string
}

// Chromium's SpellcheckWordIterator (components/spellcheck/renderer/
// spellcheck_worditerator.cc) with its Latin-script rule set: a word is a
// run of the language's letters and the digits 0-9 ($ALetter, numbers
// count as letters except in Hebrew and Arabic), with Extend and Format
// characters attached, chained through single MidLetter / MidNumLet
// characters (and U+0027) between letters. Everything else is skipped.
// Each word is NFKC-normalized and loses characters of other scripts
// (SpellcheckCharAttribute::OutputDefault keeps the script's and Common
// ones).

// isMid is $MidLetter | $MidNumLet | U+0027 (UAX #29 Word_Break).
func isMid(r rune) bool {
	switch r {
	case '\'', '.', ':', 0x00B7, 0x0387, 0x05F4, 0x2018, 0x2019, 0x2024, 0x2027,
		0xFE13, 0xFE52, 0xFE55, 0xFF07, 0xFF0E, 0xFF1A:
		return true
	}
	return false
}

// isExtend is Word_Break Extend | Format: combining marks, ZWJ/ZWNJ and
// format characters (soft hyphen, …).
func isExtend(r rune) bool {
	if r == 0x200C || r == 0x200D {
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc, unicode.Cf) && r != 0x200B && r != 0xFEFF
}

// Scripts maps the spellcheck languages to their script; others are Latin.
var langScript = map[string]*unicode.RangeTable{
	"bg": unicode.Cyrillic, "ru": unicode.Cyrillic, "uk": unicode.Cyrillic, "sr": unicode.Cyrillic, "tg": unicode.Cyrillic,
	"el": unicode.Greek, "he": unicode.Hebrew, "fa": unicode.Arabic, "hi": unicode.Devanagari, "ta": unicode.Tamil,
	"hy": unicode.Armenian, "ko": unicode.Hangul,
}

// ScriptFor returns the script Chromium's iterator uses for a language
// code ("en-US" → Latin).
func ScriptFor(lang string) *unicode.RangeTable {
	base := lang
	for i := 0; i < len(lang); i++ {
		if lang[i] == '-' {
			base = lang[:i]
			break
		}
	}
	if t, ok := langScript[base]; ok {
		return t
	}
	return unicode.Latin
}

// Tokenizer splits text into words for a set of scripts.
type Tokenizer struct {
	scripts []*unicode.RangeTable
	digits  bool
}

// NewTokenizer returns the tokenizer for the languages (Latin when none).
func NewTokenizer(langs []string) Tokenizer {
	t := Tokenizer{digits: true}
	seen := map[*unicode.RangeTable]bool{}
	for _, l := range langs {
		s := ScriptFor(l)
		if !seen[s] {
			seen[s] = true
			t.scripts = append(t.scripts, s)
		}
	}
	if len(t.scripts) == 0 {
		t.scripts = []*unicode.RangeTable{unicode.Latin}
	}
	if len(t.scripts) == 1 && (t.scripts[0] == unicode.Hebrew || t.scripts[0] == unicode.Arabic) {
		t.digits = false
	}
	return t
}

func (t Tokenizer) isLetter(r rune) bool {
	if r >= '0' && r <= '9' {
		return t.digits
	}
	for _, s := range t.scripts {
		if unicode.Is(s, r) {
			return unicode.IsLetter(r) || unicode.IsMark(r)
		}
	}
	return false
}

func (t Tokenizer) keep(r rune) bool {
	for _, s := range t.scripts {
		if unicode.Is(s, r) {
			return true
		}
	}
	return unicode.Is(unicode.Common, r) // digits, punctuation, symbols
}

// Words splits rs (rune offsets) into words. skip, if set, reports
// positions that aren't text to check (mentions, code blocks); they end a
// word like a space.
func (t Tokenizer) Words(rs []rune, skip func(i int) bool) []Word {
	var out []Word
	n := len(rs)
	at := func(i int) bool { return skip == nil || !skip(i) }
	for i := 0; i < n; {
		if !at(i) || !t.isLetter(rs[i]) {
			i++
			continue
		}
		start := i
		end := i
		for i < n && at(i) {
			r := rs[i]
			switch {
			case t.isLetter(r) || isExtend(r):
				i++
				end = i
				continue
			case isMid(r) && i+1 < n && at(i+1) && t.isLetter(rs[i+1]) && i > start && (t.isLetter(rs[i-1]) || isExtend(rs[i-1])):
				i++
				continue
			}
			break
		}
		i = end
		w := t.word(rs[start:end])
		if w.Text != "" {
			w.Start, w.End = start, end
			out = append(out, w)
		}
		if i == start {
			i++
		}
	}
	return out
}

func (t Tokenizer) word(rs []rune) Word {
	s := norm.NFKC.String(string(rs))
	var text []rune
	var parts []string
	var part []rune
	for _, r := range s {
		if !t.keep(r) {
			continue
		}
		text = append(text, r)
		if isMid(r) {
			if len(part) > 0 {
				parts = append(parts, string(part))
			}
			part = part[:0]
			continue
		}
		part = append(part, r)
	}
	if len(part) > 0 {
		parts = append(parts, string(part))
	}
	w := Word{Text: string(text)}
	if len(parts) > 1 {
		w.Parts = parts
	}
	return w
}
