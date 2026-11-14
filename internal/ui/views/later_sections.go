package views

import (
	"context"
	"encoding/json"
	"image"
	"slices"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
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
// The dividers are Tab stops between the sections' lists (below). A
// collapsed section opens when the selected to-do is in it (todoSelection).

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

// todoSectioned is the sectioned page: later_todos (6owI tj).
func (s *savedBase) todoSectioned() bool {
	c := s.h.Core()
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
			return l.para(gtx, 15, 22.5, wReg, l.p.Muted, 0, strings.Replace(nsLater.T("You’re all caught up :tada:"), ":tada:", "🎉", 1)) // NoInProgressTodosRow
		})
	}
	sec, label, fg := todoSecInProgress, nsLater.T("In progress"), l.p.Text
	switch r.kind {
	case tdOverdue:
		sec, label = todoSecOverdue, nsLater.T("Overdue")
	case tdCompleted:
		sec, label, fg = todoSecCompleted, nsLater.T("Completed"), l.p.Muted
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
	if c, ok := l.secClick[sec].Update(gtx); ok {
		l.secCollapsed[sec] = !l.secCollapsed[sec]
		if c.NumClicks > 0 && !gtx.Focused(&l.secClick[sec]) {
			// a click focuses the button (no :focus-visible ring)
			l.sk.clicked[sec] = 2
			gtx.Execute(key.FocusCmd{Tag: &l.secClick[sec]})
		}
		l.h.Invalidate()
	}
	ring := l.todoDividerFocus(gtx, sec)
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
		d := layout.Inset{Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
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
		if ring {
			// .dividerToggle[aria-expanded]:focus-visible:after: inset 5px,
			// radius 8
			in := gtx.Dp(5)
			l.vkRing(gtx, image.Rect(in, in, d.Size.X-in, d.Size.Y-in), gtx.Dp(8))
		}
		return d
	})
	return D{Size: image.Pt(w, top+gtx.Dp(32))}
}

// todoNow is the page's clock for the overdue test (seconds).
func todoNow() int64 { return time.Now().Unix() }

// ---- a keyboard list per section (TodosPage, vAPb) ----
//
// TodosPage (client-later-list-view vAPb, build d3de3a487b1c; the
// unified-triage copy is ZuTx) renders each section's to-dos in its own
// InteractiveList (boot-shared DERT) after its divider (nx89, a button),
// so the page's tab order is: Overdue's divider, its list, In progress's
// divider, its list, Completed's divider, its list. A list is one Tab
// stop, its roving item the list's initialActiveItem (S: the first row,
// then the last one focused; when S goes, the row at its old index). A
// collapsed section's list is hidden, so neither its rows nor its stop
// are there. The keys inside a list (todoKey) stay in it.
//
// When the focused row leaves its list (cleared, completed, archived),
// TodosPage's effect focuses the row now at its old index in that list,
// or, with that list empty, the page (rn: the first non-empty list's
// initial row, else the first divider).
//
// Clack draws every section in one widget.List with one key focus tag
// (Later.kb) for the rows: todoSecKB keeps each list's keys and initial
// row, and Tab and Shift+Tab move between the stops with a key filter
// that is only there while one of the stops has focus and has a stop
// after (before) it, so Tab still leaves the page at either end.
//
// Each section's rows are capped at 50 (eI) and grow by 50 when its
// sentinel scrolls into view; End in a capped section shows them all and
// focuses the last (onListKeyDownCapture). Clack's list is virtual and
// draws every loaded row, so End reaches the same row.

// todoSecKB is the per-section keyboard state.
type todoSecKB struct {
	keys, keysPrev [3][]string    // each section's to-do keys, this frame and last
	of, ofPrev     map[string]int // key → section
	init           [3]string      // each list's initialActiveItem (S)
	initAt         [3]int         // its index (DERT's I.current)
	divElem        [3]int         // the divider's list element (-1: none)
	drawn, drawnP  [3]bool        // the divider was laid out this frame / last frame
	hadFocus       [3]bool        // the divider held key focus last frame
	ring           [3]bool        // the divider's focus came from the keyboard (:focus-visible)
	clicked        [3]int         // a click is focusing the divider (frames left)
	pend           int            // a divider to focus once it's laid out (-1: none)
	pendRing       bool           // …with the ring
}

// todoDivKey is a divider's key in Later.kb's items (not focusable; it
// lets the list reveal the divider).
func todoDivKey(sec int) string { return "div:" + strconv.Itoa(sec) }

