package listview

// Subtasks (lists_sub_tasks, client-boot Hm0j isListsSubTasksEnabled;
// CDN build fa938a086569, 2026-11-13). The records come with the list
// (WCdF fetches every list with includeSubtasks); a record with a
// parent_record_id is a subtask and never a row of its own. With the
// experiment on:
//
//   - The grid's primary cell is ListPrimaryColumnActions' new layout
//     (boot-shared Yja+): at rest, right-aligned (p-list_primary_column_
//     actions_new__static: content-sec, margin-right 34px, 20px in a list
//     that isn't a to-do list, 2px up), the SubtasksProgressSummary
//     button (uzTK, tooltip "Open subtasks") and the new ThreadIndicator
//     (p-thread_indicator__new: threads or, unread, threads-filled in
//     sunflower-40, and the count, 9+ past nine; tooltip "View
//     comment(s)"); on hover the icon-only Open item button (sidebar 20,
//     p-list_primary_column_actions__subtasks: 10px from the cell's right
//     and top). The old hover box (Open, Add comment, More actions) is
//     gone.
//   - A board card in a to-do list shows the summary under its name
//     (p-board-card__subtasks, margin-left 22px).
//   - The record pane has 2kGA's ListRecordSubtasks after the fields:
//     "Subtasks" with the summary, the subtasks (by position, else by
//     creation), each with its done checkbox, name, Open subtask, and in
//     a to-do list the assignee and the due date (MM/DD), and Add Subtask
//     (none in a view-only list or an archived item; disabled with the
//     limit's tooltip and banner at 50).
//   - A subtask's pane says "Subtask of {parent}" (and "in {list}" when
//     the list isn't the primary view) instead of the list's name.
//
// Off, the summary, the pane's section and the new indicator aren't
// drawn; BaseRecordContextMenu's "Open subtasks" (any record with
// subtasks) is in both groups. Clack's off group keeps its own Open
// button: Slack's older hover box (Open, the old ThreadIndicator, More
// actions) isn't ported.

import (
	"context"
	"image"
	"image/color"
	"sort"
	"strconv"

	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// subtaskLimit is ListRecordSubtasks' M: at 50 Add Subtask is disabled.
const subtaskLimit = 50

// subTasksOn is isListsSubTasksEnabled.
func (c *ctl) subTasksOn() bool { return c.exp("lists_sub_tasks") }

// subtasksOf are parent's subtasks as ListRecordSubtasks orders them (B):
// by position when both have one, else by date_created.
func subtasksOf(d *core.ListDoc, parent string) []*lists.Record {
	if d == nil || parent == "" {
		return nil
	}
	var out []*lists.Record
	for i := range d.Records {
		if d.Records[i].ParentRecordID == parent {
			out = append(out, &d.Records[i])
		}
	}
	num := func(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Position != "" && b.Position != "" {
			return num(a.Position) < num(b.Position)
		}
		return a.DateCreated < b.DateCreated
	})
	return out
}

// hasSubtasks is Prtm's eD (parentRecordsById has the record).
func hasSubtasks(d *core.ListDoc, rec string) bool {
	if d == nil || rec == "" {
		return false
	}
	for i := range d.Records {
		if d.Records[i].ParentRecordID == rec {
			return true
		}
	}
	return false
}

// subtaskProgress is Prtm's LS: how many of the subtasks have
// fields.todo_completed, and how many there are.
func subtaskProgress(d *core.ListDoc, rec string) (done, total int) {
	for _, s := range subtasksOf(d, rec) {
		total++
		if f := s.Field(lists.TodoCompleted); f != nil && f.Checkbox != nil && *f.Checkbox {
			done++
		}
	}
	return done, total
}

// summaryText is SubtasksProgressSummary's text: "done/total" in a to-do
// list, else the total; past 99, "99+". (Slack prints the raw done count
// when it is under 100, which is the same number.)
func summaryText(done, total int, todo bool) string {
	cap99 := func(n int) string {
		if n > 99 {
			return "99+"
		}
		return strconv.Itoa(n)
	}
	if todo {
		return cap99(done) + "/" + cap99(total)
	}
	return cap99(total)
}

// summaryIcon is the summary's glyph: subtask-complete when every
// subtask is done, subtask-progress when some are, else subtask.
func summaryIcon(done, total int) string {
	switch {
	case done == total:
		return "subtask-complete"
	case done > 0:
		return "subtask-progress"
	}
	return "subtask"
}

