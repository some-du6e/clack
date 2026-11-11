package views

import (
	"encoding/json"
	"image"
	"strconv"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
)

// The To-dos page's sections, from Slack's TodosPage (the todos-page
// chunk, ZuTx; build 4d0c78085232) and the rows it is built from:
//
//   - With later_todos (6owI tj) the to-dos come as the in-progress ones
//     the status filter shows, then the completed ones (W9x8 n), and the
//     rows (+Vcf V) open the in-progress run with "In progress" and the
//     completed run with "Completed". When the status filter shows both,
//     no in-progress to-do is visible and some are completed
//     (getHasNoInProgressTodos), "In progress" stands over
//     NoInProgressTodosRow, "You’re all caught up 🎉" (sIGl).
//   - Under activity_in_home_prototype (6rqO j) the overdue to-dos (a due
//     date that isn't the creation date, at or before now; not completed
//     or archived: qb0V Ep, JL) move out of "In progress" into "Overdue"
//     at the top (WfFj OD), and "In progress" follows only with to-dos
//     left or more open ones on the server.
//   - Each divider (nx89) is a button that collapses its section (the
//     rows go, the divider stays: WfFj ib): .dividerToggle (padding 8px
//     20px, 8px gaps) with a 16px caret-down / caret-right in
//     content-ter, the label and the count in 13px bold (1.25): Overdue
//     and In progress in content-pry, Completed in content-sec, the count
//     content-ter. Its counts: Overdue the overdue rows, In progress the
//     server's open to-do count (getTodoCount; the loaded ones without
//     it) less the overdue ones, Completed the server's completed count
//     (getCompletedTabCount).
//   - The open sections (.openSection) end with 32px; the completed
//     divider sits on .completedDivider (base-sec in light, transparent in
//     dark, a 1px otl-ter line on top, 4px over the toggle).
//
// Not ported: Tab focus on the dividers (they are buttons outside the
// InteractiveList; Clack toggles them with the mouse), and Slack opening a
// collapsed section when the selected to-do is in it.

const (
	tdItem = iota
	tdOverdue
	tdInProgress
	tdCompleted
	tdNoInProgress
	tdSectionEnd
)

// todoSection indexes Later.secCollapsed (getIsTodoSectionCollapsed).
const (
	todoSecOverdue = iota
	todoSecInProgress
	todoSecCompleted
)

// todoRow is one element of the To-dos list.
type todoRow struct {
	kind, item, count int
}

// todoOverdueRow is WfFj isOverdueRow: qb0V Ep (a due date, not
// archived or completed, not a to-do without a reminder) and JL (the real
// due date at or before now).
func todoOverdueRow(it *core.SavedItem, now int64) bool {
	if it.DateDue == 0 || laterCompleted(it) || it.Archived {
		return false
	}
	real := it.DateDue
	if real <= 1 || real == it.DateCreated {
		return false // getRealDueDate 0: no reminder
	}
	return real <= now
}

// todoCounts are saved.list's counts for the page (getTodoCount,
// getCompletedTabCount).
type todoCounts struct{ todo, completed int }

func (l *Later) todoCounts(inprog, done int) todoCounts {
	var c struct {
		Todo       *int `json:"todo_count"`
		TodoC      *int `json:"todoCount"`
		Completed  *int `json:"completed_count"`
		CompletedC *int `json:"completedCount"`
	}
	_ = json.Unmarshal(l.feed.counts, &c)
	out := todoCounts{todo: inprog, completed: done}
	switch {
	case c.Todo != nil:
		out.todo = *c.Todo
	case c.TodoC != nil:
		out.todo = *c.TodoC
	}
	switch {
	case c.Completed != nil:
		out.completed = *c.Completed
	case c.CompletedC != nil:
		out.completed = *c.CompletedC
	}
	return out
}

// todoSectioned is the sectioned page: later_todos.
func (l *Later) todoSectioned() bool {
	c := l.h.Core()
	return c != nil && c.Experiment("later_todos") == "on"
}

