package canvasview

// The B3 parts 2-3 review's MAJOR and MINOR in the view (wf_63029827-35f):
// a refetch from before a new block's create, a heading's bold.

import (
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
)

// A new block whose create hasn't landed survives a refetch from before
// it: the block stays where it was made, a session on it stays open with
// what's typed, and the typing saves once the create has.
func TestCanvasQuipNewBlockSurvivesRefetch(t *testing.T) {
	h, fs, v, s, frame := quipEnv(t)
	settle(t, s, v, "the canvas's model", func() bool { return h.a.NewCanvasSectionID(quipCanvasID) != "" })
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Delay: 3 * time.Second, Times: 1}) // the create lands late
	v.StartNewBlock()
	frame()
	if v.edit.cur == nil {
		t.Fatal("no new block under the last one")
	}
	v.edit.cur.ed.InsertText("A new paragraph")
	frame()
	if !v.edit.finish(v) {
		t.Fatal("the new paragraph was refused")
	}
	nb := findBlk(v, func(b *canvas.Block) bool { return b.Text() == "A new paragraph" })
	if nb == nil || nb.b.ID == "" {
		t.Fatal("no new block drawn")
	}
	id := nb.b.ID
	v.edit.start(v, nb)
	frame()
	if v.edit.cur == nil {
		t.Fatal("the new block didn't open")
	}
	ed := v.edit.cur.ed
	ed.SetSelection(ed.Doc().Len(), ed.Doc().Len())
	ed.InsertText(", typed again")
	before := v.Doc()
	v.load(true)
	settle(t, s, v, "the refetch", func() bool { return v.Doc() != before })
	frame()
	if v.edit.cur == nil || v.edit.cur.id != id {
		t.Fatal("the refetch closed the session on the new block")
	}
	if findBlk(v, func(b *canvas.Block) bool { return b.ID == id }) == nil {
		t.Error("the refetch took the new block away")
	}
	settle(t, s, v, "the create", func() bool { return quipBlock(fs, id) != nil })
	if !v.edit.finish(v) {
		t.Fatal("the typing in the new block was refused")
	}
	settle(t, s, v, "the typing", func() bool { b := quipBlock(fs, id); return b != nil && b.Text() == "A new paragraph, typed again" })
}

// A paragraph with a bold word made a heading keeps the bold, and saves its
// style alone, as Slack's setSectionStyle does.
func TestCanvasQuipHeadingKeepsBold(t *testing.T) {
	const p = "temp:C:QSV00031"
	_, fs, v, s, frame := quipEnvBlocks(t, []canvas.Block{
		{ID: quipFirstID, Kind: canvas.Paragraph, Spans: txt("First paragraph")},
		{ID: p, Kind: canvas.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: "Q3 "}, {Kind: richtext.Text, Text: "goals", Style: richtext.Bold}}},
	})
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == p }))
	frame()
	if v.edit.cur == nil {
		t.Fatal("the paragraph didn't open")
	}
	v.edit.apply(v, "h2", image.Point{})
	frame()
	if !v.edit.finish(v) {
		t.Fatal("the heading was refused")
	}
	settle(t, s, v, "the heading", func() bool { b := quipBlock(fs, p); return b != nil && b.Kind == canvas.Heading2 })
	rs := quipSaves(fs)
	if len(rs) != 1 || len(rs[0].Save.Sections) != 1 {
		t.Fatalf("%d saves", len(rs))
	}
	if got := quip.FieldsString(&rs[0].Save.Sections[0]); got != "1,2,9,10,13,19,20,33" {
		t.Errorf("the restyle's fields %s, want the style alone", got)
	}
	bold := false
	for _, sp := range quipBlock(fs, p).Spans {
		bold = bold || sp.Text == "goals" && sp.Style&richtext.Bold != 0
	}
	if !bold {
		t.Errorf("the heading lost its bold: %+v", quipBlock(fs, p).Spans)
	}
}
