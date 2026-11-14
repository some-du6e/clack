package ui

import (
	"encoding/json"
	"image/color"
	"regexp"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// The caption buttons' symbol colour (titlebar.go) once Slack's client is
// up: boot-shared's useWindowThemedTitlebar (EbgN) sends the main window
// setTitleBarOverlay({color, symbolColor, height}) on Windows, with
// symbolColor e.symbolColor || j || A.top_nav_text. The top nav passes no
// symbolColor, and j (constants-white or theme-content-inv-pry, the
// colour injectCustomTitlebarStyling also gets) is only resolved when
// g7Mm's Jd() holds: a "custom-extension" titleBarStyle or the
// "window-chrome" debug feature, never Slack's titleBarStyle "hidden".
// So the symbols take A.top_nav_text, A being client-boot's OpEF ze: the
// legacy sidebar theme for the light or dark mode (udDV v7 / Pl), the
// sidebar_theme pref's named theme, or sidebar_theme_custom_values
// (convertCustomThemeToTheme: its top_nav_text, else ia_top_nav_theme's,
// else its text_color).

// wcoTopNavText is each named sidebar theme's top_nav_text: udDV's light
// table v7 (b: g with default_theme l.me) and dark table Pl (y: _).
// cotton_theme and eco_theme have none in either (void 0).
var wcoTopNavText = [2]map[string]string{
	{ // light
		"default_theme": "#FFFFFF", "aubergine_bright": "#FFFFFF",
		"aubergine_classic_theme": "#FFFFFF", "hoth_theme": "#383F45",
		"monument_theme": "#FFFFFF", "chocolate_theme": "#FFFFFF",
		"ocean_theme": "#FFFFFF", "workhard_theme": "#FFFFFF",
		"nocturne_theme": "#FFFFFF", "solanum_theme": "#FFFFFF",
		"brinjal_theme": "#FFFFFF", "banana_theme": "#591035",
		"sweet_treat_theme": "#4A154B", "mood_indigo_theme": "#FFFFFF",
		"eggplant_theme": "#FFFFFF", "cmyk_theme": "#FFFFFF",
		"mondrian_theme": "#FFFFFF", "terminal_theme": "#4EE077",
		"expensive_theme": "#1A1D21", "ultraviolet_theme": "#FFFFFF",
		"discotheque_theme": "#FDF6E3", "haberdashery_theme": "#FDF6E3",
	},
	{ // dark
		"default_theme": "#D1D2D3", "aubergine_bright": "#D1D2D3",
		"aubergine_classic_theme": "#D1D2D3", "hoth_theme": "#F8F8FA",
		"monument_theme": "#FFFFFF", "chocolate_theme": "#FFFFFF",
		"ocean_theme": "#FFFFFF", "workhard_theme": "#FFFFFF",
		"nocturne_theme": "#FFFFFF", "solanum_theme": "#D1D2D3",
		"brinjal_theme": "#D1D2D3", "banana_theme": "#FFC806",
		"sweet_treat_theme": "#FFC2C0", "mood_indigo_theme": "#FFFFFF",
		"eggplant_theme": "#FBFAF7", "cmyk_theme": "#0E0B01",
		"mondrian_theme": "#FFFFFF", "terminal_theme": "#4EE077",
		"expensive_theme": "#1A1D21", "ultraviolet_theme": "#FFFFFF",
		"discotheque_theme": "#FFD476", "haberdashery_theme": "#EBEDDF",
	},
}

// wcoThemeNames are udDV's isSidebarThemeName values that aren't in the
// tables: the special names (f) and cotton/eco (h).
var wcoThemeNames = map[string]bool{
	"custom_theme": true, "default": true, "workspace_default": true, "org_default": true,
	"cotton_theme": true, "eco_theme": true,
}

// isValidCustomThemeKey's patterns (udDV c, u, m).
var (
	wcoHexRE = regexp.MustCompile(`(?i)^#?[0-9a-f]{3,8}$`)
	wcoRGBRE = regexp.MustCompile(`(?i)^rgba?\([0-9,.\s]+\)$`)
	wcoHSLRE = regexp.MustCompile(`(?i)^hsla?\([0-9,.\s]+\)$`)
)

func wcoValidColor(s string) bool {
	return s != "" && (wcoHexRE.MatchString(s) || wcoRGBRE.MatchString(s) || wcoHSLRE.MatchString(s))
}

// wcoThemeText is OpEF ze's top_nav_text: dark is the theme mode, name
// the sidebar_theme pref, custom sidebar_theme_custom_values and ia
// ia_top_nav_theme (JSON). It returns the colour as Slack passes it to
// setTitleBarOverlay.
func wcoThemeText(dark bool, name, custom, ia string) string {
	mode := 0
	if dark {
		mode = 1
	}
	def := wcoTopNavText[mode]["default_theme"] // e: the mode's default theme
	t := name                                   // getChannelSidebarThemeName
	if t == "default" {
		t = "default_theme"
	} else if _, ok := wcoTopNavText[0][t]; !ok && !wcoThemeNames[t] {
		t = ""
	}
	r := def
	var cv map[string]any // F: the custom values, when isCustomTheme
	if custom != "" && json.Unmarshal([]byte(custom), &cv) == nil && wcoIsCustomTheme(cv) {
		var iv map[string]any // ia_top_nav_theme, when isIaTheme
		if ia != "" && json.Unmarshal([]byte(ia), &iv) == nil && !(wcoValidKey(iv, "top_nav_bg") && wcoValidKey(iv, "top_nav_text")) {
			iv = nil
		}
		// convertCustomThemeToTheme: e.top_nav_text||t?.top_nav_text||e.text_color
		r = wcoStr(cv, "top_nav_text")
		if r == "" {
			r = wcoStr(iv, "top_nav_text")
		}
		if r == "" {
			r = wcoStr(cv, "text_color")
		}
	}
	if t != "" && t != "custom_theme" && t != "workspace_default" && t != "org_default" {
		if t == "default_theme" {
			return def
		}
		if v, ok := wcoTopNavText[mode][t]; ok {
			return v
		}
		return r // cotton/eco: Pl[t]||r
	}
	return r
}

func wcoStr(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func wcoValidKey(m map[string]any, k string) bool {
	s, ok := m[k].(string)
	return ok && wcoValidColor(s)
}

// wcoIsCustomTheme is udDV's isCustomTheme.
func wcoIsCustomTheme(m map[string]any) bool {
	if m == nil {
		return false
	}
	for _, k := range []string{"column_bg", "menu_bg", "active_item", "active_item_text", "hover_item", "text_color", "active_presence", "badge"} {
		if !wcoValidKey(m, k) {
			return false
		}
	}
	for _, k := range []string{"top_nav_bg", "top_nav_text"} {
		if _, set := m[k]; set && !wcoValidKey(m, k) {
			return false
		}
	}
	return true
}

// wcoSymbolColor parses the colour Slack sends: Electron's
// setTitleBarOverlay throws on what content::ParseCssColorString can't
// read, and the overlay keeps its symbol colour (the BrowserWindow's
// #FFFFFF, lF).
func wcoSymbolColor(s string) color.NRGBA {
	if c, ok := css.ParseColor(s); ok && (len(s) == 0 || s[0] == '#' || s[0] == 'r' || s[0] == 'R' || s[0] == 'h' || s[0] == 'H') {
		return c
	}
	return wcoBootSymbol
}
