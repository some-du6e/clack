package slick

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// settings.json is Slick's own settings file format (src/app/configStore.ts):
//
//	{"enabled": true,                 global kill switch (false: load, run nothing)
//	 "theme": "nord",                 "" (Slack's), "custom" (custom CSS only), a bundled id, or "imported:<uuid>"
//	 "importedThemes": {"imported:…": {…theme json…}},
//	 "plugins": {"<id>": {"enabled": true, "<setting>": …}},
//	 "pluginWorkspaces": {"<id>": null | ["T…", "E…"]}}
//
// so a settings.json from Slick's desktop app can be dropped in as is.
// Unknown top-level keys are kept when Clack writes the file back.

type storedConfig struct {
	raw              map[string]json.RawMessage
	Enabled          *bool
	Theme            string
	ImportedThemes   map[string]json.RawMessage
	Plugins          map[string]map[string]any
	PluginWorkspaces map[string]json.RawMessage
}

func loadConfig(path string) (*storedConfig, error) {
	c := &storedConfig{raw: map[string]json.RawMessage{}, Plugins: map[string]map[string]any{}, ImportedThemes: map[string]json.RawMessage{}, PluginWorkspaces: map[string]json.RawMessage{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(b, &c.raw); err != nil {
		return &storedConfig{raw: map[string]json.RawMessage{}, Plugins: map[string]map[string]any{}, ImportedThemes: map[string]json.RawMessage{}, PluginWorkspaces: map[string]json.RawMessage{}}, err
	}
	if v, ok := c.raw["enabled"]; ok {
		var e bool
		if json.Unmarshal(v, &e) == nil {
			c.Enabled = &e
		}
	}
	if v, ok := c.raw["theme"]; ok {
		_ = json.Unmarshal(v, &c.Theme)
	}
	if v, ok := c.raw["importedThemes"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(v, &m) == nil {
			for k, t := range m {
				if strings.HasPrefix(k, "imported:") && validTheme(t) {
					c.ImportedThemes[k] = t
				}
			}
		}
	}
	if v, ok := c.raw["plugins"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(v, &m) == nil {
			for id, block := range m {
				var o map[string]any
				if json.Unmarshal(block, &o) == nil && o != nil {
					c.Plugins[id] = o
				}
			}
		}
	}
	if v, ok := c.raw["pluginWorkspaces"]; ok {
		_ = json.Unmarshal(v, &c.PluginWorkspaces)
		if c.PluginWorkspaces == nil {
			c.PluginWorkspaces = map[string]json.RawMessage{}
		}
	}
	return c, nil
}

func (c *storedConfig) globallyEnabled() bool { return c.Enabled == nil || *c.Enabled }

// theme applies Slick's renamed themes.
func (c *storedConfig) theme() string {
	if c.Theme == "ultraviolet" {
		return "catppuccin-mocha"
	}
	return c.Theme
}

func (c *storedConfig) save(path string) error {
	out := map[string]any{}
	for k, v := range c.raw {
		out[k] = v
	}
	if c.Enabled != nil {
		out["enabled"] = *c.Enabled
	} else {
		delete(out, "enabled")
	}
	if c.Theme != "" {
		out["theme"] = c.Theme
	} else {
		delete(out, "theme")
	}
	if len(c.ImportedThemes) > 0 {
		out["importedThemes"] = c.ImportedThemes
	} else {
		delete(out, "importedThemes")
	}
	out["plugins"] = c.Plugins
	if len(c.PluginWorkspaces) > 0 {
		out["pluginWorkspaces"] = c.PluginWorkspaces
	} else {
		delete(out, "pluginWorkspaces")
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'), 0o600)
}

// workspaces is pluginWorkspaces[id]: nil = every workspace, [] = none.
func (c *storedConfig) workspaces(id string) []string {
	raw, ok := c.PluginWorkspaces[id]
	if !ok || string(raw) == "null" {
		return nil
	}
	var list []any
	if json.Unmarshal(raw, &list) != nil {
		return []string{}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, x := range list {
		if s, ok := x.(string); ok && workspaceID.MatchString(s) && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
