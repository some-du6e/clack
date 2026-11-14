package listview

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// boardState is the board's scroll, hover and hit areas.
type boardState struct {
	scrollX, scrollY int
	wheel            wheelScroll
	hover            image.Point
	press            f32.Point
	pressed          bool
	tag              int
	hits             []boardHit
}

type boardHit struct {
	r     image.Rectangle
	rec   string // a card
	add   bool   // a lane's Add item
	group string
}

// Slack's board metrics (p-board_view__column 340px + 6px margins,
// p-board_lane_header ≥35px, p-board_card padding 12 16 16).
const (
	laneW   = 340
	laneGap = 12
	laneHdr = 40
)

// boardGroupCol is the board's lanes column: the view's group-by, else the
// first select column.
func boardGroupCol(d *core.ListDoc, lv *lists.View) *lists.Column {
	if c := d.Meta.Column(lv.GroupBy()); c != nil {
		return c
	}
	for i := range d.Meta.Schema {
		if d.Meta.Schema[i].Type == lists.Select {
			return &d.Meta.Schema[i]
		}
	}
	return nil
}

// laneColor is a lane's option colour ("gray" for the empty lane).
func laneColor(c *lists.Column, v string) string {
	if c != nil {
		if ch, ok := c.Choice(v); ok && ch.Color != "" {
			return ch.Color
		}
	}
	return "gray"
}

