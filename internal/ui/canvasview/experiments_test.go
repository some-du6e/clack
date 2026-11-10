package canvasview

// The canvas experiments (experiments.go): each test runs the account's
// group (the fake answers experiments.getByUser as the account does) and
// flips it the way the Experiments plugins do, through core's
// SetExperimentGroup.

import (
	"encoding/json"
	"image"
	"net/url"
	"strings"
	"testing"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/composer"
)

const tocCanvas = "F0CANVTOC01" // a title, headings at two levels, a heading in a quote

func txtSpans(s string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: s}} }

func addTOCCanvas(fs *fakeslack.Server) {
	para := func(id, s string) canvas.Block {
		return canvas.Block{ID: id, Kind: canvas.Paragraph, Spans: txtSpans(s)}
	}
	var bs []canvas.Block
	bs = append(bs, canvas.Block{ID: "temp:C:TOC00001", Kind: canvas.Heading1, Spans: txtSpans("Roadmap")})
	for _, sec := range []struct {
		id, title string
		kind      canvas.Kind
	}{{"temp:C:TOC00010", "Goals", canvas.Heading2}, {"temp:C:TOC00020", "This quarter", canvas.Heading3},
		{"temp:C:TOC00030", "Risks", canvas.Heading2}, {"temp:C:TOC00040", "Owners", canvas.Heading2}} {
		bs = append(bs, canvas.Block{ID: sec.id, Kind: sec.kind, Spans: txtSpans(sec.title)})
		for i := 0; i < 12; i++ {
			bs = append(bs, para(sec.id+"p"+string(rune('a'+i)), "A line of notes under "+sec.title+" that fills the page."))
		}
	}
	bs = append(bs, canvas.Block{ID: "temp:C:TOC00050", Kind: canvas.Heading2}) // empty: no entry
	fs.AddCanvas(fakeslack.FakeCanvas{ID: tocCanvas, Title: "Roadmap", Doc: canvas.Doc{Blocks: bs}})
}

func mkBlks(bs ...canvas.Block) []*blk {
	out := make([]*blk, len(bs))
	for i := range bs {
		b := bs[i]
		out[i] = &blk{b: &b}
	}
	return out
}

// computeTableOfContentsInfo's entries: the title and the headings with
// text, levels renumbered from the ones present.
func TestTableOfContentsEntries(t *testing.T) {
	bs := mkBlks(
		canvas.Block{ID: "t", Kind: canvas.Heading1, Spans: txtSpans("Plan")},
		canvas.Block{ID: "p", Kind: canvas.Paragraph, Spans: txtSpans("intro")},
		canvas.Block{ID: "g", Kind: canvas.Heading2, Spans: txtSpans("  Goals")},
		canvas.Block{ID: "s", Kind: canvas.Heading3, Spans: txtSpans("Short term")},
		canvas.Block{ID: "e", Kind: canvas.Heading2, Spans: txtSpans("   ")},
		canvas.Block{ID: "r", Kind: canvas.Heading2, Spans: append(txtSpans("Risks "), richtext.Span{Kind: richtext.Emoji, ID: "fire", Text: "🔥"})},
	)
	q := &blk{b: &canvas.Block{ID: "q", Kind: canvas.Quote}, kids: mkBlks(canvas.Block{ID: "qh", Kind: canvas.Heading3, Spans: txtSpans("Quoted")})}
	bs = append(bs, q)
	got := buildTOC(bs)
	type e struct {
		row    int
		id     string
		indent int
		title  bool
		text   string
	}
	want := []e{{0, "t", 0, true, "Plan"}, {2, "g", 0, false, "Goals"}, {3, "s", 1, false, "Short term"},
		{5, "r", 0, false, "Risks 🔥"}, {6, "qh", 1, false, "Quoted"}}
	if len(got) != len(want) {
		t.Fatalf("entries %+v", got)
	}
	for i, w := range want {
		g := got[i]
		if g.row != w.row || g.id != w.id || g.indent != w.indent || g.title != w.title || g.text != w.text {
			t.Errorf("entry %d: %+v, want %+v", i, g, w)
		}
	}
	// a canvas of H2s and H3s, no title: the H2s are level 0
	got = buildTOC(mkBlks(
		canvas.Block{ID: "p", Kind: canvas.Paragraph, Spans: txtSpans("intro")},
		canvas.Block{ID: "a", Kind: canvas.Heading2, Spans: txtSpans("A")},
		canvas.Block{ID: "b", Kind: canvas.Heading3, Spans: txtSpans("B")},
	))
	if len(got) != 2 || got[0].indent != 0 || got[1].indent != 1 || got[0].row != 2 {
		t.Errorf("no title: %+v", got)
	}
}

