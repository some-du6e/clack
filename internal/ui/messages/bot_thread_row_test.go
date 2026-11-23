package messages

// Karim, 2026-11-23 (α.146 on Windows), Hack Club's help bots in a thread
// pane: "theres a gap there on help bots", and "the pfps are blurry".
//
// The gap: Heidi the Helper's replies are a section, two buttons and a
// context block, the section's mrkdwn opening with "\n" (read 2026-11-23,
// shapes only). Slack's thread pane (Izie's screenshot) draws the text's
// first line right under the sender's line, as for anyone's message:
// Clack drew an 8px empty line first.
//
// The blur: an avatar's picture follows the device pixels it fills
// (Slack's getMemberProfileImage: the image_* for the box, its 2x for
// the screen), so a 36px avatar on a 4x capture (the inspect tool's,
// "1918x1080 dp at 1x" drawn 4 px a dp) draws the 192px picture, not the
// 72px one scaled up. A bot's own icons stop at 72: that one, never a
// smaller one. Synthetic ids, names, text and URLs.

import (
	"encoding/json"
	"image"
	"strconv"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	helpCh    = "C0HELPDSK1"
	helpRoot  = "1700200000.000100"
	helpHeidi = "1700200010.000100"
	helpBob   = "1700200020.000100"
	helpRelay = "1700200030.000100" // a user-less bot's, with icons
	heidiID   = "U0HEIDI001"
	heidiBot  = "B0HEIDI001"
)

func heidiPic(px int) string {
	return "https://avatars.invalid/heidi-" + strconv.Itoa(px) + ".png"
}

// helpThread is a help channel's thread: a person's question, Heidi's
// reply, a person's reply and a relay bot's.
func helpThread(t *testing.T) coreHost {
	app, _ := newTestApp(t)
	s := app.S
	s.PutUsers([]slack.User{{ID: heidiID, Name: "heidi", RealName: "Heidi the Helper", IsBot: true,
		Profile: slack.Profile{DisplayName: "Heidi the Helper", BotID: heidiBot,
			Image24: heidiPic(24), Image32: heidiPic(32), Image48: heidiPic(48), Image72: heidiPic(72), Image192: heidiPic(192), Image512: heidiPic(512)}}})
	blocks, _ := json.Marshal([]any{
		map[string]any{"type": "section", "block_id": "s1", "text": map[string]any{"type": "mrkdwn",
			"text": "\nhi alice! seems like it's your first time here, welcome to the help channel! someone will be along soon"}},
		map[string]any{"type": "actions", "block_id": "a1", "elements": []any{
			map[string]any{"type": "button", "action_id": "b1", "text": map[string]any{"type": "plain_text", "text": "Resolved"}},
			map[string]any{"type": "button", "action_id": "b2", "text": map[string]any{"type": "plain_text", "text": "Not yet"}}}},
		map[string]any{"type": "context", "block_id": "c1", "elements": []any{map[string]any{"type": "mrkdwn", "text": "a synthetic footnote"}}},
	})
	s.PutConv(slack.Conversation{ID: helpCh, Name: "help", IsChannel: true, IsMember: true, NumMembers: 3})
	root := slack.Message{Type: "message", TS: helpRoot, User: "UALICE", Text: "a synthetic question", ThreadTS: helpRoot, ReplyCount: 3}
	s.SetLatest(helpCh, []slack.Message{root}, false)
	s.SetThread(helpCh, helpRoot, []slack.Message{
		root,
		{Type: "message", TS: helpHeidi, User: heidiID, BotID: heidiBot, Username: "heidi the helper", ThreadTS: helpRoot,
			Text: " hi alice! seems like it's your first time here", Blocks: blocks},
		{Type: "message", TS: helpBob, User: "UBOB", Text: "a synthetic answer", ThreadTS: helpRoot},
		{Type: "message", TS: helpRelay, Subtype: "bot_message", BotID: "B0RELAY001", Username: "relay", ThreadTS: helpRoot,
			Text: "a relayed synthetic line", Icons: &slack.Icons{Image36: "https://avatars.invalid/relay-36.png",
				Image48: "https://avatars.invalid/relay-48.png", Image72: "https://avatars.invalid/relay-72.png"}},
	})
	return coreHost{app: app, th: theme.New(true)}
}

