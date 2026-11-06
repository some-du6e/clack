package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// All fixtures here are synthetic (scraping policy: never real messages).

type fakeRes struct{}

var (
	fakeUsers  = map[string]string{"U1": "alice", "USELF": "me", "U2": "bob"}
	fakeChans  = map[string]string{"C1": "general"}
	fakeGroups = map[string]string{"S1": "design"}
	fakeCustom = map[string]string{"parrot": "https://emoji.slack-edge.com/T1/parrot/abc.gif"}
)

func (fakeRes) UserName(id string) string      { return fakeUsers[id] }
func (fakeRes) ChannelName(id string) string   { return orID(fakeChans[id], id) }
func (fakeRes) UsergroupName(id string) string { return orID(fakeGroups[id], id) }
func (fakeRes) Proxy(u string) string          { return ProxyURL(u) }
func (fakeRes) SelfID() string                 { return "USELF" }
func (fakeRes) Emoji(name string) (string, string, bool) {
	if u, ok := fakeCustom[name]; ok {
		return u, "", true
	}
	if u, ok := emoji.StandardURL(name, "apple", "medium"); ok {
		_, t, _ := emoji.Standard(name)
		return u, t, true
	}
	return "", "", false
}

func orID(v, id string) string {
	if v == "" {
		return id
	}
	return v
}

var R fakeRes

type check struct {
	has, not []string
}

func expect(t *testing.T, name, out string, c check) {
	t.Helper()
	for _, h := range c.has {
		if !strings.Contains(out, h) {
			t.Errorf("%s: want %q in\n%s", name, h, out)
		}
	}
	for _, n := range c.not {
		if strings.Contains(out, n) {
			t.Errorf("%s: did not want %q in\n%s", name, n, out)
		}
	}
}

const (
	B  = `<b data-stringify-type="bold">`
	I  = `<i data-stringify-type="italic">`
	S  = `<s data-stringify-type="strike">`
	CD = `<code class="c-mrkdwn__code" data-stringify-type="code">`
)

