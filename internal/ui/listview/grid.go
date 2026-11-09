package listview

import (
	"image"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/lists"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// gridState is the table's scroll and hover state.
type gridState struct {
	scrollX, scrollY int
	hover            int // line under the pointer (-1 none)
	hoverX           int
	press            f32.Point
	pressed          bool
	tag              int
	lines            []gridLine // last frame's lines (hit testing)
	cols             []gridCol
	headH, x0        int
}

type lineKind uint8

const (
	lineRow lineKind = iota
	lineGroup
	lineAdd
	lineNew // the editor for a new item
)

type gridLine struct {
	kind  lineKind
	y, h  int
	row   lists.Row
	group string
	count int
}

type gridCol struct {
	c    *lists.Column
	x, w int // x relative to the grid's origin before scrolling
}

// Slack's grid metrics (p-grid_header__cell 32px + 6px margins, p-grid_cell
// 48px, p-add_row_button 44px).
const (
	headerCellH = 44
	rowH        = 48
	addRowH     = 44
	groupRowH   = 44
)

// layoutGrid draws a table view.
func (v *View) layoutGrid(gtx layout.Context, d *core.ListDoc, lv *lists.View, rows []lists.Row) layout.Dimensions {
	th := v.h.Theme()
	c := &th.C
	sz := gtx.Constraints.Max
	g := &v.grid
	cols := d.Meta.VisibleColumns(lv)
	g.x0 = v.pad(gtx) - gtx.Dp(12)
	g.cols = g.cols[:0]
	x := 0
	for _, col := range cols {
		w := gtx.Dp(unit.Dp(lists.ColumnWidth(lv, col)))
		g.cols = append(g.cols, gridCol{c: col, x: x, w: w})
		x += w
	}
	totalW := x
	// lines
	g.lines = g.lines[:0]
	y := 0
	g.headH = gtx.Dp(headerCellH)
	y += g.headH
	var groupCol *lists.Column
	if gb := lv.GroupBy(); gb != "" {
		groupCol = d.Meta.Column(gb)
	}
	addRows := func(rs []lists.Row, group string) {
		for _, r := range rs {
			g.lines = append(g.lines, gridLine{kind: lineRow, y: y, h: gtx.Dp(rowH), row: r, group: group})
			y += gtx.Dp(rowH)
		}
		if v.edit.active && v.edit.rec == "" && v.edit.group == group {
			g.lines = append(g.lines, gridLine{kind: lineNew, y: y, h: gtx.Dp(rowH), group: group})
			y += gtx.Dp(rowH)
		}
		if d.Info.Editable {
			g.lines = append(g.lines, gridLine{kind: lineAdd, y: y, h: gtx.Dp(addRowH), group: group})
			y += gtx.Dp(addRowH)
		}
	}
	if groupCol != nil {
		for _, gr := range lists.GroupRows(groupCol, rows) {
			if len(gr.Rows) == 0 && gr.Value == "" {
				continue
			}
			g.lines = append(g.lines, gridLine{kind: lineGroup, y: y, h: gtx.Dp(groupRowH), group: gr.Value, count: len(gr.Rows)})
			y += gtx.Dp(groupRowH)
			addRows(gr.Rows, gr.Value)
		}
	} else {
		addRows(rows, "")
	}
	totalH := y + gtx.Dp(24)
	// scrolling
	maxY := max(0, totalH-sz.Y)
	maxX := max(0, g.x0+totalW+gtx.Dp(32)-sz.X)
	v.gridEvents(gtx, d, maxX, maxY)
	g.scrollY = min(max(0, g.scrollY), maxY)
	g.scrollX = min(max(0, g.scrollX), maxX)

	firstW := 0
	if len(g.cols) > 0 {
		firstW = g.cols[0].w
	}
	// rows (behind the sticky header)
	for i := range g.lines {
		ln := &g.lines[i]
		ty := ln.y - g.scrollY
		if ty+ln.h < g.headH || ty > sz.Y {
			continue
		}
		v.layoutLine(gtx, d, lv, ln, i, ty, firstW, groupCol)
	}
	// the sticky header row
	theme.Fill(gtx, image.Rect(0, 0, sz.X, g.headH), c.Bg)
	hc := shade(th, "gray-70")
	for i, gc := range g.cols {
		gx := g.x0 + gc.x
		if i > 0 {
			gx -= g.scrollX
			if gx+gc.w <= g.x0+firstW {
				continue
			}
		}
		func() {
			cl := image.Rect(max(gx, g.x0+firstW), 0, gx+gc.w, g.headH)
			if i == 0 {
				cl.Min.X = gx
			}
			defer clip.Rect(cl).Push(gtx.Ops).Pop()
			nx := gx + gtx.Dp(10)
			if i == 0 {
				nx = gx + gtx.Dp(12)
			}
			if gc.c.Type == lists.TodoCompleted {
				at(gtx, image.Pt(gx+(gc.w-gtx.Dp(16))/2, (g.headH-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("check-circle")(gtx, 16, hc)
				})
				return
			}
			at(gtx, image.Pt(nx, 0), image.Pt(max(0, gc.w-gtx.Dp(20)), g.headH), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, g.headH, func(gtx layout.Context) layout.Dimensions {
					return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: 13, Color: hc, MaxLines: 1}, gc.c.Name)
				})
			})
		}()
	}
	// the event area over everything
	func() {
		defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &g.tag)
		if g.hover >= 0 {
			pointer.CursorPointer.Add(gtx.Ops)
		}
	}()
	return layout.Dimensions{Size: sz}
}

