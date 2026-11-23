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
	quipQuoteID  = "temp:C:QSV00005"
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
	return quipEnvBlocks(t, quipBlocks())
}

// quipEnvBlocks opens a Quip canvas of blocks bs (the first quipFirstID)
// with saves through edit-document on.
func quipEnvBlocks(t *testing.T, bs []canvas.Block) (*testHost, *fakeslack.Server, *View, *shooter, func()) {
	t.Helper()
	h, fs := newEnv(t, false)
	h.a.SetQuipEdit(true)
	fs.AddCanvas(fakeslack.FakeCanvas{ID: quipCanvasID, Title: "Quip saves", Doc: canvas.Doc{Blocks: bs}})
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
// a paragraph emptied, text typed around a mention, which stays, a new
// block under the last one), and none goes through canvases.edit.
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

	// a new block under the last one: a new section after it
	v.StartNewBlock()
	frame()
	if v.edit.cur == nil {
		t.Fatal("no new block under the last one")
	}
	if got := v.edit.cur.ed.MaxIndent; got != quip.MaxListLevel {
		t.Errorf("a new block's editor makes lists %d deep, want %d", got, quip.MaxListLevel)
	}
	v.edit.cur.ed.InsertText("A new paragraph")
	frame()
	if !v.edit.finish(v) {
		t.Fatal("a new paragraph was refused")
	}
	settle(t, s, v, "the new paragraph", func() bool {
		fc, _ := fs.Canvas(quipCanvasID)
		n := len(fc.Doc.Blocks)
		return n > 0 && fc.Doc.Blocks[n-1].Text() == "A new paragraph"
	})
	ok("a new block", 6)
	if n := cantEdits(h); n != 0 {
		t.Errorf("%d can't-edit toasts", n)
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
	if v.edit.finish(v) || v.edit.cur == nil {
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

// Enter in a block saves the new block after it, in one save with the
// block's own text: at the end of a paragraph (the paragraph as it was, a
// new one after it), in a checklist item (a new item in its list). A new
// block that starts a list after a paragraph can't be saved yet: the
// editor says so and stays open.
func TestCanvasQuipNewBlocks(t *testing.T) {
	h, fs, v, s, frame := quipEnv(t)
	settle(t, s, v, "the canvas's model", func() bool { return h.a.NewCanvasSectionID(quipCanvasID) != "" })
	blocks := func() []canvas.Block { fc, _ := fs.Canvas(quipCanvasID); return fc.Doc.Blocks }
	after := func(id string) *canvas.Block {
		bs := blocks()
		for i := range bs {
			if bs[i].ID == id && i+1 < len(bs) {
				return &bs[i+1]
			}
		}
		return nil
	}
	enter := func(id, typed string) bool {
		t.Helper()
		v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == id }))
		frame()
		if v.edit.cur == nil {
			t.Fatalf("block %s didn't open", id)
		}
		ed := v.edit.cur.ed
		ed.SetSelection(ed.Doc().Len(), ed.Doc().Len())
		ed.InsertText(typed)
		for i := 0; i < 3; i++ {
			frame()
		}
		return v.edit.finish(v)
	}
	if !enter(quipSecondID, "\nTyped after Enter") {
		t.Fatal("Enter at the end of a paragraph was refused")
	}
	settle(t, s, v, "the paragraph after Enter", func() bool { b := after(quipSecondID); return b != nil && b.Text() == "Typed after Enter" })
	if r := quipSaves(fs); len(r) != 1 || len(r[0].Save.Sections) != 2 {
		t.Errorf("Enter: %d saves, want 1 with the paragraph and the new one", len(r))
	}
	if !enter(quipCheckID, "\nAnother item") {
		t.Fatal("Enter in a checklist item was refused")
	}
	settle(t, s, v, "the new checklist item", func() bool {
		b := after(quipCheckID)
		return b != nil && b.Kind == canvas.Check && b.Text() == "Another item" && !b.Checked
	})
	n := len(quipSaves(fs))
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipFirstID }))
	frame()
	ed := v.edit.cur.ed
	ed.SetSelection(ed.Doc().Len(), ed.Doc().Len())
	ed.InsertText("\na list")
	ed.ToggleBlock(composer.Bullet) // the new line made a bullet
	frame()
	if v.edit.finish(v) || v.edit.cur == nil || cantEdits(h) != 1 {
		t.Errorf("a new list after a paragraph: editor %v, toasts %d", v.edit.cur != nil, cantEdits(h))
	}
	if v.edit.escape(v) || v.edit.cur == nil || cantEdits(h) != 2 { // Escape again keeps the text, and says so again
		t.Errorf("Escape after the refusal: closed %v, toasts %d", v.edit.cur == nil, cantEdits(h))
	}
	v.edit.discard(v)
	time.Sleep(200 * time.Millisecond)
	if got := len(quipSaves(fs)); got != n {
		t.Errorf("%d edit-document calls for a refused new list", got-n)
	}
	if es := fs.CanvasEdits(); len(es) != 0 {
		t.Errorf("%d canvases.edit calls, want none", len(es))
	}
}

