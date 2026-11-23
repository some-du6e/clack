package richview

// A data_table block: Slack's DataTableBlock (boot-shared Phw4; its body
// rows cNHK, header row x+0S, state A1Xw, sort and filter UCSE, the
// pagination 4YPB; CDN build 9f450b8ad3db), with client-boot-styles'
// .dataTableBlock* rules:
//
//   - .dataTableBlockOuterContainer: a column, margin 4px 0 (the block's,
//     blocks.go); at most 800px wide in a message (ConstrainedWidth), the
//     full width in a modal and App Home;
//   - .dataTableBlockHeader: padding 8px 4px, items right-aligned 8px
//     apart, centred: the caption (15px/1.46668 weight 900, content-pry,
//     one line with an ellipsis, flex auto), then Search table, View
//     {caption} and More actions, each a 28px IconButton (small: 16px
//     glyph, content-sec; :hover content-pry on base-ter) in a 1px
//     otl-sec border with radius 8;
//   - .dataTableBlockContainer: surf-pry, radius 12, padding 0 4px; the
//     table max(100%, columns × 120px) wide (overflow-x auto: Slack
//     scrolls a wider one sideways), its columns 1fr each;
//   - .dataTableBlockRowHeader: 36px, content-sec; a header cell's
//     sortable button 28px tall, padding 0 12px, radius 8, 4px gaps: the
//     text (13px/1.38463 bold, one line), the sort's arrow-up or
//     arrow-down (16px) and a caret-down shown on :hover;
//   - .dataTableBlockRowBody: 48px rows of 15px/1.46668 content-pry; each
//     cell base-pry (base-sec under the pointer), padding 0 12px, its
//     content clamped to one 24px line; a 1px otl-sec border round the
//     body (radius 8 at its corners), rows after the first under an inset
//     1px otl-ter line;
//   - .dataTableBlockSpacer: the rows a short page lacks (48px each), so
//     every page is as tall as the first;
//   - .dataTableBlockFooter: with more rows than a page (or is_salesforce)
//     a 32px row with the pagination centred (13px content-sec, gap 8;
//     Pagination: the back and forward carets in 20px circles and up to
//     five 20px page buttons 4px apart, the current one base-inv-hgl-2 on
//     content-inv-pry), hidden (visibility) with one page; else a 4px
//     .dataTableBlockFooterCompact.
//
// A render that throws draws the surface's errorFallback, as a chart's
// (dataviz.go). Left out: the sideways scroll (a table wider than its pane
// draws its columns narrower than 120px here), the header's filter field
// (Search table opens the fullscreen view's), the expand-cell popover and
// the caption's and header cells' truncation tooltips.

import (
	"image"
	"image/color"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/richtext"
)

// DataTableHandler is optional on a Handler: a data_table's header button
// or a column header was clicked (btn: the button, window coordinates).
// act is "search", "view", "more" or "sort" (TableControl.Column).
type DataTableHandler interface {
	DataTableAction(act string, c *TableControl, btn image.Rectangle)
}

// TableControl is one data_table's component state (useDataTableState) as
// the menus and the fullscreen view see it.
type TableControl struct {
	Table  *richtext.DataTable
	Column int // "sort": the column header clicked
	// Elements is the element host of the view the table is in (its
	// message's): the fullscreen view draws the action cells' buttons
	// through it, so they send block_actions as the inline ones do
	Elements Elements
	st       *tableState
}

// tableState is useDataTableState's reducer state: the page, the sort and
// the filter. The View keeps one per table across layouts.
type tableState struct {
	page    int
	sortCol int // -1: unsorted
	desc    bool
	filter  string
	changed bool // a menu changed it: lay out again
}

// SortState is the sort: its column and direction (ok false: unsorted).
func (c *TableControl) SortState() (col int, desc, ok bool) {
	if c == nil || c.st == nil || c.st.sortCol < 0 {
		return 0, false, false
	}
	return c.st.sortCol, c.st.desc, true
}

// Sort is SORT_COLUMN: sorts by col, the same sort again unsorts; back to
// the first page.
func (c *TableControl) Sort(col int, desc bool) {
	if c == nil || c.st == nil {
		return
	}
	s := c.st
	if s.sortCol == col && s.desc == desc {
		s.sortCol = -1
	} else {
		s.sortCol, s.desc = col, desc
	}
	s.page, s.changed = 0, true
}