// Slack's tokens the subtask parts use (kit.css, light then dark).
var (
	surfPry    = [2]color.NRGBA{{0x1d, 0x1c, 0x1d, 0x0f}, {0xf8, 0xf8, 0xf8, 0x0f}} // --dt_color-surf-pry
	surfSec    = [2]color.NRGBA{{0x1d, 0x1c, 0x1d, 0x40}, {0xf8, 0xf8, 0xf8, 0x40}} // --dt_color-surf-sec
	contentTer = [2]color.NRGBA{{0x5e, 0x5d, 0x60, 0xff}, {0x9a, 0x9b, 0x9e, 0xff}} // --dt_color-content-ter
	skHigh     = [2]color.NRGBA{{18, 100, 163, 0xff}, {29, 155, 209, 0xff}}         // --sk_highlight
	skHighHov  = [2]color.NRGBA{{11, 76, 140, 0xff}, {64, 179, 228, 0xff}}          // --sk_highlight_hover
)

func tok(th *theme.Theme, t [2]color.NRGBA) color.NRGBA {
	if th.Dark {
		return t[1]
	}
	return t[0]
}

// summarySize is the summary's box: as the grid's button
// (progressSummaryBtn: padding 2px 7px 2px 0, around container__O5xJU's
// 8px margin, the 18px icon, 5px and the 13px bold text) or bare.
func summarySize(gtx layout.Context, th *theme.Theme, done, total int, todo, btn bool) image.Point {
	w := gtx.Dp(8+18+5) + th.Measure(gtx, 13, theme.Bold, summaryText(done, total, todo))
	h := gtx.Dp(18)
	if btn {
		w += gtx.Dp(7)
		h += gtx.Dp(4)
	}
	return image.Pt(w, h)
}

// drawSummary draws SubtasksProgressSummary at the origin (content-sec;
// the button's hover is surf-pry, radius 4).
func drawSummary(gtx layout.Context, th *theme.Theme, done, total int, todo, btn, hov bool) layout.Dimensions {
	sz := summarySize(gtx, th, done, total, todo, btn)
	fg := th.C.FgMuted
	top := 0
	if btn {
		if hov {
			theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), tok(th, surfPry))
		}
		top = gtx.Dp(2)
	}
	ih := gtx.Dp(18)
	at(gtx, image.Pt(gtx.Dp(8), top), image.Pt(ih, ih), func(gtx layout.Context) layout.Dimensions {
		return icons.Get(summaryIcon(done, total))(gtx, 18, fg)
	})
	txt := summaryText(done, total, todo)
	at(gtx, image.Pt(gtx.Dp(8+18+5), top), image.Pt(th.Measure(gtx, 13, theme.Bold, txt)+2, ih), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, ih, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 13, theme.Bold, fg, txt)
		})
	})
	return layout.Dimensions{Size: sz}
}

// threadCount is ThreadIndicator's count: 9+ past nine.
func threadCount(n int) string {
	if n > 9 {
		return "9+"
	}
	return strconv.Itoa(n)
}

// indicatorSize is the new ThreadIndicator's box (p-thread_indicator__new
// padding 4px 6px around the 18px icon, 4px and the 13px count).
func indicatorSize(gtx layout.Context, th *theme.Theme, n int) image.Point {
	return image.Pt(gtx.Dp(6+18+4+6)+th.Measure(gtx, 13, theme.Regular, threadCount(n)), gtx.Dp(26))
}

// drawIndicator draws the new ThreadIndicator: threads (unread:
// threads-filled in sunflower-40) and the count, in content-sec; its
// hover is surf-pry, radius 4.
func drawIndicator(gtx layout.Context, th *theme.Theme, n int, unread, hov bool) layout.Dimensions {
	sz := indicatorSize(gtx, th, n)
	if hov {
		theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), tok(th, surfPry))
	}
	icon, ic := "threads", th.C.FgMuted
	if unread {
		icon, ic = "threads-filled", shade(th, "sunflower-40")
	}
	at(gtx, image.Pt(gtx.Dp(6), gtx.Dp(4)), image.Pt(gtx.Dp(18), gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
		return icons.Get(icon)(gtx, 18, ic)
	})
	txt := threadCount(n)
	at(gtx, image.Pt(gtx.Dp(6+18+4), 0), image.Pt(th.Measure(gtx, 13, theme.Regular, txt)+2, sz.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 13, theme.Regular, th.C.FgMuted, txt)
		})
	})
	return layout.Dimensions{Size: sz}
}

// threadUnread is getRecordThreadIsUnread: the viewer follows the item's
// thread and its latest reply is past the thread's last read.
func (c *ctl) threadUnread(d *core.ListDoc, rec *lists.Record) bool {
	a := c.h.Core()
	if a == nil || rec.ThreadTS == "" {
		return false
	}
	m, ok := a.S.Find(core.ListCommentsChannel(d.Info.ID), rec.ThreadTS)
	if !ok || !m.Subscribed {
		return false
	}
	last := m.LastRead
	if last == "" {
		last = "0"
	}
	latest := m.LatestReply
	if latest == "" {
		latest = m.TS
	}
	return latest > last
}

