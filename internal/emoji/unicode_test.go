package emoji

import "testing"

func TestMatchUnicode(t *testing.T) {
	for _, c := range []struct {
		in, name string
		n        int // runes consumed; 0 = no match
	}{
		{"😀x", "grinning", 1},
		{"👍", "+1", 1},
		{"👍🏽!", "+1::skin-tone-4", 2},
		{"❤️ ", "heart", 2},
		{"❤ ", "", 0}, // text-default without VS16 stays text, like Slack
		{"©", "", 0},
		{"©️", "copyright", 2},
		{"👨‍👩‍👧‍👦", "man-woman-girl-boy", 7},
		{"🇺🇸", "us", 2},
		{"🏳️‍🌈", "rainbow-flag", 4},
		{"🏳‍🌈", "", 0}, // a bare text-default 🏳 starts no emoji
		{"1️⃣", "one", 3},
		{"a", "", 0},
		{"1", "", 0},
	} {
		name, end, ok := MatchUnicode([]rune(c.in), 0)
		if c.n == 0 {
			if ok {
				t.Errorf("%q matched %q", c.in, name)
			}
			continue
		}
		if !ok || name != c.name || end != c.n {
			t.Errorf("%q = %q, %d, %v; want %q, %d", c.in, name, end, ok, c.name, c.n)
		}
		// the name resolves back to the same emoji
		if _, txt, ok := Standard(name); !ok || txt == "" {
			t.Errorf("%q: Standard(%q) failed", c.in, name)
		}
	}
}
