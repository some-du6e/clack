package messages

// The /remind confirmation as Slack sends it (captured 2026-11-24 in a
// self-DM: one Slackbot ephemeral, a rich_text block of an emoji and a
// text element, no attachments) draws as Slack draws it: "Only visible to
// you" over the sender line, and the body at the sender name's x, with no
// card around it. Natively and with MessageText and Blocks patched by a
// plugin that passes them on (Karim's plugins patch both). Synthetic text.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const remindChan = "C0REMIND01"

func remindEvent(ts string) []byte {
	b, _ := json.Marshal(map[string]any{"type": "message", "subtype": "bot_message", "user": "USLACKBOT", "bot_id": "B01", "is_ephemeral": true,
		"channel": remindChan, "ts": ts, "text": "I will remind you “stretch” at 3PM tomorrow.",
		"blocks": []any{map[string]any{"type": "rich_text", "block_id": "aB1", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
			map[string]any{"type": "emoji", "name": "thumbsup", "unicode": "1f44d"},
			map[string]any{"type": "text", "text": " I will remind you “stretch” at 3PM tomorrow."}}}}}}})
	return b
}

// passBlocksJS patches MessageText and Blocks, each passing its props on.
var passBlocksJS = pluginJS(`
class PassBlocks extends TautPlugin {
  static id = 'PassBlocks'
  static pluginName = 'Pass blocks on (test)'
  static description = 'test: MessageText and Blocks pass their props on'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const pass = (Original) => (props) => React.createElement(Original, { ...props })
    this.api.patchComponent('MessageText', pass)
    this.api.patchComponent('Blocks', pass)
  }
}
return PassBlocks`)

func TestRemindConfirmationFlushLikeSlack(t *testing.T) {
	for _, patched := range []bool{false, true} {
		name := "native"
		if patched {
			name = "patched"
		}
		t.Run(name, func(t *testing.T) {
			app, _, _ := shotsApp(t)
			app.S.PutConv(slack.Conversation{ID: remindChan, Name: "reminders", IsChannel: true, IsMember: true, NumMembers: 3})
			app.S.SetLatest(remindChan, []slack.Message{{Type: "message", TS: "1700000000.000100", User: "UANA00001", Text: "an earlier synthetic message"}}, false)
			if patched {
				patchedEnv(t, app, passBlocksJS, "MessageText", "Blocks")
			}
			_ = app.Interactive() // its socket tap marks the ephemeral
			ts := "1700000100.000100"
			// as the socket delivers it: the event's channel is the conversation's
			app.Inject(slack.Event{Type: "message", Subtype: "bot_message", Channel: json.RawMessage(`"` + remindChan + `"`), Raw: remindEvent(ts)})
			v := NewChannelView(coreHost{app: app, th: theme.New(false)}, remindChan)
			v.Preview = true
			frame := frameFn(v)
			until(t, 10*time.Second, "the confirmation's row drawn as an ephemeral", frame, func() bool {
				r := v.s.rows[ts]
				return r != nil && r.rv != nil && r.height > 0 && app.Interactive().Ephemeral(remindChan, ts)
			})
			frame()
			r := v.s.rows[ts]
			for _, b := range r.rv.Doc().Blocks {
				if b.Kind == richtext.AttachmentBlock {
					t.Fatalf("the body holds an attachment: %+v", b)
				}
			}
			if r.bodyRect.Min.X != r.col.X+r.nameX {
				t.Errorf("the body at x %d, the sender name at %d: Slack's body is flush with the name", r.bodyRect.Min.X, r.col.X+r.nameX)
			}
			if patched && !slackc.Patched("MessageText") {
				t.Error("MessageText isn't patched")
			}
			if !ephemeral(&v.s.rc, &r.m) {
				t.Error("no Only visible to you label")
			}
		})
	}
}
