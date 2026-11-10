package taut

import (
	"strings"
	"testing"
)

// TestPluginExperimentOverridesReachCore: whatever a plugin does to Slack's
// experiments slice reaches core's experiment checks, live, and goes away
// with the plugin. Taut's api.experiments (Taut's Experiments,
// BringBackSlackbot) and a raw slice patch (Slick's Experiments) take the
// same path.
func TestPluginExperimentOverridesReachCore(t *testing.T) {
	h := newHarness(t)
	forced := func() map[string]string { return h.app.ForcedExperiments() }

	id := h.install(pluginJS(`class ForceExp extends TautPlugin {
  static id = 'ForceExp'
  static pluginName = 'Force experiments'
  static description = 'forces experiment groups'
  static authors = ['jeremy']
  static category = 'other'
  static defaultConfig = {enabled: false}
  start() {
    this.api.experiments.set('filter_dormant_channels', 'off')
    this.api.experiments.set('clack_test_unassigned', null)
  }
}
return ForceExp`), true)
	eventually(t, renderWait(), "api.experiments in core", func() bool {
		return h.app.Experiment("filter_dormant_channels") == "off"
	})
	// null on an experiment the server never assigned reads the same: nothing to force
	if _, ok := forced()["clack_test_unassigned"]; ok {
		t.Errorf("an unassigned experiment forced to null was sent: %v", forced())
	}
	if err := h.tt.SetEnabled(h.ctx(), id, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, renderWait(), "the groups back after the plugin stopped", func() bool { return len(forced()) == 0 })

	id = h.install(pluginJS(`class PatchExp extends TautPlugin {
  static id = 'PatchExp'
  static pluginName = 'Patch experiments'
  static description = 'patches the experiments slice'
  static authors = ['jeremy']
  static category = 'other'
  static defaultConfig = {enabled: false}
  start() {
    this.api.redux.patchSlice('experiments',
      (name, a) => name === 'sidebar_section_search' ? {...(a ?? {experiment_id: 'from-override', type: 'user'}), group: 'off', log_exposures: false} : a,
      () => ['sidebar_section_search'])
  }
}
return PatchExp`), true)
	eventually(t, renderWait(), "a slice patch in core", func() bool {
		return h.app.Experiment("sidebar_section_search") == "off"
	})
	if err := h.tt.SetEnabled(h.ctx(), id, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, renderWait(), "the patch gone", func() bool { return len(forced()) == 0 })
}

// TestExperimentReadsModule: Clack's own experiment checks are a module
// source in the format the Experiments plugins scan for, next to Slack's
// selectors module.
func TestExperimentReadsModule(t *testing.T) {
	h := newHarness(t)
	var src, got string
	h.jsInto(`return [...T.getModuleSources()].find(([id]) => id === 'clack-experiment-reads')?.[1] ?? ''`, &src)
	for _, want := range []string{`x=n("experiments")`, `"on"===(0,x.getGroupForUser)(s,"filter_dormant_channels")`,
		`"without_edu"===(0,x.getGroupForUser)(s,"dm_recency_sort_rollout")||"with_edu"===(0,x.getGroupForUser)(s,"dm_recency_sort_rollout")`} {
		if !strings.Contains(src, want) {
			t.Errorf("reads module lacks %s:\n%.300s", want, src)
		}
	}
	// Slack's getters answer null for an experiment the viewer isn't in
	h.jsInto(`const m = T.getExport((e) => e && typeof e === 'object' && typeof e.getGroupForUserWithoutExposure === 'function'); return String(m.getGroupForUser({experiments: {}}, 'nope')) + ',' + m.getGroupForUserWithoutExposure({experiments: {a: {group: 'on'}}}, 'a')`, &got)
	if got != "null,on" {
		t.Errorf("selectors: %s", got)
	}
}
