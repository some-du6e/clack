package canvasview

// Slack's canvas look (docs/CANVASES.md, "How it looks"): synthetic
// canvases only, added to the fake server by hand.

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/yoloslackgoclient/internal/canvas"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

const (
	lookStarter = "F0CANVLOOK1" // a heading, Quip's default line, one paragraph
	lookEmpty   = "F0CANVLOOK2" // a new canvas: its title and the default line
)

func addLookCanvases(fs *fakeslack.Server) {
	txt := func(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }
	def := canvas.Block{ID: "temp:C:LOOK00002", Kind: canvas.Paragraph, Spans: txt("Time to document your genius!"), Default: true}
	fs.AddCanvas(fakeslack.FakeCanvas{ID: lookStarter, Title: "Release notes", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:LOOK00001", Kind: canvas.Heading1, Spans: append(txt("Release notes "), richtext.Span{Kind: richtext.Emoji, ID: "rocket", Text: "🚀"})},
		def,
		{ID: "temp:C:LOOK00003", Kind: canvas.Paragraph, Spans: txt("First draft of the notes.")},
	}}})
	def.ID = "temp:C:LOOK00005"
	fs.AddCanvas(fakeslack.FakeCanvas{ID: lookEmpty, Title: "Untitled", Doc: canvas.Doc{Blocks: []canvas.Block{
		{ID: "temp:C:LOOK00004", Kind: canvas.Heading1, Spans: txt("Untitled")},
		def,
	}}})
}

func frames(s *shooter, v *View, n int) *op.Ops {
	var ops *op.Ops
	for i := 0; i < n; i++ {
		ops = s.frame(func(gtx layout.Context) { v.Layout(gtx) })
		time.Sleep(15 * time.Millisecond)
	}
	return ops
}

// The page: the column, the title section, no byline, the default line
// kept but not drawn, the placeholder only on an empty canvas, and the
// bottom toolbar; then the floating bar over a selection.
func TestRenderLook(t *testing.T) {
	for _, dark := range []bool{false, true} {
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		h, fs := newEnv(t, dark)
		addLookCanvases(fs)
		sz := image.Pt(1038, 717) // Slack's channel tab pane in the reference shots

		v := New(h, lookStarter, Tab, Actions{})
		s := newShooter(t, sz)
		settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil })
		s.shot("look-starter-tab-"+suffix, frames(s, v, 30))
		if !titleSection(v.blocks) || len(v.blocks) != 3 || !v.blocks[1].b.Default {
			t.Fatalf("blocks: %d, title %v", len(v.blocks), titleSection(v.blocks))
		}
		if ph := v.placeholder(v.blocks[1:]); ph != "" {
			t.Errorf("placeholder %q on a canvas with text", ph)
		}

		// geometry: an 800px column 96px into a centred 990px wrapper,
		// the title 100px down; 20px under the 34px title the first
		// block (the default line, 22px tall), 4px, the paragraph
		gtx := gtxFor(new(op.Ops), sz)
		if x, cw := v.column(gtx, sz.X); cw != 800 || x != (sz.X-990)/2+96 {
			t.Errorf("column x %d width %d", x, cw)
		}
		if g := gap(gtx, nil, v.blocks[1], true); g != 20 {
			t.Errorf("gap under the title %d", g)
		}
		if g := gap(gtx, v.blocks[1], v.blocks[2], false); g != 4 {
			t.Errorf("paragraph gap %d", g)
		}

		// a new canvas: the placeholder Slack picks from the file id
		e := New(h, lookEmpty, Tab, Actions{})
		settle(t, s, e, "the empty canvas", func() bool { return e.Doc() != nil })
		s.shot("look-empty-tab-"+suffix, frames(s, e, 20))
		if ph := e.placeholder(e.blocks[1:]); ph != placeholderFor(lookEmpty) || ph == "" {
			t.Errorf("placeholder %q", ph)
		}

		// editing: no bar until text is selected, then the floating bar
		v.edit.start(v, v.blocks[2])
		frames(s, v, 5)
		ed := v.edit.cur.ed
		ed.SetSelection(0, len("First draft"))
		s.shot("look-selection-tab-"+suffix, frames(s, v, 10))
		if v.edit.cur == nil {
			t.Fatal("the edit ended")
		}
		v.edit.discard(v)

		// the side panel and full view: the same page under their bars
		for _, m := range []struct {
			mode Mode
			name string
			sz   image.Point
		}{{Side, "side", image.Pt(480, 717)}, {Full, "full", image.Pt(1200, 800)}} {
			mv := New(h, lookStarter, m.mode, Actions{Close: func() {}, FullView: func() {}, SideView: func() {}})
			ms := newShooter(t, m.sz)
			settle(t, ms, mv, "the canvas", func() bool { return mv.Doc() != nil })
			ms.shot("look-starter-"+m.name+"-"+suffix, frames(ms, mv, 30))
		}
	}
}

// Slack's getIndexFromFileId picks the placeholder from the file id's last
// two characters.
func TestPlaceholderFor(t *testing.T) {
	seen := map[string]bool{}
	for _, id := range []string{"F0CANVAS001", "F0CANVAS002", "F0CANVLOOK2", "F09ABCDEFGH", "F0AZ", "F0"} {
		p := placeholderFor(id)
		if p == "" {
			t.Errorf("%s: no placeholder", id)
		}
		seen[p] = true
	}
	// "AB": a = 0*17 + 0 = 0, then 0*17 + 1 = 1 -> index 1
	if p := placeholderFor("FXAB"); !strings.HasPrefix(p, "What") {
		t.Errorf("FXAB: %q", p)
	}
}
