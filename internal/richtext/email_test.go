package richtext

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// An email file decodes into Email (Slack's shape, read from Hack Club's
// on 2026-11-18), its recipients named: a member by their name, a
// channel id with no address (the email went to a channel's address) by
// the channel's; the DMARC check and the attachments kept. A file of
// another mode with fields of the same names in other shapes still
// decodes.
func TestEmailFields(t *testing.T) {
	raw := `{"type":"message","ts":"1762800000.000200","user":"USLACKBOT","bot_id":"B0FAKEMAIL","upload":true,"files":[
	{"id":"F0E","mode":"email","filetype":"email","pretty_type":"Email","mimetype":"text/html","title":"Hi","subject":"Hi",
	 "from":[{"address":"a@x.example","name":"A","original":"A <a@x.example>","slack_user_id":"U1"}],
	 "to":[{"address":"","name":"C0EMAIL0001","original":"inbox"}],
	 "cc":[{"address":"c@x.example","name":"","original":"c@x.example"}],
	 "plain_text":"hello","preview":"<p>hello</p>","preview_plain_text":"hello","timestamp":1762799990,
	 "email_security":{"has_valid_dmarc":false},"app_id":"A0E","bot_id":"B0E",
	 "attachments":[{"filename":"a.pdf","size":10,"mimetype":"application/pdf","url":"https://files.slack.com/a.pdf","metadata":null,"slack_file_id":"F0A"}],
	 "original_attachment_count":1},
	{"id":"F0O","mode":"hosted","filetype":"text","title":"notes.txt","from":"nobody","to":7,"attachments":"none","headers":[1]}]}`
	var m slack.Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d := FromMessage(testNames{}, &m)
	var files []*File
	for _, b := range d.Blocks {
		if b.Kind == FileBlock {
			files = append(files, b.File)
		}
	}
	if len(files) != 2 {
		t.Fatalf("%d files", len(files))
	}
	e := files[0].Email
	if e == nil {
		t.Fatal("no Email on the email file")
	}
	if len(e.From) != 1 || e.From[0].Name != "Ann" || e.From[0].UserID != "U1" || e.From[0].Address != "a@x.example" {
		t.Errorf("from %+v, want the member's name", e.From)
	}
	if len(e.To) != 1 || e.To[0].Name != "#inbox" {
		t.Errorf("to %+v, want the channel's name", e.To)
	}
	if len(e.Cc) != 1 || e.Cc[0].Address != "c@x.example" {
		t.Errorf("cc %+v", e.Cc)
	}
	if !e.Unverified || e.Subject != "Hi" || e.PlainText != "hello" || e.HTML != "<p>hello</p>" || e.Timestamp != 1762799990 || e.AppID != "A0E" {
		t.Errorf("email %+v", e)
	}
	if len(e.Attachments) != 1 || e.Attachments[0].FileID != "F0A" || e.Attachments[0].Name != "a.pdf" || e.Attachments[0].Size != 10 {
		t.Errorf("attachments %+v", e.Attachments)
	}
	if files[1].Email != nil {
		t.Errorf("a hosted file got an Email: %+v", files[1].Email)
	}
}

type testNames struct{}

func (testNames) UserName(id string) string { return "@" + id }
func (testNames) ChannelName(id string) string {
	if id == "C0EMAIL0001" {
		return "#inbox"
	}
	return id
}
func (testNames) UsergroupName(id string) string { return id }
func (testNames) Emoji(string) (string, string, bool) {
	return "", "", false
}
func (testNames) SelfID() string { return "U0" }
func (testNames) MemberName(id string) (string, bool) {
	if id == "U1" {
		return "Ann", true
	}
	return "", false
}
func (testNames) MemberAvatar(string) string { return "" }

func blockTexts(bs []Block) []string {
	var out []string
	for _, b := range bs {
		var s strings.Builder
		write := func(sp []Span) {
			for _, x := range sp {
				if x.Kind == Break {
					s.WriteString("|")
					continue
				}
				s.WriteString(x.Text)
			}
		}
		write(b.Spans)
		for i, it := range b.Items {
			if i > 0 {
				s.WriteString(" / ")
			}
			write(it)
		}
		out = append(out, s.String())
	}
	return out
}

// An email's HTML as blocks: paragraphs and divs, breaks, headings in
// bold, links (http(s) and mailto only), lists, a table's cells on one
// line, quotes and preformatted text; whitespace collapsed; head, style,
// script and hidden parts dropped.
func TestEmailBlocks(t *testing.T) {
	src := `<html><head><title>T</title><style>p{color:red}</style><script>alert(1)</script></head><body>
	<h1>Weekly   Build</h1>
	<p>Hello <b>makers</b>,
	   read the <a href="https://example.com/n">notes</a> or <a href="javascript:alert(1)">this</a>.<br>Bye</p>
	<ul><li>One</li><li>Two <i>more</i></li></ul>
	<table><tr><td>Builds</td><td>42</td></tr><tr><td>Reviews</td><td>17</td></tr></table>
	<div style="display: none">hidden</div><span hidden>also hidden</span>
	<blockquote>quoted</blockquote><pre>a  b
c</pre><div>last</div></body></html>`
	bs := EmailBlocks(src)
	got := blockTexts(bs)
	want := []string{"Weekly Build", "Hello makers, read the notes or this.|Bye", "One / Two more", "Builds  42", "Reviews  17", "quoted", "a  b|c", "last"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("blocks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if bs[0].Spans[0].Style&Bold == 0 {
		t.Error("the heading isn't bold")
	}
	if bs[2].Kind != List || bs[5].Kind != Quote || bs[6].Kind != Preformatted {
		t.Errorf("kinds %v %v %v", bs[2].Kind, bs[5].Kind, bs[6].Kind)
	}
	var links []string
	for _, sp := range bs[1].Spans {
		if sp.Kind == Link {
			links = append(links, sp.Text+"="+sp.URL)
		}
		if sp.Text == "makers" && sp.Style&Bold == 0 {
			t.Error("<b> isn't bold")
		}
	}
	if strings.Join(links, ",") != "notes=https://example.com/n" {
		t.Errorf("links %q (a javascript: link must stay text)", links)
	}
	// untrusted: nesting past the cap and an endless body stop, they don't hang
	deep := strings.Repeat("<div>", 5000) + "x" + strings.Repeat("</div>", 5000)
	if bs := EmailBlocks(deep); len(blockTexts(bs)) != 1 || blockTexts(bs)[0] != "x" {
		t.Errorf("deep nesting: %q", blockTexts(bs))
	}
	if bs := EmailBlocks(strings.Repeat("<p>word</p>", 100000)); len(bs) > emailMaxBlocks+1 {
		t.Errorf("%d blocks past the cap", len(bs))
	}
}
