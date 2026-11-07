// Package slick runs Slick plugins (https://github.com/3kh0/slick) natively
// in Clack: unmodified plugin files built with Slick's `build:plugin` (the
// .js Slick users share and install), on the same foundation as Taut
// plugins: one jsrt runtime, the Slack-shaped Redux mirror, Taut's thunk
// routing (taut.Dispatch), composer/message hooks and slackc's component
// patch points. Privileged halves (a plugin's main.ts) run in the same
// runtime against a MainCtx backed by Go.
//
// Slick is GPLv3; no Slick code is in this package. Its plugin contract
// (src/shared/Plugin.ts, src/plugins/README.md, the SlickAPI) is
// implemented independently. See docs/PLUGINS-slick.md.
//
//   - js/*.js: `$slick`, each plugin's SlickAPI, the renderer lifecycle and
//     the main-half host, over the TautAPI internals.
//   - slick.go, host.go: the taut.Extension and the host functions.
//   - manager.go: plugin files, install from file/URL (with updates),
//     enable/disable, settings changes (live or restart), workspaces.
//   - config.go, schema.go: Slick's settings.json and settings coercion.
//   - themes.go: Slick themes and custom CSS (pure Go, no JS).
//   - settings.go: the model for Preferences > Slick.
//
// Zero cost: New reads settings.json and the plugins' cached metadata.
// Without an enabled plugin no JS engine starts; a theme or custom CSS is
// one stylesheet handed to the UI.
package slick

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
)

//go:embed js/*.js
var jsFiles embed.FS

// prelude is every js/ file in name order inside one function scope.
var prelude = func() string {
	names, _ := fs.Glob(jsFiles, "js/*.js")
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("(function () {\n'use strict';\n")
	for _, n := range names {
		src, _ := jsFiles.ReadFile(n)
		fmt.Fprintf(&b, "// ---- %s ----\n", n)
		b.Write(src)
		b.WriteString("\n")
	}
	b.WriteString("})();\n")
	return b.String()
}()

var workspaceID = regexp.MustCompile(`^[TE][A-Z0-9]+$`)

// Options configures Slick. Taut is required: Slick plugins share its
// runtime and UI wiring.
type Options struct {
	Taut *taut.Taut
	// Dir holds settings.json, user.css, plugins/ and themes/.
	// Default: $CLACK_HOME/slick or ~/.config/clack/slick.
	Dir string
	// OpenURL opens a URL in the OS browser (MainCtx.shell.openExternal).
	OpenURL func(url string)
	// OpenFile is the native file picker for `file` settings and
	// MainCtx.dialog.openFile: accept is a comma-separated extension list;
	// "" means cancelled.
	OpenFile func(title, accept string) (string, error)
	// LifecycleTimeout bounds start/stop (Slick: 5 s; JS enforces it too).
	LifecycleTimeout time.Duration
	// NoAutoStart keeps New from starting enabled plugins (tests).
	NoAutoStart bool
	// NoUpdates turns off the URL-installed plugins' update checks.
	NoUpdates bool
}

// Slick is the plugin system. All methods are safe for concurrent use.
type Slick struct {
	opts Options
	t    *taut.Taut
	dir  string

	mu      sync.Mutex
	cfg     *storedConfig
	cfgErr  error
	plugins map[string]*plugin
	sources map[string]string // id → install URL
	prefs   map[string]string // user-plugin-prefs (autoUpdate)
	userCSS string
	themes  map[string]Theme
	queues  map[string]*sync.Mutex
	rt      *jsrt.Runtime // the runtime the prelude is in (nil until a plugin needs JS)
	route   [2]string     // team, channel the UI shows
	dark    atomic.Bool
	styled  string // the theme CSS last handed to the UI

	changeMu sync.Mutex
	changeCh map[chan struct{}]struct{}

	updating  sync.Mutex
	stopTimer chan struct{}
	closed    atomic.Bool
}

