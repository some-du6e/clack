package richtext

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// All fixtures in this package are synthetic (Hack Club scraping policy:
// never store real message content).

type fakeRes struct{}

var (
	fakeUsers  = map[string]string{"U1": "alice", "USELF": "me", "U2": "@bob"}
	fakeChans  = map[string]string{"C1": "general"}
	fakeGroups = map[string]string{"S1": "design"}
	fakeEmoji  = map[string][2]string{
		"smile":             {"https://e.test/smile.png", "😄"},
		"wave":              {"https://e.test/wave.png", "👋"},
		"wave::skin-tone-3": {"https://e.test/wave-3.png", "👋🏼"},
		"+1":                {"https://e.test/+1.png", "👍"},
		"white_check_mark":  {"https://e.test/check.png", "✅"},
		"parrot":            {"https://e.test/parrot.gif", ""},
	}
)

func (fakeRes) UserName(id string) string      { return fakeUsers[id] }
func (fakeRes) ChannelName(id string) string   { return fakeChans[id] }
func (fakeRes) UsergroupName(id string) string { return fakeGroups[id] }
func (fakeRes) SelfID() string                 { return "USELF" }
func (fakeRes) Emoji(name string) (string, string, bool) {
	e, ok := fakeEmoji[name]
	return e[0], e[1], ok
}

var R fakeRes

// span constructors
func tx(s string) Span          { return Span{Kind: Text, Text: s} }
func st(s string, x Style) Span { return Span{Kind: Text, Text: s, Style: x} }
func lk(label, url string) Span { return Span{Kind: Link, Text: label, URL: url} }
func em(name string) Span       { e := fakeEmoji[name]; return emSpan(name, e) }
func br() Span                  { return Span{Kind: Break} }
func para(ss ...Span) Block     { return Block{Kind: Paragraph, Spans: ss} }
func quote(ss ...Span) Block    { return Block{Kind: Quote, Spans: ss} }
func pre(ss ...Span) Block      { return Block{Kind: Preformatted, Spans: ss} }
func with(s Span, x Style) Span { s.Style |= x; return s }
func user(id, text string) Span { return Span{Kind: User, ID: id, Text: text, Self: id == "USELF"} }
func chn(id, text string) Span  { return Span{Kind: Channel, ID: id, Text: text} }
func grp(id, text string) Span  { return Span{Kind: Usergroup, ID: id, Text: text} }
func bcast(name string) Span    { return Span{Kind: Broadcast, ID: name, Text: "@" + name, Self: true} }
func dump(v any) string         { b, _ := json.MarshalIndent(v, "", " "); return string(b) }
func emSpan(name string, e [2]string) Span {
	t := e[1]
	if t == "" {
		t = ":" + name + ":"
	}
	return Span{Kind: Emoji, Text: t, URL: e[0], ID: name}
}

func eq(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got %s\nwant %s", name, dump(got), dump(want))
	}
}

// checkInvariants enforces documented guarantees on every produced block.
func checkInvariants(t *testing.T, name string, bs []Block) {
	t.Helper()
	var spans func(where string, ss []Span)
	spans = func(where string, ss []Span) {
		for _, s := range ss {
			if strings.Contains(s.Text, "\n") {
				t.Errorf("%s: %s span %q contains a newline", name, where, s.Text)
			}
			if s.Kind == Break && s.Text != "" {
				t.Errorf("%s: %s Break with text %q", name, where, s.Text)
			}
			if s.Kind == Link && SafeURL(s.URL) == "" {
				t.Errorf("%s: %s unsafe link %q", name, where, s.URL)
			}
		}
	}
	var walk func([]Block)
	walk = func(bs []Block) {
		for _, b := range bs {
			spans("block", b.Spans)
			for _, it := range b.Items {
				spans("item", it)
			}
			for _, f := range b.Fields {
				spans("field", f)
			}
			if b.FieldShort != nil && len(b.FieldShort) != len(b.Fields) {
				t.Errorf("%s: FieldShort len %d != Fields len %d", name, len(b.FieldShort), len(b.Fields))
			}
			if b.Kind == AttachmentBlock && b.Att == nil {
				t.Errorf("%s: Attachment block without Att", name)
			}
			if b.Kind == FileBlock && b.File == nil {
				t.Errorf("%s: File block without File", name)
			}
			if b.Att != nil {
				spans("pretext", b.Att.Pretext)
				spans("title", b.Att.Title)
				spans("footer", b.Att.Footer)
				walk(b.Att.Text)
			}
			if b.Image != nil && SafeImage(b.Image.URL) == "" {
				t.Errorf("%s: unsafe image %q", name, b.Image.URL)
			}
			walk(b.Children)
		}
	}
	walk(bs)
}

