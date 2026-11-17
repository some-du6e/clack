package taut

import (
	"encoding/json"
	"slices"
	"testing"
)

// Slack's navigate thunk (client-boot pf/G → navigateInMainWindow, qBXZ):
// Clack opens the views it has through their deep links (UIHooks.DeepLink)
// and, as Slack's, resolves undefined whatever happened, so a caller
// checking `result === false` (Slick's ChatTabs) goes on. A thread opens
// beside its channel (Slack's forceRenderInPrimary draws it alone).
func TestNavigateThunk(t *testing.T) {
	h := newHarness(t)
	out := h.js(`
		const r = []
		for (const args of [
			{ id: 'thread', viewType: 'Thread', params: { threadId: 'C0GENERAL1-1700000002.000200', highlightRoot: true }, options: { forceRenderInPrimary: true, forceBackStack: true } },
			{ id: 'C0GENERAL1', viewType: 'Channel' },
			{ id: 'UALICE0001', viewType: 'Profile' },
			{ id: 'whatever', viewType: 'BrowseApps' },
			{ id: 'thread', viewType: 'Thread', params: { threadId: 'nonsense' } },
		]) {
			const v = await T.redux.dispatchThunk('navigate', args)
			r.push(v === undefined ? 'undefined' : v === null ? 'null' : JSON.stringify(v))
		}
		return r`)
	var results []string
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatal(err)
	}
	for i, r := range results {
		if r != "undefined" && r != "null" {
			t.Errorf("navigate #%d resolved %s, Slack's resolves undefined", i, r)
		}
	}
	h.ui.mu.Lock()
	links := slices.Clone(h.ui.links)
	h.ui.mu.Unlock()
	want := []string{
		"slack://channel?id=C0GENERAL1&thread_ts=1700000002.000200",
		"slack://channel?id=C0GENERAL1",
		"slack://user?id=UALICE0001",
	}
	if !slices.Equal(links, want) {
		t.Errorf("deep links %q, want %q", links, want)
	}
}
