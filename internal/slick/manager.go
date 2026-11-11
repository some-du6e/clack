package slick

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"

	"github.com/some-du6e/clack/internal/slack/tlsfp"
)

// Plugin files, under Dir/plugins:
//
//	<id>.js         the renderer, exactly as Slick's `build:plugin` wrote it
//	<id>.main.js    the privileged half (main.ts), when there is one
//	<id>.meta.json  what the class declares, cached so listing and boot need no JS
//	sources.json    id → URL for "Install from URL" plugins (re-downloaded for updates)
//	prefs.json      Slick's user-plugin-prefs (autoUpdate)

// Meta is what a plugin class declares.
type Meta struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Category         *string  `json:"category"`
	Authors          string   `json:"authors"`
	DefaultEnabled   bool     `json:"defaultEnabled"`
	Settings         Schema   `json:"settings"`
	LiveSettings     []string `json:"liveSettings"`
	RelaunchSettings []string `json:"relaunchSettings"`
	RequiredAPIs     []string `json:"requiredAPIs"`
	// Main is the privileged half's declaration, when installed.
	Main *MainMeta `json:"main,omitempty"`
}

// MainMeta describes a main half.
type MainMeta struct {
	Capabilities []string `json:"capabilities"`
	RPC          []string `json:"rpc"`
}

type metaFile struct {
	Hash string `json:"hash"`
	Meta *Meta  `json:"meta"`
}

type plugin struct {
	id          string
	meta        Meta
	hash        string
	settings    map[string]any // resolved, as the running instance has it
	inJS        bool           // renderer evaluated in the current runtime
	mainInJS    bool
	running     bool
	mainRunning bool
	startError  string
}

const (
	maxPluginSize   = 16 << 20
	fetchTimeout    = 20 * time.Second
	updateDelay     = 15 * time.Second
	updateInterval  = 3 * time.Hour
	maxStoredFile   = 16 << 20
	rendererMarker  = "__slickPlugins"
	mainMarker      = "__slickMainPlugins"
	pluginIDPattern = `^[A-Za-z0-9_.-]{1,100}$`
)

var pluginID = regexp.MustCompile(pluginIDPattern)

// validPluginID is Slick's PLUGIN_ID plus what keeps a plugin's files
// apart in plugins/: no hidden names ("." and ".." included), and nothing
// that ends like another plugin's .main.js or .meta.json.
func validPluginID(id string) bool {
	if !pluginID.MatchString(id) || strings.HasPrefix(id, ".") {
		return false
	}
	l := strings.ToLower(id)
	return !strings.HasSuffix(l, ".main") && !strings.HasSuffix(l, ".meta")
}

func hashOf(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		io.WriteString(h, p)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (s *Slick) code(id string) (renderer, main string) {
	if !validPluginID(id) {
		return "", ""
	}
	if b, err := os.ReadFile(s.path("plugins", id+".js")); err == nil {
		renderer = string(b)
	}
	if b, err := os.ReadFile(s.path("plugins", id+".main.js")); err == nil {
		main = string(b)
	}
	return
}

// loadInstalled reads every plugin's cached metadata. A plugin whose cache
// is missing or stale gets its metadata from JS the first time it's needed.
func (s *Slick) loadInstalled() {
	files, _ := filepath.Glob(s.path("plugins", "*.js"))
	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasSuffix(base, ".main.js") {
			continue
		}
		id := strings.TrimSuffix(base, ".js")
		if !validPluginID(id) {
			continue
		}
		r, m := s.code(id)
		h := hashOf(r, m)
		p := &plugin{id: id, hash: h}
		var mf metaFile
		readJSONFile(s.path("plugins", id+".meta.json"), &mf)
		if mf.Meta != nil && mf.Hash == h && mf.Meta.ID == id {
			p.meta = *mf.Meta
		} else {
			p.meta = Meta{ID: id, Name: id}
			p.hash = "" // describe on first use
		}
		s.plugins[id] = p
	}
}