// ResetSort is RESET_SORT.
func (c *TableControl) ResetSort() {
	if c == nil || c.st == nil {
		return
	}
	c.st.sortCol, c.st.page, c.st.changed = -1, 0, true
}

// FilterText is the filter's text.
func (c *TableControl) FilterText() string {
	if c == nil || c.st == nil {
		return ""
	}
	return c.st.filter
}

// SetFilter is FILTER: the rows that match s, from the first page.
func (c *TableControl) SetFilter(s string) {
	if c == nil || c.st == nil || c.st.filter == s {
		return
	}
	c.st.filter, c.st.page, c.st.changed = s, 0, true
}

// NewTableControl is a fresh state for t (the fullscreen view's, from the
// inline table's sort and filter: initialSortState, initialFilterText).
func NewTableControl(t *richtext.DataTable, from *TableControl) *TableControl {
	st := &tableState{sortCol: -1}
	c := &TableControl{Table: t, st: st}
	if from != nil {
		c.Elements = from.Elements
		if from.st != nil {
			st.sortCol, st.desc, st.filter = from.st.sortCol, from.st.desc, from.st.filter
		}
	}
	return c
}

// SetTableControl makes c the state of v's data_table t (the fullscreen
// view's richview).
func (v *View) SetTableControl(t *richtext.DataTable, c *TableControl) {
	if v.tables == nil {
		v.tables = map[any]*tableState{}
	}
	v.tables[tableKey(t)] = c.st
	v.dirty = true
}

func tableKey(t *richtext.DataTable) any {
	if t.BlockID != "" {
		return "dt:" + t.BlockID
	}
	return t
}

// tablesChanged reports (and clears) a change a menu made to a table's
// state.
func (v *View) tablesChanged() bool {
	ch := false
	for _, s := range v.tables {
		if s.changed {
			s.changed, ch = false, true
		}
	}
	return ch
}

func (l *lay) tableState(t *richtext.DataTable) *tableState {
	if l.tables == nil {
		return &tableState{sortCol: -1}
	}
	k := tableKey(t)
	s := l.tables[k]
	if s == nil {
		s = &tableState{sortCol: -1}
		l.tables[k] = s
	}
	return s
}

// dtView is computeView: the page's rows and what the footer shows.
type dtView struct {
	rows     [][]richtext.DTCell
	page     int // pageIndex
	pages    int // totalPages
	empty    bool
	spacer   int // rows the spacer stands in for
	footer   bool
	paginate bool
}

// pageSize is the table's page_size as the state uses it: under 1 (which
// Slack's schema refuses) a page is one row.
func pageSize(t *richtext.DataTable) float64 {
	if t.PageSize < 1 || math.IsInf(t.PageSize, 0) {
		return max(1, min(t.PageSize, 1e9))
	}
	return t.PageSize
}

func computeDTView(t *richtext.DataTable, s *tableState, all bool) dtView {
	rows := t.Body
	if s.filter != "" {
		rows = filterDTRows(rows, s.filter)
	}
	if s.sortCol >= 0 {
		rows = sortDTRows(rows, s.sortCol, s.desc)
	}
	var v dtView
	if all {
		v.rows = rows
		v.empty = len(rows) == 0 && s.filter != ""
		return v
	}
	ps := pageSize(t)
	v.pages = int(math.Ceil(float64(len(rows)) / ps))
	v.page = min(s.page, max(0, v.pages-1))
	start := float64(v.page) * ps
	end := min(start+ps, float64(len(rows)))
	if a, b := int(start), int(end); a < b {
		v.rows = rows[a:b]
	}
	v.empty = len(v.rows) == 0 && s.filter != ""
	want := int(min(float64(len(t.Body)), ps))
	v.spacer = want - len(v.rows)
	v.footer = float64(len(t.Body)) > ps || t.Salesforce
	v.paginate = !v.empty && v.pages > 1
	return v
}

// sortDTRows is sortRows: a column of raw_number cells (and empty ones)
// by value, else by the cells' text upper-cased, empty ones last; stable.
func sortDTRows(rows [][]richtext.DTCell, col int, desc bool) [][]richtext.DTCell {
	cellAt := func(r []richtext.DTCell) *richtext.DTCell {
		if col < len(r) {
			return &r[col]
		}
		return nil
	}
	numeric := true
	for _, r := range rows {
		c := cellAt(r)
		if c != nil && c.Kind == richtext.DTNumber {
			continue
		}
		if c != nil && c.Sort != "" {
			numeric = false
			break
		}
	}
	out := slices.Clone(rows)
	sort.SliceStable(out, func(i, j int) bool {
		return dtCompare(cellAt(out[i]), cellAt(out[j]), numeric, desc) < 0
	})
	return out
}

