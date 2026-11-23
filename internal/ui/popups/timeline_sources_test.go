package popups

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
)

// Synthetic fixtures only.

// A section's "(Sources)" modal (TimelineSourcesBody): its title, then its
// sources de-duplicated by getSourceKey and grouped as Messages, Files,
// Web and Memories; a message the client doesn't hold is its time, a link
// to it; a file its name (from its URL without one), a web page its title
// (else its host) and snippet, a memory a topic's rich_text_title; a
// message's and a file's links close the modal, a web page's doesn't.
func TestTimelineSourcesDoc(t *testing.T) {
	h := chartHost(t, true)
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	rt := func(s string) string {
		return `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}]}`
	}
	sec := &richtext.TimelineSection{
		Title: []richtext.Span{{Kind: richtext.Text, Text: "Looked around"}},
		SourceList: []json.RawMessage{
			raw(`{"type":"web","url":"https://a.test/x","title":"Release notes","snippet":"What changed"}`),
			raw(`{"type":"web","url":"https://a.test/x","title":"Again"}`),
			raw(`{"type":"web","url":"https://b.test/y"}`),
			raw(`{"type":"file","url":"https://f.test/dir/plan.pdf"}`),
			raw(`{"type":"message","channel_id":"C1","timestamp":"1700000000.000100"}`),
			raw(`{"memory_id":"m1","memory_type":"topic","rich_text_title":` + rt("Offsite") + `,"rich_text_content":` + rt("We meet in May") + `}`),
			raw(`{"memory_id":"m2","memory_type":"fact","rich_text_title":` + rt("Ignored") + `,"rich_text_content":` + rt("Lunch is at noon") + `}`),
		},
	}
	doc, closes := timelineSourcesDoc(h, sec)
	var blocks []string
	var msgLink string
	for _, b := range doc.Blocks {
		var parts []string
		for _, sp := range b.Spans {
			switch {
			case sp.Kind == richtext.Link && strings.Contains(sp.URL, "/archives/C1/"):
				parts = append(parts, "[time]("+sp.URL+")")
				msgLink = sp.URL
			case sp.Kind == richtext.Link:
				parts = append(parts, "["+sp.Text+"]("+sp.URL+")")
			case sp.Kind == richtext.Break:
				parts = append(parts, "/")
			default:
				parts = append(parts, sp.Text)
			}
		}
		blocks = append(blocks, strings.Join(parts, ""))
	}
	want := []string{
		"Looked around",
		"Messages",
		"[time](https://hackclub.slack.com/archives/C1/p1700000000000100)",
		"Files",
		"[plan.pdf](https://f.test/dir/plan.pdf)",
		"Web",
		"[Release notes](https://a.test/x)/What changed",
		"[b.test](https://b.test/y)",
		"Memories",
		"Offsite",
		"Lunch is at noon",
	}
	if strings.Join(blocks, "\n") != strings.Join(want, "\n") {
		t.Errorf("the modal's blocks:\n%s\nwant:\n%s", strings.Join(blocks, "\n"), strings.Join(want, "\n"))
	}
	if !closes[msgLink] || !closes["https://f.test/dir/plan.pdf"] || closes["https://a.test/x"] {
		t.Errorf("links that close the modal: %v", closes)
	}
	if got := cutSourceText(strings.Repeat("a", 200)); len([]rune(got)) != 166 || !strings.HasSuffix(got, "…") {
		t.Errorf("a long message cut to %d runes", len([]rune(got)))
	}
	if got := cutSourceText("one\ntwo\nthree\nfour"); got != "one\ntwo\nthree…" {
		t.Errorf("four lines cut to %q", got)
	}
}
