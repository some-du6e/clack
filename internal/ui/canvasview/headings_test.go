package canvasview

// Clicks on block text, quotes, the title placeholder, tables, the
// side panel / full view headers and the insertion toolbar's inserts
// (docs/CANVASES.md). Synthetic canvases only, added to the fake server.

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	headCanvas  = "F0CANVHEAD1" // a title, headings, a list, a two-paragraph quote, a table
	headEmpty   = "F0CANVHEAD2" // a new canvas: an empty title section and an empty line
	headWide    = "F0CANVHEAD3" // a table whose stored column widths overflow the page
	headH2Text  = "Heading two"
	headQuote1  = "First quoted line"
	headQuote2  = "Second quoted line"
	headH2ID    = "temp:C:HEAD00002"
	headQ1ID    = "temp:C:HEAD00005"
	headQ2ID    = "temp:C:HEAD00006"
	headBodyTxt = "Body text under the heading."
)

func txt(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }

func cells(rows ...[]string) [][]canvas.Cell {
	var out [][]canvas.Cell
	for _, r := range rows {
		var row []canvas.Cell
		for _, c := range r {
			row = append(row, canvas.Cell{Blocks: []canvas.Block{{Kind: canvas.Paragraph, Spans: txt(c)}}})
		}
		out = append(out, row)
	}
	return out
}

func addHeadCanvases(fs *fakeslack.Server) {
	now := time.Now().Unix()
	fs.AddCanvas(fakeslack.FakeCanvas{ID: headCanvas, Title: "Plan", Created: now - 3*86400, Updated: now - 5*60, Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:HEAD00001", Kind: canvas.Heading1, Spans: txt("Plan")},
		{ID: headH2ID, Kind: canvas.Heading2, Spans: txt(headH2Text)},
		{ID: "temp:C:HEAD00003", Kind: canvas.Paragraph, Spans: txt(headBodyTxt)},
		{ID: "temp:C:HEAD00004", Kind: canvas.Bullet, Spans: txt("A list item")},
		{Kind: canvas.Quote, Children: []canvas.Block{
			{ID: headQ1ID, Kind: canvas.Paragraph, Spans: txt(headQuote1)},
			{ID: headQ2ID, Kind: canvas.Paragraph, Spans: txt(headQuote2)},
		}},
		{ID: "temp:C:HEAD00007", Kind: canvas.Check, Spans: txt("Something to do")},
		{ID: "temp:C:HEAD00008", Kind: canvas.Table, Header: true, Rows: cells(
			[]string{"Name", "Role"}, []string{"Ada", "Lead"}, []string{"Grace", "A longer cell that wraps onto a second line in its column"})},
		{ID: "temp:C:HEAD00009", Kind: canvas.Paragraph, Spans: txt("The end.")},
	}}})
	fs.AddCanvas(fakeslack.FakeCanvas{ID: headEmpty, Title: "Untitled", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:HEAD00010", Kind: canvas.Heading1},
		{ID: "temp:C:HEAD00011", Kind: canvas.Paragraph, Spans: txt("Time to document your genius!"), Default: true},
	}}})
	fs.AddCanvas(fakeslack.FakeCanvas{ID: headWide, Title: "Wide", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:HEAD00012", Kind: canvas.Heading1, Spans: txt("Wide")},
		{ID: "temp:C:HEAD00013", Kind: canvas.Table, Header: true, ColWidths: []int{260, 260, 260, 260},
			Rows: cells([]string{"Q1", "Q2", "Q3", "Q4"}, []string{"Plan", "Build", "Ship", "Rest"})},
	}}})
}

// headRig is the pointer rig on a fake canvas in the channel tab (900 x
// 700: the 710px column starts 96px in, the title 100px down).
func headRig(t *testing.T, dark bool, id string) (*testHost, *fakeslack.Server, *pointerRig) {
	t.Helper()
	h, fs := newEnv(t, dark)
	addHeadCanvases(fs)
	v := New(h, id, Tab, Actions{})
	r := &pointerRig{t: t, v: v, sz: image.Pt(900, 700)}
	deadline := time.Now().Add(20 * time.Second)
	for v.Doc() == nil || v.shown == nil {
		if time.Now().After(deadline) {
			t.Fatal("canvas didn't load")
		}
		r.frame()
		time.Sleep(10 * time.Millisecond)
	}
	r.frame()
	return h, fs, r
}

