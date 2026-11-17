package slick

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// Slick #473's api.menu.addMessageActions and api.settings.panel, and
// the MAMenu patch Slick installs for them (src/app/api/menu.tsx,
// settingsTabs.ts, pluginManager.ts), against plugins in build:plugin's
// shape.

// pluginSource is a plugin file as Slick's build:plugin writes one (a
// factory queued on __slickPlugins, $slick from the host) with body as
// its class body; required "" leaves requiredAPIs at Slick's default
// (every API).
func pluginSource(id, required, body string) string {
	req := ""
	if required != "" {
		req = "  static requiredAPIs = " + required + ";\n"
	}
	return `// slick-plugin {"id":"` + id + `"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class P extends import_slick.SlickPlugin {
  static id = "` + id + `";
  static pluginName = "` + id + `";
  static description = "test";
` + req + body + `
}
return P;
});
`
}

// actsPlugin adds two actions to message 1700000002.000200's menu
// (plus one whose id the menu already has), and has two providers that
// give nothing: one returns undefined, one throws.
var actsPlugin = pluginSource("ActsSlick", `["menu"]`, `
  start() {
    globalThis.__actsPicked = [];
    this.api.menu.addMessageActions(({ channelId, ts }) => channelId === "C0GENERAL1" && ts === "1700000002.000200" ? [
      { id: "acts_one", name: "Act one", onSelect: () => globalThis.__actsPicked.push("one") },
      { id: "acts_two", name: "Act two", "data-qa": "acts-two", icon: "threads", onSelect: () => globalThis.__actsPicked.push("two") },
      { id: "copy_link", name: "Their copy link", onSelect: () => globalThis.__actsPicked.push("dup") },
      { id: "acts_boom", name: "Act boom", onSelect: () => { globalThis.__actsPicked.push("boom"); throw new Error("boom"); } },
    ] : []);
    this.api.menu.addMessageActions(() => undefined);
    this.api.menu.addMessageActions(() => { throw new Error("no actions here"); });
    globalThis.__actsMenuKeys = Object.keys(this.api.menu).sort();
  }`)

// maProps are MAMenu's props for a message the way Clack's More actions
// menu passes them (popups' MessageActionsProps), two native groups.
func maProps(ts string, root bool) map[string]any {
	p := map[string]any{
		"channelId": chGen, "ts": ts, "messageContainerType": "message_pane",
		// Go's menu close (a Go func crossing into JS), counted by the harness
		"onTriggerClose": map[string]any{"$gofn": triggerCloseFn},
		"actions": []any{
			[]any{map[string]any{"id": "mark_unread", "name": "Mark unread", "data-qa": "mark_unread"}},
			[]any{map[string]any{"id": "copy_link", "name": "Copy link", "data-qa": "copy_link"},
				map[string]any{"id": "organize_actions_menu", "name": "Organize", "data-qa": "organize_actions_menu",
					"submenuActions": []any{map[string]any{"id": "pin", "name": "Pin to channel", "data-qa": "pin_message"}}}},
		},
	}
	if root {
		p["autoClogProps"] = map[string]any{"uiComponentName": "message_actions_menu", "uiComponentVariant": "message_actions_menu_overflow_menu"}
	}
	return p
}

// triggerCloseFn is the id of the Go func maProps passes as onTriggerClose.
const triggerCloseFn = 424242

// maRendered renders MAMenu with props and returns the props its native
// (Clack's menu) got.
func (h *harness) maRendered(root uint64, props map[string]any) map[string]any {
	h.t.Helper()
	n := findNative(h.render(root, "MAMenu", props), "MAMenu")
	if n == nil {
		h.t.Fatal("no MAMenu native in the tree")
	}
	p, _ := n["p"].(map[string]any)
	return p
}

// actionIDs lists each group's action ids.
func actionIDs(p map[string]any) [][]string {
	var out [][]string
	groups, _ := p["actions"].([]any)
	for _, g := range groups {
		var ids []string
		list, _ := g.([]any)
		for _, a := range list {
			m, _ := a.(map[string]any)
			id, _ := m["id"].(string)
			ids = append(ids, id)
		}
		out = append(out, ids)
	}
	return out
}

func (h *harness) patched(name string) bool {
	return slices.Contains(h.state().Components, name)
}