// A format chosen in the editor saves as Slack's setSectionStyle: the first
// paragraph made a heading 2 from the toolbar (no text typed) is one save
// with its style alone, and the fake's block is a heading.
func TestCanvasQuipRestyle(t *testing.T) {
	_, fs, v, s, frame := quipEnv(t)
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipFirstID }))
	frame()
	if v.edit.cur == nil {
		t.Fatal("the paragraph didn't open")
	}
	if got := v.edit.cur.ed.MaxIndent; got != quip.MaxListLevel {
		t.Errorf("the block's editor makes lists %d deep, want Slack's canvas editor's %d", got, quip.MaxListLevel)
	}
	v.edit.apply(v, "h2", image.Point{})
	frame()
	if !v.edit.finish(v) {
		t.Fatal("a heading 2 was refused")
	}
	settle(t, s, v, "the heading", func() bool { b := quipBlock(fs, quipFirstID); return b != nil && b.Kind == canvas.Heading2 })
	rs := quipSaves(fs)
	if len(rs) != 1 || len(rs[0].Save.Sections) != 1 {
		t.Fatalf("%d saves", len(rs))
	}
	if got := quip.FieldsString(&rs[0].Save.Sections[0]); got != "1,2,9,10,13,19,20,33" {
		t.Errorf("a restyle's fields %s, want the style alone", got)
	}
}

// A quote's paragraph (a section of style 16) saves its text through
// edit-document as a text edit of its section, which stays in the quote;
// Enter in it (a second quote paragraph) waits for the rest of B3.
func TestCanvasQuipQuoteParagraph(t *testing.T) {
	quote := canvas.Block{Kind: canvas.Quote, Children: []canvas.Block{{ID: quipQuoteID, Kind: canvas.Paragraph, Spans: txt("Keep it short.")}}}
	h, fs, v, s, frame := quipEnvBlocks(t, append(quipBlocks(), quote))
	open := func() {
		t.Helper()
		q := findBlk(v, func(b *canvas.Block) bool { return b.Kind == canvas.Quote })
		if q == nil || len(q.kids) != 1 {
			t.Fatal("no quote with one paragraph")
		}
		v.edit.start(v, q.kids[0])
		frame()
		if v.edit.cur == nil || !v.edit.cur.quoted {
			t.Fatal("the quote's paragraph didn't open as one")
		}
	}
	open()
	ed := v.edit.cur.ed
	ed.SetSelection(0, ed.Doc().Len())
	ed.InsertText("Keep it short, please.")
	frame()
	if !v.edit.finish(v) {
		t.Fatal("the quote's paragraph was refused")
	}
	settle(t, s, v, "the quote's new text", func() bool {
		fc, _ := fs.Canvas(quipCanvasID)
		for _, b := range fc.Doc.Blocks {
			if b.Kind == canvas.Quote && len(b.Children) == 1 && b.Children[0].Text() == "Keep it short, please." {
				return true
			}
		}
		return false
	})
	rs := quipSaves(fs)
	if len(rs) != 1 || len(rs[0].Save.Sections) != 1 {
		t.Fatalf("%d saves", len(rs))
	}
	sec := &rs[0].Save.Sections[0]
	if got := quip.FieldsString(sec); got != "1,2,9,12,13,19,20,33" || sec.ID == nil || *sec.ID != quipQuoteID {
		t.Errorf("the save's section %s, fields %s: want the quote's text alone", quip.SectionRTML(sec), got)
	}

	// Enter: a new quote paragraph isn't saved yet
	open()
	ed = v.edit.cur.ed
	ed.SetSelection(ed.Doc().Len(), ed.Doc().Len())
	ed.InsertText("\nAnd clear.")
	frame()
	if v.edit.finish(v) || v.edit.cur == nil || cantEdits(h) != 1 {
		t.Errorf("a second quote paragraph: editor %v, toasts %d", v.edit.cur != nil, cantEdits(h))
	}
	v.edit.discard(v)
	time.Sleep(200 * time.Millisecond)
	if n := len(quipSaves(fs)); n != 1 {
		t.Errorf("%d edit-document calls, want the first one", n)
	}
	if es := fs.CanvasEdits(); len(es) != 0 {
		t.Errorf("%d canvases.edit calls, want none", len(es))
	}
}