func dtCompare(a, b *richtext.DTCell, numeric, desc bool) float64 {
	if numeric && a != nil && b != nil && a.Kind == richtext.DTNumber && b.Kind == richtext.DTNumber {
		x, y := math.NaN(), math.NaN()
		if a.NumberOK {
			x = a.Number
		}
		if b.NumberOK {
			y = b.Number
		}
		d := x - y
		if desc {
			d = -d
		}
		if math.IsNaN(d) {
			return 0 // a NaN comparison is +0
		}
		return d
	}
	o, d := "", ""
	if a != nil {
		o = a.Sort
	}
	if b != nil {
		d = b.Sort
	}
	switch {
	case o == "" && d != "":
		return 1
	case o != "" && d == "":
		return -1
	case o == "" && d == "":
		return 0
	}
	c := utf16Compare(strings.ToUpper(o), strings.ToUpper(d))
	if desc {
		c = -c
	}
	return float64(c)
}

// utf16Compare orders strings by their UTF-16 code units, as JavaScript's
// < and > do.
func utf16Compare(a, b string) int {
	x, y := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			if x[i] < y[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(x) < len(y):
		return -1
	case len(x) > len(y):
		return 1
	}
	return 0
}

// filterDTRows is filterRows: the rows with a cell whose text Slack's
// FuzzyMatcher (fuzzyLimit 3) scores 3 or less for s.
func filterDTRows(rows [][]richtext.DTCell, s string) [][]richtext.DTCell {
	m := acrank.NewMatcher(s, acrank.MatcherOpts{FuzzyLimit: 3})
	var out [][]richtext.DTCell
	for _, r := range rows {
		for i := range r {
			if r[i].Sort != "" && m.NormalizeAndScore(r[i].Sort) <= 3 {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// ---- layout ----

// dataTable lays out a data_table block.
func (l *lay) dataTable(b *richtext.Block, x, y, w int) int {
	dt := b.DataTable
	if dt == nil {
		return 0
	}
	if dt.Err {
		if l.opts.FillBlocks {
			return 0 // a view passes no errorFallback
		}
		return l.topLevelError(x, y, w)
	}
	st := l.tableState(dt)
	vw := computeDTView(dt, st, dt.Full)
	ww := w
	if !l.opts.FillBlocks && !dt.Full {
		ww = min(w, l.dp(800))
	}
	ctl := &TableControl{Table: dt, st: st, Elements: l.opts.Elements}
	yy := y
	if !dt.Full {
		yy += l.dtHeader(dt, ctl, x, yy, ww)
	}
	yy += l.dtContainer(dt, ctl, &vw, x, yy, ww)
	return yy - y
}

// dtHeader is .dataTableBlockHeader.
func (l *lay) dtHeader(dt *richtext.DataTable, ctl *TableControl, x, y, w int) int {
	padX, padY, gap, bs := l.dp(4), l.dp(8), l.dp(8), l.dp(30)
	lineH := l.dp(22)
	h := 2*padY + max(bs, lineH)
	label := dt.Label
	if label == "" {
		label = nsBlockKit.T("table")
	}
	bx := x + w - padX
	by := y + padY + (h-2*padY-bs)/2
	_ = label // the buttons' aria-labels: "View {caption}", "More actions", "Search table"
	for _, a := range []struct{ id, icon string }{
		{"more", "kebab"},      // Slack's ellipsis-vertical-filled
		{"view", "fullscreen"}, // Slack's arrow-expand-full
		{"search", "search"},
	} {
		bx -= bs
		l.dtIconButton(ctl, a.id, a.icon, image.Rect(bx, by, bx+bs, by+bs))
		bx -= gap
	}
	if dt.Caption != "" {
		fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Black}, size: l.sp(15), lineH: lineH,
			col: l.tok("content-pry"), emoji: l.dp(22), maxLines: 1, ellipsis: true}
		l.plainAdd("\n")
		cw := bx - (x + padX)
		if cw > 0 {
			l.plainFlow(dt.Caption, x+padX, y+padY+(h-2*padY-lineH)/2, cw, &fs, -1)
		}
	}
	return h
}

// dtIconButton is a header button: a small IconButton in its 1px otl-sec
// frame.
func (l *lay) dtIconButton(ctl *TableControl, id, icon string, r image.Rectangle) {
	bw := max(1, l.dp(1))
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: l.dp(8), stroke: l.tok("otl-sec"), bw: bw, target: -1})
	in := r.Inset(bw)
	tg := l.d.addTarget(target{kind: tDataTable, id: id, table: ctl})
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: in, radius: l.dp(8) - bw, target: tg, hover: l.tok("base-ter")})
	l.d.hits = append(l.d.hits, hit{r: in, t: tg})
	gs := l.dp(16)
	at := image.Pt(in.Min.X+(in.Dx()-gs)/2, in.Min.Y+(in.Dy()-gs)/2)
	gr := image.Rectangle{Min: at, Max: at.Add(image.Pt(gs, gs))}
	l.d.imgs = append(l.d.imgs, imgPrim{r: gr, url: iconURL + icon, target: -1, px: gs, tint: l.tok("content-sec")})
	// :hover's content-pry glyph
	k := l.newHover()
	nb, ni, nf := l.hoverFrom()
	l.d.imgs = append(l.d.imgs, imgPrim{r: gr, url: iconURL + icon, target: -1, px: gs, tint: l.tok("content-pry")})
	l.hoverEnd(k, nb, ni, nf)
	l.d.hovers[k-1].card, l.d.hovers[k-1].cardTo = tg, tg
}

