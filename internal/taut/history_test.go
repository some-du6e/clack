package taut

import (
	"slices"
	"testing"
	"time"
)

// TestHistoryHiddenFollowsPatches: a patch that filters channelHistory,
// as Slick's HideResolved filters help channels' tickets, reaches the
// UI's lists through HistoryHidden: the timestamps it took out of the key
// a list shows, told whenever the store changes. The patch here reads
// viewUiState too, as HideResolved does to keep a linked message shown: a
// jump's highlight (SetViewHighlight, Slack's {[channel]: {highlightTs},
// ts}) brings the message back with no change to channelHistory, and a
// navigation without one takes it out again. JS watches just the key the
// list asked for; unpatched, nothing is hidden.
func TestHistoryHiddenFollowsPatches(t *testing.T) {
	h := newHarness(t)
	const gone, kept = "1700000002.000200", "1700000001.000100"
	if got, ok := h.tt.HistoryHidden(chGen); got != nil || !ok {
		t.Fatalf("unpatched: %v (%v), want nothing hidden", got, ok)
	}
	h.js(`globalThis.__unpatchHistory = T.redux.patchState((st) => !st.channelHistory ? st : ({
		...st,
		channelHistory: T.redux.mapEntries(st.channelHistory, (key, hist) => {
			if (key !== '` + chGen + `' || !Array.isArray(hist?.slices)) return hist
			const linked = st.viewUiState?.['` + chGen + `']?.['` + chGen + `']?.highlightTs
			return { ...hist, slices: hist.slices.map((s) => ({ ...s, timestamps: s.timestamps.filter((ts) => ts !== '` + gone + `' || ts === linked) })) }
		}),
	}))
	return null`)
	hidden := func(what string, want ...string) {
		t.Helper()
		eventually(t, 5*time.Second, what, func() bool {
			got, ok := h.tt.HistoryHidden(chGen)
			if !ok || got.Len() != len(want) {
				return false
			}
			for _, ts := range want {
				if !got.Has(ts) {
					return false
				}
			}
			return true
		})
	}
	hidden("the message the patch took out", gone)
	if got, _ := h.tt.HistoryHidden(chGen); got.Has(kept) {
		t.Errorf("%s hidden: the patch kept it", kept)
	}
	if keys := h.tt.historyKeys(); !slices.Equal(keys, []string{chGen}) {
		t.Errorf("JS watches %v, want just the list's %s", keys, chGen)
	}

	// a jump to it: the patch keeps the highlighted message, and the
	// change is viewUiState's alone
	h.tt.SetViewHighlight(chGen, gone)
	hidden("nothing hidden with the message highlighted")
	var ui map[string]any
	h.jsInto(`return T.redux.getRawState().viewUiState?.['`+chGen+`'] ?? null`, &ui)
	if hl, _ := ui[chGen].(map[string]any); hl["highlightTs"] != gone || ui["ts"] != gone {
		t.Errorf("viewUiState[%s] = %v, want {%s: {highlightTs: %s}, ts: %s}", chGen, ui, chGen, gone, gone)
	}
	// a navigation with no message to start at drops the highlight
	h.tt.SetViewHighlight(chGen, "")
	hidden("the message hidden again after a plain navigation", gone)
	h.jsInto(`return T.redux.getRawState().viewUiState?.['`+chGen+`'] ?? null`, &ui)
	if hl, _ := ui[chGen].(map[string]any); hl == nil || hl["highlightTs"] != nil || ui["ts"] != nil {
		t.Errorf("viewUiState[%s] after the plain navigation = %v, want {%s: {}}", chGen, ui, chGen)
	}

	// the patch gone: nothing hidden
	h.js(`globalThis.__unpatchHistory(); return null`)
	eventually(t, 5*time.Second, "nothing hidden unpatched", func() bool {
		got, ok := h.tt.HistoryHidden(chGen)
		return ok && got == nil
	})
}
