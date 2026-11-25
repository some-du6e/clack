package canvasview

// EmbeddedSlackList (client-boot-shared p/yH) → EmbeddedSlackListStatic
// (es5B) → ListEntity expanded (fm8k, r7Sl), the card Slack draws in a
// canvas for a list or a list item (a /lists/T…/F… link, its view_id or
// record_id): the list's header over a seven-row preview of its first two
// columns, or the item's fields, in the 799×400 box (at least 235 tall,
// what's below clipped). The list is files.info and lists.records.list
// (core.List, no lists.open, no subscription); nothing in the card moves,
// so it asks for no frames of its own. Build 889ac449a32d; the notes are
// in /root/clack-ops/scratch/link-url-box-3/cards-spec.md §2.

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"
	"unicode"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/listview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var nsLists = i18n.NS("lists")

// listState is a list the view fetched for its list cards (core.List).
type listState struct {
	done  bool
	doc   *core.ListDoc
	code  string // file_deleted, not_found: files.info's refusal, kept
	tries int    // failures that said nothing about the list
	retry time.Time
}

// listCardData is what ListEntity reads, worked out from the list.
type listCardData struct {
	doc  *core.ListDoc
	code string // the list's refusal (file_deleted, not_found), or view_deleted
	// a list item link whose item the list doesn't hold: Slack's record
	// is isNonExistent, and ListEntity stays loading
	noRecord bool
	rec      *lists.Record
	view     *lists.View
	todo     bool
	cols     []*lists.Column // the preview's columns (todo_completed first in a to-do list)
	rows     []lists.Row     // the records shown
	fields   []fieldOf       // an item's fields
	primary  string
	second   string
	done     bool // a to-do item that's completed
}

type fieldOf struct {
	c *lists.Column
	f *lists.Field
}

// readList reads the route's list for its card; it reports whether to
// fetch the list.
func (v *View) readList(c *linkCard) bool {
	c.ls = listCardData{}
	id := c.rt.fileID
	if c.rt.kind != "list" || id == "" {
		return false
	}
	a := v.h.Core()
	if a == nil {
		return false
	}
	if doc := a.CachedList(id); doc != nil {
		v.listDerive(c, doc)
		return false
	}
	if v.lists == nil {
		v.lists = map[string]*listState{}
	}
	st := v.lists[id]
	switch {
	case st == nil:
		v.lists[id] = &listState{}
		return true
	case st.done && st.code != "":
		c.ls.code = st.code
	case st.done:
		v.listDerive(c, st.doc)
	case st.tries > 0 && !time.Now().Before(st.retry): // a failure that said nothing: ask again
		st.retry = time.Now().Add(time.Hour)
		return true
	}
	return false
}

// fetchList loads list id (files.info and its records), then refreshes
// the cards.
func (v *View) fetchList(id string) {
	a := v.h.Core()
	if a == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		doc, err := a.List(ctx, id)
		code := ""
		switch {
		case err == nil:
		case core.IsAPIError(err, "file_deleted"):
			code = "file_deleted"
		case core.IsFileGone(err):
			code = "not_found" // file_not_found, or a file that isn't a list
		}
		v.post(func() {
			st := v.lists[id]
			if st == nil {
				return
			}
			if err == nil || code != "" {
				st.done, st.doc, st.code = true, doc, code
				v.refreshCards()
				return
			}
			st.tries++
			wait := fileRetry(st.tries)
			st.retry = time.Now().Add(wait)
			go func() {
				time.Sleep(wait)
				v.post(v.refreshCards)
			}()
		})
	}()
}