// dtContainer is .dataTableBlockContainer: the header row, the page's
// rows, the spacer and the footer.
func (l *lay) dtContainer(dt *richtext.DataTable, ctl *TableControl, vw *dtView, x, y, w int) int {
	bw := max(1, l.dp(1))
	pad := l.dp(4)
	full := dt.Full
	if full {
		pad = 0 // .dataTableBlockVirtualizedContainer: no padding, no fill
	}
	box0 := len(l.d.boxes)
	inner := w - 2*pad
	n := dt.Cols
	tx := x + pad
	colX := func(i int) int {
		if n <= 0 {
			return tx
		}
		return tx + i*inner/n
	}
	yy := y
	sortCol, desc, sorted := ctl.SortState()
	if dt.Header != nil && !vw.empty {
		hh := l.dp(36)
		fill := l.tok("surf-pry")
		if full { // .dataTableBlockVirtualizedContainer .dataTableBlockRowHeader
			l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: image.Rect(tx, yy, tx+inner, yy+hh), perCorner: true,
				corners: [4]int{l.dp(8), l.dp(8), 0, 0}, fill: fill, stroke: l.tok("otl-sec"), bw: bw, target: -1})
		}
		k := 0 // a header cell that isn't drawn leaves no grid item
		l.plainAdd("\n")
		for ci := range dt.Header {
			c := &dt.Header[ci]
			if !c.Present || !c.HeaderText {
				continue
			}
			if k > 0 {
				l.plainAdd("\t")
			}
			l.dtHeaderCell(ctl, ci, c.Text, sorted && sortCol == ci, desc, full, image.Rect(colX(k), yy, colX(k+1), yy+hh), fill)
			k++
		}
		yy += hh
	}
	rows := vw.rows
	rowH := l.dp(48)
	if vw.empty {
		yy += l.dtEmpty(tx, yy, inner, vw, dt)
	}
	if len(rows) > 0 {
		// the body's border (.dataTableBlockRowsContainer: the first and
		// last cells' and rows' outer borders, radius 8 at the corners),
		// as wide as its widest row
		ncell := 0
		for _, r := range rows {
			ncell = max(ncell, min(len(r), n))
		}
		bodyB := len(l.d.boxes)
		top := yy
		corners := [4]int{l.dp(8), l.dp(8), l.dp(8), l.dp(8)}
		if full { // under the header row's border, square at the top
			top -= bw
			corners[0], corners[1] = 0, 0
		}
		l.d.boxes = append(l.d.boxes, box{kind: bBorder, perCorner: true, corners: corners, fill: l.tok("base-pry"), stroke: l.tok("otl-sec"), bw: bw, target: -1})
		for ri, row := range rows {
			h := rowH
			if full {
				h = l.dtRowHeight(row, n, colX, rowH)
			}
			l.dtRow(row, ri, len(rows), n, colX, yy, h, full)
			yy += h
		}
		if ncell > 0 {
			l.d.boxes[bodyB].r = image.Rect(colX(0), top, colX(ncell), yy)
		}
	}
	if !vw.empty && vw.spacer > 0 && !full {
		yy += vw.spacer * rowH
	}
	if full {
		yy += pad
	} else if vw.footer {
		yy += l.dtFooter(ctl, vw, dt, tx, yy, inner)
	} else {
		yy += l.dp(4) // .dataTableBlockFooterCompact
	}
	if !full {
		bg := box{kind: bFill, r: image.Rect(x, y, x+w, yy), radius: l.dp(12), fill: l.tok("surf-pry"), target: -1}
		l.d.boxes = append(l.d.boxes, box{})
		copy(l.d.boxes[box0+1:], l.d.boxes[box0:])
		l.d.boxes[box0] = bg
	}
	return yy - y
}