// callFn calls a function prop of a host tree with JSON args as Go's
// natives do (slackc.JSFunc.CallResult) and returns its JSON result
// ("null" for undefined).
func (h *harness) callFn(fn any, args string) string {
	h.t.Helper()
	m, _ := fn.(map[string]any)
	id, ok := m["$fn"].(float64)
	if !ok {
		h.t.Fatalf("not a function prop: %v", fn)
	}
	ch := make(chan string, 1)
	h.tt.Bridge().Call(int64(id), []byte(args), func(res []byte, err error) {
		switch {
		case err != nil:
			ch <- "error: " + err.Error()
		case len(res) == 0:
			ch <- "null"
		default:
			ch <- string(res)
		}
	})
	select {
	case s := <-ch:
		return s
	case <-time.After(5 * time.Second):
		h.t.Fatal("the function never answered")
	}
	return ""
}

func TestMessageActions(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, actsPlugin)
	if h.patched("MAMenu") {
		t.Fatal("MAMenu patched before a plugin added a message action provider")
	}
	if err := h.s.SetEnabled(h.ctx(), "ActsSlick", true); err != nil {
		t.Fatal(err)
	}
	if info := h.plugin("ActsSlick"); !info.Running || info.StartError != "" {
		t.Fatalf("didn't start: %q", info.StartError)
	}
	if keys := h.jsString(`return globalThis.__actsMenuKeys.join(",")`); keys != "Menu,addMessageActions,getMessageActions" {
		t.Errorf("api.menu has %s", keys)
	}
	if !h.patched("MAMenu") || !h.state().MessageMenu {
		t.Fatal("Slick's MAMenu patch isn't installed with the providers")
	}
	if n := h.state().MessageActions; n != 3 {
		t.Errorf("%d providers registered, want 3", n)
	}

	// the root menu: one more group, its actions after the menu's, the
	// one whose id the menu has left out, data-qa defaulting to the id
	p := h.maRendered(1, maProps("1700000002.000200", true))
	ids := actionIDs(p)
	if len(ids) != 3 || !slices.Equal(ids[2], []string{"acts_one", "acts_two", "acts_boom"}) || !slices.Equal(ids[0], []string{"mark_unread"}) {
		t.Fatalf("groups %v", ids)
	}
	added := p["actions"].([]any)[2].([]any)
	one, two := added[0].(map[string]any), added[1].(map[string]any)
	if one["name"] != "Act one" || one["data-qa"] != "acts_one" || two["data-qa"] != "acts-two" || two["icon"] != "threads" {
		t.Errorf("added actions %v %v", one, two)
	}
	// selecting one runs its onSelect, then the menu's onTriggerClose, and
	// answers true; the menu's own actions go on to Slack's onSelectAction
	// (none here: undefined) and don't close it
	sel := p["onSelectAction"]
	if got := h.callFn(sel, `[{"type":"click"},{"id":"acts_two"},{}]`); got != "true" {
		t.Errorf("onSelectAction(acts_two) = %s, want true", got)
	}
	eventually(t, 3*time.Second, "onTriggerClose after acts_two", func() bool { return h.ui.called(triggerCloseFn) == 1 })
	if got := h.callFn(sel, `[{"type":"click"},{"id":"mark_unread"},{}]`); got != "null" {
		t.Errorf("onSelectAction(mark_unread) = %s, want Slack's (none)", got)
	}
	if got := h.callFn(sel, `[{"type":"click"},{"id":"copy_link"},{}]`); got != "true" {
		t.Errorf("onSelectAction(copy_link) = %s: a provider's action with the menu's id still selects (custom.find)", got)
	}
	// an onSelect that throws still closes the menu (try/finally), and the
	// throw reaches the caller
	if got := h.callFn(sel, `[{"type":"click"},{"id":"acts_boom"},{}]`); !strings.Contains(got, "boom") {
		t.Errorf("onSelectAction(acts_boom) = %s, want its throw", got)
	}
	eventually(t, 3*time.Second, "onTriggerClose after copy_link and acts_boom", func() bool { return h.ui.called(triggerCloseFn) == 3 })
	if picked := h.jsString(`return globalThis.__actsPicked.join(",")`); picked != "two,dup,boom" {
		t.Errorf("picked %q", picked)
	}

	// a submenu's MAMenu (no autoClogProps): nothing added, selections
	// still go to the providers
	sp := h.maRendered(2, maProps("1700000002.000200", false))
	if ids := actionIDs(sp); len(ids) != 2 {
		t.Errorf("a submenu got actions: %v", ids)
	}
	if got := h.callFn(sp["onSelectAction"], `[{"type":"click"},{"id":"acts_one"},{}]`); got != "true" {
		t.Errorf("submenu onSelectAction(acts_one) = %s", got)
	}
	// an outer patch that placed them itself says so: nothing added, the
	// flag gone from what the menu gets
	hp := maProps("1700000002.000200", true)
	hp["slickMessageActionsHandled"] = true
	handled := h.maRendered(3, hp)
	if ids := actionIDs(handled); len(ids) != 2 {
		t.Errorf("handled menu got actions: %v", ids)
	}
	if _, ok := handled["slickMessageActionsHandled"]; ok {
		t.Error("slickMessageActionsHandled reached the menu")
	}
	// another message: every provider gives nothing (the throwing one and
	// the one returning undefined are logged and skipped): the menu as it was
	other := h.maRendered(4, maProps("1700000001.000100", true))
	if ids := actionIDs(other); len(ids) != 2 {
		t.Errorf("a message with no actions got %v", ids)
	}
	if _, ok := other["onSelectAction"]; ok {
		t.Error("onSelectAction replaced for a message without actions")
	}

	// stopping the plugin removes its providers, and with the last one
	// Slick's patch: message menus are Clack's own again
	if err := h.s.SetEnabled(h.ctx(), "ActsSlick", false); err != nil {
		t.Fatal(err)
	}
	if n := h.state().MessageActions; n != 0 {
		t.Errorf("%d providers left after stop", n)
	}
	if ids := actionIDs(h.maRendered(5, maProps("1700000002.000200", true))); len(ids) != 2 {
		t.Errorf("actions after stop: %v", ids)
	}
	if h.patched("MAMenu") || h.state().MessageMenu {
		t.Error("Slick's MAMenu patch outlived its last provider")
	}
}

