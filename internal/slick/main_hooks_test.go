package slick

import (
	"slices"
	"testing"
)

// SelfCare's main half (Slick main), in its shape: during a break it
// clears the app badge (app.getBadgeCount / setBadgeCount) and mutes the
// Slack window (webContents isAudioMuted / setAudioMuted). Clack's
// electron shim had neither, so its ready threw after it had added its
// notification filter, which then outlived the plugin. A main half's
// filters go with it however it ends: a ready that throws, a stop, a
// disposer that forgot them.
func TestMainHalfBreakHooks(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, probePlugin("BreakProbe", `{}`, ""))
	mustImport(t, h, probeMain("BreakProbe", `["notifications"]`, `
  async ready(ctx) {
    const { app } = require("electron");
    const remove = ctx.notifications.filter(() => true);
    app.setBadgeCount(app.getBadgeCount()); // SelfCare's silenceAttention as it starts
    if (globalThis.__breakFails) throw new Error("ready failed after adding its filter");
    return globalThis.__breakForgets ? undefined : remove;
  },
  window(ctx, window) { globalThis.__breakWindow = window.webContents; },
  rpc: {
    begin() {
      const { app } = require("electron");
      const c = globalThis.__breakWindow;
      app.setBadgeCount(5);
      const count = app.getBadgeCount();
      app.setBadgeCount(0);
      const was = c.isAudioMuted();
      c.setAudioMuted(true);
      return { count, cleared: app.getBadgeCount(), was, muted: c.isAudioMuted() };
    },
  }`))
	enable := func(on bool) {
		t.Helper()
		if err := h.s.SetEnabled(h.ctx(), "BreakProbe", on); err != nil {
			t.Fatal(err)
		}
	}
	info := func() PluginInfo {
		t.Helper()
		for _, p := range h.s.Settings().Plugins {
			if p.ID == "BreakProbe" {
				return p
			}
		}
		t.Fatal("BreakProbe isn't installed")
		return PluginInfo{}
	}

	// a ready that throws after adding its filter leaves none behind, and
	// Preferences says the privileged half didn't start
	h.jsString(`globalThis.__breakFails = true; return ""`)
	enable(true)
	if p := info(); !p.Running || mainErrorOf(p) == "" {
		t.Errorf("a failed ready: running %v, main error %q", p.Running, mainErrorOf(p))
	}
	if st := h.state(); st.NotifyFilters != 0 || slices.Contains(st.Mains, "BreakProbe") {
		t.Errorf("a failed ready left %d notification filters (mains %v)", st.NotifyFilters, st.Mains)
	}
	enable(false)

	// a break: the badge and the window's mute answer as Electron's do
	h.jsString(`globalThis.__breakFails = false; return ""`)
	enable(true)
	if p := info(); mainErrorOf(p) != "" {
		t.Fatalf("main error %q", mainErrorOf(p))
	}
	if st := h.state(); st.NotifyFilters != 1 || !slices.Contains(st.Mains, "BreakProbe") {
		t.Fatalf("running: %d notification filters, mains %v", st.NotifyFilters, st.Mains)
	}
	var r struct {
		Count   int  `json:"count"`
		Cleared int  `json:"cleared"`
		Was     bool `json:"was"`
		Muted   bool `json:"muted"`
	}
	h.jsInto(`return await S.callMain("BreakProbe", "begin", [])`, &r)
	if r.Count != 5 || r.Cleared != 0 || r.Was || !r.Muted {
		t.Errorf("break %+v: want the count kept and cleared, the window muted", r)
	}
	enable(false)
	if st := h.state(); st.NotifyFilters != 0 {
		t.Errorf("stopped: %d notification filters", st.NotifyFilters)
	}

	// a disposer that doesn't remove its filter: stopping drops it anyway
	h.jsString(`globalThis.__breakForgets = true; return ""`)
	enable(true)
	if st := h.state(); st.NotifyFilters != 1 {
		t.Fatalf("running: %d notification filters", st.NotifyFilters)
	}
	enable(false)
	if st := h.state(); st.NotifyFilters != 0 {
		t.Errorf("stopped with a forgetful disposer: %d notification filters", st.NotifyFilters)
	}
}