func (r *pointerRig) queue(evs ...pointer.Event) {
	for _, e := range evs {
		r.router.Queue(e)
	}
	r.frame()
}

func mouse(k pointer.Kind, x, y float32) pointer.Event {
	e := pointer.Event{Kind: k, Source: pointer.Mouse, Position: f32.Pt(x, y)}
	if k == pointer.Press || k == pointer.Drag {
		e.Buttons = pointer.ButtonPrimary
	}
	if k == pointer.Drag { // the router makes a Move with a button held a Drag
		e.Kind = pointer.Move
	}
	return e
}

// h2Pos is a point on the H2's text just after its first n runes: the
// H2 sits 24px (its margin, over the title's 20px) under the 34px title.
func h2Pos(r *pointerRig, n int) (x, y float32) {
	th := r.v.h.Theme()
	gtx := gtxFor(new(op.Ops), r.sz)
	w := th.Measure(gtx, h2Size, theme.Black, headH2Text[:n])
	return float32(96 + w), float32(docTop + titleLine + 24 + h2Line/2)
}

// A click on a heading's text (not beside it) opens it with the caret
// where the click landed; a drag selects from there; a double click
// selects the word. The read view keeps no selection behind.
func TestEditClickHeadingText(t *testing.T) {
	_, _, r := headRig(t, false, headCanvas)
	v := r.v
	x, y := h2Pos(r, 4) // "Head|ing two"
	r.click(x, y)
	s := v.edit.cur
	if s == nil || s.target == nil || s.target.b.ID != headH2ID {
		t.Fatalf("a click on the heading's text didn't open it (session %+v)", s)
	}
	if a, b := s.ed.Selection(); a != 4 || b != 4 {
		t.Errorf("caret at %d-%d, want 4 (where the click landed)", a, b)
	}
	if s.target.rv != nil && s.target.rv.HasSelection() {
		t.Error("the read view kept a selection")
	}
	v.edit.discard(v)
	r.frame()

	// drag from "He|" to past the end: the selection runs to the end
	x0, _ := h2Pos(r, 2)
	r.queue(mouse(pointer.Move, x0, y), mouse(pointer.Press, x0, y))
	for _, dx := range []float32{40, 120, 400} {
		r.queue(mouse(pointer.Drag, x0+dx, y))
	}
	r.queue(mouse(pointer.Release, x0+400, y))
	r.frame()
	s = v.edit.cur
	if s == nil || s.target == nil || s.target.b.ID != headH2ID {
		t.Fatal("the drag didn't open the heading")
	}
	if a, b := s.ed.Selection(); a != 2 || b != len(headH2Text) {
		t.Errorf("drag selected %d-%d, want 2-%d", a, b, len(headH2Text))
	}
	v.edit.discard(v)
	r.frame()

	// a double click on "two" selects it
	x2, _ := h2Pos(r, len("Heading t"))
	r.click(x2, y)
	r.click(x2, y)
	s = v.edit.cur
	if s == nil {
		t.Fatal("no session after the double click")
	}
	if a, b := s.ed.Selection(); min(a, b) != len("Heading ") || max(a, b) != len(headH2Text) {
		t.Errorf("double click selected %d-%d, want the word %q", a, b, "two")
	}
}

