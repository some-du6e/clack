package theme

import (
	"fmt"
	"image/color"
	"maps"
	"slices"
	"testing"
)

// Slack's colours for the chrome (top nav, tab rail), the search pill and
// the sidebar under an account theme, from Slack's own code: the CDN
// build 43167bbd57a5's gantry-v2-vendors zTtO (getThemeVariables,
// getConicGradient, getCustomHexFlatBackgroundShade), iJEa (the custom
// colours' HCT ramps) and the variable sets 858H / qmPJ / lYwo, boot-shared
// zoFo (the Theme component's stylesheet, replicated) and vYdp
// (useInvertedTheme), run in node over the plt ramps of Slack Kit's
// stylesheet. Each value is the token a field paints in Slack, in the
// scope useInvertedTheme gives that surface: the rail's labels and icons
// and the top nav's are .p-tab_rail / .p-top_nav__button
// theme-content-inv-pry, the rail tiles and the search pill
// theme-surf-inv-pry, the sidebar's rows theme-content-inv-sec /
// -inv-pry, theme-surf-inv-ter on hover, theme-base-hgl-1 and
// theme-content-hgl-1 selected. ChromeHoverTint is theme-surf-inv-pry-hover
// blended over theme-surf-inv-pry (--dt_blend-mode-interaction: multiply,
// lighten in dark mode), the W3C blend's (1-ab)*Cs + ab*B(Cb, Cs).
//
// Slack has no light-or-dark ink choice per colour: the ink is the scope's.
// A light client puts the chrome and rail in the inverted scope (white ink)
// whenever isSidebarInverted(brightness) (brightness > 3), whatever the
// custom colour; its Custom theme picker keeps them apart by setting a
// custom System navigation colour's brightness from its tone
// (getBrightnessFromCustomHex: #FED4BE, tone 88, is shade 10, brightness
// 2), so "karim picker" has the light scope's #1c0901 ink. Slack's Import
// (createPayloadFromHexStrings with four hexes) keeps the brightness of the
// preset the palettes match (Aubergine's 6) instead: "karim import" is
// Slack's own white ink over the pale #f5ccb6 gradient.
var slackInkCases = []struct {
	name            string
	ia              string // ia_theme; "" is no pref (Slack's eD, DefaultIATheme)
	dark            bool
	chrome, sidebar string // useInvertedTheme's scope for "chrome"/"tab-rail" and "sidebar"
	want            map[string]color.NRGBA
}{
	{"karim picker light", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`, false, "light", "light", map[string]color.NRGBA{
		"ChromeText":        hx(0x1c0901),
		"ChromeTextDim":     hxa(0x1c0901, 0xcc),
		"ChromeActive":      hxa(0x1c0901, 0x21),
		"ChromeHover":       hxa(0x1c0901, 0x21),
		"SearchBg":          hxa(0x1c0901, 0x21),
		"SearchText":        hx(0x1c0901),
		"RailBadge":         hx(0x83388a),
		"RailBadgeText":     hx(0xf9edff),
		"SidebarBg":         hxa(0xffffff, 0xe6),
		"SidebarText":       hxa(0x1c0901, 0xcc),
		"SidebarTextUnread": hx(0x1c0901),
		"SidebarHover":      hxa(0x41281a, 0x21),
		"SidebarActive":     hx(0x611f69),
		"SidebarActiveText": hx(0xf9edff),
		"SidebarDivider":    hxa(0x41281a, 0x21),
		"SidebarButton":     hxa(0x1c0901, 0x21),
		"Badge":             hx(0x83388a),
		"BadgeText":         hx(0xf9edff),
		"ChromeHoverTint":   hxa(0x190801, 0x38),
		"ChromeA":           hx(0xf5ccb6),
		"ChromeB":           hx(0xeabdfb),
	}},
	{"karim picker dark", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`, true, "dark", "dark", map[string]color.NRGBA{
		"ChromeText":        hx(0xf8f8f8),
		"ChromeTextDim":     hxa(0xffdfcf, 0xcc),
		"ChromeActive":      hxa(0xfff4f0, 0x40),
		"ChromeHover":       hxa(0xfff4f0, 0x40),
		"SearchBg":          hxa(0xfff4f0, 0x40),
		"SearchText":        hx(0xf8f8f8),
		"RailBadge":         hx(0xcfa1de),
		"RailBadgeText":     hx(0x200221),
		"SidebarBg":         hxa(0x101214, 0x99),
		"SidebarText":       hxa(0xffdfcf, 0xcc),
		"SidebarTextUnread": hx(0xf8f8f8),
		"SidebarHover":      hxa(0xfff4f0, 0x14),
		"SidebarActive":     hx(0x7d3986),
		"SidebarActiveText": hx(0xefe1f5),
		"SidebarDivider":    hxa(0xfff4f0, 0x38),
		"SidebarButton":     hxa(0xfff4f0, 0x40),
		"Badge":             hx(0xcfa1de),
		"BadgeText":         hx(0x200221),
		"ChromeHoverTint":   hxa(0xfff4f0, 0x14),
		"ChromeA":           hx(0x41281a),
		"ChromeB":           hx(0x200221),
	}},
	{"karim import light", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine","custom":"#611F69"},"highlight2":{"palette":"jade","custom":"#2EB67D"},"important":{"palette":"aubergine","custom":"#E01E5A"},"brightness":6,"sidebarInverted":false,"useCustomHex":true}`, false, "inverted", "light", map[string]color.NRGBA{
		"ChromeText":        hx(0xffffff),
		"ChromeTextDim":     hxa(0xffdfcf, 0xe6),
		"ChromeActive":      hxa(0xfff4f0, 0x40),
		"ChromeHover":       hxa(0xfff4f0, 0x40),
		"SearchBg":          hxa(0xfff4f0, 0x40),
		"SearchText":        hx(0xffffff),
		"RailBadge":         hx(0xe01e5a),
		"RailBadgeText":     hx(0xfff4f4),
		"SidebarBg":         hxa(0xffffff, 0xe6),
		"SidebarText":       hxa(0x1c0901, 0xcc),
		"SidebarTextUnread": hx(0x1c0901),
		"SidebarHover":      hxa(0x41281a, 0x21),
		"SidebarActive":     hx(0x611f69),
		"SidebarActiveText": hx(0xfff3fa),
		"SidebarDivider":    hxa(0x41281a, 0x21),
		"SidebarButton":     hxa(0x1c0901, 0x21),
		"Badge":             hx(0xe01e5a),
		"BadgeText":         hx(0xfff4f4),
		"ChromeHoverTint":   hxa(0x573b2b, 0x14),
		"ChromeA":           hx(0xf5ccb6),
		"ChromeB":           hx(0xda8dde),
	}},
	{"karim import dark", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine","custom":"#611F69"},"highlight2":{"palette":"jade","custom":"#2EB67D"},"important":{"palette":"aubergine","custom":"#E01E5A"},"brightness":6,"sidebarInverted":false,"useCustomHex":true}`, true, "dark", "dark", map[string]color.NRGBA{
		"ChromeText":        hx(0xf8f8f8),
		"ChromeTextDim":     hxa(0xffdfcf, 0xcc),
		"ChromeActive":      hxa(0xfff4f0, 0x40),
		"ChromeHover":       hxa(0xfff4f0, 0x40),
		"SearchBg":          hxa(0xfff4f0, 0x40),
		"SearchText":        hx(0xf8f8f8),
		"RailBadge":         hx(0xe01e5a),
		"RailBadgeText":     hx(0xfff4f4),
		"SidebarBg":         hxa(0x101214, 0x99),
		"SidebarText":       hxa(0xffdfcf, 0xcc),
		"SidebarTextUnread": hx(0xf8f8f8),
		"SidebarHover":      hxa(0xfff4f0, 0x14),
		"SidebarActive":     hx(0x611f69),
		"SidebarActiveText": hx(0xfff3fa),
		"SidebarDivider":    hxa(0xfff4f0, 0x38),
		"SidebarButton":     hxa(0xfff4f0, 0x40),
		"Badge":             hx(0xe01e5a),
		"BadgeText":         hx(0xfff4f4),
		"ChromeHoverTint":   hxa(0xfff4f0, 0x14),
		"ChromeA":           hx(0x41281a),
		"ChromeB":           hx(0x200025),
	}},
	{"dark custom light", `{"primary":{"palette":"aubergine","custom":"#4A154B"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":5,"sidebarInverted":true,"useCustomHex":true}`, false, "inverted", "inverted", map[string]color.NRGBA{
		"ChromeText":        hx(0xffffff),
		"ChromeTextDim":     hxa(0xffdbf8, 0xe6),
		"ChromeActive":      hxa(0xfff3f9, 0x40),
		"ChromeHover":       hxa(0xfff3f9, 0x40),
		"SearchBg":          hxa(0xfff3f9, 0x40),
		"SearchText":        hx(0xffffff),
		"RailBadge":         hx(0xeabdfb),
		"RailBadgeText":     hx(0x39063a),
		"SidebarBg":         hxa(0xfff3f9, 0x1c),
		"SidebarText":       hxa(0xffdbf8, 0xe6),
		"SidebarTextUnread": hx(0xffffff),
		"SidebarHover":      hxa(0xfff3f9, 0x14),
		"SidebarActive":     hx(0xf9edff),
		"SidebarActiveText": hx(0x39063a),
		"SidebarDivider":    hxa(0xfff3f9, 0x38),
		"SidebarButton":     hxa(0xfff3f9, 0x40),
		"Badge":             hx(0xeabdfb),
		"BadgeText":         hx(0x39063a),
		"ChromeHoverTint":   hxa(0x642c62, 0x14),
		"ChromeA":           hx(0x4d184e),
		"ChromeB":           hx(0x4a154b),
	}},
	{"dark custom dark", `{"primary":{"palette":"aubergine","custom":"#4A154B"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":5,"sidebarInverted":true,"useCustomHex":true}`, true, "dark", "dark", map[string]color.NRGBA{
		"ChromeText":        hx(0xf8f8f8),
		"ChromeTextDim":     hxa(0xffdbf8, 0xcc),
		"ChromeActive":      hxa(0xfff3f9, 0x40),
		"ChromeHover":       hxa(0xfff3f9, 0x40),
		"SearchBg":          hxa(0xfff3f9, 0x40),
		"SearchText":        hx(0xf8f8f8),
		"RailBadge":         hx(0xcfa1de),
		"RailBadgeText":     hx(0x200221),
		"SidebarBg":         hxa(0x101214, 0x99),
		"SidebarText":       hxa(0xffdbf8, 0xcc),
		"SidebarTextUnread": hx(0xf8f8f8),
		"SidebarHover":      hxa(0xfff3f9, 0x14),
		"SidebarActive":     hx(0x7d3986),
		"SidebarActiveText": hx(0xefe1f5),
		"SidebarDivider":    hxa(0xfff3f9, 0x38),
		"SidebarButton":     hxa(0xfff3f9, 0x40),
		"Badge":             hx(0xcfa1de),
		"BadgeText":         hx(0x200221),
		"ChromeHoverTint":   hxa(0xfff3f9, 0x14),
		"ChromeA":           hx(0x4d184e),
		"ChromeB":           hx(0x200221),
	}},
	{"ochin light", `{"primary":{"palette":"ocean"},"highlight1":{"palette":"ocean"},"highlight2":{"palette":"cilantro"},"important":{"palette":"mojito"},"brightness":5,"sidebarInverted":false,"useCustomHex":false}`, false, "inverted", "light", map[string]color.NRGBA{
		"ChromeText":        hx(0xffffff),
		"ChromeTextDim":     hxa(0xc2e6fd, 0xe6),
		"ChromeActive":      hxa(0xe6f5fe, 0x40),
		"ChromeHover":       hxa(0xe6f5fe, 0x40),
		"SearchBg":          hxa(0xe6f5fe, 0x40),
		"SearchText":        hx(0xffffff),
		"RailBadge":         hx(0x87dc9b),
		"RailBadgeText":     hx(0x004522),
		"SidebarBg":         hxa(0xffffff, 0xe6),
		"SidebarText":       hxa(0x122a59, 0xcc),
		"SidebarTextUnread": hx(0x122a59),
		"SidebarHover":      hxa(0x0b4c8c, 0x21),
		"SidebarActive":     hx(0x0b4c8c),
		"SidebarActiveText": hx(0xe6f5fe),
		"SidebarDivider":    hxa(0x0b4c8c, 0x21),
		"SidebarButton":     hxa(0x122a59, 0x21),
		"Badge":             hx(0x1a8343),
		"BadgeText":         hx(0xe9ffec),
		"ChromeHoverTint":   hxa(0x1263a3, 0x14),
		"ChromeA":           hx(0x1264a3),
		"ChromeB":           hx(0x093a73),
	}},
	{"default light", ``, false, "inverted", "inverted", map[string]color.NRGBA{
		"ChromeText":        hx(0xffffff),
		"ChromeTextDim":     hxa(0xf6e4ff, 0xe6),
		"ChromeActive":      hxa(0xf9edff, 0x40),
		"ChromeHover":       hxa(0xf9edff, 0x40),
		"SearchBg":          hxa(0xf9edff, 0x40),
		"SearchText":        hx(0xffffff),
		"RailBadge":         hx(0xeabdfb),
		"RailBadgeText":     hx(0x39063a),
		"SidebarBg":         hxa(0xf9edff, 0x1c),
		"SidebarText":       hxa(0xf6e4ff, 0xe6),
		"SidebarTextUnread": hx(0xffffff),
		"SidebarHover":      hxa(0xf9edff, 0x14),
		"SidebarActive":     hx(0xf9edff),
		"SidebarActiveText": hx(0x39063a),
		"SidebarDivider":    hxa(0xf9edff, 0x38),
		"SidebarButton":     hxa(0xf9edff, 0x40),
		"Badge":             hx(0xeabdfb),
		"BadgeText":         hx(0x39063a),
		"ChromeHoverTint":   hxa(0x82378a, 0x14),
		"ChromeA":           hx(0x4a154b),
		"ChromeB":           hx(0x39063a),
	}},
	{"default dark", ``, true, "dark", "dark", map[string]color.NRGBA{
		"ChromeText":        hx(0xf8f8f8),
		"ChromeTextDim":     hxa(0xe3ceeb, 0xcc),
		"ChromeActive":      hxa(0xefe1f5, 0x40),
		"ChromeHover":       hxa(0xefe1f5, 0x40),
		"SearchBg":          hxa(0xefe1f5, 0x40),
		"SearchText":        hx(0xf8f8f8),
		"RailBadge":         hx(0xcfa1de),
		"RailBadgeText":     hx(0x200221),
		"SidebarBg":         hxa(0x101214, 0x99),
		"SidebarText":       hxa(0xe3ceeb, 0xcc),
		"SidebarTextUnread": hx(0xf8f8f8),
		"SidebarHover":      hxa(0xefe1f5, 0x14),
		"SidebarActive":     hx(0x7d3986),
		"SidebarActiveText": hx(0xefe1f5),
		"SidebarDivider":    hxa(0xefe1f5, 0x38),
		"SidebarButton":     hxa(0xefe1f5, 0x40),
		"Badge":             hx(0xcfa1de),
		"BadgeText":         hx(0x200221),
		"ChromeHoverTint":   hxa(0xefe1f5, 0x14),
		"ChromeA":           hx(0x431349),
		"ChromeB":           hx(0x200221),
	}},
}

