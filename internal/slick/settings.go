package slick

import (
	"encoding/json"
	"sort"
	"strings"
)

// The model for Preferences > Slick, shaped like Slick's own settings UI
// (src/app/settings.tsx): an overview tab (global switch), Plugins (filter
// pills, search, plugins grouped by category, a detail view with the
// settings form, workspaces and source), Themes (cards, imported themes,
// custom CSS) and Install plugins (file, URL, updates). ui-shell draws it
// with native widgets; plugins' own settings.addTab tabs arrive through
// Taut's PreferencesTabs like Taut plugins' tabs.

// Settings is everything Preferences > Slick shows.
type Settings struct {
	// Enabled is Slick's global switch (off: plugins and the theme don't run).
	Enabled bool
	// ConfigError is set when settings.json can't be parsed (nothing is saved until fixed).
	ConfigError string
	// Plugins are every installed plugin, sorted by name.
	Plugins []PluginInfo
	// Sections group Plugins like Slick: category order, then "User plugins", "Other".
	Sections []Section
	// Filters are Slick's pills (All, On, Off, User plugins, Problems); User
	// plugins and Problems only when non-empty.
	Filters []Filter
	// Themes are the cards: Slack's (""), bundled, imported, "Custom CSS only" ("custom").
	Themes []ThemeCard
	// Theme is the selected card's ID.
	Theme       string
	UserCSS     string
	AutoUpdates bool
	// Workspace is the window's workspace id (for the workspaces picker).
	Workspace string
}

// Filter is one pill.
type Filter struct {
	ID    string // all | on | off | user | problems
	Label string
	Count int
}

// Section is one category heading and its plugins.
type Section struct {
	Category string // "" for User plugins / Other
	Label    string
	Icon     string // Slack SvgIcon name (hackclub: Clack's Hack Club glyph)
	On       int    // "N on"
	Plugins  []PluginInfo
}

// PluginInfo is one plugin row and its detail view.
type PluginInfo struct {
	ID, Name, Description string
	Category              string // Slick category key, "" when none
	CategoryLabel         string
	Authors               string // Slack mentions, `<@U…>` style (render as mrkdwn)
	User                  bool   // installed from a file or URL (every plugin in Clack)
	Source                string // the URL it updates from, "" for a file
	DefaultEnabled        bool
	Enabled               bool // its switch
	Active                bool // should run here (global, switch, workspace)
	Running               bool
	StartError            string
	WorkspaceAllowed      bool
	Workspaces            []string // nil: all workspaces
	AppWideReason         string   // set: workspace restriction doesn't apply
	HasMain               bool     // a privileged half is installed
	Capabilities          []string // the main half's declared capabilities
	Options               []OptionInfo
	RelaunchSettings      []string
}

// OptionInfo is one schema setting with its current value.
type OptionInfo struct {
	Key, Type, Label, Description string
	Value, Default                any
	Stored                        bool // set in settings.json (Reset applies)
	Min, Max                      *float64
	MaxLength                     int
	Accept                        string
	Options, Statuses             []SelectOption
	Live                          bool // applies without restarting the plugin
	Relaunch                      bool // needs an app restart
	RestartRequired               bool
}

// ThemeCard is one theme choice.
type ThemeCard struct {
	ID, Name, Description string
	Mode                  string // "", dark, light ("Only in dark mode")
	Preview               *ThemePreview
	Imported              bool
	Selected              bool
}

var categoryIcons = map[string]string{
	"messages": "message", "composer": "compose", "people": "user", "channels": "channel", "sounds": "headphones",
	"privacy": "lock", "appearance": "paintbrush", "app": "settings", "hackclub": "hackclub",
}

// Settings builds the model (no JS).
func (s *Slick) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Settings{
		Enabled:     s.cfg.globallyEnabled(),
		Theme:       s.cfg.theme(),
		UserCSS:     s.userCSS,
		AutoUpdates: s.prefs["autoUpdate"] != "off",
	}
	if s.cfgErr != nil {
		out.ConfigError = s.cfgErr.Error()
	}
	ws := s.workspaceLocked()
	out.Workspace = ws
	for _, p := range s.plugins {
		out.Plugins = append(out.Plugins, s.infoLocked(p, ws))
	}
	sort.Slice(out.Plugins, func(i, j int) bool {
		return strings.ToLower(out.Plugins[i].Name) < strings.ToLower(out.Plugins[j].Name)
	})
	on, off, user, problems := 0, 0, 0, 0
	groups := map[string]*Section{}
	for _, info := range out.Plugins {
		if info.Enabled {
			on++
		} else {
			off++
		}
		if info.User {
			user++
		}
		if info.StartError != "" && info.Enabled {
			problems++
		}
		g := groups[info.CategoryLabel]
		if g == nil {
			icon := "code"
			if i, ok := categoryIcons[info.Category]; ok {
				icon = i
			}
			g = &Section{Category: info.Category, Label: info.CategoryLabel, Icon: icon}
			groups[info.CategoryLabel] = g
		}
		g.Plugins = append(g.Plugins, info)
		if info.Enabled {
			g.On++
		}
	}
	order := map[string]int{}
	for i, c := range Categories {
		order[c.Label] = i
	}
	order["User plugins"] = len(Categories)
	order["Other"] = len(Categories) + 1
	for _, g := range groups {
		out.Sections = append(out.Sections, *g)
	}
	sort.Slice(out.Sections, func(i, j int) bool { return order[out.Sections[i].Label] < order[out.Sections[j].Label] })
	out.Filters = []Filter{{"all", "All", len(out.Plugins)}, {"on", "On", on}, {"off", "Off", off}}
	if user > 0 {
		out.Filters = append(out.Filters, Filter{"user", "User plugins", user})
	}
	if problems > 0 {
		out.Filters = append(out.Filters, Filter{"problems", "Problems", problems})
	}
	out.Themes = s.themeCardsLocked(out.Theme)
	return out
}