// ensureMeta fills a plugin's metadata from JS when the cache was stale.
func (s *Slick) ensureMeta(ctx context.Context, p *plugin) error {
	s.mu.Lock()
	known := p.hash != ""
	s.mu.Unlock()
	if known {
		return nil
	}
	r, m := s.code(p.id)
	rt, err := s.js()
	if err != nil {
		return err
	}
	meta, err := s.describe(ctx, rt, r, m)
	if err != nil {
		return err
	}
	if meta.ID != p.id {
		return fmt.Errorf("stored as %s but declares %s", p.id, meta.ID)
	}
	h := hashOf(r, m)
	s.mu.Lock()
	p.meta, p.hash = *meta, h
	s.mu.Unlock()
	_ = s.writeMeta(p.id, h, meta)
	return nil
}

func (s *Slick) writeMeta(id, hash string, m *Meta) error {
	b, err := json.MarshalIndent(metaFile{Hash: hash, Meta: m}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.path("plugins", id+".meta.json"), b, 0o600)
}

// describe reads a renderer's (and main half's) declarations in JS.
func (s *Slick) describe(ctx context.Context, rt *jsrt.Runtime, renderer, main string) (*Meta, error) {
	var m Meta
	if err := s.call(ctx, rt, "describe", &m, renderer); err != nil {
		return nil, err
	}
	if main != "" {
		var mm struct {
			ID           string   `json:"id"`
			Capabilities []string `json:"capabilities"`
			RPC          []string `json:"rpc"`
		}
		if err := s.call(ctx, rt, "describeMain", &mm, main); err != nil {
			return nil, fmt.Errorf("main half: %w", err)
		}
		if mm.ID != m.ID {
			return nil, fmt.Errorf("the main half declares %s, not %s", mm.ID, m.ID)
		}
		m.Main = &MainMeta{Capabilities: mm.Capabilities, RPC: mm.RPC}
	}
	return &m, nil
}

func (s *Slick) lock(id string) func() {
	s.mu.Lock()
	q := s.queues[id]
	if q == nil {
		q = new(sync.Mutex)
		s.queues[id] = q
	}
	s.mu.Unlock()
	q.Lock()
	return q.Unlock
}

// settingsFor resolves a plugin's stored block (Slick's resolveSettings).
func (s *Slick) settingsFor(p *plugin) map[string]any {
	return p.meta.Settings.resolve(p.meta.DefaultEnabled, s.cfg.Plugins[p.id])
}

// active is ConfigStore.isActive: globally on, the plugin on, this
// workspace allowed.
func (s *Slick) activeLocked(p *plugin) bool {
	if !s.cfg.globallyEnabled() {
		return false
	}
	if on, _ := s.settingsFor(p)["enabled"].(bool); !on {
		return false
	}
	if appWide[p.id] != "" {
		return true
	}
	ws := s.cfg.workspaces(p.id)
	if ws == nil {
		return true
	}
	cur := s.workspaceLocked()
	for _, w := range ws {
		if w == cur {
			return true
		}
	}
	return false
}

// appWide are Slick's process-wide plugins (shared/pluginWorkspaces.ts):
// workspace restrictions don't apply to them.
var appWide = map[string]string{
	"AccountSwitcher": "Account switching manages the shared Slack session.",
	"Click2Load":      "Embed blocking applies to the shared Slack session.",
	"CustomSounds":    "Native notification sounds apply to the whole app.",
	"NoTrack":         "Network tracking protection applies to the shared Slack session.",
	"QuietSpotify":    "Embedded audio controls apply to the whole app.",
	"ShutUpSlackbot":  "Native notification filtering applies to the whole app.",
	"Snappy":          "Startup optimizations apply to the whole app.",
	"StreamerMode":    "Streamer mode can silence notifications across the whole app.",
}

// StartEnabled starts every plugin that should run. New calls it (unless
// NoAutoStart); without one, it does nothing and starts no JS.
func (s *Slick) StartEnabled(ctx context.Context) {
	s.reconcileAll(ctx)
}

func (s *Slick) reconcileAll(ctx context.Context) {
	s.mu.Lock()
	ids := make([]string, 0, len(s.plugins))
	for id := range s.plugins {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		c, cancel := context.WithTimeout(ctx, 2*s.opts.LifecycleTimeout+10*time.Second)
		s.reconcile(c, id)
		cancel()
	}
}