func inkScope(inverted, dark bool) string {
	switch {
	case inverted:
		return "inverted"
	case dark:
		return "dark"
	}
	return "light"
}

func inkHex(c color.NRGBA) string {
	if c.A == 0xff {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}

// The palette Clack draws the chrome and sidebar from (the mode's palette
// with the account theme applied) holds Slack's colours: the ink of the
// rail, the top nav and the sidebar, their hover, active and selected
// fills, the badges and the window gradient, for Karim's custom colour as
// Slack's picker and Slack's import store it, a dark custom colour, Ochin
// (a light sidebar beside an inverted rail) and the default theme, in
// light and dark mode.
func TestThemeInkMatchesSlack(t *testing.T) {
	for _, c := range slackInkCases {
		t.Run(c.name, func(t *testing.T) {
			th := DefaultIATheme
			if c.ia != "" {
				th = mustTheme(t, c.ia)
			}
			if got := inkScope(th.InvertedChrome(c.dark), c.dark); got != c.chrome {
				t.Errorf("chrome scope %s, Slack %s", got, c.chrome)
			}
			if got := inkScope(th.InvertedSidebar(c.dark), c.dark); got != c.sidebar {
				t.Errorf("sidebar scope %s, Slack %s", got, c.sidebar)
			}
			p := LightPalette
			if c.dark {
				p = DarkPalette
			}
			ApplyIATheme(&p, th, c.dark, true)
			for _, k := range slices.Sorted(maps.Keys(c.want)) {
				get := accountFields[k]
				if get == nil {
					t.Errorf("%s: not a palette field the account theme drives (Slack %s)", k, inkHex(c.want[k]))
					continue
				}
				if got := *get(&p); got != c.want[k] {
					t.Errorf("%s = %s, Slack %s", k, inkHex(got), inkHex(c.want[k]))
				}
			}
		})
	}
}
