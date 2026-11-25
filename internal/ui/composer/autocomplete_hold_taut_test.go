package composer

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"
)

// slowEmojiPlugin patches autocompleteEmoji as HMojis does, answering a
// while after the original (__slowEmojiMs, 400 ms: HMojis waits on its
// image fetches) with a hidden row of its own, named for the query, first.
// ":zzhid" matches no emoji of the composer's; ":many" answers 500 rows;
// ":canon" a row whose emojiBestNameMatchCanonical isn't the name shown.
const slowEmojiPlugin = `(() => {
const TautPlugin = globalThis.TautPlugin
class SlowEmoji extends TautPlugin {
  static id = 'SlowEmoji'
  static pluginName = 'Slow emoji (test)'
  static description = 'test: answers the emoji autocomplete late, as HMojis does'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    this.api.redux.patchThunk('autocompleteEmoji', (original) => (...args) => {
      const a = args[0] || {}
      const q = String(a.query || '').replace(/^:/, '')
      const row = (name, canon) => ({ id: 'E' + name, name, emojiBestNameMatch: name, emojiBestNameMatchCanonical: canon || name,
        item: { name, updated: 1, url: 'https://emoji.example/' + name + '.png', id: 'E' + name, keywords: [] }, hmojiHidden: true })
      const finish = (rows) => new Promise((resolve) => setTimeout(() => {
        rows = Array.isArray(rows) ? rows : []
        if (q === 'many') return resolve(Array.from({ length: 500 }, (_, i) => row('many_' + i)))
        if (q === 'canon') return resolve([row('shown_name', 'real_name'), ...rows])
        resolve([row('hidden_' + q), ...rows])
      }, globalThis.__slowEmojiMs ?? 400))
      const called = original(...args)
      if (called && typeof called.then === 'function') return called.then(finish)
      if (typeof called === 'function') return async (...rest) => finish(await called(...rest))
      return finish([])
    })
  }
  stop() {}
}
return SlowEmoji
})()`

// neverEmojiPlugin patches autocompleteEmoji with an answer that never
// comes, counting what it was asked, the signals it was passed and the
// aborts it heard (__never).
const neverEmojiPlugin = `(() => {
const TautPlugin = globalThis.TautPlugin
class NeverEmoji extends TautPlugin {
  static id = 'NeverEmoji'
  static pluginName = 'Never emoji (test)'
  static description = 'test: an emoji autocomplete answer that never comes'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const n = (globalThis.__never = { asked: 0, signals: 0, aborted: 0 })
    this.api.redux.patchThunk('autocompleteEmoji', (original) => (...args) => {
      const a = args[0] || {}
      n.asked++
      if (a.signal && typeof a.signal.addEventListener === 'function') {
        n.signals++
        a.signal.addEventListener('abort', () => { n.aborted++ })
      }
      return () => new Promise(() => {})
    })
  }
  stop() {}
}
return NeverEmoji
})()`

// acDriver types into an autocomplete and runs what lands for the UI
// goroutine (the composer's later queue), as the composer's frames do.
type acDriver struct {
	t      *testing.T
	ac     *Autocomplete
	mu     sync.Mutex
	q      []func()
	doc    *Doc
	picked []acItem
}

func newACDriver(t *testing.T, ac *Autocomplete) *acDriver {
	d := &acDriver{t: t, ac: ac, doc: acDocOf("")}
	ac.later = func(f func()) { d.mu.Lock(); d.q = append(d.q, f); d.mu.Unlock() }
	ac.Live = func() (*Doc, int) { return d.doc, d.doc.Len() }
	ac.OnAccept = func(it acItem, start, end int) { d.picked = append(d.picked, it) }
	return d
}

// typeText is a keystroke: the text becomes s, and the frame's Update.
func (d *acDriver) typeText(s string) {
	d.doc = acDocOf(s)
	d.ac.Update(d.doc, d.doc.Len())
}

// pump is a frame: what landed runs, then Update.
func (d *acDriver) pump() {
	d.mu.Lock()
	fs := d.q
	d.q = nil
	d.mu.Unlock()
	for _, f := range fs {
		f()
	}
	d.ac.Update(d.doc, d.doc.Len())
}

func (d *acDriver) names() []string {
	items, _ := d.ac.Items()
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Name
	}
	return out
}

// until pumps frames until cond holds for the rows' names.
func (d *acDriver) until(what string, cond func([]string) bool) []string {
	d.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		d.pump()
		if n := d.names(); cond(n) {
			return n
		}
	}
	d.t.Fatalf("%s: rows %v", what, d.names())
	return nil
}

