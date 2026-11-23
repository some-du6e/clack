package canvas

import (
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
)

// Quip draws a canvas, file, message or list mention as an <a href>
// (quip-collab-editor's "Canvas link", "File link", "Message link"), so
// a control that holds a link is that link, the hand under the pointer,
// not a date. A date control is text only, and stays a date. Made-up
// ids.
func TestControlLinkIsLink(t *testing.T) {
	src := `<div class="quip-canvas-content">` +
		`<p id="temp:C:CTL00001" class="line">See <control id="temp:C:CTLc0001" data-remapped="true">` +
		`<a href="https://fake.slack.com/docs/T0FAKE0001/F0CANVAS002">Weekly sync notes</a></control> and ` +
		`<control id="temp:C:CTLc0002" data-remapped="true"><lnk href="https://fake.slack.com/files/U0FAKE00001/F0CANVFILE1/report.pdf">report.pdf</lnk></control>, ` +
		`due <control id="temp:C:CTLc0003" data-remapped="true">August 11th</control></p>` +
		`</div>`
	d, err := Parse(src)
	if err != nil || len(d.Blocks) != 1 {
		t.Fatalf("parse: %v %+v", err, d.Blocks)
	}
	var links, dates []richtext.Span
	for _, s := range d.Blocks[0].Spans {
		switch s.Kind {
		case richtext.Link:
			links = append(links, s)
		case richtext.Date:
			dates = append(dates, s)
		}
	}
	if len(links) != 2 || links[0].URL != "https://fake.slack.com/docs/T0FAKE0001/F0CANVAS002" || links[0].Text != "Weekly sync notes" ||
		links[1].URL != "https://fake.slack.com/files/U0FAKE00001/F0CANVFILE1/report.pdf" || links[1].Text != "report.pdf" {
		t.Errorf("the canvas and file mentions: links %+v", links)
	}
	if len(dates) != 1 || dates[0].Text != "August 11th" {
		t.Errorf("the date: %+v", dates)
	}
}
