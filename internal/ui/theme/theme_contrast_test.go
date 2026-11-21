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
// theme-content-hgl-1 selected, the top nav buttons' :active
// theme-surf-inv-sec (ChromePressed). ChromeHoverTint is theme-surf-inv-pry-hover
// blended over theme-surf-inv-pry (--dt_blend-mode-interaction: multiply,
// lighten in dark mode), the W3C blend's (1-ab)*Cs + ab*B(Cb, Cs).
//
// The root scope's fields are the main pane's: ThemeImp / ThemeImpText
// (theme-base-imp / -content-imp, Activity's unread dots and count
// badges), which Slack's getThemeVariableDefinitions sets from the
// important role only, Accent (theme-base-pry, tab underlines),
// Presence (theme-base-inv-hgl-2) and ThemeInvSec / -InvTer /
// ThemeOtlInvPry (theme-content-inv-sec, -inv-ter and theme-otl-inv-pry:
// the DMs peek's timestamps and rows' rules).
// ChromeBasePry / ChromeContentTer are
// the control strip's :active and --active (theme-base-pry,
// theme-content-ter in the rail's scope), ChromeBasePryHover the
// surf-inv-pry-hover layer blended over that base-pry; SidebarTextTer is
// theme-content-inv-ter, SidebarRule Slack Kit's --sk_foreground_low at
// .13 in the sidebar's scope. The blends take the variable sets' own
// alphas (858H, qmPJ, lYwo).
//
// Not modelled (a known gap): ia_theme's highContrast beyond
// content-inv-pry. Slack's getThemeVariables also swaps in its
// high-contrast shade table (eM) and variable sets there; nothing in
// Slack's client writes the flag.
//
// Slack has no light-or-dark ink choice per colour: the ink is the scope's.
// A light client puts the chrome and rail in the inverted scope (white ink)
// whenever isSidebarInverted(brightness) (brightness > 3), whatever the
// custom colour; its Custom theme picker keeps them apart by setting a
// custom System navigation colour's brightness from its tone
// (getBrightnessFromCustomHex: #FED4BE, tone 88, is shade 10, brightness
// 2), so "karim picker" has the light scope's #1c0901 ink. Slack's Import
// (createPayloadFromHexStrings with four hexes) keeps the brightness of the
// preset the palettes match (Aubergine's 6) instead, and sets
// sidebarInverted = isSidebarInverted(6), which setIaThemePrefByApi keeps
// only on a light client: "karim import" (saved from a dark client, or
// Darker sidebars off since) is Slack's own white chrome ink over the pale
// #f5ccb6 gradient beside a light sidebar; "karim import light save" has
// white sidebar ink over it too. "focus gray" is focus mode's silent
// sidebar theme (K6OQ GRAY), whose control strip button is --active.
var slackInkCases = []struct {
	name            string
	ia              string // ia_theme; "" is no pref (Slack's eD, DefaultIATheme)
	dark            bool
	chrome, sidebar string // useInvertedTheme's scope for "chrome"/"tab-rail" and "sidebar"
	want            map[string]color.NRGBA
}{
	{"karim picker light", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`, false, "light", "light", map[string]color.NRGBA{
		"ChromeText":         hx(0x1c0901),
		"ChromeTextDim":      hxa(0x1c0901, 0xcc),
		"ChromeActive":       hxa(0x1c0901, 0x21),
		"ChromeHover":        hxa(0x1c0901, 0x21),
		"ChromePressed":      hxa(0xffffff, 0xe6),
		"SearchBg":           hxa(0x1c0901, 0x21),
		"SearchText":         hx(0x1c0901),
		"RailBadge":          hx(0x83388a),
		"RailBadgeText":      hx(0xf9edff),
		"SidebarBg":          hxa(0xffffff, 0xe6),
		"SidebarText":        hxa(0x1c0901, 0xcc),
		"SidebarTextUnread":  hx(0x1c0901),
		"SidebarHover":       hxa(0x41281a, 0x21),
		"SidebarActive":      hx(0x611f69),
		"SidebarActiveText":  hx(0xf9edff),
		"SidebarDivider":     hxa(0x41281a, 0x21),
		"SidebarButton":      hxa(0x1c0901, 0x21),
		"Badge":              hx(0x83388a),
		"BadgeText":          hx(0xf9edff),
		"SidebarTextTer":     hxa(0x1c0901, 0x80),
		"ChromeBasePry":      hx(0x573c2c),
		"ChromeContentTer":   hx(0xfff4f0),
		"ChromeSurfInvTer":   hxa(0x41281a, 0x21),
		"ChromeOtlInvPry":    hxa(0x41281a, 0x21),
		"ThemeImp":           hx(0x83388a),
		"ThemeImpText":       hx(0xf9edff),
		"ThemeInvSec":        hxa(0x1c0901, 0xcc),
		"ThemeInvTer":        hxa(0x1c0901, 0x80),
		"ThemeOtlInvPry":     hxa(0x41281a, 0x21),
		"Accent":             hx(0x573c2c),
		"Presence":           hx(0x20a271),
		"ChromeHoverTint":    hxa(0x190801, 0x38),
		"ChromeBasePryHover": hx(0x462f22),
		"SidebarRule":        hxa(0x1d1c1d, 0x21),
		"ChromeA":            hx(0xf5ccb6),
		"ChromeB":            hx(0xeabdfb),
	}},
	{"karim picker dark", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`, true, "dark", "dark", map[string]color.NRGBA{
		"ChromeText":         hx(0xf8f8f8),
		"ChromeTextDim":      hxa(0xffdfcf, 0xcc),
		"ChromeActive":       hxa(0xfff4f0, 0x40),
		"ChromeHover":        hxa(0xfff4f0, 0x40),
		"ChromePressed":      hxa(0x101214, 0x99),
		"SearchBg":           hxa(0xfff4f0, 0x40),
		"SearchText":         hx(0xf8f8f8),
		"RailBadge":          hx(0xcfa1de),
		"RailBadgeText":      hx(0x200221),
		"SidebarBg":          hxa(0x101214, 0x99),
		"SidebarText":        hxa(0xffdfcf, 0xcc),
		"SidebarTextUnread":  hx(0xf8f8f8),
		"SidebarHover":       hxa(0xfff4f0, 0x14),
		"SidebarActive":      hx(0x7d3986),
		"SidebarActiveText":  hx(0xefe1f5),
		"SidebarDivider":     hxa(0xfff4f0, 0x38),
		"SidebarButton":      hxa(0xfff4f0, 0x40),
		"Badge":              hx(0xcfa1de),
		"BadgeText":          hx(0x200221),
		"SidebarTextTer":     hxa(0xffdfcf, 0x99),
		"ChromeBasePry":      hx(0xffdfcf),
		"ChromeContentTer":   hx(0x1c0901),
		"ChromeSurfInvTer":   hxa(0xfff4f0, 0x14),
		"ChromeOtlInvPry":    hxa(0xfff4f0, 0x38),
		"ThemeImp":           hx(0xcfa1de),
		"ThemeImpText":       hx(0x200221),
		"ThemeInvSec":        hxa(0xffdfcf, 0xcc),
		"ThemeInvTer":        hxa(0xffdfcf, 0x99),
		"ThemeOtlInvPry":     hxa(0xfff4f0, 0x38),
		"Accent":             hx(0xffdfcf),
		"Presence":           hx(0x3daa7c),
		"ChromeHoverTint":    hxa(0xfff4f0, 0x14),
		"ChromeBasePryHover": hx(0xffe1d2),
		"SidebarRule":        hxa(0xe8e8e8, 0x21),
		"ChromeA":            hx(0x41281a),
		"ChromeB":            hx(0x200221),
	}},
	{"karim import light", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine","custom":"#611F69"},"highlight2":{"palette":"jade","custom":"#2EB67D"},"important":{"palette":"aubergine","custom":"#E01E5A"},"brightness":6,"sidebarInverted":false,"useCustomHex":true}`, false, "inverted", "light", map[string]color.NRGBA{
		"ChromeText":         hx(0xffffff),
		"ChromeTextDim":      hxa(0xffdfcf, 0xe6),
		"ChromeActive":       hxa(0xfff4f0, 0x40),
		"ChromeHover":        hxa(0xfff4f0, 0x40),
		"ChromePressed":      hxa(0xfff4f0, 0x1c),
		"SearchBg":           hxa(0xfff4f0, 0x40),
		"SearchText":         hx(0xffffff),
		"RailBadge":          hx(0xe01e5a),
		"RailBadgeText":      hx(0xfff4f4),
		"SidebarBg":          hxa(0xffffff, 0xe6),
		"SidebarText":        hxa(0x1c0901, 0xcc),
		"SidebarTextUnread":  hx(0x1c0901),
		"SidebarHover":       hxa(0x41281a, 0x21),
		"SidebarActive":      hx(0x611f69),
		"SidebarActiveText":  hx(0xfff3fa),
		"SidebarDivider":     hxa(0x41281a, 0x21),
		"SidebarButton":      hxa(0x1c0901, 0x21),
		"Badge":              hx(0xe01e5a),
		"BadgeText":          hx(0xfff4f4),
		"SidebarTextTer":     hxa(0x1c0901, 0x80),
		"ChromeBasePry":      hx(0xfff4f0),
		"ChromeContentTer":   hx(0x1c0901),
		"ChromeSurfInvTer":   hxa(0xfff4f0, 0x14),
		"ChromeOtlInvPry":    hxa(0xfff4f0, 0x38),
		"ThemeImp":           hx(0xe01e5a),
		"ThemeImpText":       hx(0xfff4f4),
		"ThemeInvSec":        hxa(0x1c0901, 0xcc),
		"ThemeInvTer":        hxa(0x1c0901, 0x80),
		"ThemeOtlInvPry":     hxa(0x41281a, 0x21),
		"Accent":             hx(0x573c2c),
		"Presence":           hx(0x2eb67d),
		"ChromeHoverTint":    hxa(0x573b2b, 0x14),
		"ChromeBasePryHover": hx(0xf2e5e0),
		"SidebarRule":        hxa(0x1d1c1d, 0x21),
		"ChromeA":            hx(0xf5ccb6),
		"ChromeB":            hx(0xda8dde),
	}},
	{"karim import dark", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine","custom":"#611F69"},"highlight2":{"palette":"jade","custom":"#2EB67D"},"important":{"palette":"aubergine","custom":"#E01E5A"},"brightness":6,"sidebarInverted":false,"useCustomHex":true}`, true, "dark", "dark", map[string]color.NRGBA{
		"ChromeText":         hx(0xf8f8f8),
		"ChromeTextDim":      hxa(0xffdfcf, 0xcc),
		"ChromeActive":       hxa(0xfff4f0, 0x40),
		"ChromeHover":        hxa(0xfff4f0, 0x40),
		"ChromePressed":      hxa(0x101214, 0x99),
		"SearchBg":           hxa(0xfff4f0, 0x40),
		"SearchText":         hx(0xf8f8f8),
		"RailBadge":          hx(0xe01e5a),
		"RailBadgeText":      hx(0xfff4f4),
		"SidebarBg":          hxa(0x101214, 0x99),
		"SidebarText":        hxa(0xffdfcf, 0xcc),
		"SidebarTextUnread":  hx(0xf8f8f8),
		"SidebarHover":       hxa(0xfff4f0, 0x14),
		"SidebarActive":      hx(0x611f69),
		"SidebarActiveText":  hx(0xfff3fa),
		"SidebarDivider":     hxa(0xfff4f0, 0x38),
		"SidebarButton":      hxa(0xfff4f0, 0x40),
		"Badge":              hx(0xe01e5a),
		"BadgeText":          hx(0xfff4f4),
		"SidebarTextTer":     hxa(0xffdfcf, 0x99),
		"ChromeBasePry":      hx(0xffdfcf),
		"ChromeContentTer":   hx(0x1c0901),
		"ChromeSurfInvTer":   hxa(0xfff4f0, 0x14),
		"ChromeOtlInvPry":    hxa(0xfff4f0, 0x38),
		"ThemeImp":           hx(0xe01e5a),
		"ThemeImpText":       hx(0xfff4f4),
		"ThemeInvSec":        hxa(0xffdfcf, 0xcc),
		"ThemeInvTer":        hxa(0xffdfcf, 0x99),
		"ThemeOtlInvPry":     hxa(0xfff4f0, 0x38),
		"Accent":             hx(0xffdfcf),
		"Presence":           hx(0x2eb67d),
		"ChromeHoverTint":    hxa(0xfff4f0, 0x14),
		"ChromeBasePryHover": hx(0xffe1d2),
		"SidebarRule":        hxa(0xe8e8e8, 0x21),
		"ChromeA":            hx(0x41281a),
		"ChromeB":            hx(0x200025),
	}},
	{"dark custom light", `{"primary":{"palette":"aubergine","custom":"#4A154B"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":5,"sidebarInverted":true,"useCustomHex":true}`, false, "inverted", "inverted", map[string]color.NRGBA{
		"ChromeText":         hx(0xffffff),
		"ChromeTextDim":      hxa(0xffdbf8, 0xe6),
		"ChromeActive":       hxa(0xfff3f9, 0x40),
		"ChromeHover":        hxa(0xfff3f9, 0x40),
		"ChromePressed":      hxa(0xfff3f9, 0x1c),
		"SearchBg":           hxa(0xfff3f9, 0x40),
		"SearchText":         hx(0xffffff),
		"RailBadge":          hx(0xeabdfb),
		"RailBadgeText":      hx(0x39063a),
		"SidebarBg":          hxa(0xfff3f9, 0x1c),
		"SidebarText":        hxa(0xffdbf8, 0xe6),
		"SidebarTextUnread":  hx(0xffffff),
		"SidebarHover":       hxa(0xfff3f9, 0x14),
		"SidebarActive":      hx(0xf9edff),
		"SidebarActiveText":  hx(0x39063a),
		"SidebarDivider":     hxa(0xfff3f9, 0x38),
		"SidebarButton":      hxa(0xfff3f9, 0x40),
		"Badge":              hx(0xeabdfb),
		"BadgeText":          hx(0x39063a),
		"SidebarTextTer":     hxa(0xfff3f9, 0x99),
		"ChromeBasePry":      hx(0xfff3f9),
		"ChromeContentTer":   hx(0x210023),
		"ChromeSurfInvTer":   hxa(0xfff3f9, 0x14),
		"ChromeOtlInvPry":    hxa(0xfff3f9, 0x38),
		"ThemeImp":           hx(0x83388a),
		"ThemeImpText":       hx(0xf9edff),
		"ThemeInvSec":        hxa(0x210023, 0xcc),
		"ThemeInvTer":        hxa(0x210023, 0x80),
		"ThemeOtlInvPry":     hxa(0x4d184e, 0x21),
		"Accent":             hx(0x642d63),
		"Presence":           hx(0x20a271),
		"ChromeHoverTint":    hxa(0x642c62, 0x14),
		"ChromeBasePryHover": hx(0xf3e3ed),
		"SidebarRule":        hxa(0xe8e8e8, 0x21),
		"ChromeA":            hx(0x4d184e),
		"ChromeB":            hx(0x4a154b),
	}},
	{"dark custom dark", `{"primary":{"palette":"aubergine","custom":"#4A154B"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":5,"sidebarInverted":true,"useCustomHex":true}`, true, "dark", "dark", map[string]color.NRGBA{
		"ChromeText":         hx(0xf8f8f8),
		"ChromeTextDim":      hxa(0xffdbf8, 0xcc),
		"ChromeActive":       hxa(0xfff3f9, 0x40),
		"ChromeHover":        hxa(0xfff3f9, 0x40),
		"ChromePressed":      hxa(0x101214, 0x99),
		"SearchBg":           hxa(0xfff3f9, 0x40),
		"SearchText":         hx(0xf8f8f8),
		"RailBadge":          hx(0xcfa1de),
		"RailBadgeText":      hx(0x200221),
		"SidebarBg":          hxa(0x101214, 0x99),
		"SidebarText":        hxa(0xffdbf8, 0xcc),
		"SidebarTextUnread":  hx(0xf8f8f8),
		"SidebarHover":       hxa(0xfff3f9, 0x14),
		"SidebarActive":      hx(0x7d3986),
		"SidebarActiveText":  hx(0xefe1f5),
		"SidebarDivider":     hxa(0xfff3f9, 0x38),
		"SidebarButton":      hxa(0xfff3f9, 0x40),
		"Badge":              hx(0xcfa1de),
		"BadgeText":          hx(0x200221),
		"SidebarTextTer":     hxa(0xffdbf8, 0x99),
		"ChromeBasePry":      hx(0xffdbf8),
		"ChromeContentTer":   hx(0x210023),
		"ChromeSurfInvTer":   hxa(0xfff3f9, 0x14),
		"ChromeOtlInvPry":    hxa(0xfff3f9, 0x38),
		"ThemeImp":           hx(0xcfa1de),
		"ThemeImpText":       hx(0x200221),
		"ThemeInvSec":        hxa(0xffdbf8, 0xcc),
		"ThemeInvTer":        hxa(0xffdbf8, 0x99),
		"ThemeOtlInvPry":     hxa(0xfff3f9, 0x38),
		"Accent":             hx(0xffdbf8),
		"Presence":           hx(0x3daa7c),
		"ChromeHoverTint":    hxa(0xfff3f9, 0x14),
		"ChromeBasePryHover": hx(0xffddf8),
		"SidebarRule":        hxa(0xe8e8e8, 0x21),
		"ChromeA":            hx(0x4d184e),
		"ChromeB":            hx(0x200221),
	}},
	{"karim import light save", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine","custom":"#611F69"},"highlight2":{"palette":"jade","custom":"#2EB67D"},"important":{"palette":"aubergine","custom":"#E01E5A"},"brightness":6,"sidebarInverted":true,"useCustomHex":true}`, false, "inverted", "inverted", map[string]color.NRGBA{
		"ChromeText":         hx(0xffffff),
		"ChromeTextDim":      hxa(0xffdfcf, 0xe6),
		"ChromeActive":       hxa(0xfff4f0, 0x40),
		"ChromeHover":        hxa(0xfff4f0, 0x40),
		"ChromePressed":      hxa(0xfff4f0, 0x1c),
		"SearchBg":           hxa(0xfff4f0, 0x40),
		"SearchText":         hx(0xffffff),
		"RailBadge":          hx(0xe01e5a),
		"RailBadgeText":      hx(0xfff4f4),
		"SidebarBg":          hxa(0xfff4f0, 0x1c),
		"SidebarText":        hxa(0xffdfcf, 0xe6),
		"SidebarTextUnread":  hx(0xffffff),
		"SidebarHover":       hxa(0xfff4f0, 0x14),
		"SidebarActive":      hx(0x611f69),
		"SidebarActiveText":  hx(0xfff3fa),
		"SidebarDivider":     hxa(0xfff4f0, 0x38),
		"SidebarButton":      hxa(0xfff4f0, 0x40),
		"Badge":              hx(0xe01e5a),
		"BadgeText":          hx(0xfff4f4),
		"SidebarTextTer":     hxa(0xfff4f0, 0x99),
		"ChromeBasePry":      hx(0xfff4f0),
		"ChromeContentTer":   hx(0x1c0901),
		"ChromeSurfInvTer":   hxa(0xfff4f0, 0x14),
		"ChromeOtlInvPry":    hxa(0xfff4f0, 0x38),
		"ThemeImp":           hx(0xe01e5a),
		"ThemeImpText":       hx(0xfff4f4),
		"ThemeInvSec":        hxa(0x1c0901, 0xcc),
		"ThemeInvTer":        hxa(0x1c0901, 0x80),
		"ThemeOtlInvPry":     hxa(0x41281a, 0x21),
		"Accent":             hx(0x573c2c),
		"Presence":           hx(0x2eb67d),
		"ChromeHoverTint":    hxa(0x573b2b, 0x14),
		"ChromeBasePryHover": hx(0xf2e5e0),
		"SidebarRule":        hxa(0xe8e8e8, 0x21),
		"ChromeA":            hx(0xf5ccb6),
		"ChromeB":            hx(0xda8dde),
	}},
	{"ochin light", `{"primary":{"palette":"ocean"},"highlight1":{"palette":"ocean"},"highlight2":{"palette":"cilantro"},"important":{"palette":"mojito"},"brightness":5,"sidebarInverted":false,"useCustomHex":false}`, false, "inverted", "light", map[string]color.NRGBA{
		"ChromeText":         hx(0xffffff),
		"ChromeTextDim":      hxa(0xc2e6fd, 0xe6),
		"ChromeActive":       hxa(0xe6f5fe, 0x40),
		"ChromeHover":        hxa(0xe6f5fe, 0x40),
		"ChromePressed":      hxa(0xe6f5fe, 0x1c),
		"SearchBg":           hxa(0xe6f5fe, 0x40),
		"SearchText":         hx(0xffffff),
		"RailBadge":          hx(0x87dc9b),
		"RailBadgeText":      hx(0x004522),
		"SidebarBg":          hxa(0xffffff, 0xe6),
		"SidebarText":        hxa(0x122a59, 0xcc),
		"SidebarTextUnread":  hx(0x122a59),
		"SidebarHover":       hxa(0x0b4c8c, 0x21),
		"SidebarActive":      hx(0x0b4c8c),
		"SidebarActiveText":  hx(0xe6f5fe),
		"SidebarDivider":     hxa(0x0b4c8c, 0x21),
		"SidebarButton":      hxa(0x122a59, 0x21),
		"Badge":              hx(0x1a8343),
		"BadgeText":          hx(0xe9ffec),
		"SidebarTextTer":     hxa(0x122a59, 0x80),
		"ChromeBasePry":      hx(0xe6f5fe),
		"ChromeContentTer":   hx(0x122a59),
		"ChromeSurfInvTer":   hxa(0xe6f5fe, 0x14),
		"ChromeOtlInvPry":    hxa(0xe6f5fe, 0x38),
		"ThemeImp":           hx(0x1a8343),
		"ThemeImpText":       hx(0xe9ffec),
		"ThemeInvSec":        hxa(0x122a59, 0xcc),
		"ThemeInvTer":        hxa(0x122a59, 0x80),
		"ThemeOtlInvPry":     hxa(0x0b4c8c, 0x21),
		"Accent":             hx(0x1264a3),
		"Presence":           hx(0x379e15),
		"ChromeHoverTint":    hxa(0x1263a3, 0x14),
		"ChromeBasePryHover": hx(0xd5e9f7),
		"SidebarRule":        hxa(0x1d1c1d, 0x21),
		"ChromeA":            hx(0x1264a3),
		"ChromeB":            hx(0x093a73),
	}},
	{"focus gray light", `{"primary":{"palette":"gray"},"highlight1":{"palette":"gray"},"highlight2":{"palette":"mojito"},"important":{"palette":"gray"},"brightness":6,"sidebarInverted":true,"useCustomHex":false}`, false, "inverted", "inverted", map[string]color.NRGBA{
		"ChromeText":         hx(0xffffff),
		"ChromeTextDim":      hxa(0xeaeaea, 0xe6),
		"ChromeActive":       hxa(0xf8f8f8, 0x40),
		"ChromeHover":        hxa(0xf8f8f8, 0x40),
		"ChromePressed":      hxa(0xf8f8f8, 0x1c),
		"SearchBg":           hxa(0xf8f8f8, 0x40),
		"SearchText":         hx(0xffffff),
		"RailBadge":          hx(0xcbcccf),
		"RailBadgeText":      hx(0x1d1c1d),
		"SidebarBg":          hxa(0xf8f8f8, 0x1c),
		"SidebarText":        hxa(0xeaeaea, 0xe6),
		"SidebarTextUnread":  hx(0xffffff),
		"SidebarHover":       hxa(0xf8f8f8, 0x14),
		"SidebarActive":      hx(0xf8f8f8),
		"SidebarActiveText":  hx(0x1d1c1d),
		"SidebarDivider":     hxa(0xf8f8f8, 0x38),
		"SidebarButton":      hxa(0xf8f8f8, 0x40),
		"Badge":              hx(0xcbcccf),
		"BadgeText":          hx(0x1d1c1d),
		"SidebarTextTer":     hxa(0xf8f8f8, 0x99),
		"ChromeBasePry":      hx(0xf8f8f8),
		"ChromeContentTer":   hx(0x1d1c1d),
		"ChromeSurfInvTer":   hxa(0xf8f8f8, 0x14),
		"ChromeOtlInvPry":    hxa(0xf8f8f8, 0x38),
		"ThemeImp":           hx(0x454447),
		"ThemeImpText":       hx(0xf8f8f8),
		"ThemeInvSec":        hxa(0x1d1c1d, 0xcc),
		"ThemeInvTer":        hxa(0x1d1c1d, 0x80),
		"ThemeOtlInvPry":     hxa(0x333133, 0x21),
		"Accent":             hx(0x454447),
		"Presence":           hx(0x2fa659),
		"ChromeHoverTint":    hxa(0x454447, 0x14),
		"ChromeBasePryHover": hx(0xeae9ea),
		"SidebarRule":        hxa(0xe8e8e8, 0x21),
		"ChromeA":            hx(0x262426),
		"ChromeB":            hx(0x1d1c1d),
	}},
	{"focus gray dark", `{"primary":{"palette":"gray"},"highlight1":{"palette":"gray"},"highlight2":{"palette":"mojito"},"important":{"palette":"gray"},"brightness":6,"sidebarInverted":true,"useCustomHex":false}`, true, "dark", "dark", map[string]color.NRGBA{
		"ChromeText":         hx(0xf8f8f8),
		"ChromeTextDim":      hxa(0xe0e0e0, 0xcc),
		"ChromeActive":       hxa(0xf8f8f8, 0x40),
		"ChromeHover":        hxa(0xf8f8f8, 0x40),
		"ChromePressed":      hxa(0x101214, 0x99),
		"SearchBg":           hxa(0xf8f8f8, 0x40),
		"SearchText":         hx(0xf8f8f8),
		"RailBadge":          hx(0xb9babd),
		"RailBadgeText":      hx(0x101214),
		"SidebarBg":          hxa(0x101214, 0x99),
		"SidebarText":        hxa(0xe0e0e0, 0xcc),
		"SidebarTextUnread":  hx(0xf8f8f8),
		"SidebarHover":       hxa(0xf8f8f8, 0x14),
		"SidebarActive":      hx(0x414549),
		"SidebarActiveText":  hx(0xf8f8f8),
		"SidebarDivider":     hxa(0xf8f8f8, 0x38),
		"SidebarButton":      hxa(0xf8f8f8, 0x40),
		"Badge":              hx(0xb9babd),
		"BadgeText":          hx(0x101214),
		"SidebarTextTer":     hxa(0xe0e0e0, 0x99),
		"ChromeBasePry":      hx(0xe0e0e0),
		"ChromeContentTer":   hx(0x101214),
		"ChromeSurfInvTer":   hxa(0xf8f8f8, 0x14),
		"ChromeOtlInvPry":    hxa(0xf8f8f8, 0x38),
		"ThemeImp":           hx(0xb9babd),
		"ThemeImpText":       hx(0x101214),
		"ThemeInvSec":        hxa(0xe0e0e0, 0xcc),
		"ThemeInvTer":        hxa(0xe0e0e0, 0x99),
		"ThemeOtlInvPry":     hxa(0xf8f8f8, 0x38),
		"Accent":             hx(0xe0e0e0),
		"Presence":           hx(0x4dad68),
		"ChromeHoverTint":    hxa(0xf8f8f8, 0x14),
		"ChromeBasePryHover": hx(0xe2e2e2),
		"SidebarRule":        hxa(0xe8e8e8, 0x21),
		"ChromeA":            hx(0x212428),
		"ChromeB":            hx(0x101214),
	}},
	{"default light", ``, false, "inverted", "inverted", map[string]color.NRGBA{
		"ChromeText":         hx(0xffffff),
		"ChromeTextDim":      hxa(0xf6e4ff, 0xe6),
		"ChromeActive":       hxa(0xf9edff, 0x40),
		"ChromeHover":        hxa(0xf9edff, 0x40),
		"ChromePressed":      hxa(0xf9edff, 0x1c),
		"SearchBg":           hxa(0xf9edff, 0x40),
		"SearchText":         hx(0xffffff),
		"RailBadge":          hx(0xeabdfb),
		"RailBadgeText":      hx(0x39063a),
		"SidebarBg":          hxa(0xf9edff, 0x1c),
		"SidebarText":        hxa(0xf6e4ff, 0xe6),
		"SidebarTextUnread":  hx(0xffffff),
		"SidebarHover":       hxa(0xf9edff, 0x14),
		"SidebarActive":      hx(0xf9edff),
		"SidebarActiveText":  hx(0x39063a),
		"SidebarDivider":     hxa(0xf9edff, 0x38),
		"SidebarButton":      hxa(0xf9edff, 0x40),
		"Badge":              hx(0xeabdfb),
		"BadgeText":          hx(0x39063a),
		"SidebarTextTer":     hxa(0xf9edff, 0x99),
		"ChromeBasePry":      hx(0xf9edff),
		"ChromeContentTer":   hx(0x39063a),
		"ChromeSurfInvTer":   hxa(0xf9edff, 0x14),
		"ChromeOtlInvPry":    hxa(0xf9edff, 0x38),
		"ThemeImp":           hx(0x83388a),
		"ThemeImpText":       hx(0xf9edff),
		"ThemeInvSec":        hxa(0x39063a, 0xcc),
		"ThemeInvTer":        hxa(0x39063a, 0x80),
		"ThemeOtlInvPry":     hxa(0x611f69, 0x21),
		"Accent":             hx(0x83388a),
		"Presence":           hx(0x20a271),
		"ChromeHoverTint":    hxa(0x82378a, 0x14),
		"ChromeBasePryHover": hx(0xefdef6),
		"SidebarRule":        hxa(0xe8e8e8, 0x21),
		"ChromeA":            hx(0x4a154b),
		"ChromeB":            hx(0x39063a),
	}},
	{"default dark", ``, true, "dark", "dark", map[string]color.NRGBA{
		"ChromeText":         hx(0xf8f8f8),
		"ChromeTextDim":      hxa(0xe3ceeb, 0xcc),
		"ChromeActive":       hxa(0xefe1f5, 0x40),
		"ChromeHover":        hxa(0xefe1f5, 0x40),
		"ChromePressed":      hxa(0x101214, 0x99),
		"SearchBg":           hxa(0xefe1f5, 0x40),
		"SearchText":         hx(0xf8f8f8),
		"RailBadge":          hx(0xcfa1de),
		"RailBadgeText":      hx(0x200221),
		"SidebarBg":          hxa(0x101214, 0x99),
		"SidebarText":        hxa(0xe3ceeb, 0xcc),
		"SidebarTextUnread":  hx(0xf8f8f8),
		"SidebarHover":       hxa(0xefe1f5, 0x14),
		"SidebarActive":      hx(0x7d3986),
		"SidebarActiveText":  hx(0xefe1f5),
		"SidebarDivider":     hxa(0xefe1f5, 0x38),
		"SidebarButton":      hxa(0xefe1f5, 0x40),
		"Badge":              hx(0xcfa1de),
		"BadgeText":          hx(0x200221),
		"SidebarTextTer":     hxa(0xe3ceeb, 0x99),
		"ChromeBasePry":      hx(0xe3ceeb),
		"ChromeContentTer":   hx(0x200221),
		"ChromeSurfInvTer":   hxa(0xefe1f5, 0x14),
		"ChromeOtlInvPry":    hxa(0xefe1f5, 0x38),
		"ThemeImp":           hx(0xcfa1de),
		"ThemeImpText":       hx(0x200221),
		"ThemeInvSec":        hxa(0xe3ceeb, 0xcc),
		"ThemeInvTer":        hxa(0xe3ceeb, 0x99),
		"ThemeOtlInvPry":     hxa(0xefe1f5, 0x38),
		"Accent":             hx(0xe3ceeb),
		"Presence":           hx(0x3daa7c),
		"ChromeHoverTint":    hxa(0xefe1f5, 0x14),
		"ChromeBasePryHover": hx(0xe4d0ec),
		"SidebarRule":        hxa(0xe8e8e8, 0x21),
		"ChromeA":            hx(0x431349),
		"ChromeB":            hx(0x200221),
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
				got := *get(&p)
				if base := LightPalette; got.A == 0 {
					if c.dark {
						base = DarkPalette
					}
					if *get(&base) == (color.NRGBA{}) {
						// a field the palette leaves unset (ThemeImp, the
						// root scope's ThemeInv*): the default theme's
						// value, which the views keep as their own
						// (palette.go)
						got = CachedFields(DefaultIATheme, c.dark, true)[k]
					}
				}
				if got != c.want[k] {
					t.Errorf("%s = %s, Slack %s", k, inkHex(got), inkHex(c.want[k]))
				}
			}
		})
	}
}