// A plugin's ":" rows change the list once a keystroke, as Slack's
// TextyAutocomplete's do: while the plugin's answer for the next query is
// out the list keeps the rows it shows (Slack keeps state.results until
// its search answers), so a hidden row doesn't drop out for the
// composer's and come back; a Tab while that answer is out takes the
// answer's row 0 (Slack's deferredSelection); and Enter on a name only the
// plugin has waits for its row instead of sending. Before, the composer's
// rows showed at each keystroke, Tab took the composer's row 0, and Enter
// on a hidden-only name went to the editor.
func TestTautAutocompleteEmojiHoldsRows(t *testing.T) {
	e := startTautCode(t, []byte(slowEmojiPlugin), nil, nil)
	waitFor(t, 10*time.Second, "autocompleteEmoji patched", func() bool { return currentTaut().ThunkPatched("autocompleteEmoji") })
	ac := newAutocompleteFor(t, e.app)
	d := newACDriver(t, ac)
	d.typeText(":ca")
	d.until("the plugin's row for :ca", func(n []string) bool { return len(n) > 1 && n[0] == "hidden_ca" })

	d.typeText(":cat") // its answer is 400 ms out
	if n := d.names(); len(n) == 0 || n[0] != "hidden_ca" {
		t.Errorf("while the plugin's answer for :cat is out the list shows %v; want the rows it showed (hidden_ca first), not the composer's", n)
	}
	if !ac.Key(key.NameTab, 0) {
		t.Fatal("the list didn't take Tab")
	}
	if len(d.picked) != 1 || d.picked[0].ID != "hidden_cat" {
		t.Errorf("Tab while the answer was out picked %+v; want the answer's row 0, hidden_cat", d.picked)
	}

	// a name only the plugin has: Enter waits for its row
	d.typeText("hello")
	d.typeText(":zzhid")
	d.picked = nil
	if !ac.Key(key.NameReturn, 0) {
		t.Error("Enter on :zzhid went to the editor, which sends; want it to wait for the plugin's row")
	}
	if len(d.picked) != 1 || d.picked[0].ID != "hidden_zzhid" {
		t.Errorf("Enter on :zzhid picked %+v; want the plugin's hidden_zzhid", d.picked)
	}
}

// A plugin's ":" rows are taken as Slack takes them: a row inserts its
// emojiBestNameMatchCanonical (the name shown can be another), and an
// answer shows at most 200 rows (the list polled every row each frame).
func TestTautAutocompleteEmojiRowsAsSlack(t *testing.T) {
	e := startTautCode(t, []byte(slowEmojiPlugin), nil, nil)
	waitFor(t, 10*time.Second, "autocompleteEmoji patched", func() bool { return currentTaut().ThunkPatched("autocompleteEmoji") })
	ac := newAutocompleteFor(t, e.app)
	d := newACDriver(t, ac)
	d.typeText(":canon")
	d.until("the plugin's row for :canon", func(n []string) bool { return len(n) > 0 && n[0] == "shown_name" })
	if items, _ := ac.Items(); items[0].Atom == nil || items[0].Atom.ID != "real_name" || items[0].Atom.Text != ":real_name:" {
		t.Errorf("the row shown as shown_name inserts %+v; want its emojiBestNameMatchCanonical, :real_name:", items[0].Atom)
	}
	d.typeText(":many")
	d.until("the plugin's rows for :many", func(n []string) bool { return len(n) > 0 && n[0] == "many_0" })
	if items, _ := ac.Items(); len(items) != 200 {
		t.Errorf("the plugin's 500 rows show as %d; want 200", len(items))
	}
}

// A plugin's ":" answer that never comes doesn't stay out for good: each
// query's dispatch ends the last's and Dismiss the open one's, as Slack's
// next search aborts the last (searchAbortController.abort()), and the
// plugin hears the signal it was passed abort; one left alone ends at its
// deadline (Slack's search timeout). Before, each query typed left its
// dispatch waiting for the runtime's life: a goroutine, its rows, a thunk
// table entry and the JS frames awaiting it.
func TestTautAutocompleteEmojiAbortsAndTimesOut(t *testing.T) {
	defer setPluginTimeout(time.Second)()
	e := startTautCode(t, []byte(neverEmojiPlugin), nil, nil)
	waitFor(t, 10*time.Second, "autocompleteEmoji patched", func() bool { return currentTaut().ThunkPatched("autocompleteEmoji") })
	rt, err := e.tt.JS()
	if err != nil {
		t.Fatal(err)
	}
	ac := newAutocompleteFor(t, e.app)
	d := newACDriver(t, ac)
	for i, q := range []string{":aa", ":ab", ":ac"} {
		d.typeText(q)
		// the plugin asked before the next key (a dispatch ended before its
		// JS ran is never asked)
		waitJS(t, rt, q+" asked", fmt.Sprintf("globalThis.__never && __never.asked === %d", i+1))
	}
	waitJS(t, rt, "each asked with a signal, the first two aborted", `__never.signals === 3 && __never.aborted === 2`)
	ac.Dismiss()
	waitJS(t, rt, "the third aborted at Dismiss", `__never.aborted === 3`)
	waitFor(t, 5*time.Second, "no dispatch waiting", func() bool { return inFlightDispatches(e.tt) == 0 })

	d.typeText(":smi") // left alone: its deadline ends it
	waitJS(t, rt, "the fourth aborted at its deadline", `__never.asked === 4 && __never.aborted === 4`)
	waitFor(t, 5*time.Second, "no dispatch waiting after the deadline", func() bool { return inFlightDispatches(e.tt) == 0 })
	d.until("the composer's rows for :smi", func(n []string) bool { return len(n) > 0 })
}
