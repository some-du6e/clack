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
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/composer"
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
	for i := 0; i < 10; i++ { // the router names an area once a frame has asked for its events
		if full, _, ok := r.router.AreaBounds(b.rv); ok {
			return full.Min.Y
		}
		r.frame()
	}
	t.Fatalf("block %s's text has no input area", id)
	return 0
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
// a paragraph emptied, text typed around a mention, which stays), and none
// goes through canvases.edit. What it can't make yet (a new block) says so
// and sends nothing.
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

	// text typed before a mention: the mention stays, the same control
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipMentID }))
	frame()
	if v.edit.cur == nil {
		t.Fatal("the mention's paragraph didn't open")
	}
	ed = v.edit.cur.ed
	ed.SetSelection(0, 0)
	ed.InsertText("Please ")
	frame()
	if !v.edit.finish(v) {
		t.Fatal("text typed before a mention was refused")
	}
	settle(t, s, v, "the paragraph with its mention", func() bool {
		b := quipBlock(fs, quipMentID)
		return b != nil && strings.HasPrefix(b.Text(), "Please Ask ")
	})
	ok("text around a mention", 5)
	if b := quipBlock(fs, quipMentID); len(b.Spans) == 0 || b.Spans[len(b.Spans)-1].Kind != richtext.User || b.Spans[len(b.Spans)-1].ID != "U0FAKE00001" {
		t.Errorf("the mention after the save: %+v", b.Spans)
	}

	// a new block
	v.StartNewBlock()
	frame()
	if v.edit.cur != nil || cantEdits(h) != 1 {
		t.Errorf("a new block: editor %v, toasts %d", v.edit.cur != nil, cantEdits(h))
	}
	time.Sleep(200 * time.Millisecond)
	frame()
	if n := len(quipSaves(fs)); n != 5 {
		t.Errorf("%d edit-document calls after the refusal, want 5", n)
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
	// where a block sits in the window: the router knows an area from
	// the frames after the one that laid it out
	r := &pointerRig{t: t, v: v, sz: image.Pt(1100, 900)}
	frames := func() {
		for i := 0; i < 3; i++ {
			r.frame()
		}
	}
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
	// the block under it, as drawn with the editor open (an open block
	// is the editor, not its text view)
	frames()
	before := blockY(t, r, quipSecondID)
	v.edit.toggleCheck(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipCheckID }))
	settle(t, s, v, "the error state", func() bool { return h.a.CanvasSaveState(quipCanvasID).ReadOnly })
	frame()
	if b := v.saveBanner(); b != "quipOffline" {
		t.Errorf("the error state's banner %q, want quipOffline", b)
	}
	if v.edit.canEdit(v) {
		t.Error("editing is open in the error state")
	}
	frames()
	if after := blockY(t, r, quipSecondID); after < before+48 {
		t.Errorf("the banner didn't push the canvas down: the second paragraph at y %d, %d before", after, before)
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

// Formatting typed in the editor saves through edit-document: bold
// toggled on a word of a plain paragraph, then the word typed over in
// italics, each one edit-document; the fake's block shows the styles.
func TestCanvasQuipRichSaves(t *testing.T) {
	_, fs, v, s, frame := quipEnv(t)
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipFirstID }))
	frame()
	ed := v.edit.cur.ed
	ed.SetSelection(len("First "), len("First paragraph"))
	ed.ToggleStyle(composer.Bold)
	frame()
	if !v.edit.finish(v) {
		t.Fatal("a bold word was refused")
	}
	styled := func(word string, st richtext.Style) func() bool {
		return func() bool {
			b := quipBlock(fs, quipFirstID)
			if b == nil {
				return false
			}
			for _, sp := range b.Spans {
				if sp.Text == word && sp.Style&st != 0 {
					return true
				}
			}
			return false
		}
	}
	settle(t, s, v, "the bold word", styled("paragraph", richtext.Bold))
	if n := len(quipSaves(fs)); n != 1 {
		t.Errorf("%d edit-document calls, want 1", n)
	}
	r := quipSaves(fs)[0]
	if len(r.Save.Sections) != 1 || !strings.Contains(quip.SectionRTML(&r.Save.Sections[0]), "<b>paragraph</b>") {
		t.Errorf("the save's section %+v", r.Save.Sections)
	}

	// an italic word typed at the end
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipFirstID }))
	frame()
	ed = v.edit.cur.ed
	ed.SetSelection(ed.Doc().Len(), ed.Doc().Len())
	ed.InsertText(" ")
	ed.ToggleStyle(composer.Italic)
	ed.InsertText("again")
	frame()
	if !v.edit.finish(v) {
		t.Fatal("an italic word was refused")
	}
	settle(t, s, v, "the italic word", styled("again", richtext.Italic))
	if es := fs.CanvasEdits(); len(es) != 0 {
		t.Errorf("%d canvases.edit calls, want none", len(es))
	}
}

// A canvas, file or message mention (Quip's control around a link) keeps
// its block out of the editor with the switch on too: the editor would
// hold it as a plain link, and a save would turn the mention into one.
func TestCanvasQuipControlLinkNotEditable(t *testing.T) {
	h, fs := newEnv(t, false)
	h.a.SetQuipEdit(true)
	src := `<div class="quip-canvas-content">` +
		`<p id="temp:C:QSV00009" class="line">See <control id="temp:C:CTLc0001">` +
		`<a href="https://fake.slack.com/docs/T0FAKE0001/F0CANVAS002">Weekly sync notes</a></control></p></div>`
	d, err := canvas.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	fs.AddCanvas(fakeslack.FakeCanvas{ID: "F0QUIPSAVE02", Title: "Mentions", HTML: src, Doc: d})
	v := New(h, "F0QUIPSAVE02", Full, Actions{})
	s := newShooter(t, image.Pt(1100, 900))
	settle(t, s, v, "the canvas", func() bool {
		return findBlk(v, func(b *canvas.Block) bool { return b.ID == "temp:C:QSV00009" }) != nil
	})
	b := findBlk(v, func(b *canvas.Block) bool { return b.ID == "temp:C:QSV00009" })
	if b.edOK || b.edWhy != "a canvas, file or message mention" {
		t.Errorf("a paragraph with a canvas mention: editable %v (%q)", b.edOK, b.edWhy)
	}
}