// ---- the grid's primary cell ----

// gridAct is one of a row's primary-cell buttons (grid coordinates).
type gridAct struct {
	r    image.Rectangle
	rec  string
	kind int
}

const (
	actSummary = iota // SubtasksProgressSummary: Open subtasks
	actThread         // ThreadIndicator: the item's comments
	actOpen           // Open item
	actKinds
)

// primaryStatic measures the static part of a row's primary cell: the
// summary and the indicator, and the gap between them; w is 0 when
// neither shows.
type primaryStatic struct {
	done, total, replies int
	unread, todo         bool
	sum, ind             image.Point
	w                    int
}

func (v *View) primaryStatic(gtx layout.Context, d *core.ListDoc, rec *lists.Record) primaryStatic {
	th := v.h.Theme()
	ps := primaryStatic{todo: d.Meta.TodoMode}
	ps.done, ps.total = subtaskProgress(d, rec.ID)
	ps.replies = v.commentCount(d, rec)
	if ps.total > 0 {
		ps.sum = summarySize(gtx, th, ps.done, ps.total, ps.todo, true)
		ps.w += ps.sum.X
		if ps.replies > 0 {
			ps.w += gtx.Dp(2) // p-list_primary_column_actions__subtask_summary--has_replies
		}
	}
	if ps.replies > 0 {
		ps.unread = v.threadUnread(d, rec)
		ps.ind = indicatorSize(gtx, th, ps.replies)
		ps.w += ps.ind.X
	}
	return ps
}

// staticRight is where the static part ends, from the cell's right edge:
// the inner container's 8px padding and the margin (34px, 20px outside
// to-do lists).
func staticRight(gtx layout.Context, todo bool) int {
	if todo {
		return gtx.Dp(8 + 34)
	}
	return gtx.Dp(8 + 20)
}

// drawPrimaryActs draws the row's static summary and indicator and, on
// hover, the Open item button, in the line's coordinates (the cell spans
// gx..gx+w; ty is the line's top in the grid) and records their rects.
func (v *View) drawPrimaryActs(gtx layout.Context, rec *lists.Record, ps primaryStatic, gx, w, h, ty int, hovRow bool) {
	th := v.h.Theme()
	g := &v.grid
	hp := image.Pt(g.hoverX, g.hoverY-ty) // the pointer in the line
	right := gx + w
	x := right - staticRight(gtx, ps.todo) - ps.w
	cy := h/2 - gtx.Dp(2)
	add := func(r image.Rectangle, kind int) bool {
		g.acts = append(g.acts, gridAct{r: r.Add(image.Pt(0, ty)), rec: rec.ID, kind: kind})
		return hovRow && hp.In(r)
	}
	if ps.total > 0 {
		r := image.Rectangle{Min: image.Pt(x, cy-ps.sum.Y/2)}
		r.Max = r.Min.Add(ps.sum)
		hov := add(r, actSummary)
		at(gtx, r.Min, ps.sum, func(gtx layout.Context) layout.Dimensions {
			d := drawSummary(gtx, th, ps.done, ps.total, ps.todo, true, hov)
			if hov {
				g.tipHit[actSummary] = true
				popups.Tooltip(gtx, th, &g.tips[actSummary], true, ps.sum, popups.Above, nsLists.T("Open subtasks"), "")
			}
			return d
		})
		x += ps.sum.X
		if ps.replies > 0 {
			x += gtx.Dp(2)
		}
	}
	if ps.replies > 0 {
		r := image.Rectangle{Min: image.Pt(x, cy-ps.ind.Y/2)}
		r.Max = r.Min.Add(ps.ind)
		hov := add(r, actThread)
		at(gtx, r.Min, ps.ind, func(gtx layout.Context) layout.Dimensions {
			d := drawIndicator(gtx, th, ps.replies, ps.unread, hov)
			if hov {
				g.tipHit[actThread] = true
				popups.Tooltip(gtx, th, &g.tips[actThread], true, ps.ind, popups.Above,
					nsLists.T("{count, plural, =1 {View comment} other {View comments}}", "count", ps.replies), "")
			}
			return d
		})
	}
	if !hovRow || v.edit.active {
		return
	}
	// p-list_primary_column_actions__subtask_open: padding 4px 6px around
	// the 20px sidebar icon, 10px in from the cell's top and right
	bs := image.Pt(gtx.Dp(32), gtx.Dp(28))
	r := image.Rectangle{Min: image.Pt(right-gtx.Dp(10)-bs.X, gtx.Dp(10))}
	r.Max = r.Min.Add(bs)
	hov := add(r, actOpen)
	at(gtx, r.Min, bs, func(gtx layout.Context) layout.Dimensions {
		if hov {
			theme.RRect(gtx, image.Rectangle{Max: bs}, gtx.Dp(4), tok(th, surfPry))
		}
		at(gtx, image.Pt(gtx.Dp(6), gtx.Dp(4)), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("sidebar")(gtx, 20, th.C.FgMuted)
		})
		if hov {
			g.tipHit[actOpen] = true
			popups.Tooltip(gtx, th, &g.tips[actOpen], true, bs, popups.Above, nsLists.T("Open item"), "")
		}
		return layout.Dimensions{Size: bs}
	})
}

