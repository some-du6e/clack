package richview

import (
	"image"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Synthetic fixtures only.

// A pill box on no target (a channel the viewer can't see draws its
// mention-style chip without a link) never takes the hover fill: hovering
// the link before it (target 0) leaves the chip as it was.
func TestHoverFillNeedsTarget(t *testing.T) {
	d := doc(para(txt("See "), rt.Span{Kind: rt.Link, Text: "the doc", URL: "https://example.com"}, txt(" in "),
		rt.Span{Kind: rt.Channel, Text: "#old-team", ID: "C0HIDDEN", Off: true}))
	hs := newHarness(d, &recHandler{})
	var chip *box
	for i := range hs.v.d.boxes {
		if b := &hs.v.d.boxes[i]; b.hoverOver && b.target < 0 {
			chip = b
		}
	}
	if chip == nil {
		t.Fatal("no pill box without a target (the hidden channel's chip)")
	}
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: center(findFrag(t, &hs.v.d, "doc"))})
	if hs.v.hover != 0 {
		t.Fatalf("hover %d, want the link (0)", hs.v.hover)
	}
	if hs.v.hoverOverShown(chip) {
		t.Errorf("hovering the link shows the hidden channel chip's hover fill")
	}
}

// plainDTHandler is dtHandler with a search card's plain click.
type plainDTHandler struct {
	dtHandler
	plain int
}

func (h *plainDTHandler) PlainClick() { h.plain++ }

// A data_table row takes no click of its own (its target is the row's
// hover only): a click on it is the view's plain click, which opens a
// search card's result. A header button is its own action.
func TestDataTableRowPlainClick(t *testing.T) {
	h := &plainDTHandler{}
	hs := newHarness(kitDoc(t, `[`+boardTable(3, "")+`]`), h)
	hs.w = 700
	hs.frame()
	tbl := oneTable(t, hs.v)
	if len(tbl.Rows) != 3 {
		t.Fatalf("%d rows", len(tbl.Rows))
	}
	hs.clickRect(tbl.Rows[1], 0)
	if h.plain != 1 {
		t.Errorf("a click on a row: %d plain clicks, want 1", h.plain)
	}
	hs.clickRect(oneTable(t, hs.v).Buttons["more"], time.Second)
	if h.plain != 1 || len(h.acts) != 1 || h.acts[0] != "more" {
		t.Errorf("More actions: %d plain clicks, actions %q; want 1 and [more]", h.plain, h.acts)
	}
}

// A table that shrinks under a later page (an edited message): the view
// shows its last page, and prev steps back from the page shown, as
// computeView keeps the clamped pageIndex.
func TestDataTablePageAfterShrink(t *testing.T) {
	h := &dtHandler{}
	hs := newHarness(kitDoc(t, `[`+boardTable(30, "")+`]`), h)
	hs.w = 700
	hs.frame()
	for i := range 5 {
		hs.clickRect(oneTable(t, hs.v).Buttons["next"], time.Duration(i)*time.Second)
	}
	if p, n := hs.v.DataTablePage(oneTable(t, hs.v).Block); p != 5 || n != 6 {
		t.Fatalf("page %d of %d, want 5 of 6", p, n)
	}
	hs.v.Set(kitDoc(t, `[`+boardTable(12, "")+`]`), Options{})
	hs.frame()
	dt := oneTable(t, hs.v).Block
	if got := hs.v.DataTableRows(dt); strings.Join(got, ",") != "11,12" {
		t.Errorf("12 rows: shown %q, want the last page", got)
	}
	hs.clickRect(oneTable(t, hs.v).Buttons["prev"], 10*time.Second)
	if got := hs.v.DataTableRows(dt); strings.Join(got, ",") != "6,7,8,9,10" {
		t.Errorf("prev from the last page: %q, want page 2", got)
	}
}

// kitRows is a Kit table of a header and n rows.
func kitRows(n int, whole bool) rt.Doc {
	cell := func(s string) []rt.Span { return []rt.Span{{Kind: rt.Text, Text: s}} }
	b := rt.Block{Kind: rt.Table, Kit: true, Whole: whole, Rows: [][][]rt.Span{{cell("Series"), cell("Value")}}}
	for i := 1; i <= n; i++ {
		b.Rows = append(b.Rows, [][]rt.Span{cell("s" + strconv.Itoa(i)), cell(strconv.Itoa(i * 10))})
	}
	return doc(b)
}

// A message's table shows its first 10 rows (TablePreview); a whole one
// (View as table's, TableFullscreenModal) every row.
func TestKitTableWhole(t *testing.T) {
	st := newTestStyle()
	has := func(v *View, s string) bool {
		for _, f := range textFrags(&v.d) {
			if f.sh.str == s {
				return true
			}
		}
		return false
	}
	v := layoutAt(st, kitRows(12, false), Options{}, 600)
	if !has(v, "s9") || has(v, "s10") || has(v, "s12") {
		t.Errorf("preview: want the header and rows s1-s9 (10 rows)")
	}
	v = layoutAt(st, kitRows(12, true), Options{}, 600)
	for i := 1; i <= 12; i++ {
		if !has(v, "s"+strconv.Itoa(i)) {
			t.Errorf("whole: no row s%d", i)
		}
	}
}