// dtHeaderCell is DataTableBlockHeaderCell: the sortable button that opens
// the column's Sort menu.
func (l *lay) dtHeaderCell(ctl *TableControl, col int, text string, sorted, desc, full bool, r image.Rectangle, fill color.NRGBA) {
	bh := l.dp(28)
	br := image.Rect(r.Min.X, r.Min.Y+(r.Dy()-bh)/2, r.Max.X, r.Min.Y+(r.Dy()-bh)/2+bh)
	tg := l.d.addTarget(target{kind: tDataTable, id: "sort", col: col, table: ctl})
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: br, radius: l.dp(8), target: tg, hover: fill})
	l.d.hits = append(l.d.hits, hit{r: br, t: tg})
	padX, gap, gs := l.dp(12), l.dp(4), l.dp(16)
	col2 := l.tok("content-sec")
	// the caret (margin-left auto) and the sort's arrow take their room
	right := br.Max.X - padX - gs
	textR := right - gap
	if sorted {
		textR -= gs + gap
	}
	fs := flowStyle{face: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: l.sp(13), lineH: l.dp(18), col: col2,
		emoji: l.dp(18), maxLines: 1, ellipsis: !full}
	if full {
		fs.maxLines = 0
	}
	tx := br.Min.X + padX
	_, tw := l.plainFlow(text, tx, br.Min.Y+(bh-l.dp(18))/2, max(textR-tx, 1), &fs, -1)
	gy := br.Min.Y + (bh-gs)/2
	if sorted {
		icon := "arrow-up"
		if desc {
			icon = "arrow-down"
		}
		ax := tx + min(tw, max(textR-tx, 0)) + gap
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(ax, gy, ax+gs, gy+gs), url: iconURL + icon, target: -1, px: gs, tint: col2})
	}
	// .dataTableBlockCellHeaderCaret: opacity 0 until the button's :hover
	k := l.newHover()
	nb, ni, nf := l.hoverFrom()
	l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(right, gy, right+gs, gy+gs), url: iconURL + "chevron-down", target: -1, px: gs, tint: col2}) // Slack's caret-down
	l.hoverEnd(k, nb, ni, nf)
	l.d.hovers[k-1].card, l.d.hovers[k-1].cardTo = tg, tg
}

// dtRowHeight is a fullscreen row's height: its tallest cell (height
// auto, at least 48px).
func (l *lay) dtRowHeight(row []richtext.DTCell, n int, colX func(int) int, minH int) int {
	h := minH
	padX := l.dp(12)
	for ci := 0; ci < len(row) && ci < n; ci++ {
		cw := colX(ci+1) - colX(ci) - 2*padX
		if ch := l.dtMeasureCell(&row[ci], cw) + 2*l.dp(13); ch > h {
			h = ch
		}
	}
	return h
}

// dtMeasureCell is a wrapped cell's content height, laid out on a scratch
// display: nothing of it (frags, element slots, targets, images) stays.
func (l *lay) dtMeasureCell(c *richtext.DTCell, w int) int {
	saved := l.d
	l.d = &display{}
	h := l.dtCellContent(c, 0, 0, max(w, 1), true)
	l.d = saved
	return h
}

