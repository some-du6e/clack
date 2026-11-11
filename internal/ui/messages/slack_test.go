package messages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

func testSlackMsg() core.Message {
	return core.Message{TS: "1700000000.000100", Channel: "C1", BotID: "B1", Name: "relay", Avatar: "https://a/48.png",
		Username: "relay", Icons: &core.SenderIcons{Image48: "https://a/48.png"},
		Subtype: "bot_message", IsBot: true, Text: "hi *there*", ThreadTS: "1700000000.000100", ReplyCount: 2,
		ReplyUsers: []string{"U1", "U2"}, LatestReply: "1700000005.000100", Edited: true, EditedTS: "1700000001.000000",
		Reactions: []core.Reaction{{Name: "tada", Count: 2, Users: []string{"U1", "U2"}}},
		Doc: richtext.Doc{Blocks: []richtext.Block{
			{Kind: richtext.Paragraph, Spans: []richtext.Span{{Text: "hi "}, {Text: "there", Style: richtext.Bold}, {Kind: richtext.User, ID: "U2", Text: "@bob"}}},
			{Kind: richtext.Context, Spans: []richtext.Span{{Text: "Sent using", Style: richtext.Bold}, {Text: " "}, {Kind: richtext.User, ID: "U9", Text: "@bot"}}},
			{Kind: richtext.FileBlock, File: &richtext.File{ID: "F1", Name: "a.png", Mime: "image/png"}},
			{Kind: richtext.AttachmentBlock, Att: &richtext.Attachment{AuthorName: "relay", AuthorIcon: "https://a/i.png", IsShare: true, ShareChannelID: "C2", ShareChannel: "general"}},
		}},
	}
}