func TestFromMrkdwn(t *testing.T) {
	date := FormatDate(time.Unix(1700000000, 0), "{date_num} at {time}")
	cases := []struct {
		name, in string
		want     []Block
	}{
		{"empty", "", nil},
		{"plain", "hello world", []Block{para(tx("hello world"))}},
		{"bold", "a *bold* b", []Block{para(tx("a "), st("bold", Bold), tx(" b"))}},
		{"italic", "_ital_", []Block{para(st("ital", Italic))}},
		{"strike", "~gone~", []Block{para(st("gone", Strike))}},
		{"two bolds", "*a* and *b*", []Block{para(st("a", Bold), tx(" and "), st("b", Bold))}},
		{"nested", "*bold _both_*", []Block{para(st("bold ", Bold), st("both", Bold|Italic))}},
		{"strike bold", "~*both*~", []Block{para(st("both", Bold|Strike))}},
		{"punct after", "*bold*.", []Block{para(st("bold", Bold), tx("."))}},
		{"parens", "(*bold*)", []Block{para(tx("("), st("bold", Bold), tx(")"))}},
		{"quote chars", `"_x_"`, []Block{para(tx(`"`), st("x", Italic), tx(`"`))}},
		{"intraword underscore", "snake_case_name", []Block{para(tx("snake_case_name"))}},
		{"intraword star", "2*3*4", []Block{para(tx("2*3*4"))}},
		{"space inside markers", "* not bold *", []Block{para(tx("* not bold *"))}},
		{"close needs boundary", "*bold*text", []Block{para(tx("*bold*text"))}},
		{"italic needs boundary", "_italic_text_", []Block{para(tx("_italic_text_"))}},
		{"empty markers", "** __", []Block{para(tx("** __"))}},
		{"unmatched", "a * b", []Block{para(tx("a * b"))}},
		{"code", "`code *x*`", []Block{para(st("code *x*", Code))}},
		{"two codes", "`a` `b`", []Block{para(st("a", Code), tx(" "), st("b", Code))}},
		{"code in bold", "*`x`*", []Block{para(st("x", Code|Bold))}},
		{"link in code", "`see <https://x.test|x>`", []Block{para(st("see ", Code), with(lk("x", "https://x.test"), Code))}},
		{"code no emoji", "`:smile:`", []Block{para(st(":smile:", Code))}},
		{"lone backtick", "it`s", []Block{para(tx("it`s"))}},
		{"link label", "<https://x.test|label>", []Block{para(lk("label", "https://x.test"))}},
		{"bare link", "<https://x.test/a_b_c>", []Block{para(lk("https://x.test/a_b_c", "https://x.test/a_b_c"))}},
		{"link amp", "<https://x.test?a=1&amp;b=2|q &amp; a>", []Block{para(lk("q & a", "https://x.test?a=1&b=2"))}},
		{"mailto", "<mailto:a@b.test|a@b.test>", []Block{para(lk("a@b.test", "mailto:a@b.test"))}},
		{"mailto bare", "<mailto:a@b.test>", []Block{para(lk("a@b.test", "mailto:a@b.test"))}},
		{"unsafe link", "<javascript:alert(1)|click>", []Block{para(tx("click"))}},
		{"bold link", "*<https://x.test|link>*", []Block{para(with(lk("link", "https://x.test"), Bold))}},
		{"bold around link", "*see <https://x.test|this> now*", []Block{para(st("see ", Bold), with(lk("this", "https://x.test"), Bold), st(" now", Bold))}},
		{"user", "hi <@U1>", []Block{para(tx("hi "), user("U1", "@alice"))}},
		{"self", "<@USELF>", []Block{para(user("USELF", "@me"))}},
		{"user at-name", "<@U2>", []Block{para(user("U2", "@bob"))}},
		{"user label", "<@U9|someone>", []Block{para(user("U9", "@someone"))}},
		{"user unknown", "<@U9>", []Block{para(user("U9", "@U9"))}},
		{"channel", "<#C1>", []Block{para(chn("C1", "#general"))}},
		{"channel label", "<#C2|random>", []Block{para(chn("C2", "#random"))}},
		{"usergroup", "<!subteam^S1>", []Block{para(grp("S1", "@design"))}},
		{"usergroup label", "<!subteam^S2|@ops>", []Block{para(grp("S2", "@ops"))}},
		{"here", "<!here>", []Block{para(bcast("here"))}},
		{"here label", "<!here|here>", []Block{para(bcast("here"))}},
		{"channel bcast", "<!channel>", []Block{para(bcast("channel"))}},
		{"everyone", "<!everyone>", []Block{para(bcast("everyone"))}},
		{"other special", "<!foo|Foo>", []Block{para(tx("Foo"))}},
		{"date", "<!date^1700000000^{date_num} at {time}|fallback>", []Block{para(Span{Kind: Date, Text: date, ID: "1700000000"})}},
		{"date link", "<!date^1700000000^{date_num} at {time}^https://d.test|f>", []Block{para(Span{Kind: Date, Text: date, ID: "1700000000", URL: "https://d.test"})}},
		{"date bad", "<!date^nope^{date}|Fallback>", []Block{para(tx("Fallback"))}},
		{"escapes", "a &amp; b &lt;c&gt; &amp;lt;", []Block{para(tx("a & b <c> &lt;"))}},
		{"escaped not token", "&lt;@U1&gt;", []Block{para(tx("<@U1>"))}},
		{"emoji", ":smile: hi", []Block{para(em("smile"), tx(" hi"))}},
		{"emoji skin", ":wave::skin-tone-3:", []Block{para(em("wave::skin-tone-3"))}},
		{"emoji adjacent", ":smile::wave:", []Block{para(em("smile"), em("wave"))}},
		{"emoji plus", ":+1:", []Block{para(em("+1"))}},
		{"emoji underscore not italic", "_:white_check_mark: done_", []Block{para(with(em("white_check_mark"), Italic), st(" done", Italic))}},
		{"custom emoji", "hi :parrot:", []Block{para(tx("hi "), em("parrot"))}},
		{"unknown emoji", ":nope: :smile:", []Block{para(tx(":nope: "), em("smile"))}},
		{"time not emoji", "at 10:30:00", []Block{para(tx("at 10:30:00"))}},
		{"lines", "line1\nline2\n\nline4", []Block{para(tx("line1"), br(), tx("line2"), br(), br(), tx("line4"))}},
		{"style per line", "*a\nb*", []Block{para(tx("*a"), br(), tx("b*"))}},
		{"quote", "&gt; quoted *b*\nnot", []Block{quote(tx("quoted "), st("b", Bold)), para(tx("not"))}},
		{"quote raw gt", "> q", []Block{quote(tx("q"))}},
		{"quote multi", "&gt; a\n&gt; b", []Block{quote(tx("a"), br(), tx("b"))}},
		{"quote rest", "x\n&gt;&gt;&gt; a\nb", []Block{para(tx("x")), quote(tx("a"), br(), tx("b"))}},
		{"gt mid-line", "a &gt; b", []Block{para(tx("a > b"))}},
		{"pre", "before\n```\ncode *x*\n<@U1> :smile:\n```\nafter", []Block{
			para(tx("before")), pre(tx("code *x*"), br(), user("U1", "@alice"), tx(" :smile:")), para(tx("after")),
		}},
		{"pre inline", "look ```a &amp; b``` ok", []Block{para(tx("look ")), pre(tx("a & b")), para(tx(" ok"))}},
		{"pre only", "```x```", []Block{pre(tx("x"))}},
		{"two pres", "```a```\n```b```", []Block{pre(tx("a")), pre(tx("b"))}},
		{"unterminated fence", "```unterminated", []Block{para(tx("```unterminated"))}},
		{"third fence literal", "```a``` b ```c", []Block{pre(tx("a")), para(tx(" b ```c"))}},
		{"unicode", "héllo *wörld* 日本", []Block{para(tx("héllo "), st("wörld", Bold), tx(" 日本"))}},
		{"unclosed token", "a <b", []Block{para(tx("a <b"))}},
		{"space token", "< not a token>", []Block{para(tx("< not a token>"))}},
	}
	for _, c := range cases {
		got := FromMrkdwn(R, c.in)
		eq(t, c.name, got, c.want)
		checkInvariants(t, c.name, got)
	}
}

