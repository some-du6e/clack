package richtext

import (
	"strings"
	"testing"
)

// Synthetic fixtures only.

func rtJSON(s string) string {
	return `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}]}`
}

func timelineOf(t *testing.T, d *Doc, i int) *Timeline {
	t.Helper()
	if i >= len(d.Blocks) || d.Blocks[i].Kind != TimelineBlock || d.Blocks[i].Timeline == nil {
		t.Fatalf("block %d isn't a timeline: %s", i, dump(d.Blocks))
	}
	return d.Blocks[i].Timeline
}

// A timeline's points and their steps' sections as TimelineCondensed
// reads them (rSPu: rA's titles, V1's icons, Gw's sources).
func TestTimelineDecode(t *testing.T) {
	d := blocksMsg(t, `[{"type":"timeline","block_id":"tl","summary":`+rtJSON("Planned it")+`,"points":[
		{"id":"p1","status":"complete","text":"Searched","icon":"agentforce--filled","steps":[{"id":"sources","sections":[
			{"rich_text_title":`+rtJSON("Looked")+`,"sources":[{"type":"web","url":"https://a.test/x"},{"type":"web","url":"https://a.test/x"},{"type":"message","channel_id":"C1","timestamp":"1.0"}]},
			{"rich_text_title":`+rtJSON("Read")+`},
			{"sources":[{"type":"file","url":"https://f.test/a.pdf"}]}]}]},
		{"id":"p2","status":"in_progress","rich_text_title":`+rtJSON("Drafting")+`,"text":"ignored","icon_url":"https://i.test/icon.png"},
		{"id":"p3","status":"complete","text":7},
		{"id":"p4","status":"complete","text":{"a":1},"icon":"nope"}]}]`)
	tl := timelineOf(t, d, 0)
	if len(tl.Points) != 4 || !tl.HasSummary || PlainText(Doc{Blocks: []Block{{Kind: Paragraph, Spans: tl.Summary}}}) != "Planned it" || !tl.Running || tl.Hidden {
		t.Fatalf("timeline %+v", tl)
	}
	p := tl.Points
	if TimelineIcon(p[0].Icon) != "agentforce-filled" || p[1].IconURL != "https://i.test/icon.png" || TimelineIcon(p[3].Icon) != "" {
		t.Errorf("icons %q %q %q", p[0].Icon, p[1].IconURL, p[3].Icon)
	}
	title := func(ss []Span) string { return spansText(ss, nil) }
	if title(p[1].Title) != "Drafting" || title(p[2].Title) != "7" || title(p[3].Title) != `{"a":1}` {
		t.Errorf("titles %q %q %q (rich_text_title over text, a number's and an object's JSON)", title(p[1].Title), title(p[2].Title), title(p[3].Title))
	}
	// the sources step: the sections without sources of their own, the
	// first holding every source once; a section without a title skipped
	secs := p[0].Sections
	if len(secs) != 1 || title(secs[0].Title) != "Read" || secs[0].Sources != 3 {
		t.Errorf("sections %+v", secs)
	}
	// a finished stream: no point runs
	m := msg(t, `{"ts":"1.0","text":"t","streaming_state":"completed","blocks":[{"type":"timeline","points":[{"id":"a","status":"in_progress","text":"x"}]}]}`)
	if d := FromMessage(R, m); d.Blocks[0].Timeline == nil || d.Blocks[0].Timeline.Running {
		t.Errorf("streamed and done: running")
	}
	m = msg(t, `{"ts":"1.0","text":"t","ai_context":{"result_status":"streaming"},"blocks":[{"type":"timeline","points":[{"id":"a","status":"in_progress","text":"x"}]}]}`)
	if d := FromMessage(R, m); d.Blocks[0].Timeline == nil || !d.Blocks[0].Timeline.Running {
		t.Errorf("ai_context streaming: not running")
	}
	// no points, or points not an array: nothing; the other blocks draw
	for _, b := range []string{`{"type":"timeline","points":[]}`, `{"type":"timeline","points":{"a":1}}`, `{"type":"timeline"}`} {
		d := blocksMsg(t, `[`+b+`,{"type":"section","text":{"type":"mrkdwn","text":"after"}}]`)
		if len(d.Blocks) != 1 || PlainText(*d) != "after" {
			t.Errorf("%s: %s", b, dump(d.Blocks))
		}
	}
	// task_attribution_: hidden (Slack's task attribution feature is off)
	if tl := timelineOf(t, blocksMsg(t, `[{"type":"timeline","block_id":"task_attribution_9","points":[{"id":"a","status":"complete","text":"x"}]}]`), 0); !tl.Hidden {
		t.Errorf("task attribution timeline not hidden")
	}
}

// convertTimelineBlocksToPlainText: each point's title, "In progress: "
// and "Error: " before the running and failed ones.
func TestTimelineNotificationText(t *testing.T) {
	got := notificationTextOf(t, `{"ts":"1.0","text":"fallback","blocks":[{"type":"timeline","points":[
		{"id":"a","status":"complete","text":"Searched"},{"id":"b","status":"error","text":"Booked"},{"id":"c","status":"in_progress","rich_text_title":`+rtJSON("Drafting")+`}]}]}`)
	if want := "Searched Error: Booked In progress: Drafting"; got != want {
		t.Errorf("notification %q, want %q", got, want)
	}
	if got := notificationTextOf(t, `{"ts":"1.0","text":"fallback","blocks":[{"type":"timeline","points":[]}]}`); !strings.Contains(got, "fallback") {
		t.Errorf("no points: %q", got)
	}
}