// endTips lets go of the tooltips no button held this frame.
func (v *View) endTips(gtx layout.Context) {
	g := &v.grid
	for k := 0; k < actKinds; k++ {
		if !g.tipHit[k] {
			popups.Tooltip(gtx, v.h.Theme(), &g.tips[k], false, image.Point{}, popups.Above, "", "")
		}
		g.tipHit[k] = false
	}
}

// actAt is the primary-cell button under (x, y), if any.
func (g *gridState) actAt(x, y int) (gridAct, bool) {
	p := image.Pt(x, y)
	for _, a := range g.acts {
		if p.In(a.r) {
			return a, true
		}
	}
	return gridAct{}, false
}

// ---- the record pane ----

// subtaskSection is ListRecordSubtasks (2kGA) at y in the pane's content;
// it returns the y below it. Its rows' hits are kept for clicks.
func (it *Item) subtaskSection(gtx layout.Context, d *core.ListDoc, rec *lists.Record, y, w int, self string) int {
	th := it.h.Theme()
	c := &th.C
	todo := d.Meta.TodoMode
	subs := subtasksOf(d, rec.ID)
	done, total := subtaskProgress(d, rec.ID)
	ro := !d.Info.Editable // I9 isListViewOnly
	over := len(subs) >= subtaskLimit
	hp := it.hover.Add(image.Pt(0, it.scroll))
	otl := theme.OtlTer(th)
	// container__tkQ7V: a 1px otl-ter top, padding 16; "Subtasks" 18/27
	// black, then the summary
	theme.Fill(gtx, image.Rect(0, y, w, y+gtx.Dp(1)), otl)
	y += gtx.Dp(16)
	hdr := nsLists.T("Subtasks")
	hw := th.Measure(gtx, 18, theme.Black, hdr)
	lh := gtx.Dp(27)
	at(gtx, image.Pt(gtx.Dp(16), y), image.Pt(hw+2, lh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 18, theme.Black, c.Fg, hdr)
		})
	})
	if total > 0 {
		ss := summarySize(gtx, th, done, total, todo, false)
		at(gtx, image.Pt(gtx.Dp(16)+hw, y+(lh-ss.Y)/2), ss, func(gtx layout.Context) layout.Dimensions {
			return drawSummary(gtx, th, done, total, todo, false, false)
		})
	}
	y += lh + gtx.Dp(16)
	if over { // SubtaskLimitBanner: an error alert, margins 0 16 4
		y = it.limitBanner(gtx, y, w)
	}
	var tc, ac, dc *lists.Column // useGetSubtaskSchema's completed, assignee and due date
	for i := range d.Meta.Schema {
		col := &d.Meta.Schema[i]
		switch {
		case col.Type == lists.TodoCompleted && tc == nil:
			tc = col
		case col.Type == lists.TodoAssignee && ac == nil:
			ac = col
		case col.Type == lists.TodoDueDate && dc == nil:
			dc = col
		}
	}
	p := d.Meta.Primary()
	// subtasksContainer__Qy9cK: padding 0 16; each row container__HUa7M
	// (min-height 48, padding 6px 8px, a 1px otl-ter bottom; hover
	// surf-pry and the 4×35 surf-sec handle at the left)
	x0, x1 := gtx.Dp(16), w-gtx.Dp(16)
	for _, s := range subs {
		s := s
		rh := gtx.Dp(48)
		row := image.Rect(x0, y, x1, y+rh)
		hov := hp.In(row)
		if hov {
			theme.Fill(gtx, row, tok(th, surfPry))
			theme.RRect(gtx, image.Rect(x0, y+gtx.Dp(6), x0+gtx.Dp(4), y+gtx.Dp(6+35)), gtx.Dp(6), tok(th, surfSec))
			it.hits = append(it.hits, itemHit{r: image.Rect(x0, y, x0+gtx.Dp(9), y+rh), rec: s.ID, act: "subtask-menu"})
		}
		theme.Fill(gtx, image.Rect(x0, y+rh-gtx.Dp(1), x1, y+rh), otl)
		lx := x0 + gtx.Dp(8)
		rx := x1 - gtx.Dp(8)
		completed := false
		if f := s.Field(lists.TodoCompleted); f != nil && f.Checkbox != nil && *f.Checkbox {
			completed = true
		}
		// rightContent (to-do lists): the assignee's 36px button, then the
		// due date
		if todo {
			if dc != nil {
				dw := it.subtaskDate(gtx, dc, s, rx, y, ro || rec.Archived, hp)
				rx -= dw
			}
			if ac != nil {
				ar := image.Rect(rx-gtx.Dp(36), y+gtx.Dp(6), rx, y+gtx.Dp(6+36))
				it.subtaskAssignee(gtx, ac, s, ar, ro || rec.Archived, hp)
				rx = ar.Min.X
			}
		}
		// leftContent: the checkbox (to-do lists; padding-top 8), the name
		// (subtaskNameField padding 2px 12px 6px, nameField bold with 4px
		// on top and 24px on the right), Open subtask at its right
		if todo && tc != nil {
			cb := image.Rect(lx, y+gtx.Dp(6+8), lx+gtx.Dp(20), y+gtx.Dp(6+8+20))
			at(gtx, cb.Min, cb.Size(), func(gtx layout.Context) layout.Dimensions {
				it.drawField(gtx, tc, s.Field(tc.Key), cb.Dx(), cb.Dy(), self)
				return layout.Dimensions{Size: cb.Size()}
			})
			if !rec.Archived {
				it.hits = append(it.hits, itemHit{r: cb, col: tc, x0: cb.Min.X, rec: s.ID})
			}
			lx = cb.Max.X
		}
		// leftContent: flex 1 beside rightContent, at most 100% - 72px in a
		// to-do list
		left := x1 - gtx.Dp(8)
		if todo {
			left = min(rx, x1-gtx.Dp(8+72))
		}
		ob := image.Rect(left-gtx.Dp(28), y+gtx.Dp(6), left, y+gtx.Dp(6+28))
		nx := lx + gtx.Dp(12)
		nr := image.Rect(nx, y+gtx.Dp(6+4+2), max(nx, ob.Min.X-gtx.Dp(24)), y+rh-gtx.Dp(6))
		if p != nil && it.edit.editing(s.ID, p) {
			at(gtx, image.Pt(nr.Min.X, y+gtx.Dp(6)), image.Pt(nr.Dx(), gtx.Dp(36)), func(gtx layout.Context) layout.Dimensions {
				return it.layoutEditor(gtx, d, nr.Dx(), gtx.Dp(36))
			})
		} else {
			name := ""
			if p != nil {
				name = s.Field(p.Key).PlainText()
			}
			fg := c.Fg
			if completed { // crossedOut__Wguq7: content-ter, struck through
				fg = tok(th, contentTer)
			}
			at(gtx, nr.Min, image.Pt(nr.Dx(), gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.X = nr.Dx()
				d := th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Bold), Size: 15, Color: fg, MaxLines: 1}, name)
				if tw := min(th.Measure(gtx, 15, theme.Bold, name), nr.Dx()); completed && tw > 0 {
					ly := gtx.Dp(11) // line-through: the text's width only
					theme.Fill(gtx, image.Rect(0, ly, tw, ly+max(1, gtx.Dp(1))), fg)
				}
				return d
			})
			if p != nil && !ro && !rec.Archived {
				it.hits = append(it.hits, itemHit{r: nr, col: p, x0: nr.Min.X, rec: s.ID})
			}
		}
		if hov { // openSubtaskBtnContainer: shown on the row's hover
			ohov := hp.In(ob)
			at(gtx, ob.Min, ob.Size(), func(gtx layout.Context) layout.Dimensions {
				fg := c.FgMuted
				if ohov {
					theme.RRect(gtx, image.Rectangle{Max: ob.Size()}, gtx.Dp(8), c.BgHover)
					fg = c.Fg
				}
				at(gtx, image.Pt(gtx.Dp(6), gtx.Dp(6)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("sidebar")(gtx, 16, fg)
				})
				if ohov {
					it.tipHit = true
					popups.Tooltip(gtx, th, &it.tip, true, ob.Size(), popups.Above, nsLists.T("Open subtask"), "")
				}
				return layout.Dimensions{Size: ob.Size()}
			})
			it.hits = append(it.hits, itemHit{r: ob, rec: s.ID, act: "subtask-open"})
		}
		it.hits = append(it.hits, itemHit{r: row, rec: s.ID, act: "subtask-row"}) // right-clicks
		y += rh
	}
	// Add Subtask (not in a view-only list or an archived item): margin
	// 16px 16px 16px 30px, sk_highlight; plus-filled 16, 4px, the label
	if !ro && !rec.Archived {
		y += gtx.Dp(16)
		label := nsLists.T("Add Subtask")
		lw := th.Measure(gtx, 15, theme.Regular, label)
		br := image.Rect(gtx.Dp(30), y, gtx.Dp(30+16+4)+lw, y+gtx.Dp(22))
		hov := hp.In(br)
		fg := tok(th, skHigh)
		if over {
			fg = tok(th, contentTer)
		} else if hov {
			fg = tok(th, skHighHov)
		}
		at(gtx, image.Pt(br.Min.X, y+gtx.Dp(3)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("plus-filled")(gtx, 16, fg)
		})
		at(gtx, image.Pt(br.Min.X+gtx.Dp(20), y), image.Pt(lw+2, br.Dy()), func(gtx layout.Context) layout.Dimensions {
			d := th.Label(gtx, 15, theme.Regular, fg, label)
			if over && hov {
				it.tipHit = true
				popups.Tooltip(gtx, th, &it.tip, true, br.Size(), popups.Above, nsLists.T("Maximum number of 50 subtasks reached."), "")
			}
			return d
		})
		if !over {
			it.hits = append(it.hits, itemHit{r: br, act: "subtask-add"})
		}
		y += br.Dy() + gtx.Dp(16)
	}
	return y
}