// layoutLine draws one row, group header, add-item or new-item line.
func (v *View) layoutLine(gtx layout.Context, d *core.ListDoc, lv *lists.View, ln *gridLine, idx, ty, firstW int, groupCol *lists.Column) {
	th := v.h.Theme()
	c := &th.C
	g := &v.grid
	sz := gtx.Constraints.Max
	defer clip.Rect(image.Rect(0, g.headH, sz.X, sz.Y)).Push(gtx.Ops).Pop()
	last := len(g.cols) - 1
	right := g.x0 + firstW
	if last >= 0 {
		right = g.x0 + g.cols[last].x + g.cols[last].w - g.scrollX
	}
	switch ln.kind {
	case lineGroup:
		defer op.Offset(image.Pt(0, ty)).Push(gtx.Ops).Pop()
		x := g.x0 + gtx.Dp(8)
		label := ""
		if groupCol != nil {
			if ln.group == "" {
				label = lists.EmptyGroupLabel(groupCol)
			} else if ch, ok := groupCol.Choice(ln.group); ok {
				x += optionPill(gtx, th, x, ln.h, ch, 15) + gtx.Dp(8)
			} else {
				label = CellText(v.h.Core(), groupCol, groupField(groupCol, ln.group))
			}
		}
		if label != "" {
			lw := th.Measure(gtx, 15, theme.Bold, label)
			at(gtx, image.Pt(x, 0), image.Pt(lw+2, ln.h), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, ln.h, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 15, theme.Bold, c.Fg, label)
				})
			})
			x += lw + gtx.Dp(8)
		}
		cnt := itemsLabel(ln.count)
		cw := th.Measure(gtx, 13, theme.Regular, cnt)
		at(gtx, image.Pt(x, 0), image.Pt(cw+2, ln.h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, ln.h, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 13, theme.Regular, c.FgMuted, cnt)
			})
		})
		return
	case lineAdd:
		defer op.Offset(image.Pt(0, ty)).Push(gtx.Ops).Pop()
		ac := shade(th, "gray-70")
		lw := th.Measure(gtx, 13, theme.Bold, "Add item")
		bw := gtx.Dp(8+16+6+8) + lw
		bx := g.x0 + gtx.Dp(4)
		if g.hover == idx {
			theme.RRect(gtx, image.Rect(bx, (ln.h-gtx.Dp(28))/2, bx+bw, (ln.h+gtx.Dp(28))/2), gtx.Dp(8), c.BgHover)
		}
		at(gtx, image.Pt(bx+gtx.Dp(8), (ln.h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("plus")(gtx, 16, ac)
		})
		at(gtx, image.Pt(bx+gtx.Dp(30), 0), image.Pt(lw+2, ln.h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, ln.h, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 13, theme.Bold, ac, "Add item")
			})
		})
		return
	}
	// a record row (or the new item's editor row)
	defer op.Offset(image.Pt(0, ty)).Push(gtx.Ops).Pop()
	bg := c.Bg
	if ln.kind == lineRow && g.hover == idx && !v.edit.active {
		bg = shade(th, "gray-0")
	}
	if ln.kind == lineNew {
		bg = theme.WithAlpha(c.Accent, 0x14)
	}
	line := shade(th, "gray-5")
	edge := shade(th, "gray-10")
	if th.Dark {
		line = shade(th, "gray-10")
	}
	theme.Fill(gtx, image.Rect(g.x0+gtx.Dp(4), 0, right, ln.h), bg)
	theme.Fill(gtx, image.Rect(g.x0+gtx.Dp(4), 0, right, gtx.Dp(1)), line)
	theme.Fill(gtx, image.Rect(g.x0+gtx.Dp(4), 0, g.x0+gtx.Dp(5), ln.h), edge)
	theme.Fill(gtx, image.Rect(right-gtx.Dp(1), 0, right, ln.h), edge)
	if next := idx + 1; next >= len(g.lines) || g.lines[next].kind != lineRow && g.lines[next].kind != lineNew {
		theme.Fill(gtx, image.Rect(g.x0+gtx.Dp(4), ln.h-gtx.Dp(1), right, ln.h), edge)
	}
	self := ""
	if a := v.h.Core(); a != nil {
		self = a.Self().ID
	}
	var rec *lists.Record
	if ln.kind == lineRow {
		rec = ln.row.Rec
	}
	for i := len(g.cols) - 1; i >= 0; i-- { // the sticky first column draws last
		gc := g.cols[i]
		gx := g.x0 + gc.x
		if i > 0 {
			gx -= g.scrollX
			if gx+gc.w <= g.x0+firstW || gx > sz.X {
				continue
			}
		}
		func() {
			cl := image.Rect(max(gx, g.x0+firstW), gtx.Dp(1), gx+gc.w, ln.h)
			if i == 0 {
				cl.Min.X = gx + gtx.Dp(5)
				theme.Fill(gtx, image.Rect(gx+gtx.Dp(5), gtx.Dp(1), gx+gc.w, ln.h), bg)
			}
			defer clip.Rect(cl).Push(gtx.Ops).Pop()
			px := gtx.Dp(10)
			if i == 0 {
				px = gtx.Dp(12)
			}
			if ln.kind == lineNew {
				if i == 0 {
					at(gtx, image.Pt(gx+gtx.Dp(4), 0), image.Pt(gc.w-gtx.Dp(4), ln.h), func(gtx layout.Context) layout.Dimensions {
						return v.layoutEditor(gtx, d, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)
					})
				}
				return
			}
			if v.edit.editing(rec.ID, gc.c) {
				at(gtx, image.Pt(gx, 0), image.Pt(gc.w, ln.h), func(gtx layout.Context) layout.Dimensions {
					return v.layoutEditor(gtx, d, gc.w, ln.h)
				})
				return
			}
			cw := gc.w - px - gtx.Dp(8)
			if i == 0 && g.hover == idx && !v.edit.active {
				cw -= gtx.Dp(60) // room for "Open"
			}
			at(gtx, image.Pt(gx+px, 0), image.Pt(max(0, cw), ln.h), func(gtx layout.Context) layout.Dimensions {
				v.drawCell(gtx, gc.c, rec.Field(gc.c.Key), max(0, cw), ln.h, cellOpts{primary: i == 0 && gc.c.IsPrimary, wrap: lists.Wraps(lv, gc.c), self: self})
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
			if i == 0 && g.hover == idx && !v.edit.active {
				v.openButton(gtx, gx+gc.w-gtx.Dp(64), ln.h)
			}
			if i > 0 {
				theme.Fill(gtx, image.Rect(gx, gtx.Dp(1), gx+gtx.Dp(1), ln.h), theme.WithAlpha(line, 0x80))
			}
		}()
	}
	if g.scrollX > 0 && len(g.cols) > 1 { // the sticky column's right edge
		theme.Fill(gtx, image.Rect(g.x0+firstW-gtx.Dp(1), 0, g.x0+firstW, ln.h), line)
	}
}

// openButton is the primary cell's hover action (p-list_primary_column_actions).
func (v *View) openButton(gtx layout.Context, x, h int) {
	th := v.h.Theme()
	c := &th.C
	lw := th.Measure(gtx, 13, theme.Bold, "Open")
	w := lw + gtx.Dp(16)
	bh := gtx.Dp(26)
	at(gtx, image.Pt(x, (h-bh)/2), image.Pt(w, bh), func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: image.Pt(w, bh)}
		theme.RRect(gtx, r, gtx.Dp(6), c.Bg)
		theme.Border(gtx, r, gtx.Dp(6), gtx.Dp(1), c.Border)
		return at(gtx, image.Pt(gtx.Dp(8), 0), image.Pt(lw+2, bh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 13, theme.Bold, c.Fg, "Open")
			})
		})
	})
}

