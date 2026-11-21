package views

// An edit that brings in what a plugin rewrites, in the views drawn from
// viewMsg (Saved, the Pins tab, Threads, Unreads, Activity). Slack runs a
// messages slice patch inside the render, so an edit that brings a word
// Censorship masks into a message it had left alone draws masked: the
// word never shows. viewMsg drew core's copy of the edit until JS's came,
// the word in clear for a JS round trip (the reviews). Under plugins that
// rewrite text the view of the revision before now stands while JS
// answers, at most taut.EditHold; a patch that rewrites no text
// (ShowRealUser's sender) draws the edit at once. Synthetic data only.

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// maskSliceJS masks "job" in the text and rich text of the messages
// slice, as Slick Censorship's patchSlice does.
const maskSliceJS = `(() => {
const TautPlugin = globalThis.TautPlugin
class MaskSlice extends TautPlugin {
  static id = 'MaskSlice'
  static pluginName = 'Mask slice (test)'
  static description = 'test: masks a word in the messages slice, as Censorship does'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const fix = (t) => (typeof t === 'string' ? t.replace(/job/g, '***') : t)
    const els = (list) => (Array.isArray(list) ? list.map((e) => (e && e.type === 'text' ? { ...e, text: fix(e.text) } : e && e.elements ? { ...e, elements: els(e.elements) } : e)) : list)
    const mask = (ts, m) => (m && typeof m.text === 'string' && m.text.includes('job') ? { ...m, text: fix(m.text), blocks: els(m.blocks) } : m)
    const { patchSlice, mapEntries } = this.api.redux
    patchSlice('messages', (ch, bucket) => (bucket && typeof bucket === 'object' ? mapEntries(bucket, mask) : bucket))
  }
  stop() {}
}
return MaskSlice
})()`

// rewriteEnv is the Pins tab of fakeslack's pins channel under plugin js,
// holding msgs, its frames drawn by e.
func rewriteEnv(t *testing.T, js string, msgs ...slack.Message) (app *core.App, tt *taut.Taut, rt *jsrt.Runtime, v *ChannelPins, e *vaEnv) {
	t.Helper()
	app, fs := pfCore(t)
	tt, rt, _ = lmPluginWith(t, app, []byte(js), func(tt *taut.Taut) bool { return tt.SlicePatched("messages") }, "the plugin's patch")
	h := pfHostFor(t, app, fs, false)
	h.cfHost.filesHost.vaHost.slickHost.tt = tt
	v = NewChannelPins(h, fakeslack.PinsChannelID) // any of the views
	app.S.SetLatest(fakeslack.PinsChannelID, msgs, false)
	e = &vaEnv{size: image.Pt(1100, 900)}
	e.draw = func(gtx layout.Context) {
		slackc.Pump()
		v.Layout(gtx)
	}
	return app, tt, rt, v, e
}

// holdJS keeps the plugins' JS busy until the returned release.
func holdJS(rt *jsrt.Runtime) (release func()) {
	started, done := make(chan struct{}), make(chan struct{})
	go func() {
		_ = rt.Do(func(*jsrt.Ctx) error {
			close(started)
			<-done
			return nil
		})
	}()
	<-started
	released := false
	return func() {
		if !released {
			released = true
			close(done)
		}
	}
}

// editIn edits m in ch to text over the socket and returns core's copy of
// the edit once the store has it.
func editIn(t *testing.T, app *core.App, e *vaEnv, ch string, m slack.Message, text string) core.Message {
	t.Helper()
	edited := m
	edited.Text = text
	edited.Edited = &slack.Edited{User: m.User, TS: fmt.Sprintf("%d.000001", time.Now().Unix())}
	raw, _ := json.Marshal(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": ch,
		"ts": edited.Edited.TS, "message": edited, "previous_message": m})
	cj, _ := json.Marshal(ch)
	app.Inject(slack.Event{Type: "message", Subtype: "message_changed", Channel: cj, Raw: raw})
	var now core.Message
	e.until(t, "the edit in core's copy", func() bool {
		sm, ok := app.S.Find(ch, m.TS)
		if ok && sm.Text == text {
			now = app.MessageFromSlack(ch, &sm)
			return true
		}
		return false
	})
	return now
}