// listDerive is useListEntityLogic over the list: the view, the item, the
// columns, the records shown and the labels.
func (v *View) listDerive(c *linkCard, doc *core.ListDoc) {
	d := &c.ls
	d.doc = doc
	m := &doc.Meta
	d.todo = m.TodoMode
	title := doc.Info.Title
	if c.rt.viewID != "" {
		if d.view = m.View(c.rt.viewID); d.view == nil {
			d.code = "view_deleted" // listViewIsDeleted
			return
		}
	}
	if c.rt.recordID != "" {
		if d.rec = doc.Record(c.rt.recordID); d.rec == nil {
			d.noRecord = true
			return
		}
	}
	// eL: the view's visible columns, an item's record view's, else the
	// schema's, in order
	var keys []string
	switch {
	case d.view != nil:
		for _, vc := range d.view.Columns {
			if vc.Visible {
				keys = append(keys, vc.Key)
			}
		}
	case d.rec != nil:
		for i := range m.Views {
			if m.Views[i].Type == lists.ViewRecord && len(m.Views[i].Columns) > 0 {
				for _, vc := range m.Views[i].Columns {
					keys = append(keys, vc.Key)
				}
				break
			}
		}
	}
	if keys == nil && d.view == nil {
		for _, col := range m.Schema {
			keys = append(keys, col.Key)
		}
	}
	if d.rec != nil {
		v.listRecord(c, keys, title)
		return
	}
	// a list: its first two columns (a to-do list's leave out the
	// completed and reference columns, then lead with completed)
	for _, k := range keys {
		col := m.Column(k)
		if col == nil || d.todo && (k == lists.TodoCompleted || col.Type == "reference") {
			continue
		}
		d.cols = append(d.cols, col)
		if len(d.cols) == 2 {
			break
		}
	}
	if d.todo {
		if col := m.Column(lists.TodoCompleted); col != nil {
			d.cols = append([]*lists.Column{col}, d.cols...)
		}
	}
	// the records: top-level ones in the view's (or All items') order,
	// filters and sorts; with a view, in its groups' order, seven; else five
	lv := d.view
	if lv == nil {
		for i := range m.Views {
			if m.Views[i].IsAllItems {
				lv = &m.Views[i]
				break
			}
		}
	}
	var top []lists.Record
	for _, r := range doc.Records {
		if r.ParentRecordID == "" && !r.Archived {
			top = append(top, r)
		}
	}
	rows := lists.Rows(m, top)
	vid := ""
	if lv != nil {
		vid = lv.ID
	}
	lists.DefaultOrder(rows, vid)
	rows = lists.Apply(m, lv, rows, "", v.listEnv(doc))
	if d.view != nil {
		if col := m.Column(d.view.GroupBy()); col != nil {
			var grouped []lists.Row
			for _, g := range lists.GroupRows(col, rows) {
				grouped = append(grouped, g.Rows...)
			}
			rows = grouped
		}
		d.rows = rows[:min(7, len(rows))]
	} else {
		d.rows = rows[:min(5, len(rows))]
	}
	d.primary = title
	d.second = nsLists.T("List")
	if d.view != nil {
		d.second = nsLists.T("{listViewName} - {viewType} view", "listViewName", d.view.Name, "viewType", capitalise(d.view.Type))
	}
}

// listRecord is an item's: its primary field as the label, its other
// non-empty fields in the columns' order, seven.
func (v *View) listRecord(c *linkCard, keys []string, title string) {
	d := &c.ls
	m := &d.doc.Meta
	order := map[string]int{}
	for i, k := range keys {
		order[k] = i
	}
	for i := range d.rec.Fields {
		f := &d.rec.Fields[i]
		col := m.Column(f.Key)
		if col == nil || f.Key == "name" || f.Key == lists.TodoCompleted {
			continue
		}
		if fl := lists.Flatten(f); fl.Empty && col.Type != lists.Vote {
			continue
		}
		if _, ok := order[f.Key]; !ok && len(keys) > 0 {
			continue
		}
		d.fields = append(d.fields, fieldOf{col, f})
	}
	for i := 1; i < len(d.fields); i++ { // a stable insertion sort by eL's order
		for j := i; j > 0 && order[d.fields[j].c.Key] < order[d.fields[j-1].c.Key]; j-- {
			d.fields[j], d.fields[j-1] = d.fields[j-1], d.fields[j]
		}
	}
	d.fields = d.fields[:min(7, len(d.fields))]
	if p := m.Primary(); p != nil {
		if f := d.rec.Field(p.Key); f != nil {
			d.primary = strings.TrimSpace(lists.Flatten(f).S)
		}
	}
	if d.primary == "" {
		d.primary = nsLists.T("Untitled")
	}
	if d.rec.ParentRecordID != "" {
		d.second = nsLists.T("Subtask in {fileTitle}", "fileTitle", title)
	} else {
		d.second = nsLists.T("Item in {fileTitle}", "fileTitle", title)
	}
	if d.todo {
		if f := d.rec.Field(lists.TodoCompleted); f != nil && f.Checkbox != nil && *f.Checkbox {
			d.done = true
		}
	}
}