// todoKindSec is a divider row's section.
func todoKindSec(kind int) (int, bool) {
	switch kind {
	case tdOverdue:
		return todoSecOverdue, true
	case tdInProgress:
		return todoSecInProgress, true
	case tdCompleted:
		return todoSecCompleted, true
	}
	return 0, false
}

// todoSecItems lists the rows for Later.kb and records each section's
// keys and divider; without later_todos every row is in In progress.
func (l *Later) todoSecItems(rows []todoRow) []vkItem {
	sk := &l.sk
	sk.keysPrev, sk.ofPrev = sk.keys, sk.of
	sk.keys = [3][]string{}
	sk.of = make(map[string]int, len(rows))
	sk.divElem = [3]int{-1, -1, -1}
	items := make([]vkItem, 0, len(rows))
	sec := todoSecInProgress
	for ri, r := range rows {
		if s, ok := todoKindSec(r.kind); ok {
			sec = s
			sk.divElem[s] = ri
			items = append(items, vkItem{key: todoDivKey(s), elem: ri})
			continue
		}
		if r.kind != tdItem {
			continue
		}
		k := laterKey(&l.view[r.item])
		sk.keys[sec] = append(sk.keys[sec], k)
		sk.of[k] = sec
		items = append(items, vkItem{key: k, elem: ri, focusable: true})
	}
	return items
}

// todoSecInit keeps each list's initial row valid (DERT's effect: the
// first row to begin with; when it goes, the row at its old index).
func (l *Later) todoSecInit() {
	sk := &l.sk
	for s := range sk.keys {
		ids := sk.keys[s]
		i := slices.Index(ids, sk.init[s])
		if i < 0 {
			if len(ids) == 0 {
				sk.init[s] = ""
				continue
			}
			i = min(sk.initAt[s], len(ids)-1)
			sk.init[s] = ids[i]
		}
		sk.initAt[s] = i
	}
}

// todoFocused is Later.kb's onFocus: the row becomes its list's initial
// one (setInitialActiveItem as it takes focus).
func (l *Later) todoFocused(key string, _ bool) {
	if s, ok := l.sk.of[key]; ok {
		l.sk.init[s] = key
		l.sk.initAt[s] = slices.Index(l.sk.keys[s], key)
	}
}

// kbSec is the section of the list Later.kb stands for: the active
// row's, else the initial row's.
func (l *Later) kbSec() int {
	if s, ok := l.sk.of[l.kb.active]; ok {
		return s
	}
	if s, ok := l.sk.of[l.kb.initial]; ok {
		return s
	}
	return todoSecInProgress
}

// todoRefocus is TodosPage's effect for a focused row that left its
// list: the key to focus now ("" for none; with every list empty the
// first divider is focused once laid out).
func (l *Later) todoRefocus() string {
	k, sk := l.kb, &l.sk
	if !k.focused || k.active == "" {
		return ""
	}
	old, had := sk.ofPrev[k.active]
	if !had {
		return ""
	}
	if now, ok := sk.of[k.active]; ok && now == old {
		return ""
	}
	if ids := sk.keys[old]; len(ids) > 0 {
		i := max(slices.Index(sk.keysPrev[old], k.active), 0)
		return ids[min(i, len(ids)-1)]
	}
	// rn: the overdue or in-progress list with rows, else completed's,
	// at its initial row; else the first divider
	for _, s := range []int{todoSecOverdue, todoSecInProgress, todoSecCompleted} {
		if ids := sk.keys[s]; len(ids) > 0 {
			if slices.Contains(ids, sk.init[s]) {
				return sk.init[s]
			}
			return ids[0]
		}
	}
	for s, e := range sk.divElem {
		if e >= 0 {
			sk.pend, sk.pendRing = s, k.ring
			l.h.Invalidate()
			break
		}
	}
	return ""
}

// todoStop is one Tab stop: a divider or a section's list.
type todoStop struct {
	div bool
	sec int
}

// todoStops is the page's tab order this frame.
func (l *Later) todoStops() []todoStop {
	var st []todoStop
	for s := range 3 {
		if l.sk.divElem[s] >= 0 {
			st = append(st, todoStop{div: true, sec: s})
		}
		if len(l.sk.keys[s]) > 0 {
			st = append(st, todoStop{sec: s})
		}
	}
	return st
}

