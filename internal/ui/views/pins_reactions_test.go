package views

// The Pins tab draws a pinned message's reactions from core under a
// plugin patching the messages slice: viewMsg hands the tab the plugins'
// view of the message with core's reactions in it, as Slack's ReactionBar
// reads them from the reactions store by the message's reaction key,
// never from the messages slice. The view, converted from the mirror a
// moment after core's change, held them back (edit-latency's reactions
// round; the reviews: the Pins tab is the views' one surface drawing
// reactions from viewMsg). Synthetic data only.

import (
	"image"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

func TestPinsTabReactionsAreCoresUnderSlicePatch(t *testing.T) {
	app, fs := pfCore(t)
	tt, _, _ := lmPlugin(t, app, nil, nil, true) // patchMessageText: the messages slice patched
	h := pfHostFor(t, app, fs, false)
	h.cfHost.filesHost.vaHost.slickHost.tt = tt
	v := NewChannelPins(h, fakeslack.PinsChannelID)
	e := &vaEnv{size: image.Pt(1100, 900)}
	e.draw = func(gtx layout.Context) {
		slackc.Pump()
		v.Layout(gtx)
	}
	e.until(t, "the pins", func() bool { return v.Loaded() && len(v.Items()) == 4 })
	target := fakeslack.PinnedTS[0]
	var msg *core.Message
	for _, it := range v.Items() {
		if it.Msg != nil && it.Msg.TS == target {
			msg = it.Msg
		}
	}
	if msg == nil {
		t.Fatalf("no pinned message %s", target)
	}
	// the channel's history holds it, as with the channel open beside the
	// tab (the messages slice is the loaded history's: a pin fetched by id
	// alone has no view, and draws core's message)
	app.S.SetLatest(fakeslack.PinsChannelID, []slack.Message{{Type: "message", TS: target, User: msg.User, Text: msg.Text}}, false)
	e.until(t, "the plugins' view of the pinned message", func() bool {
		_, ok := tt.MessageView(fakeslack.PinsChannelID, target)
		return ok
	})
	// core's copy carries someone's reaction the plugins' view hasn't got
	m := *msg
	m.Reactions = append(append([]core.Reaction(nil), msg.Reactions...), core.Reaction{Name: "tada", Count: 1, Users: []string{"U0FAKE00003"}})
	v.frame() // a frame's lookups afresh
	got := v.viewMsg(&m)
	if got == &m {
		t.Fatal("the tab took core's message, not the plugins' view: the test checks nothing")
	}
	for _, r := range got.Reactions {
		if r.Name == "tada" && r.Count == 1 {
			return
		}
	}
	t.Errorf("the Pins tab's view of the message has %+v: core's reaction is missing", got.Reactions)
}
