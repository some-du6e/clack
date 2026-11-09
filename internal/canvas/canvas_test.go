package canvas

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

func kitchen(t *testing.T) Doc {
	t.Helper()
	b, err := os.ReadFile("testdata/kitchen.html")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Parse(string(b))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func kinds(bs []Block) []Kind {
	var out []Kind
	for _, b := range bs {
		out = append(out, b.Kind)
	}
	return out
}

func TestParseKitchenSink(t *testing.T) {
	d := kitchen(t)
	want := []Kind{Heading1, Paragraph, Paragraph, Paragraph, Heading2,
		Bullet, Bullet, Bullet, Bullet,
		Numbered, Numbered, Numbered, Numbered,
		Heading3, Check, Check, Check,
		Quote, Code, Divider, Table, Columns, Embed, File, Callout, Paragraph, Paragraph}
	if got := kinds(d.Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds\n got %v\nwant %v", got, want)
	}
	b := d.Blocks
	if b[0].ID != "temp:C:KS1h1" || b[0].Spans[len(b[0].Spans)-1].Kind != richtext.Emoji || b[0].Spans[len(b[0].Spans)-1].ID != "rocket" {
		t.Errorf("h1 = %+v", b[0])
	}
	// inline styles and mentions
	p := b[1].Spans
	find := func(sp []richtext.Span, text string) *richtext.Span {
		for i := range sp {
			if sp[i].Text == text {
				return &sp[i]
			}
		}
		t.Fatalf("no span %q in %+v", text, sp)
		return nil
	}
	if s := find(p, "bold"); s.Style != richtext.Bold {
		t.Errorf("bold style %v", s.Style)
	}
	if s := find(p, "italic"); s.Style != richtext.Italic {
		t.Errorf("italic %v", s.Style)
	}
	if s := find(p, "struck"); s.Style != richtext.Strike {
		t.Errorf("strike %v", s.Style)
	}
	if s := find(p, "underlined"); s.Style != richtext.Underline {
		t.Errorf("underline %v", s.Style)
	}
	if s := find(p, "inline code"); s.Style != richtext.Code {
		t.Errorf("code %v", s.Style)
	}
	if s := find(p, "@ada.lovelace"); s.Kind != richtext.User {
		t.Errorf("user mention %+v", s)
	}
	if s := find(p, "#general"); s.Kind != richtext.Channel {
		t.Errorf("channel mention %+v", s)
	}
	if s := find(p, "@here"); s.Kind != richtext.Broadcast || s.ID != "here" {
		t.Errorf("broadcast %+v", s)
	}
	q := b[2].Spans
	if s := find(q, "link with "); s.Kind != richtext.Link || s.URL != "https://example.com/docs" {
		t.Errorf("link %+v", s)
	}
	if s := find(q, "bold"); s.Kind != richtext.Link || s.Style != richtext.Bold {
		t.Errorf("bold inside link %+v", s)
	}
	if s := find(q, "August 11th"); s.Kind != richtext.Date {
		t.Errorf("date %+v", s)
	}
	if len(b[3].Spans) != 0 {
		t.Errorf("empty paragraph has spans %+v", b[3].Spans)
	}
	// list levels, numbering, checks
	lv := []int{b[5].Level, b[6].Level, b[7].Level, b[8].Level}
	if !reflect.DeepEqual(lv, []int{0, 1, 2, 0}) {
		t.Errorf("bullet levels %v", lv)
	}
	if b[5].ID != "temp:C:KS1b1" || b[5].Text() != "First bullet" {
		t.Errorf("bullet %+v", b[5])
	}
	nums := [][2]int{{b[9].Level, b[9].Number}, {b[10].Level, b[10].Number}, {b[11].Level, b[11].Number}, {b[12].Level, b[12].Number}}
	if !reflect.DeepEqual(nums, [][2]int{{0, 1}, {0, 2}, {1, 1}, {0, 3}}) {
		t.Errorf("numbering %v", nums)
	}
	if !b[14].Checked || b[15].Checked || b[16].Level != 1 {
		t.Errorf("checks %+v %+v %+v", b[14], b[15], b[16])
	}
	if len(b[17].Children) != 1 || b[17].Children[0].Text() != "Quoted wisdom" {
		t.Errorf("quote %+v", b[17])
	}
	if b[18].Code != "func main() {\n    fmt.Println(\"hi <3\")\n}" {
		t.Errorf("code %q", b[18].Code)
	}
	tb := b[20]
	if !tb.Header || len(tb.Rows) != 3 || len(tb.Rows[0]) != 2 || tb.Rows[1][1].Blocks[0].Spans[0].Style != richtext.Bold {
		t.Errorf("table %+v", tb)
	}
	if k := tb.Rows[2][1].Blocks; len(k) != 1 || k[0].Kind != Check {
		t.Errorf("checklist in a cell %+v", k)
	}
	cols := b[21].Columns
	if len(cols) != 2 || cols[0][0].Kind != Image || cols[0][0].FileID != "F0CANVIMG01" || cols[0][0].Image.W != 640 || cols[1][0].Text() != "Beside the picture" {
		t.Errorf("columns %+v", b[21])
	}
	if b[22].URL != "https://example.com/article" {
		t.Errorf("embed %+v", b[22])
	}
	if b[23].FileID != "F0CANVFILE1" || b[23].Title != "report.pdf" {
		t.Errorf("file %+v", b[23])
	}
	if b[24].Emoji != "bulb" || b[24].Children[0].Text() != "Callout text" {
		t.Errorf("callout %+v", b[24])
	}
	last := b[25].Spans
	if s := find(last, "highlighted"); s.Style != richtext.Highlight {
		t.Errorf("highlight %+v", s)
	}
	for _, s := range last {
		if s.Kind == richtext.Link {
			t.Errorf("javascript: link kept: %+v", s)
		}
	}
	if ids := d.FileIDs(); !reflect.DeepEqual(ids, []string{"F0CANVIMG01", "F0CANVFILE1"}) {
		t.Errorf("FileIDs %v", ids)
	}
	if d.Find("temp:C:KS1c2") == nil || d.Find("temp:C:KS1t6") == nil {
		t.Error("Find misses list item / table cell item")
	}
}

func TestParseRefusesHuge(t *testing.T) {
	if _, err := Parse(strings.Repeat("a", MaxHTML+1)); err != ErrTooLarge {
		t.Fatal(err)
	}
	// deep nesting doesn't recurse without bound
	deep := strings.Repeat("<div>", 5000) + "x" + strings.Repeat("</div>", 5000)
	_, _ = Parse(deep) // an error is fine; no stack overflow
}

func TestMarkdownBlocks(t *testing.T) {
	d := kitchen(t)
	b := d.Blocks
	cases := map[int]string{
		0:  "# Launch plan :rocket:",
		1:  "Hello **bold**, _italic_, ~~struck~~, underlined and `inline code`. Ask @ada.lovelace in #general, @here.",
		6:  "  - Nested **bullet**",
		10: "2. Two",
		14: "- [x] Book the venue",
		15: "- [ ] Order [stickers](https://example.com/stickers)",
		17: "> Quoted _wisdom_",
		19: "---",
	}
	for i, want := range cases {
		if got := Markdown(b[i]); got != want {
			t.Errorf("block %d: got %q want %q", i, got, want)
		}
	}
	if got := Markdown(b[18]); got != "```\nfunc main() {\n    fmt.Println(\"hi <3\")\n}\n```" {
		t.Errorf("code %q", got)
	}
	if got := Markdown(b[20]); got != "| Name | Role |\n| --- | --- |\n| Ada | **Lead** |\n| Grace | Review |" {
		t.Errorf("table %q", got)
	}
	// mentions with ids use canvas mention syntax
	sp := []richtext.Span{{Kind: richtext.User, ID: "U123", Text: "@x"}, {Kind: richtext.Text, Text: " and "}, {Kind: richtext.Channel, ID: "C9", Text: "#c"}}
	if got := Inline(sp); got != "![](@U123) and ![](#C9)" {
		t.Errorf("mentions %q", got)
	}
	if got := Inline([]richtext.Span{{Kind: richtext.Text, Text: "# not a *heading*"}}); got != `\# not a \*heading\*` {
		t.Errorf("escape %q", got)
	}
}

type fakeNames map[string]string

func (f fakeNames) UserName(id string) string    { return f[id] }
func (f fakeNames) ChannelName(id string) string { return f[id] }

func TestMarkdownRoundTrip(t *testing.T) {
	src := "# Title\n\nSome **bold** and _it_ with ![](@U1) and ![](#C1) :tada:\n\n- a\n  - b\n- [x] done\n- [ ] todo\n\n1. one\n2. two\n\n> quoted\n\n```\ncode  here\n```\n\n---\n\n| h1 | h2 |\n| --- | --- |\n| c1 | c2 |"
	bs := ParseMarkdown(src, fakeNames{"U1": "ada", "C1": "general"})
	want := []Kind{Heading1, Paragraph, Bullet, Bullet, Check, Check, Numbered, Numbered, Quote, Code, Divider, Table}
	if got := kinds(bs); !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds %v", got)
	}
	if bs[1].Spans[5].Kind != richtext.User || bs[1].Spans[5].Text != "@ada" || bs[1].Spans[5].ID != "U1" {
		t.Errorf("user %+v", bs[1].Spans)
	}
	if bs[3].Level != 1 || !bs[4].Checked || bs[7].Number != 2 {
		t.Errorf("list attrs %+v %+v %+v", bs[3], bs[4], bs[7])
	}
	// markdown -> HTML (Quip shape) -> Parse keeps every block
	h := HTML(Doc{Blocks: bs}, nil)
	d, err := Parse(h)
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(d.Blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("after HTML: %v\n%s", got, h)
	}
	for i := range bs {
		if a, b := Markdown(bs[i]), Markdown(d.Blocks[i]); a != b && bs[i].Kind != Paragraph {
			t.Errorf("block %d: %q vs %q", i, a, b)
		}
	}
	if d.Blocks[1].Text() != bs[1].Text() {
		t.Errorf("paragraph text %q vs %q", d.Blocks[1].Text(), bs[1].Text())
	}
	// and back to markdown, stable
	md := DocMarkdown(d)
	if md2 := DocMarkdown(Doc{Blocks: ParseMarkdown(md, nil)}); md2 != md {
		t.Errorf("unstable markdown:\n%s\n---\n%s", md, md2)
	}
}