func capitalise(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}

// listEnv is the names the view's filters and sorts read (people,
// channels), as the list view's.
func (v *View) listEnv(d *core.ListDoc) lists.Env {
	env := lists.Env{TodoMode: d.Meta.TodoMode, UserNames: map[string]string{}, ChannelNames: map[string]string{}, Now: time.Now()}
	a := v.h.Core()
	if a == nil {
		return env
	}
	env.Self = a.Self().ID
	for _, r := range d.Records {
		for _, f := range r.Fields {
			for _, u := range f.User {
				if us, ok := a.User(u); ok {
					env.UserNames[u] = firstNonEmpty(us.RealName, us.Name)
				}
			}
			for _, ch := range f.Channel {
				if cv, ok := a.Conv(ch); ok {
					env.ChannelNames[ch] = cv.Name
				}
			}
		}
	}
	return env
}

// decideList is ListEntity's entity state for the card: loading until the
// list is in (and for an item the list doesn't hold), the error state for
// a list Slack refused or a view it doesn't have, else the entity.
func (v *View) decideList(c *linkCard) cardKind {
	d := &c.ls
	switch {
	case c.rt.fileID == "":
		return cardEmpty
	case d.code != "":
		return cardListError
	case d.doc == nil || d.noRecord:
		return cardListLoading
	}
	return cardList
}

// listInks are the card's colours (client-boot-styles, kit.css).
type listInk struct {
	gray0, gray5, gray10, gray70   color.NRGBA
	honey0, honey40, honey60       color.NRGBA
	rowBg, pry, ter, hgl2, footer  color.NRGBA
	labelFg, footerRule, emptyFill color.NRGBA
}

func listInks(dark bool) listInk {
	if dark {
		return listInk{gray0: rgb(16, 18, 20), gray5: rgb(26, 29, 33), gray10: rgb(33, 36, 40), gray70: rgb(185, 186, 189),
			honey0: rgb(54, 30, 0), honey40: rgb(170, 108, 0), honey60: rgb(205, 150, 37),
			rowBg: rgb(26, 29, 33), pry: rgb(0xf8, 0xf8, 0xf8), ter: rgb(0x9a, 0x9b, 0x9e), hgl2: rgb(0x3d, 0xaa, 0x7c),
			footer: rgb(34, 37, 41), labelFg: color.NRGBA{232, 232, 232, 179}, footerRule: color.NRGBA{0x5e, 0x5d, 0x60, 0x21}}
	}
	return listInk{gray0: rgb(248, 248, 248), gray5: rgb(234, 234, 234), gray10: rgb(215, 215, 218), gray70: rgb(69, 68, 71),
		honey0: rgb(252, 242, 217), honey40: rgb(225, 155, 6), honey60: rgb(192, 127, 0),
		rowBg: rgb(255, 255, 255), pry: rgb(0x1d, 0x1c, 0x1d), ter: rgb(0x5e, 0x5d, 0x60), hgl2: rgb(0x00, 0x7a, 0x5a),
		footer: rgb(248, 248, 248), labelFg: color.NRGBA{29, 28, 29, 179}, footerRule: color.NRGBA{0x5e, 0x5d, 0x60, 0x21}}
}

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{r, g, b, 255} }