// roundTrip passes props through JSON as they come back from JS.
func roundTrip(t *testing.T, p slackc.Props) slackc.Props {
	t.Helper()
	b, err := json.Marshal(jsonable(p))
	if err != nil {
		t.Fatal(err)
	}
	out, err := slackc.DecodeProps(b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSlackMessageShape(t *testing.T) {
	m := testSlackMsg()
	p := roundTrip(t, slackMessage(&m))
	for k, want := range map[string]string{"type": "message", "ts": m.TS, "channel": "C1", "bot_id": "B1", "subtype": "bot_message",
		"username": "relay", "text": m.Text, "thread_ts": m.TS, "latest_reply": m.LatestReply} {
		if got := p.StrOr(k, "<missing>"); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if p.IntOr("reply_count", 0) != 2 || len(p.List("reply_users")) != 2 {
		t.Errorf("thread fields: %v", p)
	}
	if p.Map("edited").StrOr("ts", "") != m.EditedTS {
		t.Errorf("edited: %v", p["edited"])
	}
	if iconURL(p.Map("icons")) != m.Avatar || p.Map("bot_profile").StrOr("id", "") != "B1" {
		t.Errorf("icons/bot_profile: %v %v", p["icons"], p["bot_profile"])
	}
	re := slackc.Props(asMap(p.List("reactions")[0]))
	if re.StrOr("name", "") != "tada" || re.IntOr("count", 0) != 2 || len(re.List("users")) != 2 {
		t.Errorf("reactions: %v", p["reactions"])
	}
	blocks := p.List("blocks")
	if len(blocks) != 2 {
		t.Fatalf("blocks: %v", blocks)
	}
	b0 := slackc.Props(asMap(blocks[0]))
	if b0.StrOr("type", "") != "rich_text" || b0.StrOr("block_id", "") != "clack-0" {
		t.Errorf("block 0: %v", b0)
	}
	sec := slackc.Props(asMap(b0.List("elements")[0]))
	els := sec.List("elements")
	if sec.StrOr("type", "") != "rich_text_section" || len(els) != 3 ||
		!slackc.Props(asMap(els[1])).Map("style").Bool("bold") || slackc.Props(asMap(els[2])).StrOr("user_id", "") != "U2" {
		t.Errorf("section: %v", sec)
	}
	ctx := slackc.Props(asMap(blocks[1]))
	if txt := slackc.Props(asMap(ctx.List("elements")[0])).StrOr("text", ""); ctx.StrOr("type", "") != "context" || txt != "*Sent using* <@U9>" {
		t.Errorf("context block: %v (text %q)", ctx, txt)
	}
	if f := slackc.Props(asMap(p.List("files")[0])); f.StrOr("id", "") != "F1" || f.StrOr("mimetype", "") != "image/png" {
		t.Errorf("files: %v", p["files"])
	}
	if a := slackc.Props(asMap(p.List("attachments")[0])); a.StrOr("author_name", "") != "relay" || a.StrOr("channel_id", "") != "C2" || !a.Bool("is_share") {
		t.Errorf("attachments: %v", p["attachments"])
	}
	if reactionKey(&m) != "message-1700000000.000100-C1" {
		t.Errorf("reactionKey %q", reactionKey(&m))
	}
}

// Our own msg coming back unchanged must not change what is drawn.
func TestSlackMessageIdempotent(t *testing.T) {
	m := testSlackMsg()
	back := applySlackMessage(nil, &m, roundTrip(t, slackMessage(&m)))
	if back.User != m.User || back.BotID != m.BotID || back.Name != m.Name || back.Avatar != m.Avatar || back.IsBot != m.IsBot || back.Subtype != m.Subtype {
		t.Errorf("sender changed: %+v", back)
	}
	if len(back.Doc.Blocks) != len(m.Doc.Blocks) || &back.Doc.Blocks[0] != &m.Doc.Blocks[0] {
		t.Errorf("blocks not kept as-is")
	}
}

// ShowRealUser: relay bot → the real user, attachment authors rewritten.
func TestSlackMessageRelayOverride(t *testing.T) {
	m := testSlackMsg()
	p := roundTrip(t, slackMessage(&m))
	p["user"] = "U7"
	for _, k := range []string{"bot_id", "username", "icons", "bot_profile", "subtype"} {
		delete(p, k)
	}
	att := slackc.Props(asMap(p.List("attachments")[0]))
	att["author_name"], att["author_icon"] = "Real Person", "https://a/real.png"
	got := applySlackMessage(nil, &m, p)
	if got.User != "U7" || got.BotID != "" || got.Name != "" || got.Avatar != "" || got.IsBot || got.Subtype != "" {
		t.Errorf("sender not overridden: %+v", got)
	}
	a := got.Doc.Blocks[len(got.Doc.Blocks)-1].Att
	if a.AuthorName != "Real Person" || a.AuthorIcon != "https://a/real.png" || m.Doc.Blocks[3].Att.AuthorName != "relay" {
		t.Errorf("attachment author: %+v (source %+v)", a, m.Doc.Blocks[3].Att)
	}
	if got.TS != m.TS || got.Channel != m.Channel || len(got.Reactions) != 1 {
		t.Errorf("identity changed: %+v", got)
	}
}

// ShowSendingBot hideMcpFooter: the last block dropped.
func TestDocBlocksDropAndReorder(t *testing.T) {
	m := testSlackMsg()
	p := roundTrip(t, slackMessage(&m))
	blocks := p.List("blocks")
	p["blocks"] = blocks[:1]
	got := docBlocks(nil, m.Doc.Blocks, m.Blocks, p)
	if len(got) != 3 || got[0].Kind != richtext.Paragraph || got[1].Kind != richtext.FileBlock || got[2].Kind != richtext.AttachmentBlock {
		t.Errorf("dropped footer: %+v", got)
	}
	p["blocks"] = []any{blocks[1], blocks[0]}
	got = docBlocks(nil, m.Doc.Blocks, m.Blocks, p)
	if len(got) != 4 || got[0].Kind != richtext.Context || got[1].Kind != richtext.Paragraph {
		t.Errorf("reordered: %+v", got)
	}
	delete(p, "blocks")
	if got := docBlocks(nil, m.Doc.Blocks, m.Blocks, p); len(got) != 4 || &got[0] != &m.Doc.Blocks[0] {
		t.Errorf("no blocks key should keep the doc: %+v", got)
	}
}

func TestSpansMrkdwn(t *testing.T) {
	got := spansMrkdwn([]richtext.Span{{Text: "a<b"}, {Kind: richtext.Link, URL: "https://x", Text: "x"}, {Kind: richtext.Channel, ID: "C1"},
		{Kind: richtext.Break}, {Kind: richtext.Emoji, ID: "tada"}, {Text: "c", Style: richtext.Italic | richtext.Code}})
	if want := "a&lt;b<https://x|x><#C1>\n:tada:_`c`_"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// OwlMode sets ReactionBar's currentUser: "reacted" follows that user.
func TestReactionBarCurrentUser(t *testing.T) {
	e := testRowEntry("1700000000.000100")
	r := newRow(&e)
	r.rc = &rowCtx{h: nilHost{}, selfID: "U1"}
	if !r.pills[0].reacted() || r.pills[1].reacted() {
		t.Fatal("viewer's own reactions")
	}
	r.reactAs = "U3"
	if r.pills[0].reacted() || !r.pills[1].reacted() {
		t.Error("currentUser override ignored")
	}
	if p := r.pills[1].buttonProps(); p.StrOr("className", "") != "c-button-unstyled c-reaction c-reaction--reacted" || p.StrOr("data-qa", "") != "reactji" {
		t.Errorf("UnstyledButton props %v", p)
	}
}

// ShowRealUser (Slick, src/plugins/ShowRealUser asForwardedBy) rewrites a
// forwarded copy of a relay bot's post when it can look the post's sender
// up by the attachment's channel_id and ts, and it matches the reply's ts
// with ===. Slack's attachment carries the shared message's ts as a
// string; Clack sent the whole seconds as a number, so the plugin never
// fired on forwards ("forwarded copies carry a numeric ts").
func TestSharedAttachmentTSIsSlacks(t *testing.T) {
	r := store.New("")
	share := slack.Message{Type: "message", TS: "1794369300.000100", User: "U1",
		Attachments: []slack.Attachment{{IsShare: true, ChannelID: "C0CFT52NEGM", TS: json.RawMessage(`"1794369255.504199"`),
			AuthorLink: "https://hackclub.slack.com/services/B08G06U6SJG", AuthorName: "at-channel", Text: "hi"}}}
	m := core.Message{TS: share.TS, Channel: "C1", User: "U1", Doc: richtext.FromMessage(r, &share)}
	p := roundTrip(t, slackMessage(&m))
	att := slackc.Props(asMap(p.List("attachments")[0]))
	if ts, ok := att["ts"].(string); !ok || ts != "1794369255.504199" {
		t.Errorf("forwarded attachment ts %#v, want the string \"1794369255.504199\"", att["ts"])
	}
	if att.StrOr("channel_id", "") != "C0CFT52NEGM" || !strings.Contains(att.StrOr("author_link", ""), "/services/B08G06U6SJG") {
		t.Errorf("forwarded attachment %v: ShowRealUser needs channel_id and the /services/ author link", att)
	}
}
