package composer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// Synthetic ids; the shapes are Slack's (a live message_mention read
// 2026-11-08, keys and order only, and createSlugChunkFromOp).

const slugURL = "https://ws.slack.com/archives/C1/p1700000000000100"

func slugAtomFixture() *Atom {
	return messageAtom(btResolver{}, core.Slug{Type: "message", ID: "1700000000.000100", ChannelID: "C1", AuthorID: "U1"}, slugURL)
}

func TestMessageSlugSerialisation(t *testing.T) {
	d := NewDoc()
	end := d.Insert(0, "see ", Attr{})
	d.InsertAtom(end, slugAtomFixture())
	at := slugAtomFixture()
	if at.Text != "alice in #general" {
		t.Fatalf("label %q", at.Text)
	}
	var blocks []struct {
		Elements []struct {
			Elements []json.RawMessage `json:"elements"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(d.RichTextBlocks(), &blocks); err != nil {
		t.Fatal(err)
	}
	els := blocks[0].Elements[0].Elements
	got := string(els[len(els)-1])
	// Slack's element: type, message_ts, channel_id, text (the URL), url, author_id
	want := `{"type":"message_mention","message_ts":"1700000000.000100","channel_id":"C1","text":"` + slugURL + `","url":"` + slugURL + `","author_id":"U1"}`
	if got != want {
		t.Errorf("element:\n got %s\nwant %s", got, want)
	}
	if m := d.Mrkdwn(); m != "see <"+slugURL+">" {
		t.Errorf("text %q", m)
	}
	// a thread reply carries thread_ts
	r := messageAtom(btResolver{}, core.Slug{Type: "message", ID: "1700000000.000200", ChannelID: "C1", AuthorID: "U1", ThreadTS: "1700000000.000100"}, slugURL+"?thread_ts=1700000000.000100&cid=C1")
	b, _ := json.Marshal(rtAtomOut(r))
	if !strings.HasSuffix(string(b), `"author_id":"U1","thread_ts":"1700000000.000100"}`) {
		t.Errorf("reply: %s", b)
	}
	// the slug survives an edit (DocFromBlocksJSON) and the Taut delta
	d2, err := DocFromBlocksJSON(d.RichTextBlocks(), btResolver{})
	if err != nil {
		t.Fatal(err)
	}
	a2 := d2.Attrs[d2.Len()-1].Atom
	if a2 == nil || a2.Kind != AtomMessage || a2.Channel != "C1" || a2.Author != "U1" || a2.URL != slugURL || a2.Text != "alice in #general" {
		t.Fatalf("round trip: %+v", a2)
	}
	ops := d.Delta()["ops"].([]any)
	op := ops[1].(map[string]any)
	if op["insert"] != "alice in #general" || op["attributes"].(map[string]any)["slackslug"].(map[string]any)["type"] != "message" {
		t.Errorf("delta op %v", op)
	}
}

func TestPastedSlackLinksBecomeSlugs(t *testing.T) {
	e := NewEditor()
	var asked [][]string
	e.slugs.onPaste = func(urls []string) { asked = append(asked, urls) }
	e.paste("look " + slugURL + " and https://example.com/x " + slugURL)
	// every pasted link, Slack's or not, once each (convertLinksToUnprocessedSlugs)
	if len(asked) != 1 || len(asked[0]) != 2 || asked[0][0] != slugURL || asked[0][1] != "https://example.com/x" {
		t.Fatalf("asked %v (each pasted link once)", asked)
	}
	// typing a link doesn't ask
	edType(e, " https://ws.slack.com/archives/C1")
	if len(asked) != 1 {
		t.Fatalf("typing asked %v", asked)
	}
	caret := e.caret
	e.applySlugs(map[string]*Atom{slugURL: slugAtomFixture()}, []string{slugURL})
	d := e.Doc()
	n := 0
	for _, a := range d.Attrs {
		if a.Atom != nil && a.Atom.Kind == AtomMessage {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("%d slugs, want 2: %q", n, d.Text())
	}
	if want := caret - 2*(len([]rune(slugURL))-1); e.caret != want {
		t.Errorf("caret %d, want %d", e.caret, want)
	}
	if !strings.Contains(d.Text(), "https://example.com/x") {
		t.Errorf("a link without an answer stays: %q", d.Text())
	}
	e.Undo()
	if strings.Count(e.Doc().Text(), slugURL) != 2 {
		t.Errorf("undo: %q", e.Doc().Text())
	}
}

func TestSlugAtomKinds(t *testing.T) {
	if a := slugAtom(btResolver{}, core.Slug{Type: "channel", ID: "C1", Name: "general"}, ""); a == nil || a.Kind != AtomChannel || a.Text != "#general" {
		t.Errorf("channel: %+v", a)
	}
	if a := slugAtom(btResolver{}, core.Slug{Type: "user", ID: "U1", Name: "alice"}, ""); a == nil || a.Kind != AtomUser || a.Text != "@alice" {
		t.Errorf("user: %+v", a)
	}
	// a link answer is a link slug, labelled with display_url or the URL
	if a := slugAtom(btResolver{}, core.Slug{Type: "link", DisplayURL: "example.com/x"}, "https://example.com/x"); a == nil || a.Kind != AtomLink || a.Text != "example.com/x" {
		t.Errorf("link: %+v", a)
	}
	if a := slugAtom(btResolver{}, core.Slug{Type: "link"}, "https://example.com/x"); a == nil || a.Kind != AtomLink || a.Text != "https://example.com/x" {
		t.Errorf("link without display_url: %+v", a)
	}
	if a := slugAtom(btResolver{}, core.Slug{Type: "link"}, ""); a != nil {
		t.Errorf("link without a URL: %+v", a)
	}
	if a := slugAtom(btResolver{}, core.Slug{Type: "attachment"}, "https://example.com/x"); a != nil {
		t.Errorf("attachment (not ported): %+v", a)
	}
}

func TestParsePermalink(t *testing.T) {
	cases := map[string]core.Permalink{
		slugURL: {Kind: "message", ChannelID: "C1", TS: "1700000000.000100"},
		"https://ws.slack.com/archives/C1/p1700000000000200?thread_ts=1700000000.000100&cid=C1": {Kind: "message", ChannelID: "C1", TS: "1700000000.000200", ThreadTS: "1700000000.000100"},
		"https://ws.enterprise.slack.com/archives/C1":                                           {Kind: "channel", ChannelID: "C1"},
		"https://ws.slack.com/team/U1":                                                          {Kind: "user", UserID: "U1"},
	}
	for u, want := range cases {
		if got, ok := core.ParsePermalink(u); !ok || got != want {
			t.Errorf("%s: %+v %v", u, got, ok)
		}
	}
	if _, ok := core.ParsePermalink("https://example.com/archives/C1/p1700000000000100"); ok {
		t.Error("non-Slack host")
	}
}