func TestViewsEditBringingAMaskedWordNeverShowsIt(t *testing.T) {
	ch := fakeslack.PinsChannelID
	masked := slack.Message{Type: "message", TS: "1762800200.000100", User: "U0FAKE00001", Text: "the job board is up"}
	left := slack.Message{Type: "message", TS: "1762800300.000100", User: "U0FAKE00002", Text: "lunch at noon?"}
	app, tt, rt, v, e := rewriteEnv(t, maskSliceJS, masked, left)
	srcMasked, srcLeft := app.MessageFromSlack(ch, &masked), app.MessageFromSlack(ch, &left)
	e.until(t, "the masked message's view", func() bool {
		v.frame()
		return v.viewMsg(&srcMasked).Text == "the *** board is up"
	})
	e.until(t, "the other's view", func() bool {
		v.frame()
		_, _, ready, ok := tt.MessageViewState(ch, left.TS, false, 0)
		return ok && ready && v.viewMsg(&srcLeft).Text == "lunch at noon?"
	})
	if !tt.TextRewrites() {
		t.Fatal("the masked view drew, and taut doesn't know the plugins rewrite text")
	}
	// the plugins' JS busy from here; an edit brings the word in
	release := holdJS(rt)
	defer release()
	now := editIn(t, app, e, ch, left, "a job at noon?")
	for dl := time.Now().Add(80 * time.Millisecond); time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
		v.frame()
		if got := v.viewMsg(&now).Text; strings.Contains(got, "job") {
			t.Fatalf("with the plugins' JS busy, the edit drew %q: the word the plugin masks, in clear", got)
		}
	}
	release()
	e.until(t, "the edit masked", func() bool {
		v.frame()
		got := v.viewMsg(&now).Text
		if strings.Contains(got, "job") {
			t.Fatalf("as JS answered for the edit, it drew %q: the word the plugin masks, in clear", got)
		}
		return got == "a *** at noon?"
	})
}

// Under a patch that rewrites no text (a relay bot's post credited to its
// member, ShowRealUser's), an edit of another message draws core's copy
// at once, JS busy or not: nothing it rewrites can be in it.
func TestViewsEditUnderASenderPatchDrawsAtOnce(t *testing.T) {
	ch := fakeslack.PinsChannelID
	relay := slack.Message{Type: "message", Subtype: "bot_message", TS: "1762800100.000100", BotID: "B0RELAY0001", Username: "relay",
		Text: "a synthetic relayed post"}
	plain := slack.Message{Type: "message", TS: "1762800300.000100", User: "U0FAKE00002", Text: "lunch at noon?"}
	app, tt, rt, v, e := rewriteEnv(t, relayAuthorJS, relay, plain)
	srcRelay, srcPlain := app.MessageFromSlack(ch, &relay), app.MessageFromSlack(ch, &plain)
	e.until(t, "the plugins' view crediting the member", func() bool {
		v.frame()
		return v.viewMsg(&srcRelay).User == "U0FAKE00001"
	})
	e.until(t, "the other's view", func() bool {
		v.frame()
		_, _, ready, ok := tt.MessageViewState(ch, plain.TS, false, 0)
		return ok && ready && v.viewMsg(&srcPlain).Text == "lunch at noon?"
	})
	if tt.TextRewrites() {
		t.Fatal("a patch that rewrote only a sender counts as rewriting text")
	}
	release := holdJS(rt)
	defer release()
	now := editIn(t, app, e, ch, plain, "lunch at one?")
	v.frame()
	if got := v.viewMsg(&now).Text; got != "lunch at one?" {
		t.Errorf("the first read of the edit drew %q, not the edit: a pass-through patch held it", got)
	}
}
