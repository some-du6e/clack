package richview

import (
	"net/url"
	"testing"
	"time"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// Canvas links (richtext canvasslug.go), synthetic: the slug and the
// canvas-section card each open the canvas at the section, by a link
// carrying focus_section_id.

const slugURL = "https://synthetic.enterprise.slack.com/docs/T1/F0CANV0001?focus_section_id=temp%3AC%3ASEC1"

func canvasSlug() rt.Span {
	return rt.Span{Kind: rt.Canvas, ID: "F0CANV0001", URL: slugURL, Icon: "canvas-content", Text: "Plans: Ship the beta",
		Parts: &rt.SpanParts{Spans: []rt.Span{{Kind: rt.Text, Text: "Plans"},
			{Kind: rt.Emoji, ID: "smile", Text: "😄", URL: "https://e.test/smile.png"}, {Kind: rt.Text, Text: ": Ship the beta"}}}}
}

func anchorDoc() rt.Doc {
	return doc(rt.Block{Kind: rt.AttachmentBlock, Att: &rt.Attachment{Legacy: true, FromURL: slugURL, OriginalURL: slugURL,
		Text:   []rt.Block{para(txt("Ship the beta"))},
		Anchor: &rt.CanvasAnchor{FileID: "F0CANV0001", SectionID: "temp:C:SEC1", Icon: "canvas-content", FooterTitle: []rt.Span{txt("Roadmap")}}}})
}

func sectionOf(t *testing.T, u string) string {
	t.Helper()
	p, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	return p.Query().Get("focus_section_id")
}

// A click on the slug opens its link (the canvas at the section); the
// slug is a pill with its icon and a 15px emoji, never underlined.
func TestCanvasSlugClick(t *testing.T) {
	h := &recHandler{}
	hs := newHarness(doc(para(txt("see "), canvasSlug())), h)
	f := findFrag(t, &hs.v.d, "Ship")
	if f.target < 0 || !hs.v.d.targets[f.target].slug || hs.v.d.targets[f.target].underline {
		t.Fatalf("the slug's target %+v: want a slug link, not underlined", hs.v.d.targets[f.target])
	}
	emoji := false
	for _, g := range hs.v.d.frags {
		if g.kind == fImage && g.target == f.target && g.size == 15 {
			emoji = true
		}
	}
	if !emoji {
		t.Error("no 15px emoji in the slug")
	}
	p := center(f)
	hs.send(press(p, 0), release(p, 20*time.Millisecond))
	if len(h.urls) != 1 || sectionOf(t, h.urls[0]) != "temp:C:SEC1" {
		t.Fatalf("OpenURL %q, want the canvas link with its section", h.urls)
	}
}

// The canvas-section card: the section's text, then the footer's title
// (a link to the section), "View section" and "Canvas"; no author row.
// "View section" is hidden when the team allows private message
// forwarding.
func TestCanvasAnchorCard(t *testing.T) {
	h := &recHandler{}
	hs := newHarness(anchorDoc(), h)
	findFrag(t, &hs.v.d, "Ship")
	findFrag(t, &hs.v.d, "Canvas")
	view := findFrag(t, &hs.v.d, "View")
	if view.target < 0 || hs.v.d.targets[view.target].url != slugURL {
		t.Errorf("View section links to %+v", hs.v.d.targets[view.target])
	}
	p := center(findFrag(t, &hs.v.d, "Roadmap"))
	hs.send(press(p, 0), release(p, 20*time.Millisecond))
	if len(h.urls) != 1 || sectionOf(t, h.urls[0]) != "temp:C:SEC1" {
		t.Fatalf("the footer's title opened %q, want the canvas link with its section", h.urls)
	}

	st := newTestStyle()
	st.PrivateForwarding = func() bool { return true }
	v := layoutAt(st, anchorDoc(), Options{}, 400)
	if hasFrag(&v.d, "View") {
		t.Error("View section shown with private_message_forwarding on")
	}
}