func itemsLabel(n int) string {
	if n == 1 {
		return "1 item"
	}
	return itoa(n) + " items"
}

// groupField is a group's value as a cell (for its label).
func groupField(c *lists.Column, v string) *lists.Field {
	f := &lists.Field{Key: c.Key}
	switch c.Type {
	case lists.User, lists.TodoAssignee, lists.Assignee, lists.CreatedBy, lists.LastEditedBy:
		f.User = []string{v}
	case lists.Channel:
		f.Channel = []string{v}
	case lists.Date, lists.DueDate, lists.TodoDueDate:
		f.Date = []string{v}
	default:
		t := v
		f.Text = &t
	}
	return f
}

// gridEvents scrolls, tracks hover and turns clicks into cell actions.
func (v *View) gridEvents(gtx layout.Context, d *core.ListDoc, maxX, maxY int) {
	g := &v.grid
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &g.tag, Kinds: pointer.Press | pointer.Release | pointer.Move | pointer.Leave | pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}, ScrollX: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Scroll:
			dx, dy := int(pe.Scroll.X), int(pe.Scroll.Y)
			if pe.Modifiers.Contain(key.ModShift) && dx == 0 {
				dx, dy = dy, 0
			}
			g.scrollX = min(max(0, g.scrollX+dx), maxX)
			g.scrollY = min(max(0, g.scrollY+dy), maxY)
			g.hover = v.lineAt(int(pe.Position.Y))
		case pointer.Move:
			g.hover = v.lineAt(int(pe.Position.Y))
			g.hoverX = int(pe.Position.X)
		case pointer.Leave:
			g.hover = -1
		case pointer.Press:
			g.press, g.pressed = pe.Position, true
		case pointer.Release:
			if !g.pressed || pe.Position.Sub(g.press).X*pe.Position.Sub(g.press).X+pe.Position.Sub(g.press).Y*pe.Position.Sub(g.press).Y > 36 {
				g.pressed = false
				continue
			}
			g.pressed = false
			v.gridClick(gtx, d, int(pe.Position.X), int(pe.Position.Y))
		}
	}
}