// toasts counts the toasts that start with prefix.
func toasts(h *testHost, prefix string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, m := range h.toasts {
		if strings.HasPrefix(m, prefix) {
			n++
		}
	}
	return n
}

// Under the save error state's lock, Escape keeps an open block's text,
// and Escape again too: Slack keeps text typed before cant_sync and saves
// it once a save succeeds, and has no way to throw it away.
func TestCanvasQuipLockKeepsText(t *testing.T) {
	h, fs, v, s, frame := quipEnv(t)
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 2})
	box := func() *blk { return findBlk(v, func(b *canvas.Block) bool { return b.ID == quipCheckID }) }
	v.edit.toggleCheck(v, box())
	settle(t, s, v, "the first failure", func() bool { return len(quipSaves(fs)) == 1 && h.a.CanvasSaveState(quipCanvasID).Pending })
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipFirstID }))
	frame()
	if v.edit.cur == nil {
		t.Fatal("the paragraph didn't open")
	}
	ed := v.edit.cur.ed
	ed.SetSelection(ed.Doc().Len(), ed.Doc().Len())
	ed.InsertText(", kept")
	frame()
	v.edit.toggleCheck(v, box())
	settle(t, s, v, "the error state", func() bool { return h.a.CanvasSaveState(quipCanvasID).ReadOnly })
	for i := 1; i <= 2; i++ {
		if v.edit.escape(v) || v.edit.cur == nil {
			t.Fatalf("Escape %d under the lock closed the block", i)
		}
	}
	if got := v.edit.cur.ed.Doc().Text(); got != "First paragraph, kept" {
		t.Errorf("the block holds %q", got)
	}
	if n := toasts(h, lockedText); n != 2 {
		t.Errorf("%d lock toasts, want one each Escape", n)
	}
}

// A conflict found on an attempt that waited behind another save (finish
// heard nothing of it) is told as a direct one is: the toast, and the
// canvas reloads.
func TestCanvasQuipDeferredConflictToast(t *testing.T) {
	h, fs, v, s, frame := quipEnv(t)
	typed := func(id, add string) {
		t.Helper()
		v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == id }))
		frame()
		if v.edit.cur == nil {
			t.Fatalf("block %s didn't open", id)
		}
		ed := v.edit.cur.ed
		ed.SetSelection(ed.Doc().Len(), ed.Doc().Len())
		ed.InsertText(add)
		frame()
		if !v.edit.finish(v) {
			t.Fatalf("block %s's edit was refused", id)
		}
	}
	before := fs.CallCount("canvas/edit-document")
	fs.SetFault("canvas/edit-document", fakeslack.Fault{ReplyDelay: 700 * time.Millisecond, Times: 1})
	typed(quipFirstID, ", one")
	settle(t, s, v, "the first save going out", func() bool { return fs.CallCount("canvas/edit-document") > before })
	typed(quipSecondID, ", A")
	if !fs.ReplaceCanvasBlockAsOther(quipCanvasID, quipSecondID, "Second paragraph, B") {
		t.Fatal("no second paragraph to replace")
	}
	settle(t, s, v, "the conflict's toast", func() bool { return toasts(h, changedMeanwhile) == 1 })
	settle(t, s, v, "the canvas reloaded with B's paragraph", func() bool {
		b := findBlk(v, func(b *canvas.Block) bool { return b.ID == quipSecondID })
		return b != nil && b.b.Text() == "Second paragraph, B"
	})
}

