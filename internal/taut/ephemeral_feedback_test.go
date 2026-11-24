package taut

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// refusePlugin refuses a send the way Karim's Block Kit Send does when
// what follows its prefix isn't Block Kit: its prepareAndSendMessage patch
// hands Slack's dispatch a thunk that posts Slackbot's note through
// addEphemeralFeedback, which it waited for at start, and then throws, so
// the draft stays.
const refusePlugin = `
class RefuseSend extends TautPlugin {
  static id = 'RefuseSend'
  static pluginName = 'Refuse Send'
  static description = 'refuses bk: sends with a note, like Block Kit Send'
  static defaultConfig = { enabled: false }
  start() {
    this.api.redux.waitForThunkCreator('addEphemeralFeedback').then((create) => {
      this.feedback = create
      globalThis.__refuseFeedback = true
    })
    this.api.redux.patchThunk('prepareAndSendMessage', (original) => (args, ...rest) => {
      if (!String(args.text ?? '').startsWith('bk:')) return original(args, ...rest)
      return () => {
        const reason = 'Put Block Kit JSON or a Block Kit Builder link after bk:, not ' + JSON.stringify(args.text.slice(3).trim())
        this.api.redux.getStore().dispatch(this.feedback({
          message: { channel: args.channelId, thread_ts: args.replyToTs || undefined, slackbot_feels: 'sad_surprise', text: reason },
        }))
        throw new Error(reason)
      }
    })
  }
}
return RefuseSend`

// noteIn is the local Slackbot note in ch's timeline with text, if any.
func noteIn(h *harness, ch, text string) (slack.Message, bool) {
	if tl := h.app.S.Timeline(ch); tl != nil {
		for _, m := range tl.Msgs {
			if m.IsEphemeral && m.Text == text {
				return m, true
			}
		}
	}
	return slack.Message{}, false
}