// A plugin that doesn't declare the menu API can't reach it; one that
// starts without it doesn't make Slick patch MAMenu, so Clack's message
// menus stay native with no plugin runtime in them (Slick loads the menu
// integration only for plugins that declare it).
func TestMessageActionsNeedTheMenuAPI(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, pluginSource("NoMenuSlick", `["redux"]`, `
  start() { globalThis.__noMenuStarted = true; }`))
	if err := h.s.SetEnabled(h.ctx(), "NoMenuSlick", true); err != nil {
		t.Fatal(err)
	}
	if info := h.plugin("NoMenuSlick"); !info.Running {
		t.Fatalf("didn't start: %q", info.StartError)
	}
	if h.patched("MAMenu") {
		t.Error("MAMenu patched for a plugin without the menu API")
	}
	// touching api.menu without declaring it throws Slick's error, and
	// still patches nothing
	mustImport(t, h, pluginSource("TouchMenuSlick", `["redux"]`, `
  start() { this.api.menu.addMessageActions(() => []); }`))
	_ = h.s.SetEnabled(h.ctx(), "TouchMenuSlick", true)
	if info := h.plugin("TouchMenuSlick"); info.Running || !strings.Contains(info.StartError, "Plugin must declare requiredAPIs: menu") {
		t.Fatalf("running=%v error=%q", info.Running, info.StartError)
	}
	if h.patched("MAMenu") {
		t.Error("MAMenu patched by a plugin that only touched api.menu")
	}
	// declaring it gives the plugin the menu API (getMessageActions,
	// addMessageActions); Slick's MAMenu patch comes with the first
	// provider and goes with the last (in Slick it loads with the API and
	// stays: with no provider it changes nothing but drop
	// slickMessageActionsHandled, so message menus stay Clack's own)
	mustImport(t, h, pluginSource("MenuOnlySlick", `["menu"]`, `
  start() {
    globalThis.__menuOnly = typeof this.api.menu.getMessageActions + " " + typeof this.api.menu.addMessageActions;
    globalThis.__menuOnlyAPI = this.api.menu;
  }`))
	if err := h.s.SetEnabled(h.ctx(), "MenuOnlySlick", true); err != nil {
		t.Fatal(err)
	}
	if info := h.plugin("MenuOnlySlick"); !info.Running {
		t.Fatalf("MenuOnlySlick didn't start: %q", info.StartError)
	}
	if got := h.jsString(`return globalThis.__menuOnly`); got != "function function" {
		t.Errorf("api.menu: %s", got)
	}
	if h.patched("MAMenu") || h.state().MessageMenu {
		t.Error("MAMenu patched with no message action provider")
	}
	h.jsString(`globalThis.__menuOnlyOff = globalThis.__menuOnlyAPI.addMessageActions(() => []); return ""`)
	eventually(t, 3*time.Second, "Slick's MAMenu patch with a provider", func() bool { return h.patched("MAMenu") && h.state().MessageMenu })
	h.jsString(`globalThis.__menuOnlyOff(); return ""`)
	eventually(t, 3*time.Second, "Slick's MAMenu patch gone with the last provider", func() bool { return !h.patched("MAMenu") && !h.state().MessageMenu })
	// a stop takes a provider the plugin forgot to remove, and the patch
	h.jsString(`globalThis.__menuOnlyAPI.addMessageActions(() => []); return ""`)
	eventually(t, 3*time.Second, "the patch with a provider again", func() bool { return h.patched("MAMenu") })
	if err := h.s.SetEnabled(h.ctx(), "MenuOnlySlick", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, 3*time.Second, "the patch gone with the plugin", func() bool { return !h.patched("MAMenu") && !h.state().MessageMenu })
}

