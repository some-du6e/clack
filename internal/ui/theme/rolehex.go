package theme

import (
	"fmt"
	"strings"
)

// roleHexShades is getPaletteShadeFromBrightness's table for the
// variables the Custom theme tab shows, {light, dark,
// light-inverted-sidebar} (modern.vendor KlX6 eM).
var roleHexShades = map[string][3]string{
	"base-hgl-1": {"80", "30", "0"},
	"base-hgl-2": {"70", "60", "30"},
	"base-imp":   {"70", "70", "20"},
}

// RoleHex is the colour Slack's Custom theme tab shows for a role
// ("primary", "highlight1", "highlight2", "important"): the role's own hex,
// else its palette at the shade the theme uses (getConicGradientShadeColor
// for primary in light mode, getPaletteShadeFromBrightness for the rest
// in the client's mode). Like Slack's CustomTheme, Notifications without
// a hex reads highlight1's palette. Upper-case "#RRGGBB".
func (t IATheme) RoleHex(role string, dark bool) string {
	b := t.Brightness
	if b == 0 {
		b = 5
	}
	m := modeLight
	switch {
	case dark:
		m = modeDark
	case t.SidebarInverted:
		m = modeInverted
	}
	var r IARole
	var shade string
	switch role {
	case "primary":
		r, shade = t.Primary, gradientShade(modeLight, "primary", b)
	case "highlight1":
		r, shade = t.Highlight1, roleHexShades["base-hgl-1"][m]
	case "highlight2":
		r, shade = t.Highlight2, roleHexShades["base-hgl-2"][m]
	case "important":
		r, shade = t.Important, roleHexShades["base-imp"][m]
		if r.Custom == "" {
			r.Palette = t.Highlight1.Palette
		}
	default:
		return ""
	}
	if r.Custom != "" {
		return strings.ToUpper(r.Custom)
	}
	if b < 1 || b > 6 { // getPaletteShadeFromBrightness: unknown brightness is "0"
		shade = "0"
	}
	c := namedRamp(r.Palette, false)[shadeIndex(shade)]
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

// ParseHexColor reads "#RGB"/"#RRGGBB" (with or without "#") as Slack's
// hex inputs take it, returning the normalised "#RRGGBB".
func ParseHexColor(s string) (string, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return "", false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return "", false
		}
	}
	return "#" + strings.ToUpper(s), true
}