func (v *View) lineAt(y int) int {
	g := &v.grid
	if y < g.headH {
		return -1
	}
	for i, ln := range g.lines {
		ty := ln.y - g.scrollY
		if y >= ty && y < ty+ln.h {
			return i
		}
	}
	return -1
}

func (v *View) gridClick(gtx layout.Context, d *core.ListDoc, x, y int) {
	g := &v.grid
	i := v.lineAt(y)
	if i < 0 {
		return
	}
	ln := g.lines[i]
	switch ln.kind {
	case lineAdd:
		var gc *lists.Column
		if lv := v.current(d); lv != nil {
			gc = d.Meta.Column(lv.GroupBy())
		}
		v.startNew(d, gc, ln.group)
		return
	case lineRow:
	default:
		return
	}
	if v.edit.active {
		v.commit(d)
	}
	firstW := 0
	if len(g.cols) > 0 {
		firstW = g.cols[0].w
	}
	for ci, gc := range g.cols {
		gx := g.x0 + gc.x
		if ci > 0 {
			gx -= g.scrollX
			if x < g.x0+firstW {
				continue
			}
		}
		if x < gx || x >= gx+gc.w {
			continue
		}
		if ci == 0 && x >= gx+gc.w-gtx.Dp(68) {
			if v.act.OpenItem != nil {
				v.act.OpenItem(v.id, ln.row.Rec.ID)
			}
			return
		}
		px := gtx.Dp(10)
		if ci == 0 {
			px = gtx.Dp(12)
		}
		v.cellClick(d, ln.row.Rec, gc.c, x-gx-px, gtx)
		return
	}
}
