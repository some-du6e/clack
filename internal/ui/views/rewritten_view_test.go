package views

// A view a plugin rewrites outside the text stands while the plugins' JS
// is busy: a relay bot's post that a plugin credits to the member who sent
// it (ShowRealUser's sentBy: user set; username, icons and bot_profile
// dropped; the text untouched) keeps the member as its author through a
// reaction that marks the message, in the views that draw viewMsg (Saved,
// the Pins tab, Threads, Unreads, Activity). They drew core's copy on any
// read that wasn't ready, so the relay bot's name and icon flashed back on
// every reaction for as long as JS was busy (the reviews). Synthetic data
// only.

import (
	"encoding/json"
	"image"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// relayAuthorJS credits the relay bot B0RELAY0001's posts to U0FAKE00001,
// patching the messages slice as ShowRealUser's modifyMessageObject does.
const relayAuthorJS = `(() => {
const TautPlugin = globalThis.TautPlugin
class RelayAuthor extends TautPlugin {
  static id = 'RelayAuthor'
  static pluginName = 'Relay author (test)'
  static description = 'test: credits a relay bot post to the member who sent it'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const { patchSlice, mapEntries } = this.api.redux
    const credit = (m) => (m && m.bot_id === 'B0RELAY0001' ?
      { ...m, user: 'U0FAKE00001', username: undefined, icons: undefined, bot_profile: undefined, bot_id: undefined, subtype: undefined } : m)
    patchSlice('messages', (ch, bucket) => (bucket && typeof bucket === 'object' ? mapEntries(bucket, (ts, m) => credit(m)) : bucket))
  }
  stop() {}
}
return RelayAuthor
})()`

func TestViewsKeepARewrittenAuthorWhileJSIsBusy(t *testing.T) {
	app, fs := pfCore(t)
	tt, rt, _ := lmPluginWith(t, app, []byte(relayAuthorJS), func(tt *taut.Taut) bool { return tt.SlicePatched("messages") }, "the relay author patch")
	h := pfHostFor(t, app, fs, false)
	h.cfHost.filesHost.vaHost.slickHost.tt = tt
	v := NewChannelPins(h, fakeslack.PinsChannelID) // any of the views
	ch := fakeslack.PinsChannelID
	relay := slack.Message{Type: "message", Subtype: "bot_message", TS: "1762800100.000100", BotID: "B0RELAY0001", Username: "relay",
		Text: "a synthetic relayed post"}
	app.S.SetLatest(ch, []slack.Message{relay}, false)
	src := app.MessageFromSlack(ch, &relay)
	e := &vaEnv{size: image.Pt(1100, 900)}
	e.draw = func(gtx layout.Context) {
		slackc.Pump()
		v.Layout(gtx)
	}
	e.until(t, "the plugins' view crediting the member", func() bool {
		v.frame()
		return v.viewMsg(&src).User == "U0FAKE00001"
	})

	// the plugins' JS busy from here; someone's reaction marks the message
	started, done := make(chan struct{}), make(chan struct{})
	go func() {
		_ = rt.Do(func(*jsrt.Ctx) error {
			close(started)
			<-done
			return nil
		})
	}()
	<-started
	defer close(done)
	raw, _ := json.Marshal(map[string]any{"type": "reaction_added", "user": "U0FAKE00002", "reaction": "tada",
		"item": map[string]any{"type": "message", "channel": ch, "ts": relay.TS}})
	app.Inject(slack.Event{Type: "reaction_added", Raw: raw})
	// the mirror marks it: JS's copy isn't ready while JS is busy, which
	// is the case the views must stand through
	e.until(t, "the plugins' view not ready after the reaction", func() bool {
		_, _, ready, ok := tt.MessageViewState(ch, relay.TS, false, 0)
		return ok && !ready
	})
	sm, ok := app.S.Find(ch, relay.TS)
	if !ok || len(sm.Reactions) == 0 {
		t.Fatal("the reaction didn't reach core's copy")
	}
	now := app.MessageFromSlack(ch, &sm)
	for i := 0; i < 8; i++ {
		v.frame()
		if got := v.viewMsg(&now); got.User != "U0FAKE00001" || got.Username == "relay" {
			t.Fatalf("read %d with the plugins' JS busy: the author is %q (%q), the relay bot's, not the member the plugin credits", i, got.User, got.Username)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
