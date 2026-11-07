package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// synthetic fixtures only (scraping policy): shapes match Slack's canvas HTML.

type fakeResolver struct{}

func (fakeResolver) UserName(id string) string {
	if id == "U1" {
		return "orpheus"
	}
	return id
}
func (fakeResolver) ChannelName(id string) string { return "general" }
func (fakeResolver) Emoji(name string) (string, string, bool) {
	if name == "smile" {
		return "", "😄", true
	}
	return "", "", false
}
func (fakeResolver) SelfID() string { return "U9" }

const canvasFixture = `<div class="quip-canvas-content">
<h1 id="temp:C:AAA1">Plan</h1>
<h2 id="temp:C:AAA2">Goals</h2>
<p id="temp:C:AAA3" class="line">Hello <b>bold</b> <a href="https://example.com/x">link</a> and
<control id="temp:C:CTL1" data-remapped="true"><img src="https://slack-imgs.com/?c=1&amp;o1=gu&amp;url=https%3A%2F%2Femoji.slack-edge.com%2FT1%2Fgoog%2Fabc.png" alt="goog" data-is-slack="true" style="width: 18px; height: 18px">:goog:</control></p>
<div data-section-style="5" class="list-numbering-restart-at"><ul id="temp:C:L1"><li id="temp:C:B1" value="1"><span id="temp:C:B1">one</span><br></li><ul><li id="temp:C:B2"><span id="temp:C:B2">nested</span><br></li></ul><li id="temp:C:B3"><span id="temp:C:B3">two</span><br></li></ul></div>
<div data-section-style="6" class="list-numbering-restart-at"><ul id="temp:C:L2"><li id="temp:C:N1" value="1"><span>first</span><br></li><li id="temp:C:N2"><span>second</span><br></li></ul></div>
<div data-section-style="7" class="list-numbering-restart-at"><ul id="temp:C:L3"><li id="temp:C:K1" class="checked"><span>done</span><br></li><li id="temp:C:K2"><span>todo</span><br></li></ul></div>
<blockquote><p id="temp:C:Q1" class="line">quoted</p></blockquote>
<p id="temp:C:CODE" class="prettyprint line">a := 1<br>b := 2</p>
<p class="embedded-link">https://example.com/page</p>
<table><tr><td><p id="temp:C:T1" class="line">cell <a href="https://hackclub.slack.com/team/U1">@someone</a></p></td><td><p id="temp:C:T2" class="line"></p></td></tr></table>
<hr>
<p id="temp:C:IMG" class="line"><img src="https://files.slack.com/files-pri/T1-F1/pic.png" alt="pic"></p>
</div>`

