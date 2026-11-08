package theme

import (
	"encoding/json"
	"image/color"
	"strconv"
	"strings"
)

// Slack's 2026 account theme (users.prefs ia_theme), and how Slack's web
// client turns it into the --dt_color-theme-* tokens and the window
// gradient. Everything here follows Slack's theme module (gantry-v2-vendors
// "KlX6": getThemeVariables, getConicGradient, getFlatBackground, the
// eL shade table and the preset list), read from its public CDN bundle on
// 2026-11-08. The ramps are Slack's --dt_color-plt-* (plt_ramps.go);
// custom colours get Material HCT tonal ramps like Slack's (hct.go).
//
// Light/dark is not part of ia_theme: Slack keeps "userTheme" and
// "systemThemeSyncEnabled" in the client's localStorage, per device, so
// Clack keeps them in ui.json.

// IARole is one theme role: a named Slack palette, or a custom hex used
// when the theme's UseCustomHex is set.
type IARole struct {
	Palette string `json:"palette"`
	Custom  string `json:"custom,omitempty"`
}

// IATheme is users.prefs ia_theme.
type IATheme struct {
	Primary         IARole `json:"primary"`
	Highlight1      IARole `json:"highlight1"`
	Highlight2      IARole `json:"highlight2"`
	Important       IARole `json:"important"`
	Brightness      int    `json:"brightness"`
	SidebarInverted bool   `json:"sidebarInverted"`
	UseCustomHex    bool   `json:"useCustomHex"`
	HighContrast    bool   `json:"highContrast,omitempty"`
}

// DefaultIATheme is Slack's default (its eD: Aubergine, brightness 6, an
// inverted sidebar).
var DefaultIATheme = IATheme{
	Primary: IARole{Palette: "aubergine"}, Highlight1: IARole{Palette: "aubergine"},
	Highlight2: IARole{Palette: "jade"}, Important: IARole{Palette: "aubergine"},
	Brightness: 6, SidebarInverted: true,
}

// ParseIATheme reads ia_theme's JSON ("" or invalid: ok=false). Missing
// fields take Slack's defaults, like its {...eD, ...payload} merge.
func ParseIATheme(s string) (IATheme, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return DefaultIATheme, false
	}
	var raw struct {
		Primary, Highlight1, Highlight2, Important  *IARole
		Brightness                                  *int
		SidebarInverted, UseCustomHex, HighContrast *bool
	}
	if json.Unmarshal([]byte(s), &raw) != nil {
		return DefaultIATheme, false
	}
	t := DefaultIATheme
	for _, r := range []struct {
		src *IARole
		dst *IARole
	}{{raw.Primary, &t.Primary}, {raw.Highlight1, &t.Highlight1}, {raw.Highlight2, &t.Highlight2}, {raw.Important, &t.Important}} {
		if r.src != nil {
			*r.dst = *r.src
			if pltRamps[r.dst.Palette] == ([2][12]uint32{}) && r.dst.Custom == "" {
				r.dst.Palette = "aubergine" // unknown palette: Slack's CSS var would be unset
			}
		}
	}
	if raw.Brightness != nil {
		t.Brightness = *raw.Brightness
	}
	if raw.SidebarInverted != nil {
		t.SidebarInverted = *raw.SidebarInverted
	}
	if raw.UseCustomHex != nil {
		t.UseCustomHex = *raw.UseCustomHex
	}
	if raw.HighContrast != nil {
		t.HighContrast = *raw.HighContrast
	}
	return t, true
}

// JSON is the value Slack's setIaThemePrefByApi writes (the theme without
// its mode).
func (t IATheme) JSON() string {
	b, _ := json.Marshal(t)
	return string(b)
}

// ForSave applies setIaThemePrefByApi's rule before writing: the sidebar
// can't be inverted in dark mode or at low brightness.
func (t IATheme) ForSave(dark bool) IATheme {
	b := t.Brightness
	if b == 0 {
		b = 5
	}
	t.SidebarInverted = !dark && t.SidebarInverted && b >= 3
	return t
}