// reconcile brings one plugin to where its config wants it: started,
// stopped, live-updated (every changed key in liveSettings) or restarted.
func (s *Slick) reconcile(ctx context.Context, id string) {
	unlock := s.lock(id)
	defer unlock()
	s.reconcileLocked(ctx, id)
}

func (s *Slick) reconcileLocked(ctx context.Context, id string) {
	s.mu.Lock()
	p := s.plugins[id]
	if p == nil || s.closed.Load() {
		s.mu.Unlock()
		return
	}
	wanted := s.activeLocked(p)
	running := p.running || p.mainRunning
	switches := hasSwitches(p)
	s.mu.Unlock()
	if !wanted {
		if running {
			s.stopLocked(ctx, p)
		}
		if switches {
			s.dropSwitches(id)
		}
		s.mu.Lock()
		hadErr := p.startError != ""
		p.startError = ""
		s.mu.Unlock()
		if hadErr {
			s.changed()
		}
		return
	}
	if err := s.ensureMeta(ctx, p); err != nil {
		s.fail(p, err.Error())
		return
	}
	s.mu.Lock()
	next := s.settingsFor(p)
	prev := p.settings
	s.mu.Unlock()
	if !running {
		s.mu.Lock()
		failed := p.startError != "" && len(changedKeys(prev, next)) == 0 && prev != nil
		s.mu.Unlock()
		if failed {
			return // stays failed until its own config changes
		}
		s.startLocked(ctx, p, next)
		return
	}
	var changed []string
	for _, k := range changedKeys(prev, next) {
		if k != "enabled" {
			changed = append(changed, k)
		}
	}
	if len(changed) == 0 {
		return
	}
	live := map[string]bool{}
	for _, k := range p.meta.LiveSettings {
		live[k] = true
	}
	allLive := true
	for _, k := range changed {
		allLive = allLive && live[k]
	}
	rt, err := s.js()
	if err != nil {
		s.fail(p, err.Error())
		return
	}
	s.mu.Lock()
	p.settings = next
	s.mu.Unlock()
	if allLive {
		_ = s.call(ctx, rt, "live", nil, id, next, changed)
		s.refreshSwitches(ctx, rt, p, next)
		return
	}
	// a restart: the renderer only; the main half hears the change
	s.post(rt, "mainSettings", id, next)
	var stopErr *string
	_ = s.call(ctx, rt, "stop", &stopErr, id)
	s.mu.Lock()
	p.running = false
	s.mu.Unlock()
	s.startLocked(ctx, p, next)
}

func (s *Slick) fail(p *plugin, msg string) {
	s.mu.Lock()
	p.startError = msg
	p.running = false
	s.mu.Unlock()
	log.Printf("slick: %s: %s", p.id, msg)
	s.changed()
}

// startLocked loads the plugin into JS if needed, readies its main half,
// then starts the renderer.
func (s *Slick) startLocked(ctx context.Context, p *plugin, settings map[string]any) {
	rt, err := s.js()
	if err != nil {
		s.fail(p, err.Error())
		return
	}
	s.mu.Lock()
	p.settings = settings
	p.startError = ""
	inJS, mainInJS := p.inJS, p.mainInJS
	s.mu.Unlock()
	r, m := s.code(p.id)
	if !inJS {
		var meta Meta
		if err := s.call(ctx, rt, "load", &meta, r); err != nil {
			s.fail(p, err.Error())
			return
		}
		s.mu.Lock()
		p.inJS = true
		s.mu.Unlock()
	}
	if m != "" && !mainInJS {
		// the capabilities Preferences shows (from the cached declaration)
		// are the only ones the main half gets, whatever it declares later
		s.mu.Lock()
		caps := []string{}
		if p.meta.Main != nil {
			caps = append(caps, p.meta.Main.Capabilities...)
		}
		s.mu.Unlock()
		if err := s.call(ctx, rt, "loadMain", nil, m, caps); err != nil {
			s.fail(p, "main half: "+err.Error())
			return
		}
		s.mu.Lock()
		p.mainInJS = true
		s.mu.Unlock()
	}
	if m != "" {
		var problem *string
		if err := s.call(ctx, rt, "startMain", &problem, p.id, settings); err != nil {
			s.fail(p, "main half: "+err.Error())
			return
		}
		if problem == nil {
			s.mu.Lock()
			p.mainRunning = true
			s.mu.Unlock()
		}
		s.refreshSwitches(ctx, rt, p, settings)
	}
	var problem *string
	if err := s.call(ctx, rt, "start", &problem, p.id, settings); err != nil {
		s.fail(p, err.Error())
		return
	}
	if problem != nil {
		s.fail(p, *problem)
		return
	}
	s.mu.Lock()
	p.running = true
	s.mu.Unlock()
	s.changed()
}