// A plugin that won't send a message says why with Slack's
// addEphemeralFeedback, as Block Kit Send does: Clack defines the thunk,
// the refused send's native never runs and the patch's error comes back
// (the composer keeps the text), and Slackbot's "Only visible to you"
// note is in the conversation, in core and in state.messages with what
// the plugin put on it.
func TestRefusedSendPostsSlackbotNote(t *testing.T) {
	h := newHarness(t)
	h.install(pluginJS(refusePlugin), true)
	eventually(t, 10*time.Second, "the plugin's prepareAndSendMessage patch", func() bool { return h.tt.ThunkPatched("prepareAndSendMessage") })
	ready := func() bool { return string(h.js(`return !!globalThis.__refuseFeedback`)) == "true" }
	deadline := time.Now().Add(5 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("waitForThunkCreator('addEphemeralFeedback') never resolved: Clack defines no addEphemeralFeedback thunk, so the plugin can't say why it refused a send")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// the composer's send of an ordinary message goes through untouched
	var natives atomic.Int32
	native := func(context.Context, map[string]any) (any, error) { natives.Add(1); return nil, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := Dispatch(ctx, h.tt, "prepareAndSendMessage", map[string]any{"channelId": chGen, "text": "hello"}, native); err != nil || natives.Load() != 1 {
		t.Fatalf("an ordinary send: err %v, natives %d; want it sent", err, natives.Load())
	}

	reason := `Put Block Kit JSON or a Block Kit Builder link after bk:, not "lol"`
	_, err := Dispatch(ctx, h.tt, "prepareAndSendMessage", map[string]any{"channelId": chGen, "text": "bk: lol"}, native)
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("the refused send's error is %v, want the plugin's reason", err)
	}
	if n := natives.Load(); n != 1 {
		t.Errorf("the refused send's native ran (%d sends): the message went out", n-1)
	}
	var note slack.Message
	eventually(t, 5*time.Second, "Slackbot's note in #general", func() bool {
		var ok bool
		note, ok = noteIn(h, chGen, reason)
		return ok
	})
	if note.User != slackbot || note.ThreadTS != "" {
		t.Errorf("the note is %+v, want Slackbot's, in the channel", note)
	}
	if !h.app.Interactive().Ephemeral(chGen, note.TS) {
		t.Error("the note isn't an ephemeral (Only visible to you)")
	}
	var seen struct {
		User  string `json:"user"`
		Feels string `json:"slackbot_feels"`
		Eph   bool   `json:"is_ephemeral"`
		Text  string `json:"text"`
	}
	eventually(t, 5*time.Second, "the note in state.messages", func() bool {
		h.jsInto(`const m = T.redux.getRawState().messages['`+chGen+`']?.['`+note.TS+`']
			return m ? { user: m.user, slackbot_feels: m.slackbot_feels, is_ephemeral: m.is_ephemeral, text: m.text } : {}`, &seen)
		return seen.Text != ""
	})
	if seen.User != slackbot || seen.Feels != "sad_surprise" || !seen.Eph || seen.Text != reason {
		t.Errorf("plugins see the note as %+v", seen)
	}

	// a second refusal takes the first note's place (addOrFlashEphemeralBotMsg)
	if _, err := Dispatch(ctx, h.tt, "prepareAndSendMessage", map[string]any{"channelId": chGen, "text": "bk: again"}, native); err == nil {
		t.Fatal("the second refusal sent")
	}
	again := `Put Block Kit JSON or a Block Kit Builder link after bk:, not "again"`
	var second slack.Message
	eventually(t, 5*time.Second, "the second refusal's note", func() bool {
		var ok bool
		second, ok = noteIn(h, chGen, again)
		return ok
	})
	count := 0
	for _, m := range h.app.S.Timeline(chGen).Msgs {
		if m.IsEphemeral {
			count++
		}
	}
	if count != 1 || second.TS != note.TS {
		t.Errorf("after two refusals: %d notes, the second at %s (the first at %s); want the first note in place with the second's text", count, second.TS, note.TS)
	}
}

// addEphemeralFeedback as Slack's thunk reads its args (client-boot
// w0M8): temporary unless temporary is exactly false, its type's last
// note replaced in place unless ignoreExistingMessage, and inputDelta
// back in the conversation's composer through the input proxy; it
// resolves undefined.
func TestAddEphemeralFeedbackThunk(t *testing.T) {
	h := newHarness(t)
	var got struct {
		Defined  bool            `json:"defined"`
		Results  []any           `json:"results"`
		ThrewNil bool            `json:"threwNil"`
		Input    json.RawMessage `json:"input"`
	}
	h.jsInto(`
		const create = await api.within(api.redux.waitForThunkCreator('addEphemeralFeedback'), 3000, '')
		if (!create) return { defined: false }
		T.registerInput({ viewContext: 'MESSAGE_PANE', channelId: '`+chGen+`' }, true)
		const d = (o) => T.redux.getStore().dispatch(create(o))
		const results = []
		results.push(await d({ message: { channel: '`+chGen+`', text: 'one' } }))
		results.push(await d({ message: { channel: '`+chGen+`', text: 'two' }, temporary: 0 }))
		results.push(await d({ message: { channel: '`+chGen+`', text: 'beside' }, ignoreExistingMessage: 1 }))
		return { defined: true, results }`, &got)
	if !got.Defined {
		t.Fatal("Clack defines no addEphemeralFeedback thunk")
	}
	shown := func() string {
		var texts []string
		for _, m := range h.app.S.Timeline(chGen).Msgs {
			if m.IsEphemeral && h.app.Interactive().Ephemeral(chGen, m.TS) {
				texts = append(texts, m.Text)
			}
		}
		return strings.Join(texts, ",")
	}
	// "two" (temporary: 0 is temporary) took "one"'s place, and "beside"
	// was added next to it
	if s := shown(); s != "two,beside" {
		t.Errorf("the notes shown are %q, want two,beside", s)
	}
	h.jsInto(`
		const create = await api.redux.waitForThunkCreator('addEphemeralFeedback')
		const d = (o) => T.redux.getStore().dispatch(create(o))
		const results = [await d({ message: { channel: '`+chGen+`', text: 'lasts' }, temporary: false, inputDelta: { ops: [{ insert: 'bk: {}\n' }] } })]
		let threwNil = false
		try { await d({}) } catch { threwNil = true }
		return { defined: true, results: [...results], threwNil }`, &got)
	for i, r := range got.Results {
		if r != nil {
			t.Errorf("dispatch %d resolved %v, want undefined", i, r)
		}
	}
	if !got.ThrewNil {
		t.Error("a call without a message resolved; Slack's throws reading its channel")
	}
	// the lasting note removed the type's notes: it stays alone
	if s := shown(); s != "lasts" {
		t.Errorf("after the lasting note the notes shown are %q, want lasts alone", s)
	}
	h.ui.mu.Lock()
	in := h.ui.inputs["MESSAGE_PANE|"+chGen]
	h.ui.mu.Unlock()
	if !strings.Contains(in, `bk: {}`) {
		t.Errorf("the composer's contents are %q, want the note's inputDelta", in)
	}
}