// dtRow is DataTableBlockRow: its cells in the body's border, base-sec
// while the row is under the pointer.
func (l *lay) dtRow(row []richtext.DTCell, ri, nrows, n int, colX func(int) int, y, h int, full bool) {
	bw := max(1, l.dp(1))
	rad := l.dp(8)
	first, last := ri == 0 && !full, ri == nrows-1
	ncell := min(len(row), n)
	if ncell == 0 {
		return
	}
	x0, x1 := colX(0), colX(ncell)
	rowR := image.Rect(x0, y, x1, y+h)
	tg := l.d.addTarget(target{kind: tDataTable, id: "row", menuOnly: true})
	l.d.hits = append(l.d.hits, hit{r: rowR, t: tg})
	if ri > 0 || full { // a row under another: inset 0 1px 0 0 otl-ter (the virtualized body's border-top)
		if ri > 0 {
			l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(x0+bw, y, x1-bw, y+bw), fill: l.tok("otl-ter"), target: -1})
		}
	}
	// the hover: the cells' background under the pointer, over their fill
	// and under their text (hoverOver), inside the borders
	hov := image.Rect(x0+bw, y, x1-bw, y+h)
	if first || ri > 0 {
		hov.Min.Y = y + bw
	}
	if last {
		hov.Max.Y = y + h - bw
	}
	var corners [4]int
	if first {
		corners[0], corners[1] = rad-bw, rad-bw
	}
	if last {
		corners[2], corners[3] = rad-bw, rad-bw
	}
	hb := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bFill, r: hov, perCorner: true, corners: corners, target: tg, hover: l.tok("base-sec"), hoverOver: true})
	padX := l.dp(12)
	l.plainAdd("\n")
	for ci := 0; ci < ncell; ci++ {
		c := &row[ci]
		if ci > 0 {
			l.plainAdd("\t")
		}
		if !c.Present {
			continue
		}
		cx, cw := colX(ci)+padX, colX(ci+1)-colX(ci)-2*padX
		if full {
			l.dtCellContent(c, cx, y+l.dp(13), max(cw, 1), true)
			continue
		}
		ch := l.dtCellHeight(c)
		l.dtCellContent(c, cx, y+(h-ch)/2, max(cw, 1), false)
	}
	l.d.boxes[hb].tgTo = int16(len(l.d.targets) - 1)
}

// dtCellHeight is a clamped cell's content height: one 22px line (an
// element's own height for an action cell).
func (l *lay) dtCellHeight(c *richtext.DTCell) int {
	if c.Kind == richtext.DTAction && c.Button != nil {
		if el := l.opts.Elements; el != nil {
			sz, _ := el.ElementsSize([]richtext.Button{*c.Button}, l.dp(400), false, l.metric())
			return sz.Y
		}
		return l.dp(28)
	}
	return l.dp(22)
}

// dtCellContent is renderCellContent at (x, y): one line with an ellipsis
// (wrap false), or wrapped (the fullscreen view). It returns its height.
func (l *lay) dtCellContent(c *richtext.DTCell, x, y, w int, wrap bool) int {
	fs := l.bodyStyle()
	fs.size, fs.lineH = l.sp(15), l.dp(22)
	fs.col = l.tok("content-pry")
	if !wrap {
		fs.maxLines, fs.ellipsis = 1, true
	}
	switch c.Kind {
	case richtext.DTText, richtext.DTNumber:
		if c.Text == "" {
			return 0
		}
		h, _ := l.plainFlow(c.Text, x, y, w, &fs, -1)
		return h
	case richtext.DTRich:
		if len(c.Spans) == 0 {
			return 0
		}
		l.addSpans(c.Spans, &fs)
		h, _ := l.flow(x, y, w, &fs)
		return h
	case richtext.DTAction:
		if c.Button == nil {
			return 0
		}
		bs := []richtext.Button{*c.Button}
		if el := l.opts.Elements; el != nil {
			sz, _ := el.ElementsSize(bs, w, false, l.metric())
			l.addSlot(bs, image.Rect(x, y, x+min(sz.X, w), y+sz.Y), false)
			return sz.Y
		}
		return l.buttons(bs, x, y, w)
	}
	return 0
}

// dtEmpty is the filter's empty state: "No results found." over Clear
// filter, as tall as the header row and the page (36px + the rows).
func (l *lay) dtEmpty(x, y, w int, vw *dtView, dt *richtext.DataTable) int {
	n := int(min(float64(len(dt.Body)), pageSize(dt)))
	h := l.dp(36) + n*l.dp(48)
	fs := l.bodyStyle()
	fs.size, fs.lineH = l.sp(15), l.dp(22)
	fs.col = l.tok("content-sec")
	top := y + l.dp(32)
	l.plainAdd("\n")
	th := l.centredFlow(nsBlockKit.T("No results found."), x+l.dp(16), top, w-2*l.dp(16), &fs)
	fs.col = l.tok("content-hgl-1")
	fs.face.Weight = font.Bold
	l.plainAdd("\n")
	l.centredFlow(nsBlockKit.T("Clear filter"), x+l.dp(16), top+th+l.dp(4), w-2*l.dp(16), &fs)
	return h
}

