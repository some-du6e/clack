package composer

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// btDoc builds test documents through the public Doc editing methods.
type btDoc struct{ d *Doc }

func btNew() *btDoc { return &btDoc{d: NewDoc()} }

// t appends text with style st.
func (b *btDoc) t(s string, st Style) *btDoc {
	p := b.d.Len()
	e := b.d.Insert(p, s, Attr{})
	if st != 0 {
		b.d.SetStyle(p, e, st, true)
	}
	return b
}

// link appends text linked to url.
func (b *btDoc) link(s, url string, st Style) *btDoc {
	p := b.d.Len()
	e := b.d.Insert(p, s, Attr{})
	b.d.SetLink(p, e, url)
	if st != 0 {
		b.d.SetStyle(p, e, st, true)
	}
	return b
}

func (b *btDoc) atom(a *Atom) *btDoc {
	b.d.InsertAtom(b.d.Len(), a)
	return b
}

// nl starts a new line (which copies the current line's format).
func (b *btDoc) nl() *btDoc {
	b.d.Insert(b.d.Len(), "\n", Attr{})
	return b
}

// kind sets the format of the last line.
func (b *btDoc) kind(k BlockKind, indent int) *btDoc {
	n := len(b.d.Lines) - 1
	b.d.SetLineKind(n, n, k)
	b.d.Lines[n].Indent = indent
	return b
}

const btBlockID = "Bk1d0"

func btStubBlockID(t *testing.T) {
	t.Helper()
	old := newBlockID
	newBlockID = func() string { return btBlockID }
	t.Cleanup(func() { newBlockID = old })
}

func btWrap(els string) string {
	return `[{"type":"rich_text","block_id":"` + btBlockID + `","elements":[` + els + `]}]`
}

var (
	btUser   = &Atom{Kind: AtomUser, ID: "U123", Text: "@alice"}
	btChan   = &Atom{Kind: AtomChannel, ID: "C456", Text: "#general"}
	btHere   = &Atom{Kind: AtomBroadcast, ID: "here", Text: "@here"}
	btGroup  = &Atom{Kind: AtomUsergroup, ID: "S789", Text: "@devs"}
	btThumbs = &Atom{Kind: AtomEmoji, ID: "thumbsup", Text: ":thumbsup:", Uni: "\U0001F44D"}
	btWave   = &Atom{Kind: AtomEmoji, ID: "wave::skin-tone-3", Text: ":wave::skin-tone-3:", Uni: "\U0001F44B\U0001F3FC"}
	btParrot = &Atom{Kind: AtomEmoji, ID: "partyparrot", Text: ":partyparrot:", URL: "https://emoji.example/partyparrot.gif"}
)

type btCase struct {
	name   string
	doc    func() *Doc
	blocks string // rich_text elements, wrapped by btWrap
	mrkdwn string
}