// An underline-only change, or a link made with the Link button over its
// own address, saves: canvas markdown can't tell either from the text as
// it was (no underline; a link that is its URL is bare text), so the
// session had closed without a save.
func TestCanvasQuipFormatOnlySaves(t *testing.T) {
	const linkID = "temp:C:QSV00007"
	bs := append(quipBlocks(), canvas.Block{ID: linkID, Kind: canvas.Paragraph, Spans: txt("See https://example.com/docs")})
	_, fs, v, s, frame := quipEnvBlocks(t, bs)
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipFirstID }))
	frame()
	ed := v.edit.cur.ed
	ed.SetSelection(len("First "), len("First paragraph"))
	ed.ToggleStyle(composer.Underline)
	frame()
	if !v.edit.finish(v) {
		t.Fatal("an underlined word was refused")
	}
	settle(t, s, v, "the underline's save", func() bool { return len(quipSaves(fs)) == 1 })
	if r := quipSaves(fs)[0]; !strings.Contains(quip.SectionRTML(&r.Save.Sections[0]), "<u>paragraph</u>") {
		t.Errorf("the save's section %q", quip.SectionRTML(&r.Save.Sections[0]))
	}
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == linkID }))
	frame()
	ed = v.edit.cur.ed
	ed.SetSelection(len("See "), ed.Doc().Len())
	v.edit.apply(v, "link", image.Point{})
	frame()
	if !v.edit.finish(v) {
		t.Fatal("a link made with the Link button was refused")
	}
	settle(t, s, v, "the link's save", func() bool { return len(quipSaves(fs)) == 2 })
	if r := quipSaves(fs)[1]; !strings.Contains(quip.SectionRTML(&r.Save.Sections[0]), `<a href="https://example.com/docs">https://example.com/docs</a>`) {
		t.Errorf("the save's section %q", quip.SectionRTML(&r.Save.Sections[0]))
	}
}

// A block holding a line break isn't opened with the switch on: the
// editor makes each line a block of its own, so the block would come back
// split and save as two sections, even left as it was.
func TestCanvasQuipLineBreakNotEditable(t *testing.T) {
	const brID = "temp:C:QSV00008"
	bs := append(quipBlocks(), canvas.Block{ID: brID, Kind: canvas.Paragraph, Spans: []richtext.Span{
		{Kind: richtext.Text, Text: "two"}, {Kind: richtext.Break}, {Kind: richtext.Text, Text: "lines"}}})
	_, fs, v, _, _ := quipEnvBlocks(t, bs)
	b := findBlk(v, func(b *canvas.Block) bool { return b.ID == brID })
	if b == nil || b.edOK || b.edWhy != "a line break" {
		t.Errorf("a block with a line break: %+v", b)
	}
	v.edit.start(v, b)
	if v.edit.cur != nil {
		t.Error("the block opened")
	}
	if n := len(quipSaves(fs)); n != 0 {
		t.Errorf("%d edit-document calls", n)
	}
}