// New reads settings.json, user.css, themes and the installed plugins'
// metadata, registers with Taut, applies the theme, and (unless
// NoAutoStart) starts the enabled plugins in the background.
func New(o Options) (*Slick, error) {
	if o.Taut == nil {
		return nil, errors.New("slick: Options.Taut is required")
	}
	if o.Dir == "" {
		o.Dir = filepath.Join(clackHome(), "slick")
	}
	if o.LifecycleTimeout <= 0 {
		o.LifecycleTimeout = 5 * time.Second
	}
	if err := os.MkdirAll(filepath.Join(o.Dir, "plugins"), 0o700); err != nil {
		return nil, fmt.Errorf("slick: %w", err)
	}
	s := &Slick{
		opts:      o,
		t:         o.Taut,
		dir:       o.Dir,
		plugins:   map[string]*plugin{},
		sources:   map[string]string{},
		prefs:     map[string]string{},
		queues:    map[string]*sync.Mutex{},
		changeCh:  map[chan struct{}]struct{}{},
		stopTimer: make(chan struct{}),
	}
	cfg, err := loadConfig(s.path("settings.json"))
	if err != nil {
		log.Printf("slick: settings.json: %v (changes won't be saved until it's fixed)", err)
		s.cfgErr = err
	}
	s.cfg = cfg
	if b, err := os.ReadFile(s.path("user.css")); err == nil {
		s.userCSS = string(b)
	}
	readJSONFile(s.path("plugins", "sources.json"), &s.sources)
	readJSONFile(s.path("plugins", "prefs.json"), &s.prefs)
	s.themes = bundledThemes(o.Dir)
	s.loadInstalled()
	if err := s.t.Extend(taut.Extension{
		Name:             "slick",
		Install:          s.installHost,
		Prelude:          prelude,
		Ready:            s.ready,
		LocalStorage:     s.localStorage,
		LocalStorageKeys: s.localStorageKeys,
	}); err != nil {
		return nil, err
	}
	s.applyStyles()
	if !o.NoAutoStart {
		go s.StartEnabled(context.Background())
		if !o.NoUpdates {
			go s.autoUpdate()
		}
	}
	return s, nil
}

// Close stops every plugin (each bounded by the stop timeout). Taut keeps
// the runtime; close Slick before Taut.
func (s *Slick) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(s.stopTimer)
	ctx, cancel := context.WithTimeout(context.Background(), 2*s.opts.LifecycleTimeout+time.Second)
	defer cancel()
	s.mu.Lock()
	ids := make([]string, 0, len(s.plugins))
	for id, p := range s.plugins {
		if p.running || p.mainRunning {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.stopPlugin(ctx, id)
	}
	if ui := s.t.UIHooks(); ui.RemoveStyle != nil {
		ui.RemoveStyle("slick:theme")
		ui.RemoveStyle("slick:user")
	}
	return nil
}

func (s *Slick) path(parts ...string) string {
	return filepath.Join(append([]string{s.dir}, parts...)...)
}

// Started reports whether Slick's JS is loaded (a plugin needed it).
func (s *Slick) Started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rt != nil
}

// js returns the runtime with the Slick prelude in it, starting Taut's
// runtime when needed.
func (s *Slick) js() (*jsrt.Runtime, error) {
	if s.closed.Load() {
		return nil, jsrt.ErrClosed
	}
	rt, err := s.t.JS()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.rt != rt {
		// a fresh runtime (the first, or after a crash): nothing is loaded in it
		s.rt = rt
		for _, p := range s.plugins {
			p.inJS, p.mainInJS = false, false
		}
		team, ch := s.route[0], s.route[1]
		s.mu.Unlock()
		if team != "" {
			s.post(rt, "setRoute", team, ch)
		}
		return rt, nil
	}
	s.mu.Unlock()
	return rt, nil
}

// ready is the extension's Ready: after a crash, bring back what ran.
func (s *Slick) ready(rt *jsrt.Runtime, restarted bool) {
	if !restarted {
		return
	}
	s.mu.Lock()
	var restart []string
	for id, p := range s.plugins {
		p.inJS, p.mainInJS = false, false
		if p.running || p.mainRunning {
			restart = append(restart, id)
		}
		p.running, p.mainRunning = false, false
	}
	s.rt = nil
	s.mu.Unlock()
	if len(restart) > 0 {
		log.Printf("slick: the JS runtime restarted; restarting %d plugins", len(restart))
	}
	for _, id := range restart {
		ctx, cancel := context.WithTimeout(context.Background(), 2*s.opts.LifecycleTimeout+5*time.Second)
		s.reconcile(ctx, id)
		cancel()
	}
}

// call runs __slick.<fn>(args…) and decodes its result into out.
func (s *Slick) call(ctx context.Context, rt *jsrt.Runtime, fn string, out any, args ...any) error {
	raw, err := rt.Call(ctx, "__slick."+fn, args...)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (s *Slick) post(rt *jsrt.Runtime, fn string, args ...any) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := rt.Call(ctx, "__slick."+fn, args...); err != nil {
			log.Printf("slick: %s: %v", fn, err)
		}
	}()
}

// SetRoute tells plugins which workspace and conversation the window shows
// (location.pathname /client/<team>/<channel>: channels.getCurrentChannelId,
// the active team). Cheap when no plugin runs.
func (s *Slick) SetRoute(team, channel string) {
	s.mu.Lock()
	changed := s.route != [2]string{team, channel}
	s.route = [2]string{team, channel}
	rt := s.rt
	s.mu.Unlock()
	if changed && rt != nil {
		s.post(rt, "setRoute", team, channel)
	}
	if changed {
		go s.reconcileAll(context.Background()) // workspace restrictions
	}
}

