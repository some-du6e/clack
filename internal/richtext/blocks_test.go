package richtext

import (
	"strings"
	"testing"
	"time"
)

func blocksMsg(t *testing.T, blocks string) *Doc {
	t.Helper()
	d := FromMessage(R, msg(t, `{"ts":"1.0","text":"fallback text","blocks":`+blocks+`}`))
	checkInvariants(t, blocks, d.Blocks)
	return &d
}

func TestRichTextInline(t *testing.T) {
	d := blocksMsg(t, `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
		{"type":"text","text":"hi "},
		{"type":"text","text":"bold","style":{"bold":true}},
		{"type":"text","text":" all","style":{"bold":true,"italic":true,"strike":true,"underline":true,"highlight":true}},
		{"type":"link","url":"https://x.test","text":"code link","style":{"code":true}},
		{"type":"link","url":"https://x.test/bare"},
		{"type":"link","url":"https://y.test","text":"plain","style":{"unlink":true}},
		{"type":"link","url":"javascript:alert(1)","text":"evil"},
		{"type":"user","user_id":"USELF"},
		{"type":"user","user_id":"U1","style":{"bold":true}},
		{"type":"channel","channel_id":"C1"},
		{"type":"channel","channel_id":"C404"},
		{"type":"usergroup","usergroup_id":"S1"},
		{"type":"broadcast","range":"channel"},
		{"type":"emoji","name":"wave","skin_tone":3},
		{"type":"emoji","name":"smile","skin_tone":5},
		{"type":"emoji","name":"grinning","unicode":"1f600"},
		{"type":"emoji","name":"mystery"},
		{"type":"date","timestamp":1700000000,"format":"{date_num}","fallback":"x"},
		{"type":"date","timestamp":1700000000,"format":"{date_num}","url":"https://d.test","fallback":"x"},
		{"type":"color","value":"#FF0000"},
		{"type":"color","value":"red"},
		{"type":"text","text":"a\n\nb"}
	]}]}]`)
	date := FormatDate(time.Unix(1700000000, 0), "{date_num}")
	want := []Block{para(
		tx("hi "), st("bold", Bold), st(" all", Bold|Italic|Strike|Underline|Highlight),
		with(lk("code link", "https://x.test"), Code), lk("https://x.test/bare", "https://x.test/bare"),
		tx("plain"), tx("evil"),
		user("USELF", "@me"), with(user("U1", "@alice"), Bold),
		chn("C1", "#general"), chn("C404", "#C404"), grp("S1", "@design"), bcast("channel"),
		em("wave::skin-tone-3"), em("smile"),
		Span{Kind: Emoji, Text: "😀", ID: "grinning"}, tx(":mystery:"),
		Span{Kind: Date, Text: date, ID: "1700000000"}, Span{Kind: Date, Text: date, ID: "1700000000", URL: "https://d.test"},
		Span{Kind: ColorSwatch, Text: "#FF0000"}, tx("red"),
		tx("a"), brk(BrPara), brk(BrNone), tx("b"),
	)}
	eq(t, "rich inline", d.Blocks, want)
	if d.Jumbo {
		t.Error("jumbo on mixed content")
	}
}

func TestRichTextStructure(t *testing.T) {
	d := blocksMsg(t, `[{"type":"rich_text","elements":[
		{"type":"rich_text_section","elements":[{"type":"text","text":"intro"}]},
		{"type":"rich_text_list","style":"bullet","indent":0,"elements":[
			{"type":"rich_text_section","elements":[{"type":"text","text":"one"}]},
			{"type":"rich_text_section","elements":[{"type":"text","text":"two","style":{"bold":true}}]}]},
		{"type":"rich_text_list","style":"bullet","indent":1,"border":1,"elements":[
			{"type":"rich_text_section","elements":[{"type":"text","text":"nested"}]}]},
		{"type":"rich_text_list","style":"ordered","indent":0,"offset":2,"elements":[
			{"type":"rich_text_section","elements":[{"type":"text","text":"third"}]}]},
		{"type":"rich_text_preformatted","border":0,"elements":[{"type":"text","text":"x := 1\ny := 2","style":{"bold":true}},{"type":"link","url":"https://x.test"}]},
		{"type":"rich_text_quote","elements":[{"type":"text","text":"wise "},{"type":"text","text":"words","style":{"italic":true}}]},
		{"type":"rich_text_section","elements":[]}
	]}]`)
	want := []Block{
		para(tx("intro")),
		{Kind: List, Items: [][]Span{{tx("one")}, {st("two", Bold)}}},
		{Kind: List, Indent: 1, Border: true, Items: [][]Span{{tx("nested")}}},
		{Kind: List, Ordered: true, Offset: 2, Items: [][]Span{{tx("third")}}},
		pre(tx("x := 1"), br(), tx("y := 2"), lk("https://x.test", "https://x.test")),
		quote(tx("wise "), st("words", Italic)),
	}
	eq(t, "rich structure", d.Blocks, want)
	eq(t, "plain", PlainText(*d), "intro\n• one\n• two\n    • nested\n3. third\nx := 1\ny := 2https://x.test\n> wise words")
}

