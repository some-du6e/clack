package richview

import (
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// Slack's insertMessageLabelIntoBlocks (boot-shared bhnT, the message
// blocks' renderer x48o): with Block Kit blocks the "(edited)" label goes
// inside the last block only when that one is rich_text; otherwise it is a
// rich_text block of its own after the blocks, no leading space. Karim's
// vote message (2026-11-10: a section, then two actions blocks) had it
// after "Downvotes: 11" instead of under the buttons. Text without Block
// Kit blocks keeps it after its last text block.
func TestEditedPlacementWithBlockKit(t *testing.T) {
	st := newTestStyle()
	vote := doc(
		rt.Block{Kind: rt.Section, Kit: true, Spans: []rt.Span{txt("Upvotes: 0")}},
		rt.Block{Kind: rt.Actions, Kit: true, Buttons: []rt.Button{{Text: "Upvote"}}},
		rt.Block{Kind: rt.Actions, Kit: true, Buttons: []rt.Button{{Text: "Downvote"}}},
	)
	v := layoutAt(st, vote, Options{Edited: true}, 400)
	ed := findFrag(t, &v.d, "(edited)")
	btnBottom := 0
	for _, b := range v.d.boxes {
		if b.kind == bBorder {
			btnBottom = max(btnBottom, b.r.Max.Y)
		}
	}
	if btnBottom == 0 {
		t.Fatal("no buttons drawn")
	}
	if ed.top < btnBottom {
		t.Errorf("(edited) at y %d..%d, want a line under the buttons (they end at %d)", ed.top, ed.bot, btnBottom)
	}
	if sec := findFrag(t, &v.d, "Upvotes:"); ed.line == sec.line {
		t.Errorf("(edited) is on the section's line")
	}

	// the last block is a section (not rich_text): a line of its own
	v = layoutAt(st, doc(rt.Block{Kind: rt.Section, Kit: true, Spans: []rt.Span{txt("Deployed")}}), Options{Edited: true}, 400)
	if ed, sec := findFrag(t, &v.d, "(edited)"), findFrag(t, &v.d, "Deployed"); ed.line == sec.line {
		t.Errorf("after a section, (edited) shares its line")
	}

	// rich_text last, after Block Kit blocks: inline at its end
	v = layoutAt(st, doc(rt.Block{Kind: rt.Section, Kit: true, Spans: []rt.Span{txt("Header")}}, para(txt("closing"))), Options{Edited: true}, 400)
	if ed, last := findFrag(t, &v.d, "(edited)"), findFrag(t, &v.d, "closing"); ed.line != last.line {
		t.Errorf("rich_text last: (edited) on line %d, want the rich_text's line %d", ed.line, last.line)
	}

	// no Block Kit: after the last text, above an actions run (as before)
	v = layoutAt(st, doc(para(txt("plain")), rt.Block{Kind: rt.Actions, Buttons: []rt.Button{{Text: "Go"}}}), Options{Edited: true}, 400)
	if ed, last := findFrag(t, &v.d, "(edited)"), findFrag(t, &v.d, "plain"); ed.line != last.line {
		t.Errorf("mrkdwn text: (edited) on line %d, want the text's line %d", ed.line, last.line)
	}
}