// ThemePreset is one of Slack's named themes (Preferences > Appearance).
type ThemePreset struct {
	ID, Name, Group string
	Theme           IATheme
}

func preset(group, id, name string, b int, inv, custom bool, p, h1, h2, imp string, hex ...string) ThemePreset {
	r := func(pal string, i int) IARole {
		x := IARole{Palette: pal}
		if i < len(hex) {
			x.Custom = hex[i]
		}
		return x
	}
	return ThemePreset{ID: id, Name: name, Group: group, Theme: IATheme{
		Primary: r(p, 0), Highlight1: r(h1, 1), Highlight2: r(h2, 2), Important: r(imp, 3),
		Brightness: b, SidebarInverted: inv, UseCustomHex: custom}}
}

// ThemePresets are Slack's preset groups (its eR), in Slack's order.
var ThemePresets = []ThemePreset{
	preset("Single color", "aubergine", "Aubergine", 6, true, false, "aubergine", "aubergine", "jade", "aubergine"),
	preset("Single color", "clementine", "Clementine", 4, false, false, "tangerine", "tangerine", "jade", "tangerine"),
	preset("Single color", "banana", "Banana", 2, false, false, "sunflower", "sunflower", "jade", "sunflower"),
	preset("Single color", "jade", "Jade", 4, false, false, "jade", "jade", "jade", "jade"),
	preset("Single color", "lagoon", "Lagoon", 5, true, false, "lagoon", "lagoon", "jade", "lagoon"),
	preset("Single color", "barbra", "Barbra", 3, false, false, "flamingo", "flamingo", "jade", "flamingo"),
	preset("Single color", "gray", "Gray", 2, false, false, "gray", "gray", "jade", "flamingo"),
	preset("Single color", "mood-indigo", "Mood Indigo", 6, true, false, "indigo", "indigo", "jade", "indigo"),
	preset("Vision assistive", "tritanopia", "Tritanopia", 6, true, true, "aubergine", "jade", "jade", "paprika", "#000000", "#F8F8F8", "#20A271", "#EF3E75"),
	preset("Vision assistive", "protanopia-and-deuteranopia", "Protanopia & Deuteranopia", 6, true, true, "aubergine", "grass", "grass", "aubergine", "#2D0033", "#F9EDFF", "#20A271", "#EABDFB"),
	preset("Fun and new", "raspberry-beret", "Raspberry Beret", 3, false, false, "aubergine", "flamingo", "aubergine", "flamingo"),
	preset("Fun and new", "big-business", "Big Business", 4, true, false, "gray", "indigo", "indigo", "indigo"),
	preset("Fun and new", "pog", "POG", 3, true, false, "sangria", "honeycomb", "sunflower", "sunflower"),
	preset("Fun and new", "mint-chip", "Mint Chip", 3, false, false, "cilantro", "indigo", "indigo", "horchata"),
	preset("Fun and new", "pbj", "PB&J", 3, false, false, "horchata", "sangria", "sangria", "flamingo"),
	preset("Fun and new", "chill-vibes", "Chill Vibes", 4, true, false, "jade", "aquarium", "jade", "aquarium"),
	preset("Fun and new", "forest-floor", "Forest Floor", 4, false, false, "grass", "horchata", "sunflower", "sunflower"),
	preset("Fun and new", "slackr", "Slackr", 3, true, false, "flamingo", "lagoon", "mojito", "flamingo"),
	preset("Fun and new", "sea-glass", "Sea Glass", 3, false, false, "aquarium", "aubergine", "aquarium", "aubergine"),
	preset("Fun and new", "lemon-lime", "Lemon Lime", 4, false, false, "sunflower", "cilantro", "aubergine", "cilantro"),
	preset("Fun and new", "falling-leaves", "Falling Leaves", 4, true, false, "horchata", "sunflower", "tangerine", "tomato"),
	preset("Fun and new", "sunrise", "Sunrise", 2, false, false, "tangerine", "flamingo", "campfire", "honeycomb"),
	preset("Updated classics", "choco-mint", "Choco Mint", 5, true, false, "horchata", "jade", "gray", "jade"),
	preset("Updated classics", "cmyk", "CMYK", 3, false, false, "lagoon", "flamingo", "sunflower", "lagoon"),
	preset("Updated classics", "haberdashery", "Haberdashery", 6, true, false, "lagoon", "sunflower", "cilantro", "rose"),
	preset("Updated classics", "hoth", "Hoth", 1, false, false, "gray", "lagoon", "cilantro", "rose"),
	preset("Updated classics", "ochin", "Ochin", 5, false, false, "ocean", "ocean", "cilantro", "mojito"),
	preset("Updated classics", "sweet-treat", "Sweet Treat", 2, false, false, "tomato", "tomato", "horchata", "jade"),
}