func TestRichJumbo(t *testing.T) {
	cases := []struct {
		blocks string
		want   bool
	}{
		{`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"emoji","name":"smile"},{"type":"text","text":" "},{"type":"emoji","name":"wave"}]}]}]`, true},
		{`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"emoji","name":"smile"},{"type":"text","text":"hi"}]}]}]`, false},
		{`[{"type":"rich_text","elements":[{"type":"rich_text_quote","elements":[{"type":"emoji","name":"smile"}]}]}]`, false},
		{`[{"type":"divider"}]`, false},
	}
	for _, c := range cases {
		if d := blocksMsg(t, c.blocks); d.Jumbo != c.want {
			t.Errorf("jumbo(%s) = %v, want %v", c.blocks, d.Jumbo, c.want)
		}
	}
}

func TestBlockKit(t *testing.T) {
	d := blocksMsg(t, `[
		{"type":"header","text":{"type":"plain_text","text":"Weekly :smile: &amp; more","emoji":true}},
		{"type":"divider"},
		{"type":"section","block_id":"b1","text":{"type":"mrkdwn","text":"*Hi* <@U1>"},
		 "fields":[{"type":"mrkdwn","text":"*Owner*\nalice"},{"type":"plain_text","text":"Plain"}],
		 "accessory":{"type":"image","image_url":"https://files.slack.com/files-pri/T1-F1/a.png","alt_text":"pic"}},
		{"type":"section","block_id":"b2","text":{"type":"mrkdwn","text":"&gt; quoted\nnot"},
		 "accessory":{"type":"button","text":{"type":"plain_text","text":"Go"},"url":"https://go.test","style":"primary","action_id":"a1","value":"v1"}},
		{"type":"section","text":{"type":"plain_text","text":"menu"},"accessory":{"type":"overflow","action_id":"o1"}},
		{"type":"context","elements":[{"type":"image","image_url":"https://img.test/i.png","alt_text":"icon"},{"type":"mrkdwn","text":"by *bot*"},{"type":"plain_text","text":"ok"}]},
		{"type":"image","image_url":"https://img.test/big.png","alt_text":"big","title":{"type":"plain_text","text":"Big pic"},"image_width":640,"image_height":480},
		{"type":"image","image_url":"javascript:x","alt_text":"bad"},
		{"type":"image","slack_file":{"url":"https://files.slack.com/files-pri/T1-F2/b.png","id":"F2"},"alt_text":"sf"},
		{"type":"actions","block_id":"b3","elements":[
			{"type":"button","text":{"type":"plain_text","text":"Delete"},"style":"danger","action_id":"del","value":"x"},
			{"type":"static_select","placeholder":{"type":"plain_text","text":"Pick one"},"action_id":"sel"},
			{"type":"users_select","initial_option":{"text":{"type":"plain_text","text":"Chosen"}}},
			{"type":"datepicker","action_id":"dp"},
			{"type":"mystery_widget"}]},
		{"type":"input","label":{"type":"plain_text","text":"Name"},"element":{"type":"plain_text_input","placeholder":{"type":"plain_text","text":"Type here"}}},
		{"type":"video","title":{"type":"plain_text","text":"Demo"},"title_url":"https://v.test/w","video_url":"https://v.test/embed","thumbnail_url":"https://v.test/t.jpg","alt_text":"demo","author_name":"Someone","provider_name":"VidSite","provider_icon_url":"https://v.test/i.png","description":{"type":"plain_text","text":"A demo"}},
		{"type":"unknown_future_block","text":{"type":"mrkdwn","text":"future _text_"}},
		{"type":"unknown_with_fallback","fallback":{"type":"section","text":{"type":"mrkdwn","text":"degraded"}}},
		{"type":"markdown","text":"**md bold** and [a link](https://md.test)"}
	]`)
	want := []Block{
		{Kind: Header, Kit: true, Spans: []Span{tx("Weekly "), em("smile"), tx(" & more")}},
		{Kind: Divider, Kit: true},
		{Kind: Section, Kit: true, BlockID: "b1", Spans: []Span{st("Hi", Bold), tx(" "), user("U1", "@alice")},
			Fields:     [][]Span{{st("Owner", Bold), br(), tx("alice")}, {tx("Plain")}},
			FieldShort: []bool{true, true},
			Image:      &Image{URL: "https://files.slack.com/files-pri/T1-F1/a.png", Alt: "pic", Proxy: true}},
		{Kind: Section, Kit: true, BlockID: "b2", Children: []Block{quote(tx("quoted")), para(tx("not"))},
			Buttons: []Button{{Kind: "button", Text: "Go", URL: "https://go.test", Style: "primary", ActionID: "a1", Value: "v1", BlockID: "b2"}}},
		{Kind: Section, Kit: true, Spans: []Span{tx("menu")}, Buttons: []Button{{Kind: "overflow", Text: "⋮", ActionID: "o1"}}},
		{Kind: Context, Kit: true, Spans: []Span{{Kind: InlineImage, Text: "icon", URL: "https://img.test/i.png"}, tx(" "), tx("by "), st("bot", Bold), tx(" "), tx("ok")},
			Children: []Block{{Kind: ImageBlock, Image: &Image{URL: "https://img.test/i.png", Alt: "icon"}}, para(tx("by "), st("bot", Bold)), para(tx("ok"))}},
		{Kind: ImageBlock, Kit: true, Image: &Image{URL: "https://img.test/big.png", Alt: "big", Title: "Big pic", W: 640, H: 480}},
		{Kind: Unsupported, Kit: true, Spans: []Span{tx("This content could not be displayed.")}},
		{Kind: ImageBlock, Kit: true, Image: &Image{URL: "https://files.slack.com/files-pri/T1-F2/b.png", Alt: "sf", Proxy: true}},
		{Kind: Actions, Kit: true, Buttons: []Button{
			{Kind: "button", Text: "Delete", Style: "danger", ActionID: "del", Value: "x", BlockID: "b3"},
			{Kind: "select", Text: "Pick one", ActionID: "sel", BlockID: "b3"},
			{Kind: "select", Text: "Chosen", BlockID: "b3"},
			{Kind: "datepicker", Text: "Select a date", ActionID: "dp", BlockID: "b3"},
		}},
		{Kind: Section, Kit: true, Spans: []Span{st("Name", Bold)}, Buttons: []Button{{Kind: "input", Text: "Type here"}}},
		{Kind: AttachmentBlock, Att: &Attachment{ServiceName: "VidSite", ServiceIcon: "https://v.test/i.png", AuthorName: "Someone",
			Title: []Span{tx("Demo")}, TitleLink: "https://v.test/w", VideoURL: "https://v.test/embed", IsUnfurl: true,
			Image: &Image{URL: "https://v.test/t.jpg", Alt: "demo"}, Text: []Block{para(tx("A demo"))}}},
		// SUIRendererItem: a type Slack doesn't know is UnsupportedBlockFallback,
		// or its own fallback payload when it carries one
		{Kind: UnknownBlock, Spans: []Span{tx("This message can’t be fully displayed.")}},
		{Kind: Section, Kit: true, Spans: []Span{tx("degraded")}},
		para(st("md bold", Bold), tx(" and "), lk("a link", "https://md.test")),
	}
	if len(d.Blocks) != len(want) {
		t.Fatalf("got %d blocks, want %d:\n%s", len(d.Blocks), len(want), dump(d.Blocks))
	}
	for i := range want {
		eq(t, "block "+want[i].kindName(), d.Blocks[i], want[i])
	}
}

