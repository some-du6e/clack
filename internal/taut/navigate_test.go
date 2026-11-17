package taut

import (
	"context"
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
		if r != "undefined" {
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

// Slack's view state as plugins read it (ChatTabs' currentTab): the main
// window's active tab and its primary view. The route Clack shows is a
// Channel view; a thread navigate asked for in the primary view is that
// Thread view while Clack shows it (beside its channel), until the window
// moves on. A route update that comes before the thread's leaves it be.
func TestNavigateViewState(t *testing.T) {
	h := newHarness(t)
	var got []string
	view := func(channel, thread string) {
		t.Helper()
		var v string
		h.jsInto(`T.setView(`+jsQuote(channel)+`, `+jsQuote(thread)+`)
			const s = T.redux.getStore().getState()
			const p = s.view?.['v2::main::' + s.window?.main?.activeTab + '::primary']
			return (s.window?.main?.activeTab ?? '-') + ' ' + (p ? p.viewType + ':' + (p.params?.threadId ?? p.id) : '-')`, &v)
		got = append(got, v)
	}
	view("C0GENERAL1", "")
	h.js(`await T.redux.dispatchThunk('navigate', { id: 'thread', viewType: 'Thread', params: { threadId: 'C0GENERAL1-1700000002.000200' }, options: { forceRenderInPrimary: true } }); return null`)
	view("C0GENERAL1", "")                  // the old route, before the thread's
	view("C0GENERAL1", "1700000002.000200") // the thread navigate opened
	view("C0GENERAL1", "")                  // the window moved on
	view("C0GENERAL1", "1700000002.000200") // the same thread opened some other way
	h.js(`await T.redux.dispatchThunk('navigate', { id: 'thread', viewType: 'Thread', params: { threadId: 'C0GENERAL1-1700000003.000300' } }); return null`)
	view("C0GENERAL1", "1700000003.000300") // no forceRenderInPrimary: beside the channel
	want := []string{
		"home Channel:C0GENERAL1",
		"home Channel:C0GENERAL1",
		"home Thread:C0GENERAL1-1700000002.000200",
		"home Channel:C0GENERAL1",
		"home Channel:C0GENERAL1",
		"home Channel:C0GENERAL1",
	}
	if !slices.Equal(got, want) {
		t.Errorf("views %q, want %q", got, want)
	}
}

// A thunk Go registers once the runtime runs (blockkit's sendActions, when
// the plugins started first) gets its JS creator then: a plugin's
// dispatch finds it, though JS rejects a thunk it doesn't know at once.
func TestThunkRegisteredLater(t *testing.T) {
	h := newHarness(t)
	h.js(`return null`) // the runtime runs, its natives defined
	h.tt.RegisterThunk("lateThunkForTest", func(ctx context.Context, a []json.RawMessage) (any, error) { return "late", nil })
	var got string
	h.jsInto(`return await T.redux.dispatchThunk('lateThunkForTest', {})`, &got)
	if got != "late" {
		t.Errorf("the late thunk answered %q", got)
	}
}