// computeActiveSectionId: the entry in the top half of the view, else the
// last one above it.
func TestTableOfContentsActive(t *testing.T) {
	gtx := gtxFor(new(op.Ops), image.Pt(800, 600))
	entries := []tocEntry{{row: 0, id: "t", title: true}, {row: 2, id: "g"}, {row: 3, id: "s"}, {row: 5, id: "r"}}
	ts := newTOC()
	// the top of the page: the title (100px down) is current
	ts.rowTop = map[int]int{0: 0, 1: 300, 2: 350}
	ts.rowGap = map[int]int{2: 24}
	ts.first, ts.last = 0, 2
	if a := ts.active(gtx, entries, 600); a != "t" {
		t.Errorf("top of the page: %q", a)
	}
	// "g" scrolled past the top, "s" starts in the top half
	ts.rowTop = map[int]int{2: -50, 3: 200, 4: 500}
	ts.rowGap = map[int]int{2: 24, 3: 20}
	ts.first, ts.last = 2, 4
	if a := ts.active(gtx, entries, 600); a != "s" {
		t.Errorf("s in the top half: %q", a)
	}
	// "s" in the lower half: "g" stays current
	ts.rowTop = map[int]int{2: -50, 3: 400}
	if a := ts.active(gtx, entries, 600); a != "g" {
		t.Errorf("s in the lower half: %q", a)
	}
}

// The drawer and Quip's gutter follow canvas_table_of_contents.
func TestRenderTableOfContents(t *testing.T) {
	for _, dark := range []bool{false, true} {
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		h, fs := newEnv(t, dark)
		addTOCCanvas(fs)
		sz := image.Pt(1038, 717)
		v := New(h, tocCanvas, Tab, Actions{})
		s := newShooter(t, sz)
		settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil })
		frames(s, v, 10)
		if !v.tocOn() {
			t.Fatal("canvas_table_of_contents isn't on for the account")
		}
		es := v.tocEntries()
		if len(es) != 5 || !es[0].title || es[2].indent != 1 {
			t.Fatalf("entries %+v", es)
		}
		gtx := gtxFor(new(op.Ops), sz)
		if x, cw := v.column(gtx, sz.X); cw != 800 || x != (sz.X-990)/2+96 {
			t.Errorf("with the outline: column x %d width %d", x, cw)
		}
		if x, cw := columnFor(gtx, 450, true); x != 96 || cw != 450-96-51 {
			t.Errorf("narrow, with the outline: x %d width %d", x, cw)
		}
		s.shot("toc-collapsed-tab-"+suffix, frames(s, v, 10))

		// hovering opens the panel; a click jumps to the section
		v.toc.expanded, v.toc.latchTop = true, gtx.Dp(100)
		s.shot("toc-expanded-tab-"+suffix, frames(s, v, 5))
		v.tocJump(gtx, es[3])
		if v.list.Position.First != es[3].row || v.toc.jumped != es[3].id {
			t.Errorf("jump to %+v: list at %d, current %q", es[3], v.list.Position.First, v.toc.jumped)
		}
		frames(s, v, 5)
		if a := v.toc.active(gtx, es, sz.Y); a != es[3].id {
			t.Errorf("after the jump the current entry is %q", a)
		}
		s.shot("toc-jumped-tab-"+suffix, frames(s, v, 5))

		// off: no drawer, and no gutter (94px both sides)
		h.a.SetExperimentGroup("canvas_table_of_contents", "off")
		frames(s, v, 5)
		if v.tocOn() {
			t.Error("still on after the override")
		}
		if x, cw := v.column(gtx, sz.X); cw != 800 || x != (sz.X-988)/2+94 {
			t.Errorf("without the outline: column x %d width %d", x, cw)
		}
		if x, cw := columnFor(gtx, 450, false); x != 51 || cw != 450-102 {
			t.Errorf("narrow, without the outline: x %d width %d", x, cw)
		}
		s.shot("toc-off-tab-"+suffix, frames(s, v, 5))
		h.a.ClearExperimentGroup("canvas_table_of_contents")
		frames(s, v, 2)
		if !v.tocOn() {
			t.Error("not back on after clearing the override")
		}
	}
}