// PaletteNames are Slack's named palettes (the custom-theme pickers).
var PaletteNames = []string{"tomato", "paprika", "tangerine", "campfire", "honeycomb", "sunflower", "grass", "cilantro",
	"mojito", "jade", "aquarium", "lagoon", "ocean", "indigo", "aubergine", "sangria", "flamingo", "rose", "horchata", "gray"}

// PresetID is the preset t matches ("" for a custom theme). Like Slack's
// matcher it compares the four roles and the brightness (and the custom
// hexes of custom-hex presets).
func (t IATheme) PresetID() string {
	for _, p := range ThemePresets {
		if p.Theme.sameColors(t) {
			return p.ID
		}
	}
	return ""
}

func (t IATheme) sameColors(o IATheme) bool {
	if t.Brightness != o.Brightness || t.UseCustomHex != o.UseCustomHex {
		return false
	}
	rs := [][2]IARole{{t.Primary, o.Primary}, {t.Highlight1, o.Highlight1}, {t.Highlight2, o.Highlight2}, {t.Important, o.Important}}
	for _, r := range rs {
		if r[0].Palette != r[1].Palette {
			return false
		}
		if t.UseCustomHex && !strings.EqualFold(r[0].Custom, r[1].Custom) {
			return false
		}
	}
	return true
}

// ---- token computation ----

// themeMode is Slack's theme mode: "light", "dark" or
// "light-inverted-sidebar" (a light client with Slack's dark sidebar).
type themeMode int

const (
	modeLight themeMode = iota
	modeDark
	modeInverted
)

// pltShades are Slack's shade labels; ramps are indexed in this order.
var pltShades = []string{"0", "5", "10", "20", "30", "40", "50", "60", "70", "80", "90", "100"}

func shadeIndex(s string) int {
	for i, x := range pltShades {
		if x == s {
			return i
		}
	}
	return 0
}

// themeShades is Slack's eL: the shade of each --dt_color-theme-* token
// per mode {light, dark, light-inverted-sidebar} (brightness only gates
// validity in Slack's getPaletteShadeFromBrightness).
var themeShades = map[string][3]string{
	"base-pry":        {"70", "90", "0"},
	"base-hgl-1":      {"80", "30", "0"},
	"base-imp":        {"70", "70", "20"},
	"base-inv-hgl-2":  {"50", "60", "50"},
	"surf-inv-pry":    {"100", "100", "0"},
	"surf-inv-sec":    {"0", "0", "0"},
	"surf-inv-ter":    {"80", "100", "0"},
	"content-inv-sec": {"100", "90", "5"},
	"content-hgl-1":   {"0", "100", "100"},
	"content-imp":     {"0", "0", "100"},
	"otl-inv-pry":     {"80", "100", "0"},
}

// ramp is a role's 12-shade ramp in the mode: its named palette, or the
// HCT ramp of its custom colour.
func (t IATheme) ramp(r IARole, m themeMode) [12]color.NRGBA {
	dark := m == modeDark
	if t.UseCustomHex {
		if c, ok := parseHex(r.Custom); ok {
			return CustomRamp(c, dark)
		}
	}
	return namedRamp(r.Palette, dark)
}

