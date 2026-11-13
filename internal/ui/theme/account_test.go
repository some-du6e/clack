package theme

import (
	"image/color"
	"testing"
)

func hx(v uint32) color.NRGBA { return rgb(v) }

func hxa(v uint32, a uint8) color.NRGBA { c := rgb(v); c.A = a; return c }

// Slack's default theme computes to Clack's measured Aubergine chrome,
// and applying it changes nothing.
func TestIAThemeDefaultMatchesMeasured(t *testing.T) {
	l := DefaultIATheme.Fields(false, true)
	for k, want := range map[string]color.NRGBA{
		"ChromeA": LightPalette.ChromeA, "ChromeB": LightPalette.ChromeB, "SidebarActive": LightPalette.SidebarActive,
		"SidebarActiveText": LightPalette.SidebarActiveText, "Badge": LightPalette.Badge, "BadgeText": LightPalette.BadgeText,
		"SidebarText": LightPalette.SidebarText, "ChromeTextDim": LightPalette.ChromeTextDim, "ChromeActive": LightPalette.ChromeActive,
		"SidebarBg": LightPalette.SidebarBg, "SidebarTextUnread": LightPalette.SidebarTextUnread, "Accent": LightPalette.Accent,
		"Presence": LightPalette.Presence,
	} {
		if l[k] != want {
			t.Errorf("light %s = %v, measured %v", k, l[k], want)
		}
	}
	d := DefaultIATheme.Fields(true, true)
	for k, want := range map[string]color.NRGBA{
		"ChromeA": DarkPalette.ChromeA, "ChromeB": DarkPalette.ChromeB, "SidebarActive": DarkPalette.SidebarActive,
	} {
		if d[k] != want {
			t.Errorf("dark %s = %v, measured %v", k, d[k], want)
		}
	}
	for _, dark := range []bool{false, true} {
		p := LightPalette
		if dark {
			p = DarkPalette
		}
		q := p
		ApplyIATheme(&q, DefaultIATheme, dark, true)
		if q != p {
			t.Errorf("dark=%v: the default theme changed the palette", dark)
		}
	}
}

func mustTheme(t *testing.T, s string) IATheme {
	t.Helper()
	th, ok := ParseIATheme(s)
	if !ok {
		t.Fatalf("ParseIATheme(%s) failed", s)
	}
	return th
}

// Ochin (light sidebar, brightness 5) as Slack stores it in ia_theme.
func TestIAThemeNamedOchin(t *testing.T) {
	th := mustTheme(t, `{"primary":{"palette":"ocean"},"highlight1":{"palette":"ocean"},"highlight2":{"palette":"cilantro"},"important":{"palette":"mojito"},"brightness":5,"sidebarInverted":false,"useCustomHex":false}`)
	if id := th.PresetID(); id != "ochin" {
		t.Errorf("PresetID = %q", id)
	}
	p := LightPalette
	ApplyIATheme(&p, th, false, true)
	for k, want := range map[string]color.NRGBA{
		"ChromeA":           hx(0x1264a3), // ocean-70 (primary, brightness 5, light)
		"ChromeB":           hx(0x093a73), // ocean-90 (highlight1)
		"SidebarBg":         hxa(0xffffff, 0xe6),
		"SidebarText":       hxa(0x122a59, 0xcc), // content-inv-sec: ocean-100 at .8
		"SidebarTextUnread": hx(0x122a59),        // content-inv-pry light: plt-100
		"SidebarActive":     hx(0x0b4c8c),        // base-hgl-1 light: ocean-80
		"SidebarActiveText": hx(0xe6f5fe),        // content-hgl-1 light: ocean-0
		"Badge":             hx(0x1a8343),        // base-imp light: mojito-70
		"BadgeText":         hx(0xe9ffec),        // content-imp: mojito-0
		"Presence":          hx(0x379e15),        // base-inv-hgl-2 light: cilantro-50
		"Accent":            hx(0x1264a3),        // base-pry light: ocean-70
	} {
		if got := *accountFields[k](&p); got != want {
			t.Errorf("%s = %v, want %v", k, got, want)
		}
	}
	// fields the theme doesn't drive keep Clack's values
	if p.Bg != LightPalette.Bg || p.Link != LightPalette.Link {
		t.Error("main pane colours changed")
	}
}

