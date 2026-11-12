package core

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Each saved message's metadata, as Slack's web client keeps it on the
// message (message.saved) and in its saved-items store, from Slack's CDN
// build d3de3a487b1c (boot-shared):
//
//   - p6HR isSaved (Gs): any saved metadata. isTodo (Tx): todo_state
//     "to_do". isSavedAndCompleted (wq): a date_completed.
//     isSavedButUncompleted (P9), MessageLabels' isLater: saved and no
//     date_completed.
//   - qb0V isTodoWithoutReminder (dK): a to-do whose date_due is its
//     date_created (Add to-do sets both to now, bs). getRealDueDate: a
//     date_due over 1 that isn't date_created. isOverdue (JL): that date
//     is now or past. Ep: a due date to show (not archived, completed or a
//     to-do without a reminder).
//   - Oyqq runAddToLaterTodos, the to-do action's click (Yoyn) with
//     later_todos on: a to-do is completed (e_ toggleSavedMessageComplete
//     {mark: completed}: saved.update {date_due: 0, mark, todo_state:
//     completed}, the "To-do marked complete" toast); a completed item
//     goes back to the to-dos ({mark: uncompleted}: date_due its
//     date_created, todo_state to_do, "To-do added"); a saved bookmark
//     moves to the to-dos (tOLz runMoveTodoState: saved.update {todo_state:
//     to_do, date_due: date_created}, mark unarchived when archived,
//     "Moved to to-dos"); anything else is added (VJ addTodoMessage:
//     saved.add {todo_state: to_do, date_due: now}, "To-do added").
//
// Clack used to keep one bool per message (Message.Saved), so a row
// couldn't tell a to-do from a bookmark. The metadata comes from
// saved.list (loadSaved), the saved_added / saved_updated events and
// Clack's own writes.

// SavedMeta is one saved item's metadata (Slack's message.saved).
type SavedMeta struct {
	TodoState     string // to_do | saved | completed ("" on a legacy item)
	DateCreated   int64
	DateDue       int64
	DateCompleted int64
	Archived      bool
}

// IsTodo is p6HR isTodo: todo_state "to_do".
func (m SavedMeta) IsTodo() bool { return m.TodoState == "to_do" }

// Completed is p6HR isSavedAndCompleted: a date_completed.
func (m SavedMeta) Completed() bool { return m.DateCompleted != 0 }

// RealDueDate is qb0V getRealDueDate: date_due when it's over 1 and not
// date_created, else 0.
func (m SavedMeta) RealDueDate() int64 {
	if m.DateDue > 1 && m.DateDue != m.DateCreated {
		return m.DateDue
	}
	return 0
}

// Overdue is qb0V isOverdue at now (unix s).
func (m SavedMeta) Overdue(now int64) bool {
	d := m.RealDueDate()
	return d != 0 && d <= now
}

// ShowsDue is qb0V Ep: a due date that isn't archived, completed or a
// to-do without a reminder.
func (m SavedMeta) ShowsDue() bool {
	return m.DateDue != 0 && !m.Archived && !m.Completed() && !(m.IsTodo() && m.DateDue == m.DateCreated)
}

func savedMetaOf(it *savedRaw) SavedMeta {
	return SavedMeta{TodoState: it.TodoState, DateCreated: it.DateCreated, DateDue: it.DateDue,
		DateCompleted: it.DateCompleted, Archived: it.IsArchived}
}

// SavedMeta returns ch/ts's saved metadata; ok is false when the message
// isn't saved.
func (a *App) SavedMeta(ch, ts string) (SavedMeta, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.saved[savedKey(ch, ts)] {
		return SavedMeta{}, false
	}
	return a.savedMeta[savedKey(ch, ts)], true
}

// setSavedMeta stores ch/ts's metadata and marks it saved (which redraws
// the message).
func (a *App) setSavedMeta(ch, ts string, m SavedMeta) {
	a.mu.Lock()
	if a.savedMeta == nil {
		a.savedMeta = map[string]SavedMeta{}
	}
	a.savedMeta[savedKey(ch, ts)] = m
	a.mu.Unlock()
	a.markSaved(ch, ts, true)
}

// patchSavedMeta changes ch/ts's stored metadata (a zero one when there is
// none) and marks it saved.
func (a *App) patchSavedMeta(ch, ts string, f func(*SavedMeta)) {
	a.mu.Lock()
	m := a.savedMeta[savedKey(ch, ts)]
	a.mu.Unlock()
	f(&m)
	a.setSavedMeta(ch, ts, m)
}

// TodoAction is what ToggleTodo did, for its toast.
type TodoAction int

const (
	TodoAdded      TodoAction = iota // VJ addTodoMessage, or a completed item back in the to-dos
	TodoCompleted                    // a to-do marked complete
	TodoMovedToDos                   // a saved bookmark moved to the to-dos
)

// ToggleTodo is the to-do action's click (Oyqq runAddToLaterTodos, with
// later_todos on): complete a to-do, put a completed item back in the
// to-dos, move a bookmark to the to-dos, or add the message as a to-do.
// The metadata changes at once and goes back if the call fails.
func (a *App) ToggleTodo(ctx context.Context, ch, ts string) (TodoAction, error) {
	old, saved := a.SavedMeta(ch, ts)
	now := time.Now().Unix()
	undo := func() {
		if saved {
			a.setSavedMeta(ch, ts, old)
		} else {
			a.markSaved(ch, ts, false)
		}
	}
	update := func(p url.Values, m SavedMeta, act TodoAction) (TodoAction, error) {
		a.setSavedMeta(ch, ts, m)
		if err := a.C.Call(ctx, "saved.update", p, nil); err != nil {
			undo()
			return act, err
		}
		return act, nil
	}
	switch {
	case saved && old.IsTodo():
		// e_ {mark: completed}: date_completed now, todo_state completed,
		// date_due 0
		p := savedParams(ch, ts)
		p.Set("date_due", "0")
		p.Set("mark", "completed")
		p.Set("todo_state", "completed")
		m := old
		m.TodoState, m.DateCompleted, m.DateDue = "completed", now, 0
		return update(p, m, TodoCompleted)
	case saved && old.Completed():
		// e_ {mark: uncompleted}: todo_state to_do, date_due date_created
		p := savedParams(ch, ts)
		p.Set("date_due", strconv.FormatInt(old.DateCreated, 10))
		p.Set("mark", "uncompleted")
		p.Set("todo_state", "to_do")
		m := old
		m.TodoState, m.DateCompleted, m.DateDue = "to_do", 0, old.DateCreated
		return update(p, m, TodoAdded)
	case saved:
		// tOLz runMoveTodoState {targetTodoState: to_do}
		p := savedParams(ch, ts)
		p.Set("todo_state", "to_do")
		p.Set("date_due", strconv.FormatInt(old.DateCreated, 10))
		if old.Archived {
			p.Set("mark", "unarchived")
		}
		m := old
		m.TodoState, m.DateDue, m.Archived = "to_do", old.DateCreated, false
		return update(p, m, TodoMovedToDos)
	}
	// VJ addTodoMessage: dateDue now, date_created the same (bs), todo_state
	// to_do (Qq: a to-do without a reminder)
	a.setSavedMeta(ch, ts, SavedMeta{TodoState: "to_do", DateCreated: now, DateDue: now})
	p := savedParams(ch, ts)
	p.Set("todo_state", "to_do")
	p.Set("date_due", strconv.FormatInt(now, 10))
	if err := a.C.Call(ctx, "saved.add", p, nil); err != nil && !isCode(err, "already_saved") {
		undo()
		return TodoAdded, err
	}
	return TodoAdded, nil
}
