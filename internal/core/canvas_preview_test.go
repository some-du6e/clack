package core

// The link-url-box-3 fix check's blocker: a canvas preview that failed
// without an answer, or that waited for a slot past the resolve batch's
// 30 s, was stored as "no preview" for the session (channel message cards
// too). Previews now run on their own, each with its own deadline.

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
)

// A page of thirty canvases whose exports each take 5 s: four at a time,
// the last ones wait well past 30 s for a slot. Every one lands with its
// preview; none is left without one.
func TestCanvasPreviewsSlowPage(t *testing.T) {
	a, fs := canvasApp(t)
	var ids []string
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("F0PREVIEW%03d", i)
		ids = append(ids, id)
		fs.AddCanvas(fakeslack.FakeCanvas{ID: id, Title: "Preview " + id, Doc: canvas.Doc{Blocks: []canvas.Block{
			{Kind: canvas.Heading1, Spans: []richtext.Span{{Kind: richtext.Text, Text: "Preview " + id}}},
			{Kind: canvas.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: "The body of " + id}}},
		}}})
		fs.SetFault("files-pri/"+id, fakeslack.Fault{Delay: 5 * time.Second})
	}
	for _, id := range ids {
		a.S.CanvasPreview(id) // a page of message cards reads them: each lookup noted
	}
	deadline := time.Now().Add(100 * time.Second)
	for {
		left := 0
		for _, id := range ids {
			b, ok := a.S.CanvasPreview(id)
			switch {
			case ok && len(b) == 0:
				t.Fatalf("canvas %s was left without a preview (export GETs %d)", id, fs.CallCount("files-pri/"+id))
			case !ok:
				left++
			}
		}
		if left == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of 30 previews never landed", left)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
