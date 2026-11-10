package slackc

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Plugin component text (Slack's DOM in slackc) draws a 😱 as Slack's
// emoji image: no .notdef, no system-font glyph (theme.StartTofuScan).
func TestEmojiTofuScan(t *testing.T) {
	stop := theme.StartTofuScan()
	v, _ := layoutJS(t, `{"t":"div","k":1,"p":{"id":"p"},"c":[{"t":"#","v":"plugin label 😱 with 🦜 emoji, unique 7f3a"}]}`, 400, 400)
	st := stop()
	if st.Glyphs == 0 {
		t.Fatal("no glyphs shaped")
	}
	if st.Notdef > 0 || st.Fallback > 0 {
		t.Errorf("%d .notdef and %d system-font glyphs", st.Notdef, st.Fallback)
	}
	var urls []string
	var walk func(list []*box)
	walk = func(list []*box) {
		for _, b := range list {
			if b.tc != nil {
				for _, tk := range b.tc.toks {
					if tk.kind == iEmoji {
						urls = append(urls, tk.url)
					}
				}
			}
			walk(b.kids)
		}
	}
	walk(v.top)
	joined := strings.Join(urls, " ")
	if !strings.Contains(joined, "1f631") || !strings.Contains(joined, "1f99c") {
		t.Errorf("emoji images %q, want 1f631 and 1f99c", urls)
	}
}