// The empty filter's Clear filter is a button: it clears the filter (the
// rows come back) and tells the owner, whose filter field clears too.
func TestDataTableClearFilter(t *testing.T) {
	h := &dtHandler{}
	d := kitDoc(t, `[`+boardTable(12, "")+`]`)
	dt := d.Blocks[0].DataTable
	hs := newHarness(d, h)
	hs.w = 700
	c := NewTableControl(dt, nil)
	c.SetFilter("zzz")
	hs.v.SetTableControl(dt, c)
	hs.frame()
	if got := hs.v.DataTableRows(dt); len(got) != 0 {
		t.Fatalf("filtered: rows %q", got)
	}
	hs.clickRect(fragRect(findFrag(t, &hs.v.d, "Clear")), 0)
	if len(h.acts) != 1 || h.acts[0] != "clearfilter" {
		t.Errorf("actions %q, want [clearfilter]", h.acts)
	}
	if c.FilterText() != "" {
		t.Errorf("filter %q after Clear filter", c.FilterText())
	}
	if got := hs.v.DataTableRows(dt); strings.Join(got, ",") != "1,2,3,4,5" {
		t.Errorf("after Clear filter: rows %q", got)
	}
}

// A new doc drops the chart hover held from the one before.
func TestChartHoverResetOnSet(t *testing.T) {
	hs := newHarness(kitDoc(t, `[`+startupViz+`]`), &dtHandler{})
	hs.w = 700
	hs.frame()
	cs := hs.v.ChartRects()
	b := hs.v.ChartLayout(0).Bars[1]
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(cs[0].R.Min.X)+float32(b.X+b.W/2), float32(cs[0].R.Min.Y)+float32(b.Y+b.H/2))})
	if ci, idx := hs.v.ChartHover(); ci != 0 || idx != 1 {
		t.Fatalf("hover %d %d, want chart 0, bar 1", ci, idx)
	}
	hs.v.Set(kitDoc(t, `[`+boardTable(3, "")+`]`), Options{})
	if hs.v.vizHot != 0 || hs.v.vizIdx != -1 {
		t.Errorf("after Set: chart %d, index %d held, want none", hs.v.vizHot, hs.v.vizIdx)
	}
}

// With the view's window origin known (the pointer's moves over it teach
// it), a chart's More actions and a table's buttons reach the handler in
// window coordinates, so their menus open at the button wherever the view
// sits (a modal, App Home, a views row), not at the window's corner.
func TestDataActionsWindowRect(t *testing.T) {
	org := image.Pt(130, 70)
	var win image.Point
	prevP, prevS := theme.WindowPointer, theme.WindowSize
	theme.WindowPointer = func() (image.Point, bool) { return win, true }
	theme.WindowSize = func() image.Point { return image.Pt(1600, 1000) }
	t.Cleanup(func() { theme.WindowPointer, theme.WindowSize = prevP, prevS })
	at := func(p f32.Point) { win = image.Pt(int(p.X+.5), int(p.Y+.5)).Add(org) }

	h := &dtHandler{}
	hs := newHarness(kitDoc(t, `[`+startupViz+`,`+boardTable(12, "")+`]`), h)
	hs.w = 700
	hs.frame()
	cs := hs.v.ChartRects()
	b := hs.v.ChartLayout(0).Bars[1]
	p := f32.Pt(float32(cs[0].R.Min.X)+float32(b.X+b.W/2), float32(cs[0].R.Min.Y)+float32(b.Y+b.H/2))
	at(p)
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	var more image.Rectangle
	for _, r := range hs.v.FileActionRects() {
		if r.Action == FileMore {
			more = r.R
		}
	}
	if more.Empty() {
		t.Fatal("no More actions on the hovered chart")
	}
	at(dtCenter(more))
	hs.clickRect(more, time.Second)
	if len(h.charts) != 1 || len(h.wins) != 1 || !h.wins[0] || h.btns[0] != more.Add(org) {
		t.Errorf("ChartAction: %d calls, window %v, rect %v; want 1, true, %v", len(h.charts), h.wins, h.btns, more.Add(org))
	}
	btn := oneTable(t, hs.v).Buttons["more"]
	at(dtCenter(btn))
	hs.clickRect(btn, 2*time.Second)
	if len(h.acts) != 1 || h.acts[0] != "more" || !h.ctls[0].Window || h.btns[1] != btn.Add(org) {
		t.Errorf("DataTableAction: %q, rects %v; want [more] at %v in window coordinates", h.acts, h.btns, btn.Add(org))
	}
}