// threadFrameAt lays tv out at scale px per dp.
func threadFrameAt(tv *ThreadView, scale float32) func() {
	ops := new(op.Ops)
	return func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
			Constraints: layout.Exact(image.Pt(int(420*scale), int(900*scale))), Now: time.Now()}
		tv.Layout(gtx)
	}
}

// A bot's thread reply lays out as anyone's: its body right under the
// sender's 22px line, its section text's first line at the body's top.
func TestBotThreadRowLikeSlack(t *testing.T) {
	h := helpThread(t)
	tv := NewThreadView(h, helpCh, helpRoot)
	f := threadFrameAt(tv, 1)
	until(t, 5*time.Second, "the replies", f, func() bool {
		hr, br := tv.s.rows[helpHeidi], tv.s.rows[helpBob]
		return hr != nil && br != nil && hr.rv != nil && br.rv != nil && hr.rv.FirstTextTop() >= 0 && br.rv.FirstTextTop() >= 0
	})
	for i := 0; i < 5; i++ {
		f()
	}
	hr, br := tv.s.rows[helpHeidi], tv.s.rows[helpBob]
	if hr.badge == "" {
		t.Fatalf("Heidi's row has no APP badge: the case tests nothing")
	}
	if got, want := hr.rv.FirstTextTop(), br.rv.FirstTextTop(); got != want {
		t.Errorf("Heidi's text starts %dpx into the body, a person's %dpx; Slack draws both first lines right under the sender's line", got, want)
	}
	if hr.bodyRect.Min.Y != br.bodyRect.Min.Y {
		t.Errorf("Heidi's body starts at %d, a person's at %d: the sender's line is 22px for both", hr.bodyRect.Min.Y, br.bodyRect.Min.Y)
	}
}

// The avatar picture for the device pixels: a person's and a bot user's
// 72px one up to 72 px, then 192px; a bot's icons at their largest.
func TestAvatarPictureForScaleLikeSlack(t *testing.T) {
	for _, c := range []struct {
		scale float32
		heidi string
		relay string
	}{
		{1, heidiPic(72), "https://avatars.invalid/relay-72.png"},
		{2, heidiPic(72), "https://avatars.invalid/relay-72.png"},
		{3, heidiPic(192), "https://avatars.invalid/relay-72.png"},
		{4, heidiPic(192), "https://avatars.invalid/relay-72.png"}, // the inspect tool's capture
	} {
		h := helpThread(t)
		tv := NewThreadView(h, helpCh, helpRoot)
		f := threadFrameAt(tv, c.scale)
		until(t, 5*time.Second, "the replies", f, func() bool {
			hr, rr := tv.s.rows[helpHeidi], tv.s.rows[helpRelay]
			return hr != nil && rr != nil && hr.avDrawn != "" && rr.avDrawn != ""
		})
		if got := tv.s.rows[helpHeidi].avDrawn; got != c.heidi {
			t.Errorf("at %vx Heidi's 36px avatar draws %q, want %q", c.scale, got, c.heidi)
		}
		if got := tv.s.rows[helpRelay].avDrawn; got != c.relay {
			t.Errorf("at %vx the relay bot's avatar draws %q, want its largest icon %q", c.scale, got, c.relay)
		}
		// the root's sender has no picture past 48px: that one at every scale
		alice, _ := h.app.User("UALICE")
		if r := tv.s.rows[helpRoot]; r != nil && r.avDrawn != "" && r.avDrawn != alice.Avatar {
			t.Errorf("at %vx the root's sender draws %q, want their only picture %q", c.scale, r.avDrawn, alice.Avatar)
		}
	}
}
