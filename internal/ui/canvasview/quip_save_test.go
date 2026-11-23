package canvasview

import (
	"image"
	"net/http"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
)

// The Quip canvas: plain blocks edit-document can save now, and one with a
// mention it can't yet.
const (
	quipCanvasID = "F0QUIPSAVE01"
	quipFirstID  = "temp:C:QSV00001"
	quipSecondID = "temp:C:QSV00002"
	quipCheckID  = "temp:C:QSV00003"
	quipMentID   = "temp:C:QSV00004"
)

func quipBlocks() []canvas.Block {
	return []canvas.Block{
		{ID: quipFirstID, Kind: canvas.Paragraph, Spans: txt("First paragraph")},
		{ID: quipSecondID, Kind: canvas.Paragraph, Spans: txt("Second paragraph")},
		{ID: quipCheckID, Kind: canvas.Check, Spans: txt("Order stickers")},
		{ID: quipMentID, Kind: canvas.Paragraph, Spans: []richtext.Span{
			{Kind: richtext.Text, Text: "Ask "},
			{Kind: richtext.User, ID: "U0FAKE00001"},
		}},
	}
}

// quipEnv opens the Quip canvas with saves through edit-document on.
func quipEnv(t *testing.T) (*testHost, *fakeslack.Server, *View, *shooter, func()) {
	t.Helper()
	h, fs := newEnv(t, false)
	h.a.SetQuipEdit(true)
	fs.AddCanvas(fakeslack.FakeCanvas{ID: quipCanvasID, Title: "Quip saves", Doc: canvas.Doc{Blocks: quipBlocks()}})
	v := New(h, quipCanvasID, Full, Actions{})
	s := newShooter(t, image.Pt(1100, 900))
	frame := func() { s.frame(func(gtx layout.Context) { v.Layout(gtx) }) }
	settle(t, s, v, "the canvas", func() bool {
		return v.Doc() != nil && findBlk(v, func(b *canvas.Block) bool { return b.ID == quipFirstID }) != nil
	})
	frame()
	if !v.edit.canEdit(v) {
		t.Fatal("the Quip canvas isn't editable")
	}
	return h, fs, v, s, frame
}

// blockY is where block id's text sits in the rig's window.
func blockY(t *testing.T, r *pointerRig, id string) int {
	t.Helper()
	b := findBlk(r.v, func(b *canvas.Block) bool { return b.ID == id })
	if b == nil || b.rv == nil {
		t.Fatalf("no block %s", id)
	}
	full, _, ok := r.router.AreaBounds(b.rv)
	if !ok {
		t.Fatalf("block %s's text has no input area", id)
	}
	return full.Min.Y
}

func quipBlock(fs *fakeslack.Server, id string) *canvas.Block {
	fc, _ := fs.Canvas(quipCanvasID)
	return fc.Doc.Find(id)
}

func quipSaves(fs *fakeslack.Server) []fakeslack.QuipRequest {
	var out []fakeslack.QuipRequest
	for _, r := range fs.QuipRequests() {
		if r.Path == "edit-document" {
			out = append(out, r)
		}
	}
	return out
}

