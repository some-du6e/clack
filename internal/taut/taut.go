// Package taut runs Taut plugins (https://github.com/jeremy46231/taut)
// natively in Clack: unmodified plugin bundles built with Taut's
// `build:user-plugin`, on the jsrt JS runtime, against Clack's own
// implementation of the TautAPI over internal/core.
//
// Taut is GPLv3; no Taut code is in this package. The contract (what
// plugins import and call) is implemented independently from its
// documentation and behaviour. See docs/PLUGINS.md.
//
// Layout of the pieces:
//   - js/*.js: the TautAPI itself (the `$taut` exports, redux mirror and
//     patches, thunks, patchComponent registry, elements, storage, …).
//   - runtime.go, host.go: jsrt wiring and the host functions JS calls.
//   - manager.go, config.go, data.go: plugin files, config.json semantics,
//     secrets, plugin storage, lifecycle (enable/disable, restart on config
//     change, 5 s start/stop timeout, errors), Hack Club gating.
//   - mirror.go: the Slack-shaped Redux slices, built from core snapshots
//     and kept current from core events.
//   - thunks.go: Dispatch, the native path UI owners route Slack's named
//     thunks through (zero overhead when unpatched).
//   - ui.go: the slackc bridge (patched components, overlays, CSS) and the
//     composer / message hooks UI owners call.
//   - settings.go: the Preferences > Taut model.
//
// Without an enabled plugin nothing here starts a JS engine or does
// per-frame work: Taut.New only reads config.json and the plugin metadata
// cache.
package taut

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// HackClubIDs are the Hack Club enterprise grid and its workspace;
// hackClubOnly plugins run only there.
var HackClubIDs = map[string]bool{"E09V59WQY1E": true, "T0266FRGM": true}

// Options configures Taut. App is required; everything else is optional.
type Options struct {
	App *core.App
	// Dir holds config.json, user.css, plugins/, storage/ and secrets.json.
	// Default: $CLACK_HOME/taut or ~/.config/clack/taut.
	Dir string
	// CredsPath is Clack's creds.json (accounts, cookies). Default
	// $CLACK_HOME/creds.json or ~/.config/clack/creds.json.
	CredsPath string
	// UI connects plugins to the native UI (set by the ui root).
	UI UIHooks
	// Fetch backs fetch() and api.fetch; nil uses an HTTP client.
	Fetch func(ctx context.Context, req *http.Request) (*http.Response, error)
	// CacheDir for jsrt's compiled engine. Default <Dir>/cache.
	CacheDir string
	// OpenURL, ClipboardWrite and ClipboardRead go to jsrt.
	OpenURL        func(url string)
	ClipboardWrite func(text string) error
	ClipboardRead  func() (string, error)
	// Log receives plugin console output (level, message). nil logs warnings
	// and errors with the log package. Message contents are never logged by
	// Clack itself; what a plugin prints is the plugin's business.
	Log func(level, msg string)
	// TaskTimeout bounds one JS task (jsrt's Options.Timeout).
	TaskTimeout time.Duration
	// LifecycleTimeout is the start/stop limit (Taut: 5 s).
	LifecycleTimeout time.Duration
	// NoAutoStart keeps New from starting enabled plugins (tests).
	NoAutoStart bool
}

// Taut is the plugin system. All methods are safe for concurrent use.
type Taut struct {
	opts Options
	app  *core.App
	dir  string

	mu      sync.Mutex
	rt      *runtime
	rtErr   error
	plugins map[string]*plugin
	cfg     *configFile
	userCSS string
	queues  map[string]*sync.Mutex // per-plugin lifecycle lock

	data   *dataStore
	mirror *mirror
	thunks *thunkTable
	ui     *uiState
	fns    *goFns

	changeMu sync.Mutex
	changeCh map[chan struct{}]struct{}

	running   atomic.Int32 // plugins with a live instance
	stopWatch chan struct{}
	closed    atomic.Bool
}