func (s *Slick) stopPlugin(ctx context.Context, id string) {
	unlock := s.lock(id)
	defer unlock()
	s.mu.Lock()
	p := s.plugins[id]
	s.mu.Unlock()
	if p != nil {
		s.stopLocked(ctx, p)
	}
}

func (s *Slick) stopLocked(ctx context.Context, p *plugin) {
	s.mu.Lock()
	rt := s.rt
	running, mainRunning := p.running, p.mainRunning
	p.running, p.mainRunning = false, false
	s.mu.Unlock()
	if rt != nil {
		if running {
			_ = s.call(ctx, rt, "stop", nil, p.id)
		}
		if mainRunning {
			_ = s.call(ctx, rt, "stopMain", nil, p.id)
		}
	}
	s.changed()
}

// ---- actions (Preferences > Slick) ----

func (s *Slick) saveConfigLocked() error {
	if s.cfgErr != nil {
		return fmt.Errorf("settings.json isn't valid JSON (%v); fix or delete it first", s.cfgErr)
	}
	return s.cfg.save(s.path("settings.json"))
}

// SetEnabled turns a plugin on or off.
func (s *Slick) SetEnabled(ctx context.Context, id string, on bool) error {
	return s.setPluginValue(ctx, id, "enabled", on)
}

// SetOption saves one setting (coerced against the schema) and applies it:
// live when the key is in liveSettings, otherwise by restarting the plugin.
func (s *Slick) SetOption(ctx context.Context, id, key string, value any) error {
	if err := s.saveOption(id, key, value); err != nil {
		return err
	}
	s.reconcile(ctx, id)
	s.changed()
	return nil
}

// saveOption stores one coerced setting without applying it.
func (s *Slick) saveOption(id, key string, value any) error {
	if key == "enabled" {
		on, _ := value.(bool)
		return s.store(id, key, on)
	}
	s.mu.Lock()
	p := s.plugins[id]
	var set Setting
	ok := false
	if p != nil {
		set, ok = p.meta.Settings.Settings[key]
	}
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("slick: %s isn't installed", id)
	}
	if !ok {
		return fmt.Errorf("slick: %s has no setting %s", id, key)
	}
	return s.store(id, key, set.coerce(normalize(value)))
}

// ResetOption removes a stored setting (back to its default).
func (s *Slick) ResetOption(ctx context.Context, id, key string) error {
	s.mu.Lock()
	if b := s.cfg.Plugins[id]; b != nil {
		delete(b, key)
	}
	err := s.saveConfigLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.reconcile(ctx, id)
	s.changed()
	return nil
}

func (s *Slick) setPluginValue(ctx context.Context, id, key string, value any) error {
	if err := s.store(id, key, value); err != nil {
		return err
	}
	s.reconcile(ctx, id)
	s.changed()
	return nil
}

func (s *Slick) store(id, key string, value any) error {
	s.mu.Lock()
	if s.plugins[id] == nil {
		s.mu.Unlock()
		return fmt.Errorf("slick: %s isn't installed", id)
	}
	b := s.cfg.Plugins[id]
	if b == nil {
		b = map[string]any{}
		s.cfg.Plugins[id] = b
	}
	b[key] = value
	err := s.saveConfigLocked()
	s.mu.Unlock()
	return err
}

