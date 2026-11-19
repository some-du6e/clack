package blockkit

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Karim (2026-11-18): an app's "Inspect message" modal drew "Here's the
// raw JSON from that message: ```{ …" with its backticks and no code
// block. A section's text, a field and an input's hint are Slack's
// MrkdwnElement (boot-shared WoLU), which hands a mrkdwn text to Mrkdwn
// (JOT5) and TSF as it is, whatever its verbatim (an app showing raw JSON
// sets it): verbatim only tells Slack's server not to link the names and
// URLs in it. So the ``` block is a code block, verbatim or not, in a
// modal and in App Home alike (both are Surfaces).
func TestModalMrkdwnVerbatim(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	h := &bkHost{a: core.New(slack.New(fs.Creds()), store.New("")), th: theme.New(false)}
	const text = "Here's the raw JSON from that message: ```{\n  \"type\": \"message\",\n  \"text\": \"*hi*\"\n}```"
	for _, verbatim := range []bool{false, true} {
		d := docOfText(h, Text{Type: "mrkdwn", Text: text, Verbatim: verbatim})
		var kinds []richtext.BlockKind
		for _, b := range d.Blocks {
			kinds = append(kinds, b.Kind)
		}
		if len(d.Blocks) != 2 || d.Blocks[0].Kind != richtext.Paragraph || d.Blocks[1].Kind != richtext.Preformatted {
			t.Errorf("verbatim %v: blocks %v (%q), want the line and a code block", verbatim, kinds, richtext.PlainText(d))
			continue
		}
		if s := richtext.PlainText(richtext.Doc{Blocks: d.Blocks[:1]}); strings.Contains(s, "`") {
			t.Errorf("verbatim %v: the line %q has backticks", verbatim, s)
		}
		code := richtext.PlainText(richtext.Doc{Blocks: d.Blocks[1:]})
		if !strings.Contains(code, `"text": "*hi*"`) || strings.Contains(code, "`") {
			t.Errorf("verbatim %v: the code block %q, want the JSON as it is (no bold inside)", verbatim, code)
		}
	}
	// plain_text stays plain: its backticks are text
	if d := docOfText(h, Text{Type: "plain_text", Text: "a ```b``` c"}); len(d.Blocks) != 1 || !strings.Contains(richtext.PlainText(d), "```b```") {
		t.Errorf("plain_text: %q", richtext.PlainText(d))
	}
}
