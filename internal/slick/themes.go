package slick

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Themes are Slick's theme JSON (themes/*.json in Slick, plus imported ones
// kept in settings.json). They are pure CSS, so Clack converts them in Go
// and hands the stylesheet to taut-ui's CSS engine: a theme or custom CSS
// never starts the JS engine.
//
// Bundled themes are read from <Dir>/themes/*.json (copy Slick's themes/
// folder there; Clack doesn't ship Slick's files).

// Theme is a theme file.
type Theme struct {
	Name        string                    `json:"name,omitempty"`
	Description string                    `json:"description,omitempty"`
	Mode        string                    `json:"mode,omitempty"` // "", "dark", "light"
	Palette     map[string]map[string]any `json:"palette,omitempty"`
	Sidebar     map[string]any            `json:"sidebar,omitempty"`
	Vars        map[string]any            `json:"vars,omitempty"`
	CSS         json.RawMessage           `json:"css,omitempty"` // string or []string
}

const themeSelector = ":root,html,body,.sk-client-theme--dark,.sk-client-theme--light"

// variables is Slick's variables(): an object of strings and numbers.
func variables(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for _, x := range m {
		switch x.(type) {
		case string, float64:
		default:
			return false
		}
	}
	return true
}

// validTheme is Slick's isThemeJson. A key that is present counts, even as
// null: Slick checks `!== undefined`, so `"palette": null` is rejected.
func validTheme(raw json.RawMessage) bool {
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return false
	}
	for _, k := range []string{"name", "description"} {
		if x, ok := v[k]; ok {
			if _, isString := x.(string); !isString {
				return false
			}
		}
	}
	if m, ok := v["mode"]; ok && m != "dark" && m != "light" {
		return false
	}
	if p, ok := v["palette"]; ok {
		ramps, isObject := p.(map[string]any)
		if !isObject {
			return false
		}
		for _, shades := range ramps {
			if !variables(shades) {
				return false
			}
		}
	}
	for _, k := range []string{"sidebar", "vars"} {
		if x, ok := v[k]; ok && !variables(x) {
			return false
		}
	}
	if c, ok := v["css"]; ok {
		switch c := c.(type) {
		case string:
		case []any:
			for _, e := range c {
				if _, isString := e.(string); !isString {
					return false
				}
			}
		default:
			return false
		}
	}
	for _, k := range []string{"palette", "sidebar", "vars", "css"} {
		if _, ok := v[k]; ok {
			return true
		}
	}
	return false
}

// varString is JavaScript's String(value) for a theme value.
func varString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return jsNumber(x)
	}
	return fmt.Sprint(v)
}

// jsNumber formats a number as JavaScript's Number.prototype.toString does:
// the shortest round-trip digits, positional below 1e21 and from 1e-6,
// exponent form outside that (1e+21, 1.5e-7).
func jsNumber(x float64) string {
	if x == 0 {
		return "0"
	}
	if a := math.Abs(x); a >= 1e21 || a < 1e-6 {
		mant, exp, _ := strings.Cut(strconv.FormatFloat(x, 'e', -1, 64), "e")
		return mant + "e" + exp[:1] + strings.TrimLeft(exp[1:], "0")
	}
	return strconv.FormatFloat(x, 'f', -1, 64)
}

