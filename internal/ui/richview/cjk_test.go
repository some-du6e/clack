package richview

import (
	"testing"
	"unicode"
)

// TestIsCJKEveryRune: isCJK's fast rejection below U+1100 changes nothing:
// for every rune it's the four scripts' tables.
func TestIsCJKEveryRune(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		want := unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
		if got := isCJK(r); got != want {
			t.Fatalf("isCJK(%U) = %v, the tables say %v", r, got, want)
		}
	}
}