// New loads config.json, user.css and the installed plugins' metadata. If
// any plugin is enabled (and runs in this workspace) it starts the runtime
// and the plugins in the background.
func New(o Options) (*Taut, error) {
	if o.App == nil {
		return nil, errors.New("taut: Options.App is required")
	}
	if o.Dir == "" {
		o.Dir = filepath.Join(clackHome(), "taut")
	}
	if o.CredsPath == "" {
		o.CredsPath = filepath.Join(clackHome(), "creds.json")
	}
	if o.CacheDir == "" {
		o.CacheDir = filepath.Join(o.Dir, "cache")
	}
	if o.LifecycleTimeout <= 0 {
		o.LifecycleTimeout = 5 * time.Second
	}
	if o.Fetch == nil {
		client := &http.Client{Timeout: 60 * time.Second}
		o.Fetch = func(ctx context.Context, req *http.Request) (*http.Response, error) {
			return client.Do(req.WithContext(ctx))
		}
	}
	if err := os.MkdirAll(filepath.Join(o.Dir, "plugins"), 0o700); err != nil {
		return nil, fmt.Errorf("taut: %w", err)
	}
	t := &Taut{
		opts:     o,
		app:      o.App,
		dir:      o.Dir,
		plugins:  map[string]*plugin{},
		queues:   map[string]*sync.Mutex{},
		changeCh: map[chan struct{}]struct{}{},
		fns:      newGoFns(),
	}
	t.data = newDataStore(o.Dir)
	t.mirror = newMirror(t)
	t.thunks = newThunkTable(t)
	t.ui = newUIState(t)
	cfg, err := loadConfig(filepath.Join(o.Dir, "config.json"))
	if err != nil {
		t.logf("taut: config.json: %v (using defaults until it's fixed)", err)
	}
	t.cfg = cfg
	if b, err := os.ReadFile(filepath.Join(o.Dir, "user.css")); err == nil {
		t.userCSS = string(b)
	}
	t.loadInstalled()
	if t.userCSS != "" && o.UI.SetStyle != nil {
		o.UI.SetStyle("user", t.userCSS)
	}
	t.stopWatch = make(chan struct{})
	if !o.NoAutoStart {
		go t.StartEnabled(context.Background())
		go t.watchFiles(t.stopWatch)
	}
	return t, nil
}

// Close stops every plugin (each bounded by the stop timeout) and the
// runtime, and flushes plugin storage.
func (t *Taut) Close() error {
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), t.opts.LifecycleTimeout+time.Second)
	defer cancel()
	t.mu.Lock()
	ids := make([]string, 0, len(t.plugins))
	for id, p := range t.plugins {
		if p.running {
			ids = append(ids, id)
		}
	}
	rt := t.rt
	t.mu.Unlock()
	for _, id := range ids {
		_ = t.stopPlugin(ctx, id)
	}
	close(t.stopWatch)
	t.mirror.stop()
	if rt != nil {
		rt.close()
	}
	return t.data.flush()
}

// Runtime returns the JS runtime, starting it if needed (the ui root uses
// it for slackc's DOM events, rects and viewport).
func (t *Taut) Runtime() (*runtime, error) { return t.ensureRuntime() }

// Started reports whether the JS runtime is up, without starting it.
func (t *Taut) Started() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rt != nil
}

func (t *Taut) ensureRuntime() (*runtime, error) {
	t.mu.Lock()
	if t.rt != nil {
		r := t.rt
		t.mu.Unlock()
		return r, nil
	}
	t.mu.Unlock()
	r, err := t.startRuntime()
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil {
		t.rtErr = err
		return nil, err
	}
	if t.rt != nil { // lost a race: keep the first
		r.close()
		return t.rt, nil
	}
	t.rt = r
	t.mirror.start()
	if t.opts.UI.Attached != nil {
		t.opts.UI.Attached(t.Bridge())
	}
	return r, nil
}

func (t *Taut) logf(format string, args ...any) { log.Printf(format, args...) }

// IsHackClub reports whether Clack is signed in to the Hack Club Slack.
func (t *Taut) IsHackClub() bool {
	s := t.app.S
	s.RLock()
	team := s.TeamID
	self := s.Self.TeamID
	s.RUnlock()
	ent := t.app.C.Creds.EnterpriseID
	return HackClubIDs[team] || HackClubIDs[ent] || HackClubIDs[self] || HackClubIDs[t.app.C.Creds.Team]
}

// Subscribe notifies about changes the settings view shows (plugin list,
// enabled state, errors, config). Coalescing: one pending signal at most.
func (t *Taut) Subscribe() (<-chan struct{}, func()) {
	c := make(chan struct{}, 1)
	t.changeMu.Lock()
	t.changeCh[c] = struct{}{}
	t.changeMu.Unlock()
	return c, func() {
		t.changeMu.Lock()
		delete(t.changeCh, c)
		t.changeMu.Unlock()
	}
}

func (t *Taut) changed() {
	t.changeMu.Lock()
	for c := range t.changeCh {
		select {
		case c <- struct{}{}:
		default:
		}
	}
	t.changeMu.Unlock()
	if inv := t.opts.UI.Invalidate; inv != nil {
		inv()
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