func (v *View) layoutBoard(gtx layout.Context, d *core.ListDoc, lv *lists.View, rows []lists.Row) layout.Dimensions {
	th := v.h.Theme()
	c := &th.C
	sz := gtx.Constraints.Max
	b := &v.board
	b.hits = b.hits[:0]
	if v.mode == Peek { // disablePointerEvents
		b.hover = image.Pt(-1, -1)
	}
	gc := boardGroupCol(d, lv)
	var groups []lists.Group
	if gc != nil {
		groups = lists.GroupRows(gc, rows)
	} else {
		groups = []lists.Group{{Rows: rows}}
	}
	cols := d.Meta.VisibleColumns(lv)
	self := ""
	if a := v.h.Core(); a != nil {
		self = a.Self().ID
	}
	x0 := v.pad(gtx) - gtx.Dp(6)
	lw := gtx.Dp(laneW)
	// lane heights first (to clamp the scroll)
	cardH := func(r lists.Row) int {
		n := 0
		for _, col := range cols {
			if col.IsPrimary || (gc != nil && col.Key == gc.Key) {
				continue
			}
			if f := r.Flat[col.Key]; !f.Empty {
				n++
			}
		}
		h := gtx.Dp(12+22+16) + n*gtx.Dp(28)
		if v.cardSummary(d, r.Rec) {
			h += gtx.Dp(22)
		}
		return h
	}
	tallest := 0
	for _, g := range groups {
		h := gtx.Dp(laneHdr)
		for _, r := range g.Rows {
			h += cardH(r) + gtx.Dp(4)
		}
		h += gtx.Dp(44 + 8)
		tallest = max(tallest, h)
	}
	maxX := max(0, x0+len(groups)*(lw+gtx.Dp(laneGap))+gtx.Dp(16)-sz.X)
	maxY := max(0, tallest+gtx.Dp(16)-sz.Y)
	v.boardEvents(gtx, d, maxX, maxY)
	if dx, dy := b.wheel.tick(gtx, b.scrollX, b.scrollY, maxX, maxY); dx != 0 || dy != 0 {
		b.scrollX += dx
		b.scrollY += dy
	}
	b.scrollX = min(max(0, b.scrollX), maxX)
	b.scrollY = min(max(0, b.scrollY), maxY)
	for gi, g := range groups {
		lx := x0 + gi*(lw+gtx.Dp(laneGap)) - b.scrollX
		if lx > sz.X || lx+lw < 0 {
			continue
		}
		ramp := rampOf(laneColor(gc, g.Value))
		laneBg := shade(th, ramp+"-5")
		if ramp == "gray" {
			laneBg = shade(th, "gray-0")
		}
		if ramp == "aubergine" {
			laneBg = shade(th, ramp+"-10")
		}
		border := shade(th, ramp+"-20")
		if ramp == "gray" {
			border = shade(th, "gray-10")
		}
		y := -b.scrollY
		laneH := gtx.Dp(laneHdr)
		for _, r := range g.Rows {
			laneH += cardH(r) + gtx.Dp(4)
		}
		if v.edit.active && v.edit.rec == "" && v.edit.group == g.Value {
			laneH += gtx.Dp(56)
		}
		laneH += gtx.Dp(44 + 8)
		theme.RRect(gtx, image.Rect(lx, y, lx+lw, y+laneH), gtx.Dp(12), laneBg)
		// header: the value (no pill background in lane headers), the count
		hx := lx + gtx.Dp(12)
		label := ""
		if gc != nil && g.Value == "" {
			label = lists.EmptyGroupLabel(gc)
		} else if gc != nil {
			if ch, ok := gc.Choice(g.Value); ok {
				label = ch.Label
			} else {
				label = CellText(v.h.Core(), gc, groupField(gc, g.Value))
			}
		} else {
			label = nsLists.T("All items")
		}
		hh := gtx.Dp(laneHdr)
		if gc != nil && (gc.Type == lists.Checkbox || gc.Type == lists.TodoCompleted) {
			at(gtx, image.Pt(hx, y+(hh-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return checkbox(gtx, th, g.Value == "true")
			})
			hx += gtx.Dp(28)
		} else {
			tw := th.Measure(gtx, 15, theme.Bold, label)
			at(gtx, image.Pt(hx, y), image.Pt(min(tw+2, lw-gtx.Dp(100)), hh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
					return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Bold), Size: 15, Color: c.Fg, MaxLines: 1}, label)
				})
			})
			hx += min(tw, lw-gtx.Dp(100)) + gtx.Dp(8)
		}
		cnt := itemsLabel(len(g.Rows))
		cntCol := shade(th, ramp+"-70")
		at(gtx, image.Pt(hx, y), image.Pt(th.Measure(gtx, 13, theme.Regular, cnt)+2, hh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 13, theme.Regular, cntCol, cnt)
			})
		})
		y += hh
		cx := lx + gtx.Dp(5)
		cw := lw - gtx.Dp(10)
		for _, r := range g.Rows {
			ch := cardH(r)
			rc := image.Rect(cx, y+gtx.Dp(2), cx+cw, y+gtx.Dp(2)+ch)
			if rc.Max.Y > 0 && rc.Min.Y < sz.Y {
				hov := b.hover.In(rc)
				v.drawCard(gtx, d, cols, gc, r, rc, border, hov, self)
			}
			b.hits = append(b.hits, boardHit{r: rc, rec: r.Rec.ID, group: g.Value})
			y += ch + gtx.Dp(4)
		}
		if v.edit.active && v.edit.rec == "" && v.edit.group == g.Value {
			rc := image.Rect(cx, y+gtx.Dp(2), cx+cw, y+gtx.Dp(50))
			at(gtx, rc.Min, rc.Size(), func(gtx layout.Context) layout.Dimensions {
				return v.layoutEditor(gtx, d, rc.Dx(), rc.Dy())
			})
			y += gtx.Dp(56)
		}
		if d.Info.Editable && v.mode != Peek {
			fr := image.Rect(cx, y+gtx.Dp(4), cx+cw, y+gtx.Dp(44))
			if b.hover.In(fr) {
				theme.RRect(gtx, fr, gtx.Dp(8), theme.WithAlpha(border, 0x60))
			}
			ac := shade(th, "gray-70")
			at(gtx, image.Pt(fr.Min.X+gtx.Dp(12), fr.Min.Y+(fr.Dy()-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("plus")(gtx, 16, ac)
			})
			at(gtx, image.Pt(fr.Min.X+gtx.Dp(34), fr.Min.Y), image.Pt(fr.Dx()-gtx.Dp(40), fr.Dy()), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, fr.Dy(), func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 13, theme.Bold, ac, nsLists.T("Add item"))
				})
			})
			b.hits = append(b.hits, boardHit{r: fr, add: true, group: g.Value})
		}
	}
	func() {
		defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &b.tag)
		for _, h := range b.hits {
			if b.hover.In(h.r) {
				pointer.CursorPointer.Add(gtx.Ops)
				break
			}
		}
	}()
	return layout.Dimensions{Size: sz}
}