func TestMrkdwn(t *testing.T) {
	cases := []struct {
		name, in string
		check
	}{
		{"bold", "a *bold* b", check{has: []string{"a " + B + "bold</b> b"}}},
		{"italic", "_ital_", check{has: []string{I + "ital</i>"}}},
		{"strike", "~gone~", check{has: []string{S + "gone</s>"}}},
		{"nested", "*_both_*", check{has: []string{B + I + "both</i></b>"}}},
		{"adjacent", "*a* *b*", check{has: []string{B + "a</b> " + B + "b</b>"}}},
		{"after quote char", `"*q*"`, check{has: []string{"&#34;" + B + "q</b>&#34;"}}},
		{"punct after", "*wow*!", check{has: []string{B + "wow</b>!"}}},
		{"intraword star", "2*3*4", check{not: []string{"<b"}}},
		{"snake case", "snake_case_word", check{has: []string{"snake_case_word"}, not: []string{"<i"}}},
		{"code", "x `code *y*` z", check{has: []string{CD + "code *y*</code>"}, not: []string{"<b"}}},
		{"pre", "```\nline1\n*x*\n```", check{has: []string{`<pre class="c-mrkdwn__pre"`, "line1\n*x*"}, not: []string{"<b"}}},
		{"unterminated fence", "```oops", check{has: []string{"```oops"}}},
		{"quote", "&gt; quoted\nnot", check{has: []string{`<blockquote type="cite" class="c-mrkdwn__quote" data-stringify-type="quote">quoted</blockquote>not`}}},
		{"quote multi", "&gt; a\n&gt; b", check{has: []string{`data-stringify-type="quote">a<br>b</blockquote>`}}},
		{"rest quote", "&gt;&gt;&gt; a\nb", check{has: []string{`data-stringify-type="quote">a<br>b</blockquote>`}}},
		{"newline", "a\nb", check{has: []string{"a<br>b"}}},
		{"link label", "<https://example.com|Example>", check{has: []string{`<a class="c-link" href="https://example.com" target="_blank" rel="noopener noreferrer" data-stringify-link="https://example.com">Example</a>`}}},
		{"bare link", "<https://example.com/a?b=1&amp;c=2>", check{has: []string{`href="https://example.com/a?b=1&amp;c=2"`, `>https://example.com/a?b=1&amp;c=2</a>`}}},
		{"mailto", "<mailto:a@b.co|a@b.co>", check{has: []string{`href="mailto:a@b.co"`}}},
		{"js link", "<javascript:alert(1)|click>", check{has: []string{"click"}, not: []string{"javascript", "<a"}}},
		{"typed tag", "&lt;script&gt;alert(1)&lt;/script&gt;", check{has: []string{"&lt;script&gt;alert(1)&lt;/script&gt;"}, not: []string{"<script", "<a"}}},
		{"amp", "A &amp; B", check{has: []string{"A &amp; B"}}},
		{"user", "<@U1> hi", check{has: []string{`<a class="c-member_slug c-member_slug--link" data-member-id="U1" data-stringify-type="mention" href="#/u/U1">@alice</a> hi`}}},
		{"self mention", "<@USELF>", check{has: []string{"c-member_slug--mention", "@me"}}},
		{"unknown user label", "<@U9|zed>", check{has: []string{"@zed"}}},
		{"channel", "<#C1>", check{has: []string{`data-channel-id="C1" href="#/c/C1">#general</a>`}}},
		{"channel label", "<#C2|random>", check{has: []string{">#random</a>"}}},
		{"subteam", "<!subteam^S1>", check{has: []string{`data-usergroup-id="S1">@design</span>`}}},
		{"subteam label", "<!subteam^S1|@devs>", check{has: []string{">@devs</span>"}}},
		{"here", "<!here>", check{has: []string{`<span class="c-mention_badge">@here</span>`}}},
		{"date", "<!date^1392734382^{date_short} at {time}|fallback>", check{has: []string{`data-ts="1392734382"`, "2014", `data-format="{date_short} at {time}"`}}},
		{"bad date", "<!date^nope^{time}|fallback>", check{has: []string{"fallback"}}},
		// Formatting must never leak into generated attributes.
		{"links and italics", "<https://ex.com/a_b_c> and <https://x.com/_y_> _it_", check{has: []string{I + "it</i>", `href="https://ex.com/a_b_c"`, `href="https://x.com/_y_"`}}},
		{"mention then link", "<@U1> <https://a.com> <@U2>", check{has: []string{"@alice", "@bob", `href="https://a.com"`}, not: []string{"<i"}}},
		{"bold link", "*<https://a.com|go>*", check{has: []string{B + `<a class="c-link" href="https://a.com"`}}},
		{"emoji", "hi :+1:", check{has: []string{`class="c-emoji c-emoji__medium"`, "apple-medium/1f44d.png", `alt="👍"`}}},
		{"custom emoji", "x :parrot:", check{has: []string{`src="https://emoji.slack-edge.com/T1/parrot/abc.gif"`}}},
		{"jumbomoji", ":+1: :parrot:", check{has: []string{"c-emoji__large"}, not: []string{"c-emoji__medium"}}},
		{"skin tone", "a :+1::skin-tone-3:", check{has: []string{"1f44d-1f3fc.png"}}},
		{"unknown emoji", ":nopeemoji:", check{has: []string{":nopeemoji:"}, not: []string{"<img"}}},
		{"time not emoji", "at 12:30:45", check{has: []string{"12:30:45"}, not: []string{"<img"}}},
		{"emoji not in code", "`:+1:`", check{not: []string{"<img"}}},
	}
	for _, c := range cases {
		expect(t, c.name, Mrkdwn(R, c.in), c.check)
	}
}

func TestMrkdwnBalancedTags(t *testing.T) {
	ins := []string{
		"*a _b ~c~ d_ e*", "<@U1> _x_ <https://a.com/_q_|l_i_nk> *y*", "_<https://a.com|z>_", "~*_x_*~",
		"&gt; *q* <#C1>\n```\npre <@U1>\n```\n_end_", "* not bold *", "_ not italic _",
	}
	for _, in := range ins {
		out := Mrkdwn(R, in)
		for _, tag := range []string{"b", "i", "s", "a", "code", "pre", "blockquote", "span"} {
			o := strings.Count(out, "<"+tag+" ") + strings.Count(out, "<"+tag+">")
			c := strings.Count(out, "</"+tag+">")
			if o != c {
				t.Errorf("%q: unbalanced <%s> (%d open, %d close):\n%s", in, tag, o, c, out)
			}
		}
		if strings.ContainsAny(out, "\x01\x10\x11\x12\x13\x14\x15") {
			t.Errorf("%q: leaked placeholder: %q", in, out)
		}
	}
}