func (b Block) kindName() string {
	return [...]string{"Paragraph", "Preformatted", "Quote", "List", "Header", "Divider", "Context", "Section", "ImageBlock", "Actions", "AttachmentBlock", "FileBlock", "Tombstone", "Table", "Agent", "Unsupported", "UnknownBlock", "FileStatus", "CallBlock", "AlertBlock", "CalloutBlock", "LoadingBlock", "RichHeader", "RichDivider", "ContainerBlock", "ShortcutBlock", "CardBlock"}[b.Kind]
}

func TestBlocksFallBackToText(t *testing.T) {
	for _, blocks := range []string{`[]`, `null`, `"not an array"`} {
		d := blocksMsg(t, blocks)
		eq(t, "fallback "+blocks, d.Blocks, []Block{para(tx("fallback text"))})
	}
	// a block type Slack doesn't know draws its card, not the message text
	d := blocksMsg(t, `[{"type":"mystery"}]`)
	eq(t, "unknown block", d.Blocks, []Block{{Kind: UnknownBlock, Spans: []Span{tx("This message can’t be fully displayed.")}}})
}

// TestTextOverBlocksLikeSlack: shouldRenderBlocksInsteadOfText (iub5)
// leaves some messages to another renderer and draws their text: there the
// text stands in for blocks that draw nothing (ui's empty rich_text case),
// for automated subtypes (h3YX), channel tab and folder messages, huddle
// notes, list record roots, calendar events, a meeting's artifacts DM and
// a canvas share that carries files. A plain message and a canvas share
// without files draw their blocks, even when those draw nothing; and
// blocks that draw stay, as MessageAutomatedSubtype's GenericSystemMessage
// shows agent_view_added's.
func TestTextOverBlocksLikeSlack(t *testing.T) {
	const empty = `[{"type":"rich_text","elements":[]}]`
	const section = `[{"type":"section","text":{"type":"mrkdwn","text":"the blocks"}}]`
	for _, c := range []struct {
		name, extra, blocks string
		text                bool // the message text draws, not the blocks
	}{
		{"a plain message", ``, empty, false},
		{"a plain message's section", ``, section, false},
		{"an automated subtype", `"subtype":"bot_add",`, empty, true},
		{"a pinned item", `"subtype":"pinned_item",`, empty, true},
		{"a channel tab message", `"subtype":"channel_tab_added",`, empty, true},
		{"huddle notes", `"subtype":"huddle_notes_started",`, empty, true},
		{"a list record's root", `"subtype":"list_record_comment","slack_list":{"list_id":"F0LIST","list_record_id":"Rec1"},`, empty, true},
		{"a calendar event", `"attachments":[{"id":1,"calendar_event":{"title":"Sync"}}],`, empty, true},
		{"a meeting's artifacts DM", `"metadata":{"event_type":"slack_system.meeting.artifacts_ready","event_payload":{}},`, empty, true},
		{"a canvas share with files", `"subtype":"canvas_sharing_message","files":[{"id":"F0CANVAS","title":"Plan"}],`, empty, true},
		{"a canvas share without files", `"subtype":"canvas_sharing_message",`, empty, false},
		{"agent_view_added's blocks", `"subtype":"agent_view_added",`, section, false},
	} {
		d := FromMessage(R, msg(t, `{"ts":"1.0",`+c.extra+`"text":"fallback text","blocks":`+c.blocks+`}`))
		got := len(d.Blocks) > 0 && d.Blocks[0].Kind == Paragraph && PlainText(Doc{Blocks: d.Blocks[:1]}) == "fallback text"
		if got != c.text {
			t.Errorf("%s: draws the text %v, Slack %v (%s)", c.name, got, c.text, dump(d.Blocks))
		}
		if !c.text && strings.Contains(PlainText(d), "fallback text") {
			t.Errorf("%s: the text drew beside its blocks (%s)", c.name, dump(d.Blocks))
		}
	}
}

// TestCallBlockPlainText: a call block's plain text (copy, a thread row's
// text) is its card's header, its title and status lines, as ui read the
// call attachment's title before the card.
func TestCallBlockPlainText(t *testing.T) {
	d := blocksMsg(t, `[{"type":"call","call_id":"R1","call":{"v1":{"id":"R1","name":"Synthetic sync","date_start":1700000000,"date_end":1700003600}}}]`)
	if len(d.Blocks) != 1 || d.Blocks[0].Kind != CallBlock || d.Blocks[0].Call == nil || d.Blocks[0].Call.Title == "" {
		t.Fatalf("blocks %s", dump(d.Blocks))
	}
	c := d.Blocks[0].Call
	got := PlainText(*d)
	if !strings.HasPrefix(got, c.Title) || (c.Status != "" && !strings.Contains(got, c.Status)) {
		t.Errorf("plain text %q, want the card's %q and %q", got, c.Title, c.Status)
	}
}