// Slack's scopes (udDV isSidebarInverted / getBackgroundMode, y4F2
// useInvertedTheme): a light client at a dark brightness puts the top nav
// and rail in the inverted scope whether or not Darker sidebars is on;
// the sidebar and the window gradient follow sidebarInverted; at
// brightness 3 or less nothing inverts.
func TestIAThemeInvertedScopes(t *testing.T) {
	ochin := mustTheme(t, `{"primary":{"palette":"ocean"},"highlight1":{"palette":"ocean"},"highlight2":{"palette":"cilantro"},"important":{"palette":"mojito"},"brightness":5,"sidebarInverted":false,"useCustomHex":false}`)
	if !ochin.InvertedChrome(false) || ochin.InvertedSidebar(false) || ochin.InvertedChrome(true) {
		t.Fatalf("ochin scopes: chrome %v sidebar %v", ochin.InvertedChrome(false), ochin.InvertedSidebar(false))
	}
	f := ochin.Fields(false, true)
	inv := namedRamp("ocean", false)
	for k, want := range map[string]color.NRGBA{
		"ChromeText":    hx(0xffffff),                  // content-inv-pry, inverted
		"ChromeTextDim": withA(inv[1], 0xe6),           // content-inv-sec: ocean-5 at .9
		"ChromeActive":  withA(inv[0], 0x40),           // surf-inv-pry: ocean-0 at .25
		"RailBadge":     namedRamp("mojito", false)[3], // base-imp inverted: mojito-20
		"SidebarText":   hxa(0x122a59, 0xcc),           // the sidebar stays light
		"Badge":         hx(0x1a8343),                  // and its badge: mojito-70
		"ChromeA":       hx(0x1264a3),                  // the gradient in the light mode
	} {
		if f[k] != want {
			t.Errorf("ochin %s = %v, want %v", k, f[k], want)
		}
	}
	// Ochin with Darker sidebars: the sidebar and gradient invert too
	ochin.SidebarInverted = true
	f = ochin.Fields(false, true)
	if f["SidebarTextUnread"] != hx(0xffffff) || f["ChromeA"] != inv[shadeIndex("80")] {
		t.Errorf("ochin inverted: unread %v chrome %v", f["SidebarTextUnread"], f["ChromeA"])
	}
	// brightness 3 with Darker sidebars stored: Slack inverts nothing (the
	// pale shade-10 gradient keeps the light scope's dark text)
	low := DefaultIATheme
	low.Brightness = 3
	if low.InvertedChrome(false) || low.InvertedSidebar(false) {
		t.Fatal("brightness 3 inverts")
	}
	f = low.Fields(false, true)
	aub := namedRamp("aubergine", false)
	if f["ChromeA"] != aub[shadeIndex("10")] || f["ChromeText"] != aub[11] || f["SidebarTextUnread"] != aub[11] || f["SidebarBg"] != hxa(0xffffff, 0xe6) {
		t.Errorf("brightness 3: chrome %v text %v unread %v bg %v", f["ChromeA"], f["ChromeText"], f["SidebarTextUnread"], f["SidebarBg"])
	}
	// the default theme's fields are Clack's measured palette (unchanged)
	d := DefaultIATheme.Fields(false, true)
	if d["RailBadge"] != LightPalette.RailBadge || d["RailBadgeText"] != LightPalette.RailBadgeText {
		t.Errorf("default rail badge %v %v", d["RailBadge"], d["RailBadgeText"])
	}
}

func TestIAThemeDarkAndBrightness(t *testing.T) {
	th := mustTheme(t, `{"primary":{"palette":"ocean"},"highlight1":{"palette":"ocean"},"highlight2":{"palette":"cilantro"},"important":{"palette":"mojito"},"brightness":5}`)
	f := th.Fields(true, true)
	ocean := namedRamp("ocean", true)
	if f["ChromeA"] != ocean[2] || f["ChromeB"] != ocean[0] { // dark: primary 10, highlight1 0
		t.Errorf("dark chrome = %v %v", f["ChromeA"], f["ChromeB"])
	}
	// Hoth: brightness 1, gray / lagoon
	h := ThemePresets[presetIndex(t, "hoth")].Theme
	f = h.Fields(false, true)
	if f["ChromeA"] != hx(0xf8f8f8) || f["ChromeB"] != hx(0x9fe8fd) { // gray-0, lagoon-10
		t.Errorf("hoth chrome = %v %v", f["ChromeA"], f["ChromeB"])
	}
	// Lagoon, inverted, brightness 5: primary 80, highlight1 90
	lg := ThemePresets[presetIndex(t, "lagoon")].Theme
	f = lg.Fields(false, true)
	if f["ChromeA"] != hx(0x0b527b) || f["ChromeB"] != hx(0x1c405f) || f["SidebarTextUnread"] != hx(0xffffff) {
		t.Errorf("lagoon = %v %v %v", f["ChromeA"], f["ChromeB"], f["SidebarTextUnread"])
	}
}

func presetIndex(t *testing.T, id string) int {
	for i, p := range ThemePresets {
		if p.ID == id {
			return i
		}
	}
	t.Fatalf("no preset %s", id)
	return 0
}