func TestSafeURL(t *testing.T) {
	for u, want := range map[string]string{
		"https://a.com": "https://a.com", "http://a.com": "http://a.com", "mailto:x@y.z": "mailto:x@y.z",
		"javascript:alert(1)": "", " JavaScript:alert(1)": "", "data:text/html,x": "", "vbscript:x": "",
		"/file?u=x": "/file?u=x", "#/c/C1": "#/c/C1", "foo": "",
	} {
		if got := SafeURL(u); got != want {
			t.Errorf("SafeURL(%q) = %q, want %q", u, got, want)
		}
	}
}

func rich(t *testing.T, js string) string {
	t.Helper()
	var raw json.RawMessage = []byte(js)
	return Blocks(R, raw)
}

func TestRichText(t *testing.T) {
	cases := []struct {
		name, js string
		check
	}{
		{"styles", `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
			{"type":"text","text":"b","style":{"bold":true}},{"type":"text","text":"i","style":{"italic":true}},
			{"type":"text","text":"s","style":{"strike":true}},{"type":"text","text":"c","style":{"code":true}},
			{"type":"text","text":"all","style":{"bold":true,"italic":true,"strike":true,"code":true}},
			{"type":"text","text":"<x> & \"y\"\nz"}]}]}]`,
			check{has: []string{B + "b</b>", I + "i</i>", S + "s</s>", CD + "c</code>", B + I + S + CD + "all</code></s></i></b>", "&lt;x&gt; &amp; &#34;y&#34;<br>z"}}},
		{"bullet list", `[{"type":"rich_text","elements":[{"type":"rich_text_list","style":"bullet","indent":1,"elements":[
			{"type":"rich_text_section","elements":[{"type":"text","text":"one"}]},
			{"type":"rich_text_section","elements":[{"type":"text","text":"two"}]}]}]}]`,
			check{has: []string{`<ul class="p-rich_text_list p-rich_text_list__bullet" data-stringify-type="unordered-list" data-indent="1"`, "<li data-stringify-indent=\"1\">one</li>", ">two</li>", "</ul>"}}},
		{"ordered list offset", `[{"type":"rich_text","elements":[{"type":"rich_text_list","style":"ordered","offset":2,"elements":[
			{"type":"rich_text_section","elements":[{"type":"text","text":"three"}]}]}]}]`,
			check{has: []string{`<ol class="p-rich_text_list p-rich_text_list__ordered"`, `start="3"`, "three</li></ol>"}}},
		{"quote", `[{"type":"rich_text","elements":[{"type":"rich_text_quote","elements":[{"type":"text","text":"wise"}]}]}]`,
			check{has: []string{`<blockquote type="cite" class="c-mrkdwn__quote" data-stringify-type="quote">wise</blockquote>`}}},
		{"preformatted", `[{"type":"rich_text","elements":[{"type":"rich_text_preformatted","elements":[{"type":"text","text":"x := 1\ny"}]}]}]`,
			check{has: []string{`<pre class="c-mrkdwn__pre" data-stringify-type="pre">`, "x := 1"}}},
		{"entities", `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
			{"type":"user","user_id":"USELF"},{"type":"user","user_id":"U1"},{"type":"channel","channel_id":"C1"},
			{"type":"usergroup","usergroup_id":"S1"},{"type":"broadcast","range":"channel"},
			{"type":"emoji","name":"+1","skin_tone":2},{"type":"text","text":" "},
			{"type":"link","url":"https://a.com","text":"site"},{"type":"link","url":"https://b.com","text":"plain","style":{"unlink":true}},
			{"type":"link","url":"javascript:alert(1)","text":"evil"},
			{"type":"date","timestamp":1392734382,"format":"{date_num}","fallback":"x"},
			{"type":"color","value":"#ff0000"},{"type":"color","value":"red;position:fixed"}]}]}]`,
			check{has: []string{"c-member_slug--mention", "@alice", "#general", "@design", "@channel", "1f44d-1f3fb.png",
				`href="https://a.com"`, ">site</a>", "plain", "evil", `data-ts="1392734382"`, "2014-02-1", `style="background:#ff0000"`, "red;position:fixed"},
				not: []string{`href="https://b.com"`, "javascript", `background:red`}}},
		{"jumbomoji", `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"emoji","name":"+1"},{"type":"text","text":" "},{"type":"emoji","name":"parrot"}]}]}]`,
			check{has: []string{"c-emoji__large"}, not: []string{"c-emoji__medium"}}},
		{"not jumbo", `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"emoji","name":"+1"},{"type":"text","text":" hi"}]}]}]`,
			check{has: []string{"c-emoji__medium"}}},
		{"section+fields+button", `[{"type":"section","text":{"type":"mrkdwn","text":"*hi*"},"fields":[{"type":"plain_text","text":"f1"}],
			"accessory":{"type":"button","text":{"type":"plain_text","text":"Go"},"url":"javascript:x","action_id":"a1"}},
			{"type":"divider"},{"type":"header","text":{"type":"plain_text","text":"Head"}},
			{"type":"context","elements":[{"type":"mrkdwn","text":"_ctx_"},{"type":"image","image_url":"https://img.example/x.png","alt_text":"alt"}]},
			{"type":"image","image_url":"javascript:alert(1)","alt_text":"bad"}]`,
			check{has: []string{B + "hi</b>", "f1", `<button class="c-button c-button--outline c-button--small p-block_kit_button_element" type="button" data-action-id="a1"`, "<hr", "Head", I + "ctx</i>", `src="https://img.example/x.png"`},
				not: []string{"javascript"}}},
	}
	for _, c := range cases {
		expect(t, c.name, rich(t, c.js), c.check)
	}
}