func btCases() []btCase {
	return []btCase{
		{
			name:   "plain",
			doc:    func() *Doc { return btNew().t("hello world", 0).d },
			blocks: `{"type":"rich_text_section","elements":[{"type":"text","text":"hello world"}]}`,
			mrkdwn: "hello world",
		},
		{
			name:   "blank line between",
			doc:    func() *Doc { return btNew().t("first\n\nsecond", 0).d },
			blocks: `{"type":"rich_text_section","elements":[{"type":"text","text":"first\n\nsecond"}]}`,
			mrkdwn: "first\n\nsecond",
		},
		{
			name: "bold italic overlap",
			doc: func() *Doc {
				d := btNew().t("one two three", 0).d
				d.SetStyle(0, 7, Bold, true)
				d.SetStyle(4, 13, Italic, true)
				return d
			},
			blocks: `{"type":"rich_text_section","elements":[` +
				`{"type":"text","text":"one ","style":{"bold":true}},` +
				`{"type":"text","text":"two","style":{"bold":true,"italic":true}},` +
				`{"type":"text","text":" three","style":{"italic":true}}]}`,
			mrkdwn: "*one _two_* _three_",
		},
		{
			name:   "bold across lines",
			doc:    func() *Doc { return btNew().t("bold one\nbold two", Bold).d },
			blocks: `{"type":"rich_text_section","elements":[{"type":"text","text":"bold one","style":{"bold":true}},{"type":"text","text":"\n"},{"type":"text","text":"bold two","style":{"bold":true}}]}`,
			mrkdwn: "*bold one*\n*bold two*",
		},
		{
			name: "link over text",
			doc: func() *Doc {
				return btNew().t("see ", 0).link("the docs", "https://example.com/docs?a=1&b=2", 0).t(" now", 0).d
			},
			blocks: `{"type":"rich_text_section","elements":[` +
				`{"type":"text","text":"see "},` +
				`{"type":"link","url":"https://example.com/docs?a=1&b=2","text":"the docs"},` +
				`{"type":"text","text":" now"}]}`,
			mrkdwn: "see <https://example.com/docs?a=1&amp;b=2|the docs> now",
		},
		{
			name: "styled link",
			doc: func() *Doc {
				return btNew().link("docs", "https://example.com", Bold|Italic).d
			},
			blocks: `{"type":"rich_text_section","elements":[{"type":"link","url":"https://example.com","text":"docs","style":{"bold":true,"italic":true}}]}`,
			mrkdwn: "*_<https://example.com|docs>_*",
		},
		{
			name: "bare url",
			doc: func() *Doc {
				return btNew().t("read https://example.com/x, then www.example.org.", 0).d
			},
			blocks: `{"type":"rich_text_section","elements":[` +
				`{"type":"text","text":"read "},` +
				`{"type":"link","url":"https://example.com/x"},` +
				`{"type":"text","text":", then "},` +
				`{"type":"link","url":"http://www.example.org","text":"www.example.org"},` +
				`{"type":"text","text":"."}]}`,
			mrkdwn: "read https://example.com/x, then www.example.org.",
		},
		{
			name: "atoms",
			doc: func() *Doc {
				return btNew().t("hi ", 0).atom(btUser).t(" in ", 0).atom(btChan).t(" ", 0).atom(btHere).
					t(" ", 0).atom(btThumbs).atom(btWave).t(" ", 0).atom(btGroup).atom(btParrot).d
			},
			blocks: `{"type":"rich_text_section","elements":[` +
				`{"type":"text","text":"hi "},` +
				`{"type":"user","user_id":"U123"},` +
				`{"type":"text","text":" in "},` +
				`{"type":"channel","channel_id":"C456"},` +
				`{"type":"text","text":" "},` +
				`{"type":"broadcast","range":"here"},` +
				`{"type":"text","text":" "},` +
				`{"type":"emoji","name":"thumbsup","unicode":"1f44d"},` +
				`{"type":"emoji","name":"wave","unicode":"1f44b-1f3fc","skin_tone":3},` +
				`{"type":"text","text":" "},` +
				`{"type":"usergroup","usergroup_id":"S789"},` +
				`{"type":"emoji","name":"partyparrot"}]}`,
			mrkdwn: "hi <@U123> in <#C456> <!here> :thumbsup::wave::skin-tone-3: <!subteam^S789>:partyparrot:",
		},
		{
			name: "nested bullets then paragraph",
			doc: func() *Doc {
				return btNew().kind(Bullet, 0).t("alpha", 0).
					nl().kind(Bullet, 1).t("beta", 0).
					nl().t("gamma", 0).
					nl().kind(Bullet, 0).t("delta", 0).
					nl().kind(Para, 0).t("after", 0).d
			},
			blocks: `{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"alpha"}]}],"style":"bullet","indent":0,"border":0},` +
				`{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"beta"}]},{"type":"rich_text_section","elements":[{"type":"text","text":"gamma"}]}],"style":"bullet","indent":1,"border":0},` +
				`{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"delta"}]}],"style":"bullet","indent":0,"border":0},` +
				`{"type":"rich_text_section","elements":[{"type":"text","text":"after"}]}`,
			mrkdwn: "• alpha\n    • beta\n    • gamma\n• delta\nafter",
		},
		{
			name: "ordered list with resumed numbering",
			doc: func() *Doc {
				return btNew().kind(Ordered, 0).t("one", 0).
					nl().t("two", 0).
					nl().kind(Ordered, 1).t("sub", 0).
					nl().kind(Ordered, 0).t("three", 0).d
			},
			blocks: `{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"one"}]},{"type":"rich_text_section","elements":[{"type":"text","text":"two"}]}],"style":"ordered","indent":0,"border":0},` +
				`{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"sub"}]}],"style":"ordered","indent":1,"border":0},` +
				`{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"three"}]}],"style":"ordered","indent":0,"offset":2,"border":0}`,
			mrkdwn: "1. one\n2. two\n    1. sub\n3. three",
		},
		{
			name: "blank paragraph between lists",
			doc: func() *Doc {
				return btNew().kind(Bullet, 0).t("a", 0).
					nl().kind(Para, 0).
					nl().kind(Bullet, 0).t("b", 0).d
			},
			blocks: `{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"a"}]}],"style":"bullet","indent":0,"border":0},` +
				`{"type":"rich_text_section","elements":[{"type":"text","text":"\n"}]},` +
				`{"type":"rich_text_list","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"b"}]}],"style":"bullet","indent":0,"border":0}`,
			mrkdwn: "• a\n\n• b",
		},
		{
			name: "quote",
			doc: func() *Doc {
				return btNew().kind(Quote, 0).t("to be", 0).
					nl().t("or ", 0).t("not", Italic).
					nl().kind(Para, 0).t("said someone", 0).d
			},
			blocks: `{"type":"rich_text_quote","elements":[{"type":"text","text":"to be\nor "},{"type":"text","text":"not","style":{"italic":true}}]},` +
				`{"type":"rich_text_section","elements":[{"type":"text","text":"said someone"}]}`,
			mrkdwn: "> to be\n> or _not_\nsaid someone",
		},
		{
			name: "code block between paragraphs",
			doc: func() *Doc {
				d := btNew().t("before\nx := 1 < 2\ny && z\nafter", 0).d
				d.SetLineKind(1, 2, CodeBlock)
				return d
			},
			blocks: `{"type":"rich_text_section","elements":[{"type":"text","text":"before"}]},` +
				`{"type":"rich_text_preformatted","elements":[{"type":"text","text":"x := 1 < 2\ny && z"}],"border":0},` +
				`{"type":"rich_text_section","elements":[{"type":"text","text":"after"}]}`,
			mrkdwn: "before\n```\nx := 1 &lt; 2\ny &amp;&amp; z\n```\nafter",
		},
		{
			name: "inline code",
			doc: func() *Doc {
				return btNew().t("run ", 0).t("curl https://example.com", Code).t(" now", 0).d
			},
			blocks: `{"type":"rich_text_section","elements":[` +
				`{"type":"text","text":"run "},` +
				`{"type":"text","text":"curl https://example.com","style":{"code":true}},` +
				`{"type":"text","text":" now"}]}`,
			mrkdwn: "run `curl https://example.com` now",
		},
		{
			name: "escaping underline strike",
			doc: func() *Doc {
				return btNew().t("x < y & z > w ", 0).t("under", Underline).t(" ", 0).t("gone", Strike).d
			},
			blocks: `{"type":"rich_text_section","elements":[` +
				`{"type":"text","text":"x < y & z > w "},` +
				`{"type":"text","text":"under","style":{"underline":true}},` +
				`{"type":"text","text":" "},` +
				`{"type":"text","text":"gone","style":{"strike":true}}]}`,
			mrkdwn: "x &lt; y &amp; z &gt; w under ~gone~",
		},
	}
}