// A click on a quote's second paragraph, a list item's text and the
// body text each open that block; saving a quote's paragraph keeps it in
// the quote ("> …", one Quip section of style 16).
func TestEditClickQuoteAndListText(t *testing.T) {
	_, fs, r := headRig(t, false, headCanvas)
	v := r.v
	var q *blk
	for _, b := range v.blocks {
		if b.b.Kind == canvas.Quote {
			q = b
		}
	}
	if q == nil || len(q.kids) != 2 || q.kids[1].parent != q {
		t.Fatal("no two-paragraph quote")
	}
	v.edit.start(v, q.kids[1])
	r.frame()
	s := v.edit.cur
	if s == nil || !s.quoted {
		t.Fatal("the quote's paragraph didn't open as a quoted session")
	}
	s.ed.SetSelection(s.ed.Doc().Len(), s.ed.Doc().Len())
	s.ed.InsertText(" again")
	r.frame()
	if !v.edit.finish(v) {
		t.Fatal("save refused")
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(fs.CanvasEdits()) == 0 && time.Now().Before(deadline) {
		r.frame()
		time.Sleep(10 * time.Millisecond)
	}
	var cs []map[string]any
	es := fs.CanvasEdits()
	if len(es) != 1 || json.Unmarshal([]byte(es[0].Get("changes")), &cs) != nil || len(cs) != 1 {
		t.Fatalf("edits %v", es)
	}
	if md := cs[0]["document_content"].(map[string]any)["markdown"]; md != "> "+headQuote2+" again" || cs[0]["section_id"] != headQ2ID {
		t.Errorf("quote change %v", cs[0])
	}
	if len(q.kids) != 2 || q.kids[1].b.Text() != headQuote2+" again" || q.kids[1].parent != q {
		t.Error("the quote's paragraph wasn't replaced in place")
	}
	inQuote := func() (applied, nested bool) {
		fc, _ := fs.Canvas(headCanvas)
		if b := fc.Doc.Find(headQ2ID); b == nil || b.Text() != headQuote2+" again" {
			return false, false
		}
		for _, top := range fc.Doc.Blocks {
			if top.Kind == canvas.Quote && len(top.Children) == 2 && top.Children[1].ID == headQ2ID && top.Children[1].Kind == canvas.Paragraph {
				return true, false
			}
		}
		return true, true
	}
	for {
		applied, nested := inQuote()
		if nested {
			t.Fatal("the fake nested the quote instead of replacing its paragraph")
		}
		if applied {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake didn't apply the quote edit")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Slack draws one bar per quote paragraph (each is its own section):
// 3px padding above and below each paragraph, 12px between them, the
// bar 4px in from each paragraph's top and bottom.
func TestRenderQuoteBars(t *testing.T) {
	h, _ := newEnv(t, false)
	v := New(h, "F0QUOTE0001", Tab, Actions{})
	q := &blk{b: &canvas.Block{Kind: canvas.Quote}}
	for _, s := range []string{"one", "two"} {
		k := &blk{b: &canvas.Block{Kind: canvas.Paragraph, Spans: txt(s)}, parent: q}
		doc, _ := v.richDoc(nil, k.b)
		k.rv = richview.New(doc, richview.Options{})
		q.kids = append(q.kids, k)
	}
	v.styles.sync(h.th, h)
	ops := new(op.Ops)
	gtx := gtxFor(ops, image.Pt(700, 400))
	gtx.Constraints = layout.Constraints{Max: image.Pt(700, 400)}
	if got, want := v.layoutQuote(gtx, q, 700), 2*(bodyLine+6)+12; got != want {
		t.Errorf("quote height %d, want %d (two sections, 12px apart)", got, want)
	}
}

// An empty title section shows Slack's "Your canvas title" to editors,
// and the empty line under it the placeholder picked by file id.
func TestRenderTitlePlaceholder(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h, fs := newEnv(t, dark)
		addHeadCanvases(fs)
		v := New(h, headEmpty, Tab, Actions{})
		s := newShooter(t, image.Pt(1038, 717))
		settle(t, s, v, "the empty canvas", func() bool { return v.Doc() != nil && v.shown != nil })
		ops := frames(s, v, 20)
		if !titleSection(v.blocks) || !emptyTitle(v.blocks[0]) {
			t.Fatal("the empty H1 isn't the title section")
		}
		if v.tphView == nil || !strings.Contains(v.tphView.Doc().Blocks[0].Spans[0].Text, titlePlaceholder) {
			t.Error("no title placeholder drawn")
		}
		if v.placeholderRow() != v.blocks[1] {
			t.Error("no text placeholder on the empty line")
		}
		s.shot("headings-title-placeholder-"+mode(dark), ops)
		// editing the empty title keeps the placeholder in the editor
		v.edit.start(v, v.blocks[0])
		frames(s, v, 3)
		if v.edit.cur == nil {
			t.Fatal("the empty title didn't open")
		}
		v.edit.discard(v)
	}
}

func mode(dark bool) string {
	if dark {
		return "dark"
	}
	return "light"
}

// Tables are drawn like Slack's canvas tables: even columns filling the
// column, 1px collapsed borders, 46px rows, a tinted header row; stored
// widths wider than the page scroll sideways.
func TestRenderTables(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h, fs := newEnv(t, dark)
		addHeadCanvases(fs)
		v := New(h, headCanvas, Full, Actions{Share: func(string) {}, SideView: func() {}})
		s := newShooter(t, image.Pt(1200, 900))
		settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil && v.shown != nil })
		var tb *blk
		for _, b := range v.blocks {
			if b.b.Kind == canvas.Table {
				tb = b
			}
		}
		if tb == nil || len(tb.cells) != 3 || len(tb.cells[0]) != 2 {
			t.Fatal("table cells not built")
		}
		for _, row := range tb.cells {
			for _, c := range row {
				for _, k := range c.kids {
					if !k.inCell {
						t.Error("a table cell's block is editable")
					}
				}
			}
		}
		if sp := tb.cells[0][0].kids[0].b.Spans; len(sp) == 0 || sp[0].Style&richtext.Bold == 0 {
			t.Error("the header row isn't bold")
		}
		gtx := gtxFor(new(op.Ops), image.Pt(800, 600))
		ws := colWidths(gtx, tb.b, 2, 800)
		if ws[0]+ws[1] != 799 || ws[0] != 399 {
			t.Errorf("column widths %v, want the column split evenly", ws)
		}
		v.list.Position.First = 6 // scroll the table into view
		s.shot("headings-table-full-"+mode(dark), frames(s, v, 20))

		w := New(h, headWide, Tab, Actions{})
		ws2 := newShooter(t, image.Pt(900, 500))
		settle(t, ws2, w, "the wide table", func() bool { return w.Doc() != nil && w.shown != nil })
		ws2.shot("headings-table-wide-"+mode(dark), frames(ws2, w, 20))
		var wt *blk
		for _, b := range w.blocks {
			if b.b.Kind == canvas.Table {
				wt = b
			}
		}
		if wt == nil {
			t.Fatal("no wide table")
		}
		wt.tscr.x = 1 << 20 // scrolled all the way: clamped to the overflow
		frames(ws2, w, 2)
		if want := 4*260 + 1 - 710; wt.tscr.x != want {
			t.Errorf("wide table scroll %d, want %d", wt.tscr.x, want)
		}
		ws2.shot("headings-table-wide-scrolled-"+mode(dark), frames(ws2, w, 2))
	}
}

