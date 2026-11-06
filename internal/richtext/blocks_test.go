package richtext

import (
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
		tx("a"), br(), br(), tx("b"),
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
		{"type":"call","call_id":"R1","call":{"v1":{"name":"Standup","join_url":"https://call.test/j","date_start":1700000000,"app_icon_urls":{"image_32":"https://call.test/i.png"}}}},
		{"type":"unknown_future_block","text":{"type":"mrkdwn","text":"future _text_"}},
		{"type":"markdown","text":"**md bold** and [a link](https://md.test)"}
	]`)
	want := []Block{
		{Kind: Header, Spans: []Span{tx("Weekly "), em("smile"), tx(" & more")}},
		{Kind: Divider},
		{Kind: Section, Spans: []Span{st("Hi", Bold), tx(" "), user("U1", "@alice")},
			Fields:     [][]Span{{st("Owner", Bold), br(), tx("alice")}, {tx("Plain")}},
			FieldShort: []bool{true, true},
			Image:      &Image{URL: "https://files.slack.com/files-pri/T1-F1/a.png", Alt: "pic", Proxy: true}},
		{Kind: Section, Children: []Block{quote(tx("quoted")), para(tx("not"))},
			Buttons: []Button{{Kind: "button", Text: "Go", URL: "https://go.test", Style: "primary", ActionID: "a1", Value: "v1", BlockID: "b2"}}},
		{Kind: Section, Spans: []Span{tx("menu")}, Buttons: []Button{{Kind: "overflow", Text: "⋮", ActionID: "o1"}}},
		{Kind: Context, Spans: []Span{{Kind: InlineImage, Text: "icon", URL: "https://img.test/i.png"}, tx(" "), tx("by "), st("bot", Bold), tx(" "), tx("ok")}},
		{Kind: ImageBlock, Image: &Image{URL: "https://img.test/big.png", Alt: "big", Title: "Big pic", W: 640, H: 480}},
		{Kind: ImageBlock, Image: &Image{URL: "https://files.slack.com/files-pri/T1-F2/b.png", Alt: "sf", Proxy: true}},
		{Kind: Actions, Buttons: []Button{
			{Kind: "button", Text: "Delete", Style: "danger", ActionID: "del", Value: "x", BlockID: "b3"},
			{Kind: "select", Text: "Pick one", ActionID: "sel", BlockID: "b3"},
			{Kind: "select", Text: "Chosen", BlockID: "b3"},
			{Kind: "datepicker", Text: "Select a date", ActionID: "dp", BlockID: "b3"},
		}},
		{Kind: Section, Spans: []Span{st("Name", Bold)}, Buttons: []Button{{Kind: "input", Text: "Type here"}}},
		{Kind: AttachmentBlock, Att: &Attachment{ServiceName: "VidSite", ServiceIcon: "https://v.test/i.png", AuthorName: "Someone",
			Title: []Span{tx("Demo")}, TitleLink: "https://v.test/w", VideoURL: "https://v.test/embed", IsUnfurl: true,
			Image: &Image{URL: "https://v.test/t.jpg", Alt: "demo"}, Text: []Block{para(tx("A demo"))}}},
		{Kind: AttachmentBlock, Att: &Attachment{ServiceName: "Call", ServiceIcon: "https://call.test/i.png", IsUnfurl: true,
			Title: []Span{tx("Standup")}, TitleLink: "https://call.test/j", TS: 1700000000}},
		para(tx("future "), st("text", Italic)),
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
	return [...]string{"Paragraph", "Preformatted", "Quote", "List", "Header", "Divider", "Context", "Section", "ImageBlock", "Actions", "AttachmentBlock", "FileBlock", "Tombstone"}[b.Kind]
}

func TestBlocksFallBackToText(t *testing.T) {
	for _, blocks := range []string{`[]`, `null`, `[{"type":"mystery"}]`, `"not an array"`, `[{"type":"rich_text","elements":[]}]`} {
		d := blocksMsg(t, blocks)
		eq(t, "fallback "+blocks, d.Blocks, []Block{para(tx("fallback text"))})
	}
}
