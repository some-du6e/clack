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
// Web and Memories; a message the client doesn't hold is a link to it, a
// file its name (from its URL without one), a web page its title (else
// its address) and snippet, a memory its topic.
func TestTimelineSourcesDoc(t *testing.T) {
	h := chartHost(t, true)
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	sec := &richtext.TimelineSection{
		Title: []richtext.Span{{Kind: richtext.Text, Text: "Looked around"}},
		SourceList: []json.RawMessage{
			raw(`{"type":"web","url":"https://a.test/x","title":"Release notes","snippet":"What changed"}`),
			raw(`{"type":"web","url":"https://a.test/x","title":"Again"}`),
			raw(`{"type":"web","url":"https://b.test/y"}`),
			raw(`{"type":"file","url":"https://f.test/dir/plan.pdf"}`),
			raw(`{"type":"message","channel_id":"C1","timestamp":"1700000000.000100"}`),
			raw(`{"memory_id":"m1","topic_name":"Offsite"}`),
		},
	}
	var blocks []string
	for _, b := range timelineSourcesDoc(h, sec).Blocks {
		var parts []string
		for _, sp := range b.Spans {
			switch sp.Kind {
			case richtext.Link:
				parts = append(parts, "["+sp.Text+"]("+sp.URL+")")
			case richtext.Break:
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
		"[https://hackclub.slack.com/archives/C1/p1700000000000100](https://hackclub.slack.com/archives/C1/p1700000000000100)",
		"Files",
		"[plan.pdf](https://f.test/dir/plan.pdf)",
		"Web",
		"[Release notes](https://a.test/x)/What changed",
		"[https://b.test/y](https://b.test/y)",
		"Memories",
		"Offsite",
	}
	if strings.Join(blocks, "\n") != strings.Join(want, "\n") {
		t.Errorf("the modal's blocks:\n%s\nwant:\n%s", strings.Join(blocks, "\n"), strings.Join(want, "\n"))
	}
}
