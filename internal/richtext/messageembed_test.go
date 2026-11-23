package richtext

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only.

// embedRes is a resolver that holds messages (the store's FindHeld).
type embedRes struct {
	fakeRes
	msgs map[string]slack.Message
}

func (r embedRes) FindHeld(ch, ts string) (slack.Message, bool) {
	m, ok := r.msgs[ch+"/"+ts]
	return m, ok
}

func embedDoc(t *testing.T, r Resolver, blocks string) Doc {
	t.Helper()
	return FromMessage(r, msg(t, `{"ts":"9.0","text":"embed text","blocks":`+blocks+`}`))
}

// MessageEmbed draws the held message at channel/ts as a message unfurl
// (getMessageAttachmentProps): its blocks, author, channel and thread
// context; nothing for a message the client doesn't hold.
func TestMessageEmbed(t *testing.T) {
	r := embedRes{msgs: map[string]slack.Message{
		"C1/1.0": {TS: "1.0", Channel: "C1", User: "U1", Text: "fallback of the original",
			Blocks: []byte(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"the original"}]}]},` +
				`{"type":"related_automation_decorator"},{"type":"mcp_app","app_id":"A1"}]`)},
		"C1/2.0": {TS: "2.0", Channel: "C1", ThreadTS: "1.0", BotProfile: &slack.BotProfile{Name: "Deploy Bot"}, Text: "a reply"},
		"C1/3.0": {TS: "3.0", Channel: "C1", User: "U1", Attachments: []slack.Attachment{{Blocks: []byte(`[{"type":"section","text":{"type":"mrkdwn","text":"from its attachment"}}]`)}}},
	}}
	d := embedDoc(t, r, `[{"type":"message_embed","team_id":"T1","channel":"C1","ts":"1.0"}]`)
	if len(d.Blocks) != 1 || d.Blocks[0].Kind != AttachmentBlock || d.Blocks[0].Att == nil {
		t.Fatalf("blocks %s", dump(d.Blocks))
	}
	a := d.Blocks[0].Att
	if !a.MsgCard || !a.Embed || !d.Blocks[0].Kit || a.AuthorID != "U1" || a.AuthorName != "alice" || a.ShareChannel != "general" || a.MsgTS != "1.0" {
		t.Errorf("card %+v", a)
	}
	text := PlainText(Doc{Blocks: a.Text})
	if !strings.Contains(text, "the original") || !strings.Contains(text, "This content is only available in the original conversation.") {
		t.Errorf("card text %q: the message's blocks, its mcp_app as Slack's notice", text)
	}
	if got := spansText(a.Context, nil); !strings.Contains(got, "Posted in") {
		t.Errorf("context %q, want Posted in #general", got)
	}
	// a bot's reply in a thread: the bot's name, "From a thread in"
	d = embedDoc(t, r, `[{"type":"message_embed","channel":"C1","ts":"2.0"}]`)
	if len(d.Blocks) != 1 || d.Blocks[0].Att == nil {
		t.Fatalf("reply embed: %s", dump(d.Blocks))
	}
	if a := d.Blocks[0].Att; a.AuthorName != "Deploy Bot" || !strings.Contains(spansText(a.Context, nil), "From a thread in") {
		t.Errorf("reply embed: author %q, context %q", a.AuthorName, spansText(a.Context, nil))
	}
	// no blocks and no text: its first attachment's blocks
	d = embedDoc(t, r, `[{"type":"message_embed","channel":"C1","ts":"3.0"}]`)
	if len(d.Blocks) != 1 || d.Blocks[0].Att == nil || !strings.Contains(PlainText(Doc{Blocks: d.Blocks[0].Att.Text}), "from its attachment") {
		t.Errorf("attachment-blocks embed: %s", dump(d.Blocks))
	}
	// a message the client doesn't hold, or a mistyped key: nothing, and
	// the other blocks draw
	for _, b := range []string{
		`{"type":"message_embed","channel":"C1","ts":"404.0"}`,
		`{"type":"message_embed","channel":{"id":"C1"},"ts":["1.0"]}`,
		`{"type":"message_embed"}`,
	} {
		d = embedDoc(t, r, `[{"type":"section","text":{"type":"mrkdwn","text":"before"}},`+b+`]`)
		if len(d.Blocks) != 1 || PlainText(d) != "before" {
			t.Errorf("%s: blocks %s", b, dump(d.Blocks))
		}
	}
	// a resolver without messages (notifications, previews) draws none
	d = embedDoc(t, R, `[{"type":"message_embed","channel":"C1","ts":"1.0"}]`)
	if len(d.Blocks) != 0 {
		t.Errorf("no finder: %s", dump(d.Blocks))
	}
}