// Window gradient off: Slack's flat getFlatBackground colour.
func TestIAThemeNoGradients(t *testing.T) {
	f := DefaultIATheme.Fields(false, false)
	// inverted sidebar: highlight1's brightness-6 shade (100) on the primary palette
	if f["ChromeA"] != hx(0x39063a) || f["ChromeB"] != hx(0x39063a) || f["ChromeC"] != hx(0x39063a) {
		t.Errorf("flat = %v %v %v", f["ChromeA"], f["ChromeB"], f["ChromeC"])
	}
	p := LightPalette
	ApplyIATheme(&p, DefaultIATheme, false, false)
	if p.ChromeA != p.ChromeB {
		t.Error("gradients off still draws a gradient")
	}
}

// Custom colours: base tokens use the hex as is; content picks the
// custom ramp's end by the hex's tone.
func TestIAThemeCustomHex(t *testing.T) {
	th := mustTheme(t, `{"primary":{"palette":"aubergine","custom":"#000000"},"highlight1":{"palette":"jade","custom":"#F8F8F8"},"highlight2":{"palette":"jade","custom":"#20A271"},"important":{"palette":"paprika","custom":"#EF3E75"},"brightness":6,"sidebarInverted":true,"useCustomHex":true}`)
	if id := th.PresetID(); id != "tritanopia" {
		t.Errorf("PresetID = %q", id)
	}
	f := th.Fields(false, true)
	if f["SidebarActive"] != hx(0xf8f8f8) || f["Badge"] != hx(0xef3e75) || f["Presence"] != hx(0x20a271) {
		t.Errorf("custom bases = %v %v %v", f["SidebarActive"], f["Badge"], f["Presence"])
	}
	// #F8F8F8 is light (tone ≥ 66): dark text, the ramp's 100
	if want := CustomRamp(hx(0xf8f8f8), false)[11]; f["SidebarActiveText"] != want {
		t.Errorf("content-hgl-1 = %v, want %v", f["SidebarActiveText"], want)
	}
	if want := CustomRamp(hx(0xef3e75), false)[0]; f["BadgeText"] != want {
		t.Errorf("content-imp = %v, want %v", f["BadgeText"], want)
	}
	// the same theme without useCustomHex uses the palettes
	th.UseCustomHex = false
	if f := th.Fields(false, true); f["Badge"] != hx(0xe8a991) { // paprika-20 (inverted base-imp)
		t.Errorf("palette badge = %v", f["Badge"])
	}
}

func TestParseIATheme(t *testing.T) {
	if _, ok := ParseIATheme(""); ok {
		t.Error("empty parsed")
	}
	if _, ok := ParseIATheme("{nope"); ok {
		t.Error("invalid parsed")
	}
	th := mustTheme(t, `{"primary":{"palette":"jade"}}`) // missing fields: Slack's defaults
	if th.Highlight2.Palette != "jade" || th.Brightness != 6 || !th.SidebarInverted || th.Important.Palette != "aubergine" {
		t.Errorf("defaults = %+v", th)
	}
	if th := mustTheme(t, `{"primary":{"palette":"nonsense"}}`); th.Primary.Palette != "aubergine" {
		t.Errorf("unknown palette kept: %+v", th.Primary)
	}
	back := mustTheme(t, ThemePresets[3].Theme.JSON())
	if back != ThemePresets[3].Theme {
		t.Errorf("JSON round trip: %+v", back)
	}
	// setIaThemePrefByApi's rule: no inverted sidebar in dark mode or at brightness 3 or less
	if ThemePresets[0].Theme.ForSave(true).SidebarInverted {
		t.Error("ForSave(dark) kept sidebarInverted")
	}
	low := ThemePresets[0].Theme
	low.Brightness = 3 // isSidebarInverted is brightness > 3
	if low.ForSave(false).SidebarInverted {
		t.Error("ForSave(brightness 3) kept sidebarInverted")
	}
}

// Every preset names Slack palettes the ramps know.
func TestThemePresetsPalettes(t *testing.T) {
	if len(ThemePresets) != 28 {
		t.Errorf("%d presets", len(ThemePresets))
	}
	for _, p := range ThemePresets {
		for _, r := range []IARole{p.Theme.Primary, p.Theme.Highlight1, p.Theme.Highlight2, p.Theme.Important} {
			if _, ok := pltRamps[r.Palette]; !ok {
				t.Errorf("%s: palette %q", p.ID, r.Palette)
			}
		}
		if got := p.Theme.PresetID(); got != p.ID && !dupColors(p) {
			t.Errorf("%s matches %s", p.ID, got)
		}
	}
}

func dupColors(p ThemePreset) bool {
	for _, q := range ThemePresets {
		if q.ID != p.ID && q.Theme.sameColors(p.Theme) {
			return true
		}
	}
	return false
}