// BetterMessageMenu's way: an outer MAMenu patch reads the providers'
// actions (api.menu.getMessageActions), places them itself and passes
// slickMessageActionsHandled; the selection still reaches the provider
// through Slick's patch.
func TestMessageActionsPlacedByAnotherPatch(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, actsPlugin)
	mustImport(t, h, pluginSource("PlacerSlick", `["menu"]`, `
  start() {
    this.api.patchComponent("MAMenu", (Original) => (props) => {
      if (props.autoClogProps?.uiComponentName !== "message_actions_menu" || !Array.isArray(props.actions)) return React.createElement(Original, props);
      const custom = this.api.menu.getMessageActions(props).filter((a) => a.id.startsWith("acts_"));
      return React.createElement(Original, { ...props, actions: [custom, ...props.actions], slickMessageActionsHandled: true });
    });
  }`))
	for _, id := range []string{"ActsSlick", "PlacerSlick"} {
		if err := h.s.SetEnabled(h.ctx(), id, true); err != nil {
			t.Fatal(err)
		}
	}
	p := h.maRendered(1, maProps("1700000002.000200", true))
	ids := actionIDs(p)
	if len(ids) != 3 || !slices.Equal(ids[0], []string{"acts_one", "acts_two", "acts_boom"}) {
		t.Fatalf("groups %v: the placed actions first, none added at the end", ids)
	}
	if got := h.callFn(p["onSelectAction"], `[{"type":"click"},{"id":"acts_one"},{}]`); got != "true" {
		t.Errorf("onSelectAction(acts_one) = %s", got)
	}
	if picked := h.jsString(`return globalThis.__actsPicked.join(",")`); picked != "one" {
		t.Errorf("picked %q", picked)
	}
}

// Plugin files built before Slick #473 (ChatTabs) patched the message
// menu's renderer by a filter on its source: Clack's MAMenu matches it.
func TestOldMessageMenuPatch(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, pluginSource("OldTabsSlick", `["redux", "elements"]`, `
  start() {
    globalThis.__oldPicked = [];
    this.api.patchComponent({
      filter: (candidate) => {
        const render = candidate?.render ?? candidate;
        return typeof render === "function" && String(render).includes("renderActionGroup") && String(render).includes("dispatchMessageAction");
      },
    }, (Original) => (props) => {
      if (props.autoClogProps?.uiComponentName !== "message_actions_menu" || !Array.isArray(props.actions)) return React.createElement(Original, props);
      return React.createElement(Original, {
        ...props,
        actions: [[{ id: "old-tabs-open-thread", name: "Open in new Slick tab", icon: "threads", "data-qa": "old-tabs-open-thread" }], ...props.actions],
        onSelectAction: (event, action, context) => {
          if (action?.id === "old-tabs-open-thread") { globalThis.__oldPicked.push(context?.ts ?? "?"); return true; }
          return props.onSelectAction?.(event, action, context);
        },
      });
    });
  }`))
	if err := h.s.SetEnabled(h.ctx(), "OldTabsSlick", true); err != nil {
		t.Fatal(err)
	}
	if !h.patched("MAMenu") {
		t.Fatal("the filter on renderActionGroup / dispatchMessageAction matched no component")
	}
	p := h.maRendered(1, maProps("1700000002.000200", true))
	if ids := actionIDs(p); len(ids) != 3 || !slices.Equal(ids[0], []string{"old-tabs-open-thread"}) {
		t.Fatalf("groups %v", ids)
	}
	if got := h.callFn(p["onSelectAction"], `[{"type":"click"},{"id":"old-tabs-open-thread"},{"ts":"1700000002.000200"}]`); got != "true" {
		t.Errorf("onSelectAction = %s", got)
	}
	if picked := h.jsString(`return globalThis.__oldPicked.join(",")`); picked != "1700000002.000200" {
		t.Errorf("picked %q", picked)
	}
	if err := h.s.SetEnabled(h.ctx(), "OldTabsSlick", false); err != nil {
		t.Fatal(err)
	}
	if h.patched("MAMenu") {
		t.Error("the old patch outlived its plugin")
	}
}

