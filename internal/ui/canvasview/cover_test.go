package canvasview

// Quip's default content and the header's Add cover (docs/CANVASES.md),
// on synthetic canvases: the new canvas (an empty title, Quip's default
// line), the starter (a heading, the default line, a paragraph) and the
// one with headings.

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// defaultLinePos is a point on headEmpty's default line: 20px under the
// 34px title, which sits 100px down.
func defaultLinePos(r *pointerRig) (x, y float32) {
	gtx := gtxFor(new(op.Ops), r.sz)
	x0, _ := r.v.column(gtx, r.sz.X)
	return float32(x0 + 40), float32(docTop + titleLine + 20 + bodyLine/2)
}

// A click on the new canvas's default line opens it empty, caret at 0,
// under Slack's placeholder: "Time to document your genius!" is never
// text you can select (Section.hideDefaultContent). Leaving it empty
// sends nothing and keeps the default line; typing replaces it with an
// ordinary section.
func TestDefaultLineOpensEmpty(t *testing.T) {
	_, fs, r := headRig(t, false, headEmpty)
	v := r.v
	def := v.blocks[1]
	if !def.b.Default || v.placeholderRow() != def {
		t.Fatalf("the default line isn't the placeholder row (default %v)", def.b.Default)
	}
	r.click(defaultLinePos(r))
	s := v.edit.cur
	if s == nil || s.target != def || !s.def {
		t.Fatalf("the click didn't open the default line (session %+v)", s)
	}
	if n := s.ed.Doc().Len(); n != 0 {
		t.Fatalf("the editor holds %d runes (%q), want none", n, string(s.ed.Doc().Runes))
	}
	s.ed.SetSelection(0, 40)
	if a, b := s.ed.Selection(); a != 0 || b != 0 {
		t.Errorf("selection %d-%d in the emptied line, want nothing to select", a, b)
	}
	if v.placeholderRow() != def {
		t.Error("the placeholder left the focused, empty line")
	}
	if !v.edit.finish(v) || len(fs.CanvasEdits()) != 0 {
		t.Fatalf("leaving the empty default line sent %d edits", len(fs.CanvasEdits()))
	}
	if !v.blocks[1].b.Default {
		t.Error("the default line lost its default content")
	}

	r.click(defaultLinePos(r))
	s = v.edit.cur
	if s == nil || !s.def {
		t.Fatal("the second click didn't open the default line")
	}
	s.ed.InsertText("Plan for Monday")
	r.frame()
	if !v.edit.finish(v) {
		t.Fatal("save refused")
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(fs.CanvasEdits()) == 0 && time.Now().Before(deadline) {
		r.frame()
		time.Sleep(10 * time.Millisecond)
	}
	ch := lastChange(t, fs)
	if ch["operation"] != "replace" || ch["section_id"] != "temp:C:HEAD00011" {
		t.Errorf("change %v, want a replace of the default line", ch)
	}
	if md := ch["document_content"].(map[string]any)["markdown"]; md != "Plan for Monday" {
		t.Errorf("markdown %q", md)
	}
	if b := findBlk(v, func(b *canvas.Block) bool { return b.ID == "temp:C:HEAD00011" }); b == nil || b.b.Default || b.b.Text() != "Plan for Monday" {
		t.Error("the default line wasn't replaced by an ordinary section")
	}
}

// In a canvas with other text the default line is drawn empty; opened,
// its saved default content is the editor's hint (the secondary overlay,
// slashShortcutCommandsNuxEnabled), not text.
func TestDefaultLineHintInCanvasWithText(t *testing.T) {
	h, fs := newEnv(t, false)
	addLookCanvases(fs)
	v := New(h, lookStarter, Tab, Actions{})
	s := newShooter(t, image.Pt(900, 700))
	settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil && v.shown != nil })
	frames(s, v, 3)
	if v.placeholderRow() != nil {
		t.Fatal("a canvas with text has the empty-canvas placeholder")
	}
	v.edit.start(v, v.blocks[1])
	frames(s, v, 3)
	cur := v.edit.cur
	if cur == nil || !cur.def || cur.defText != "Time to document your genius!" || cur.ed.Doc().Len() != 0 {
		t.Fatalf("session %+v", cur)
	}
}

// Hovering the band above the title, or the title itself, fades in Add
// cover; leaving fades it out; a key pressed over the title hides it
// until the pointer moves. The click says Clack can't save a cover and
// sends nothing. Viewers who can't edit get no button.
func TestAddCoverHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h, fs, r := headRig(t, dark, headCanvas)
		v := r.v
		c := &v.cover
		settleCover := func() {
			for i := 0; i < 30; i++ {
				r.frame()
				time.Sleep(15 * time.Millisecond)
			}
		}
		r.queue(mouse(pointer.Move, 450, 50))
		settleCover()
		if !c.inBand || !c.shown {
			t.Fatalf("hovering the header band: inBand %v shown %v", c.inBand, c.shown)
		}
		if o, moving := c.opacity(time.Now()); o != 1 || moving {
			t.Errorf("opacity %v (moving %v) after the fade", o, moving)
		}
		if c.rect.Empty() || c.rect.Max.Y != coverStrip || c.rect.Min.X < 90 {
			t.Errorf("button at %v, want bottom-aligned at 80px on the text column", c.rect)
		}
		sh := newShooter(t, r.sz)
		sh.shot("cover-hover-"+mode(dark), frames(sh, v, 2))

		r.queue(mouse(pointer.Move, 450, 450))
		settleCover()
		if c.shown {
			t.Error("Add cover still shown with the pointer on the body")
		}
		ty := float32(docTop + 10)
		r.queue(mouse(pointer.Move, 450, ty))
		settleCover()
		if !c.inTitle || !c.shown {
			t.Errorf("hovering the title: inTitle %v shown %v", c.inTitle, c.shown)
		}
		c.typed()
		settleCover()
		if c.shown {
			t.Error("Add cover shown after a key over the title")
		}
		r.queue(mouse(pointer.Move, 452, ty))
		settleCover()
		if !c.shown {
			t.Error("Add cover didn't come back when the pointer moved")
		}

		p := c.rect.Min.Add(c.rect.Size().Div(2))
		r.click(float32(p.X), float32(p.Y))
		h.mu.Lock()
		got := strings.Join(h.toasts, "|")
		h.mu.Unlock()
		if !strings.Contains(got, cantCover) || len(fs.CanvasEdits()) != 0 {
			t.Errorf("click: toasts %q, %d edits", got, len(fs.CanvasEdits()))
		}
	}

	// a viewer who can't edit
	h, fs := newEnv(t, false)
	fs.AddCanvas(fakeslack.FakeCanvas{ID: "F0CANVREAD1", Title: "Read only", ViewerAccess: "read", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:READ00001", Kind: canvas.Heading1, Spans: txt("Read only")}}}})
	v := New(h, "F0CANVREAD1", Tab, Actions{})
	r := &pointerRig{t: t, v: v, sz: image.Pt(900, 700)}
	deadline := time.Now().Add(20 * time.Second)
	for v.Doc() == nil || v.shown == nil {
		if time.Now().After(deadline) {
			t.Fatal("canvas didn't load")
		}
		r.frame()
		time.Sleep(10 * time.Millisecond)
	}
	r.queue(mouse(pointer.Move, 450, 50))
	r.frame()
	if !v.cover.rect.Empty() || v.cover.shown {
		t.Error("Add cover for a viewer who can't edit")
	}
}