// limitBanner is SubtaskLimitBanner, an Alert (VvO8) of level error:
// a 1px base-inv-imp outline on surf-imp, radius 8, padding 12, the
// warning icon (20, base-inv-imp) and "<strong>Maximum number of 50
// subtasks reached.</strong> <br></br>Delete subtasks to open space."
// (margins 0 16px 4px).
func (it *Item) limitBanner(gtx layout.Context, y, w int) int {
	th := it.h.Theme()
	c := &th.C
	red, bg := slackTok(th, "base-inv-imp"), slackTok(th, "surf-imp")
	r := image.Rect(gtx.Dp(16), y, w-gtx.Dp(16), y+gtx.Dp(12+44+12))
	theme.RRect(gtx, r, gtx.Dp(8), red)
	theme.RRect(gtx, r.Inset(max(1, gtx.Dp(1))), gtx.Dp(8)-max(1, gtx.Dp(1)), theme.Over(bg, c.Bg))
	at(gtx, image.Pt(r.Min.X+gtx.Dp(12), r.Min.Y+gtx.Dp(12+1)), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("warning")(gtx, 20, red)
	})
	tx := r.Min.X + gtx.Dp(12+28)
	tw := r.Max.X - tx - gtx.Dp(12)
	at(gtx, image.Pt(tx, r.Min.Y+gtx.Dp(12)), image.Pt(tw, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Bold), Size: 15, Color: c.Fg, MaxLines: 1}, nsLists.T("Maximum number of 50 subtasks reached."))
	})
	at(gtx, image.Pt(tx, r.Min.Y+gtx.Dp(12+22)), image.Pt(tw, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: 15, Color: c.Fg, MaxLines: 1}, nsLists.T("Delete subtasks to open space."))
	})
	return r.Max.Y + gtx.Dp(4)
}

