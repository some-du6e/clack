package taut

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestRenderedViewContents: Slack's RenderedViewContents is the view
// element itself, a div with p-view_contents p-view_contents--<viewType>
// and its className around its children. Clack's view body comes in as a
// child slot; a patch that puts a node before props.children (Slick's
// ChatTabs, its tab bar) gets it inside the view element, first, with the
// body slot after it. Slack's own layout for the element is a layered
// sheet, so plugin rules win over it.
func TestRenderedViewContents(t *testing.T) {
	h := newHarness(t)
	props := map[string]any{
		"$key": "primary", "className": "p-workspace__primary_view", "viewType": "primary",
		"children": map[string]any{"$slot": 41, "n": "ClackPrimaryViewBody", "p": map[string]any{"style": map[string]any{"flex": "1 1 0"}}},
	}

	view := func(nodes []any) (map[string]any, []any) {
		t.Helper()
		if len(nodes) != 1 {
			t.Fatalf("want one root node, got %d: %s", len(nodes), jsonOf(nodes))
		}
		d, _ := nodes[0].(map[string]any)
		if d["t"] != "div" {
			t.Fatalf("root isn't a div: %s", jsonOf(nodes))
		}
		p, _ := d["p"].(map[string]any)
		if got := p["className"]; got != "p-view_contents p-view_contents--primary p-workspace__primary_view" {
			t.Errorf("view classes %q", got)
		}
		kids, _ := d["c"].([]any)
		return d, kids
	}

	// unpatched: the element around the body slot
	_, kids := view(h.render(1, "RenderedViewContents", props))
	if len(kids) != 1 || findNative(kids, "ClackPrimaryViewBody") == nil {
		t.Fatalf("children %s, want the body slot", jsonOf(kids))
	}
	if n := findNative(kids, "ClackPrimaryViewBody"); n["p"].(map[string]any)["$slot"] != float64(41) {
		t.Errorf("the slot lost its id: %s", jsonOf(n))
	}
	h.ui.mu.Lock()
	sheet := h.ui.styles["clack:view-contents"]
	h.ui.mu.Unlock()
	if !strings.HasPrefix(sheet, "@layer ") || !strings.Contains(sheet, ".p-view_contents") || !strings.Contains(sheet, "position: relative") {
		t.Errorf("view layout sheet %q", sheet)
	}

	// patched as ChatTabs does: <Original {...props}>{bar}{props.children}</Original>
	h.js(`
		const R = globalThis.React
		T.patchComponent('RenderedViewContents', (Original) => (props) =>
			R.createElement(Original, props, R.createElement('div', { className: 'bar' }, 'tabs'), props.children))
		return 1`)
	eventually(t, 3*time.Second, "RenderedViewContents patched", func() bool {
		h.ui.mu.Lock()
		defer h.ui.mu.Unlock()
		return has(h.ui.patched, "RenderedViewContents")
	})
	var out []any
	eventually(t, 3*time.Second, "the bar inside the view", func() bool {
		_, kids = view(h.render(1, "RenderedViewContents", props))
		out = kids
		return len(kids) == 2
	})
	bar, _ := out[0].(map[string]any)
	if bar["t"] != "div" || bar["p"].(map[string]any)["className"] != "bar" {
		t.Errorf("first child %s, want the patch's bar", jsonOf(bar))
	}
	if n, _ := out[1].(map[string]any); n["t"] != "@" || n["n"] != "ClackPrimaryViewBody" {
		t.Errorf("second child %s, want the body slot", jsonOf(n))
	}
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
