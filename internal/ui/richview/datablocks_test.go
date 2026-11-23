package richview

import (
	"image"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/chart"
	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
)

// Synthetic fixtures only (internal/richtext's datablocks_test.go has the
// same shapes).

const startupViz = `{"type":"data_visualization","block_id":"viz-bar-single","title":"startup loading time","chart":{"type":"bar","series":[{"name":"time to window (ms)","data":[{"label":"Slack","value":1059},{"label":"Slick","value":1816},{"label":"Taut","value":2579},{"label":"Clack","value":212}]}],"axis_config":{"categories":["Slack","Slick","Taut","Clack"],"x_label":"client","y_label":"time to window (ms)"}}}`

// boardTable is a three-column data_table of n raw_text rows.
func boardTable(n int, extra string) string {
	var b strings.Builder
	b.WriteString(`{"type":"data_table","block_id":"dtlb1","caption":"Full points leaderboard"` + extra + `,"rows":[[{"type":"raw_text","text":"Rank"},{"type":"raw_text","text":"Name"},{"type":"raw_text","text":"Points"}]`)
	for i := 0; i < n; i++ {
		b.WriteString(`,[{"type":"raw_text","text":"` + strconv.Itoa(i+1) + `"},{"type":"raw_text","text":"member ` + strconv.Itoa(i+1) + `"},{"type":"raw_text","text":"` + strconv.Itoa(900-i*8) + `"}]`)
	}
	b.WriteString(`]}`)
	return b.String()
}

// dtHandler records a data_table's and a chart's actions.
type dtHandler struct {
	recHandler
	acts   []string
	ctls   []*TableControl
	btns   []image.Rectangle
	charts []*rt.DataViz
	wins   []bool // ChartAction's window flags
}

func (h *dtHandler) DataTableAction(act string, c *TableControl, btn image.Rectangle) {
	h.acts, h.ctls, h.btns = append(h.acts, act), append(h.ctls, c), append(h.btns, btn)
}

func (h *dtHandler) ChartAction(v *rt.DataViz, btn image.Rectangle, window bool) {
	h.charts = append(h.charts, v)
	h.btns = append(h.btns, btn)
	h.wins = append(h.wins, window)
}

func dtCenter(r image.Rectangle) f32.Point {
	return f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
}

// oneTable is v's only data_table (fatal otherwise).
func oneTable(t *testing.T, v *View) DataTableRect {
	t.Helper()
	ts := v.DataTableRects()
	if len(ts) != 1 {
		t.Fatalf("%d data_tables laid out, want 1", len(ts))
	}
	return ts[0]
}

// clickRect clicks r's centre at time at.
func (hs *harness) clickRect(r image.Rectangle, at time.Duration) {
	p := dtCenter(r)
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: at}, press(p, at), release(p, at+20*time.Millisecond))
}