// A stylesheet's --sk_foreground_low recolours the sidebar's rules where
// it reaches them: in dark mode, in a light sidebar scope and declared on
// .p-view_contents--sidebar itself. A light client's inverted scope
// redeclares the variable, so a root override leaves its rules alone.
func TestSidebarRuleFollowsSheets(t *testing.T) {
	want := color.NRGBA{R: 0x80, G: 0x10, B: 0x10, A: 0x21}
	tk := fakeTokens{"rgba(var(--sk_foreground_low),0.13)": want, "var(--sk_foreground_low)": want} // the sheet declares it
	dark := DarkPalette
	ApplyTokens(&dark, tk, true)
	if dark.SidebarRule != want {
		t.Errorf("dark: SidebarRule %s, the sheet's %s", inkHex(dark.SidebarRule), inkHex(want))
	}
	inverted := LightPalette // Slack's default theme: the inverted sidebar scope
	ApplyTokens(&inverted, tk, false)
	if inverted.SidebarRule != LightPalette.SidebarRule {
		t.Errorf("light, inverted sidebar: SidebarRule %s; the scope's own %s", inkHex(inverted.SidebarRule), inkHex(LightPalette.SidebarRule))
	}
	lightSide := LightPalette
	ApplyIATheme(&lightSide, mustTheme(t, slackInkCases[0].ia), false, true) // karim picker: a light sidebar
	ApplyTokens(&lightSide, tk, false)
	if lightSide.SidebarRule != want {
		t.Errorf("light sidebar scope: SidebarRule %s, the sheet's %s", inkHex(lightSide.SidebarRule), inkHex(want))
	}
	own := LightPalette
	ApplySidebarTokens(&own, tk)
	if own.SidebarRule != want {
		t.Errorf("declared on the sidebar: SidebarRule %s, the sheet's %s", inkHex(own.SidebarRule), inkHex(want))
	}
}