// normalize turns Go values into what JSON decoding gives (float64, maps).
func normalize(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

// SetGlobalEnabled is Slick's global kill switch.
func (s *Slick) SetGlobalEnabled(ctx context.Context, on bool) error {
	s.mu.Lock()
	if on {
		s.cfg.Enabled = nil
	} else {
		f := false
		s.cfg.Enabled = &f
	}
	err := s.saveConfigLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.reconcileAll(ctx)
	s.changed()
	return nil
}

// SetWorkspaces restricts a plugin to workspaces (nil = all, empty = none).
func (s *Slick) SetWorkspaces(ctx context.Context, id string, workspaces []string) error {
	s.mu.Lock()
	if workspaces == nil {
		delete(s.cfg.PluginWorkspaces, id)
	} else {
		clean := []string{}
		for _, w := range workspaces {
			if workspaceID.MatchString(w) {
				clean = append(clean, w)
			}
		}
		b, _ := json.Marshal(clean)
		s.cfg.PluginWorkspaces[id] = b
	}
	err := s.saveConfigLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.reconcile(ctx, id)
	s.changed()
	return nil
}

// SetTheme picks a theme: "" (Slack's), "custom" (custom CSS only), a
// bundled theme id or an imported one.
func (s *Slick) SetTheme(id string) error {
	s.mu.Lock()
	s.cfg.Theme = id
	err := s.saveConfigLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.applyStyles()
	s.changed()
	return nil
}

// ImportTheme adds a theme JSON file, selects it (Slick's importTheme sets
// config.theme) and returns its id ("imported:…").
func (s *Slick) ImportTheme(data []byte, filename string) (string, error) {
	if len(data) > 256*1024 {
		return "", errors.New("Theme files must be smaller than 256 KiB.")
	}
	t, err := ParseTheme(data, filename)
	if err != nil {
		return "", err
	}
	id := "imported:" + newUUID()
	s.mu.Lock()
	s.cfg.ImportedThemes[id] = t
	prev := s.cfg.Theme
	s.cfg.Theme = id
	err = s.saveConfigLocked()
	if err != nil {
		delete(s.cfg.ImportedThemes, id)
		s.cfg.Theme = prev
	}
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	s.applyStyles()
	s.changed()
	return id, nil
}

// RemoveTheme deletes an imported theme (selecting Slack's if it was picked).
func (s *Slick) RemoveTheme(id string) error {
	s.mu.Lock()
	delete(s.cfg.ImportedThemes, id)
	if s.cfg.Theme == id {
		s.cfg.Theme = ""
	}
	err := s.saveConfigLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.applyStyles()
	s.changed()
	return nil
}

// SetUserCSS saves the custom CSS (applied over the theme).
func (s *Slick) SetUserCSS(css string) error {
	if err := writeFileAtomic(s.path("user.css"), []byte(css), 0o600); err != nil {
		return err
	}
	s.mu.Lock()
	s.userCSS = css
	s.mu.Unlock()
	s.applyStyles()
	s.changed()
	return nil
}

// UserCSS is the custom CSS.
func (s *Slick) UserCSS() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userCSS
}

// Import installs or updates a plugin from a file Slick's build wrote: a
// renderer (`build:plugin`'s .js, or its .user.js) or, for a plugin that
// is already installed, its privileged half (<id>.main.js). It returns the
// plugin's name. Like Slick, a newer file with the same id updates it in
// place.
func (s *Slick) Import(ctx context.Context, code []byte) (string, error) {
	return s.install(ctx, string(code), "", "")
}

// Peek is what a plugin file says about itself, read without running it
// (the "// slick-plugin {…}" / "// slick-main {…}" header build:plugin and
// build.mjs write). It is what the file claims, for the confirmation
// shown before installing; installing runs the file and checks it.
type Peek struct {
	ID, Name, Description string
	Main                  bool // a privileged half
	Installed             bool // a plugin with this id is installed (an update)
	HasMain               bool // the installed plugin has a privileged half
	Capabilities          []string
	StoresMessages        bool
}

// IsPluginFile reports whether code looks like a Slick plugin file (a
// renderer or a privileged half), without running it. Clack's one install
// box uses it to tell Slick's files from Taut's.
func IsPluginFile(code []byte) bool {
	c := string(code)
	return strings.Contains(c, rendererMarker) || strings.Contains(c, mainMarker)
}