func TestParseCanvasHTML(t *testing.T) {
	c := parseCanvasHTML(fakeResolver{}, []byte(canvasFixture))
	if c.Title != "Plan" || c.TitleID != "temp:C:AAA1" {
		t.Fatalf("title %q %q", c.Title, c.TitleID)
	}
	kinds := []CanvasBlockKind{CanvasH2, CanvasParagraph, CanvasBullet, CanvasBullet, CanvasBullet, CanvasNumbered, CanvasNumbered,
		CanvasCheck, CanvasCheck, CanvasQuote, CanvasCode, CanvasEmbed, CanvasTable, CanvasDivider, CanvasImage}
	if len(c.Blocks) != len(kinds) {
		for _, b := range c.Blocks {
			t.Logf("block %d %q", b.Kind, richtext.SpansText(b.Spans))
		}
		t.Fatalf("got %d blocks, want %d", len(c.Blocks), len(kinds))
	}
	for i, k := range kinds {
		if c.Blocks[i].Kind != k {
			t.Errorf("block %d kind %d want %d", i, c.Blocks[i].Kind, k)
		}
	}
	p := c.Blocks[1]
	if p.ID != "temp:C:AAA3" {
		t.Errorf("para id %q", p.ID)
	}
	var sawBold, sawLink, sawEmoji bool
	for _, s := range p.Spans {
		switch {
		case s.Kind == richtext.Text && s.Style&richtext.Bold != 0 && s.Text == "bold":
			sawBold = true
		case s.Kind == richtext.Link && s.URL == "https://example.com/x":
			sawLink = true
		case s.Kind == richtext.Emoji && s.ID == "goog":
			sawEmoji = strings.HasPrefix(s.URL, "https://emoji.slack-edge.com/")
		}
	}
	if !sawBold || !sawLink || !sawEmoji {
		t.Errorf("inline: bold=%v link=%v emoji=%v %+v", sawBold, sawLink, sawEmoji, p.Spans)
	}
	if c.Controls["temp:C:CTL1"] == "" {
		t.Errorf("control text missing: %v", c.Controls)
	}
	if b := c.Blocks[3]; b.Indent != 1 || richtext.SpansText(b.Spans) != "nested" {
		t.Errorf("nested bullet %+v", b)
	}
	if c.Blocks[5].Number != 1 || c.Blocks[6].Number != 2 {
		t.Errorf("numbers %d %d", c.Blocks[5].Number, c.Blocks[6].Number)
	}
	if !c.Blocks[7].Checked || c.Blocks[8].Checked {
		t.Errorf("checked %v %v", c.Blocks[7].Checked, c.Blocks[8].Checked)
	}
	code := c.Blocks[10]
	if len(code.Spans) != 3 || code.Spans[1].Kind != richtext.Break {
		t.Errorf("code spans %+v", code.Spans)
	}
	if c.Blocks[11].URL != "https://example.com/page" {
		t.Errorf("embed %q", c.Blocks[11].URL)
	}
	tb := c.Blocks[12]
	if len(tb.Rows) != 1 || len(tb.Rows[0]) != 2 || tb.Rows[0][0].ID != "temp:C:T1" {
		t.Fatalf("table %+v", tb.Rows)
	}
	var mention bool
	for _, s := range tb.Rows[0][0].Spans {
		if s.Kind == richtext.User && s.ID == "U1" && s.Text == "@orpheus" {
			mention = true
		}
	}
	if !mention {
		t.Errorf("mention %+v", tb.Rows[0][0].Spans)
	}
	if img := c.Blocks[14].Image; img == nil || !img.Proxy {
		t.Errorf("image %+v", c.Blocks[14].Image)
	}
}

func TestPBRoundTrip(t *testing.T) {
	sec := pb().Str(1, "temp:C:X").Int(2, 7000).Str(8, "aab").Int(9, 0).Int(10, 2).
		Msg(12, pb().Msg(1, pb().Str(1, "hi <b>there</b>"))).Msg(13, pb().Str(1, "temp:C:L").Int(4, 7)).
		Msg(16, pb().Int(1, 2)).Int(29, 1)
	s, ok := parseQuipSection(sec.b)
	if !ok || s.ID != "temp:C:X" || s.Version != 7000 || s.Style != 2 || s.RTML != "hi <b>there</b>" ||
		s.ListOf != "temp:C:L" || s.ListKind != 7 || s.Indent != 2 || !s.InQuote || !s.IsText {
		t.Fatalf("section %+v", s)
	}
	title := pb().Str(1, "temp:C:T").Int(2, 4000).Str(7, "DOC").Str(8, "aaa").Int(9, 64).Msg(12, pb().Msg(58, pb().Str(1, "Name")))
	load := pb().Msg(2, pb().Int(1, 1).Msg(2, pb().Msg(3, pb().Str(1, "THR")).Msg(6, pb().Str(1, "DOC")).
		Bytes(7, title.b).Bytes(7, sec.b)))
	d, err := parseQuipLoad(load.b)
	if err != nil {
		t.Fatal(err)
	}
	if d.Thread != "THR" || d.Document != "DOC" || len(d.Sections) != 2 {
		t.Fatalf("doc %+v", d)
	}
	if ts, ok := d.section("temp:C:T"); !ok || !ts.IsTitle || ts.Title != "Name" {
		t.Fatalf("title %+v", ts)
	}
	if _, err := pbDecode([]byte{0x0a, 0x05, 'a'}); err == nil {
		t.Error("truncated message decoded")
	}
}

