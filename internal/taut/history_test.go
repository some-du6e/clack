package taut

import (
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
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
	ui = nil // Unmarshal into a map keeps the keys the new JSON hasn't
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

// TestChannelHistoryFollowsLoads: channelHistory as JS read it before a
// channel's history loaded follows the load with no event, as Slack's
// reducer makes the slice the load's. A help channel opened for the first
// time was read before its history came (its list asks at once), JS kept
// that read (a missing entry is cached too), and HideResolved, filtering
// what JS read, hid nothing: the mirror's sweep now sees the history JS
// holds change (historyFP), and a load the UI's model takes in
// (MessagesAdded) tells JS at once. A thread reply isn't in the history,
// and a reaction doesn't change it.
func TestChannelHistoryFollowsLoads(t *testing.T) {
	h := newHarness(t)
	const ch = "C0LOADS001"
	const ts1, ts2, ts3, reply = "1700000101.000100", "1700000102.000100", "1700000103.000100", "1700000104.000100"
	h.app.S.PutConv(slack.Conversation{ID: ch, Name: "loads", IsChannel: true, IsMember: true, NumMembers: 3})
	// a patch of channelHistory, as HideResolved's (MessagesAdded's path)
	h.js(`globalThis.__unpatchLoads = T.redux.patchState((st) => st); return null`)
	t.Cleanup(func() { h.js(`globalThis.__unpatchLoads?.(); return null`) })
	listed := func() []string {
		var ts []string
		h.jsInto(`const hist = T.redux.getRawState().channelHistory?.['`+ch+`']
		return hist ? hist.slices.flatMap((s) => s.timestamps) : null`, &ts)
		return ts
	}
	if got := listed(); got != nil {
		t.Fatalf("before any load JS lists %v, want no history", got)
	}
	msg := func(ts string) slack.Message {
		return slack.Message{Type: "message", TS: ts, User: "U0ALICE01", Text: "synthetic message " + ts}
	}
	h.app.S.SetLatest(ch, []slack.Message{msg(ts1), msg(ts2)}, false)
	eventually(t, 5*time.Second, "the first load in JS's history, with no event", func() bool {
		return slices.Equal(listed(), []string{ts1, ts2})
	})
	before := h.tt.mirror.historyFP(ch)
	withReaction := msg(ts2)
	withReaction.Reactions = []slack.Reaction{{Name: "white_check_mark", Count: 1, Users: []string{"U0ALICE01"}}}
	r := msg(reply)
	r.ThreadTS = ts1
	h.app.S.SetLatest(ch, []slack.Message{msg(ts1), withReaction, r}, false)
	if got := h.tt.mirror.historyFP(ch); got != before {
		t.Error("a reaction or a thread reply changed the history's fingerprint: neither is in it")
	}
	// a newer page the model takes in
	h.app.S.SetLatest(ch, []slack.Message{msg(ts1), withReaction, r, msg(ts3)}, false)
	h.tt.MessagesAdded(ch, []string{ts3})
	eventually(t, 5*time.Second, "the page in JS's history", func() bool {
		return slices.Equal(listed(), []string{ts1, ts2, ts3})
	})
}