// dtFooter is .dataTableBlockFooter with DataTableBlockPagination.
func (l *lay) dtFooter(ctl *TableControl, vw *dtView, dt *richtext.DataTable, x, y, w int) int {
	h := l.dp(32)
	gap := l.dp(8)
	page, total := vw.page+1, vw.pages
	if vw.empty {
		page, total = 1, 0
	}
	// the Salesforce label, then the pagination
	sfW := 0
	fs := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: l.dp(18), col: l.tok("content-sec"), emoji: l.dp(18)}
	k := l.faceFor(fs.face, fs.size, 0)
	sfText := nsBlockKit.T("Synced from Salesforce")
	if dt.Salesforce {
		sfW = l.dp(16) + gap + int(math.Ceil(float64(l.measure(sfText, k))))
	}
	type pbtn struct {
		id    string
		label string
		w     int
		cur   bool
		on    bool
	}
	var btns []pbtn
	pw := 0
	if vw.paginate || dt.Salesforce || vw.footer {
		s := l.dp(20)
		btns = append(btns, pbtn{id: "prev", w: s, on: page > 1})
		for _, p := range pageRange(page, total) {
			lb := strconv.Itoa(p)
			bw := max(s, int(math.Ceil(float64(l.measure(lb, k))))+2*l.dp(6))
			btns = append(btns, pbtn{id: "page:" + lb, label: lb, w: bw, cur: p == page, on: true})
		}
		btns = append(btns, pbtn{id: "next", w: s, on: page < total})
		pw = 2 * l.dp(8)
		for i, b := range btns {
			if i > 0 {
				pw += l.dp(4)
			}
			pw += b.w
		}
	}
	// .dataTableBlockPaginationHidden is visibility: hidden on the whole
	// footer: with one page neither the pagination nor the Salesforce
	// label shows, the 32px stay
	shown := vw.paginate
	if !shown {
		return h
	}
	tw := pw
	if sfW > 0 {
		tw += sfW + gap
	}
	cx := x + (w-tw)/2
	if dt.Salesforce {
		gs := l.dp(16)
		gy := y + (h-gs)/2
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(cx, gy, cx+gs, gy+gs), url: iconURL + "cloud", target: -1, px: gs, tint: l.tok("plt-lagoon-50")}) // Slack's sf-cloud-filled (Clack has the outline)
		l.plainAdd("\n")
		l.plainFlow(sfText, cx+gs+gap, y+(h-l.dp(18))/2, sfW-gs-gap+l.dp(1), &fs, -1)
		cx += sfW + gap
	}
	bx := cx + l.dp(8)
	s := l.dp(20)
	by := y + (h-s)/2
	for i, b := range btns {
		if i > 0 {
			bx += l.dp(4)
		}
		r := image.Rect(bx, by, bx+b.w, by+s)
		switch {
		case b.id == "prev" || b.id == "next":
			icon := "chevron-left"
			if b.id == "next" {
				icon = "chevron-right"
			}
			col := l.tok("content-sec")
			tg := int16(-1)
			if b.on {
				tg = l.d.addTarget(target{kind: tDataTable, id: b.id, table: ctl})
				l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: s / 2, target: tg, hover: l.tok("base-ter")})
				l.d.hits = append(l.d.hits, hit{r: r, t: tg})
			} else {
				col.A /= 2 // aria-disabled
			}
			gs := l.dp(18)
			gr := image.Rect(r.Min.X+(s-gs)/2, r.Min.Y+(s-gs)/2, r.Min.X+(s-gs)/2+gs, r.Min.Y+(s-gs)/2+gs)
			l.d.imgs = append(l.d.imgs, imgPrim{r: gr, url: iconURL + icon, target: -1, px: gs, tint: col})
		default:
			tg := l.d.addTarget(target{kind: tDataTable, id: b.id, table: ctl})
			l.d.hits = append(l.d.hits, hit{r: r, t: tg})
			pfs := fs
			pfs.noSelect = true
			if b.cur {
				l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(4), fill: l.tok("base-inv-hgl-2"), target: -1})
				pfs.col = l.tok("content-inv-pry")
			} else {
				l.d.boxes = append(l.d.boxes, box{kind: bFill, r: r, radius: l.dp(4), target: tg, hover: l.tok("base-sec")})
			}
			l.centredFlow(b.label, r.Min.X, r.Min.Y+(s-l.dp(18))/2, r.Dx(), &pfs)
			if !b.cur { // :hover's content-pry
				k := l.newHover()
				nb, ni, nf := l.hoverFrom()
				pfs.col = l.tok("content-pry")
				l.centredFlow(b.label, r.Min.X, r.Min.Y+(s-l.dp(18))/2, r.Dx(), &pfs)
				l.hoverEnd(k, nb, ni, nf)
				l.d.hovers[k-1].card, l.d.hovers[k-1].cardTo = tg, tg
			}
		}
		bx += b.w
	}
	return h
}