func TestRTMLEdit(t *testing.T) {
	ctl := map[string]string{"C1": ":goog:"}
	cases := []struct{ rtml, text, want string }{
		{"hello", "hello world", "hello world"},
		{"a <b>bold</b> c", "a bolder c", "a <b>bolder</b> c"},
		{`hi <control id="C1"></control> there`, "hi :goog: there!", `hi <control id="C1"></control> there!`},
		{`hi <control id="C1"></control> there`, "hi there", `hi there`},
		{"x &amp; y", "x & y & z", "x &amp; y &amp; z"},
		{"line<br>two", "line\ntwo\nthree", "line<br>two<br>three"},
		{"<i>all</i>", "", "<i></i>"},
	}
	for _, c := range cases {
		got := rtmlEdit(c.rtml, c.text, ctl, false)
		if got != c.want {
			t.Errorf("rtmlEdit(%q, %q) = %q, want %q", c.rtml, c.text, got, c.want)
		}
		if p := rtmlPlain(got, ctl); p != c.text {
			t.Errorf("plain(%q) = %q, want %q", got, p, c.text)
		}
	}
	if got := rtmlEdit("a", "a<b>", nil, false); got != "a&lt;b&gt;" {
		t.Errorf("escape: %q", got)
	}
}

func TestOrderBetween(t *testing.T) {
	cases := [][2]string{{"aaa", "aab"}, {"aab", "aabaaaa"}, {"aab", ""}, {"aab", "aaz"}, {"aab", "aabc"}, {"", "aab"}}
	for _, c := range cases {
		k := orderBetween(c[0], c[1])
		if k <= c[0] || (c[1] != "" && k >= c[1]) {
			t.Errorf("orderBetween(%q, %q) = %q", c[0], c[1], k)
		}
		for _, r := range k {
			if r < 'a' || r > 'z' {
				t.Errorf("orderBetween(%q, %q) = %q has %q", c[0], c[1], k, r)
			}
		}
	}
}

func TestListMetaAndCells(t *testing.T) {
	meta := `{"schema":[{"id":"Col1","name":"Name","key":"name","type":"text","is_primary_column":true},
	{"id":"Col2","name":"status","key":"Col2k","type":"select","options":{"format":"single_select","choices":[{"value":"Opt1","label":"idea","color":"gray"}]}},
	{"id":"Col3","name":"done","key":"Col3k","type":"checkbox"}],
	"views":[{"id":"V2","name":"Board","type":"board","position":"2","grouping":{"group_by":"Col2k"},"columns":[]},
	{"id":"V1","name":"All","type":"table","position":"1","is_all_items_view":true,"show_completed_items":false,
	 "columns":[{"visible":true,"key":"name","id":"Col1","position":"2"},{"visible":false,"key":"Col3k","id":"Col3","position":"1"}],
	 "sort":[{"column_id":"Col2k","direction":"desc"}]}],"todo_mode":false}`
	r := &canvasFileRaw{ID: "F1", Title: "L", Filetype: "list", ListMetadata: json.RawMessage(meta)}
	var l List
	parseListMeta(r, &l, nil)
	if len(l.Columns) != 3 || !l.Columns[0].Primary || len(l.Columns[1].Choices) != 1 {
		t.Fatalf("columns %+v", l.Columns)
	}
	if len(l.Views) != 2 || l.Views[0].ID != "V1" || l.Views[1].GroupBy != "Col2k" {
		t.Fatalf("views %+v", l.Views)
	}
	if v := l.Views[0]; v.ShowCompleted || len(v.Sort) != 1 || !v.Sort[0].Desc || v.Columns[0].Key != "Col3k" {
		t.Errorf("view %+v", v)
	}
	yes := true
	if m := cellJSON("checkbox", ListValue{Checkbox: &yes}); m["checkbox"] != true {
		t.Errorf("checkbox cell %v", m)
	}
	if m := cellJSON("select", ListValue{Select: []string{"Opt1"}}); len(m["select"].([]string)) != 1 {
		t.Errorf("select cell %v", m)
	}
	b, _ := json.Marshal(cellJSON("text", ListValue{Text: "hi"}))
	if !strings.Contains(string(b), `"rich_text"`) || !strings.Contains(string(b), `"text":"hi"`) {
		t.Errorf("text cell %s", b)
	}
	if id := canvasEventID(json.RawMessage(`{"type":"list_record_updated","list_id":"F1"}`)); id != "F1" {
		t.Errorf("event id %q", id)
	}
	if !CanvasChanged(Event{Type: "canvas"}, CanvasMeta{ID: "F1"}) || CanvasChanged(Event{Type: "canvas", ID: "F2"}, CanvasMeta{ID: "F1"}) {
		t.Error("CanvasChanged")
	}
}