// Peek reads a file's header.
func (s *Slick) Peek(code []byte) (Peek, error) {
	c := string(code)
	if len(c) > maxPluginSize {
		return Peek{}, errors.New("That file is too big to be a plugin.")
	}
	var pk Peek
	pk.Main = strings.Contains(c, mainMarker) && !strings.Contains(c, rendererMarker+" ??=")
	if !pk.Main && !strings.Contains(c, rendererMarker) {
		return Peek{}, errors.New("That file is not a Slick plugin.")
	}
	line, _, _ := strings.Cut(c, "\n")
	for _, prefix := range []string{"// slick-plugin ", "// slick-main "} {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			var h struct{ ID, Name, Description string }
			if json.Unmarshal([]byte(rest), &h) == nil {
				pk.ID, pk.Name, pk.Description = h.ID, h.Name, h.Description
			}
		}
	}
	if !validPluginID(pk.ID) {
		pk.ID = ""
	}
	if pk.Name == "" {
		pk.Name = pk.ID
	}
	s.mu.Lock()
	if p := s.plugins[pk.ID]; p != nil {
		pk.Installed = true
		if p.meta.Main != nil {
			pk.HasMain, pk.Capabilities = true, append([]string(nil), p.meta.Main.Capabilities...)
		}
		if p.meta.Name != "" && pk.Name == pk.ID {
			pk.Name = p.meta.Name
		}
	}
	s.mu.Unlock()
	pk.StoresMessages = storesMessages[pk.ID]
	return pk, nil
}

// InstallURL downloads and installs a plugin, and keeps it updated (Slick's
// "Install from URL": https, or http on localhost; GitHub file pages work).
func (s *Slick) InstallURL(ctx context.Context, raw string) (string, error) {
	code, u, err := s.Download(ctx, raw)
	if err != nil {
		return "", err
	}
	return s.InstallDownloaded(ctx, code, u)
}

// Download fetches a plugin file for "Install from URL" (https, or http on
// localhost; 16 MiB, 20 s) without installing it, so the UI can Peek and
// confirm first. It returns the URL to pass to InstallDownloaded.
func (s *Slick) Download(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := pluginURL(raw)
	if err != nil {
		return nil, "", err
	}
	code, err := s.download(ctx, u)
	if err != nil {
		return nil, "", fmt.Errorf("Could not download the plugin: %v", err)
	}
	return []byte(code), u, nil
}

// InstallDownloaded installs what Download returned and keeps it updated
// from source.
func (s *Slick) InstallDownloaded(ctx context.Context, code []byte, source string) (string, error) {
	u, err := pluginURL(source)
	if err != nil {
		return "", err
	}
	return s.install(ctx, string(code), u, "")
}