func namedRamp(name string, dark bool) [12]color.NRGBA {
	rs, ok := pltRamps[name]
	if !ok {
		rs = pltRamps["aubergine"]
	}
	i := 0
	if dark {
		i = 1
	}
	var out [12]color.NRGBA
	for k, v := range rs[i] {
		out[k] = rgb(v)
	}
	return out
}

func (t IATheme) custom(r IARole) (color.NRGBA, bool) {
	if !t.UseCustomHex {
		return color.NRGBA{}, false
	}
	return parseHex(r.Custom)
}

// token is a role's --dt_color-theme-<name> in the mode.
func (t IATheme) token(r IARole, name string, m themeMode) color.NRGBA {
	return t.ramp(r, m)[shadeIndex(themeShades[name][m])]
}

// baseToken is a base token with Slack's dynamic content: a custom colour
// is used as is (getThemeBaseVariableDefinitionWithDynamicContent).
func (t IATheme) baseToken(r IARole, name string, m themeMode) color.NRGBA {
	if c, ok := t.custom(r); ok {
		return c
	}
	return t.token(r, name, m)
}

// contentToken is the content token paired with a base token: on a custom
// colour, shade 100 or 0 of the role's ramp by the colour's HCT tone
// (≥66: dark text), flipped in dark mode.
func (t IATheme) contentToken(r IARole, name string, m themeMode) color.NRGBA {
	if c, ok := t.custom(r); ok {
		dark := HexTone(c) >= 66
		if m == modeDark {
			dark = !dark
		}
		rp := t.ramp(r, m)
		if dark {
			return rp[11]
		}
		return rp[0]
	}
	return t.token(r, name, m)
}

// gradientShade is Slack's getConicGradientShadeColor.
func gradientShade(m themeMode, role string, b int) string {
	if role == "primary" {
		if m == modeDark {
			return "10"
		}
		switch b {
		case 2:
			return "5"
		case 3:
			return "10"
		case 4:
			if m == modeInverted {
				return "70"
			}
			return "60"
		case 5:
			if m == modeInverted {
				return "80"
			}
			return "70"
		case 6:
			if m == modeInverted {
				return "90"
			}
			return "80"
		case 1:
			return "0" // Slack's case 1 falls out of the switch to "0"
		default:
			return "70"
		}
	}
	if m == modeDark {
		return "0"
	}
	switch b {
	case 1:
		return "10"
	case 2:
		return "20"
	case 3:
		return "30"
	case 4:
		return "80"
	case 6:
		return "100"
	default:
		return "90"
	}
}

// brightnessFromShade is Slack's getBrightnessFromPaletteShade.
func brightnessFromShade(s string) int {
	switch s {
	case "10", "20":
		return 2
	case "30", "40":
		return 3
	case "50", "60":
		return 4
	case "70", "80":
		return 5
	case "90", "100":
		return 6
	}
	return 1
}

// customFlatShade is Slack's getCustomHexFlatBackgroundShade.
func customFlatShade(m themeMode, hex color.NRGBA) string {
	b := brightnessFromShade(pltShades[CustomKeyShade(hex, false)])
	z := gradientShade(m, "primary", b)
	y := gradientShade(m, "highlight1", b)
	j := pltShades[CustomKeyShade(hex, m == modeDark)]
	zi, yi, ji := atoi(z), atoi(y), atoi(j)
	switch {
	case m == modeDark && ji <= zi:
		return j
	case m == modeDark:
		return z
	case yi <= ji:
		return y
	case ji <= zi:
		return z
	}
	return j
}

