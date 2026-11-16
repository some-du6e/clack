package composer

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// wrapSend wraps the thread composer's prepareAndSendMessage, as a send
// check does (NativeAtChannel, Message Logger's): the wrapper calls back
// the props it was rendered with.
const wrapSend = `(() => {
const TautPlugin = globalThis.TautPlugin
class WrapSend extends TautPlugin {
  static id = 'WrapSend'
  static pluginName = 'Wrap send (test)'
  static description = 'test: a send check around the thread composer'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const R = globalThis.React
    this.api.patchComponent('InputContainer', (Original) => (props) =>
      R.createElement(Original, { ...props, prepareAndSendMessage: (o) => props.prepareAndSendMessage(o) }))
  }
  stop() {}
}
return WrapSend
})()`

// A thread can have two composers on screen: the thread pane's and its
// card's in the Threads view (or the pane's in the main and the split
// view). Both keyed their InputContainer by the thread, so they shared
// one plugin root: its tree was the one rendered last, with the other
// composer's prepareAndSendMessage, and a reply sent from the first went
// to the second, which had nothing to send. Each is drawn in a pane of its
// own (LayoutIn: its conversation list's, "threads:…" for a card) and
// sends its own reply.
func TestComposersForOneThreadSendTheirOwn(t *testing.T) {
	e := startTautCode(t, []byte(wrapSend), nil, nil)
	e.patched("InputContainer")
	const thread = "1700000000.000100"
	clearDraft(tChGen, thread)
	pane, card := New(e.h, tChGen, thread), New(e.h, tChGen, thread)
	t.Cleanup(func() { pane.Close(); card.Close(); clearDraft(tChGen, thread) })
	ops, r := new(op.Ops), new(input.Router)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Min: image.Pt(600, 0), Max: image.Pt(600, 400)}, Now: time.Now(), Source: r.Source()}
		slackc.Frame(gtx)
		pane.LayoutIn(gtx, tChGen+"/"+thread)
		card.LayoutIn(gtx, "threads:"+tChGen+"/"+thread)
		r.Frame(ops)
	}
	e.until(10*time.Second, "the plugin's prepareAndSendMessage in both composers", frame, func() bool {
		return pane.sendJS.ID != 0 && card.sendJS.ID != 0
	})
	for i := 0; i < 5; i++ {
		frame()
	}
	if pane.sendJS.ID == card.sendJS.ID {
		t.Errorf("both composers send through one wrapper (JS function %d): one root for both", pane.sendJS.ID)
	}
	posted := func(text string) func() bool {
		return func() bool {
			for _, f := range e.slack.called("chat.postMessage") {
				if f.Get("thread_ts") == thread && (strings.Contains(f.Get("text"), text) || strings.Contains(f.Get("blocks"), text)) {
					return true
				}
			}
			return false
		}
	}
	e.why = func() string {
		var out []string
		for _, f := range e.slack.called("chat.postMessage") {
			out = append(out, f.Get("thread_ts")+" "+f.Get("text"))
		}
		return "posts: " + strings.Join(out, "; ") + "; trees: " + slackc.KeyedTrees(pane.key())
	}
	pane.ed.InsertText("sent from the thread pane")
	pane.submit()
	e.until(10*time.Second, "the thread pane's reply sent", frame, posted("sent from the thread pane"))
	card.ed.InsertText("sent from the Threads view")
	card.submit()
	e.until(10*time.Second, "the Threads view's reply sent", frame, posted("sent from the Threads view"))
}
