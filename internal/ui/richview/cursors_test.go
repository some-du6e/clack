package richview

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
)

// Synthetic fixtures only.

// cursorAt moves the mouse to p and reads the cursor Gio shows there.
func (hs *harness) cursorAt(p image.Point) pointer.Cursor {
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
	return hs.r.Cursor()
}

// rectMid is r's middle.
func rectMid(r image.Rectangle) image.Point { return r.Min.Add(r.Max).Div(2) }

// fragMid is a frag's middle.
func fragMid(f *frag) image.Point {
	r := fragRect(f)
	return r.Min.Add(r.Max).Div(2)
}

func wantCursor(t *testing.T, hs *harness, what string, p image.Point, want pointer.Cursor) {
	t.Helper()
	if c := hs.cursorAt(p); c != want {
		t.Errorf("%s at %v: cursor %v, want %v", what, p, c, want)
	}
}

// Over a chart ECharts' cursors: the canvas' default arrow off its items,
// the hand on a bar (zrender's cursor 'pointer'); not the text cursor
// (Karim, α.160).
func TestChartCursors(t *testing.T) {
	hs := newHarness(kitDoc(t, `[`+startupViz+`]`), &dtHandler{})
	hs.w = 700
	hs.frame()
	cs := hs.v.ChartRects()
	if len(cs) != 1 {
		t.Fatalf("%d charts", len(cs))
	}
	l := hs.v.ChartLayout(0)
	b := l.Bars[1]
	o := cs[0].R.Min
	wantCursor(t, hs, "a bar", o.Add(image.Pt(int(b.X+b.W/2), int(b.Y+b.H/2))), pointer.CursorPointer)
	wantCursor(t, hs, "the plot off the bars", o.Add(image.Pt(int(l.Grid.X)+2, int(l.Grid.Y)+2)), pointer.CursorDefault)
}

// A data_table: the text cursor on a cell's text, the default arrow off
// it (the header between its caption and its buttons), the hand on a
// button.
func TestDataTableCursors(t *testing.T) {
	hs := newHarness(kitDoc(t, `[`+boardTable(3, "")+`]`), &dtHandler{})
	hs.w = 700
	hs.frame()
	wantCursor(t, hs, "a cell's text", fragMid(findFrag(t, &hs.v.d, "member")), pointer.CursorText)
	cap := fragRect(findFrag(t, &hs.v.d, "leaderboard"))
	wantCursor(t, hs, "the header past the caption", image.Pt(cap.Max.X+40, (cap.Min.Y+cap.Max.Y)/2), pointer.CursorDefault)
	wantCursor(t, hs, "More actions", rectMid(oneTable(t, hs.v).Buttons["more"]), pointer.CursorPointer)
}

// A timeline: the hand on "(Sources)", the text cursor on a point's
// title, the default arrow beside it.
func TestTimelineCursors(t *testing.T) {
	hs := newHarness(kitDoc(t, usedTools), &tlHandler{})
	hs.w = 600
	hs.frame()
	hs.clickRect(image.Rect(10, 4, 60, 14), 0) // open it
	wantCursor(t, hs, "(Sources)", fragMid(findFrag(t, &hs.v.d, "(Sources)")), pointer.CursorPointer)
	sum := fragRect(findFrag(t, &hs.v.d, "Checked"))
	wantCursor(t, hs, "a point's title", sum.Min.Add(sum.Max).Div(2), pointer.CursorText)
	wantCursor(t, hs, "beside it", image.Pt(sum.Max.X+120, (sum.Min.Y+sum.Max.Y)/2), pointer.CursorDefault)
}

// A contact card is the hand all over (.p-contact_card__container
// {cursor:pointer}), a stranger's card too, whose click does nothing.
func TestContactCardCursors(t *testing.T) {
	for _, c := range []struct {
		name     string
		interact bool
	}{{"a member's", true}, {"a stranger's", false}} {
		card := robinCard
		card.Member.CanInteract = c.interact
		hs := newHarness(contactDoc(card), &ccHandler{})
		hs.w = 600
		hs.frame()
		var frame image.Rectangle
		for _, b := range hs.v.d.boxes {
			if b.kind == bBorder && b.radius == 12 {
				frame = b.r
				break
			}
		}
		if frame.Empty() {
			t.Fatalf("%s: no card frame", c.name)
		}
		wantCursor(t, hs, c.name+" card's corner", frame.Max.Sub(image.Pt(6, 6)), pointer.CursorPointer)
	}
}

// A share_prompt: the hand on Send, the text cursor on the prompt, the
// default arrow on the header's tile.
func TestSharePromptCursors(t *testing.T) {
	hs := newHarness(promptDoc(pipelinePrompt), &spHandler{})
	hs.w = 600
	hs.frame()
	r := hs.v.SharePromptRects()
	if len(r) != 1 {
		t.Fatalf("%d share prompts", len(r))
	}
	wantCursor(t, hs, "Send", rectMid(r[0].Send), pointer.CursorPointer)
	wantCursor(t, hs, "the prompt", fragMid(findFrag(t, &hs.v.d, "Summarise")), pointer.CursorText)
	wantCursor(t, hs, "the tile", r[0].Card.Min.Add(image.Pt(12+20, 12+20)), pointer.CursorDefault)
}
