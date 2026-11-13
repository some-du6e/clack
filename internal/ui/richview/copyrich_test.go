package richview

import (
	"strings"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// A copied selection is Slack's three clipboard types: the text says
// "@alice", slack/html keeps MemberSlug's data-stringify marks (and a
// user group's, a channel's data-channel-id), text/html is cleaned: no
// classes or data- attributes, the mention a styled link (Djgz).
// Synthetic names.
func TestHTMLRangeKeepsMentions(t *testing.T) {
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	doc := rt.Doc{Blocks: []rt.Block{{Kind: rt.Paragraph, Spans: []rt.Span{
		{Kind: rt.Text, Text: "ping "},
		{Kind: rt.User, Text: "@alice", ID: "UALICE0001"},
		{Kind: rt.Text, Text: " & "},
		{Kind: rt.Usergroup, Text: "@crew", ID: "S0CREW0001"},
		{Kind: rt.Text, Text: " in "},
		{Kind: rt.Channel, Text: "#general", ID: "C0GENERAL1"},
	}}}}
	v := layoutAt(st, doc, Options{}, 480)
	text := v.TextRange(0, v.PlainLen())
	if !strings.Contains(text, "ping @alice & @crew in #general") {
		t.Fatalf("text %q", text)
	}
	sh, ch := v.HTMLRange(0, v.PlainLen())
	for _, want := range []string{`<meta charset="utf-8">ping `, `data-stringify-type="mention" data-stringify-id="UALICE0001" data-stringify-label="@alice"`,
		`href="/team/UALICE0001">@alice</a>`, ` &amp; `, `data-stringify-id="S0CREW0001" data-stringify-label="@crew">@crew</span>`,
		`data-channel-id="C0GENERAL1" data-channel-name="general" data-stringify-type="channel"`} {
		if !strings.Contains(sh, want) {
			t.Errorf("slack/html lacks %q:\n%s", want, sh)
		}
	}
	if strings.Contains(ch, "data-") || strings.Contains(ch, "class=") || !strings.Contains(ch, `style="color: #1264a3; text-decoration: none; background-color: #e8f5fa`) ||
		!strings.Contains(ch, ">@alice</a>") {
		t.Errorf("text/html isn't Slack's cleaned copy:\n%s", ch)
	}
	// a part of the mention copies as that part, still marked
	i := strings.Index(text, "@alice")
	sh, _ = v.HTMLRange(int32(i+1), int32(i+6))
	if !strings.Contains(sh, `data-stringify-label="@alice" href="/team/UALICE0001">alice</a>`) {
		t.Errorf("partial mention: %s", sh)
	}
}