// api.settings.panel: the plugin's own React for its Preferences page,
// mounted through Slick.RenderPanel once per plugin. As Slick's
// PluginDetail calls `panels[id]()` while it renders, the panel renders
// again in the same root when a live setting changes (no remount: its
// React state stays) and when the plugin sets another panel; it's gone
// when the plugin stops.
func TestSettingsPanel(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, pluginSource("PanelSlick", `[]`, `
  static liveSettings = ["word"];
  static settings = { word: { type: "text", label: "Word", default: "Pick" } };
  start() {
    globalThis.__panelMounts = globalThis.__panelMounts ?? 0;
    const plugin = this;
    function Panel() {
      React.useEffect(() => { globalThis.__panelMounts++; }, []);
      return React.createElement("div", { className: "panel-slick" }, plugin.config.word + " your actions");
    }
    this.api.settings.panel(() => React.createElement(Panel));
  }
  onSettingsChange() {
    if (this.config.word === "Swap") this.api.settings.panel(() => React.createElement("div", null, "a second panel"));
  }`))
	if panelOf(h.plugin("PanelSlick")) {
		t.Fatal("a panel before the plugin ran")
	}
	if err := h.s.SetEnabled(h.ctx(), "PanelSlick", true); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the plugin's panel in its info", func() bool { return panelOf(h.plugin("PanelSlick")) })
	const root = 7
	renderPanel(h.s, root, "PanelSlick")
	eventually(t, 5*time.Second, "the panel", func() bool { return h.rootText(root) == "Pick your actions" })
	// a live setting (this.config changes in place): the same root renders
	// it, without mounting the panel again
	if err := h.s.SetOption(h.ctx(), "PanelSlick", "word", "Sort"); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the panel after the live setting", func() bool { return h.rootText(root) == "Sort your actions" })
	if n := h.jsString(`return String(globalThis.__panelMounts)`); n != "1" {
		t.Errorf("the panel mounted %s times, want once", n)
	}
	// a second settings.panel call (from onSettingsChange): the same root
	// shows the new panel, as Slick's PluginDetail calls the plugin's
	// current panels[id]() as it renders
	if err := h.s.SetOption(h.ctx(), "PanelSlick", "word", "Swap"); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the plugin's second panel", func() bool { return h.rootText(root) == "a second panel" })
	if err := h.s.SetEnabled(h.ctx(), "PanelSlick", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "no panel after stop", func() bool { return !panelOf(h.plugin("PanelSlick")) && h.rootText(root) == "" })
	if left := h.state().Panels; len(left) != 0 {
		t.Errorf("panels left: %v", left)
	}
	// on again: the same root shows the new instance's panel
	if err := h.s.SetEnabled(h.ctx(), "PanelSlick", true); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the panel after a restart", func() bool { return h.rootText(root) == "Swap your actions" })
}

// rootText is the text of the tree last delivered for root ("" before).
func (h *harness) rootText(root uint64) string {
	h.ui.mu.Lock()
	tree := h.ui.trees[root]
	h.ui.mu.Unlock()
	if tree == nil {
		return ""
	}
	var nodes []any
	if err := json.Unmarshal(tree, &nodes); err != nil {
		return ""
	}
	return treeText(nodes)
}
