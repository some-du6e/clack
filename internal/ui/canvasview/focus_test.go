package canvasview

import (
	"image"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// A canvas-section link (a canvas slug or a canvas-section card, both
// carrying focus_section_id) opens the canvas with that section at the
// top (openCanvas → focusSection); a link to a section of the canvas
// already open focuses it in place. Synthetic canvas in the shape of a
// real one (fakeslack canvas_section.go).
func TestFocusSectionFromLink(t *testing.T) {
	h, fs := newEnv(t, false)
	fs.AddSectionCanvas()
	link := fakeslack.CanvasSectionURL()
	if CanvasLinkID(link) != fakeslack.CanvasSection || CanvasLinkSection(link) != fakeslack.CanvasSectionID {
		t.Fatalf("link %q: id %q section %q", link, CanvasLinkID(link), CanvasLinkSection(link))
	}
	opened := ""
	v := New(h, fakeslack.CanvasSection, Side, Actions{Canvas: func(id string) { opened = id }})
	s := newShooter(t, image.Pt(520, 700))
	v.FocusSection(CanvasLinkSection(link))
	settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil && v.shown != nil })
	frames(s, v, 3)
	rows := v.blocks[1:] // the title section is the header row
	want := rowOf(rows, fakeslack.CanvasSectionID) + 1
	if want < 2 {
		t.Fatalf("the section isn't in the canvas (%d rows)", len(rows))
	}
	// at the top, or as far as the list scrolls (the section is near
	// the canvas's end): in view either way
	if pos := v.list.Position; pos.First > want || pos.First+pos.Count <= want || pos.First < want-30 {
		t.Errorf("rows %d..%d in view, want the section's row %d at the top or in view at the end", pos.First, pos.First+pos.Count, want)
	}
	if v.focus != "" {
		t.Error("the focus wasn't used up")
	}
	s.shot("canvas-section-focused", frames(s, v, 10))

	// back to the top, then the same link from inside the canvas: focusSection in place
	v.list.Position.First = 0
	v.handler.OpenURL(link)
	frames(s, v, 3)
	if opened != "" {
		t.Errorf("a link to this canvas's own section opened %q again", opened)
	}
	if pos := v.list.Position; pos.First > want || pos.First+pos.Count <= want || pos.First == 0 {
		t.Errorf("in-place focus: rows %d..%d in view, want row %d", pos.First, pos.First+pos.Count, want)
	}
}
