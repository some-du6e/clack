package popups

import (
	"context"
	"errors"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
)

// The to-do and reminder actions' names, from Slack's CDN build
// d3de3a487b1c (boot-shared), which the hover toolbar, the right-click
// menu and the ⋯ menu share:
//
//   - The to-do action (Yoyn, the saved_for_later I18n): "Complete to-do"
//     when the message is a to-do (p6HR isTodo: saved.todo_state
//     "to_do"), else "Add to-do", so a saved bookmark or a completed item
//     still reads "Add to-do". Its icon: check-add, or for a to-do
//     check-circle-filled in the hover toolbar (with className
//     c-message_actions__button--complete_todo) and check in a menu.
//   - The reminder action (ih0g, the message I18n): with later_todos,
//     "Edit reminder" on a to-do, else "Add a reminder" ("Remind me"
//     without later_todos, which Clack's menus don't draw: they're the
//     to-dos layout, Add to saved ⇧⌘S included).
//   - The click (Oyqq runAddToLaterTodos, core ToggleTodo) and its toast
//     (9XxT maybeEnqueueLaterTodoToast, 43Gs's text): "To-do marked
//     complete", "To-do added", "Moved to to-dos"; a failure toasts by
//     the error code (TodoToast).

// IsTodo is p6HR isTodo for m: its saved metadata's todo_state is to_do.
func IsTodo(app *core.App, m *core.Message) bool {
	if app == nil || m == nil {
		return false
	}
	meta, ok := app.SavedMeta(m.Channel, m.TS)
	return ok && meta.IsTodo()
}

// TodoAction is the to-do action's name and icons for m (Yoyn).
type TodoAction struct {
	Label     string // "Add to-do" or "Complete to-do" (saved_for_later)
	MenuIcon  string // check-add (Clack's todo-add) or check
	HoverIcon string // check-add or check-circle-filled
	Complete  bool   // c-message_actions__button--complete_todo
}

// TodoActionFor is Yoyn's action for m.
func TodoActionFor(app *core.App, m *core.Message) TodoAction {
	if IsTodo(app, m) {
		return TodoAction{Label: "Complete to-do", MenuIcon: "check", HoverIcon: "check-circle-filled", Complete: true}
	}
	return TodoAction{Label: "Add to-do", MenuIcon: "todo-add", HoverIcon: "todo-add"}
}

// ReminderLabel is ih0g's name for the reminder row on m (the message
// I18n's source string).
func ReminderLabel(app *core.App, m *core.Message) string {
	if IsTodo(app, m) {
		return "Edit reminder"
	}
	return "Add a reminder"
}

// TodoToast is the toast for what ToggleTodo did (43Gs, saved_for_later):
// LaterTodoToast on success; on failure LaterTodoErrorToast (Kk) by the
// API's error code, or for a failed completion the generic
// BulkActionErrorToast (ULct, Oyqq's maybeEnqueueLaterTodoGenericErrorToast).
func TodoToast(act core.TodoAction, err error) string {
	if err == nil {
		switch act {
		case core.TodoCompleted:
			return nsSaved.T("To-do marked complete")
		case core.TodoMovedToDos:
			return nsSaved.T("Moved to to-dos")
		}
		return nsSaved.T("To-do added")
	}
	if act == core.TodoCompleted {
		return nsSaved.T("Something went wrong. Please try again.")
	}
	var se *slack.Error
	code := ""
	if errors.As(err, &se) {
		code = se.Code
	}
	move := act == core.TodoMovedToDos
	switch code {
	case "permission_denied":
		return nsSaved.T("You don’t have permission to do that.")
	case "channel_not_found", "message_not_found", "file_not_found", "section_not_found":
		if move {
			return nsSaved.T("Couldn’t move to your to-dos. The item couldn’t be found.")
		}
		return nsSaved.T("Couldn’t add to your to-dos. The item couldn’t be found.")
	case "already_saved":
		return nsSaved.T("This is already in your to-dos.")
	case "description_too_long":
		return nsSaved.T("Couldn’t add to your to-dos. The description is too long.")
	}
	if move {
		return nsSaved.T("Couldn’t move to your to-dos. Try again?")
	}
	return nsSaved.T("Couldn’t add to your to-dos. Try again?")
}

