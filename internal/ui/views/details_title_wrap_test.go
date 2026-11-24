package views

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestCDTitleLines: the channel details title wraps as Slack's
// .p-about_modal__title .p-view_header__channel_title does (white-space:
// break-spaces, word-break: break-word): at spaces, the space kept on the
// line it ends, and a name too long for a whole line cut where the line
// is full. Clack drew it on one line, cut with an ellipsis. A monospace
// width (10px a character) keeps the expected lines exact.
func TestCDTitleLines(t *testing.T) {
	mono := func(s string) int { return 10 * utf8.RuneCountInString(s) }
	for _, c := range []struct {
		in   string
		tw   int
		want []string
	}{
		{"general", 200, []string{"general"}},
		{"Avery Admin, Robin Example, Sam Lee", 200, []string{"Avery Admin, Robin ", "Example, Sam Lee"}},
		{"agents-lab-release-coordination-and-planning", 200, []string{"agents-lab-release-", "coordination-and-", "planning"}},
		{"averyveryveryverylongchannelnamewithnobreaks", 200, []string{"averyveryveryverylon", "gchannelnamewithnobr", "eaks"}},
	} {
		got := cdTitleLines(c.in, c.tw, mono)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("cdTitleLines(%q, %d) = %q, want %q", c.in, c.tw, got, c.want)
		}
		if strings.Join(got, "") != c.in {
			t.Errorf("cdTitleLines(%q) lost text: %q", c.in, got)
		}
		for _, ln := range got {
			if mono(strings.TrimRight(ln, " ")) > c.tw {
				t.Errorf("cdTitleLines(%q): line %q is wider than %d", c.in, ln, c.tw)
			}
		}
	}
}