// Only blocks that survive canvases.edit markdown unchanged are editable.
func TestEditable(t *testing.T) {
	d := kitchen(t)
	want := map[string]bool{
		"temp:C:KS1h1": true,  // heading with an emoji
		"temp:C:KS1p1": false, // underline, a name-only mention, @here
		"temp:C:KS1p2": false, // a date
		"temp:C:KS1p3": true,  // an empty line
		"temp:C:KS1b2": true,  // nested bullet with bold
		"temp:C:KS1c1": true,  // ticked item
		"temp:C:KS1c2": true,  // item with a link
		"temp:C:KS1p6": false, // highlight
		"temp:C:KS1p7": false, // unresolvable mention, underline, highlight
	}
	for id, w := range want {
		b := d.Find(id)
		if b == nil {
			t.Fatalf("no %s", id)
		}
		if ok, why := Editable(*b); ok != w {
			t.Errorf("%s: Editable = %v (%s), want %v", id, ok, why, w)
		}
	}
	for _, k := range []Kind{Table, Image, Columns, Callout, Embed, File, Quote, Divider} {
		if ok, _ := Editable(Block{Kind: k}); ok {
			t.Errorf("kind %d editable", k)
		}
	}
	// a resolved mention is fine; the same without its id isn't
	b := Block{Kind: Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: "hi "}, {Kind: richtext.User, ID: "U0ABC", Text: "@ada"}}}
	if ok, why := Editable(b); !ok {
		t.Errorf("resolved mention: %s", why)
	}
	b.Spans[1].ID = ""
	if ok, _ := Editable(b); ok {
		t.Error("unresolved mention editable")
	}
	// a code block holding a fence can't be written back
	if ok, _ := Editable(Block{Kind: Code, Code: "a\n```\nb"}); ok {
		t.Error("fenced code editable")
	}
	if ok, _ := Editable(Block{Kind: Code, Code: "  indented\nx"}); !ok {
		t.Error("plain code not editable")
	}
}

// Quip's default content (a new canvas's "Time to document your genius!")
// is marked so the view can draw it the way Slack does (transparent);
// a customized default section is ordinary text.
func TestParseDefaultContent(t *testing.T) {
	d, err := Parse(`<div class="quip-canvas-content"><h1 id="temp:C:A">Notes</h1>` +
		`<p id="temp:C:B" class="default-content line">Time to document your genius!</p>` +
		`<p id="temp:C:C" class="default-content customized-default-content line">Agenda</p>` +
		`<p id="temp:C:D" class="line">typed</p></div>`)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Blocks) != 4 {
		t.Fatalf("%d blocks", len(d.Blocks))
	}
	for i, want := range []bool{false, true, false, false} {
		if d.Blocks[i].Default != want {
			t.Errorf("block %d Default %v", i, d.Blocks[i].Default)
		}
	}
	back, err := Parse(HTML(d, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !back.Blocks[1].Default || back.Blocks[3].Default {
		t.Errorf("HTML round trip lost the default-content mark")
	}
}
