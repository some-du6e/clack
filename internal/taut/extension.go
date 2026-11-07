package taut

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
)

// Extension lets another plugin system share Taut's runtime: the same JS
// engine, Redux mirror, patchComponent registry, thunk routing, composer and
// message hooks, so everything the UI wired for Taut works for it too.
// internal/slick (Slick plugins) is the one user.
//
// Extensions cost nothing until something calls Taut.JS: a registered
// extension doesn't start the runtime.
type Extension struct {
	// Name identifies the extension in logs.
	Name string
	// Install registers host functions. It runs on every fresh runtime,
	// after the TautAPI is up and before Prelude.
	Install func(rt *jsrt.Runtime)
	// Prelude is a classic script evaluated after Install. It can use the
	// TautAPI internals on globalThis.__taut and the base API on
	// globalThis.TautAPI.
	Prelude string
	// Ready runs (on its own goroutine) once the runtime is attached:
	// restarted is false for the first runtime and true for a fresh one
	// after the engine crashed, when the extension should restart what
	// it was running.
	Ready func(rt *jsrt.Runtime, restarted bool)
	// LocalStorage serves read-only virtual localStorage keys (e.g. Slack's
	// localConfig_v2). They shadow stored keys and are never written to
	// disk.
	LocalStorage func(key string) (string, bool)
	// LocalStorageKeys lists the virtual keys, for localStorage.key/length.
	LocalStorageKeys func() []string
}

type extensions struct {
	mu       sync.Mutex
	list     []Extension
	runtimes int // runtimes started so far (>1 after a crash)
}

func (e *extensions) snapshot() []Extension {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Extension(nil), e.list...)
}

// Extend registers an extension. If the runtime is already up, the
// extension is installed into it right away and Ready runs.
func (t *Taut) Extend(e Extension) error {
	t.exts.mu.Lock()
	t.exts.list = append(t.exts.list, e)
	t.exts.mu.Unlock()
	t.mu.Lock()
	r := t.rt
	t.mu.Unlock()
	if r == nil {
		return nil
	}
	if err := installExtension(r.rt, e); err != nil {
		return err
	}
	if e.Ready != nil {
		go e.Ready(r.rt, false)
	}
	return nil
}

func installExtension(rt *jsrt.Runtime, e Extension) error {
	if e.Install != nil {
		e.Install(rt)
	}
	if e.Prelude != "" {
		if _, err := rt.Eval(e.Name+".js", e.Prelude); err != nil {
			return fmt.Errorf("taut: extension %s: %w", e.Name, err)
		}
	}
	return nil
}

// installExtensions runs in startRuntime, after __taut.init.
func (t *Taut) installExtensions(rt *jsrt.Runtime) error {
	for _, e := range t.exts.snapshot() {
		if err := installExtension(rt, e); err != nil {
			return err
		}
	}
	return nil
}

// readyExtensions runs after a runtime is attached.
func (t *Taut) readyExtensions(rt *jsrt.Runtime) {
	t.exts.mu.Lock()
	t.exts.runtimes++
	restarted := t.exts.runtimes > 1
	t.exts.mu.Unlock()
	for _, e := range t.exts.snapshot() {
		if e.Ready != nil {
			go e.Ready(rt, restarted)
		}
	}
}

// JS returns the shared JS runtime, starting it (and Taut's mirror and
// the slackc bridge) if needed. Extensions call it when they have
// something to run.
func (t *Taut) JS() (*jsrt.Runtime, error) {
	r, err := t.ensureRuntime()
	if err != nil {
		return nil, err
	}
	return r.rt, nil
}

// App is the core.App Taut serves plugins from.
func (t *Taut) App() *core.App { return t.app }

// UIHooks are the hooks the ui root gave Taut (extensions draw through the
// same ones: styles, toasts, preferences).
func (t *Taut) UIHooks() UIHooks { return t.opts.UI }

// FetchFunc is the HTTP transport plugins' fetch uses.
func (t *Taut) FetchFunc() func(ctx context.Context, req *http.Request) (*http.Response, error) {
	return t.opts.Fetch
}

// Namespace data: extensions keep plugin data in Taut's data store under
// their own namespaces (names ending in ":secrets" go to the 0600 secret
// store), so one write-back and one Close flush cover both.

// DataList lists the keys of namespace ns.
func (t *Taut) DataList(ns string) []string { return t.data.list(ns) }

// DataRead reads ns[key].
func (t *Taut) DataRead(ns, key string) (string, bool) { return t.data.read(ns, key) }

// DataWrite writes ns[key].
func (t *Taut) DataWrite(ns, key, value string) { t.data.write(ns, key, value) }

// DataDelete deletes ns[key] and reports whether it existed.
func (t *Taut) DataDelete(ns, key string) bool { return t.data.delete(ns, key) }

// DataClear deletes every key of ns.
func (t *Taut) DataClear(ns string) { t.data.clear(ns) }

// virtualKV is localStorage with the extensions' virtual keys on top.
type virtualKV struct {
	base jsrt.KV
	t    *Taut
}

func (v *virtualKV) virtual(key string) (string, bool) {
	for _, e := range v.t.exts.snapshot() {
		if e.LocalStorage != nil {
			if s, ok := e.LocalStorage(key); ok {
				return s, true
			}
		}
	}
	return "", false
}

func (v *virtualKV) Get(key string) (string, bool) {
	if s, ok := v.virtual(key); ok {
		return s, true
	}
	return v.base.Get(key)
}

func (v *virtualKV) Set(key, value string) {
	if _, ok := v.virtual(key); ok {
		return
	}
	v.base.Set(key, value)
}

func (v *virtualKV) Delete(key string) {
	if _, ok := v.virtual(key); ok {
		return
	}
	v.base.Delete(key)
}

func (v *virtualKV) Keys() []string {
	keys := v.base.Keys()
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	for _, e := range v.t.exts.snapshot() {
		if e.LocalStorageKeys == nil {
			continue
		}
		for _, k := range e.LocalStorageKeys() {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func (v *virtualKV) Clear() { v.base.Clear() }
