package canvasview

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// The cursor canvas: one block for each inline kind Quip gives its own
// cursor. Every block but the date's is one Clack's editor opens.
const (
	cursorCanvas   = "F0CURSOR001"
	cursorReadOnly = "F0CURSOR002"
	cursorTitleID  = "temp:C:CUR00001"
	cursorLinksID  = "temp:C:CUR00002" // a link, a person, a channel
	cursorFilesID  = "temp:C:CUR00003" // a file link and a canvas link
	cursorDateID   = "temp:C:CUR00004" // a date first, then text
	cursorCheckID  = "temp:C:CUR00005"
	cursorTableID  = "temp:C:CUR00006"
	cursorCellID   = "temp:C:CUR00007" // a table cell's date
)

func cursorBlocks() []canvas.Block {
	return []canvas.Block{
		{ID: cursorTitleID, Kind: canvas.Heading1, Spans: txt("Cursors")},
		{ID: cursorLinksID, Kind: canvas.Paragraph, Spans: []richtext.Span{
			{Kind: richtext.Text, Text: "Read "},
			{Kind: richtext.Link, Text: "the docs", URL: "https://example.com/docs"},
			{Kind: richtext.Text, Text: ", ask "},
			{Kind: richtext.User, ID: "U0FAKE00001"},
			{Kind: richtext.Text, Text: " in "},
			{Kind: richtext.Channel, ID: "C0FAKE00000"},
			{Kind: richtext.Text, Text: " today."},
		}},
		{ID: cursorFilesID, Kind: canvas.Paragraph, Spans: []richtext.Span{
			{Kind: richtext.Link, Text: "report.pdf", URL: "https://fake.slack.com/files/U0FAKE00001/" + fakeslack.CanvasFile + "/report.pdf"},
			{Kind: richtext.Text, Text: " and "},
			{Kind: richtext.Link, Text: "Weekly sync notes", URL: "https://fake.slack.com/docs/" + fakeslack.TeamID + "/" + fakeslack.CanvasShared},
		}},
		{ID: cursorDateID, Kind: canvas.Paragraph, Spans: []richtext.Span{
			{Kind: richtext.Date, Text: "August 11th"},
			{Kind: richtext.Text, Text: " is launch day"},
		}},
		{ID: cursorCheckID, Kind: canvas.Check, Spans: txt("Order stickers")},
		{ID: cursorTableID, Kind: canvas.Table, Rows: [][]canvas.Cell{{
			{Blocks: []canvas.Block{{ID: cursorCellID, Kind: canvas.Paragraph, Spans: []richtext.Span{{Kind: richtext.Date, Text: "August 12th"}}}}},
			{Blocks: []canvas.Block{{ID: "temp:C:CUR00008", Kind: canvas.Paragraph, Spans: txt("Stickers")}}},
		}}},
	}
}

// cursorRig opens canvas id in the channel tab with the pointer rig and
// waits for its mentions to have names (a mention is Quip's loading
// skeleton, no link, until then).
func cursorRig(t *testing.T, id, access string) *pointerRig {
	t.Helper()
	r, _, _ := cursorEnv(t, id, access)
	return r
}

// cursorEnv is cursorRig with its host and fake.
func cursorEnv(t *testing.T, id, access string) (*pointerRig, *testHost, *fakeslack.Server) {
	t.Helper()
	h, fs := newEnv(t, false)
	fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "Cursors", ViewerAccess: access, Doc: canvas.Doc{Blocks: cursorBlocks()}})
	v := New(h, id, Tab, Actions{})
	r := &pointerRig{t: t, v: v, sz: image.Pt(900, 700)}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("the canvas and its mentions didn't load")
		}
		r.frame()
		if b := r.block(cursorLinksID); b != nil && hasRect(b.rv, richview.HitUser) && hasRect(b.rv, richview.HitChannel) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.frame()
	return r, h, fs
}

func (r *pointerRig) block(id string) *blk {
	var found *blk
	var walk func(bs []*blk)
	walk = func(bs []*blk) {
		for _, b := range bs {
			if found == nil && b.b != nil && b.b.ID == id && b.rv != nil {
				found = b
			}
			walk(b.kids)
			for _, row := range b.cells {
				for _, c := range row {
					walk(c.kids)
				}
			}
		}
	}
	walk(r.v.blocks)
	return found
}