func TestAttachments(t *testing.T) {
	a := slack.Attachment{
		Color: "ff0000", Pretext: "pre *x*", AuthorName: "Author", AuthorLink: "https://author.example", AuthorIcon: "https://img.example/a.png",
		Title: "Title :+1:", TitleLink: "https://title.example", Text: "body _y_",
		Fields:   []slack.AttachmentField{{Title: "F", Value: "*v*", Short: true}},
		ImageURL: "https://img.example/big.png", ImageWidth: 720, ImageHeight: 540,
		Footer: "foot", TS: json.RawMessage(`"1392734382.000"`),
	}
	out := Attachment(R, &a)
	expect(t, "attachment", out, check{has: []string{
		`class="c-message_attachment c-message_attachment--colored" style="--attachment-color:#ff0000"`,
		B + "x</b>", `href="https://author.example"`, `href="https://title.example"`, "1f44d.png", I + "y</i>",
		"c-message_attachment__field--short", B + "v</b>", `src="https://img.example/big.png"`, "max-width:360px;max-height:270px",
		`data-ts="1392734382"`, "foot",
	}})

	bad := slack.Attachment{Color: `red"><script>`, TitleLink: "javascript:alert(1)", Title: "t", ImageURL: "javascript:x"}
	expect(t, "bad attachment", Attachment(R, &bad), check{has: []string{"c-message_attachment__title"}, not: []string{"<script", "javascript", "style="}})

	share := slack.Attachment{IsMsgUnfurl: true, AuthorName: "alice", ChannelID: "C1", Text: "shared *text*", FromURL: "https://hackclub.slack.com/archives/C1/p1"}
	expect(t, "share", Attachment(R, &share), check{has: []string{"c-message_attachment--share", "alice", "Posted in #general", B + "text</b>", "View message"}})
}