// RunTodoAction runs the to-do action on m (Oyqq) off the frame and
// toasts the result (or the failure) on it.
func RunTodoAction(h Host, m core.Message) {
	app := h.Core()
	if app == nil {
		return
	}
	ch, ts := m.Channel, m.TS
	run(h, func(ctx context.Context) (core.TodoAction, error) { return app.ToggleTodo(ctx, ch, ts) }, func(act core.TodoAction, err error) {
		h.Toast(TodoToast(act, err))
	})
}

// The ⋯ menu's save row (oMi4, the saved_for_later I18n, with
// later_todos): "Move to Saved" on a to-do, or on a completed item when
// the saved page is on (6owI hq: later_todos and the has_starred_item
// pref), "Remove from Saved" on any other saved item, else "Add to
// saved"; the icon bookmark, or bookmark-filled to remove. Its click
// (aSBj messageActionsLaterAction, no reminder time): Move to Saved is
// tOLz runMoveTodoState to "saved" ("To-do moved to Saved"), Remove from
// Saved is removeSavedMessage (saved.delete; "Saved item cleared" for a
// bookmark, "To-do cleared" for a to-do), Add to saved is addSavedMessage
// (saved.add, "Added to Saved").

// SaveAction is the save row's name, icon and what its click does.
type SaveAction struct {
	Label, Icon string
	Do          int // SaveAdd, SaveRemove or SaveMove
}

const (
	SaveAdd = iota
	SaveRemove
	SaveMove
)

// SaveActionFor is oMi4's row for m.
func SaveActionFor(app *core.App, m *core.Message) SaveAction {
	var meta core.SavedMeta
	saved := false
	synth := false
	if app != nil && m != nil {
		meta, saved = app.SavedMeta(m.Channel, m.TS)
		synth = app.Prefs().HasStarredItem
	}
	switch {
	case saved && meta.IsTodo(), saved && synth && meta.Completed():
		return SaveAction{Label: "Move to Saved", Icon: "bookmark", Do: SaveMove}
	case saved:
		return SaveAction{Label: "Remove from Saved", Icon: "bookmark-filled", Do: SaveRemove}
	}
	return SaveAction{Label: "Add to saved", Icon: "bookmark", Do: SaveAdd}
}

// RunSaveAction runs the save row's click on m and toasts as Slack does.
func RunSaveAction(h Host, m core.Message) {
	app := h.Core()
	if app == nil {
		h.Toast("Not connected yet")
		return
	}
	sa := SaveActionFor(app, &m)
	meta, _ := app.SavedMeta(m.Channel, m.TS)
	ch, ts := m.Channel, m.TS
	run(h, func(ctx context.Context) (struct{}, error) {
		switch sa.Do {
		case SaveMove:
			return struct{}{}, app.MoveToSaved(ctx, ch, ts)
		case SaveRemove:
			return struct{}{}, app.Unsave(ctx, ch, ts)
		}
		return struct{}{}, app.Save(ctx, ch, ts, 0)
	}, func(_ struct{}, err error) {
		switch {
		case err != nil && sa.Do == SaveMove:
			h.Toast(nsSaved.T("Couldn’t move to Saved. Try again?"))
		case err != nil && sa.Do == SaveRemove && meta.TodoState == "saved":
			h.Toast(nsSaved.T("Couldn’t clear the saved item. Try again?"))
		case err != nil && sa.Do == SaveRemove:
			h.Toast(nsSaved.T("Couldn’t clear your to-do. Try again?"))
		case err != nil:
			h.Toast(nsSaved.T("Couldn’t add to your to-dos. Try again?"))
		case sa.Do == SaveMove:
			h.Toast(nsSaved.T("To-do moved to Saved"))
		case sa.Do == SaveRemove && meta.TodoState == "saved":
			h.Toast(nsSaved.T("Saved item cleared"))
		case sa.Do == SaveRemove && (meta.TodoState == "to_do" || meta.TodoState == "completed"):
			h.Toast(nsSaved.T("To-do cleared"))
		case sa.Do == SaveAdd:
			h.Toast(nsSaved.T("Added to Saved"))
		}
	})
}