func (s *Slick) install(ctx context.Context, code, source, expectedID string) (string, error) {
	if len(code) > maxPluginSize {
		return "", errors.New("That file is too big to be a plugin.")
	}
	isMain := strings.Contains(code, mainMarker) && !strings.Contains(code, rendererMarker+" ??=")
	if !isMain && !strings.Contains(code, rendererMarker) {
		return "", errors.New("That file is not a Slick plugin.")
	}
	if isMain && expectedID != "" {
		// an update replaces a renderer; a privileged half (and its
		// capabilities) only ever comes from the user importing one
		return "", fmt.Errorf("the update for %s is a privileged main half now; install it yourself if you trust it", expectedID)
	}
	rt, err := s.js()
	if err != nil {
		return "", err
	}
	var id string
	var renderer, main string
	if isMain {
		var mm struct {
			ID string `json:"id"`
		}
		if err := s.call(ctx, rt, "describeMain", &mm, code); err != nil {
			return "", err
		}
		id = mm.ID
		if !validPluginID(id) {
			return "", fmt.Errorf("plugin has an invalid id: %q", id)
		}
		renderer, _ = s.code(id)
		if renderer == "" {
			return "", fmt.Errorf("Install %s's plugin file first, then its main half.", id)
		}
		main = code
	} else {
		var m Meta
		if err := s.call(ctx, rt, "describe", &m, code); err != nil {
			return "", err
		}
		id = m.ID
		renderer = code
		_, main = s.code(id)
	}
	if !validPluginID(id) {
		return "", fmt.Errorf("plugin has an invalid id: %q", id)
	}
	if expectedID != "" && id != expectedID {
		return "", fmt.Errorf("the file now declares %s, not %s", id, expectedID)
	}
	s.mu.Lock()
	for other := range s.plugins {
		if other != id && strings.EqualFold(other, id) {
			s.mu.Unlock()
			// one file on case-insensitive disks (macOS, Windows)
			return "", fmt.Errorf("%s is already installed as %s", id, other)
		}
	}
	s.mu.Unlock()
	meta, err := s.describe(ctx, rt, renderer, main)
	if err != nil {
		return "", err
	}
	unlock := s.lock(id)
	defer unlock()
	s.mu.Lock()
	existing := s.plugins[id]
	s.mu.Unlock()
	if existing != nil && (existing.running || existing.mainRunning) {
		s.stopLocked(ctx, existing)
	}
	name := "plugins/" + id + ".js"
	if isMain {
		name = "plugins/" + id + ".main.js"
	}
	if err := writeFileAtomic(s.path(filepath.FromSlash(name)), []byte(code), 0o600); err != nil {
		return "", errors.New("Could not save the plugin.")
	}
	h := hashOf(renderer, main)
	_ = s.writeMeta(id, h, meta)
	s.mu.Lock()
	p := existing
	if p == nil {
		p = &plugin{id: id}
		s.plugins[id] = p
	}
	p.meta, p.hash = *meta, h
	p.inJS, p.mainInJS, p.startError = false, false, ""
	if !isMain {
		if source != "" {
			s.sources[id] = source
		} else {
			delete(s.sources, id)
		}
		_ = writeJSON(s.path("plugins", "sources.json"), s.sources)
	}
	s.mu.Unlock()
	s.reconcileLocked(ctx, id)
	s.changed()
	if meta.Name != "" {
		return meta.Name, nil
	}
	return id, nil
}

// Remove uninstalls a plugin and deletes its data (Slick's "Remove plugin").
func (s *Slick) Remove(ctx context.Context, id string) error {
	unlock := s.lock(id)
	defer unlock()
	s.mu.Lock()
	p := s.plugins[id]
	s.mu.Unlock()
	if p == nil {
		return nil
	}
	s.stopLocked(ctx, p)
	s.mu.Lock()
	rt := s.rt
	delete(s.plugins, id)
	delete(s.sources, id)
	delete(s.cfg.Plugins, id)
	delete(s.cfg.PluginWorkspaces, id)
	_ = writeJSON(s.path("plugins", "sources.json"), s.sources)
	_ = s.saveConfigLocked()
	s.mu.Unlock()
	if rt != nil {
		_ = s.call(ctx, rt, "unload", nil, id)
	}
	for _, f := range []string{id + ".js", id + ".main.js", id + ".meta.json"} {
		_ = os.Remove(s.path("plugins", f))
	}
	s.dropSwitches(id)
	s.clearData(id, true)
	s.mu.Lock()
	if _, ok := s.prefs[keepOnDiskPref+id]; ok {
		delete(s.prefs, keepOnDiskPref+id)
		_ = writeJSON(s.path("plugins", "prefs.json"), s.prefs)
	}
	s.mu.Unlock()
	s.changed()
	return nil
}

// ClearData deletes a plugin's storage (api.storage, Cache, stored files).
func (s *Slick) ClearData(ctx context.Context, id string) error {
	s.clearData(id, false)
	s.reconcile(ctx, id)
	s.changed()
	return nil
}

// ---- updates ----

// UpdateResult is what a check for updates did.
type UpdateResult struct {
	Updated []string
	Failed  int
}