// SetDark tells Slick whether the UI is in dark mode (themes with a mode
// apply only in that mode).
func (s *Slick) SetDark(dark bool) {
	if s.dark.Swap(dark) != dark {
		s.applyStyles()
	}
}

// Subscribe notifies about changes Preferences > Slick shows (plugins,
// enabled state, errors, settings, themes). Coalescing: one pending signal.
func (s *Slick) Subscribe() (<-chan struct{}, func()) {
	c := make(chan struct{}, 1)
	s.changeMu.Lock()
	s.changeCh[c] = struct{}{}
	s.changeMu.Unlock()
	return c, func() {
		s.changeMu.Lock()
		delete(s.changeCh, c)
		s.changeMu.Unlock()
	}
}

func (s *Slick) changed() {
	s.changeMu.Lock()
	for c := range s.changeCh {
		select {
		case c <- struct{}{}:
		default:
		}
	}
	s.changeMu.Unlock()
	if inv := s.t.UIHooks().Invalidate; inv != nil {
		inv()
	}
}

// workspace is the window's workspace id (route first, then the account's).
func (s *Slick) workspace() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workspaceLocked()
}

func (s *Slick) workspaceLocked() string {
	team := s.route[0]
	if team != "" {
		return team
	}
	app := s.t.App()
	if app == nil {
		return ""
	}
	if e := app.C.Creds.EnterpriseID; e != "" {
		return e
	}
	return app.C.Creds.Team
}

// ---- localStorage: Slack's localConfig_v2, served (never stored) ----

const localConfigKey = "localConfig_v2"

func (s *Slick) localStorageKeys() []string { return []string{localConfigKey} }

func (s *Slick) localStorage(key string) (string, bool) {
	if key != localConfigKey {
		return "", false
	}
	app := s.t.App()
	if app == nil {
		return "", false
	}
	c := app.C.Creds
	app.S.RLock()
	name, domain := app.S.TeamName, app.S.TeamDomain
	app.S.RUnlock()
	teamID := c.Team
	url := c.TeamURL
	if url == "" && domain != "" {
		url = "https://" + domain + ".slack.com/"
	}
	teams := map[string]any{}
	entry := func(id, u string) map[string]any {
		m := map[string]any{"id": id, "name": name, "domain": domain, "user_id": c.UserID, "token": c.Token, "url": u}
		if c.EnterpriseID != "" {
			m["enterprise_id"] = c.EnterpriseID
		}
		return m
	}
	order := []string{}
	if teamID != "" {
		teams[teamID] = entry(teamID, url)
		order = append(order, teamID)
	}
	if c.EnterpriseID != "" {
		eu := c.EnterpriseURL
		if eu == "" {
			eu = url
		}
		teams[c.EnterpriseID] = entry(c.EnterpriseID, eu)
	}
	last := s.workspace()
	if _, ok := teams[last]; !ok {
		last = teamID
	}
	b, _ := json.Marshal(map[string]any{"teams": teams, "lastActiveTeamId": last, "orderedTeamIds": order})
	return string(b), true
}

// ---- styles: theme and custom CSS (no JS) ----

// applyStyles hands the selected theme's CSS and user.css to the UI. Both
// stay while Slick is paused: Slick's installTheme and user CSS never read
// the global switch, which only stops plugins.
func (s *Slick) applyStyles() {
	ui := s.t.UIHooks()
	if ui.SetStyle == nil {
		return
	}
	s.mu.Lock()
	css := ""
	if th, ok := s.themeLocked(s.cfg.theme()); ok && (th.Mode == "" || (th.Mode == "dark") == s.dark.Load()) {
		css = themeCSS(th)
	}
	user := s.userCSS
	prev := s.styled
	s.styled = css
	s.mu.Unlock()
	if css != prev {
		if css == "" {
			if ui.RemoveStyle != nil {
				ui.RemoveStyle("slick:theme")
			}
		} else {
			ui.SetStyle("slick:theme", css)
		}
	}
	if user == "" {
		if ui.RemoveStyle != nil {
			ui.RemoveStyle("slick:user")
		}
	} else {
		ui.SetStyle("slick:user", user)
	}
}

func (s *Slick) themeLocked(id string) (Theme, bool) {
	if id == "" || id == "custom" {
		return Theme{}, false
	}
	if raw, ok := s.cfg.ImportedThemes[id]; ok {
		var t Theme
		if json.Unmarshal(raw, &t) == nil {
			return t, true
		}
	}
	t, ok := s.themes[id]
	return t, ok
}

func readJSONFile(path string, v any) {
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, v)
	}
}

func clackHome() string {
	if h := os.Getenv("CLACK_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("YOLOSLACK_HOME"); h != "" {
		return h
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "clack")
}
