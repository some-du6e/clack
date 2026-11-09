package theme

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Slack's theme import and Surprise me (CDN build a84662d2d135).

// ImportThemeString is createPayloadFromHexStrings (client-boot udDV) with
// ThemeImportHexModalV2's input handling: the string upper-cased, spaces
// dropped, split on commas. Four hexes are the four roles' custom colours;
// eight or ten are a legacy theme string (column_bg, menu_bg, active_item,
// active_item_text, hover_item, text_color, active_presence, badge[, top_nav
// …]), whose column_bg, active_item, active_presence and badge become the
// roles, as a named palette when the hex is one of its shades. Brightness is
// the matching preset's, else getBrightnessFromCustomHex of the primary
// custom colour (light), else the conic-gradient brightness of its palette
// shade; sidebarInverted is brightness > 3. ok is false for an invalid
// string ("Invalid theme string").
func ImportThemeString(cur IATheme, s string) (IATheme, bool) {
	cs := strings.Split(strings.ReplaceAll(strings.ToUpper(s), " ", ""), ",")
	if n := len(cs); n != 4 && n != 8 && n != 10 {
		return cur, false
	}
	for _, c := range cs {
		if !validImportHex(c) {
			return cur, false
		}
	}
	t := cur
	hex := [4]string{cs[0], cs[1], cs[2], cs[3]}
	if len(cs) != 4 {
		hex = [4]string{cs[0], cs[2], cs[6], cs[7]}
	}
	roles := []*IARole{&t.Primary, &t.Highlight1, &t.Highlight2, &t.Important}
	for i, r := range roles {
		if len(cs) == 4 {
			r.Custom = hex[i]
			continue
		}
		if pal, _, ok := paletteOfHex(hex[i]); ok {
			r.Palette, r.Custom = pal, ""
		} else {
			r.Custom = hex[i]
		}
	}
	t.UseCustomHex = false
	for _, r := range roles {
		if r.Custom != "" {
			t.UseCustomHex = true
		}
	}
	b := 0
	for _, p := range ThemePresets { // findMatchingPresetByColorsOnly: palettes only
		if p.Theme.Primary.Palette == t.Primary.Palette && p.Theme.Highlight1.Palette == t.Highlight1.Palette &&
			p.Theme.Highlight2.Palette == t.Highlight2.Palette && p.Theme.Important.Palette == t.Important.Palette {
			b = p.Theme.Brightness
			break
		}
	}
	if b == 0 {
		if t.Primary.Custom != "" {
			b = BrightnessFromCustomHex(t.Primary.Custom)
		} else {
			shade := "50"
			if _, sh, ok := paletteOfHex(hex[0]); ok {
				shade = sh
			}
			b = conicBrightnessFromShade(shade)
		}
	}
	t.Brightness = b
	t.SidebarInverted = b > 3 // isSidebarInverted
	return t, true
}

// validImportHex is $V: "#" and six hex digits.
func validImportHex(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if !strings.ContainsRune("0123456789ABCDEF", r) {
			return false
		}
	}
	return true
}

// paletteOfHex is getPaletteFromHex / getPaletteShadeFromHex: the named
// palette (and shade) whose light swatch is hex.
func paletteOfHex(hex string) (name, shade string, ok bool) {
	for _, p := range PaletteNames {
		for i, c := range namedRamp(p, false) {
			if strings.EqualFold(fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B), hex) {
				return p, pltShades[i], true
			}
		}
	}
	return "", "", false
}

// conicBrightnessFromShade is getConicGradientBrightnessFromShade.
func conicBrightnessFromShade(s string) int {
	switch s {
	case "0":
		return 1
	case "5":
		return 2
	case "10", "20", "30", "40":
		return 3
	case "50", "60":
		return 4
	case "70":
		return 5
	}
	return 6
}

// SurpriseTheme is ThemeSurprisePresetButton's theme: KlX6 randomizeTheme
// (each role a random named palette, a random brightness 1–6, sidebar
// inverted above 3) applied with useCustomHex true.
func SurpriseTheme(r *rand.Rand) IATheme {
	pick := func() IARole { return IARole{Palette: PaletteNames[r.IntN(len(PaletteNames))]} }
	b := 1 + r.IntN(6)
	return IATheme{Primary: pick(), Highlight1: pick(), Highlight2: pick(), Important: pick(),
		Brightness: b, SidebarInverted: b > 3, UseCustomHex: true}
}

// BrightnessFromCustomHex is getBrightnessFromCustomHex (t+RV S6) in light
// mode: the brightness of the custom ramp shade nearest hex's tone (0 for
// junk). Slack gives a custom primary colour this brightness.
func BrightnessFromCustomHex(hex string) int {
	c, ok := parseHex(hex)
	if !ok {
		return 0
	}
	return brightnessFromShade(pltShades[CustomKeyShade(c, false)])
}