// layoutList draws the list card w wide and returns its height: the
// AspectBox, max(w×400/799, 235), with the entity clipped to it.
func (v *View) layoutList(gtx layout.Context, c *linkCard, w int) int {
	box := max(w*400/799, gtx.Dp(235))
	defer clip.UniformRRect(image.Rect(0, 0, w, box), gtx.Dp(12)).Push(gtx.Ops).Pop()
	th := v.h.Theme()
	ink := listInks(th.Dark)
	switch c.kind {
	case cardListLoading:
		v.layoutListLoading(gtx, w)
	case cardListError:
		v.layoutListError(gtx, c, w, box, ink)
	case cardList:
		v.layoutListHeader(gtx, c, w, ink)
		y := gtx.Dp(60)
		paint.FillShape(gtx.Ops, v.styles.card.Palette.Border, clip.Rect(image.Rect(0, y, w, y+gtx.Dp(1))).Op()) // the preview's top border
		y += gtx.Dp(1)
		if c.ls.rec != nil {
			v.layoutListRecord(gtx, c, w, y, box, ink)
		} else {
			v.layoutListTable(gtx, c, w, y, ink)
		}
	}
	c.fileHit = image.Rectangle{}
	if c.kind == cardList { // FileLinkButton: the card opens the list, its view or the item
		c.fileHit = image.Rect(0, 0, w, box)
	}
	return box
}

// layoutListLoading is ListEntity's loading: static boxes (no shimmer).
func (v *View) layoutListLoading(gtx layout.Context, w int) {
	col := v.styles.card.Palette.Missing
	pad := gtx.Dp(12)
	theme.RRect(gtx, image.Rect(pad, pad, pad+gtx.Dp(36), pad+gtx.Dp(36)), gtx.Dp(8), col)
	x := pad + gtx.Dp(36) + pad
	bar := gtx.Dp(8)
	y := pad + (gtx.Dp(36)-2*bar-gtx.Dp(8))/2
	theme.RRect(gtx, image.Rect(x, y, x+gtx.Dp(93), y+bar), bar, col)
	y += bar + gtx.Dp(8)
	theme.RRect(gtx, image.Rect(x, y, x+gtx.Dp(131), y+bar), bar, col)
	top := gtx.Dp(60)
	paint.FillShape(gtx.Ops, col, clip.Rect(image.Rect(0, top, w, top+gtx.Dp(350))).Op())
}

// layoutListHeader is the entity's header: the 36px thumbnail (a list's
// lists-filled on honeycomb-40, an item's list-item on honeycomb-0), then
// the stacked labels, padded 12 with a 12 gap.
func (v *View) layoutListHeader(gtx layout.Context, c *linkCard, w int, ink listInk) {
	d := &c.ls
	th := v.h.Theme()
	pad, ic := gtx.Dp(12), gtx.Dp(36)
	bg, fg, glyph := ink.honey40, ink.honey0, "lists-filled"
	if d.rec != nil {
		bg, fg, glyph = ink.honey0, ink.honey60, "list-item"
	}
	theme.RRect(gtx, image.Rect(pad, pad, pad+ic, pad+ic), gtx.Dp(8), bg)
	func() {
		s := gtx.Dp(20)
		defer op.Offset(image.Pt(pad+(ic-s)/2, pad+(ic-s)/2)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(s, s))
		icons.GetSlack(glyph)(g, unit.Dp(20), fg)
	}()
	x := pad + ic + pad
	tw := max(0, w-x-pad)
	measure := func(s theme.TextStyle, txt string) (layout.Dimensions, op.CallOp) {
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(tw, gtx.Constraints.Max.Y)}
		dims := th.Layout(g, s, txt)
		return dims, m.Stop()
	}
	pDims, pCall := measure(theme.TextStyle{Size: 15, Weight: theme.Bold, Color: ink.pry, LineHeight: 22, MaxLines: 1}, d.primary)
	sDims, sCall := measure(theme.TextStyle{Size: 13, Weight: theme.Regular, Color: ink.ter, LineHeight: 18, MaxLines: 1}, d.second)
	gap := gtx.Dp(4)
	y := pad + (ic-pDims.Size.Y-gap-sDims.Size.Y)/2
	func() {
		defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
		pCall.Add(gtx.Ops)
	}()
	y += pDims.Size.Y + gap
	func() {
		defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
		sCall.Add(gtx.Ops)
	}()
}