// UpdateUserPlugins re-downloads every URL-installed plugin and installs
// the ones whose file changed.
func (s *Slick) UpdateUserPlugins(ctx context.Context) UpdateResult {
	s.updating.Lock()
	defer s.updating.Unlock()
	s.mu.Lock()
	sources := map[string]string{}
	for id, u := range s.sources {
		sources[id] = u
	}
	s.mu.Unlock()
	var res UpdateResult
	ids := make([]string, 0, len(sources))
	for id := range sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		u := sources[id]
		if _, err := pluginURL(u); err != nil {
			res.Failed++
			log.Printf("slick: not updating %s: %v", id, err)
			continue
		}
		code, err := s.download(ctx, u)
		if err != nil {
			res.Failed++
			log.Printf("slick: could not update %s: %v", id, err)
			continue
		}
		s.mu.Lock()
		still := s.sources[id] == u
		s.mu.Unlock()
		if cur, _ := s.code(id); !still || cur == code {
			continue
		}
		name, err := s.install(ctx, code, u, id)
		if err != nil {
			res.Failed++
			log.Printf("slick: could not update %s: %v", id, err)
			continue
		}
		res.Updated = append(res.Updated, name)
	}
	return res
}

// AutoUpdates reports whether URL plugins update by themselves.
func (s *Slick) AutoUpdates() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prefs["autoUpdate"] != "off"
}

// SetAutoUpdates turns the periodic update check on or off.
func (s *Slick) SetAutoUpdates(on bool) error {
	s.mu.Lock()
	if on {
		delete(s.prefs, "autoUpdate")
	} else {
		s.prefs["autoUpdate"] = "off"
	}
	err := writeJSON(s.path("plugins", "prefs.json"), s.prefs)
	s.mu.Unlock()
	s.changed()
	return err
}

func (s *Slick) autoUpdate() {
	select {
	case <-time.After(updateDelay):
	case <-s.stopTimer:
		return
	}
	tick := time.NewTicker(updateInterval)
	defer tick.Stop()
	for {
		s.mu.Lock()
		n := len(s.sources)
		s.mu.Unlock()
		if n > 0 && s.AutoUpdates() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			s.UpdateUserPlugins(ctx)
			cancel()
		}
		select {
		case <-tick.C:
		case <-s.stopTimer:
			return
		}
	}
}

// pluginURL is Slick's pluginUrl: https (or http on localhost), GitHub
// file pages turned into raw files.
func pluginURL(input string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(input))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("That is not a valid URL.")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return "", errors.New("Plugin URLs must use https.")
	}
	if u.Hostname() == "github.com" {
		if m := regexp.MustCompile(`^/([^/]+)/([^/]+)/blob/(.+)$`).FindStringSubmatch(u.Path); m != nil {
			out := "https://raw.githubusercontent.com/" + m[1] + "/" + m[2] + "/" + m[3]
			if u.RawQuery != "" {
				out += "?" + u.RawQuery
			}
			return out, nil
		}
	}
	return u.String(), nil
}

func (s *Slick) download(ctx context.Context, u string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}
	do := s.t.FetchFunc()
	if do == nil {
		do = func(ctx context.Context, r *http.Request) (*http.Response, error) { return tlsfp.Client.Do(r) }
	}
	res, err := do(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.New("the request timed out")
		}
		// *url.Error repeats the URL, which can carry a token
		// (raw.githubusercontent.com ?token=); logs and the UI get the cause
		var ue *url.Error
		if errors.As(err, &ue) {
			return "", ue.Err
		}
		return "", err
	}
	defer res.Body.Close()
	if res.Request != nil && res.Request.URL != nil {
		// a redirect must not leave https (or localhost)
		if _, err := pluginURL(res.Request.URL.String()); err != nil {
			return "", errors.New("the server redirected to a URL that isn't https")
		}
	}
	if res.StatusCode != 200 {
		return "", fmt.Errorf("the server answered %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxPluginSize+1))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b, 0o600)
}

// InstalledSHA256 is the hex sha256 of plugin id's renderer and main half
// ("" for a file that isn't installed), for the plugin Library.
func (s *Slick) InstalledSHA256(id string) (renderer, main string) {
	if !pluginID.MatchString(id) {
		return "", ""
	}
	sum := func(name string) string {
		b, err := os.ReadFile(s.path("plugins", name))
		if err != nil {
			return ""
		}
		h := sha256.Sum256(b)
		return hex.EncodeToString(h[:])
	}
	return sum(id + ".js"), sum(id + ".main.js")
}
