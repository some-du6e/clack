package taut

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The model behind Preferences > Taut. ui-shell draws it with native
// widgets; option kinds map to controls (boolean: toggle, number: number
// field with Min/Max, string: text field, select: dropdown of Choices,
// list: one row per string, color: color picker, json: code box, secret:
// password field). An option with HasEditor is a React editor rendered
// through Bridge.RenderEditor, and a running plugin's data panel through
// Bridge.RenderDataPanel. Plugins' own tabs (api.preferences.addTab) are
// PreferencesTabs, rendered with Bridge.RenderPreferencesTab.

// Categories in Taut's display order; plugins without a known category go
// under "Other".
var Categories = []struct{ Key, Label string }{
	{"messages", "Messages"}, {"messageBox", "Message Box"}, {"people", "People"},
	{"privacy", "Privacy"}, {"app", "App"}, {"fun", "Fun"}, {"other", "Other"},
}

// Settings is the whole tab.
type Settings struct {
	Categories []SettingsCategory
	UserCSS    string // the custom CSS box (Taut's user.css → setStyle key "user")
	// ConfigError is why config.json can't be read (edits are refused until fixed).
	ConfigError string
	// RuntimeError is why the JS runtime couldn't start.
	RuntimeError string
	Tabs         []PreferencesTab
}

type SettingsCategory struct {
	Key, Label string
	Plugins    []PluginInfo
}

// PluginInfo is one plugin's row and detail page.
type PluginInfo struct {
	ID, Name, Description string // Description is mrkdwn
	Authors               []Author
	AuthorsMrkdwn         string // older plugins name authors in one mrkdwn string
	Category              string
	HackClubOnly          bool
	Enabled               bool // what config says
	Running               bool
	Error                 string // load or start error, shown on the plugin
	Problems              map[string]string
	Options               []OptionState // without "enabled"
	HasDataPanel          bool
	HasStorage, HasCache  bool // offer "Reset data" / "Clear cache"
	RunID                 int  // changes on every (re)start: remount the data panel
	IsUser                bool // user-imported (every plugin in Clack is): offer Update code / Remove
}

// OptionState is an option with its current value.
type OptionState struct {
	Option
	Current json.RawMessage // what the plugin gets (default when not set)
	Set     bool            // saved in config.json (offer "reset")
	Problem string          // why the saved value isn't used
	// SecretSet says a secret option has a value (the value itself is
	// read with Taut.Secret only when the field is revealed).
	SecretSet bool
}

// Author is a plugin author.
type Author struct {
	Name    string `json:"name"`
	SlackID string `json:"slackId,omitempty"`
	URL     string `json:"url,omitempty"`
}

// knownAuthors are the people Taut's plugins credit by key.
var knownAuthors = map[string]Author{
	"jeremy":  {"Jeremy", "U06UYA5GMB5", "https://jeremywoolley.com"},
	"rowan":   {"Rowan", "U080A3QP42C", "https://3kh0.net"},
	"ani":     {"Ani", "U01D9DWGEB0", "https://github.com/anirudhb"},
	"sahil":   {"Sahil", "U08PUHSMW4V", "https://sahil.ink"},
	"miggy":   {"Miggy", "U07VC9705D4", "https://shymike.dev"},
	"scooter": {"Scooter", "U046VA0KR8R", "https://github.com/scooterthedev"},
	"cyril":   {"Cyril", "U07FXPUDYDC", "https://github.com/CyrilSLi"},
	"izie":    {"Izie", "U09KKMHLS15", "https://izie.top"},
	"adryd":   {"adryd", "", "https://adryd.com"},
}

func resolveAuthors(raw json.RawMessage) ([]Author, string) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return nil, s
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil, ""
	}
	var out []Author
	for _, it := range items {
		var key string
		if json.Unmarshal(it, &key) == nil {
			if a, ok := knownAuthors[key]; ok {
				out = append(out, a)
			} else {
				out = append(out, Author{Name: key})
			}
			continue
		}
		var a Author
		if json.Unmarshal(it, &a) == nil && a.Name != "" {
			out = append(out, a)
		}
	}
	return out, ""
}

// Settings returns the Preferences > Taut model. Hack Club–only plugins
// are left out outside the Hack Club Slack, like Taut.
func (t *Taut) Settings() Settings {
	hc := t.IsHackClub()
	t.mu.Lock()
	ps := make([]*plugin, 0, len(t.plugins))
	for _, p := range t.plugins {
		ps = append(ps, p)
	}
	rtErr := t.rtErr
	css := t.userCSS
	t.mu.Unlock()
	sort.Slice(ps, func(i, j int) bool { return strings.ToLower(ps[i].id) < strings.ToLower(ps[j].id) })
	byCat := map[string][]PluginInfo{}
	for _, p := range ps {
		if p.meta != nil && p.meta.HackClubOnly && !hc {
			continue
		}
		info := t.info(p)
		byCat[info.Category] = append(byCat[info.Category], info)
	}
	s := Settings{UserCSS: css, Tabs: t.PreferencesTabs()}
	if t.cfg.err != nil {
		s.ConfigError = t.cfg.err.Error()
	}
	if rtErr != nil {
		s.RuntimeError = rtErr.Error()
	}
	for _, c := range Categories {
		if len(byCat[c.Key]) == 0 {
			continue
		}
		sort.SliceStable(byCat[c.Key], func(i, j int) bool {
			return strings.ToLower(byCat[c.Key][i].Name) < strings.ToLower(byCat[c.Key][j].Name)
		})
		s.Categories = append(s.Categories, SettingsCategory{Key: c.Key, Label: c.Label, Plugins: byCat[c.Key]})
	}
	return s
}