// TestHeadingDividerBlocks covers the header and divider block types: the
// mixed top-level array, the plain-text flattening of headings, the mrkdwn
// fallback and (where lossless) the round trip.
func TestHeadingDividerBlocks(t *testing.T) {
	btStubBlockID(t)
	id := btBlockID
	hdr := func(level int, text string) string {
		return `{"type":"header","block_id":"` + id + `","text":{"type":"plain_text","text":"` + text + `","emoji":true},"level":` + strconv.Itoa(level) + `}`
	}
	div := `{"type":"divider","block_id":"` + id + `"}`
	rt := func(els string) string {
		return `{"type":"rich_text","block_id":"` + id + `","elements":[` + els + `]}`
	}
	sect := func(text string) string {
		return `{"type":"rich_text_section","elements":[{"type":"text","text":"` + text + `"}]}`
	}
	cases := []struct {
		name      string
		doc       func() *Doc
		blocks    string
		mrkdwn    string
		roundTrip bool
	}{
		{
			name:      "big heading then divider (ground truth)",
			doc:       func() *Doc { return btNew().kind(Header1, 0).t("Title", 0).nl().kind(Divider, 0).d },
			blocks:    "[" + hdr(1, "Title") + "," + div + "]",
			mrkdwn:    "*Title*\n" + rtDividerMrkdwn,
			roundTrip: true,
		},
		{
			name: "heading divider paragraph",
			doc: func() *Doc {
				return btNew().kind(Header1, 0).t("Heading", 0).nl().kind(Divider, 0).nl().kind(Para, 0).t("Body", 0).d
			},
			blocks:    "[" + hdr(1, "Heading") + "," + div + "," + rt(sect("Body")) + "]",
			mrkdwn:    "*Heading*\n" + rtDividerMrkdwn + "\nBody",
			roundTrip: true,
		},
		{
			name:      "divider between paragraphs",
			doc:       func() *Doc { return btNew().t("a", 0).nl().kind(Divider, 0).nl().kind(Para, 0).t("b", 0).d },
			blocks:    "[" + rt(sect("a")) + "," + div + "," + rt(sect("b")) + "]",
			mrkdwn:    "a\n" + rtDividerMrkdwn + "\nb",
			roundTrip: true,
		},
		{
			name: "all heading levels",
			doc: func() *Doc {
				return btNew().kind(Header1, 0).t("H1", 0).nl().kind(Header2, 0).t("H2", 0).nl().kind(Header3, 0).t("H3", 0).d
			},
			blocks:    "[" + hdr(1, "H1") + "," + hdr(2, "H2") + "," + hdr(3, "H3") + "]",
			mrkdwn:    "*H1*\n*H2*\n*H3*",
			roundTrip: true,
		},
		{
			name:   "heading flattens atoms",
			doc:    func() *Doc { return btNew().kind(Header1, 0).t("Hi ", 0).atom(btUser).t(" ", 0).atom(btThumbs).d },
			blocks: "[" + hdr(1, "Hi @alice \U0001F44D") + "]",
			mrkdwn: "*Hi @alice \U0001F44D*",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.doc().RichTextBlocks()
			if !json.Valid(got) {
				t.Fatalf("invalid JSON: %s", got)
			}
			if string(got) != c.blocks {
				t.Errorf("blocks mismatch\n got: %s\nwant: %s", got, c.blocks)
			}
			if m := c.doc().Mrkdwn(); m != c.mrkdwn {
				t.Errorf("mrkdwn mismatch\n got: %q\nwant: %q", m, c.mrkdwn)
			}
			if !c.roundTrip {
				return
			}
			back, err := DocFromBlocksJSON(got, nil)
			if err != nil {
				t.Fatalf("DocFromBlocksJSON: %v", err)
			}
			btSameDoc(t, back, c.doc())
			if again := string(back.RichTextBlocks()); again != c.blocks {
				t.Errorf("re-serialized differ\n got: %s\nwant: %s", again, c.blocks)
			}
		})
	}
}