// slackTok is a --dt_color token in th's mode (Slack's stylesheet values).
func slackTok(th *theme.Theme, name string) color.NRGBA {
	c, _ := css.ParseColor(css.SlackVars(th.Dark)["--dt_color-"+name])
	return c
}

// subtaskAssignee is the row's assignee button (tC): the assignee's 24px
// avatar in a 36px box, else the user icon (20; half opacity when it
// can't be changed) with "Set assignee"; hover surf-pry, radius 6.
func (it *Item) subtaskAssignee(gtx layout.Context, ac *lists.Column, s *lists.Record, r image.Rectangle, disabled bool, hp image.Point) {
	th := it.h.Theme()
	hov := !disabled && hp.In(r)
	uid := ""
	if f := s.Field(ac.Key); f != nil && len(f.User) > 0 {
		uid = f.User[0]
	}
	at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
		if hov {
			theme.RRect(gtx, image.Rectangle{Max: r.Size()}, gtx.Dp(6), tok(th, surfPry))
		}
		if uid != "" {
			url := ""
			if a := it.h.Core(); a != nil {
				if u, ok := a.User(uid); ok {
					url = u.Avatar
				}
			}
			at(gtx, image.Pt(gtx.Dp(6), gtx.Dp(6)), image.Pt(gtx.Dp(24), gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
				return avatar(gtx, it.h, url, 24)
			})
		} else {
			fg := th.C.FgMuted
			if disabled {
				fg = theme.WithAlpha(fg, fg.A/2)
			}
			at(gtx, image.Pt(gtx.Dp(8), gtx.Dp(8)), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("user")(gtx, 20, fg)
			})
			if hov {
				it.tipHit = true
				popups.Tooltip(gtx, th, &it.tip, true, r.Size(), popups.Above, nsLists.T("Set assignee"), "")
			}
		}
		return layout.Dimensions{Size: r.Size()}
	})
	if !disabled {
		it.hits = append(it.hits, itemHit{r: r, col: ac, x0: r.Min.X, rec: s.ID})
	}
}