func (t *Taut) info(p *plugin) PluginInfo {
	info := PluginInfo{ID: p.id, Name: p.id, Category: "other", Error: p.err, Running: p.running, RunID: p.runID, IsUser: true, Problems: p.problems}
	info.HasStorage, info.HasCache = t.HasData(p.id)
	m := p.meta
	if m == nil {
		return info
	}
	info.Name = m.Name
	info.Description = m.Description
	info.Authors, info.AuthorsMrkdwn = resolveAuthors(m.Authors)
	if m.Category != nil {
		for _, c := range Categories {
			if c.Key == *m.Category {
				info.Category = c.Key
			}
		}
	}
	info.HackClubOnly = m.HackClubOnly
	info.HasDataPanel = m.HasDataPanel
	info.Enabled = t.enabled(p)
	block := t.cfg.block(p.id)
	for _, o := range m.Options {
		if o.Key == "enabled" {
			continue
		}
		st := OptionState{Option: o, Current: o.Value}
		if raw, ok := block[o.Key]; ok && o.Kind.Type != "secret" {
			st.Set = true
			if prob := p.problems[o.Key]; prob != "" {
				st.Problem = prob
			} else {
				st.Current = raw
			}
		}
		if o.Kind.Type == "secret" {
			st.Current = json.RawMessage(`""`)
			st.SecretSet = t.Secret(p.id, o.Key) != ""
			st.Set = st.SecretSet
		}
		if p.config != nil && o.Kind.Type != "secret" {
			if v, ok := p.config[o.Key]; ok {
				st.Current = v
			}
		}
		info.Options = append(info.Options, st)
	}
	return info
}

// Dir is the plugin directory (config.json, user.css, plugins/).
func (t *Taut) Dir() string { return t.dir }

// Code returns a plugin's installed code (for "Update code" / export).
func (t *Taut) Code(id string) string {
	if p := t.get(id); p != nil {
		return p.code
	}
	return ""
}

// SetUserCSS saves the custom CSS box and applies it (setStyle key "user").
func (t *Taut) SetUserCSS(css string) error {
	if err := writeFileAtomic(filepath.Join(t.dir, "user.css"), []byte(css), 0o600); err != nil {
		return err
	}
	t.mu.Lock()
	t.userCSS = css
	t.mu.Unlock()
	if strings.TrimSpace(css) == "" {
		if f := t.opts.UI.RemoveStyle; f != nil {
			f("user")
		}
	} else if f := t.opts.UI.SetStyle; f != nil {
		f("user", css)
	}
	t.changed()
	return nil
}

// ReloadConfig re-reads config.json (edited by hand) and applies it: like
// Taut's file watcher, plugins whose resolved config changed restart.
func (t *Taut) ReloadConfig(ctx context.Context) error {
	cfg, err := loadConfig(filepath.Join(t.dir, "config.json"))
	t.mu.Lock()
	t.cfg = cfg
	ids := make([]string, 0, len(t.plugins))
	for id := range t.plugins {
		ids = append(ids, id)
	}
	t.mu.Unlock()
	if err != nil {
		t.changed()
		return err
	}
	for _, id := range ids {
		p := t.get(id)
		if p == nil || p.meta == nil {
			continue
		}
		unlock := t.lock(id)
		should := t.enabled(p) && t.runsHere(p)
		cur, _ := json.Marshal(t.cfg.block(id))
		if should == p.running && jsonEqual(cur, p.block) {
			unlock()
			continue
		}
		if should || p.running {
			_ = t.applyConfigLocked(ctx, p, p.running)
		}
		unlock()
	}
	if b, err := os.ReadFile(filepath.Join(t.dir, "user.css")); err == nil && string(b) != t.userCSS {
		_ = t.SetUserCSS(string(b))
	}
	t.changed()
	return nil
}

// watchFiles applies hand edits to config.json and user.css (Taut watches
// them too). It polls their modification times every 2 s, and only while
// at least one plugin is installed.
func (t *Taut) watchFiles(stop <-chan struct{}) {
	stat := func(name string) time.Time {
		fi, err := os.Stat(filepath.Join(t.dir, name))
		if err != nil {
			return time.Time{}
		}
		return fi.ModTime()
	}
	cfgAt, cssAt := stat("config.json"), stat("user.css")
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		t.mu.Lock()
		n := len(t.plugins)
		t.mu.Unlock()
		if n == 0 {
			continue
		}
		c, s := stat("config.json"), stat("user.css")
		if c.Equal(cfgAt) && s.Equal(cssAt) {
			continue
		}
		cfgAt, cssAt = c, s
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := t.ReloadConfig(ctx); err != nil {
			t.logf("taut: config.json: %v", err)
		}
		cancel()
	}
}
