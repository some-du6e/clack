package richtext

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

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

// embedCards counts the message_embed cards in bs, nested ones too.
func embedCards(bs []Block) int {
	n := 0
	for i := range bs {
		b := &bs[i]
		if b.Kind == AttachmentBlock && b.Att != nil {
			if b.Att.Embed {
				n++
			}
			n += embedCards(b.Att.Text)
		}
		n += embedCards(b.Children)
	}
	return n
}

// Embeds inside embeds are bounded: a chain of messages each embedding
// the one before three times draws at most maxEmbedCards cards, and a
// message embedding its own ts draws its card once, without itself in
// it; both convert in bounded time.
func TestMessageEmbedBounded(t *testing.T) {
	embed := func(ts string) string { return `{"type":"message_embed","channel":"C1","ts":"` + ts + `"}` }
	r := embedRes{msgs: map[string]slack.Message{}}
	r.msgs["C1/1.0"] = slack.Message{TS: "1.0", Channel: "C1", User: "U1", Text: "the first"}
	for i := 2; i <= 12; i++ {
		prev := strconv.Itoa(i-1) + ".0"
		ts := strconv.Itoa(i) + ".0"
		r.msgs["C1/"+ts] = slack.Message{TS: ts, Channel: "C1", User: "U1", Text: "message " + ts,
			Blocks: json.RawMessage(`[` + embed(prev) + `,` + embed(prev) + `,` + embed(prev) + `]`)}
	}
	r.msgs["C1/5.5"] = slack.Message{TS: "5.5", Channel: "C1", User: "U1", Text: "self", Blocks: json.RawMessage(`[` + embed("5.5") + `]`)}
	conv := func(blocks string) Doc {
		t.Helper()
		var d Doc
		done := make(chan struct{})
		go func() { d = embedDoc(t, r, blocks); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: no conversion after 10s", blocks)
		}
		return d
	}
	if n := embedCards(conv(`[` + embed("12.0") + `]`).Blocks); n == 0 || n > maxEmbedCards {
		t.Errorf("a chain embedding three times a level: %d cards, want 1 to %d", n, maxEmbedCards)
	}
	if n := embedCards(conv(`[` + embed("5.5") + `]`).Blocks); n != 1 {
		t.Errorf("a message embedding itself: %d cards, want its own once", n)
	}
}