func formatKeys(fs []blockFormat) string {
	var ks []string
	for _, f := range fs {
		ks = append(ks, f.key)
	}
	return strings.Join(ks, ",")
}

func menuLabels(m []MenuItem) string {
	var ls []string
	for _, it := range m {
		if it.Divider {
			ls = append(ls, "─")
			continue
		}
		ls = append(ls, it.Label)
	}
	return strings.Join(ls, ",")
}

// canvas_blockquote_layout moves the blockquote out of the block-style
// menus into a button of its own and the insert menu.
func TestEditBlockquoteLayoutToolbars(t *testing.T) {
	if got := formatKeys(selectionMenuFor(true)); got != "para,h1,h2,h3,,check,numbered,bullet,,codeblock" {
		t.Errorf("block-style menu, on: %s", got)
	}
	if got := formatKeys(selectionMenuFor(false)); got != "para,h1,h2,h3,,check,numbered,bullet,,codeblock,quote" {
		t.Errorf("block-style menu, off: %s", got)
	}
	if got := formatKeys(formattingMenuFor(true)); got != "codeblock,bullet,numbered,check,,h3,h2,h1,para" {
		t.Errorf("formatting menu, on: %s", got)
	}
	if got := formatKeys(formattingMenuFor(false)); got != "quote,codeblock,,bullet,numbered,check,,h3,h2,h1,para" {
		t.Errorf("formatting menu, off: %s", got)
	}
	groups := func(gs [][]selButton) string {
		var out []string
		for _, g := range gs {
			var ks []string
			for _, b := range g {
				ks = append(ks, b.key)
			}
			out = append(out, strings.Join(ks, " "))
		}
		return strings.Join(out, " | ")
	}
	if got := groups(selGroupsFor(true)); got != "menu | bold italic underline strike | bqlayout | code | link callout" {
		t.Errorf("floating bar, on: %s", got)
	}
	if got := groups(selGroupsFor(false)); got != "menu | bold italic underline strike | code | link callout" {
		t.Errorf("floating bar, off: %s", got)
	}

	h, fs := newEnv(t, false)
	addLookCanvases(fs)
	var menu []MenuItem
	v := New(h, lookStarter, Tab, Actions{Menu: func(m []MenuItem) { menu = m }})
	s := newShooter(t, image.Pt(1038, 717))
	settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil })
	frames(s, v, 5)
	v.edit.openInsertMenu(v)
	if got := menuLabels(menu); got != "Divider,─,Date,Profile,Callout,Blockquote,Canvas,Image,File" {
		t.Errorf("insert menu, on: %s", got)
	}

	// the floating bar's button quotes the block, and unquotes it
	v.edit.start(v, v.blocks[2])
	frames(s, v, 3)
	ed := v.edit.cur.ed
	ed.SetSelection(0, len("First draft"))
	s.shot("bqlayout-selection-tab-light", frames(s, v, 5))
	v.edit.apply(v, "bqlayout", image.Point{})
	if ed.ActiveBlock() != composer.Quote {
		t.Error("the blockquote button didn't quote the block")
	}
	v.edit.apply(v, "bqlayout", image.Point{})
	if ed.ActiveBlock() == composer.Quote {
		t.Error("the blockquote button didn't unquote the block")
	}
	// quoted from the bar, the block saves as a quote section ("> ")
	v.edit.apply(v, "bqlayout", image.Point{})
	n := len(fs.CanvasEdits())
	if !v.edit.finish(v) {
		t.Fatal("the quoted block's save was refused")
	}
	waitFor(t, s, v, "canvases.edit", func() bool { return len(fs.CanvasEdits()) > n })
	if md := editMarkdown(t, fs.CanvasEdits()[n]); md != "> First draft of the notes." {
		t.Errorf("quoted block saved as %q", md)
	}

	// Insert > Blockquote: a quote line after the insert point, saved as "> "
	v.edit.insertQuote(v)
	if v.edit.cur == nil || v.edit.cur.ed.ActiveBlock() != composer.Quote {
		t.Fatal("Insert > Blockquote didn't open a quote line")
	}
	v.edit.cur.ed.InsertText("Keep it short")
	n = len(fs.CanvasEdits())
	if !v.edit.finish(v) {
		t.Fatal("the quote wasn't saved")
	}
	waitFor(t, s, v, "canvases.edit", func() bool { return len(fs.CanvasEdits()) > n })
	if md := editMarkdown(t, fs.CanvasEdits()[n]); md != "> Keep it short" {
		t.Errorf("saved %q", md)
	}

	// off: the insert menu has no Blockquote
	h.a.SetExperimentGroup("canvas_blockquote_layout", "off")
	frames(s, v, 2)
	v.edit.openInsertMenu(v)
	if got := menuLabels(menu); got != "Divider,─,Date,Profile,Callout,Canvas,Image,File" {
		t.Errorf("insert menu, off: %s", got)
	}
}