// subtaskDate is the row's due date (4e/5 with subtaskVersion): the date
// as MM/DD (15px, a 24px box, padding 0 5, 5px from the avatar), or the
// 36px calendar button ("Set date"). It returns its width and leaves it
// at the row's right edge rx.
func (it *Item) subtaskDate(gtx layout.Context, dc *lists.Column, s *lists.Record, rx, y int, disabled bool, hp image.Point) int {
	th := it.h.Theme()
	f := s.Field(dc.Key)
	val := ""
	if f != nil && len(f.Date) > 0 {
		val = renderDate(f.Date[0], "MM/DD")
	}
	if it.edit.editing(s.ID, dc) { // the date's inline editor
		ew := gtx.Dp(140)
		at(gtx, image.Pt(rx-ew, y+gtx.Dp(6)), image.Pt(ew, gtx.Dp(36)), func(gtx layout.Context) layout.Dimensions {
			it.mu.Lock()
			d := it.doc
			it.mu.Unlock()
			return it.layoutEditor(gtx, d, ew, gtx.Dp(36))
		})
		return ew + gtx.Dp(5)
	}
	var r image.Rectangle
	if val != "" {
		tw := th.Measure(gtx, 15, theme.Regular, val)
		r = image.Rect(rx-tw-gtx.Dp(10), y+gtx.Dp(6+6), rx, y+gtx.Dp(6+6+24))
	} else {
		r = image.Rect(rx-gtx.Dp(36), y+gtx.Dp(6), rx, y+gtx.Dp(6+36))
	}
	hov := !disabled && hp.In(r)
	at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
		sz := r.Size()
		if val != "" {
			if hov {
				theme.Border(gtx, image.Rectangle{Max: sz}, gtx.Dp(6), max(1, gtx.Dp(1)), theme.OtlTer(th))
			}
			at(gtx, image.Pt(gtx.Dp(5), 0), image.Pt(sz.X-gtx.Dp(5), sz.Y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 15, theme.Regular, th.C.Fg, val)
				})
			})
			if hov {
				it.tipHit = true
				popups.Tooltip(gtx, th, &it.tip, true, sz, popups.Above, nsLists.T("Edit date: {startDate}", "startDate", f.Date[0]), "")
			}
		} else {
			fg := th.C.FgMuted
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(6), tok(th, surfPry))
				fg = th.C.Fg
			}
			at(gtx, image.Pt(gtx.Dp(8), gtx.Dp(8)), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("calendar")(gtx, 20, fg)
			})
			if hov {
				it.tipHit = true
				popups.Tooltip(gtx, th, &it.tip, true, sz, popups.Above, nsLists.T("Set date"), "")
			}
		}
		return layout.Dimensions{Size: sz}
	})
	if !disabled {
		it.hits = append(it.hits, itemHit{r: r, col: dc, x0: r.Min.X, rec: s.ID})
	}
	return r.Dx() + gtx.Dp(5)
}