// TestDataTableLayout: DataTableBlock's legacy CSS at 700px: the 46px
// header (padding 8px 4px; the caption at its left, Search table, View
// and More actions right-aligned 8px apart, each a 28px IconButton in a
// 1px frame), the container (padding 0 4px) with its 36px header row, five
// 48px rows in thirds of the width, and the 32px footer with the first
// five pages.
func TestDataTableLayout(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, kitDoc(t, `[`+boardTable(110, "")+`]`), Options{}, 700)
	ts := v.DataTableRects()
	if len(ts) != 1 {
		t.Fatalf("%d tables", len(ts))
	}
	tb := ts[0]
	for id, want := range map[string]image.Rectangle{
		"more":   image.Rect(667, 13, 695, 41),
		"view":   image.Rect(629, 13, 657, 41),
		"search": image.Rect(591, 13, 619, 41),
		"sort:0": image.Rect(4, 54, 234, 82),
		"sort:2": image.Rect(465, 54, 696, 82),
	} {
		if got := tb.Buttons[id]; got != want {
			t.Errorf("%s at %v, want %v", id, got, want)
		}
	}
	// every glyph the table draws is one of Clack's icons (Slack's names
	// differ: arrow-expand-full is fullscreen here)
	for i := range v.d.imgs {
		if name, ok := strings.CutPrefix(v.d.imgs[i].url, iconURL); ok && !icons.Has(name) {
			t.Errorf("the table draws icon %q, which Clack's icons don't have", name)
		}
	}
	// each header button draws its glyph inside it
	for _, id := range []string{"more", "view", "search"} {
		r, n := tb.Buttons[id], 0
		for i := range v.d.imgs {
			if im := &v.d.imgs[i]; strings.HasPrefix(im.url, iconURL) && im.r.In(r) && im.r.Dx() == 16 {
				n++
			}
		}
		if n == 0 {
			t.Errorf("the %s button %v draws no 16px glyph", id, r)
		}
	}
	if len(tb.Rows) != 5 || tb.Rows[0] != image.Rect(4, 86, 696, 134) || tb.Rows[4] != image.Rect(4, 278, 696, 326) {
		t.Errorf("rows %v, want five 48px rows from y 86", tb.Rows)
	}
	for p := 1; p <= 5; p++ {
		if _, ok := tb.Buttons["page:"+strconv.Itoa(p)]; !ok {
			t.Errorf("no page %d button", p)
		}
	}
	if _, ok := tb.Buttons["page:6"]; ok {
		t.Errorf("a sixth page button")
	}
	if pr, nx := tb.Buttons["prev"], tb.Buttons["next"]; !pr.Empty() || nx.Dy() != 20 || nx.Min.Y != 332 {
		t.Errorf("prev %v (none on the first page), next %v (20px at y 332)", pr, nx)
	}
	c := findFrag(t, &v.d, "Full")
	if int(c.x) != 4 || c.top != 4+8+4 {
		t.Errorf("caption at (%.0f, %d), want (4, 16): the header's padding 8px 4px, centred on the buttons", c.x, c.top)
	}
	if h := findFrag(t, &v.d, "Rank"); int(h.x) != 16 || h.top < 54 || h.bot > 82 {
		t.Errorf("Rank header at x %.0f y %d–%d, want 16 inside the 28px button", h.x, h.top, h.bot)
	}
	if n := findFrag(t, &v.d, "member"); int(n.x) != 234+12 {
		t.Errorf("a name at x %.0f, want its column's 246", n.x)
	}
	if got, want := v.d.size.Y, 4+46+36+5*48+32+4; got != want {
		t.Errorf("height %d, want %d", got, want)
	}
	// the body's border: otl-sec, radius 8, round the five rows
	var body *box
	for i := range v.d.boxes {
		if b := &v.d.boxes[i]; b.kind == bBorder && b.perCorner && b.r == image.Rect(4, 86, 696, 326) {
			body = b
		}
	}
	if body == nil || body.corners != [4]int{8, 8, 8, 8} {
		t.Errorf("body border %+v", body)
	}
	// a modal or App Home: the full width; a message: at most 800px
	if w := oneTable(t, layoutAt(st, kitDoc(t, `[`+boardTable(3, "")+`]`), Options{}, 1000)).Buttons["more"].Max.X; w != 800-4-1 {
		t.Errorf("message at 1000px: More actions ends at %d, want 795 (800px max)", w)
	}
	if w := oneTable(t, layoutAt(st, kitDoc(t, `[`+boardTable(3, "")+`]`), Options{FillBlocks: true}, 1000)).Buttons["more"].Max.X; w != 1000-4-1 {
		t.Errorf("view at 1000px: More actions ends at %d, want 995", w)
	}
}

