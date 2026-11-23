package messages

import "testing"

// TestThreadsAuditScrollbarColours: the scrollbar's bar is Slack's
// .c-scrollbar__bar, rgba(var(--sk_primary_foreground), .52), in each
// theme (kit.css: 29,28,29 light, 209,210,211 dark), the same under the
// pointer (hover only shows it, .c-scrollbar:hover).
func TestThreadsAuditScrollbarColours(t *testing.T) {
	for _, c := range []struct {
		name string
		got  colors
		fg   uint32
	}{{"light", lightColors, 0x1d1c1d}, {"dark", darkColors, 0xd1d2d3}} {
		want := rgba(c.fg, 0.52)
		if c.got.ScrollThumb != want || c.got.ScrollThumbHover != want {
			t.Errorf("%s: the bar is %v, %v under the pointer; Slack's is %v both", c.name, c.got.ScrollThumb, c.got.ScrollThumbHover, want)
		}
	}
}
