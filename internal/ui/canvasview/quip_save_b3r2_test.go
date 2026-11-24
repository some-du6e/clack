package canvasview

// The B3 parts 2-3 review's blockers (wf_63029827-35f) in the editor: Enter
// in a callout, and Shift+Enter.

import (
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/canvas"
)

// Enter in a callout's paragraph is refused while the text is still in
// the editor: Slack's new section there would be the callout's own, which
// Clack can't make yet (core would save it outside the callout). Escape
// again discards it; nothing is sent.
func TestCanvasQuipEnterInCalloutRefused(t *testing.T) {
	const callout, inside = "temp:C:QSV00021", "temp:C:QSV00022"
	h, fs, v, _, frame := quipEnvBlocks(t, []canvas.Block{
		{ID: quipFirstID, Kind: canvas.Paragraph, Spans: txt("First paragraph")},
		{ID: callout, Kind: canvas.Callout, Emoji: "bulb", Children: []canvas.Block{{ID: inside, Kind: canvas.Paragraph, Spans: txt("Inside the callout")}}},
	})
	var kid *blk
	if c := findBlk(v, func(b *canvas.Block) bool { return b.ID == callout }); c != nil {
		for _, k := range c.kids {
			if k.b != nil && k.b.ID == inside {
				kid = k
			}
		}
	}
	if kid == nil {
		t.Fatal("no callout paragraph")
	}
	v.edit.start(v, kid)
	frame()
	if v.edit.cur == nil {
		t.Fatal("the callout's paragraph didn't open")
	}
	ed := v.edit.cur.ed
	ed.SetSelection(ed.Doc().Len(), ed.Doc().Len())
	ed.InsertText("\nA new line")
	frame()
	if v.edit.finish(v) || v.edit.cur == nil {
		t.Fatal("Enter in a callout closed the editor")
	}
	if last := lastToast(h); !strings.HasPrefix(last, "Clack can't add a block inside a callout") {
		t.Errorf("the toast %q", last)
	}
	if !v.edit.escape(v) || v.edit.cur != nil {
		t.Error("Escape after the refusal didn't discard the change")
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(quipSaves(fs)); n != 0 {
		t.Errorf("%d edit-document calls for a refused Enter", n)
	}
}

// Shift+Enter, Slack's line break inside a block, isn't taken: the editor
// would make it a block of its own, saved as a new section of the canvas.
func TestCanvasQuipShiftEnterRefused(t *testing.T) {
	h, fs, v, _, frame := quipEnv(t)
	v.edit.start(v, findBlk(v, func(b *canvas.Block) bool { return b.ID == quipFirstID }))
	frame()
	if v.edit.cur == nil || v.edit.cur.ed.Intercept == nil {
		t.Fatal("the paragraph didn't open")
	}
	ed := v.edit.cur.ed
	if !ed.Intercept(key.NameReturn, key.ModShift) {
		t.Error("Shift+Enter went on to the editor (a new line, saved as a new section)")
	}
	if n := len(ed.Doc().Lines); n != 1 {
		t.Errorf("%d lines after Shift+Enter", n)
	}
	if last := lastToast(h); !strings.HasPrefix(last, "Clack can't put a line break inside a canvas block") {
		t.Errorf("the toast %q", last)
	}
	if !v.edit.finish(v) {
		t.Fatal("the block, left as it was, was refused")
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(quipSaves(fs)); n != 0 {
		t.Errorf("%d edit-document calls", n)
	}
}