// Paging: the pagination's buttons move between pages, every page as tall
// as the first (the spacer stands in for a short page's rows).
func TestDataTablePaging(t *testing.T) {
	h := &dtHandler{}
	hs := newHarness(kitDoc(t, `[`+boardTable(110, "")+`]`), h)
	hs.w = 700
	hs.frame()
	tbl := oneTable(t, hs.v)
	dt := tbl.Block
	if p, n := hs.v.DataTablePage(dt); p != 0 || n != 22 {
		t.Fatalf("page %d of %d, want 0 of 22", p, n)
	}
	hs.clickRect(tbl.Buttons["next"], 0)
	if got := hs.v.DataTableRows(dt); strings.Join(got, ",") != "6,7,8,9,10" {
		t.Errorf("after next: %q", got)
	}
	hs.clickRect(oneTable(t, hs.v).Buttons["page:5"], time.Second)
	if got := hs.v.DataTableRows(dt); strings.Join(got, ",") != "21,22,23,24,25" {
		t.Errorf("page 5: %q", got)
	}
	// the range follows the page: 3..7 around page 5
	tbl = oneTable(t, hs.v)
	if _, ok := tbl.Buttons["page:7"]; !ok {
		t.Errorf("page 5 shows no page 7 button: %v", tbl.Buttons)
	}
	hs.clickRect(tbl.Buttons["prev"], 2*time.Second)
	if p, _ := hs.v.DataTablePage(dt); p != 3 {
		t.Errorf("after prev: page %d, want 3", p)
	}
	if len(h.acts) != 0 {
		t.Errorf("paging called the handler: %q", h.acts)
	}

	// a short last page keeps the height
	hs = newHarness(kitDoc(t, `[`+boardTable(7, `,"page_size":3`)+`]`), h)
	hs.w = 700
	hs.frame()
	h0 := hs.v.d.size.Y
	dt = oneTable(t, hs.v).Block
	hs.clickRect(oneTable(t, hs.v).Buttons["page:3"], 0)
	if got := hs.v.DataTableRows(dt); strings.Join(got, ",") != "7" || hs.v.d.size.Y != h0 {
		t.Errorf("last page %q at height %d, want [7] at %d", got, hs.v.d.size.Y, h0)
	}
	// one page: the footer's pagination is hidden, the row keeps its 32px
	v := layoutAt(newTestStyle(), kitDoc(t, `[`+boardTable(5, "")+`]`), Options{}, 700)
	if b := oneTable(t, v).Buttons; len(b) != 6 { // more, view, search, three sorts
		t.Errorf("one page: buttons %v", b)
	}
	if got, want := v.d.size.Y, 4+46+36+5*48+4+4; got != want {
		t.Errorf("five rows: height %d, want %d (the compact 4px footer)", got, want)
	}
}

// The header buttons and column headers reach the handler; a sort from
// the column's menu relays the table out.
func TestDataTableSort(t *testing.T) {
	h := &dtHandler{}
	hs := newHarness(kitDoc(t, `[`+boardTable(110, "")+`]`), h)
	hs.w = 700
	hs.frame()
	tbl := oneTable(t, hs.v)
	for i, id := range []string{"more", "view", "search", "sort:2"} {
		hs.clickRect(tbl.Buttons[id], time.Duration(i)*time.Second)
	}
	if strings.Join(h.acts, ",") != "more,view,search,sort" || h.ctls[3].Column != 2 {
		t.Fatalf("actions %q (column %d)", h.acts, h.ctls[len(h.ctls)-1].Column)
	}
	if want := tbl.Buttons["more"]; h.btns[0] != want {
		t.Errorf("More actions' anchor %v, want its button %v", h.btns[0], want)
	}
	// Descending from the Points column's menu: raw_text sorts as text
	// (sortRows: upper-cased code units), so "92" comes before "900"
	h.ctls[3].Sort(2, true)
	hs.frame()
	if got := hs.v.DataTableRows(tbl.Block); len(got) != 5 || got[0] != "102" || got[1] != "1" {
		t.Errorf("descending by points: %q, want rank 102 (92) then 1 (900)", got)
	}
	if c, desc, ok := h.ctls[3].SortState(); !ok || c != 2 || !desc {
		t.Errorf("sort state %d %v %v", c, desc, ok)
	}
	// the same sort again unsorts
	h.ctls[3].Sort(2, true)
	hs.frame()
	if got := hs.v.DataTableRows(tbl.Block); got[0] != "1" {
		t.Errorf("unsorted: %q", got)
	}
	// raw_number cells sort by value, empty text last either way
	nd := kitDoc(t, `[{"type":"data_table","block_id":"n","rows":[[{"type":"raw_text","text":"name"},{"type":"raw_text","text":"n"}],
		[{"type":"raw_text","text":"b"},{"type":"raw_number","value":42,"text":"42"}],
		[{"type":"raw_text","text":""},{"type":"raw_number","value":7,"text":"7"}],
		[{"type":"raw_text","text":"a"},{"type":"raw_number","value":1250,"text":"1,250"}]]}]`)
	if len(nd.Blocks) != 1 || nd.Blocks[0].DataTable == nil {
		t.Fatalf("not a data_table: %+v", nd.Blocks)
	}
	dt := nd.Blocks[0].DataTable
	for _, c := range []struct {
		col  int
		desc bool
		want string
	}{{1, false, ",b,a"}, {1, true, "a,b,"}, {0, false, "a,b,"}, {0, true, "b,a,"}} {
		s := &tableState{sortCol: c.col, desc: c.desc}
		vw := computeDTView(dt, s, false)
		var got []string
		for _, r := range vw.rows {
			got = append(got, r[0].Sort)
		}
		if strings.Join(got, ",") != c.want {
			t.Errorf("column %d desc %v: %q, want %q", c.col, c.desc, got, c.want)
		}
	}
}

