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
		t.Fatal("MAMenu patched before a plugin declaring the menu API started")
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
	if !h.patched("MAMenu") {
		t.Fatal("Slick's MAMenu patch isn't installed")
	}
	if n := h.state().MessageActions; n != 3 {
		t.Errorf("%d providers registered, want 3", n)
	}

	// the root menu: one more group, its actions after the menu's, the
	// one whose id the menu has left out, data-qa defaulting to the id
	p := h.maRendered(1, maProps("1700000002.000200", true))
	ids := actionIDs(p)
	if len(ids) != 3 || !slices.Equal(ids[2], []string{"acts_one", "acts_two"}) || !slices.Equal(ids[0], []string{"mark_unread"}) {
		t.Fatalf("groups %v", ids)
	}
	added := p["actions"].([]any)[2].([]any)
	one, two := added[0].(map[string]any), added[1].(map[string]any)
	if one["name"] != "Act one" || one["data-qa"] != "acts_one" || two["data-qa"] != "acts-two" || two["icon"] != "threads" {
		t.Errorf("added actions %v %v", one, two)
	}
	// selecting one runs its onSelect and answers true; the menu's own
	// actions go on to Slack's onSelectAction (none here: undefined)
	sel := p["onSelectAction"]
	if got := h.callFn(sel, `[{"type":"click"},{"id":"acts_two"},{}]`); got != "true" {
		t.Errorf("onSelectAction(acts_two) = %s, want true", got)
	}
	if got := h.callFn(sel, `[{"type":"click"},{"id":"mark_unread"},{}]`); got != "null" {
		t.Errorf("onSelectAction(mark_unread) = %s, want Slack's (none)", got)
	}
	if got := h.callFn(sel, `[{"type":"click"},{"id":"copy_link"},{}]`); got != "true" {
		t.Errorf("onSelectAction(copy_link) = %s: a provider's action with the menu's id still selects (custom.find)", got)
	}
	if picked := h.jsString(`return globalThis.__actsPicked.join(",")`); picked != "two,dup" {
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

	// stopping the plugin removes its providers; Slick's patch stays
	if err := h.s.SetEnabled(h.ctx(), "ActsSlick", false); err != nil {
		t.Fatal(err)
	}
	if n := h.state().MessageActions; n != 0 {
		t.Errorf("%d providers left after stop", n)
	}
	if ids := actionIDs(h.maRendered(5, maProps("1700000002.000200", true))); len(ids) != 2 {
		t.Errorf("actions after stop: %v", ids)
	}
	if !h.patched("MAMenu") {
		t.Error("Slick's MAMenu patch went with the plugin")
	}
}

// A plugin that doesn't declare the menu API can't reach it, and doesn't
// make Slick patch MAMenu.
func TestMessageActionsNeedTheMenuAPI(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, pluginSource("NoMenuSlick", `["redux"]`, `
  start() { this.api.menu.addMessageActions(() => []); }`))
	_ = h.s.SetEnabled(h.ctx(), "NoMenuSlick", true)
	if info := h.plugin("NoMenuSlick"); info.Running || !strings.Contains(info.StartError, "Plugin must declare requiredAPIs: menu") {
		t.Fatalf("running=%v error=%q", info.Running, info.StartError)
	}
	if h.patched("MAMenu") {
		t.Error("MAMenu patched for a plugin without the menu API")
	}
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
	if len(ids) != 3 || !slices.Equal(ids[0], []string{"acts_one", "acts_two"}) {
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
// mounted through Slick.RenderPanel, gone when the plugin stops.
func TestSettingsPanel(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, pluginSource("PanelSlick", `[]`, `
  static liveSettings = ["word"];
  static settings = { word: { type: "text", label: "Word", default: "Pick" } };
  start() { this.show(); }
  onSettingsChange() { this.show(); }
  show() {
    const word = this.config.word;
    this.api.settings.panel(() => React.createElement("div", { className: "panel-slick" }, word + " your actions"));
  }`))
	if h.plugin("PanelSlick").Panel != 0 {
		t.Fatal("a panel before the plugin ran")
	}
	if err := h.s.SetEnabled(h.ctx(), "PanelSlick", true); err != nil {
		t.Fatal(err)
	}
	var first int
	eventually(t, 5*time.Second, "the plugin's panel in its info", func() bool {
		first = h.plugin("PanelSlick").Panel
		return first != 0
	})
	if text := h.panelText(7, "PanelSlick"); text != "Pick your actions" {
		t.Errorf("panel %q", text)
	}
	// a new panel is a new Panel number (Preferences mounts it again)
	if err := h.s.SetOption(h.ctx(), "PanelSlick", "word", "Sort"); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the new panel", func() bool {
		p := h.plugin("PanelSlick").Panel
		return p != 0 && p != first
	})
	if text := h.panelText(8, "PanelSlick"); text != "Sort your actions" {
		t.Errorf("panel %q", text)
	}
	if err := h.s.SetEnabled(h.ctx(), "PanelSlick", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "no panel after stop", func() bool { return h.plugin("PanelSlick").Panel == 0 })
	if left := h.state().Panels; len(left) != 0 {
		t.Errorf("panels left: %v", left)
	}
}

// panelText mounts plugin id's panel into root and returns its text.
func (h *harness) panelText(root uint64, id string) string {
	h.t.Helper()
	h.ui.mu.Lock()
	delete(h.ui.trees, root)
	h.ui.mu.Unlock()
	h.s.RenderPanel(root, id)
	var tree []byte
	eventually(h.t, 5*time.Second, "the panel's tree", func() bool {
		h.ui.mu.Lock()
		defer h.ui.mu.Unlock()
		tree = h.ui.trees[root]
		return tree != nil
	})
	var nodes []any
	if err := json.Unmarshal(tree, &nodes); err != nil {
		h.t.Fatalf("tree: %v", err)
	}
	return treeText(nodes)
}
