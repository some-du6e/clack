package core

// The B3a review's MINORs about the title: edited where it's drawn, and a
// title no save's metadata can carry yet.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
)

// The title edited where it's drawn (the document's opening heading,
// which is its title section) saves its text as a rename does; formatting
// in it isn't saved yet; emptied, its section stays with no text.
func TestCanvasSaveTitleBlock(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	loadModel(t, a, fakeslack.CanvasShared)
	tid := a.canvasTitleID(fakeslack.CanvasShared)
	if tid == "" {
		t.Fatal("no title section")
	}
	fc, _ := fs.Canvas(fakeslack.CanvasShared)
	old := fc.Title
	txt := func(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }
	edit := func(from, to []richtext.Span) CanvasBlockEdit {
		return CanvasBlockEdit{SectionID: tid, Orig: from, OrigKind: canvas.Heading1, Spans: to, Kind: canvas.Heading1}
	}
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, edit(txt(old), txt(old+", edited"))); err != nil {
		t.Fatalf("the title edited where it's drawn: %v", err)
	}
	if fc, _ := fs.Canvas(fakeslack.CanvasShared); fc.Title != old+", edited" {
		t.Errorf("the canvas's title %q", fc.Title)
	}
	bold := []richtext.Span{{Kind: richtext.Text, Text: "A bold title", Style: richtext.Bold}}
	if err := a.SaveCanvasBlock(ctx, fakeslack.CanvasShared, edit(txt(old+", edited"), bold)); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("a bold title: %v", err)
	}
	n := len(saves(fs))
	if err := a.SaveCanvasDelete(ctx, fakeslack.CanvasShared, tid, txt(old+", edited")); err != nil {
		t.Fatalf("the title emptied: %v", err)
	}
	rs := saves(fs)
	if len(rs) != n+1 || len(rs[n].Save.Sections) != 1 || rs[n].Save.Sections[0].Deleted != nil {
		t.Fatalf("the emptied title's save: %+v", rs[n:])
	}
	if fc, _ := fs.Canvas(fakeslack.CanvasShared); fc.Title != "" {
		t.Errorf("the emptied title %q", fc.Title)
	}
}

// A title every save's metadata can't carry yet (a control in it: an
// emoji, a mention) is told, so the view keeps the canvas's blocks out of
// the editor instead of refusing what's typed in them.
func TestCanvasSaveTitleSaveable(t *testing.T) {
	a, _ := canvasApp(t)
	loadModel(t, a, fakeslack.CanvasShared)
	if ok, why := a.CanvasTitleSaveable(fakeslack.CanvasShared); !ok {
		t.Fatalf("a plain title: %q", why)
	}
	a.CanvasModel(fakeslack.CanvasShared, func(d *quip.Doc) {
		for _, s := range d.Sections() {
			if s.Type != nil && *s.Type == quip.TypeTitle {
				s.Content = &quip.Content{Title: &quip.ContentTitle{Text: quip.Ptr(`Launch <control id="temp:C:TTL00001"></control>`)}}
			}
		}
	})
	if ok, why := a.CanvasTitleSaveable(fakeslack.CanvasShared); ok || !strings.Contains(why, "emoji") {
		t.Errorf("a title with a control: %v (%q)", ok, why)
	}
}