// Each error state: what throws in Slack's render draws the message's
// errorFallback (nothing in a view); the other blocks still draw.
func TestDataBlocksErrorStates(t *testing.T) {
	st := newTestStyle()
	const fallback = "We can’t fully display this message."
	for _, c := range []struct{ name, block string }{
		{"a table whose rows aren't an array", `{"type":"data_table","rows":{"a":1}}`},
		{"a table whose header cell is a string", `{"type":"data_table","rows":[["x"],[{"type":"raw_text","text":"a"}]]}`},
		{"a table whose caption is an object", `{"type":"data_table","caption":{},"rows":[]}`},
		{"a chart with no chart", `{"type":"data_visualization","title":"t"}`},
		{"a chart with a duplicate category", `{"type":"data_visualization","title":"t","chart":{"type":"bar","series":[],"axis_config":{"categories":["a","a"]}}}`},
		{"a chart whose series aren't an array", `{"type":"data_visualization","title":"t","chart":{"type":"line","series":{},"axis_config":{"categories":["a"]}}}`},
		{"a chart whose title is an object", `{"type":"data_visualization","title":{"a":1},"chart":{"type":"pie","segments":[]}}`},
	} {
		d := kitDoc(t, `[{"type":"section","text":{"type":"mrkdwn","text":"before"}},`+c.block+`,{"type":"section","text":{"type":"mrkdwn","text":"after"}}]`)
		v := layoutAt(st, d, Options{}, 700)
		got := strings.Join(fragStrings(&v.d), " ")
		if !strings.Contains(got, "before") || !strings.Contains(got, "after") || !strings.Contains(got, "fully display") {
			t.Errorf("%s: %q, want the sections and %q", c.name, got, fallback)
		}
		if len(v.ChartRects()) != 0 || len(v.DataTableRects()) != 0 {
			t.Errorf("%s: drew a chart or a table", c.name)
		}
		v = layoutAt(st, d, Options{FillBlocks: true}, 700)
		if got := strings.Join(fragStrings(&v.d), " "); strings.Contains(got, "fully display") {
			t.Errorf("%s in a view: %q, want no errorFallback", c.name, got)
		}
	}
	// an unknown chart type: the card and its title, no chart
	v := layoutAt(st, kitDoc(t, `[{"type":"data_visualization","title":"a scatter chart","chart":{"type":"scatter","series":[]}}]`), Options{}, 700)
	if got := strings.Join(fragStrings(&v.d), " "); got != "a scatter chart" || len(v.ChartRects()) != 0 {
		t.Errorf("unknown type: %q, %d charts", got, len(v.ChartRects()))
	}
	// values that aren't numbers are gaps: the axes and no bars
	v = layoutAt(st, kitDoc(t, `[{"type":"data_visualization","title":"none","chart":{"type":"bar","series":[{"name":"s","data":[{"label":"a","value":"n/a"}]}],"axis_config":{"categories":["a"]}}}]`), Options{}, 700)
	if cs := v.ChartRects(); len(cs) != 1 || len(v.ChartLayout(0).Bars) != 0 {
		t.Errorf("no values: %d charts", len(cs))
	}
}