func hasRect(rv *richview.View, k richview.HitKind) bool {
	if rv == nil {
		return false
	}
	for _, lr := range rv.LinkRects() {
		if lr.Kind == k {
			return true
		}
	}
	return false
}

// origin is where block id's read view sits in the window.
func (r *pointerRig) origin(id string) image.Point {
	r.t.Helper()
	b := r.block(id)
	if b == nil {
		r.t.Fatalf("no block %s", id)
	}
	full, _, ok := r.router.AreaBounds(b.rv)
	if !ok {
		r.t.Fatalf("block %s's text has no input area", id)
	}
	return full.Min
}

// linkPoints are the middles of block id's links of kind k, in the window.
func (r *pointerRig) linkPoints(id string, k richview.HitKind) []image.Point {
	r.t.Helper()
	at := r.origin(id)
	var out []image.Point
	for _, lr := range r.block(id).rv.LinkRects() {
		if lr.Kind == k {
			c := lr.R.Min.Add(lr.R.Max).Div(2)
			out = append(out, at.Add(c))
		}
	}
	if len(out) == 0 {
		r.t.Fatalf("block %s has no link of kind %v", id, k)
	}
	return out
}

func (r *pointerRig) cursorAt(p image.Point) pointer.Cursor {
	r.queue(mouse(pointer.Move, float32(p.X), float32(p.Y)))
	return r.router.Cursor()
}

// Over a canvas the pointer is Quip's (canvas-controller CSS): the hand
// on a link, a person or channel mention, a file or canvas link
// (.document a:not(.c-link):hover{cursor:pointer}: every one of them is
// an <a>), an editor's date (control.c68
// .slack-date-contents:not(.read-only){cursor:pointer}) and the
// checklist box (li.slackified-checkbox .checkbox{cursor:pointer}); the
// text cursor on plain text and on the title. Before, the editor's press
// area laid its text cursor over the whole block, links and mentions
// included (Karim, α.150: "hovering over a link/channel in a canvas does
// the text thing").
func TestCanvasCursors(t *testing.T) {
	r := cursorRig(t, cursorCanvas, "")
	if !r.v.edit.canEdit(r.v) {
		t.Fatal("the cursor canvas isn't editable")
	}
	for _, id := range []string{cursorLinksID, cursorFilesID, cursorCheckID} {
		if b := r.block(id); !b.edOK {
			t.Fatalf("block %s doesn't open in the editor (%s): the test is about editable blocks", id, b.edWhy)
		}
	}
	text := func(what string, p image.Point) {
		t.Helper()
		if c := r.cursorAt(p); c != pointer.CursorText {
			t.Errorf("%s at %v: cursor %v, want the text cursor", what, p, c)
		}
	}
	hand := func(what string, p image.Point) {
		t.Helper()
		if c := r.cursorAt(p); c != pointer.CursorPointer {
			t.Errorf("%s at %v: cursor %v, want the hand", what, p, c)
		}
	}
	line := gtxLine(r)

	// the control: plain text, at the start of each text block, and the title
	text("plain text", r.origin(cursorLinksID).Add(image.Pt(4, line/2)))
	text("the title", r.origin(cursorTitleID).Add(image.Pt(8, gtxFor(nil, r.sz).Dp(titleLine)/2)))

	for _, p := range r.linkPoints(cursorLinksID, richview.HitLink) {
		hand("a link", p)
	}
	for _, p := range r.linkPoints(cursorLinksID, richview.HitUser) {
		hand("a person", p)
	}
	for _, p := range r.linkPoints(cursorLinksID, richview.HitChannel) {
		hand("a channel", p)
	}
	for _, p := range r.linkPoints(cursorFilesID, richview.HitLink) { // report.pdf, Weekly sync notes
		hand("a file or canvas link", p)
	}
	hand("a date", r.origin(cursorDateID).Add(image.Pt(6, line/2)))
	for _, p := range r.linkPoints(cursorCellID, richview.HitDate) {
		hand("a date in a table cell", p) // cells have no press area: richview's own pass gave the text cursor
	}

	// the checklist box, in the gutter left of the item's text
	ck := r.block(cursorCheckID)
	gtx := gtxFor(nil, r.sz)
	box := r.v.checkRect(gtx, 0)
	tx := gtx.Dp(markerDp) // the marker container left of the text (drawMarker)
	at := r.origin(cursorCheckID).Sub(image.Pt(tx, 0))
	hand("the checklist box", at.Add(box.Min.Add(box.Max).Div(2)))
	if !ck.check.Hovered() {
		t.Error("the point tried isn't on the checklist box")
	}
	text("the checklist item's text", r.origin(cursorCheckID).Add(image.Pt(4, line/2)))
}

