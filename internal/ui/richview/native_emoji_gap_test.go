package richview

// A style's NativeEmojiGap (a canvas's, on a Mac) is the margin-right of
// Quip's native glyph span, span.emoji.emoji-in-canvas: 4px at 1x in
// Chrome ([@media (resolution<=1x)] body.mac.chrome), after each standard
// emoji, typed or by :name:. A custom emoji is an <img> with no margin, and
// at 2x the span has none. Synthetic images.

import (
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
)

func TestNativeEmojiGap(t *testing.T) {
	st := newTestStyle()
	st.NativeEmojiGap = 4
	em := func(n string) rt.Span { return rt.Span{Kind: rt.Emoji, ID: n, URL: "https://emoji.test/" + n + ".png"} }
	for _, c := range []struct {
		name  string
		d     rt.Doc
		scale float32
		gap   int
	}{
		{"typed standard emoji at 1x", doc(para(txt("🟢🟢🟢"))), 1, 4},
		{"standard :name: emoji at 1x", doc(para(em("smile"), em("smile"), em("smile"))), 1, 4},
		{"custom emoji at 1x", doc(para(em("party-cat"), em("party-cat"), em("party-cat"))), 1, 0},
		{"typed standard emoji at 2x", doc(para(txt("🟢🟢🟢"))), 2, 0},
		{"standard :name: emoji at 2x", doc(para(em("smile"), em("smile"), em("smile"))), 2, 0},
	} {
		v := New(c.d, Options{})
		v.relayout(st, 600, c.scale, c.scale)
		fs := emojiFrags(v)
		if len(fs) != 3 {
			t.Fatalf("%s: %d emoji frags, want 3", c.name, len(fs))
		}
		for i := 1; i < len(fs); i++ {
			if got := drawnRect(fs[i]).Min.X - drawnRect(fs[i-1]).Max.X; got != c.gap {
				t.Errorf("%s: %dpx after emoji %d, want %d", c.name, got, i-1, c.gap)
			}
		}
	}
}
