package core

// The B3 parts 2-3 review's findings (wf_63029827-35f): a new block in a
// layout, a new block's level or a second restyle before the first one
// saved, an Enter that moves commented text, and the queue's deletes,
// splits and creates around other edits.

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
)

func r2txt(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }

// r2canvas is a canvas of blocks bs with its model loaded.
func r2canvas(t *testing.T, id string, bs []canvas.Block) (*App, *fakeslack.Server) {
	t.Helper()
	a, fs := canvasApp(t)
	fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "Review 2", Doc: canvas.Doc{Blocks: bs}})
	loadModel(t, a, id)
	return a, fs
}

// fakeBlock is the fake's block whose text is text, or nil.
func fakeBlock(fs *fakeslack.Server, id, text string) *canvas.Block {
	fc, _ := fs.Canvas(id)
	for i := range fc.Doc.Blocks {
		if fc.Doc.Blocks[i].Text() == text {
			return &fc.Doc.Blocks[i]
		}
	}
	return nil
}

// Enter in a callout's or column's paragraph (a layout's child) isn't
// saved: Slack's new section there is the layout's child too, which Clack
// can't make yet, and outside the layout it would show elsewhere.
func TestCanvasSaveNewBlockInLayout(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	a.CanvasModel(fakeslack.CanvasShared, func(d *quip.Doc) { d.Section(*p.ID).HasLayoutParent = quip.Ptr(true) })
	if !a.CanvasSectionInLayout(fakeslack.CanvasShared, *p.ID) {
		t.Error("a layout's child isn't told")
	}
	err := a.SaveCanvasSplit(ctx, fakeslack.CanvasShared, *p.ID, r2txt("Weekly sync"), r2txt("Weekly sync"),
		[]CanvasNewBlock{{ID: a.NewCanvasSectionID(fakeslack.CanvasShared), Kind: canvas.Paragraph, Spans: r2txt("in the callout")}}, 0)
	if !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("Enter in a layout's child: %v", err)
	}
	err = a.SaveCanvasNewBlocks(ctx, fakeslack.CanvasShared, *p.ID,
		[]CanvasNewBlock{{ID: a.NewCanvasSectionID(fakeslack.CanvasShared), Kind: canvas.Paragraph, Spans: r2txt("after it")}})
	if !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("a new block after a layout's child: %v", err)
	}
	if n := len(saves(fs)); n != 0 {
		t.Errorf("%d edit-document calls", n)
	}
}

// A new list item's level changed before its first save goes with its
// create (initSibling's indentation, then setSectionAttribute's).
func TestCanvasSaveNewItemLevelBeforeCreate(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = time.Hour // the retry goes when the test runs it (retryNow)
	const id, one = "F0QUIPR2A01", "temp:C:RTA00001"
	a, fs := r2canvas(t, id, []canvas.Block{
		{ID: "temp:C:RTA00002", Kind: canvas.Paragraph, Spans: r2txt("before")},
		{ID: one, Kind: canvas.Bullet, Spans: r2txt("one")},
	})
	ctx := context.Background()
	nid := a.NewCanvasSectionID(id)
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	if err := a.SaveCanvasSplit(ctx, id, one, r2txt("one"), r2txt("one"), []CanvasNewBlock{{ID: nid, Kind: canvas.Bullet, Spans: r2txt("two")}}, 0); err == nil {
		t.Fatal("the first attempt went through")
	}
	be := CanvasBlockEdit{SectionID: nid, Orig: r2txt("two"), OrigKind: canvas.Bullet, Spans: r2txt("two"), Kind: canvas.Bullet, Level: 1}
	if err := a.SaveCanvasBlock(ctx, id, be); err != nil {
		t.Fatalf("the new item's level before its first save: %v", err)
	}
	if st := a.CanvasSaveState(id); st.Pending {
		retryNow(t, a, id)
	}
	if b := fakeBlock(fs, id, "two"); b == nil || b.Kind != canvas.Bullet || b.Level != 1 {
		t.Errorf("the new item: %+v", b)
	}
}

// A block restyled again while its first restyle waits to be saved builds
// on it: the section's style as the pending edit leaves it, not a conflict.
func TestCanvasSaveRestyleTwice(t *testing.T) {
	fastSaves(t)
	saveBackoffBase = time.Hour
	const id, p = "F0QUIPR2B01", "temp:C:RTB00002"
	a, fs := r2canvas(t, id, []canvas.Block{
		{ID: "temp:C:RTB00001", Kind: canvas.Paragraph, Spans: r2txt("First")},
		{ID: p, Kind: canvas.Paragraph, Spans: r2txt("Second")},
	})
	ctx := context.Background()
	fs.SetFault("canvas/edit-document", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	be := CanvasBlockEdit{SectionID: p, Orig: r2txt("Second"), OrigKind: canvas.Paragraph, Spans: r2txt("Second"), Kind: canvas.Heading2}
	if err := a.SaveCanvasBlock(ctx, id, be); err == nil {
		t.Fatal("the first restyle's attempt went through")
	}
	be.OrigKind, be.Kind = canvas.Heading2, canvas.Heading3
	if err := a.SaveCanvasBlock(ctx, id, be); err != nil {
		t.Fatalf("a second restyle while the first waits: %v", err)
	}
	if st := a.CanvasSaveState(id); st.Pending {
		retryNow(t, a, id)
	}
	if b := fakeBlock(fs, id, "Second"); b == nil || b.Kind != canvas.Heading3 {
		t.Errorf("the block after both restyles: %+v", b)
	}
}

// An Enter that takes commented text out of its section isn't saved
// (Slack's split carries the comment's anchor; Clack can't, and it would
// go); one that leaves the commented text where it is saves, the anchor
// kept.
func TestCanvasSaveSplitMovesAnchor(t *testing.T) {
	const id, sec = "F0QUIPR2C01", "temp:C:RTC00001"
	a, fs := oneParagraph(t, id, sec, r2txt("Ship the release notes"))
	ctx := context.Background()
	a.CanvasModel(id, func(d *quip.Doc) {
		d.Section(sec).Content = &quip.Content{Text: &quip.ContentText{Text: quip.Ptr(`Ship the <annotation id="temp:C:RTCa0001">release notes</annotation>`)}}
	})
	orig := r2txt("Ship the release notes")
	moved := []CanvasNewBlock{{ID: a.NewCanvasSectionID(id), Kind: canvas.Paragraph, Spans: r2txt("release notes")}}
	if !a.CanvasSplitMovesAnchors(id, sec, orig, r2txt("Ship the "), moved) {
		t.Error("an Enter before the commented words isn't told")
	}
	if err := a.SaveCanvasSplit(ctx, id, sec, orig, r2txt("Ship the "), moved, 0); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("an Enter that moves commented text: %v", err)
	}
	if n := len(saves(fs)); n != 0 {
		t.Fatalf("%d edit-document calls for a refused Enter", n)
	}
	after := []CanvasNewBlock{{ID: a.NewCanvasSectionID(id), Kind: canvas.Paragraph, Spans: r2txt("today")}}
	if a.CanvasSplitMovesAnchors(id, sec, orig, orig, after) {
		t.Error("an Enter after the commented words is told as moving them")
	}
	if err := a.SaveCanvasSplit(ctx, id, sec, orig, orig, after, 0); err != nil {
		t.Fatalf("an Enter after the commented words: %v", err)
	}
	rs := saves(fs)
	if len(rs) != 1 || quip.AnchoredUnits(quip.SectionRTML(&rs[0].Save.Sections[0]))["temp:C:RTCa0001"] != len("release notes") {
		t.Errorf("the save kept the anchor? %+v", rs)
	}
}
