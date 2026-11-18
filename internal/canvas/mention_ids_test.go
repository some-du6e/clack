package canvas

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
)

// Quip's export writes a mention as the id its control holds, never the
// name (#ship's "Guidelines" canvas: <h1><control><a>#C0M8PUPU6</a>
// </control> Guidelines</h1>; Karim, α.106). The parser keeps the id, so
// the reader can name the mention from its own store. The markup here is
// that shape with made-up ids.
func TestParseExportMentionIDs(t *testing.T) {
	src := `<div class="quip-canvas-content">` +
		`<h1 id="temp:C:MID00001"><control id="temp:C:MIDc0001" data-remapped="true"><a>#C0FAKE00001</a></control> Guidelines</h1>` +
		`<p id="temp:C:MID00002" class="line">Ask <control id="temp:C:MIDc0002" data-remapped="true"><a>@U0FAKE00001</a></control>, ` +
		`<control id="temp:C:MIDc0003" data-remapped="true"><a>@W0FAKE00002</a></control> or ` +
		`<control id="temp:C:MIDc0004" data-remapped="true"><a>@S0FAKE00003</a></control> in ` +
		`<control id="temp:C:MIDc0005" data-remapped="true"><a>#G0FAKE00004</a></control>, ` +
		`<control id="temp:C:MIDc0006" data-remapped="true"><a>@here</a></control>.</p>` +
		// an older export's names stay names (ResolveCanvasMention), and
		// text that only looks like an id inside a sentence is text
		`<p id="temp:C:MID00003" class="line"><control id="temp:C:MIDc0007" data-remapped="true"><a>#general</a></control> ` +
		`<control id="temp:C:MIDc0008" data-remapped="true"><a>@C0FAKE00001</a></control> #C0FAKE00009</p>` +
		`</div>`
	d, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Blocks) != 3 {
		t.Fatalf("blocks %+v", d.Blocks)
	}
	type want struct {
		kind richtext.SpanKind
		id   string
	}
	mentions := func(b Block) []want {
		var out []want
		for _, s := range b.Spans {
			switch s.Kind {
			case richtext.User, richtext.Channel, richtext.Usergroup, richtext.Broadcast:
				out = append(out, want{s.Kind, s.ID})
			}
		}
		return out
	}
	check := func(i int, w ...want) {
		t.Helper()
		got := mentions(d.Blocks[i])
		if len(got) != len(w) {
			t.Fatalf("block %d mentions %+v, want %+v", i, got, w)
		}
		for j := range w {
			if got[j] != w[j] {
				t.Errorf("block %d mention %d: %+v, want %+v", i, j, got[j], w[j])
			}
		}
	}
	check(0, want{richtext.Channel, "C0FAKE00001"})
	check(1, want{richtext.User, "U0FAKE00001"}, want{richtext.User, "W0FAKE00002"}, want{richtext.Usergroup, "S0FAKE00003"},
		want{richtext.Channel, "G0FAKE00004"}, want{richtext.Broadcast, "here"})
	check(2, want{richtext.Channel, ""}, want{richtext.User, ""})
	if !strings.Contains(d.Blocks[2].Text(), "#C0FAKE00009") {
		t.Errorf("plain text after the mentions: %q", d.Blocks[2].Text())
	}
	// a heading whose mentions carry ids is editable (canvases.edit's
	// ![](#C…) form), and its markdown names the ids
	if ok, why := Editable(d.Blocks[0]); !ok {
		t.Errorf("heading with a channel id not editable: %s", why)
	}
	if md := Markdown(d.Blocks[0]); md != "# ![](#C0FAKE00001) Guidelines" {
		t.Errorf("markdown %q", md)
	}
}

// The fake's export (HTML) writes mentions the way Quip's does: the id,
// not the name, and reads back to the same ids.
func TestHTMLWritesMentionIDs(t *testing.T) {
	d := Doc{Blocks: []Block{{ID: "temp:C:MIDw0001", Kind: Heading1, Spans: []richtext.Span{
		{Kind: richtext.Channel, ID: "C0FAKE00001", Text: "#lounge"},
		{Kind: richtext.Text, Text: " and "},
		{Kind: richtext.User, ID: "U0FAKE00001", Text: "@ada"},
		{Kind: richtext.Text, Text: " "},
		{Kind: richtext.Usergroup, ID: "S0FAKE00003", Text: "@design"},
		{Kind: richtext.Text, Text: " "},
		{Kind: richtext.Broadcast, ID: "here", Text: "@here", Self: true},
	}}}}
	src := HTML(d, nil)
	for _, w := range []string{"<a>#C0FAKE00001</a>", "<a>@U0FAKE00001</a>", "<a>@S0FAKE00003</a>", "<a>@here</a>"} {
		if !strings.Contains(src, w) {
			t.Errorf("export lacks %s: %s", w, src)
		}
	}
	for _, name := range []string{"#lounge", "@ada", "@design"} {
		if strings.Contains(src, name) {
			t.Errorf("export names %s: %s", name, src)
		}
	}
	back, err := Parse(src)
	if err != nil || len(back.Blocks) != 1 {
		t.Fatalf("parse back: %v %+v", err, back.Blocks)
	}
	var ids []string
	for _, s := range back.Blocks[0].Spans {
		if s.ID != "" {
			ids = append(ids, s.ID)
		}
	}
	if got := strings.Join(ids, " "); got != "C0FAKE00001 U0FAKE00001 S0FAKE00003 here" {
		t.Errorf("ids read back: %q", got)
	}
}