func TestRichTextBlocksGolden(t *testing.T) {
	btStubBlockID(t)
	for _, c := range btCases() {
		t.Run(c.name, func(t *testing.T) {
			got := c.doc().RichTextBlocks()
			if !json.Valid(got) {
				t.Fatalf("invalid JSON: %s", got)
			}
			if want := btWrap(c.blocks); string(got) != want {
				t.Errorf("blocks mismatch\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

func TestMrkdwnGolden(t *testing.T) {
	for _, c := range btCases() {
		t.Run(c.name, func(t *testing.T) {
			if got := c.doc().Mrkdwn(); got != c.mrkdwn {
				t.Errorf("mrkdwn mismatch\n got: %q\nwant: %q", got, c.mrkdwn)
			}
		})
	}
}

func TestBlocksRoundTrip(t *testing.T) {
	for _, c := range btCases() {
		t.Run(c.name, func(t *testing.T) {
			want := c.doc()
			raw := want.RichTextBlocks()
			got, err := DocFromBlocksJSON(raw, nil)
			if err != nil {
				t.Fatalf("DocFromBlocksJSON: %v", err)
			}
			btSameDoc(t, got, want)
			if again := got.RichTextBlocks(); btStripID(again) != btStripID(raw) {
				t.Errorf("re-serialized blocks differ\n got: %s\nwant: %s", again, raw)
			}
		})
	}
}

// btStripID removes the random block_id so two serializations compare.
func btStripID(raw json.RawMessage) string {
	s := string(raw)
	const key = `"block_id":"`
	i := strings.Index(s, key)
	if i < 0 {
		return s
	}
	j := strings.IndexByte(s[i+len(key):], '"')
	return s[:i] + s[i+len(key)+j+1:]
}

// btSameDoc compares runes, styles, links, atom kinds/ids and line formats.
func btSameDoc(t *testing.T, got, want *Doc) {
	t.Helper()
	if string(got.Runes) != string(want.Runes) {
		t.Fatalf("runes: got %q, want %q", string(got.Runes), string(want.Runes))
	}
	if len(got.Attrs) != len(got.Runes) {
		t.Fatalf("got %d attrs for %d runes", len(got.Attrs), len(got.Runes))
	}
	if n := strings.Count(string(got.Runes), "\n") + 1; len(got.Lines) != n {
		t.Fatalf("got %d lines for %d newline-separated lines", len(got.Lines), n)
	}
	if len(got.Lines) != len(want.Lines) {
		t.Fatalf("lines: got %v, want %v", got.Lines, want.Lines)
	}
	for i := range want.Lines {
		if got.Lines[i] != want.Lines[i] {
			t.Errorf("line %d: got %+v, want %+v", i, got.Lines[i], want.Lines[i])
		}
	}
	for i := range want.Attrs {
		g, w := got.Attrs[i], want.Attrs[i]
		if g.Style != w.Style || g.Link != w.Link {
			t.Errorf("attr %d (%q): got style %b link %q, want style %b link %q", i, want.Runes[i], g.Style, g.Link, w.Style, w.Link)
		}
		if (g.Atom == nil) != (w.Atom == nil) {
			t.Errorf("attr %d: atom presence got %v, want %v", i, g.Atom != nil, w.Atom != nil)
			continue
		}
		if w.Atom != nil && (g.Atom.Kind != w.Atom.Kind || g.Atom.ID != w.Atom.ID) {
			t.Errorf("attr %d: atom got %+v, want %+v", i, *g.Atom, *w.Atom)
		}
	}
}

func TestRichTextBlocksTrimsBlankLines(t *testing.T) {
	btStubBlockID(t)
	d := btNew().t("\n\nhello\n \n", 0).d
	want := btWrap(`{"type":"rich_text_section","elements":[{"type":"text","text":"hello"}]}`)
	if got := string(d.RichTextBlocks()); got != want {
		t.Errorf("blocks\n got: %s\nwant: %s", got, want)
	}
	if got := d.Mrkdwn(); got != "hello" {
		t.Errorf("mrkdwn: got %q", got)
	}
}

func TestRichTextBlocksEmpty(t *testing.T) {
	for _, d := range []*Doc{NewDoc(), btNew().t("\n  \n", 0).d} {
		if got := d.RichTextBlocks(); got != nil {
			t.Errorf("empty doc: got %s, want nil", got)
		}
		if got := d.Mrkdwn(); got != "" {
			t.Errorf("empty doc mrkdwn: got %q", got)
		}
	}
}

func TestNewBlockID(t *testing.T) {
	id := newBlockID()
	if len(id) != 5 {
		t.Fatalf("block id %q: want 5 chars", id)
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			t.Fatalf("block id %q: unexpected char %q", id, r)
		}
	}
}

func TestFindURLs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"https://a.example/x.", []string{"https://a.example/x"}},
		{"(see https://en.example.org/wiki/Foo_(bar)) ok", []string{"https://en.example.org/wiki/Foo_(bar)"}},
		{"nohttps://x.example", nil},
		{"http:// nothing", nil},
		{"HTTPS://Example.com/A", []string{"HTTPS://Example.com/A"}},
		{"a www.b.example b", []string{"http://www.b.example"}},
		{"www.nodot", nil},
		{"two http://a.example and https://b.example!", []string{"http://a.example", "https://b.example"}},
	}
	for _, c := range cases {
		var got []string
		for _, m := range rtFindURLs([]rune(c.in)) {
			got = append(got, m.url)
		}
		if strings.Join(got, " ") != strings.Join(c.want, " ") || len(got) != len(c.want) {
			t.Errorf("rtFindURLs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

type btResolver struct{}

func (btResolver) UserName(id string) string {
	return map[string]string{"U1": "alice"}[id]
}

func (btResolver) ChannelName(id string) string {
	return map[string]string{"C1": "general"}[id]
}

func (btResolver) UsergroupName(id string) string {
	return map[string]string{"S1": "devs"}[id]
}

func (btResolver) Emoji(name string) (url, text string, ok bool) {
	switch name {
	case "smile":
		return "https://cdn.example/1f604.png", "\U0001F604", true
	case "partyparrot":
		return "https://emoji.example/partyparrot.gif", "", true
	}
	return "", "", false
}

func TestDocFromBlocksJSONSlackShaped(t *testing.T) {
	raw := json.RawMessage(`[{"type":"rich_text","block_id":"x1","elements":[
		{"type":"rich_text_section","elements":[
			{"type":"text","text":"hey "},{"type":"user","user_id":"U1"},
			{"type":"text","text":" see "},{"type":"channel","channel_id":"C1"},
			{"type":"text","text":" "},{"type":"usergroup","usergroup_id":"S1"},
			{"type":"text","text":" "},{"type":"user","user_id":"U9"},
			{"type":"text","text":" "},{"type":"emoji","name":"smile","unicode":"1f604"},{"type":"emoji","name":"partyparrot"},
			{"type":"emoji","name":"wave","unicode":"1f44b-1f3fd","skin_tone":4},
			{"type":"text","text":" on "},{"type":"date","timestamp":1700000000,"format":"{date_short}","fallback":"Nov 14"},
			{"type":"text","text":"\n"}]},
		{"type":"rich_text_list","style":"ordered","indent":1,"border":0,"elements":[
			{"type":"rich_text_section","elements":[{"type":"text","text":"x","style":{"bold":true,"highlight":true}}]},
			{"type":"rich_text_section","elements":[]}]},
		{"type":"rich_text_preformatted","border":0,"elements":[
			{"type":"text","text":"a "},{"type":"link","url":"https://example.com"},{"type":"text","text":"\nb"}]}
	]},{"type":"section","text":{"type":"mrkdwn","text":"ignored"}}]`)
	d, err := DocFromBlocksJSON(raw, btResolver{})
	if err != nil {
		t.Fatal(err)
	}
	wantText := "hey @alice see #general @devs @U9 \U0001F604:partyparrot:\U0001F44B\U0001F3FD on Nov 14\n\nx\n\na https://example.com\nb"
	if got := d.Text(); got != wantText {
		t.Errorf("text\n got: %q\nwant: %q", got, wantText)
	}
	wantLines := []Line{{Kind: Para}, {Kind: Para}, {Kind: Ordered, Indent: 1}, {Kind: Ordered, Indent: 1}, {Kind: CodeBlock}, {Kind: CodeBlock}}
	if len(d.Lines) != len(wantLines) {
		t.Fatalf("lines: got %+v, want %+v", d.Lines, wantLines)
	}
	for i := range wantLines {
		if d.Lines[i] != wantLines[i] {
			t.Errorf("line %d: got %+v, want %+v", i, d.Lines[i], wantLines[i])
		}
	}
	var atoms []*Atom
	for i, a := range d.Attrs {
		if a.Atom != nil {
			atoms = append(atoms, a.Atom)
		}
		if a.Link != "" {
			t.Errorf("rune %d: unexpected link %q (code block)", i, a.Link)
		}
	}
	if x := d.LineStart(2); d.Runes[x] != 'x' || d.Attrs[x].Style != Bold {
		t.Errorf("list item: %q style %b, want bold x", d.Runes[x], d.Attrs[x].Style)
	}
	if len(atoms) != 7 {
		t.Fatalf("got %d atoms, want 7", len(atoms))
	}
	if a := atoms[0]; a.Kind != AtomUser || a.ID != "U1" || a.Text != "@alice" {
		t.Errorf("user atom: %+v", *a)
	}
	if a := atoms[1]; a.Kind != AtomChannel || a.ID != "C1" || a.Text != "#general" {
		t.Errorf("channel atom: %+v", *a)
	}
	if a := atoms[2]; a.Kind != AtomUsergroup || a.ID != "S1" || a.Text != "@devs" {
		t.Errorf("usergroup atom: %+v", *a)
	}
	if a := atoms[3]; a.Kind != AtomUser || a.ID != "U9" || a.Text != "@U9" {
		t.Errorf("unresolved user atom: %+v", *a)
	}
	if a := atoms[4]; a.Kind != AtomEmoji || a.ID != "smile" || a.Text != ":smile:" || a.Uni != "\U0001F604" || a.URL != "https://cdn.example/1f604.png" {
		t.Errorf("smile atom: %+v", *a)
	}
	if a := atoms[5]; a.Kind != AtomEmoji || a.ID != "partyparrot" || a.Uni != "" || a.URL != "https://emoji.example/partyparrot.gif" {
		t.Errorf("parrot atom: %+v", *a)
	}
	if a := atoms[6]; a.Kind != AtomEmoji || a.ID != "wave::skin-tone-4" || a.Text != ":wave::skin-tone-4:" || a.Uni != "\U0001F44B\U0001F3FD" {
		t.Errorf("wave atom: %+v", *a)
	}
}

func TestDocFromBlocksJSONEdgeCases(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", `[{"type":"divider"}]`} {
		d, err := DocFromBlocksJSON(json.RawMessage(raw), nil)
		if err != nil {
			t.Errorf("%q: %v", raw, err)
			continue
		}
		if d.Len() != 0 || len(d.Lines) != 1 {
			t.Errorf("%q: want empty doc, got %q %+v", raw, string(d.Runes), d.Lines)
		}
	}
	if _, err := DocFromBlocksJSON(json.RawMessage(`[{"type":`), nil); err == nil {
		t.Error("truncated JSON: want error")
	}
	// Leading and trailing blank lines are dropped.
	d, err := DocFromBlocksJSON(json.RawMessage(`[{"type":"rich_text","elements":[
		{"type":"rich_text_section","elements":[{"type":"text","text":"\n\nhi\n"}]}]}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(d.Runes) != "hi" || len(d.Lines) != 1 {
		t.Errorf("trim: got %q %+v", string(d.Runes), d.Lines)
	}
}

func TestDocFromRichtext(t *testing.T) {
	blocks := []richtext.Block{
		{Kind: richtext.Paragraph, Spans: []richtext.Span{
			{Kind: richtext.Text, Text: "hello ", Style: richtext.Bold | richtext.Highlight},
			{Kind: richtext.User, Text: "alice", ID: "U1"},
			{Kind: richtext.Text, Text: " see "},
			{Kind: richtext.Link, Text: "docs", URL: "https://example.com/docs"},
			{Kind: richtext.Text, Text: " "},
			{Kind: richtext.Emoji, Text: "\U0001F44D", ID: "thumbsup", URL: "https://cdn.example/1f44d.png"},
			{Kind: richtext.Emoji, Text: ":parrot:", ID: "parrot", URL: "https://emoji.example/parrot.gif"},
			{Kind: richtext.Break},
			{Kind: richtext.Text, Text: "line two "},
			{Kind: richtext.Broadcast, Text: "here", Self: true},
			{Kind: richtext.Text, Text: " "},
			{Kind: richtext.Channel, Text: "#general", ID: "C1"},
			{Kind: richtext.Text, Text: " on "},
			{Kind: richtext.Date, Text: "Nov 14"},
			{Kind: richtext.Text, Text: " "},
			{Kind: richtext.Link, Text: "https://example.com/bare", URL: "https://example.com/bare"},
		}},
		{Kind: richtext.ImageBlock, Image: &richtext.Image{URL: "https://img.example/a.png"}},
		{Kind: richtext.List, Ordered: true, Indent: 1, Items: [][]richtext.Span{
			{{Kind: richtext.Text, Text: "first"}},
			{{Kind: richtext.Text, Text: "second", Style: richtext.Italic}},
		}},
		{Kind: richtext.Quote, Spans: []richtext.Span{{Kind: richtext.Text, Text: "quoted\nmore"}}},
		{Kind: richtext.Preformatted, Spans: []richtext.Span{{Kind: richtext.Text, Text: "code\nblock", Style: richtext.Code}}},
		{Kind: richtext.Divider},
		{Kind: richtext.Header, Spans: []richtext.Span{{Kind: richtext.Text, Text: "Title"}}},
	}
	d := DocFromRichtext(blocks)
	wantText := "hello @alice see docs \U0001F44D:parrot:\nline two @here #general on Nov 14 https://example.com/bare\nfirst\nsecond\nquoted\nmore\ncode\nblock\n\nTitle"
	if got := d.Text(); got != wantText {
		t.Errorf("text\n got: %q\nwant: %q", got, wantText)
	}
	wantLines := []Line{
		{Kind: Para}, {Kind: Para},
		{Kind: Ordered, Indent: 1}, {Kind: Ordered, Indent: 1},
		{Kind: Quote}, {Kind: Quote},
		{Kind: CodeBlock}, {Kind: CodeBlock},
		{Kind: Divider},
		{Kind: Header1},
	}
	if len(d.Lines) != len(wantLines) {
		t.Fatalf("lines: got %+v, want %+v", d.Lines, wantLines)
	}
	for i := range wantLines {
		if d.Lines[i] != wantLines[i] {
			t.Errorf("line %d: got %+v, want %+v", i, d.Lines[i], wantLines[i])
		}
	}
	if d.Attrs[0].Style != Bold {
		t.Errorf("hello: style %b, want bold", d.Attrs[0].Style)
	}
	// "hello " (6) + atom (1) + " see " (5) = 12: "docs" is [12, 16).
	if s, e, url := d.LinkAt(13); s != 12 || e != 16 || url != "https://example.com/docs" {
		t.Errorf("docs link: [%d,%d) %q", s, e, url)
	}
	var atoms []*Atom
	for i, a := range d.Attrs {
		if a.Atom != nil {
			atoms = append(atoms, a.Atom)
		}
		if a.Link == "https://example.com/bare" {
			t.Errorf("rune %d: bare URL should load as plain text", i)
		}
	}
	if len(atoms) != 5 {
		t.Fatalf("got %d atoms, want 5", len(atoms))
	}
	if a := atoms[0]; a.Kind != AtomUser || a.ID != "U1" || a.Text != "@alice" {
		t.Errorf("user atom: %+v", *a)
	}
	if a := atoms[1]; a.Kind != AtomEmoji || a.ID != "thumbsup" || a.Text != ":thumbsup:" || a.Uni != "\U0001F44D" || a.URL != "https://cdn.example/1f44d.png" {
		t.Errorf("thumbsup atom: %+v", *a)
	}
	if a := atoms[2]; a.Kind != AtomEmoji || a.ID != "parrot" || a.Text != ":parrot:" || a.Uni != "" || a.URL != "https://emoji.example/parrot.gif" {
		t.Errorf("parrot atom: %+v", *a)
	}
	if a := atoms[3]; a.Kind != AtomBroadcast || a.ID != "here" || a.Text != "@here" {
		t.Errorf("broadcast atom: %+v", *a)
	}
	if a := atoms[4]; a.Kind != AtomChannel || a.ID != "C1" || a.Text != "#general" {
		t.Errorf("channel atom: %+v", *a)
	}
	start := d.LineStart(3)
	for i := start; i < start+len("second"); i++ {
		if d.Attrs[i].Style != Italic {
			t.Errorf("second[%d]: style %b, want italic", i-start, d.Attrs[i].Style)
		}
	}
	for i := d.LineStart(6); i < d.LineEnd(7); i++ {
		if d.Attrs[i].Style != 0 {
			t.Errorf("code block rune %d: style %b, want none", i, d.Attrs[i].Style)
		}
	}
}
