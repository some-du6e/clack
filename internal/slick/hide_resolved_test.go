package slick

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// TestHideResolvedLikeSlack: Slick's HideResolved, built from Slick's
// source, in a help channel (#ysws-help: its default "help, *-help" names
// it). As in Slack:
//
//   - its channelHistory patch takes the resolved tickets out of the
//     channel's history: a root with ✅ and no 🤔 (🤔 wins), and a reply
//     sent to the channel with its root; an open ticket, one with both and
//     a message with neither stay, and #general (no help channel) loses
//     nothing. Clack's list hides what the patch took out
//     (Taut.HistoryHidden);
//   - its ViewHeaderSearch patch draws its toggle before the header's
//     search button (ViewHeaderSearch, Clack's native, after it):
//     "Show 3 resolved" with the count; clicking it shows them ("Hide
//     resolved messages", nothing hidden), clicking again hides them; no
//     toggle in #general's header;
//   - a ✅ landing on a message (reaction_added) hides it too, the count
//     following;
//   - a jump to a resolved ticket keeps it shown while the view highlights
//     it (viewUiState's highlightTs), and a plain navigation hides it
//     again;
//   - stopped, nothing is hidden.
func TestHideResolvedLikeSlack(t *testing.T) {
	dir := pluginsDir(t)
	h := newHarness(t)
	const chHelp = "C0YSWSHELP"
	const (
		tOpen  = "1700000101.000100" // 🤔: open
		tDone  = "1700000102.000200" // ✅: resolved
		tBoth  = "1700000103.000300" // ✅ and 🤔: open
		tPlain = "1700000104.000400" // neither
		tDone2 = "1700000105.000500" // ✅, with a reply sent to the channel
		tCast  = "1700000106.000600" // that reply: resolved with its root
	)
	check := slack.Reaction{Name: "white_check_mark", Count: 1, Users: []string{bobID}}
	open := slack.Reaction{Name: "thinking_face", Count: 1, Users: []string{bobID}}
	h.app.S.PutConv(slack.Conversation{ID: chHelp, Name: "ysws-help", IsChannel: true, IsMember: true, NumMembers: 40})
	h.app.S.SetLatest(chHelp, []slack.Message{
		{Type: "message", TS: tOpen, User: aliceID, Text: "how do I submit?", Reactions: []slack.Reaction{open}},
		{Type: "message", TS: tDone, User: bobID, Text: "my build fails", Reactions: []slack.Reaction{check}},
		{Type: "message", TS: tBoth, User: aliceID, Text: "it broke again", Reactions: []slack.Reaction{check, open}},
		{Type: "message", TS: tPlain, User: bobID, Text: "thanks all"},
		{Type: "message", TS: tDone2, User: aliceID, Text: "where is the form?", Reactions: []slack.Reaction{check}, ThreadTS: tDone2, ReplyCount: 1},
		{Type: "message", TS: tCast, User: bobID, Text: "in the canvas", ThreadTS: tDone2, Subtype: "thread_broadcast"},
	}, false)
	// a ✅ in #general, which is no help channel
	h.app.S.Mutate(chGen, "1700000003.000300", func(m *slack.Message) { m.Reactions = append(m.Reactions, check) })

	h.importFile(filepath.Join(dir, "HideResolved.js"))
	if err := h.s.SetEnabled(h.ctx(), "HideResolved", true); err != nil {
		t.Fatal(err)
	}
	if info := h.plugin("HideResolved"); !info.Running || info.StartError != "" {
		t.Fatalf("HideResolved didn't start: %+v", info)
	}
	hidden := func(what, ch string, want ...string) {
		t.Helper()
		var got []string
		eventually(t, 5*time.Second, what, func() bool {
			has, n, ok := historyHidden(h, ch)
			got = got[:0]
			for _, ts := range []string{tOpen, tDone, tBoth, tPlain, tDone2, tCast, "1700000001.000100", "1700000002.000200", "1700000003.000300"} {
				if has(ts) {
					got = append(got, ts)
				}
			}
			return ok && n == len(want) && slices.Equal(got, want)
		})
	}
	hidden("the resolved tickets hidden", chHelp, tDone, tDone2, tCast)
	hidden("nothing hidden in #general", chGen)

	// the header's search button, and the toggle the plugin puts before it
	help := map[string]any{"channel": map[string]any{"id": chHelp, "name": "ysws-help", "is_channel": true}}
	h.render(1, "ViewHeaderSearch", help)
	shows := func(tip, count string, revealed bool) func([]any) bool {
		return func(nodes []any) bool {
			tg := hideToggleOf(nodes)
			return tg.btn != nil && tg.tip == tip && tg.count == count && tg.revealed == revealed && tg.searchAfter()
		}
	}
	tg := hideToggleOf(h.waitTree(t, 1, `the toggle saying "Show 3 resolved" before the search button`, shows("Show 3 resolved", "3", false)))
	// aria-pressed={!revealed}: the attribute React writes for a boolean
	// aria-*, its string
	if tg.pressed != "true" || tg.label != "Show 3 resolved" {
		t.Errorf("the toggle: aria-pressed %#v, aria-label %q; want \"true\" and its tip", tg.pressed, tg.label)
	}
	click := func(tg hideToggle) {
		t.Helper()
		k, _ := tg.btn["k"].(float64)
		if k == 0 {
			t.Fatal("the toggle has no node id to click")
		}
		h.tt.Bridge().Dispatch(int64(k), []byte(`{"type":"click","button":0,"buttons":0,"clientX":0,"clientY":0}`))
	}
	click(tg)
	hidden("nothing hidden once they're shown", chHelp)
	tg = hideToggleOf(h.waitTree(t, 1, `the toggle saying "Hide resolved messages"`, shows("Hide resolved messages", "", true)))
	click(tg)
	hidden("the resolved tickets hidden again", chHelp, tDone, tDone2, tCast)
	h.waitTree(t, 1, `the toggle saying "Show 3 resolved" again`, shows("Show 3 resolved", "3", false))

	// a ✅ lands on the plain message
	raw, _ := json.Marshal(map[string]any{"type": "reaction_added", "user": aliceID, "reaction": "white_check_mark", "event_ts": "1700000200.000100",
		"item": map[string]any{"type": "message", "channel": chHelp, "ts": tPlain}})
	h.app.Inject(slack.Event{Type: "reaction_added", Raw: raw})
	hidden("the message the ✅ landed on hidden", chHelp, tDone, tPlain, tDone2, tCast)
	h.waitTree(t, 1, `the toggle saying "Show 4 resolved"`, shows("Show 4 resolved", "4", false))

	// a jump to a resolved ticket: shown while the view highlights it
	setViewHighlight(h, chHelp, tDone)
	hidden("the highlighted ticket shown", chHelp, tPlain, tDone2, tCast)
	setViewHighlight(h, chHelp, "")
	hidden("hidden again after a plain navigation", chHelp, tDone, tPlain, tDone2, tCast)

	// #general's header has no toggle
	h.render(2, "ViewHeaderSearch", map[string]any{"channel": map[string]any{"id": chGen, "name": "general", "is_channel": true}})
	time.Sleep(200 * time.Millisecond) // what the store's changes would re-render
	gen := h.waitTree(t, 2, "#general's header", func([]any) bool { return true })
	if g := hideToggleOf(gen); g.btn != nil || !g.searchAfter() {
		t.Errorf("#general's header: toggle %v, the search button drawn %v; want no toggle and the button", g.btn != nil, g.searchAfter())
	}

	// stopped: nothing hidden
	if err := h.s.SetEnabled(h.ctx(), "HideResolved", false); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "nothing hidden once it stopped", func() bool {
		_, n, ok := historyHidden(h, chHelp)
		return ok && n == 0
	})
}

