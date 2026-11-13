package richview

import (
	"fmt"
	"testing"
	"time"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// A canvas shared as a message's file (canvasfile.go), synthetic: the
// title line names its owner ("Owner | Canvas"), the card draws the
// lagoon tile, the title with its emoji and "Canvas", and the preview
// under a rule, cut at 240px, taking no clicks of its own (a click on it
// opens the canvas, as on the rest of the card).
func canvasFileDoc(n int) rt.Doc {
	pv := []rt.Block{{Kind: rt.Header, Spans: []rt.Span{txt("Getting started")}}}
	for i := 0; i < n; i++ {
		pv = append(pv, para(txt(fmt.Sprintf("Line %d of the preview.", i+1)), rt.Span{Kind: rt.Link, Text: "a link", URL: "https://example.test/x"}))
	}
	f := &rt.File{ID: "F0CANVFILE9", Name: "FAQ", Title: ":smile: FAQ", Type: "quip", Mode: "quip", PrettyType: "Canvas",
		TitleSpans:    []rt.Span{{Kind: rt.Emoji, ID: "smile", Text: "😄", URL: "https://e.test/smile.png"}, txt(" FAQ")},
		CanvasPreview: pv, Uploader: "Owner", UploaderID: "U0OWNER01"}
	return doc(rt.Block{Kind: rt.FileBlock, File: f})
}

func TestCanvasFileCard(t *testing.T) {
	h := &recHandler{}
	hs := newHarness(canvasFileDoc(30), h)
	hs.w = 700
	hs.frame()
	d := &hs.v.d
	for _, s := range []string{"Owner", "|", "Canvas", "FAQ", "Getting", "Line"} {
		findFrag(t, d, s)
	}
	// the tile: 36px, radius 8, lagoon-40 on light
	top := -1
	for _, b := range d.boxes {
		if b.kind == bFill && b.r.Dx() == 36 && b.r.Dy() == 36 && b.radius == 8 && b.fill == rgb(28, 182, 235) {
			top = b.r.Min.Y - 12 // the card's top: 12px padding
		}
	}
	if top < 26 {
		t.Fatalf("no lagoon canvas tile under the 26px title line (card top %d)", top)
	}
	// nothing under the cut: header 12+36+12, the rule, 12px, 240px
	cut := top + 60 + 1 + 12 + 240
	for _, f := range d.frags {
		if f.top >= cut {
			t.Fatalf("a frag at %d, under the preview's 240px cut (%d)", f.top, cut)
		}
	}
	if hs.v.d.size.Y > cut+40 {
		t.Errorf("the card is %dpx tall, want it to end at the cut (%d)", hs.v.d.size.Y, cut)
	}
	// the preview's link isn't clickable: the click opens the canvas
	ln := findFrag(t, d, "Getting")
	p := center(ln)
	hs.send(press(p, 0), release(p, 20*time.Millisecond))
	if len(h.urls) != 0 || len(h.files) != 1 || h.files[0].ID != "F0CANVFILE9" {
		t.Errorf("a click on the preview: urls %q files %d, want the canvas opened", h.urls, len(h.files))
	}
	// the owner's name opens their profile
	o := center(findFrag(t, d, "Owner"))
	hs.send(press(o, time.Second), release(o, time.Second+20*time.Millisecond))
	if len(h.users) != 1 || h.users[0] != "U0OWNER01" {
		t.Errorf("the owner's name opened %q", h.users)
	}

	// a short canvas: the card ends under its last block
	short := layoutAt(newTestStyle(), canvasFileDoc(1), Options{}, 700)
	if short.d.size.Y >= cut {
		t.Errorf("a two-block preview is %dpx tall", short.d.size.Y)
	}
}
