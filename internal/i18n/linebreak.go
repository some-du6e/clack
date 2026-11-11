package i18n

import (
	"strings"
	"unicode"
)

// LineChunks cuts a word where a line may break inside it: between CJK
// characters (each its own chunk), not before closing punctuation or a
// small kana and not after opening punctuation (kinsoku, as Blink breaks
// ja-JP text). Other text stays whole. Hand-flowed text (the channel
// notification modal, the Add emoji intro, the tab rail's labels) wraps
// Japanese with it.
func LineChunks(word string) []string {
	var out []string
	cur := ""
	glue := false // the chunk so far ends in opening punctuation
	prevCJK := false
	for _, r := range word {
		cjk := IsCJKRune(r)
		switch {
		case cur == "":
			cur = string(r)
		case glue || strings.ContainsRune(cjkNoBreakBefore, r) || !cjk && !prevCJK:
			cur += string(r)
		default:
			out = append(out, cur)
			cur = string(r)
		}
		glue = strings.ContainsRune(cjkNoBreakAfter, r)
		prevCJK = cjk
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

const (
	cjkNoBreakBefore = "、。，．・：；？！ー）」』】〕〉》］｝ぁぃぅぇぉっゃゅょゎァィゥェォッャュョヮヵヶ々"
	cjkNoBreakAfter  = "（「『【〔〈《［｛"
)

// IsCJKRune: ideographs, kana, hangul and CJK punctuation.
func IsCJKRune(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) ||
		r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef || r == 0x30fc
}