// todoRows is the list's rows over l.view (in-progress first, then the
// completed ones: rebuild).
func (l *Later) todoRows(now int64) []todoRow {
	if !l.todoSectioned() {
		rows := make([]todoRow, len(l.view))
		for i := range rows {
			rows[i] = todoRow{kind: tdItem, item: i}
		}
		return rows
	}
	var inprog, done []int
	for i := range l.view {
		if laterCompleted(&l.view[i]) {
			done = append(done, i)
		} else {
			inprog = append(inprog, i)
		}
	}
	counts := l.todoCounts(len(inprog), len(done))
	var rows []todoRow
	section := func(kind, sec, count int, items []int, end bool) {
		rows = append(rows, todoRow{kind: kind, count: count})
		if !l.secCollapsed[sec] {
			for _, i := range items {
				rows = append(rows, todoRow{kind: tdItem, item: i})
			}
		}
		if end {
			rows = append(rows, todoRow{kind: tdSectionEnd})
		}
	}
	var overdue, rest []int
	if c := l.h.Core(); c != nil && c.Experiment("activity_in_home_prototype") == "on" {
		for _, i := range inprog {
			if todoOverdueRow(&l.view[i], now) {
				overdue = append(overdue, i)
			} else {
				rest = append(rest, i)
			}
		}
	}
	wantIn, wantDone := l.statusWant()
	switch {
	case len(overdue) > 0:
		section(tdOverdue, todoSecOverdue, len(overdue), overdue, true)
		if len(rest) > 0 || counts.todo > len(overdue) {
			section(tdInProgress, todoSecInProgress, max(counts.todo-len(overdue), 0), rest, true)
		}
	case len(inprog) > 0:
		section(tdInProgress, todoSecInProgress, counts.todo, inprog, true)
	case wantIn && wantDone && len(done) > 0: // getHasNoInProgressTodos
		rows = append(rows, todoRow{kind: tdInProgress, count: counts.todo})
		if !l.secCollapsed[todoSecInProgress] {
			rows = append(rows, todoRow{kind: tdNoInProgress})
		}
		rows = append(rows, todoRow{kind: tdSectionEnd})
	}
	if len(done) > 0 || wantDone && counts.completed > 0 && l.feed.next != "" {
		section(tdCompleted, todoSecCompleted, counts.completed, done, false)
	}
	return rows
}

// todoSectionRow draws a divider, the empty in-progress row or a
// section's end.
func (l *Later) todoSectionRow(gtx C, r todoRow) D {
	w := gtx.Constraints.Max.X
	switch r.kind {
	case tdSectionEnd: // .openSection__Ul2gF padding-bottom
		return D{Size: image.Pt(w, gtx.Dp(32))}
	case tdNoInProgress: // .noInProgressRow__KPFSW: padding 8px 20px 24px, 15px/1.5 content-sec
		return layout.Inset{Top: 8, Bottom: 24, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return l.para(gtx, 15, 22.5, wReg, l.p.Muted, 0, "You’re all caught up 🎉")
		})
	}
	sec, label, fg := todoSecInProgress, "In progress", l.p.Text
	switch r.kind {
	case tdOverdue:
		sec, label = todoSecOverdue, "Overdue"
	case tdCompleted:
		sec, label, fg = todoSecCompleted, "Completed", l.p.Muted
	}
	top := 0
	if r.kind == tdCompleted { // .completedDivider__xKFxD
		hl := max(1, gtx.Dp(1))
		h := hl + gtx.Dp(4) + gtx.Dp(32)
		bg := l.p.Page
		if l.th.Dark {
			bg = l.p.Bg
		}
		fill(gtx, image.Rect(0, 0, w, h), bg)
		fill(gtx, image.Rect(0, 0, w, hl), l.p.Border)
		top = hl + gtx.Dp(4)
	}
	if l.secClick[sec].Clicked(gtx) {
		l.secCollapsed[sec] = !l.secCollapsed[sec]
		l.h.Invalidate()
	}
	caret := "caret-down"
	if l.secCollapsed[sec] {
		caret = "caret-right"
	}
	defer opOffset(gtx, image.Pt(0, top)).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, gtx.Dp(32)))
	l.secClick[sec].Layout(g, func(gtx C) D {
		pointerCursor(gtx)
		if l.secClick[sec].Hovered() { // :hover, base-pry-hover
			fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, l.p.Hover)
		}
		return layout.Inset{Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return l.icon(gtx, caret, 16, l.p.Faint) }),
					hgap(8),
					layout.Rigid(func(gtx C) D { return l.label(gtx, 13, wBold, fg, label) }),
					hgap(8),
					layout.Rigid(func(gtx C) D { return l.label(gtx, 13, wBold, l.p.Faint, strconv.Itoa(r.count)) }),
				)
			})
		})
	})
	return D{Size: image.Pt(w, top+gtx.Dp(32))}
}

// todoNow is the page's clock for the overdue test (seconds).
func todoNow() int64 { return time.Now().Unix() }