// layoutListTable is ExpandedList's table preview: a gray-0 grid inset 10
// from the preview's top left (its right 10 clipped), a 40px header row
// of the column names, then seven 43px rows (the records, then empty
// ones), each 5 padded; the first cell 60% of the row and bold, the second
// 40% (a to-do list: a 32px completed cell first).
func (v *View) layoutListTable(gtx layout.Context, c *linkCard, w, top int, ink listInk) {
	d := &c.ls
	th := v.h.Theme()
	gx, gy := gtx.Dp(10), top+gtx.Dp(10)
	gw := w // width:100%, offset 10: the right 10 is clipped
	hdrH, rowH := gtx.Dp(40), gtx.Dp(43)
	gh := hdrH + 7*rowH
	theme.RRect(gtx, image.Rect(gx, gy, gx+gw, gy+gh), gtx.Dp(8), ink.gray0)
	rx, rw := gx+gtx.Dp(4), max(0, gw-2*gtx.Dp(4)) // the grid's padding 0 4px
	rp := gtx.Dp(5)
	cw := max(0, rw-2*rp)
	// the cells' widths
	var widths []int
	switch {
	case d.todo && len(d.cols) > 0:
		first := gtx.Dp(32)
		widths = append(widths, first)
		rest := d.cols[1:]
		switch len(rest) {
		case 1:
			widths = append(widths, min(cw-first, cw*6/10))
		case 2:
			third := cw * 4 / 10
			widths = append(widths, min(cw-first-third, cw*6/10), third)
		}
	case len(d.cols) == 1:
		widths = []int{cw * 6 / 10}
	case len(d.cols) == 2:
		widths = []int{cw * 6 / 10, cw * 4 / 10}
	}
	boldCol := 0
	if d.todo {
		boldCol = 1
	}
	cell := func(i, x, y, h int, draw func(g layout.Context, w, h int)) {
		cx := x + gtx.Dp(8)
		w := max(0, widths[i]-gtx.Dp(8))
		if d.todo && i == 0 { // the completed cell: centred, no padding
			cx, w = x, widths[i]
		}
		defer op.Offset(image.Pt(cx, y)).Push(gtx.Ops).Pop()
		defer clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		draw(g, w, h)
	}
	say := func(g layout.Context, h int, s theme.TextStyle, txt string) {
		m := op.Record(g.Ops)
		dims := th.Layout(g, s, txt)
		call := m.Stop()
		defer op.Offset(image.Pt(0, (h-dims.Size.Y)/2)).Push(g.Ops).Pop()
		call.Add(g.Ops)
	}
	// the header row: transparent, the column names in gray-70 13px
	x := rx + rp
	for i, col := range d.cols {
		cell(i, x, gy+rp, hdrH-2*rp, func(g layout.Context, w, h int) {
			if col.Key == lists.TodoCompleted {
				s := g.Dp(16)
				defer op.Offset(image.Pt((w-s)/2, (h-s)/2)).Push(g.Ops).Pop()
				g.Constraints = layout.Exact(image.Pt(s, s))
				icons.GetSlack("check-circle")(g, unit.Dp(16), ink.gray70)
				return
			}
			say(g, h, theme.TextStyle{Size: 13, Weight: theme.Regular, Color: ink.gray70, LineHeight: 18, MaxLines: 1}, col.Name)
		})
		x += widths[i]
	}
	// the rows: the records, then empty ones to seven
	host := listHost{v.h}
	for r := 0; r < 7; r++ {
		y := gy + hdrH + r*rowH
		rr := image.Rect(rx, y, rx+rw, y+rowH)
		v.listRowFrame(gtx, rr, r, ink)
		var row *lists.Row
		if r < len(d.rows) {
			row = &d.rows[r]
		}
		x := rx + rp
		for i, col := range d.cols {
			var f *lists.Field
			if row != nil {
				f = row.Rec.Field(col.Key)
			}
			empty := f == nil || lists.Flatten(f).Empty
			cell(i, x, y+rp, rowH-2*rp, func(g layout.Context, w, h int) {
				switch {
				case empty && col.Key != lists.TodoCompleted && col.IsPrimary:
					// BaseFieldEmptyHoverableValue's "Untitled item", at .25 in the first cell
					st := theme.TextStyle{Size: 15, Weight: theme.Regular, Color: ink.pry, LineHeight: 22, MaxLines: 1}
					if i == 0 || d.todo && i == 1 {
						st.Color.A = 64
					}
					say(g, h, st, nsLists.T("Untitled item"))
				case empty && col.Key != lists.TodoCompleted:
					// the column type's icon: not drawn (Slack's K2 map isn't ported)
				default:
					listview.DrawCell(g, host, col, f, w, h, i == boldCol)
				}
			})
			x += widths[i]
		}
	}
}