// subtaskSubtitle is SubtaskSubtitle: "Subtask of" and the parent's name
// (list-item icon), then, when the list isn't the primary view, "in" and
// the list's title. It returns the y below it.
func (it *Item) subtaskSubtitle(gtx layout.Context, d *core.ListDoc, parent *lists.Record, y, w int) int {
	th := it.h.Theme()
	c := &th.C
	x := gtx.Dp(16)
	lh := gtx.Dp(22)
	y += gtx.Dp(4)
	word := func(s string) {
		sw := th.Measure(gtx, 15, theme.Bold, s)
		at(gtx, image.Pt(x, y), image.Pt(sw+2, lh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 15, theme.Bold, c.Fg, s)
			})
		})
		x += sw + gtx.Dp(4)
	}
	link := func(text, act string) {
		tw := th.Measure(gtx, 15, theme.Bold, text)
		r := image.Rect(x, y, min(w-gtx.Dp(16), x+gtx.Dp(16+4)+tw), y+lh)
		at(gtx, image.Pt(x, y+gtx.Dp(3)), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("list-item")(gtx, 16, c.Link)
		})
		at(gtx, image.Pt(x+gtx.Dp(20), y), image.Pt(max(0, r.Max.X-x-gtx.Dp(20)), lh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Bold), Size: 15, Color: c.Link, MaxLines: 1}, text)
			})
		})
		it.hits = append(it.hits, itemHit{r: r, act: act, rec: parent.ID})
		x = r.Max.X + gtx.Dp(4)
	}
	word(nsLists.T("Subtask of"))
	name := ""
	if p := d.Meta.Primary(); p != nil {
		name = parent.Field(p.Key).PlainText()
	}
	if name == "" {
		name = nsLists.T("Untitled item")
	}
	link(name, "subtask-parent")
	if t := d.Info.Title; t != "" && (it.act.InList == nil || !it.act.InList(d.Info.ID)) {
		word(nsLists.T("in"))
		link(t, "subtask-list")
	}
	return y + lh + gtx.Dp(4)
}

// subtaskMenu is BaseSubtaskContextMenu ("Subtask actions"): Open
// subtask · Add comment / View comments · Copy link to subtask ─ Remind
// me about this ▸ ─ Delete subtask (not in a view-only list). Open in new
// window, Share subtask, Report to Slack and Get notifications ▸ are left
// out as in the record menu (records.go).
func (it *Item) subtaskMenu(d *core.ListDoc, s *lists.Record) []MenuItem {
	listID, recID := d.Info.ID, s.ID
	open := func() {
		if it.act.OpenItem != nil {
			it.act.OpenItem(listID, recID)
		}
	}
	items := []MenuItem{{Label: nsLists.T("Open subtask"), Icon: "sidebar", Do: open}}
	if n := it.commentCount(d, s); !(s.Archived && n == 0) {
		label := nsLists.T("Add comment")
		if n > 0 {
			label = nsLists.T("{count, plural, =1 {View comment} other {View comments}}", "count", n)
		}
		items = append(items, MenuItem{Label: label, Icon: "add-comment", Do: open})
	}
	if link := core.ListRecordLink(d.Info.Permalink, recID); link != "" {
		items = append(items, MenuItem{Label: nsLists.T("Copy link to subtask"), Icon: "link", Do: func() {
			if it.act.Copy != nil {
				it.act.Copy(link)
			}
			it.h.Toast(nsLists.T("Link copied to clipboard"))
		}})
	}
	if it.exp("lists_in_later") {
		items = append(items, MenuItem{Divider: true}, it.remindItem(listID, recID))
	}
	if d.Info.Editable {
		items = append(items, MenuItem{Divider: true}, MenuItem{Label: nsLists.T("Delete subtask"), Danger: true, Do: func() {
			it.recordWrite(func(ctx context.Context, a *core.App) error { return a.DeleteListRecord(ctx, listID, recID) })
		}})
	}
	return items
}

// editSubtaskName opens subtask s's name editor (tO's onMouseDown);
// Enter is ListRecordSubtasks' onEnter (Q): the next subtask's name, or
// after the last one a new subtask while under the limit.
func (it *Item) editSubtaskName(d *core.ListDoc, s *lists.Record, gtx layout.Context) {
	p := d.Meta.Primary()
	if p == nil {
		return
	}
	it.cellClick(d, s, p, 0, gtx)
	if !it.edit.editing(s.ID, p) {
		return
	}
	id := s.ID
	it.edit.done = func(how int) {
		it.mu.Lock()
		d := it.doc
		it.mu.Unlock()
		if how != doneEnter || d == nil || !d.Info.Editable {
			return
		}
		rec := d.Record(it.rec)
		if rec == nil || rec.Archived {
			return
		}
		subs := subtasksOf(d, it.rec)
		for i, x := range subs {
			if x.ID != id {
				continue
			}
			switch {
			case i+1 < len(subs):
				it.editNext = subs[i+1].ID
			case len(subs) < subtaskLimit:
				it.addSubtask(it.id, it.rec)
			}
			return
		}
	}
}

// addSubtask is Add Subtask: lists.records.create with the parent and no
// position (uTxJ), then the new subtask's name editor.
func (it *Item) addSubtask(listID, parent string) {
	a := it.h.Core()
	if a == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		r, err := a.CreateListRecord(ctx, listID, "", parent)
		it.post(func() {
			if err != nil {
				it.h.Toast(err.Error())
				return
			}
			it.mu.Lock()
			if d := a.CachedList(listID); d != nil {
				it.doc = d
			}
			it.mu.Unlock()
			it.editNext = r.ID
		})
	}()
}