// TestDataVizLayout: the legacy layout at 700px, light and dark: the card
// 624px wide (the wrapper's max) with its 1px otl-ter border and radius
// 12, the title (18/24 Black) 12px in, the 360px chart area 8px under it,
// the bars in the theme's first series colour, the axis labels and the
// legend's swatch.
func TestDataVizLayout(t *testing.T) {
	for _, dark := range []bool{false, true} {
		st := newTestStyle()
		if dark {
			st = NewStyle(sharedShaper(), noImages{}, DarkPalette())
			st.Face, st.Mono = "Go", "Go Mono"
		}
		d := kitDoc(t, `[`+startupViz+`]`)
		v := layoutAt(st, d, Options{}, 700)
		var frame *box
		for i := range v.d.boxes {
			if b := &v.d.boxes[i]; b.kind == bBorder && b.radius == 12 {
				frame = b
				break
			}
		}
		l := lay{st: st}
		if frame == nil || frame.r.Min != image.Pt(0, 8) || frame.r.Dx() != 624 || frame.stroke != l.tok("otl-ter") || frame.fill != l.tok("base-pry") {
			t.Fatalf("dark %v: frame %+v", dark, frame)
		}
		title := findFrag(t, &v.d, "startup")
		if int(title.x) != 13 || title.top != 21 {
			t.Errorf("dark %v: title at (%.0f, %d), want (13, 21)", dark, title.x, title.top)
		}
		cs := v.ChartRects()
		if len(cs) != 1 || cs[0].R != image.Rect(13, 21+24+8, 13+598, 21+24+8+360) || cs[0].Viz == nil {
			t.Fatalf("dark %v: chart %+v", dark, cs)
		}
		c := &v.d.charts[0]
		if want := l.tok(chart.PaletteTokens[0][0]); c.pal.Series[0] != want {
			t.Errorf("dark %v: first series %v, want %v", dark, c.pal.Series[0], want)
		}
		lay := v.ChartLayout(0)
		// bars grow up from the axis (a negative height, as zrender's rects)
		if len(lay.Bars) != 4 || lay.Bars[2].H >= lay.Bars[1].H || lay.Bars[1].H >= lay.Bars[0].H || lay.Bars[3].H <= lay.Bars[0].H {
			t.Errorf("dark %v: bars %+v (Taut's the tallest, Clack's the shortest)", dark, lay.Bars)
		}
		// the axis labels are painted with the chart; the x axis label and
		// the legend are text
		labels := map[string]bool{}
		for _, lb := range lay.Labels {
			labels[lb.Text] = true
		}
		for _, s := range []string{"Slack", "Slick", "Taut", "Clack", "0", "1,000", "2,000", "3,000"} {
			if !labels[s] {
				t.Errorf("dark %v: no axis label %q in %v", dark, s, labels)
			}
		}
		findFrag(t, &v.d, "client")
		findFrag(t, &v.d, "window")
		// the legend's swatch: a 10px circle in the series' colour
		var sw *box
		for i := range v.d.boxes {
			if b := &v.d.boxes[i]; b.kind == bFill && b.r.Dx() == 10 && b.r.Dy() == 10 && b.radius == 5 {
				sw = b
			}
		}
		if sw == nil || sw.fill != c.pal.Series[0] || sw.r.Min.Y <= cs[0].R.Max.Y {
			t.Errorf("dark %v: legend swatch %+v", dark, sw)
		}
		if v.ChartLabel(0) != "Bar chart: startup loading time. The X axis is client, the Y axis is time to window (ms). There is 1 series: time to window (ms)." {
			t.Errorf("aria-label %q", v.ChartLabel(0))
		}
	}
}

