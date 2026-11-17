package ui

import (
	"fmt"
	"image/color"
	"testing"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The sidebar's own lines and its filter box follow the sidebar's theme
// scope like its rows do. Slack's Home header divider and pagesDivider
// are rgba(var(--sk_foreground_low), .13): Slack Kit gives the variable
// 232,232,232 under .sk-client-theme--dark and --light-inverted-sidebar
// and 29,28,29 at a light root, so a light sidebar (Karim's #FED4BE as
// Slack's picker stores it: brightness 2) has dark lines, not the
// inverted scope's light ones. The filter box is .c-filter_input--themed:
// theme-surf-inv-sec over the sidebar and a theme-otl-inv-pry border, the
// scope's tokens (white and the custom ramp's shade 80 there, not the
// default theme's lilac). Token values: Slack's (theme
// TestThemeInkMatchesSlack's table, from Slack's code).
func TestSidebarLinesFollowScope(t *testing.T) {
	hxa := func(v uint32, a uint8) color.NRGBA {
		return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: a}
	}
	base := color.NRGBA{R: 0x80, G: 0x70, B: 0x60, A: 0xff} // any opaque sidebar colour under the box
	for _, c := range []struct {
		name, ia string
		dark     bool
		low      color.NRGBA // --sk_foreground_low at .13
		bg, line color.NRGBA // theme-surf-inv-sec, theme-otl-inv-pry
	}{
		{"karim picker light", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`, false,
			hxa(0x1d1c1d, 0x21), hxa(0xffffff, 0xe6), hxa(0x41281a, 0x21)},
		{"karim import light", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine","custom":"#611F69"},"highlight2":{"palette":"jade","custom":"#2EB67D"},"important":{"palette":"aubergine","custom":"#E01E5A"},"brightness":6,"sidebarInverted":false,"useCustomHex":true}`, false,
			hxa(0x1d1c1d, 0x21), hxa(0xffffff, 0xe6), hxa(0x41281a, 0x21)},
		{"dark custom light", `{"primary":{"palette":"aubergine","custom":"#4A154B"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":5,"sidebarInverted":true,"useCustomHex":true}`, false,
			hxa(0xe8e8e8, 0x21), hxa(0xfff3f9, 0x1c), hxa(0xfff3f9, 0x38)},
		{"default light", ``, false, hxa(0xe8e8e8, 0x21), hxa(0xf9edff, 0x1c), hxa(0xf9edff, 0x38)},
		{"karim picker dark", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`, true,
			hxa(0xe8e8e8, 0x21), hxa(0x101214, 0x99), hxa(0xfff4f0, 0x38)},
		{"default dark", ``, true, hxa(0xe8e8e8, 0x21), hxa(0x101214, 0x99), hxa(0xefe1f5, 0x38)},
	} {
		t.Run(c.name, func(t *testing.T) {
			ia, set := theme.ParseIATheme(c.ia)
			a := &App{th: theme.Bare(c.dark)}
			a.acct = acctTheme{set: set, t: ia, gradients: true}
			a.th.Restyle = func(t *theme.Theme, p *theme.Palette) { theme.ApplyIATheme(p, ia, t.Dark, true) }
			a.th.Refresh()
			a.scopeInvSide = ia.InvertedSidebar(c.dark) // scopeTokens' sidebar scope
			a.sbBase = base
			hex := func(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A) }
			if got := a.sbForegroundLow(0x21); got != c.low {
				t.Errorf("sidebar dividers rgba(--sk_foreground_low, .13) = %s, Slack %s", hex(got), hex(c.low))
			}
			bg, line := a.sbFilterBox()
			if want := theme.Over(c.bg, base); bg != want {
				t.Errorf("filter box fill = %s, Slack theme-surf-inv-sec %s over the sidebar = %s", hex(bg), hex(c.bg), hex(want))
			}
			if want := theme.Over(c.line, base); line != want {
				t.Errorf("filter box border = %s, Slack theme-otl-inv-pry %s over the sidebar = %s", hex(line), hex(c.line), hex(want))
			}
		})
	}
}