// hideToggle is HideResolved's toggle in a ViewHeaderSearch tree.
type hideToggle struct {
	btn        map[string]any
	tip, label string
	count      string
	pressed    any
	revealed   bool
	// pre-order positions of the toggle and of the native search button
	// (-1: not in the tree)
	toggleAt, searchAt int
}

// searchAfter reports whether the native search button is drawn, after
// the toggle when there is one.
func (tg hideToggle) searchAfter() bool { return tg.searchAt >= 0 && tg.searchAt > tg.toggleAt }

// hideToggleOf finds the toggle (button.slick-hide-resolved, data-qa
// slick-hide-resolved) in nodes: the Tooltip's tip round it, its label,
// pressed state and count, and where the native search button is.
func hideToggleOf(nodes []any) hideToggle {
	tg := hideToggle{toggleAt: -1, searchAt: -1}
	n := 0
	var walk func(ns []any, tip string)
	walk = func(ns []any, tip string) {
		for _, x := range ns {
			m, ok := x.(map[string]any)
			if !ok {
				continue
			}
			n++
			p, _ := m["p"].(map[string]any)
			in := tip
			if m["t"] == "@" && m["n"] == "Tooltip" {
				in, _ = p["tip"].(string)
			}
			if m["t"] == "@" && m["n"] == "ViewHeaderSearch" && tg.searchAt < 0 {
				tg.searchAt = n
			}
			kids, _ := m["c"].([]any)
			if c, _ := p["className"].(string); m["t"] == "button" && strings.Contains(" "+c+" ", " slick-hide-resolved ") && p["data-qa"] == "slick-hide-resolved" {
				tg.btn, tg.tip, tg.toggleAt = m, in, n
				tg.label, _ = p["aria-label"].(string)
				tg.pressed = p["aria-pressed"]
				tg.revealed = strings.Contains(" "+c+" ", " slick-hide-resolved--revealed ")
				tg.count = strings.TrimSpace(treeText(kids))
				continue
			}
			walk(kids, in)
		}
	}
	walk(nodes, "")
	return tg
}