func (s *Slick) infoLocked(p *plugin, ws string) PluginInfo {
	m := p.meta
	resolved := s.settingsFor(p)
	enabled, _ := resolved["enabled"].(bool)
	info := PluginInfo{
		ID: p.id, Name: m.Name, Description: m.Description, Authors: m.Authors,
		User: true, Source: s.sources[p.id], DefaultEnabled: m.DefaultEnabled,
		Enabled: enabled, Active: s.activeLocked(p), Running: p.running,
		StartError: p.startError, AppWideReason: appWide[p.id],
		Workspaces: s.cfg.workspaces(p.id), RelaunchSettings: m.RelaunchSettings,
	}
	if info.Name == "" {
		info.Name = p.id
	}
	if m.Category != nil {
		if label, ok := categoryLabel(*m.Category); ok {
			info.Category, info.CategoryLabel = *m.Category, label
		}
	}
	if info.CategoryLabel == "" {
		info.CategoryLabel = "User plugins"
	}
	info.WorkspaceAllowed = info.Workspaces == nil || info.AppWideReason != ""
	for _, w := range info.Workspaces {
		if w == ws {
			info.WorkspaceAllowed = true
		}
	}
	if m.Main != nil {
		info.HasMain, info.Capabilities = true, m.Main.Capabilities
	}
	live, relaunch := map[string]bool{}, map[string]bool{}
	for _, k := range m.LiveSettings {
		live[k] = true
	}
	for _, k := range m.RelaunchSettings {
		relaunch[k] = true
	}
	stored := s.cfg.Plugins[p.id]
	for _, k := range m.Settings.Keys {
		set := m.Settings.Settings[k]
		_, isStored := stored[k]
		info.Options = append(info.Options, OptionInfo{
			Key: k, Type: set.Type, Label: set.Label, Description: set.Description,
			Value: resolved[k], Default: set.defaultValue(), Stored: isStored,
			Min: set.Min, Max: set.Max, MaxLength: set.MaxLength, Accept: set.Accept,
			Options: set.Options, Statuses: set.Statuses,
			Live: live[k], Relaunch: relaunch[k], RestartRequired: set.RestartRequired,
		})
	}
	return info
}

func (s *Slick) themeCardsLocked(selected string) []ThemeCard {
	cards := []ThemeCard{{ID: "", Name: "Slack", Description: "Slack's own colors", Preview: &slackPreview}}
	ids := make([]string, 0, len(s.themes))
	for id := range s.themes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	add := func(id string, t Theme, imported bool) {
		pv := previewOf(t)
		name := t.Name
		if name == "" {
			name = id
		}
		desc := t.Description
		if t.Mode != "" {
			parts := []string{}
			if desc != "" {
				parts = append(parts, desc)
			}
			desc = strings.Join(append(parts, "Only in "+t.Mode+" mode"), " · ")
		}
		cards = append(cards, ThemeCard{ID: id, Name: name, Description: desc, Mode: t.Mode, Preview: &pv, Imported: imported})
	}
	for _, id := range ids {
		add(id, s.themes[id], false)
	}
	imp := make([]string, 0, len(s.cfg.ImportedThemes))
	for id := range s.cfg.ImportedThemes {
		imp = append(imp, id)
	}
	sort.Strings(imp)
	for _, id := range imp {
		var t Theme
		if json.Unmarshal(s.cfg.ImportedThemes[id], &t) == nil {
			add(id, t, true)
		}
	}
	cards = append(cards, ThemeCard{ID: "custom", Name: "Custom CSS only", Description: "No theme, just your CSS"})
	for i := range cards {
		cards[i].Selected = cards[i].ID == selected
	}
	return cards
}

// Code is a plugin's renderer source (for "view source").
func (s *Slick) Code(id string) string {
	r, _ := s.code(id)
	return r
}

// Dir is Slick's data directory.
func (s *Slick) Dir() string { return s.dir }