// listRowFrame is a data row's background and Slack's borders: a gray-10
// left border and a gray-5 top one; the first row's top in gray-10 with
// its top left and bottom right rounded 8, the last's bottom in gray-10
// with its top right and bottom left rounded 8; no right border.
func (v *View) listRowFrame(gtx layout.Context, r image.Rectangle, i int, ink listInk) {
	rad := gtx.Dp(8)
	rr := clip.RRect{Rect: r}
	switch i {
	case 0:
		rr.NW, rr.SE = rad, rad
	case 6:
		rr.NE, rr.SW = rad, rad
	}
	b := gtx.Dp(1)
	top := ink.gray5
	if i == 0 {
		top = ink.gray10
	}
	func() { // the borders, under the background inset by them
		defer rr.Push(gtx.Ops).Pop()
		paint.FillShape(gtx.Ops, ink.gray10, clip.Rect(r).Op())
		paint.FillShape(gtx.Ops, top, clip.Rect(image.Rect(r.Min.X+b, r.Min.Y, r.Max.X, r.Min.Y+b)).Op())
	}()
	in := image.Rect(r.Min.X+b, r.Min.Y+b, r.Max.X, r.Max.Y)
	if i == 6 {
		in.Max.Y -= b
	}
	ir := clip.RRect{Rect: in}
	switch i {
	case 0:
		ir.NW, ir.SE = max(0, rad-b), max(0, rad-b)
	case 6:
		ir.NE, ir.SW = max(0, rad-b), max(0, rad-b)
	}
	defer ir.Push(gtx.Ops).Pop()
	paint.FillShape(gtx.Ops, ink.rowBg, clip.Rect(in).Op())
}