// drawCard is one p-board_card: the primary field in bold, then the
// card's other visible fields that have values.
func (v *View) drawCard(gtx layout.Context, d *core.ListDoc, cols []*lists.Column, gc *lists.Column, r lists.Row, rc image.Rectangle, border color.NRGBA, hov bool, self string) {
	th := v.h.Theme()
	c := &th.C
	theme.RRect(gtx, rc, gtx.Dp(8), c.Bg)
	bc := border
	if hov {
		bc = theme.Over(theme.WithAlpha(c.Fg, 0x30), border)
	}
	theme.Border(gtx, rc, gtx.Dp(8), gtx.Dp(1), bc)
	defer clip.Rect(rc).Push(gtx.Ops).Pop()
	x := rc.Min.X + gtx.Dp(16)
	w := rc.Dx() - gtx.Dp(32)
	y := rc.Min.Y + gtx.Dp(12)
	if p := d.Meta.Primary(); p != nil {
		name := r.Rec.Field(p.Key).PlainText()
		if name == "" {
			name = nsLists.T("Untitled item")
		}
		tx := x
		if d.Meta.TodoMode {
			if tc := d.Meta.Column(lists.TodoCompleted); tc != nil {
				at(gtx, image.Pt(x, y+gtx.Dp(1)), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					v.drawCell(gtx, tc, r.Rec.Field(tc.Key), gtx.Dp(20), gtx.Dp(20), cellOpts{})
					return layout.Dimensions{Size: gtx.Constraints.Max}
				})
				tx += gtx.Dp(28)
			}
		}
		at(gtx, image.Pt(tx, y), image.Pt(max(0, w-(tx-x)), gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Bold), Size: 15, Color: c.Fg, MaxLines: 1}, name)
		})
	}
	y += gtx.Dp(22)
	if v.cardSummary(d, r.Rec) { // p-board-card__subtasks (subtasks.go)
		done, total := subtaskProgress(d, r.Rec.ID)
		sz := summarySize(gtx, th, done, total, true, true)
		at(gtx, image.Pt(x+gtx.Dp(22), y), sz, func(gtx layout.Context) layout.Dimensions {
			return drawSummary(gtx, th, done, total, true, true, false)
		})
		y += sz.Y
	}
	y += gtx.Dp(6)
	for _, col := range cols {
		if col.IsPrimary || (gc != nil && col.Key == gc.Key) || col.Type == lists.TodoCompleted {
			continue
		}
		if r.Flat[col.Key].Empty {
			continue
		}
		col := col
		at(gtx, image.Pt(x, y), image.Pt(w, gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
			v.drawCell(gtx, col, r.Rec.Field(col.Key), w, gtx.Dp(24), cellOpts{size: 13, self: self})
			return layout.Dimensions{Size: gtx.Constraints.Max}
		})
		y += gtx.Dp(28)
	}
}

// cardSummary: the card shows SubtasksProgressSummary under its name
// (Yja+'s board renderBoardField: lists_sub_tasks, a to-do list with
// grid to-dos, the record has subtasks).
func (v *View) cardSummary(d *core.ListDoc, rec *lists.Record) bool {
	return v.gridTodos(d) && v.subTasksOn() && hasSubtasks(d, rec.ID)
}

func (v *View) boardEvents(gtx layout.Context, d *core.ListDoc, maxX, maxY int) {
	b := &v.board
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &b.tag, Kinds: pointer.Press | pointer.Release | pointer.Move | pointer.Leave | pointer.Scroll,
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
			dx, dy := b.wheel.event(gtx, pe, b.scrollX, b.scrollY, maxX, maxY, true)
			b.scrollX = min(max(0, b.scrollX+dx), maxX)
			b.scrollY = min(max(0, b.scrollY+dy), maxY)
		case pointer.Move:
			b.hover = image.Pt(int(pe.Position.X), int(pe.Position.Y))
		case pointer.Leave:
			b.hover = image.Pt(-1, -1)
		case pointer.Press:
			if ctxclick.Press(pe) { // the card's BaseRecordContextMenu (records.go)
				b.pressed = false
				p := image.Pt(int(pe.Position.X), int(pe.Position.Y))
				for _, h := range b.hits {
					if p.In(h.r) && !h.add {
						if rec := d.Record(h.rec); rec != nil {
							v.openRecordMenu(d, rec)
						}
						break
					}
				}
				continue
			}
			b.wheel.press(gtx, pe, maxX, maxY)
			b.press, b.pressed = pe.Position, true
		case pointer.Release:
			moved := pe.Position.Sub(b.press)
			if !b.pressed || moved.X*moved.X+moved.Y*moved.Y > 36 {
				b.pressed = false
				continue
			}
			b.pressed = false
			p := image.Pt(int(pe.Position.X), int(pe.Position.Y))
			for _, h := range b.hits {
				if !p.In(h.r) {
					continue
				}
				if h.add {
					v.startNew(d, boardGroupCol(d, v.current(d)), h.group)
				} else if v.act.OpenItem != nil {
					v.act.OpenItem(v.id, h.rec)
				}
				break
			}
		}
	}
}
