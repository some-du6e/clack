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
// beside its channel; one Slack's forceRenderInPrimary draws alone goes
// through UIHooks.DeepLinkPrimary, which marks it as that primary view.
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
	links, primary := slices.Clone(h.ui.links), slices.Clone(h.ui.primary)
	h.ui.mu.Unlock()
	want := []string{
		"slack://channel?id=C0GENERAL1",
		"slack://user?id=UALICE0001",
	}
	if !slices.Equal(links, want) {
		t.Errorf("deep links %q, want %q", links, want)
	}
	if want := []string{"slack://channel?id=C0GENERAL1&thread_ts=1700000002.000200"}; !slices.Equal(primary, want) {
		t.Errorf("primary thread links %q, want %q", primary, want)
	}
}

// Slack's view state as plugins read it (ChatTabs' currentTab): the main
// window's active tab and its primary view. The route Clack shows is a
// Channel view; its thread is the primary Thread view only while the
// route says so (the bit Go keeps on the history entry navigate's
// forceRenderInPrimary marked, UIHooks.DeepLinkPrimary): set again for
// the route already shown (the thread open beside its channel), and back
// on Back, without anything left over in JS.
func TestNavigateViewState(t *testing.T) {
	h := newHarness(t)
	var got []string
	view := func(channel, thread string, primary bool) {
		t.Helper()
		var v string
		p := "false"
		if primary {
			p = "true"
		}
		h.jsInto(`T.setView(`+jsQuote(channel)+`, `+jsQuote(thread)+`, `+p+`)
			const s = T.redux.getStore().getState()
			const p = s.view?.['v2::main::' + s.window?.main?.activeTab + '::primary']
			return (s.window?.main?.activeTab ?? '-') + ' ' + (p ? p.viewType + ':' + (p.params?.threadId ?? p.id) : '-')`, &v)
		got = append(got, v)
	}
	view("C0GENERAL1", "", false)
	view("C0GENERAL1", "1700000002.000200", false) // the thread opened by hand, beside its channel
	view("C0GENERAL1", "1700000002.000200", true)  // navigate's primary thread, the route already shown
	view("C0GENERAL1", "", false)                  // the window moved on
	view("C0GENERAL1", "1700000002.000200", true)  // Back to that entry
	view("C0GENERAL1", "1700000002.000200", false) // the same thread, opened some other way
	view("", "", false)                            // no conversation (a page)
	view("C0GENERAL1", "", true)                   // no thread: the channel, whatever the bit
	want := []string{
		"home Channel:C0GENERAL1",
		"home Channel:C0GENERAL1",
		"home Thread:C0GENERAL1-1700000002.000200",
		"home Channel:C0GENERAL1",
		"home Thread:C0GENERAL1-1700000002.000200",
		"home Channel:C0GENERAL1",
		"home -",
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