// For a viewer who can't edit, Quip's date is read-only and keeps the
// text cursor; links and mentions are still the hand.
func TestCanvasCursorsReadOnly(t *testing.T) {
	r := cursorRig(t, cursorReadOnly, "read")
	if r.v.edit.canEdit(r.v) {
		t.Fatal("the read-only canvas is editable")
	}
	line := gtxLine(r)
	if c := r.cursorAt(r.origin(cursorDateID).Add(image.Pt(6, line/2))); c != pointer.CursorText {
		t.Errorf("a viewer's date: cursor %v, want the text cursor", c)
	}
	for _, p := range r.linkPoints(cursorCellID, richview.HitDate) {
		if c := r.cursorAt(p); c != pointer.CursorText {
			t.Errorf("a viewer's date in a table cell: cursor %v, want the text cursor", c)
		}
	}
	for _, p := range r.linkPoints(cursorLinksID, richview.HitChannel) {
		if c := r.cursorAt(p); c != pointer.CursorPointer {
			t.Errorf("a viewer's channel at %v: cursor %v, want the hand", p, c)
		}
	}
	if c := r.cursorAt(r.origin(cursorLinksID).Add(image.Pt(4, line/2))); c != pointer.CursorText {
		t.Errorf("a viewer's plain text: cursor %v, want the text cursor", c)
	}
}

// gtxLine is the body's line height in the rig's pixels.
func gtxLine(r *pointerRig) int { return gtxFor(nil, r.sz).Dp(bodyLine) }

// A click on an editor's date, in a block Clack's editor can't open yet,
// gives the can't-edit toast as a click on the block's other text does,
// opens nothing and sends nothing. Before, the date's hand target made the
// block's press area take it for a link or mention, and the click did
// nothing at all under the hand (the canvas review of A, 11-23).
func TestCanvasDateClickRefuses(t *testing.T) {
	r, h, fs := cursorEnv(t, cursorCanvas, "")
	b := r.block(cursorDateID)
	if b.edOK {
		t.Fatal("the date's block opens in the editor: the test is about one that can't")
	}
	refusals := func() int {
		h.mu.Lock()
		defer h.mu.Unlock()
		n := 0
		for _, m := range h.toasts {
			if strings.HasPrefix(m, cantEdit) {
				n++
			}
		}
		return n
	}
	// clicks a second apart, past Gio's double-click window (a second
	// click within it would select a word, and a selection takes no toast)
	at := 10 * time.Second
	click := func(p image.Point) {
		t.Helper()
		pt := f32.Pt(float32(p.X), float32(p.Y))
		at += time.Second
		r.router.Queue(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: at},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Time: at},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: at + 50*time.Millisecond},
		)
		r.frame()
		r.frame()
	}
	date := r.linkPoints(cursorDateID, richview.HitDate)[0]
	if c := r.cursorAt(date); c != pointer.CursorPointer {
		t.Fatalf("the date at %v: cursor %v, want the hand", date, c)
	}
	click(date)
	if n := refusals(); n != 1 {
		h.mu.Lock()
		t.Errorf("a click on the date: %d can't-edit toasts, want 1 (%q)", n, h.toasts)
		h.mu.Unlock()
	}
	var dr image.Rectangle
	for _, lr := range b.rv.LinkRects() {
		if lr.Kind == richview.HitDate {
			dr = lr.R
		}
	}
	click(r.origin(cursorDateID).Add(image.Pt(dr.Max.X+24, (dr.Min.Y+dr.Max.Y)/2))) // " is launch day"
	if n := refusals(); n != 2 {
		t.Errorf("a click on the text after the date: %d can't-edit toasts in all, want 2", n)
	}
	if r.v.edit.cur != nil {
		t.Error("the date's block opened in the editor")
	}
	if b.rv.HasSelection() {
		t.Error("the clicks left a selection")
	}
	if es := fs.CanvasEdits(); len(es) != 0 {
		t.Errorf("%d canvases.edit calls, want 0: %v", len(es), es)
	}
}
