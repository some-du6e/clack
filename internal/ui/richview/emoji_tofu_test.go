package richview

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Message text (and every richview surface: topics, attachments, Block
// Kit text) draws a plain 😱 as Slack's emoji image on the app's own
// shaper: no .notdef and no system-font glyph (theme.StartTofuScan).
func TestEmojiTofuScan(t *testing.T) {
	st := NewStyle(theme.NewShaper(), noImages{}, LightPalette())
	stop := theme.StartTofuScan()
	v := layoutAt(st, doc(para(txt("message text 😱 then 🦜 unique 41c9"))), Options{}, 400)
	sc := stop()
	if sc.Glyphs == 0 {
		t.Fatal("no glyphs shaped")
	}
	if sc.Notdef > 0 || sc.Fallback > 0 {
		t.Errorf("%d .notdef and %d system-font glyphs", sc.Notdef, sc.Fallback)
	}
	var urls []string
	for _, f := range v.d.frags {
		if f.kind == fImage {
			urls = append(urls, f.url)
		}
	}
	if j := strings.Join(urls, " "); !strings.Contains(j, "1f631") || !strings.Contains(j, "1f99c") {
		t.Errorf("emoji images %q, want 1f631 and 1f99c", urls)
	}
}