// On edit-document's path the editor keeps what a block holds as it is: a
// heading's bold and a trailing no-break space aren't saved away, by a
// block left as it was (nothing is sent) or by text typed in it; an empty
// block left empty isn't deleted.
func TestCanvasQuipKeepsWhatItHolds(t *testing.T) {
	const head, nbsp, empty = "temp:C:QSV00011", "temp:C:QSV00012", "temp:C:QSV00013"
	_, fs, v, s, frame := quipEnvBlocks(t, []canvas.Block{
		{ID: quipFirstID, Kind: canvas.Paragraph, Spans: txt("First paragraph")},
		{ID: head, Kind: canvas.Heading2, Spans: []richtext.Span{{Kind: richtext.Text, Text: "Q3 "}, {Kind: richtext.Text, Text: "goals", Style: richtext.Bold}}},
		{ID: nbsp, Kind: canvas.Paragraph, Spans: txt("hello\u00a0")},
		{ID: empty, Kind: canvas.Paragraph},
		{ID: quipSecondID, Kind: canvas.Paragraph, Spans: txt("Last paragraph")},
	})
	open := func(id string) *composer.Editor {
		t.Helper()
		b := findBlk(v, func(b *canvas.Block) bool { return b.ID == id })
		if b == nil {
			t.Fatalf("no block %s", id)
		}
		v.edit.start(v, b)
		frame()
		if v.edit.cur == nil {
			t.Fatalf("block %s didn't open", id)
		}
		return v.edit.cur.ed
	}
	for _, id := range []string{head, nbsp, empty} {
		open(id)
		if !v.edit.finish(v) {
			t.Fatalf("block %s, left as it was, was refused", id)
		}
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(quipSaves(fs)); n != 0 {
		t.Errorf("%d edit-document calls for blocks left as they were", n)
	}
	if quipBlock(fs, empty) == nil {
		t.Error("the empty paragraph, left empty, was deleted")
	}

	ed := open(head)
	ed.SetSelection(0, 0)
	ed.InsertText("Our ")
	frame()
	if !v.edit.finish(v) {
		t.Fatal("text typed in the heading was refused")
	}
	settle(t, s, v, "the heading", func() bool { b := quipBlock(fs, head); return b != nil && strings.HasPrefix(b.Text(), "Our ") })
	rs := quipSaves(fs)
	if len(rs) != 1 || len(rs[0].Save.Sections) != 1 || !strings.Contains(quip.SectionRTML(&rs[0].Save.Sections[0]), "<b>goals</b>") {
		t.Errorf("the heading's save took its bold away: %+v", rs)
	}

	ed = open(nbsp)
	ed.SetSelection(0, 0)
	ed.InsertText("Well, ")
	frame()
	if !v.edit.finish(v) {
		t.Fatal("text typed before a no-break space was refused")
	}
	settle(t, s, v, "the paragraph", func() bool { b := quipBlock(fs, nbsp); return b != nil && strings.HasPrefix(b.Text(), "Well, ") })
	rs = quipSaves(fs)
	if r := quip.SectionRTML(&rs[len(rs)-1].Save.Sections[0]); !strings.HasSuffix(r, "&nbsp;") && !strings.HasSuffix(r, "\u00a0") {
		t.Errorf("the paragraph's no-break space went: %q", r)
	}
}

// A new mention in a block is refused while its text is still in the
// editor (core doesn't save one yet): the editor stays open with it, and
// Escape again discards the change; nothing is sent.
func TestCanvasQuipNewMentionKeepsText(t *testing.T) {
	h, fs, v, _, frame := quipEnv(t)
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipFirstID }))
	frame()
	if v.edit.cur == nil {
		t.Fatal("the paragraph didn't open")
	}
	ed := v.edit.cur.ed
	n := ed.Doc().Len()
	ed.SetSelection(n, n)
	ed.InsertText(" for ")
	m := ed.Doc().Len()
	ed.InsertAtom(m, m, &composer.Atom{Kind: composer.AtomUser, ID: "U0FAKE00001", Text: "@Fake"})
	frame()
	if v.edit.finish(v) || v.edit.cur == nil {
		t.Fatal("a new mention closed the editor")
	}
	h.mu.Lock()
	last := ""
	if len(h.toasts) > 0 {
		last = h.toasts[len(h.toasts)-1]
	}
	h.mu.Unlock()
	if !strings.HasPrefix(last, "Clack can't save a new mention or emoji") {
		t.Errorf("the toast %q", last)
	}
	if !v.edit.escape(v) || v.edit.cur != nil {
		t.Error("Escape after the refusal didn't discard the change")
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(quipSaves(fs)); n != 0 {
		t.Errorf("%d edit-document calls for a refused mention", n)
	}
}
