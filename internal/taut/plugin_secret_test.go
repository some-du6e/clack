package taut

import (
	"testing"
	"time"
)

// A plugin saves a secret option through Taut's plugin manager,
// globalThis.__tautPluginManager.setPluginSecret (HMojis saves a pasted
// token so): the secret store has it, the plugin restarts with it in its
// config, and the promise answers true. For a plugin that isn't loaded it
// answers false and writes nothing.
func TestPluginManagerSetPluginSecret(t *testing.T) {
	h := newHarness(t)
	h.install(pluginJS(configPlugin), true)
	var r struct {
		Type        string
		OK, Missing bool
	}
	h.jsInto(`globalThis.__cfgSeen = null
const m = globalThis.__tautPluginManager
const ok = await m.setPluginSecret('Cfg', 'token', 'tok-1')
const missing = await m.setPluginSecret('NoSuchPlugin', 'token', 'x')
return { Type: typeof m.setPluginSecret, OK: ok, Missing: missing }`, &r)
	if r.Type != "function" || !r.OK || r.Missing {
		t.Fatalf("setPluginSecret: %+v, want true for Cfg and false for a plugin that isn't loaded", r)
	}
	if v, _ := h.tt.data.readSecret("plugin:Cfg:token"); v != "tok-1" {
		t.Errorf("the secret store has %q, want tok-1", v)
	}
	if v, _ := h.tt.data.readSecret("plugin:NoSuchPlugin:token"); v != "" {
		t.Errorf("a plugin that isn't loaded got a secret: %q", v)
	}
	var seen map[string]any
	eventually(t, 5*time.Second, "the plugin restarted with the token", func() bool {
		h.jsInto(`return globalThis.__cfgSeen`, &seen)
		return seen != nil && seen["token"] == "tok-1"
	})
}