func TestOnlyEmoji(t *testing.T) {
	cases := map[string]bool{
		":smile:":                     true,
		" :smile: :wave:\n:+1: ":      true,
		":smile::skin-tone-3:":        false, // not in the fake table
		":wave::skin-tone-3:":         true,
		":smile: hi":                  false,
		":nope:":                      false,
		"":                            false,
		"   ":                         false,
		strings.Repeat(":smile:", 23): true,
		strings.Repeat(":smile:", 24): false,
	}
	for in, want := range cases {
		if got := onlyEmoji(R, in); got != want {
			t.Errorf("onlyEmoji(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMrkdwnFuzzish(t *testing.T) {
	// Arbitrary inputs must not panic and must keep the invariants.
	inputs := []string{"*", "_", "~", "`", "<", ">", "&", ":", "::", ":::skin-tone-2:", "<>", "<|>", "<@>", "<#>",
		"<!date^>", "<!date^1^>", "```", "``````", "````", "&gt;", "&gt;&gt;&gt;", "*_~`<:&", "\n\n\n", "a\n```\n", "<!subteam^>",
		"*a_b~c*d_e~", "_*_*_*", "😀*😀*", "\xff\xfe*x*"}
	for _, in := range inputs {
		got := FromMrkdwn(R, in)
		checkInvariants(t, fmt.Sprintf("%q", in), got)
		_ = PlainText(Doc{Blocks: got})
	}
}

func msg(t *testing.T, js string) *slack.Message {
	t.Helper()
	var m slack.Message
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatalf("bad fixture: %v\n%s", err, js)
	}
	return &m
}