// The side panel and full view headers are Slack's canvas flexpane
// header: the title in 18px Black, Edited … / the facepile / Share / More
// actions in the full view, More actions and close in the side panel.
func TestRenderHeaders(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h, fs := newEnv(t, dark)
		addHeadCanvases(fs)
		for _, m := range []struct {
			mode Mode
			name string
			sz   image.Point
		}{{Side, "side", image.Pt(480, 717)}, {Full, "full", image.Pt(1200, 800)}} {
			v := New(h, headCanvas, m.mode, Actions{Close: func() {}, FullView: func() {}, SideView: func() {}, Share: func(string) {}})
			s := newShooter(t, m.sz)
			settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil && v.shown != nil })
			s.shot("headings-header-"+m.name+"-"+mode(dark), frames(s, v, 20))
			_, hasShare := v.btns["bar.share"]
			_, hasSide := v.btns["bar.side"]
			_, hasClose := v.btns["bar.close"]
			if hasShare != (m.mode == Full) || hasSide || hasClose != (m.mode == Side) {
				t.Errorf("%s header buttons: share %v side %v close %v", m.name, hasShare, hasSide, hasClose)
			}
		}
	}
	for _, c := range []struct {
		d    time.Duration
		want string
	}{{10 * time.Second, "just now"}, {5 * time.Minute, "5 min ago"}, {time.Hour, "1 hour ago"}, {50 * time.Hour, "2 days ago"}} {
		if got := editedAgo(time.Now().Add(-c.d)); got != c.want {
			t.Errorf("editedAgo(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// The toolbar's inserts go through canvases.edit: Insert table is Quip's
// new 3×2 table, Insert > Image uploads the picked file and places it by
// permalink, Insert file embeds a file; layouts stays disabled.
func TestEditInserts(t *testing.T) {
	h, fs := newEnv(t, false)
	addHeadCanvases(fs)
	v := New(h, headCanvas, Full, Actions{})
	s := newShooter(t, image.Pt(1100, 900))
	settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil && v.shown != nil })
	e := v.edit
	if !e.insEnabled("table") || !e.insEnabled("file") || e.insEnabled("layouts") {
		t.Error("insertion toolbar: table and file should work, layouts stay disabled")
	}
	edits := func() int { return len(fs.CanvasEdits()) }

	// Insert table after the body paragraph being edited
	body := findBlk(v, func(b *canvas.Block) bool { return b.Text() == headBodyTxt })
	e.start(v, body)
	frames(s, v, 2)
	e.insertTable(v)
	settle(t, s, v, "the table insert", func() bool { return edits() == 1 })
	c := lastChange(t, fs)
	if c["operation"] != "insert_after" || c["section_id"] != body.b.ID {
		t.Errorf("table change %v", c)
	}
	settle(t, s, v, "the fake's table", func() bool {
		fc, _ := fs.Canvas(headCanvas)
		for i, b := range fc.Doc.Blocks {
			if b.ID == body.b.ID && i+1 < len(fc.Doc.Blocks) {
				n := fc.Doc.Blocks[i+1]
				return n.Kind == canvas.Table && !n.Header && len(n.Rows) == 3 && len(n.Rows[0]) == 2
			}
		}
		return false
	})

	// Insert > Image: a PNG the "chooser" returns
	var pngb bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 64, 40))
	for i := range img.Pix {
		img.Pix[i] = 0xc0
	}
	img.Set(3, 3, color.NRGBA{R: 0xff, A: 0xff})
	_ = png.Encode(&pngb, img)
	old := chooseFile
	t.Cleanup(func() { chooseFile = old })
	chooseFile = func(ctx context.Context) (string, []byte, error) { return "chart.png", pngb.Bytes(), nil }
	e.insertUpload(v, true)
	settle(t, s, v, "the image insert", func() bool { return edits() == 2 })
	md, _ := lastChange(t, fs)["document_content"].(map[string]any)["markdown"].(string)
	if !strings.HasPrefix(md, "![chart.png](") || !strings.Contains(md, "/files/") {
		t.Errorf("image markdown %q", md)
	}
	settle(t, s, v, "the image in the view", func() bool {
		b := findBlk(v, func(b *canvas.Block) bool { return b.Kind == canvas.Image && b.FileID != "" })
		return b != nil && b.rv != nil
	})

	// Insert file: a PDF embeds as a file
	chooseFile = func(ctx context.Context) (string, []byte, error) {
		return "notes.pdf", []byte("%PDF-1.4 synthetic"), nil
	}
	e.insertUpload(v, false)
	settle(t, s, v, "the file insert", func() bool { return edits() == 3 })
	if md, _ := lastChange(t, fs)["document_content"].(map[string]any)["markdown"].(string); !strings.HasPrefix(md, "![](") {
		t.Errorf("file markdown %q", md)
	}
	settle(t, s, v, "the file in the view", func() bool {
		return findBlk(v, func(b *canvas.Block) bool { return b.Kind == canvas.File && b.FileID != "" }) != nil
	})
	s.shot("headings-inserts", frames(s, v, 20))

	// Insert > Profile: a new line with the mention picker open
	e.insertProfile(v)
	frames(s, v, 3)
	if e.cur == nil || e.cur.place == nil || e.cur.ed.Doc().LineText(0) != "@" {
		t.Error("Profile didn't start a mention on a new line")
	}
	e.discard(v)
	_ = h
}