// pageRange is getCurrentPageRange with rangeSize 5: up to five pages
// with the current one in the middle where it can be.
func pageRange(page, total int) []int {
	const size = 5
	if page <= 0 || total <= 0 {
		return nil
	}
	start := max(min(page-size/2, total-(size-1)), 1)
	end := min(start+size, total+1)
	out := make([]int, 0, size)
	for p := start; p < end; p++ {
		out = append(out, p)
	}
	return out
}

// dataTableClick is a click on a data_table's target.
func (v *View) dataTableClick(tg *target, h Handler) {
	ctl := tg.table
	if ctl == nil || ctl.st == nil {
		return
	}
	s := ctl.st
	switch {
	case tg.id == "prev":
		if s.page > 0 {
			s.page--
		}
	case tg.id == "next":
		s.page++
	case strings.HasPrefix(tg.id, "page:"):
		if p, err := strconv.Atoi(strings.TrimPrefix(tg.id, "page:")); err == nil && p >= 1 {
			s.page = p - 1
		}
	default:
		if dh, ok := h.(DataTableHandler); ok {
			r, _ := v.targetRect(tg)
			c := *ctl
			c.Column = tg.col
			dh.DataTableAction(tg.id, &c, r.Add(v.Origin))
		}
		return
	}
	v.dirty, v.redraw = true, true
}

// ---- test API ----

// DataTableRect is a data_table's parts, view-local.
type DataTableRect struct {
	Block   *richtext.DataTable
	Buttons map[string]image.Rectangle // "search", "view", "more", "prev", "next", "page:N", "sort:C"
	Rows    []image.Rectangle          // the shown rows
}

// DataTableRects are the laid-out data_tables' buttons and rows.
func (v *View) DataTableRects() []DataTableRect {
	var out []DataTableRect
	idx := map[*richtext.DataTable]int{}
	for _, h := range v.d.hits {
		if h.t < 0 || int(h.t) >= len(v.d.targets) {
			continue
		}
		tg := &v.d.targets[h.t]
		if tg.kind != tDataTable {
			continue
		}
		var t *richtext.DataTable
		if tg.table != nil {
			t = tg.table.Table
		}
		i, ok := idx[t]
		if !ok && t != nil {
			i = len(out)
			idx[t] = i
			out = append(out, DataTableRect{Block: t, Buttons: map[string]image.Rectangle{}})
		}
		if t == nil {
			if len(out) == 0 {
				continue
			}
			i = len(out) - 1
		}
		switch {
		case tg.id == "row":
			out[i].Rows = append(out[i].Rows, h.r)
		case tg.id == "sort":
			out[i].Buttons["sort:"+strconv.Itoa(tg.col)] = h.r
		default:
			out[i].Buttons[tg.id] = h.r
		}
	}
	return out
}

// DataTablePage is the shown page (0-based) of the View's data_table t
// and its page count.
func (v *View) DataTablePage(t *richtext.DataTable) (page, pages int) {
	s := v.tables[tableKey(t)]
	if s == nil {
		s = &tableState{sortCol: -1}
	}
	vw := computeDTView(t, s, t.Full)
	return vw.page, vw.pages
}

// DataTableRows are the shown rows' first cells' text (a test's check of
// the page and the sort).
func (v *View) DataTableRows(t *richtext.DataTable) []string {
	s := v.tables[tableKey(t)]
	if s == nil {
		s = &tableState{sortCol: -1}
	}
	vw := computeDTView(t, s, t.Full)
	out := make([]string, 0, len(vw.rows))
	for _, r := range vw.rows {
		if len(r) > 0 {
			out = append(out, r[0].Sort)
		} else {
			out = append(out, "")
		}
	}
	return out
}

// HoveredTableTarget is the data_table target under the pointer ("next",
// "sort", "row"; "" none), for tests.
func (v *View) HoveredTableTarget() string {
	if !v.hovering || v.hover < 0 || int(v.hover) >= len(v.d.targets) {
		return ""
	}
	if tg := &v.d.targets[v.hover]; tg.kind == tDataTable {
		return tg.id
	}
	return ""
}
