package core

// The B3a review's blockers (wf_da55700d-6e0) at the save: what the block
// didn't show of its section was written away.

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

// a canvas of one paragraph, spans sp, with its model loaded; its section
func oneParagraph(t *testing.T, id, sec string, sp []richtext.Span) (*App, *fakeslack.Server) {
	t.Helper()
	a, fs := canvasApp(t)
	fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "Review", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: sec, Kind: canvas.Paragraph, Spans: sp},
		{ID: sec + "x", Kind: canvas.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: "after"}}},
	}}})
	loadModel(t, a, id)
	return a, fs
}

func modelRTML(t *testing.T, a *App, id, sec string) string {
	t.Helper()
	out := ""
	a.CanvasModel(id, func(d *quip.Doc) {
		if d != nil {
			out = quip.SectionRTML(d.Section(sec))
		}
	})
	return out
}

// Someone else's formatting, saved while the block was open, isn't
// written away: the block began from the section's older sequence, so
// the edit is a conflict, and nothing is sent.
func TestCanvasSaveSpansOthersFormatting(t *testing.T) {
	const id, sec = "F0QUIPREV01", "temp:C:REV00001"
	plain := []richtext.Span{{Kind: richtext.Text, Text: "Ship the release notes"}}
	a, fs := oneParagraph(t, id, sec, plain)
	ctx := context.Background()
	seq0 := a.CanvasSectionSeq(id, sec) // the block opens
	if !fs.ReplaceCanvasBlockAsOther(id, sec, "Ship the **release** notes") {
		t.Fatal("no paragraph to replace")
	}
	if err := a.fetchCanvasModel(ctx, id, a.quipCanvas(id)); err != nil {
		t.Fatal(err)
	}
	err := a.SaveCanvasBlock(ctx, id, CanvasBlockEdit{SectionID: sec, Orig: plain, OrigKind: canvas.Paragraph, OrigSeq: seq0,
		Spans: []richtext.Span{{Kind: richtext.Text, Text: "Ship the release notes today"}}, Kind: canvas.Paragraph})
	if !errors.Is(err, ErrCanvasConflict) {
		t.Errorf("an edit over someone else's bold: %v", err)
	}
	if n := len(saves(fs)); n != 0 {
		t.Errorf("%d edit-document calls", n)
	}
	if got := modelRTML(t, a, id, sec); got != "Ship the <b>release</b> notes" {
		t.Errorf("the section %q", got)
	}
}

// A link Clack's export doesn't show (tel:) isn't written away by an edit
// of the text around it: such a block isn't opened, and its save is
// refused.
func TestCanvasSaveSpansHiddenLink(t *testing.T) {
	const id, sec = "F0QUIPREV02", "temp:C:REV00002"
	a, fs := oneParagraph(t, id, sec, []richtext.Span{
		{Kind: richtext.Text, Text: "Call "},
		{Kind: richtext.Link, Text: "the on-call line", URL: "tel:+15551234567"},
		{Kind: richtext.Text, Text: " now"},
	})
	ctx := context.Background()
	base := modelRTML(t, a, id, sec)
	if base != `Call <a href="tel:+15551234567">the on-call line</a> now` {
		t.Fatalf("the section %q", base)
	}
	shown := []richtext.Span{{Kind: richtext.Text, Text: "Call the on-call line now"}} // canvas.Parse drops tel:
	if ok, why := a.CanvasBlockEditable(id, sec, shown); ok {
		t.Errorf("the block opens (%q)", why)
	}
	err := a.SaveCanvasSpans(ctx, id, sec, shown, []richtext.Span{{Kind: richtext.Text, Text: "Call the on-call line today"}})
	if !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("an edit over a hidden link: %v", err)
	}
	if n := len(saves(fs)); n != 0 {
		t.Errorf("%d edit-document calls", n)
	}
	if got := modelRTML(t, a, id, sec); got != base {
		t.Errorf("the section %q", got)
	}
}

// Formatting around a mention stays: the editor holds the mention as an
// atom it doesn't format, so a kept mention keeps the base's.
func TestCanvasSaveSpansMentionFormatting(t *testing.T) {
	const id, sec = "F0QUIPREV03", "temp:C:REV00003"
	orig := []richtext.Span{
		{Kind: richtext.Text, Text: "Owner: ", Style: richtext.Bold},
		{Kind: richtext.User, ID: "U0FAKE00001", Style: richtext.Bold},
	}
	a, fs := oneParagraph(t, id, sec, orig)
	ctx := context.Background()
	edited := []richtext.Span{
		{Kind: richtext.Text, Text: "Owners: ", Style: richtext.Bold},
		{Kind: richtext.User, ID: "U0FAKE00001"}, // the editor's atom: no style
	}
	if err := a.SaveCanvasSpans(ctx, id, sec, orig, edited); err != nil {
		t.Fatalf("an edit beside a bold mention: %v", err)
	}
	rs := saves(fs)
	if len(rs) != 1 {
		t.Fatalf("%d saves", len(rs))
	}
	got := quip.SectionRTML(&rs[0].Save.Sections[0])
	text, marks, _ := quip.ParseRTML(got)
	at := strings.Index(text, "<control")
	bold := false
	for _, m := range marks {
		if m.Name == "b" && m.Start <= at && at < m.End {
			bold = true
		}
	}
	if !bold {
		t.Errorf("the mention lost its bold: %q", got)
	}
}

// A line break at a section's end (a blank last line in Slack, which
// Clack's export drops) stays through an edit.
func TestCanvasSaveSpansTrailingBreak(t *testing.T) {
	const id, sec = "F0QUIPREV04", "temp:C:REV00004"
	a, fs := oneParagraph(t, id, sec, []richtext.Span{{Kind: richtext.Text, Text: "Notes"}, {Kind: richtext.Break}})
	ctx := context.Background()
	if base := modelRTML(t, a, id, sec); base != "Notes<br>" {
		t.Fatalf("the section %q", base)
	}
	shown := []richtext.Span{{Kind: richtext.Text, Text: "Notes"}} // trimBreaks
	if err := a.SaveCanvasSpans(ctx, id, sec, shown, []richtext.Span{{Kind: richtext.Text, Text: "Notes:"}}); err != nil {
		t.Fatalf("an edit of a section ending with a line break: %v", err)
	}
	rs := saves(fs)
	if len(rs) != 1 {
		t.Fatalf("%d saves", len(rs))
	}
	if got := quip.SectionRTML(&rs[0].Save.Sections[0]); got != "Notes:<br>" {
		t.Errorf("saved %q, want the line break kept", got)
	}
}