func cantEdits(h *testHost) int {
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

// With the switch on, each edit Clack can make the way Slack's client does
// is one edit-document the fake takes (typed text, a ticked box, a rename,
// a paragraph emptied), and none goes through canvases.edit. What it can't
// make yet (text with a mention, a new block) says so and sends nothing.
func TestCanvasQuipSaves(t *testing.T) {
	h, fs, v, s, frame := quipEnv(t)
	ok := func(what string, n int) {
		t.Helper()
		rs := quipSaves(fs)
		if len(rs) != n {
			t.Fatalf("after %s: %d edit-document calls, want %d", what, len(rs), n)
		}
		if r := rs[n-1]; r.Status != http.StatusOK {
			t.Errorf("%s: the fake answered %d (%s)", what, r.Status, r.Why)
		}
	}

	// typed text
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipFirstID }))
	frame()
	ed := v.edit.cur.ed
	ed.SetSelection(0, ed.Doc().Len())
	ed.InsertText("First paragraph, typed")
	frame()
	if !v.edit.finish(v) {
		t.Fatal("the typed paragraph was refused")
	}
	settle(t, s, v, "the typed paragraph", func() bool { b := quipBlock(fs, quipFirstID); return b != nil && b.Text() == "First paragraph, typed" })
	ok("typing", 1)

	// the box
	v.edit.toggleCheck(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipCheckID }))
	settle(t, s, v, "the tick", func() bool { b := quipBlock(fs, quipCheckID); return b != nil && b.Checked })
	ok("the tick", 2)

	// a rename: the title section's text
	v.startRename()
	frame()
	v.edit.rename.SetText("Renamed through Quip")
	v.edit.commitRename(v)
	settle(t, s, v, "the rename", func() bool { fc, _ := fs.Canvas(quipCanvasID); return fc.Title == "Renamed through Quip" })
	ok("the rename", 3)

	// a paragraph emptied: its section deleted
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipSecondID }))
	frame()
	ed = v.edit.cur.ed
	ed.SetSelection(0, ed.Doc().Len())
	ed.InsertText("")
	v.edit.finish(v)
	settle(t, s, v, "the delete", func() bool { return quipBlock(fs, quipSecondID) == nil })
	ok("the delete", 4)
	if n := cantEdits(h); n != 0 {
		t.Errorf("%d can't-edit toasts for saves Clack makes", n)
	}

	// text with a mention: refused in the editor, which stays open
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipMentID }))
	frame()
	if v.edit.cur == nil {
		t.Fatal("the mention's paragraph didn't open")
	}
	ed = v.edit.cur.ed
	ed.SetSelection(0, 0)
	ed.InsertText("Please ")
	frame()
	if v.edit.finish(v) || v.edit.cur == nil || !v.edit.cur.refused || cantEdits(h) != 1 {
		t.Errorf("text with a mention: finished, editor %v, toasts %d", v.edit.cur != nil, cantEdits(h))
	}
	v.edit.discard(v)

	// a new block
	v.StartNewBlock()
	frame()
	if v.edit.cur != nil || cantEdits(h) != 2 {
		t.Errorf("a new block: editor %v, toasts %d", v.edit.cur != nil, cantEdits(h))
	}
	time.Sleep(200 * time.Millisecond)
	frame()
	if n := len(quipSaves(fs)); n != 4 {
		t.Errorf("%d edit-document calls after the refusals, want 4", n)
	}
	if es := fs.CanvasEdits(); len(es) != 0 {
		t.Errorf("%d canvases.edit calls, want none: %v", len(es), es)
	}
}

// Two failed saves in a row are Slack's error state: QuipEmbed's banner
// over the canvas, pushing it down, and no editing until a save succeeds.
// A block open when it came keeps its text, unsaved, until Escape.
func TestCanvasQuipSaveBanner(t *testing.T) {
	h, fs, v, s, frame := quipEnv(t)
	r := &pointerRig{t: t, v: v, sz: image.Pt(1100, 900)} // the block's place in the window
	r.frame()
	before := blockY(t, r, quipFirstID)
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 2})
	ck := findBlk(v, func(b *canvas.Block) bool { return b.ID == quipCheckID })
	v.edit.toggleCheck(v, ck)
	settle(t, s, v, "the first failure", func() bool { return len(quipSaves(fs)) == 1 && h.a.CanvasSaveState(quipCanvasID).Pending })
	if b := v.saveBanner(); b != "" {
		t.Errorf("one failure: banner %q", b)
	}
	// a paragraph open, then the box's second save fails too
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipFirstID }))
	frame()
	if v.edit.cur == nil {
		t.Fatal("the paragraph didn't open after one failure")
	}
	v.edit.toggleCheck(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipCheckID }))
	settle(t, s, v, "the error state", func() bool { return h.a.CanvasSaveState(quipCanvasID).ReadOnly })
	frame()
	if b := v.saveBanner(); b != "quipOffline" {
		t.Errorf("the error state's banner %q, want quipOffline", b)
	}
	if v.edit.canEdit(v) {
		t.Error("editing is open in the error state")
	}
	r.frame()
	if after := blockY(t, r, quipFirstID); after < before+48 {
		t.Errorf("the banner didn't push the canvas down: the first block at y %d, %d before", after, before)
	}
	// what the open paragraph holds stays there, unsent
	ed := v.edit.cur.ed
	ed.SetSelection(ed.Doc().Len(), ed.Doc().Len())
	ed.InsertText(" again")
	frame()
	if v.edit.finish(v) || v.edit.cur == nil || !v.edit.cur.refused {
		t.Errorf("finishing in the error state: closed %v", v.edit.cur == nil)
	}
	if n := len(quipSaves(fs)); n != 2 {
		t.Errorf("%d edit-document calls, want the box's 2", n)
	}
	v.edit.discard(v)
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipSecondID }))
	frame()
	if v.edit.cur != nil {
		t.Error("a block opened in the error state")
	}
	if es := fs.CanvasEdits(); len(es) != 0 {
		t.Errorf("%d canvases.edit calls, want none", len(es))
	}
}