// The hover: the band under the pointer, its tooltip's rows; More actions
// opens the menu from its button.
func TestDataVizHoverAndMenu(t *testing.T) {
	h := &dtHandler{}
	hs := newHarness(kitDoc(t, `[`+startupViz+`]`), h)
	hs.w = 700
	hs.frame()
	cs := hs.v.ChartRects()
	if len(cs) != 1 {
		t.Fatalf("%d charts", len(cs))
	}
	lay := hs.v.ChartLayout(0)
	// Slick's bar's middle (chart px are view px at 1px per dp)
	b := lay.Bars[1]
	p := f32.Pt(float32(cs[0].R.Min.X)+float32(b.X+b.W/2), float32(cs[0].R.Min.Y)+float32(b.Y+b.H/2))
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	if ci, idx := hs.v.ChartHover(); ci != 0 || idx != 1 {
		t.Fatalf("hover %d %d, want chart 0, Slick (1)", ci, idx)
	}
	tip, ok := lay.TooltipAt(1)
	if !ok || tip.Header != "Slick" || len(tip.Rows) != 1 || tip.Rows[0].Name != "time to window (ms)" || tip.Rows[0].Value != "1,816" {
		t.Errorf("tooltip %+v", tip)
	}
	// More actions: shown while the card is hovered
	var more image.Rectangle
	for _, r := range hs.v.FileActionRects() {
		if r.Action == FileMore {
			more = r.R
		}
	}
	// the group at top 16px, right 16px over the card; the button 4px in
	if more.Empty() || more.Max.X != 624-16-4 || more.Min.Y != 8+16+4 || more.Dx() != 32 {
		t.Fatalf("More actions at %v, want the 32px button in its group at top 16, right 16", more)
	}
	hs.clickRect(more, time.Second)
	if len(h.charts) != 1 || h.charts[0] == nil || h.charts[0].Title != "startup loading time" {
		t.Errorf("ChartAction calls %+v", h.charts)
	}
	// the pointer leaving: no hover
	hs.send(pointer.Event{Kind: pointer.Leave, Source: pointer.Mouse, Position: f32.Pt(-1, -1)})
	if ci, _ := hs.v.ChartHover(); ci != -1 {
		t.Errorf("after leaving: chart %d hovered", ci)
	}
}

// The filter is Slack's FuzzyMatcher at word boundaries (fuzzyLimit 3),
// not a substring search; with nothing left the table draws "No results
// found." as tall as its header row and a page.
func TestDataTableFilter(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[`+boardTable(110, "")+`]`)
	if len(d.Blocks) != 1 || d.Blocks[0].DataTable == nil {
		t.Fatalf("not a data_table: %+v", d.Blocks)
	}
	dt := d.Blocks[0].DataTable
	c := NewTableControl(dt, nil)
	c.SetFilter("member 10")
	var got []string
	for _, r := range computeDTView(dt, c.st, true).rows {
		got = append(got, r[0].Sort)
	}
	if strings.Join(got, ",") != "10,100,101,102,103,104,105,106,107,108,109" {
		t.Errorf(`"member 10" matches %q`, got)
	}
	h0 := layoutAt(st, d, Options{}, 700).d.size.Y
	c.SetFilter("ember") // inside a word: no match
	v := New(d, Options{})
	v.SetTableControl(dt, c)
	v.relayout(st, 700, 1, 1)
	if vw := computeDTView(dt, c.st, false); !vw.empty || vw.paginate {
		t.Errorf("mid-word filter: %d rows (empty %v)", len(vw.rows), vw.empty)
	}
	findFrag(t, &v.d, "results")
	if v.d.size.Y != h0 {
		t.Errorf("empty filter: height %d, want %d", v.d.size.Y, h0)
	}
}
