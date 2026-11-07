package slick

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

func scalarVars(m map[string]any) bool {
	for _, v := range m {
		switch v.(type) {
		case string, float64:
		default:
			return false
		}
	}
	return true
}

// validTheme is Slick's isThemeJson.
func validTheme(raw json.RawMessage) bool {
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) != nil || probe == nil {
		return false
	}
	var t Theme
	if json.Unmarshal(raw, &t) != nil {
		return false
	}
	if t.Mode != "" && t.Mode != "dark" && t.Mode != "light" {
		return false
	}
	for _, shades := range t.Palette {
		if !scalarVars(shades) {
			return false
		}
	}
	if !scalarVars(t.Sidebar) || !scalarVars(t.Vars) {
		return false
	}
	if len(t.CSS) > 0 {
		var s string
		var list []string
		if json.Unmarshal(t.CSS, &s) != nil && json.Unmarshal(t.CSS, &list) != nil {
			return false
		}
	}
	for _, k := range []string{"palette", "sidebar", "vars", "css"} {
		if _, ok := probe[k]; ok {
			return true
		}
	}
	return false
}

func varString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprint(x)
	}
	return fmt.Sprint(v)
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
		name = strings.TrimSuffix(strings.TrimSuffix(filename, ".json"), ".JSON")
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