// waitFor draws frames until cond holds.
func waitFor(t *testing.T, s *shooter, v *View, what string, cond func() bool) {
	t.Helper()
	settle(t, s, v, what, cond)
}

// editMarkdown is a canvases.edit call's first change's markdown.
func editMarkdown(t *testing.T, call url.Values) string {
	t.Helper()
	var cs []struct {
		Content struct {
			Markdown string `json:"markdown"`
		} `json:"document_content"`
	}
	if err := json.Unmarshal([]byte(call.Get("changes")), &cs); err != nil || len(cs) == 0 {
		t.Fatalf("changes %q: %v", call.Get("changes"), err)
	}
	return cs[0].Content.Markdown
}

// slackified_checkbox: Slack's label, or the browser's own checkbox.
func TestRenderSlackifiedCheckbox(t *testing.T) {
	for _, dark := range []bool{false, true} {
		suffix := "light"
		if dark {
			suffix = "dark"
		}
		h, fs := newEnv(t, dark)
		v := New(h, fakeslack.CanvasShared, Tab, Actions{})
		s := newShooter(t, image.Pt(900, 600))
		settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil })
		frames(s, v, 10)
		gtx := gtxFor(new(op.Ops), image.Pt(900, 600))
		if !v.exp("slackified_checkbox") {
			t.Fatal("slackified_checkbox isn't on for the account")
		}
		if r := v.checkRect(gtx, 0); r != image.Rect(5, 5, 19, 19) {
			t.Errorf("Slack's label at %v", r)
		}
		s.shot("checkbox-slackified-tab-"+suffix, frames(s, v, 5))
		h.a.SetExperimentGroup("slackified_checkbox", "")
		frames(s, v, 2)
		if r := v.checkRect(gtx, 0); r.Dx() != 13 || r.Dy() != 13 || r.Min.Y != 2 {
			t.Errorf("the browser's checkbox at %v", r)
		}
		s.shot("checkbox-native-tab-"+suffix, frames(s, v, 5))
		// a check still toggles through canvases.edit either way
		var item *blk
		for _, b := range v.blocks {
			if b.b.Kind == canvas.Check && !b.b.Checked {
				item = b
				break
			}
		}
		if item == nil {
			t.Fatal("no unchecked item")
		}
		n := len(fs.CanvasEdits())
		v.edit.toggleCheck(v, item)
		waitFor(t, s, v, "the tick's save", func() bool { return len(fs.CanvasEdits()) > n })
		if md := editMarkdown(t, fs.CanvasEdits()[n]); !strings.HasPrefix(md, "- [x] ") {
			t.Errorf("ticked %q", md)
		}
	}
}