// todoTab moves key focus between the stops (call before Later.kb's
// update).
func (l *Later) todoTab(gtx C) {
	sk := &l.sk
	if p := sk.pend; p >= 0 && sk.drawnP[p] {
		sk.pend = -1
		sk.ring[p], sk.clicked[p] = sk.pendRing, 0
		if !sk.pendRing {
			sk.clicked[p] = 2
		}
		gtx.Execute(key.FocusCmd{Tag: &l.secClick[p]})
	}
	stops := l.todoStops()
	cur := -1
	for i, st := range stops {
		if st.div && gtx.Focused(&l.secClick[st.sec]) || !st.div && gtx.Focused(l.kb) && st.sec == l.kbSec() {
			cur = i
		}
	}
	if cur < 0 {
		return
	}
	var fs []event.Filter
	if cur+1 < len(stops) {
		fs = append(fs, key.Filter{Name: key.NameTab})
	}
	if cur > 0 {
		fs = append(fs, key.Filter{Name: key.NameTab, Required: key.ModShift})
	}
	if len(fs) == 0 {
		return
	}
	for {
		ev, ok := gtx.Event(fs...)
		if !ok {
			return
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		to := cur + 1
		if e.Modifiers.Contain(key.ModShift) {
			to = cur - 1
		}
		if to < 0 || to >= len(stops) {
			continue
		}
		l.todoFocusStop(gtx, stops[to])
		cur = to
	}
}

// todoFocusStop gives a stop key focus with the keyboard's ring: a
// divider (revealed first; one that wasn't laid out last frame gets focus
// once it is), or a list at its initial row (InteractiveList.focus()).
func (l *Later) todoFocusStop(gtx C, st todoStop) {
	k, sk := l.kb, &l.sk
	if st.div {
		k.reveal(todoDivKey(st.sec))
		if !sk.drawnP[st.sec] {
			sk.pend, sk.pendRing = st.sec, true
			l.h.Invalidate() // the next frame focuses it
			return
		}
		sk.ring[st.sec], sk.clicked[st.sec] = true, 0
		gtx.Execute(key.FocusCmd{Tag: &l.secClick[st.sec]})
		return
	}
	ids := sk.keys[st.sec]
	target := sk.init[st.sec]
	if !slices.Contains(ids, target) {
		target = ids[0]
	}
	k.setActive(target, true)
	k.reveal(target)
	gtx.Execute(key.FocusCmd{Tag: k})
}

// frame starts the frame's divider bookkeeping: drawnP is what the last
// layout drew (call before todoTab).
func (sk *todoSecKB) frame() {
	sk.drawnP, sk.drawn = sk.drawn, [3]bool{}
}

// todoDividerFocus tracks a divider's key focus while it's laid out: a
// focus that a click didn't give is the keyboard's (the ring shows);
// reports whether the ring shows.
func (l *Later) todoDividerFocus(gtx C, sec int) bool {
	sk := &l.sk
	sk.drawn[sec] = true
	f := gtx.Focused(&l.secClick[sec])
	switch {
	case f && !sk.hadFocus[sec]:
		sk.ring[sec] = sk.clicked[sec] == 0
		sk.clicked[sec] = 0
	case !f:
		sk.ring[sec] = false
		sk.clicked[sec] = max(sk.clicked[sec]-1, 0)
	}
	sk.hadFocus[sec] = f
	return f && sk.ring[sec]
}

// ---- the selected to-do and the tombstone (gap-w26) ----

// todoSel is vAPb's memory for the selected to-do: its rS / rT refs, the
// to-do whose collapsed section the page opened and the one it scrolled
// to. The selected row (rk, and the row's own isSelected: Wowu over qb0V
// Ub) is the to-do whose message the sibling view (primary or secondary,
// getSiblingView) shows: here the secondary column, where a row's click
// (later_beside.go) and LaterLabel's "View to-dos" (wRj5, ui
// App.ViewTodo) open it.
type todoSel struct {
	key           string // laterKey
	opened, shown bool   // the section opened, the row scrolled to
}

// todoSelection is the selected row's key ("" while the column shows no
// to-do's message) and vAPb's effect on it: a collapsed section holding it
// opens (setTodoSectionCollapsed false, once per to-do), then the row
// scrolls into view, block nearest (t3), once. The refs outlive the
// selection: the same to-do selected again does neither.
func (l *Later) todoSelection(rows []todoRow) string {
	k := l.selectedKey()
	if k == "" {
		return ""
	}
	if k != l.sel.key {
		l.sel = todoSel{key: k}
	}
	at := slices.IndexFunc(l.view, func(it core.SavedItem) bool { return laterKey(&it) == l.sel.key })
	if at < 0 {
		return l.sel.key
	}
	shown := slices.ContainsFunc(rows, func(r todoRow) bool { return r.kind == tdItem && r.item == at })
	if !shown && !l.sel.opened {
		it := &l.view[at]
		sec := todoSecInProgress
		switch {
		case laterCompleted(it):
			sec = todoSecCompleted
		case l.h.Core() != nil && l.h.Core().Experiment("activity_in_home_prototype") == "on" && todoOverdueRow(it, todoNow()):
			sec = todoSecOverdue
		}
		l.sel.opened = true
		if l.secCollapsed[sec] {
			l.secCollapsed[sec] = false
			l.h.Invalidate() // the rows come back next frame
		}
		return l.sel.key
	}
	if shown && !l.sel.shown {
		l.sel.shown = true
		l.kb.reveal(l.sel.key)
	}
	return l.sel.key
}

// laterTombstoned is openLaterRemoveModal's test for a message (Kivd):
// the savedItem's isTombstoned or the stored message a tombstone
// (ZnG4 sO, R+b2 aK). Core marks a saved message Gone when messages.list
// answered without it; one that couldn't be looked up (an error) has no
// message and isn't Gone, and Kivd, finding no message, opens no modal.
func laterTombstoned(it *core.SavedItem) bool {
	if it.ItemType != "message" || it.Channel == "" || it.TS == "" {
		return false
	}
	if m := it.Msg; m != nil {
		return m.Tombstone || m.Subtype == "tombstone"
	}
	return it.Gone
}

// openRemoveModal is openLaterRemoveModal (boot-shared Kivd) with
// LaterRemoveModal (6Fvj) over w6zd's copy, for a tombstoned message: it
// reports whether it opened. With later_todos (6owI tj) a to-do's title is
// "Remove to-do" and its text D9's "A message you added as a to-do was
// deleted. Remove it from To-dos?" (U4's "… you saved …" for a saved
// item, whose title stays "Remove saved item"); without it "Remove saved
// item" and "{removedActionText} Remove it from Later?". Cancel, then
// Remove (danger), which unsaves the message. To-dos' Enter and a To-dos,
// Saved or peek row's click run it (later_beside.go rowClick).
func (s *savedBase) openRemoveModal(it core.SavedItem) bool {
	if !laterTombstoned(&it) {
		return false
	}
	ph, ok := s.h.(popups.Host)
	if !ok {
		return false
	}
	title, body := laterRemoveCopy(&it, s.todoSectioned())
	k, ch, ts := laterKey(&it), it.Channel, it.TS
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: title,
		Body: func(gtx C) D {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return s.para(gtx, 15, 22, wReg, s.p.Text, 0, body) })
		},
		Buttons: []*popups.ModalButton{
			{Label: nsLater.T("Cancel"), Kind: popups.BtnOutline},
			{Label: nsLater.T("Remove"), Kind: popups.BtnDanger, Do: func() bool {
				s.remove(k)
				s.write(k, "", true, func(ctx context.Context, c *core.App) error { return c.Unsave(ctx, ch, ts) })
				return true
			}},
		},
	}))
	return true
}

// laterRemoveCopy is LaterRemoveModal's title and text for a deleted
// message (6Fvj U and q over w6zd; R, the message deleted, holds for a
// tombstone). todos is later_todos (6owI tj).
func laterRemoveCopy(it *core.SavedItem, todos bool) (title, body string) {
	saved := it.TodoState == "saved" // qb0V I0
	if !todos {
		// translateString(ki(Qm(message), R, "")): the English copy goes
		// through "{text}" untranslated
		return nsLater.T("Remove saved item"), nsLater.T("{removedActionText} Remove it from Later?",
			"removedActionText", nsLater.T("{text}", "text", "A message you saved was deleted."))
	}
	if saved { // U4, and the title stays P
		return nsLater.T("Remove saved item"), nsLater.T("A message you saved was deleted. Remove it from Saved?")
	}
	return nsLater.T("Remove to-do"), nsLater.T("A message you added as a to-do was deleted. Remove it from To-dos?") // B (R), D9
}