// customH1Shade is Slack's getCustomHexHighlght1ConicGradientShadeColor.
func customH1Shade(m themeMode, primaryShade string) string {
	if m == modeDark {
		return "0"
	}
	switch primaryShade {
	case "0":
		return "10"
	case "5":
		return "20"
	case "10", "20", "30", "40":
		return "30"
	case "50", "60":
		return "80"
	case "70":
		return "90"
	}
	return "100"
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// chrome is the window background: Slack's conic gradient stops (primary
// at the centre and 25/75%, highlight1 at 0/50/100%), or with gradients
// off its flat getFlatBackground colour in all three.
func (t IATheme) chrome(m themeMode, gradients bool) (a, b color.NRGBA) {
	bright := t.Brightness
	if bright == 0 {
		bright = 5
	}
	pc, pCustom := t.custom(t.Primary)
	if !gradients {
		var shade string
		if pCustom {
			shade = customFlatShade(m, pc)
		} else {
			role := "primary"
			if m == modeInverted {
				role = "highlight1"
			}
			shade = gradientShade(m, role, bright)
		}
		c := t.ramp(t.Primary, m)[shadeIndex(shade)]
		return c, c
	}
	var ps, hs string
	if pCustom {
		ps = customFlatShade(m, pc)
	} else {
		ps = gradientShade(m, "primary", bright)
	}
	if _, ok := t.custom(t.Highlight1); ok {
		hs = customH1Shade(m, ps)
	} else {
		hs = gradientShade(m, "highlight1", bright)
	}
	return t.ramp(t.Primary, m)[shadeIndex(ps)], t.ramp(t.Highlight1, m)[shadeIndex(hs)]
}

// contentInvPry is Slack's getThemeContentInvPryValue (chrome and unread
// sidebar text).
func (t IATheme) contentInvPry(m themeMode) color.NRGBA {
	switch {
	case m == modeDark && t.HighContrast, m == modeInverted:
		return rgb(0xffffff)
	case m == modeDark:
		return namedRamp("gray", true)[11]
	}
	return t.ramp(t.Primary, m)[11]
}

// chromeAlpha are the alphas Slack's stylesheet gives the translucent
// sidebar/chrome tokens, per mode {light, dark, inverted} (slackvars.go's
// --dt_color-theme-* and the inverted-sidebar scope).
var chromeAlpha = map[string][3]uint8{
	"content-inv-sec": {0xcc, 0xcc, 0xe6}, // .8 .8 .9
	"surf-inv-pry":    {0x21, 0x40, 0x40}, // .13 .25 .25
	"surf-inv-ter":    {0x21, 0x14, 0x14}, // .13 .08 .08
	"otl-inv-pry":     {0x21, 0x38, 0x38}, // .13 .22 .22
	"surf-inv-sec":    {0xe6, 0x99, 0x1c}, // .9 .6 .11
}

func withA(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }

// accountFields maps the Palette fields the account theme drives.
var accountFields = map[string]func(*Palette) *color.NRGBA{
	"ChromeA": func(p *Palette) *color.NRGBA { return &p.ChromeA }, "ChromeB": func(p *Palette) *color.NRGBA { return &p.ChromeB },
	"ChromeC": func(p *Palette) *color.NRGBA { return &p.ChromeC }, "ChromeText": func(p *Palette) *color.NRGBA { return &p.ChromeText },
	"ChromeTextDim": func(p *Palette) *color.NRGBA { return &p.ChromeTextDim }, "ChromeHover": func(p *Palette) *color.NRGBA { return &p.ChromeHover },
	"ChromeActive": func(p *Palette) *color.NRGBA { return &p.ChromeActive }, "SearchBg": func(p *Palette) *color.NRGBA { return &p.SearchBg },
	"SearchHover": func(p *Palette) *color.NRGBA { return &p.SearchHover }, "SearchText": func(p *Palette) *color.NRGBA { return &p.SearchText },
	"SidebarBg": func(p *Palette) *color.NRGBA { return &p.SidebarBg }, "SidebarText": func(p *Palette) *color.NRGBA { return &p.SidebarText },
	"SidebarTextUnread": func(p *Palette) *color.NRGBA { return &p.SidebarTextUnread }, "SidebarHover": func(p *Palette) *color.NRGBA { return &p.SidebarHover },
	"SidebarActive": func(p *Palette) *color.NRGBA { return &p.SidebarActive }, "SidebarActiveText": func(p *Palette) *color.NRGBA { return &p.SidebarActiveText },
	"SidebarDivider": func(p *Palette) *color.NRGBA { return &p.SidebarDivider }, "SidebarButton": func(p *Palette) *color.NRGBA { return &p.SidebarButton },
	"Badge": func(p *Palette) *color.NRGBA { return &p.Badge }, "BadgeText": func(p *Palette) *color.NRGBA { return &p.BadgeText },
	"Accent": func(p *Palette) *color.NRGBA { return &p.Accent }, "Presence": func(p *Palette) *color.NRGBA { return &p.Presence },
}

// Fields computes the theme's colour for each Palette field it drives
// (by field name). dark is the client mode; gradients is
// window_background_gradients.
func (t IATheme) Fields(dark, gradients bool) map[string]color.NRGBA {
	root := modeLight
	if dark {
		root = modeDark
	}
	side := root // the chrome and sidebar scope
	if !dark && t.SidebarInverted {
		side = modeInverted
	}
	al := func(k string) uint8 { return chromeAlpha[k][side] }
	out := map[string]color.NRGBA{}
	ca, cb := t.chrome(side, gradients)
	out["ChromeA"], out["ChromeB"], out["ChromeC"] = ca, cb, ca

	pr := t.Primary
	inv := t.contentInvPry(side)
	out["ChromeText"], out["SearchText"], out["SidebarTextUnread"] = inv, inv, inv
	sec := withA(t.token(pr, "content-inv-sec", side), al("content-inv-sec"))
	out["ChromeTextDim"], out["SidebarText"] = sec, sec
	sp := t.token(pr, "surf-inv-pry", side)
	act := withA(sp, al("surf-inv-pry"))
	out["ChromeActive"], out["SearchBg"], out["SidebarButton"] = act, act, act
	out["ChromeHover"], out["SearchHover"] = withA(sp, 0x26), withA(sp, 0x55)
	switch side {
	case modeLight:
		out["SidebarBg"] = withA(rgb(0xffffff), al("surf-inv-sec"))
	case modeDark:
		out["SidebarBg"] = withA(namedRamp("gray", true)[0], al("surf-inv-sec"))
	default:
		out["SidebarBg"] = withA(t.token(pr, "surf-inv-sec", side), al("surf-inv-sec"))
	}
	out["SidebarHover"] = withA(t.token(pr, "surf-inv-ter", side), al("surf-inv-ter"))
	out["SidebarDivider"] = withA(t.token(pr, "otl-inv-pry", side), al("otl-inv-pry"))
	out["SidebarActive"] = t.baseToken(t.Highlight1, "base-hgl-1", side)
	out["SidebarActiveText"] = t.contentToken(t.Highlight1, "content-hgl-1", side)
	out["Badge"] = t.baseToken(t.Important, "base-imp", side)
	out["BadgeText"] = t.contentToken(t.Important, "content-imp", side)
	// the main pane is in the root mode
	out["Accent"] = t.token(pr, "base-pry", root)
	out["Presence"] = t.baseToken(t.Highlight2, "base-inv-hgl-2", root)
	return out
}

// ApplyIATheme lays the account theme over p (a LightPalette or
// DarkPalette copy, before stylesheet tokens, so Slick themes and user CSS
// still win). A field changes only where the theme's colour differs from
// what Slack's default theme gives, so the default theme keeps Clack's
// measured palette exactly. gradients is window_background_gradients.
func ApplyIATheme(p *Palette, t IATheme, dark, gradients bool) {
	def := DefaultIATheme.Fields(dark, true)
	for k, v := range t.Fields(dark, gradients) {
		if def[k] != v {
			*accountFields[k](p) = v
		}
	}
}

func parseHex(s string) (color.NRGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return color.NRGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return rgb(uint32(v)), true
}

// PaletteSwatch is a named palette's picker colour (shade 70 of the mode's
// ramp).
func PaletteSwatch(name string, dark bool) color.NRGBA {
	return namedRamp(name, dark)[shadeIndex("70")]
}
