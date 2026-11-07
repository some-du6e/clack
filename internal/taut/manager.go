package taut

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Meta is what a plugin class declares, read once from its code (in JS) and
// cached next to it, so listing and booting need no JS.
type Meta struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Authors      json.RawMessage `json:"authors"` // mrkdwn string, or [key | {name, slackId, url}]
	Category     *string         `json:"category"`
	HackClubOnly bool            `json:"hackClubOnly"`
	HasMigrate   bool            `json:"hasMigrate"`
	HasDataPanel bool            `json:"hasDataPanel"`
	Options      []Option        `json:"options"`
}

// Option is one defaultConfig entry.
type Option struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"` // the default
	Comment   *string         `json:"comment"`
	Label     string          `json:"label"`
	Kind      Kind            `json:"kind"`
	HasEditor bool            `json:"hasEditor"`
	HasCheck  bool            `json:"hasCheck"`
}

// Kind is the control an option gets: boolean | number | string | select |
// list | color | json | secret.
type Kind struct {
	Type    string   `json:"type"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Options []Choice `json:"options,omitempty"`
}

type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

func (m *Meta) option(key string) *Option {
	for i := range m.Options {
		if m.Options[i].Key == key {
			return &m.Options[i]
		}
	}
	return nil
}

// defaultEnabled is defaultConfig.enabled.
func (m *Meta) defaultEnabled() bool {
	if o := m.option("enabled"); o != nil {
		var b bool
		_ = json.Unmarshal(o.Value, &b)
		return b
	}
	return false
}

type plugin struct {
	id   string
	code string
	hash string
	meta *Meta // nil when the code couldn't be read

	inJS     bool // T.load has run in the current runtime
	running  bool
	err      string            // why it couldn't load or start
	problems map[string]string // option → why its saved value isn't used
	config   map[string]json.RawMessage
	applied  json.RawMessage // the resolved config it last (re)started for
	block    json.RawMessage // the saved block it was last registered with
	runID    int
}

var validID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

func codeHash(code string) string {
	h := sha256.Sum256([]byte(code))
	return hex.EncodeToString(h[:12])
}

func (t *Taut) pluginsDir() string { return filepath.Join(t.dir, "plugins") }

// loadInstalled lists plugins/*.js with their cached metadata.
func (t *Taut) loadInstalled() {
	entries, _ := os.ReadDir(t.pluginsDir())
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".js") {
			continue
		}
		id := strings.TrimSuffix(name, ".js")
		b, err := os.ReadFile(filepath.Join(t.pluginsDir(), name))
		p := &plugin{id: id}
		if err != nil {
			p.err = fmt.Sprintf("couldn't read %s: %v", name, err)
		} else {
			p.code = string(b)
			p.hash = codeHash(p.code)
			p.meta = readMeta(t.dir, id, p.hash)
		}
		t.plugins[id] = p
	}
}

func (t *Taut) lock(id string) func() {
	t.mu.Lock()
	q := t.queues[id]
	if q == nil {
		q = new(sync.Mutex)
		t.queues[id] = q
	}
	t.mu.Unlock()
	q.Lock()
	return q.Unlock
}

func (t *Taut) get(id string) *plugin {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.plugins[id]
}

// enabled is the saved `enabled`, else the plugin's default.
func (t *Taut) enabled(p *plugin) bool {
	if b := t.cfg.block(p.id); b != nil {
		if raw, ok := b["enabled"]; ok {
			var on bool
			if json.Unmarshal(raw, &on) == nil {
				return on
			}
		}
	}
	return p.meta != nil && p.meta.defaultEnabled()
}

func (t *Taut) runsHere(p *plugin) bool {
	return p.meta == nil || !p.meta.HackClubOnly || t.IsHackClub()
}

// StartEnabled starts every enabled plugin that runs in this workspace,
// after core has booted (the workspace decides Hack Club–only plugins).
// Plugins whose metadata isn't cached are read first, which needs JS.
func (t *Taut) StartEnabled(ctx context.Context) {
	select {
	case <-t.app.Ready():
	case <-ctx.Done():
		return
	}
	t.mu.Lock()
	ids := make([]string, 0, len(t.plugins))
	for id := range t.plugins {
		ids = append(ids, id)
	}
	t.mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		p := t.get(id)
		if p == nil || p.code == "" {
			continue
		}
		if p.meta == nil || (t.enabled(p) && t.runsHere(p)) {
			if err := t.register(ctx, id); err != nil {
				t.logf("taut: %s: %v", id, err)
			}
		}
	}
	t.changed()
}

// register (re)loads id from its code and starts it if it should run:
// Taut's registerPlugin. Callers hold id's lock or are StartEnabled.
func (t *Taut) register(ctx context.Context, id string) error {
	unlock := t.lock(id)
	defer unlock()
	return t.registerLocked(ctx, id)
}

func (t *Taut) registerLocked(ctx context.Context, id string) error {
	p := t.get(id)
	if p == nil {
		return fmt.Errorf("plugin %s isn't installed", id)
	}
	if p.running {
		t.stopLocked(ctx, p)
	}
	if !p.inJS || p.meta == nil {
		if err := t.loadInJS(ctx, p); err != nil {
			p.err = err.Error()
			t.changed()
			return err
		}
	}
	runs := t.runsHere(p)
	if runs && p.meta.HasMigrate {
		t.migrate(ctx, p)
	}
	secrets := t.readSecrets(p, runs)
	var resolved struct {
		Config   map[string]json.RawMessage `json:"config"`
		Problems map[string]string          `json:"problems"`
	}
	rt, err := t.ensureRuntime()
	if err != nil {
		return err
	}
	if err := rt.call(ctx, "resolve", &resolved, id, t.cfg.block(id)); err != nil {
		p.err = "couldn't read its config: " + err.Error()
		t.changed()
		return err
	}
	p.config = resolved.Config
	p.problems = resolved.Problems
	p.block, _ = json.Marshal(t.cfg.block(id))
	for k, v := range p.problems {
		t.logf("taut: %s.%s: %s, using the default", id, k, v)
	}
	p.applied, _ = json.Marshal(resolved.Config)
	full := make(map[string]json.RawMessage, len(resolved.Config)+len(secrets))
	for k, v := range resolved.Config {
		full[k] = v
	}
	for k, v := range secrets {
		full[k], _ = json.Marshal(v)
	}
	var on bool
	_ = json.Unmarshal(full["enabled"], &on)
	p.err = ""
	if on && runs {
		sctx, cancel := context.WithTimeout(ctx, t.opts.LifecycleTimeout+2*time.Second)
		var startErr *string
		err := rt.call(sctx, "start", &startErr, id, full)
		cancel()
		switch {
		case err != nil:
			p.err = "Failed to start: " + err.Error()
			// make sure nothing it registered lingers
			stctx, c2 := context.WithTimeout(context.Background(), t.opts.LifecycleTimeout+time.Second)
			_ = rt.call(stctx, "stop", nil, id)
			c2()
		case startErr != nil:
			p.err = *startErr
		default:
			p.running = true
			t.running.Add(1)
		}
	}
	t.mu.Lock()
	p.runID++
	t.mu.Unlock()
	t.changed()
	if p.err != "" {
		return errors.New(p.err)
	}
	return nil
}

// loadInJS evaluates the plugin's code and refreshes its metadata.
func (t *Taut) loadInJS(ctx context.Context, p *plugin) error {
	rt, err := t.ensureRuntime()
	if err != nil {
		return err
	}
	var m Meta
	if err := rt.call(ctx, "load", &m, p.code); err != nil {
		return fmt.Errorf("couldn't load: %s", jsMessage(err))
	}
	if m.ID != p.id {
		return fmt.Errorf("stored as %q but declares the id %q", p.id, m.ID)
	}
	p.meta = &m
	p.inJS = true
	if err := writeMeta(t.dir, p.id, p.hash, &m); err != nil {
		t.logf("taut: %s: caching its metadata: %v", p.id, err)
	}
	return nil
}

// migrate runs static migrateConfig on the saved block, saving only a change.
func (t *Taut) migrate(ctx context.Context, p *plugin) {
	block := t.cfg.block(p.id)
	if block == nil {
		return
	}
	rt, err := t.ensureRuntime()
	if err != nil {
		return
	}
	var migrated map[string]json.RawMessage
	if err := rt.call(ctx, "migrate", &migrated, p.id, block); err != nil {
		t.logf("taut: %s.migrateConfig failed: %v", p.id, err)
		return
	}
	if migrated == nil {
		return
	}
	if err := t.cfg.edit(p.id, func(b map[string]json.RawMessage) {
		for k := range b {
			delete(b, k)
		}
		for k, v := range migrated {
			b[k] = v
		}
	}); err != nil {
		t.logf("taut: couldn't save %s's migrated config: %v", p.id, err)
	}
}

func secretName(id, key string) string { return "plugin:" + id + ":" + key }

// readSecrets returns the set opt.secret values; with move, a value typed
// into config.json moves to secrets.json.
func (t *Taut) readSecrets(p *plugin, move bool) map[string]string {
	out := map[string]string{}
	for _, o := range p.meta.Options {
		if o.Kind.Type != "secret" {
			continue
		}
		name := secretName(p.id, o.Key)
		if move {
			if b := t.cfg.block(p.id); b != nil {
				var inFile string
				if raw, ok := b[o.Key]; ok && json.Unmarshal(raw, &inFile) == nil && inFile != "" {
					t.data.writeSecret(name, inFile)
					key := o.Key
					if err := t.cfg.edit(p.id, func(b map[string]json.RawMessage) { delete(b, key) }); err != nil {
						t.logf("taut: moved %s.%s to secret storage, but couldn't remove it from config.json", p.id, o.Key)
					}
				}
			}
		}
		if v, ok := t.data.readSecret(name); ok && v != "" {
			out[o.Key] = v
		}
	}
	return out
}

func (t *Taut) stopPlugin(ctx context.Context, id string) error {
	unlock := t.lock(id)
	defer unlock()
	p := t.get(id)
	if p == nil {
		return nil
	}
	t.stopLocked(ctx, p)
	t.changed()
	return nil
}

// stopLocked aborts and stops a running plugin (bounded by the 5 s stop
// timeout in JS); everything it registered through TautAPI is undone.
func (t *Taut) stopLocked(ctx context.Context, p *plugin) {
	if !p.running {
		return
	}
	p.running = false
	t.running.Add(-1)
	t.mu.Lock()
	rt := t.rt
	t.mu.Unlock()
	if rt == nil {
		return
	}
	sctx, cancel := context.WithTimeout(ctx, t.opts.LifecycleTimeout+2*time.Second)
	defer cancel()
	var stopErr *string
	if err := rt.call(sctx, "stop", &stopErr, p.id); err != nil {
		t.logf("taut: stopping %s: %v", p.id, err)
	} else if stopErr != nil {
		t.logf("taut: error stopping %s: %s", p.id, *stopErr)
	}
}

// ---- edits from settings ----

// SetEnabled turns a plugin on or off (saved in config.json).
func (t *Taut) SetEnabled(ctx context.Context, id string, on bool) error {
	return t.SetOption(ctx, id, "enabled", on)
}

// SetOption saves option key (only stored while it differs from the
// default) and restarts the plugin with the new config, like Taut. For a
// secret option use SetSecret.
func (t *Taut) SetOption(ctx context.Context, id, key string, value any) error {
	unlock := t.lock(id)
	defer unlock()
	p := t.get(id)
	if p == nil || p.meta == nil {
		return fmt.Errorf("plugin %s isn't loaded", id)
	}
	o := p.meta.option(key)
	if o == nil {
		return fmt.Errorf("%s has no option %q", id, key)
	}
	if o.Kind.Type == "secret" {
		s, _ := value.(string)
		return t.setSecretLocked(ctx, p, key, s)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if problem := t.checkOption(ctx, p, o, raw); problem != "" {
		return errors.New(problem)
	}
	wasRunning := p.running
	if err := t.cfg.edit(id, func(b map[string]json.RawMessage) {
		if jsonEqual(raw, o.Value) {
			delete(b, key)
		} else {
			b[key] = raw
		}
	}); err != nil {
		return err
	}
	return t.applyConfigLocked(ctx, p, wasRunning)
}

// ResetOption drops a saved option so the default applies again.
func (t *Taut) ResetOption(ctx context.Context, id, key string) error {
	unlock := t.lock(id)
	defer unlock()
	p := t.get(id)
	if p == nil || p.meta == nil {
		return fmt.Errorf("plugin %s isn't loaded", id)
	}
	if o := p.meta.option(key); o != nil && o.Kind.Type == "secret" {
		return t.setSecretLocked(ctx, p, key, "")
	}
	wasRunning := p.running
	if err := t.cfg.edit(id, func(b map[string]json.RawMessage) { delete(b, key) }); err != nil {
		return err
	}
	return t.applyConfigLocked(ctx, p, wasRunning)
}

// SetSecret stores an opt.secret value (empty clears it) and restarts.
func (t *Taut) SetSecret(ctx context.Context, id, key, value string) error {
	unlock := t.lock(id)
	defer unlock()
	p := t.get(id)
	if p == nil || p.meta == nil {
		return fmt.Errorf("plugin %s isn't loaded", id)
	}
	return t.setSecretLocked(ctx, p, key, value)
}

func (t *Taut) setSecretLocked(ctx context.Context, p *plugin, key, value string) error {
	t.data.writeSecret(secretName(p.id, key), value)
	if p.running || (t.enabled(p) && t.runsHere(p)) {
		return t.registerLocked(ctx, p.id)
	}
	t.changed()
	return nil
}

// Secret reads an opt.secret value (for the settings field).
func (t *Taut) Secret(id, key string) string {
	v, _ := t.data.readSecret(secretName(id, key))
	return v
}

// applyConfigLocked restarts the plugin for its new config (a fresh
// instance), dropping its cache when it was just disabled.
func (t *Taut) applyConfigLocked(ctx context.Context, p *plugin, wasRunning bool) error {
	on := t.enabled(p) && t.runsHere(p)
	if !wasRunning && !on {
		p.err = ""
		t.changed()
		return nil
	}
	if wasRunning && !on {
		t.stopLocked(ctx, p)
		t.data.clear(cacheNS(p.id))
		t.changed()
		return nil
	}
	return t.registerLocked(ctx, p.id)
}

// checkOption mirrors Taut's checks: the kind's type check, then the
// option's own `check` (JS) when it has one.
func (t *Taut) checkOption(ctx context.Context, p *plugin, o *Option, raw json.RawMessage) string {
	if o.HasCheck || o.Kind.Type == "color" {
		rt, err := t.ensureRuntime()
		if err != nil {
			return err.Error()
		}
		if !p.inJS {
			if err := t.loadInJS(ctx, p); err != nil {
				return err.Error()
			}
		}
		var problem *string
		if err := rt.call(ctx, "check", &problem, p.id, o.Key, raw); err != nil {
			return err.Error()
		}
		if problem != nil {
			return *problem
		}
		return ""
	}
	return checkKind(o, raw)
}

// checkKind is the type check every option gets (settings writes only
// values that pass, and resolve applies the same rules in JS).
func checkKind(o *Option, raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "not JSON"
	}
	switch o.Kind.Type {
	case "boolean":
		if _, ok := v.(bool); !ok {
			return "expected true or false, found " + string(raw)
		}
	case "number":
		if _, ok := v.(float64); !ok {
			return "expected a number, found " + string(raw)
		}
	case "string", "secret":
		if _, ok := v.(string); !ok {
			return "expected text, found " + string(raw)
		}
	case "select":
		s, _ := v.(string)
		for _, c := range o.Kind.Options {
			if c.Value == s {
				return ""
			}
		}
		return "expected one of the choices, found " + string(raw)
	case "list":
		l, ok := v.([]any)
		if !ok {
			return "expected a list of text, found " + string(raw)
		}
		for _, x := range l {
			if _, ok := x.(string); !ok {
				return "expected a list of text, found " + string(raw)
			}
		}
	case "json":
		var d any
		_ = json.Unmarshal(o.Value, &d)
		switch d.(type) {
		case []any:
			if _, ok := v.([]any); !ok {
				return "expected a list, found " + string(raw)
			}
		case map[string]any:
			if _, ok := v.(map[string]any); !ok {
				return "expected an object, found " + string(raw)
			}
		}
	}
	return ""
}

// ---- installing ----

// Import installs a user plugin from its built .js (Taut's
// build:user-plugin output). replacing is the id of the plugin whose
// "Update code" this is, or "" for a new plugin. Like Taut, a plugin that
// fails to load, or is enabled and fails to start, isn't installed and the
// previous code (and its storage) comes back.
func (t *Taut) Import(ctx context.Context, code []byte, replacing string) (string, error) {
	src := string(code)
	rt, err := t.ensureRuntime()
	if err != nil {
		return "", err
	}
	var old *plugin
	if replacing != "" {
		unlock := t.lock(replacing)
		if p := t.get(replacing); p != nil {
			cp := *p
			old = &cp
			t.stopLocked(ctx, p)
		}
		unlock()
	}
	restoreOld := func() {
		if old == nil {
			return
		}
		unlock := t.lock(old.id)
		defer unlock()
		if p := t.get(old.id); p != nil {
			p.inJS = false
		}
		_ = t.registerLocked(ctx, old.id)
	}
	var m Meta
	if err := rt.call(ctx, "load", &m, src); err != nil {
		restoreOld()
		return "", errors.New(jsMessage(err))
	}
	id := m.ID
	if replacing != "" && replacing != id {
		t.unloadJS(ctx, id)
		restoreOld()
		return "", fmt.Errorf("edited plugin must keep id %q (received %q)", replacing, id)
	}
	unlock := t.lock(id)
	defer unlock()
	if existing := t.get(id); existing != nil && replacing == "" {
		existing.inJS = false // its class was just replaced in JS; reload on next start
		return "", fmt.Errorf("a plugin with id %q already exists; use its \"Update code\" button to replace it", id)
	}
	snap := t.data.snapshot(storageNS(id))
	path := filepath.Join(t.pluginsDir(), id+".js")
	if err := writeFileAtomic(path, code, 0o600); err != nil {
		t.unloadJS(ctx, id)
		if old != nil {
			old.inJS = false
			_ = t.registerLocked(ctx, id)
		}
		return "", fmt.Errorf("failed to save plugin to storage: %w", err)
	}
	p := &plugin{id: id, code: src, hash: codeHash(src), meta: &m, inJS: true}
	_ = writeMeta(t.dir, id, p.hash, &m)
	t.mu.Lock()
	t.plugins[id] = p
	t.mu.Unlock()
	if err := t.registerLocked(ctx, id); err != nil {
		t.rollback(ctx, id, old, snap)
		return "", err
	}
	if !t.opts.NoAutoStart {
		t.startWatch()
	}
	return id, nil
}

// rollback puts back the plugin that was there before (or nothing).
func (t *Taut) rollback(ctx context.Context, id string, old *plugin, snap map[string]string) {
	if p := t.get(id); p != nil {
		t.stopLocked(ctx, p)
	}
	t.data.clear(cacheNS(id))
	t.data.restore(storageNS(id), snap)
	path := filepath.Join(t.pluginsDir(), id+".js")
	if old == nil {
		_ = os.Remove(path)
		_ = os.Remove(metaPath(t.dir, id))
		t.mu.Lock()
		delete(t.plugins, id)
		t.mu.Unlock()
		t.unloadJS(ctx, id)
		t.changed()
		return
	}
	_ = writeFileAtomic(path, []byte(old.code), 0o600)
	if old.meta != nil {
		_ = writeMeta(t.dir, id, old.hash, old.meta)
	}
	old.inJS = false
	old.running = false
	t.mu.Lock()
	t.plugins[id] = old
	t.mu.Unlock()
	_ = t.registerLocked(ctx, id)
}

// InstallURL fetches a built plugin .js and imports it.
func (t *Taut) InstallURL(ctx context.Context, url string) (string, error) {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return "", errors.New("the plugin URL must start with https://")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := t.opts.Fetch(ctx, req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching the plugin: HTTP %d", res.StatusCode)
	}
	code, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return "", err
	}
	return t.Import(ctx, code, "")
}

// Remove deletes a user plugin with its config, storage, cache and secrets.
func (t *Taut) Remove(ctx context.Context, id string) error {
	unlock := t.lock(id)
	defer unlock()
	p := t.get(id)
	if p == nil {
		return errors.New("plugin isn't installed")
	}
	t.stopLocked(ctx, p)
	if err := os.Remove(filepath.Join(t.pluginsDir(), id+".js")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to delete plugin: %w", err)
	}
	_ = os.Remove(metaPath(t.dir, id))
	t.data.clear(cacheNS(id))
	t.data.clear(storageNS(id))
	t.data.clear(secretsNS(id))
	for _, n := range t.data.secretNames("plugin:" + id + ":") {
		t.data.writeSecret(n, "")
	}
	if err := t.cfg.remove(id); err != nil {
		t.logf("taut: deleted plugin %s, but couldn't remove its settings from config.json: %v", id, err)
	}
	t.unloadJS(ctx, id)
	t.mu.Lock()
	delete(t.plugins, id)
	t.mu.Unlock()
	t.changed()
	return nil
}

func (t *Taut) unloadJS(ctx context.Context, id string) {
	t.mu.Lock()
	rt := t.rt
	t.mu.Unlock()
	if rt != nil {
		_ = rt.call(ctx, "unload", nil, id)
	}
}

// ClearData resets a plugin's "storage" (which also drops its cache) or
// just its "cache", restarting it like Taut does.
func (t *Taut) ClearData(ctx context.Context, id, kind string) error {
	unlock := t.lock(id)
	defer unlock()
	p := t.get(id)
	if p == nil {
		return errors.New("plugin isn't installed")
	}
	wasRunning := p.running
	t.stopLocked(ctx, p)
	t.data.clear(cacheNS(id))
	if kind == "storage" {
		t.data.clear(storageNS(id))
		t.data.clear(secretsNS(id))
	}
	if wasRunning {
		return t.registerLocked(ctx, id)
	}
	t.changed()
	return nil
}

// HasData reports whether a plugin has stored data / cache entries.
func (t *Taut) HasData(id string) (storage, cache bool) {
	return len(t.data.list(storageNS(id))) > 0 || len(t.data.list(secretsNS(id))) > 0, len(t.data.list(cacheNS(id))) > 0
}

func storageNS(id string) string { return "plugin:" + id + ":storage" }
func cacheNS(id string) string   { return "plugin:" + id + ":cache" }
func secretsNS(id string) string { return "plugin:" + id + ":secrets" }

// jsMessage strips jsrt's wrapping from a thrown error.
func jsMessage(err error) string {
	s := err.Error()
	for _, p := range []string{"jsrt: ", "Error: "} {
		s = strings.TrimPrefix(s, p)
	}
	return s
}
