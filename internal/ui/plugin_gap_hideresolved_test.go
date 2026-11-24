package ui

import (
	"encoding/json"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/pluginlib"
)

// TestPluginGapHideResolved: Slick's Hide Resolved Tickets from the
// Library, in the whole client, with #plugin-bench as its help channel
// (its Help channels setting). Three messages at the channel's end: an
// open ticket (🤔), a resolved one (✅) and a plain message.
//
//   - The resolved ticket's row isn't drawn: Slack's list draws
//     channelHistory as the plugin's patch leaves it. The other two are.
//   - The header's search button gets the plugin's toggle before it, as
//     Slack's header row lays it out: the search button's component is
//     wider by the toggle, as tall as the button, and ends where it ended.
//   - Clicking the toggle (the DOM click slackc sends for it) draws the
//     resolved row again ("Hide resolved messages"); clicking again hides
//     it.
func TestPluginGapHideResolved(t *testing.T) {
	var open, done, plain string
	e := gapStartWith(t, pluginlib.Slick, "Hide Resolved Tickets", map[string]any{"channels": "plugin-bench"}, func(fs *fakeslack.Server) {
		ch := fakeslack.BenchChannelID
		open, _ = fs.PostQuiet(ch, fakeslack.SelfID, "how do I submit my project?")
		fs.ReactQuiet(ch, open, "thinking_face", fakeslack.SelfID)
		done, _ = fs.PostQuiet(ch, fakeslack.SelfID, "my build fails on deploy")
		fs.ReactQuiet(ch, done, "white_check_mark", fakeslack.SelfID)
		plain, _ = fs.PostQuiet(ch, fakeslack.SelfID, "thanks, everyone")
	})
	var placed image.Rectangle
	var button image.Point
	hdrSearchTap = func(ch string, p image.Rectangle, b image.Point) {
		if ch == fakeslack.BenchChannelID {
			placed, button = p, b
		}
	}
	t.Cleanup(func() { hdrSearchTap = nil })
	rows := func() map[string]bool {
		out := map[string]bool{}
		if c := e.a.convos[fakeslack.BenchChannelID+"/"]; c != nil {
			if l, ok := c.list.(channelList); ok {
				for _, r := range l.VisibleRows() {
					out[r.Key] = true
				}
			}
		}
		return out
	}
	e.toBottom()
	ticketRows := func() bool {
		r := rows()
		return r[open] && r[plain] && !r[done]
	}
	for deadline := time.Now().Add(20 * time.Second); !ticketRows(); {
		if time.Now().After(deadline) {
			// what the list drew, what Taut was told, and what JS sees
			ch := fakeslack.BenchChannelID
			var drawn []string
			for k := range rows() {
				drawn = append(drawn, k)
			}
			slices.Sort(drawn)
			hid, ok := e.tt.HistoryHidden(ch)
			for _, p := range e.a.Slick().Settings().Plugins {
				if p.ID == e.entry.ID {
					t.Logf("%s: enabled %v, active %v, running %v, start error %q", p.ID, p.Enabled, p.Active, p.Running, p.StartError)
				}
			}
			t.Logf("rows drawn %v; open %s, done %s, plain %s", drawn, open, done, plain)
			t.Logf("Taut.HistoryHidden(%s): %d hidden, done %v, ok %v; channelHistory patched %v", ch, hid.Len(), hid.Has(done), ok, e.tt.SlicePatched("channelHistory"))
			t.Logf("JS: %s", e.js(`(() => {
  const ch = '`+ch+`', done = '`+done+`';
  const raw = __taut.getRawState(), st = __taut.getState();
  const ts = (h) => (h && Array.isArray(h.slices) ? h.slices.flatMap((s) => (s && s.timestamps) || []) : null);
  const m = raw.messages && raw.messages[ch] && raw.messages[ch][done];
  const key = (m && m._rxn_key) || ('message-' + done + '-' + ch);
  return JSON.stringify({
    name: raw.channels && raw.channels[ch] && raw.channels[ch].name,
    raw: (ts(raw.channelHistory && raw.channelHistory[ch]) || []).slice(-5),
    view: (ts(st.channelHistory && st.channelHistory[ch]) || []).slice(-5),
    same: (raw.channelHistory && raw.channelHistory[ch]) === (st.channelHistory && st.channelHistory[ch]),
    doneMsg: !!m, msgs: raw.messages && raw.messages[ch] ? Object.keys(raw.messages[ch]).length : -1,
    rxnKey: key, rxns: raw.reactions ? raw.reactions[key] : 'no slice', vui: raw.viewUiState,
  });
})()`))
			t.Fatal("timed out after 20s waiting for the open ticket and the plain message drawn, the resolved one not")
		}
		e.frame()
		time.Sleep(time.Millisecond)
	}
	toggle := func() (k int64, tip string) {
		tree := e.treeWith("slick-hide-resolved")
		if tree == "" {
			return 0, ""
		}
		var nodes []any
		_ = json.Unmarshal([]byte(tree), &nodes)
		return hideToggleNode(nodes, "")
	}
	e.until("the toggle before the search button", 15*time.Second, func() bool {
		k, tip := toggle()
		return k != 0 && strings.HasPrefix(tip, "Show ") && strings.HasSuffix(tip, " resolved") && button.X > 0 && placed.Dx() > button.X
	})
	hidden := placed
	t.Logf("the search button %v; its component with the toggle %v", button, hidden)
	// the toggle before the button: its 28px at least and the row's 6px gap
	if w := hidden.Dx() - button.X; w < 28+6 {
		t.Errorf("the search button's component is %dpx wider than the button: no room for the toggle", w)
	}
	if hidden.Dy() != button.Y {
		t.Errorf("the component is %dpx tall, the button %dpx: the toggle isn't centred beside it", hidden.Dy(), button.Y)
	}

	click := func() {
		t.Helper()
		k, _ := toggle()
		if k == 0 {
			t.Fatal("no toggle to click")
		}
		e.tt.Bridge().Dispatch(k, []byte(`{"type":"click","button":0,"buttons":0,"clientX":0,"clientY":0}`))
	}
	click()
	e.until("the resolved row drawn once they're shown", 15*time.Second, func() bool {
		_, tip := toggle()
		r := rows()
		return tip == "Hide resolved messages" && r[open] && r[done] && r[plain]
	})
	if placed.Max.X != hidden.Max.X {
		t.Errorf("revealed, the component ends at x=%d; with the count it ended at %d: the header moved the search button", placed.Max.X, hidden.Max.X)
	}
	click()
	e.until("the resolved row hidden again", 15*time.Second, func() bool {
		_, tip := toggle()
		r := rows()
		return strings.HasPrefix(tip, "Show ") && r[open] && r[plain] && !r[done]
	})
}

// hideToggleNode is HideResolved's toggle in a tree (button
// .slick-hide-resolved): its node id, and the tip of the Tooltip round it.
func hideToggleNode(nodes []any, tip string) (int64, string) {
	for _, x := range nodes {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		p, _ := m["p"].(map[string]any)
		in := tip
		if m["t"] == "@" && m["n"] == "Tooltip" {
			in, _ = p["tip"].(string)
		}
		if c, _ := p["className"].(string); m["t"] == "button" && strings.Contains(" "+c+" ", " slick-hide-resolved ") {
			k, _ := m["k"].(float64)
			return int64(k), in
		}
		kids, _ := m["c"].([]any)
		if k, t := hideToggleNode(kids, in); k != 0 {
			return k, t
		}
	}
	return 0, ""
}