// themeCSS is Slick's themeToCss: palette, sidebar and vars as !important
// custom properties on the theme selector, then the raw css.
func themeCSS(t Theme) string {
	type decl struct{ k, v string }
	var decls []decl
	ramps := make([]string, 0, len(t.Palette))
	for r := range t.Palette {
		ramps = append(ramps, r)
	}
	sort.Strings(ramps)
	for _, r := range ramps {
		shades := make([]string, 0, len(t.Palette[r]))
		for s := range t.Palette[r] {
			shades = append(shades, s)
		}
		sort.Strings(shades)
		for _, s := range shades {
			decls = append(decls, decl{"--dt_color-plt-" + r + "-" + s, varString(t.Palette[r][s])})
		}
	}
	keys := func(m map[string]any) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	for _, k := range keys(t.Sidebar) {
		decls = append(decls, decl{"--p-team_sidebar__" + k, varString(t.Sidebar[k])})
	}
	for _, k := range keys(t.Vars) {
		decls = append(decls, decl{k, varString(t.Vars[k])})
	}
	// later duplicates win, like Slick's object assignment
	seen := map[string]int{}
	var uniq []decl
	for _, d := range decls {
		if i, ok := seen[d.k]; ok {
			uniq[i].v = d.v
			continue
		}
		seen[d.k] = len(uniq)
		uniq = append(uniq, d)
	}
	var b strings.Builder
	if len(uniq) > 0 {
		b.WriteString(themeSelector)
		b.WriteString("{")
		for i, d := range uniq {
			if i > 0 {
				b.WriteString(";")
			}
			b.WriteString(d.k + ":" + d.v + " !important")
		}
		b.WriteString("}\n")
	}
	if len(t.CSS) > 0 {
		var s string
		var list []string
		if json.Unmarshal(t.CSS, &s) == nil {
			b.WriteString(s)
		} else if json.Unmarshal(t.CSS, &list) == nil {
			b.WriteString(strings.Join(list, "\n"))
		}
	}
	return b.String()
}

// ParseTheme is Slick's parseThemeJson for an imported file.
func ParseTheme(data []byte, filename string) (json.RawMessage, error) {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return nil, errors.New("The theme file is not valid JSON.")
	}
	if !validTheme(data) {
		return nil, errors.New("Expected a Slick theme with palette, sidebar, vars, or css fields.")
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	name, _ := m["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = filename
		if n := len(name); n >= 5 && strings.EqualFold(name[n-5:], ".json") { // /\.json$/i
			name = name[:n-5]
		}
	}
	if name == "" {
		name = "Imported theme"
	}
	m["name"] = name
	return json.Marshal(m)
}

// bundledThemes reads <dir>/themes/*.json, by file name.
func bundledThemes(dir string) map[string]Theme {
	out := map[string]Theme{}
	files, _ := filepath.Glob(filepath.Join(dir, "themes", "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil || !validTheme(b) {
			continue
		}
		var t Theme
		if json.Unmarshal(b, &t) == nil {
			out[strings.TrimSuffix(filepath.Base(f), ".json")] = t
		}
	}
	return out
}

// ThemePreview is the swatch Slick's theme card shows.
type ThemePreview struct{ Background, Sidebar, Text, Accent string }

func pick(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok {
			return s
		}
	}
	return ""
}

func darkColor(hex string) bool {
	if len(hex) < 7 || hex[0] != '#' {
		return true
	}
	var r, g, b int
	if _, err := fmt.Sscanf(hex[1:7], "%02x%02x%02x", &r, &g, &b); err != nil {
		return true
	}
	return float64(r)*0.299+float64(g)*0.587+float64(b)*0.114 < 140
}

func previewOf(t Theme) ThemePreview {
	bg := pick(t.Vars, "--dt_color-base-pry")
	if bg == "" {
		bg = "#1a1d21"
	}
	sidebar := pick(t.Sidebar, "nav-bg")
	if sidebar == "" {
		sidebar = pick(t.Vars, "--dt_color-theme-base-inv-pry")
	}
	if sidebar == "" {
		sidebar = bg
	}
	text := pick(t.Vars, "--dt_color-content-pry")
	if text == "" {
		text = pick(t.Sidebar, "text-color")
	}
	if text == "" {
		if darkColor(bg) {
			text = "#d1d2d3"
		} else {
			text = "#1d1c1d"
		}
	}
	accent := pick(t.Vars, "--dt_color-content-hgl-1")
	if accent == "" {
		accent = pick(t.Sidebar, "badge")
	}
	if accent == "" {
		accent = "#1d9bd1"
	}
	return ThemePreview{bg, sidebar, text, accent}
}

// slackPreview is the "Slack" card (no theme).
var slackPreview = ThemePreview{"#ffffff", "#4a154b", "#1d1c1d", "#1264a3"}