// layoutListRecord is ExpandedListRecord: the item's fields padded 12, a
// 140px label (15px, foreground_max at .7) then the value, 14 apart; ":see
// no evil: There's nothing here" with none; a completed to-do item's
// footer at the bottom of the box.
func (v *View) layoutListRecord(gtx layout.Context, c *linkCard, w, top, box int, ink listInk) {
	d := &c.ls
	th := v.h.Theme()
	pad := gtx.Dp(12)
	host := listHost{v.h}
	if len(d.fields) == 0 {
		g := gtx
		g.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
		m := op.Record(gtx.Ops)
		dims := th.Layout(g, theme.TextStyle{Size: 15, Weight: theme.Regular, Color: ink.pry, LineHeight: 22, Align: text.Middle},
			"🙈 "+nsLists.T("There’s nothing here"))
		call := m.Stop()
		area := min(gtx.Dp(350), box-top)
		defer op.Offset(image.Pt(0, top+(area-dims.Size.Y)/2)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
		return
	}
	y := top + pad
	lw := gtx.Dp(140)
	vw := max(0, w-2*pad-lw-gtx.Dp(10))
	rowH := gtx.Dp(24)
	for _, fo := range d.fields {
		func() {
			defer op.Offset(image.Pt(pad, y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(lw, rowH)}
			m := op.Record(gtx.Ops)
			dims := th.Layout(g, theme.TextStyle{Size: 15, Weight: theme.Regular, Color: ink.labelFg, LineHeight: 18, MaxLines: 1}, fo.c.Name)
			call := m.Stop()
			func() {
				defer op.Offset(image.Pt(0, (rowH-dims.Size.Y)/2)).Push(gtx.Ops).Pop()
				call.Add(gtx.Ops)
			}()
			func() {
				defer op.Offset(image.Pt(lw+gtx.Dp(10), 0)).Push(gtx.Ops).Pop()
				g := gtx
				g.Constraints = layout.Exact(image.Pt(vw, rowH))
				listview.DrawCell(g, host, fo.c, fo.f, vw, rowH, false)
			}()
		}()
		y += rowH + gtx.Dp(14)
	}
	if d.done { // listRecordEntityExpandedFooter, completedFooter
		fh := gtx.Dp(28)
		fy := box - fh
		paint.FillShape(gtx.Ops, ink.footer, clip.Rect(image.Rect(0, fy, w, box)).Op())
		paint.FillShape(gtx.Ops, ink.footerRule, clip.Rect(image.Rect(0, fy, w, fy+gtx.Dp(1))).Op())
		s := gtx.Dp(16)
		func() {
			defer op.Offset(image.Pt(pad, fy+(fh-s)/2)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(s, s))
			icons.GetSlack("check-circle-filled")(g, unit.Dp(16), ink.hgl2)
		}()
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(max(0, w-2*pad-s-gtx.Dp(8)), fh)}
		m := op.Record(gtx.Ops)
		dims := th.Layout(g, theme.TextStyle{Size: 12, Weight: theme.Bold, Color: ink.pry, LineHeight: 18, MaxLines: 1}, nsLists.T("Completed item"))
		call := m.Stop()
		defer op.Offset(image.Pt(pad+s+gtx.Dp(8), fy+(fh-dims.Size.Y)/2)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}
}

// layoutListError is BaseListErrorState (isUnfurl: a 22px icon, the 18px
// Black title) centred in its 350px container, or ListRecordErrorState for
// an item of a list Slack refused ("🐢 Can’t view this item").
func (v *View) layoutListError(gtx layout.Context, c *linkCard, w, box int, ink listInk) {
	th := v.h.Theme()
	icon, title := "help", nsLists.T("This list was not found.")
	switch {
	case c.rt.recordID != "" && c.ls.code != "view_deleted":
		icon, title = "", "🐢 "+nsLists.T("Can’t view this item")
	case c.ls.code == "file_deleted":
		icon, title = "trash", nsLists.T("This list was deleted.")
	case c.ls.code == "view_deleted":
		icon, title = "trash", nsLists.T("This list view was deleted")
	}
	tw := max(0, w-2*gtx.Dp(16))
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, gtx.Constraints.Max.Y)}
	m := op.Record(gtx.Ops)
	dims := th.Layout(g, theme.TextStyle{Size: 18, Weight: theme.Black, Color: ink.pry, LineHeight: 24, Align: text.Middle}, title)
	call := m.Stop()
	is := 0
	if icon != "" {
		is = gtx.Dp(22)
	}
	area := min(gtx.Dp(350), box)
	y := max(0, (area-is-dims.Size.Y)/2)
	if icon != "" {
		func() {
			defer op.Offset(image.Pt((w-is)/2, y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(is, is))
			icons.GetSlack(icon)(g, unit.Dp(22), ink.pry)
		}()
		y += is
	}
	defer op.Offset(image.Pt(gtx.Dp(16), y)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

// listHost is canvasview's host as the list view's cells draw with.
type listHost struct{ Host }