func TestFiles(t *testing.T) {
	files := []slack.File{
		{ID: "F1", Name: "pic.png", Mimetype: "image/png", URLPrivate: "https://files.slack.com/files-pri/T1-F1/pic.png",
			Thumb360: "https://files.slack.com/files-tmb/T1-F1/pic_360.png", Thumb720: "https://files.slack.com/files-tmb/T1-F1/pic_720.png", Thumb720W: 720, Thumb720H: 360},
		{ID: "F2", Name: "clip.mp4", Mimetype: "video/mp4", URLPrivate: "https://files.slack.com/files-pri/T1-F2/clip.mp4", ThumbVideo: "https://files.slack.com/files-tmb/T1-F2/v.jpg"},
		{ID: "F3", Name: "memo.m4a", Mimetype: "audio/mp4", URLPrivate: "https://files.slack.com/files-pri/T1-F3/memo.m4a"},
		{ID: "F4", Name: "doc.pdf", Title: "Doc", Mimetype: "application/pdf", Filetype: "pdf", PrettyType: "PDF", Size: 1572864, URLPrivate: "https://files.slack.com/files-pri/T1-F4/doc.pdf"},
		{ID: "F5", Mode: "tombstone"},
		{ID: "F6", Name: "x.txt", Filetype: "text", Mode: "snippet", Preview: "<b>raw</b>", URLPrivate: "https://files.slack.com/files-pri/T1-F6/x.txt"},
	}
	out := Files(R, files)
	expect(t, "files", out, check{
		has: []string{
			`data-file-id="F1"`, `src="/file?u=https%3A%2F%2Ffiles.slack.com%2Ffiles-tmb%2FT1-F1%2Fpic_720.png"`, `width="360" height="180"`,
			`<video controls preload="metadata" poster="/file?u=`, `<audio controls`, "PDF · 1.5 MB", ">Doc</span>",
			"This file was deleted.", "&lt;b&gt;raw&lt;/b&gt;",
		},
		not: []string{"<b>raw"},
	})
	if got := ProxyURL("https://example.com/x.png"); got != "https://example.com/x.png" {
		t.Errorf("non-slack URL proxied: %s", got)
	}
}

func TestMessageView(t *testing.T) {
	m := slack.Message{
		TS: "1700000000.000100", User: "U1", Text: "hello *world*",
		Edited:     &slack.Edited{User: "U1", TS: "1700000001.000000"},
		Reactions:  []slack.Reaction{{Name: "+1", Count: 2, Users: []string{"U1", "USELF"}}, {Name: "parrot", Count: 1, Users: []string{"U2"}}},
		Files:      []slack.File{{ID: "F1", Name: "a.png", Mimetype: "image/png", URLPrivate: "https://files.slack.com/a.png", Size: 10, OriginalW: 100, OriginalH: 50}},
		ReplyCount: 3, ThreadTS: "1700000000.000100", PinnedTo: []string{"C1"},
	}
	v := Message(R, "C1", &m)
	if !v.Edited || v.ReplyCount != 3 || !v.Pinned || v.IsBot {
		t.Errorf("flags wrong: %+v", v)
	}
	if len(v.Reactions) != 2 || !v.Reactions[0].Self || v.Reactions[1].Self || !strings.Contains(v.Reactions[1].URL, "parrot") {
		t.Errorf("reactions wrong: %+v", v.Reactions)
	}
	if len(v.Files) != 1 || !strings.HasPrefix(v.Files[0].URL, "/file?u=") || v.Files[0].W != 100 {
		t.Errorf("files wrong: %+v", v.Files)
	}
	expect(t, "body", v.HTML, check{has: []string{B + "world</b>", "c-message__edited_label", "c-message_kit__blocks"}})

	bot := slack.Message{TS: "1", BotID: "B1", Subtype: "bot_message", Username: "Deploy Bot", Text: "ok",
		BotProfile: &slack.BotProfile{ID: "B1", Name: "deploybot", Icons: slack.Icons{Image72: "https://img.example/bot.png"}}}
	bv := Message(R, "C1", &bot)
	if !bv.IsBot || bv.Name != "Deploy Bot" || bv.Avatar != "https://img.example/bot.png" {
		t.Errorf("bot view wrong: %+v", bv)
	}

	del := slack.Message{TS: "2", Subtype: "tombstone", Text: "secret"}
	expect(t, "tombstone", Message(R, "C1", &del).HTML, check{has: []string{"This message was deleted."}, not: []string{"secret"}})

	join := slack.Message{TS: "3", Subtype: "channel_join", User: "U1", Text: "<@U1> has joined the channel"}
	if !Message(R, "C1", &join).System {
		t.Error("channel_join should be a system message")
	}

	blocks := slack.Message{TS: "4", Text: "fallback text", Blocks: json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"from blocks"}]}]}]`)}
	expect(t, "blocks preferred", Message(R, "C1", &blocks).HTML, check{has: []string{"from blocks"}, not: []string{"fallback text"}})
}
