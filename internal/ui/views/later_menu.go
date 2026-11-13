package views

// A saved item's More actions menu, on the To-dos / Saved pages and on the
// rail's Later peek (web client build f132f0e78468): the row's actions
// (boot-shared z03r) end with ActionsMenu (GrkO), "More actions", the
// ellipsis-vertical-filled button opening SavedItemActionsMenu (r8eO)
// 240px wide at its bottom-right, offsetY 6. Saved items have no context
// menu (LaterList and the pages' lists give their rows none).
//
// SavedItemActionsMenu's rows, in order, for the items Clack lists
// (messages; later_todos is eV, has_starred_item's saved page ez, the peek
// eQ), stripTerminalSeparators:
//
//	without later_todos: Open in split view (not in the peek), Open in
//	  new window (Clack has one window), a separator, Open in Home and a
//	  separator, (the peek:) Remind me about this ▸ and a separator,
//	  Archive / Unarchive (not completed), Move to in progress
//	  (completed), "Remove from Later" (danger) unless it's the To-dos tab
//	  and the item is neither completed nor archived
//	with later_todos: Mark complete (to_do), Mark incomplete (neither
//	  to_do nor, with ez, saved: completed, or saved without ez), Move to
//	  to-dos (saved, with ez), Add reminder ▸ (a to-do without one) or
//	  Edit reminder ▸ (a to-do with one), Move to saved (not saved), a
//	  separator (not in the peek), Open in Home, Open in split view (not
//	  in the peek), a separator, "Clear" (danger)
//
// Open in Home goes to the message (its thread for a reply) in Home, then
// maybeClosePeek. The reminder submenu is RemindMenu: the ReminderTimeMenu
// times, Custom… and, for a due to-do, Clear reminder.
//
// Not ported: Open in new window (one window), the reminder and Salesforce
// task items' rows (Edit to-do; Clack lists messages), Open in Slackbot.

import (
	"context"
	"image"
	"time"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// savedMenuOpen opens it's SavedItemActionsMenu at btn (the row's ⋮).
func (s *savedBase) savedMenuOpen(it core.SavedItem, r *laterRow, btn *widget.Clickable) {
	ph, ok := s.h.(popups.Host)
	if !ok {
		return
	}
	items := s.savedMenu(ph, it)
	if len(items) == 0 {
		return
	}
	var at image.Point
	if pp, ok := s.h.(interface{ PointerPos() image.Point }); ok {
		at = pp.PointerPos()
	}
	if hs := btn.History(); len(hs) > 0 {
		at = at.Sub(hs[len(hs)-1].Position)
	}
	sz := max(1, r.moreSz)
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Width: 240,
		Anchor: image.Rectangle{Min: at, Max: at.Add(image.Pt(sz, sz))}, Popover: "bottom-right", OffsetY: 6}))
}

// savedMenu is SavedItemActionsMenu's rows for it.
func (s *savedBase) savedMenu(ph popups.Host, it core.SavedItem) []popups.MenuItem {
	c := s.h.Core()
	ch, ts := it.Channel, it.TS
	if ch == "" || ts == "" {
		return nil
	}
	eV := c.Experiment("later_todos") == "on"
	eZ := eV && c.PrefBool("has_starred_item", false) // hq isLaterTodosSavedSynthPageFeatureEnabled
	peek := s.peeking
	completed := laterCompleted(&it)      // sN isSavedItemCompleted
	todo := it.TodoState == "to_do"       // Tx isTodo
	saved := it.TodoState == "saved"      // I0
	archived := it.Archived && !completed // F$ isArchived (messages)
	msg := it.ItemType == "" || it.ItemType == "message"
	closePeek := func() {}
	if peek && s.peekClose != nil {
		closePeek = s.peekClose
	}
	key := laterKey(&it)

	openInHome := popups.MenuItem{Key: "open_in_home", Label: nsLater.T("Open in Home"), Icon: "home", Do: func() {
		route := "c/" + ch
		if m := it.Msg; m != nil && m.ThreadTS != "" && m.ThreadTS != m.TS {
			route += "/t/" + m.ThreadTS
		}
		s.h.Navigate(route)
		closePeek()
	}}
	split := func() (popups.MenuItem, bool) {
		if peek || !msg {
			return popups.MenuItem{}, false
		}
		mi, ok := popups.SplitViewItem(ph, "c/"+ch, false)
		if ok {
			mi.Label, mi.Icon = nsLater.T("Open in split view"), "move-to-split-view"
		}
		return mi, ok
	}
	remove := func(label string) popups.MenuItem {
		return popups.MenuItem{Key: "remove", Label: label, Icon: "eraser", Danger: true, Do: func() {
			s.write1(it, true, nsLater.T("Saved item cleared"), func(ctx context.Context, c *core.App) error {
				return c.Unsave(ctx, ch, ts)
			})
		}}
	}
	var out []popups.MenuItem
	add := func(m popups.MenuItem) { out = append(out, m) }

	if !eV {
		if sp, ok := split(); ok {
			add(sp)
		}
		if !msg {
			add(popups.Sep())
		}
		if msg {
			add(openInHome)
			add(popups.Sep())
		}
		if peek {
			add(popups.MenuItem{Key: "remind_submenu", Label: nsLater.T("Remind me about this"), Icon: "clock", Sub: s.remindItems(ph, it)})
			add(popups.Sep())
		}
		if !completed {
			lbl, icon := nsLater.T("Archive"), "archive"
			if archived {
				lbl, icon = nsLater.T("Unarchive"), "unarchive"
			}
			arch := !archived
			add(popups.MenuItem{Key: "archive", Label: lbl, Icon: icon, Do: func() {
				s.write1(it, true, "", func(ctx context.Context, c *core.App) error {
					return c.UpdateSaved(ctx, ch, ts, core.SavedUpdate{Archived: &arch})
				})
			}})
		} else {
			add(popups.MenuItem{Key: "move_in_progress", Label: nsLater.T("Move to in progress"), Icon: "undo", Do: func() {
				no := false
				s.write1(it, true, "", func(ctx context.Context, c *core.App) error {
					return c.UpdateSaved(ctx, ch, ts, core.SavedUpdate{Completed: &no})
				})
			}})
		}
		if !s.todos || completed || archived { // eT !== ew || eN || ej
			add(remove(nsLater.T("Remove from Later")))
		}
		return stripTerminalSeps(out)
	}

	if todo && !completed { // Mark complete
		add(popups.MenuItem{Key: "mark_complete", Label: nsLater.T("Mark complete"), Icon: "check", Do: func() { s.toggleDoneKey(key) }})
	}
	if !todo && !(eZ && saved) { // Mark incomplete
		add(popups.MenuItem{Key: "mark_incomplete", Label: nsLater.T("Mark incomplete"), Icon: "check-circle", Do: func() {
			if completed {
				s.toggleDoneKey(key)
				return
			}
			s.write1(it, true, nsLater.T("Moved to to-dos"), func(ctx context.Context, c *core.App) error {
				return c.MoveToTodos(ctx, ch, ts, 0)
			})
		}})
	}
	if eZ && saved { // Move to to-dos
		add(popups.MenuItem{Key: "move_to_todos", Label: nsLater.T("Move to to-dos"), Icon: "todo-add", Do: func() {
			s.write1(it, true, nsLater.T("Moved to to-dos"), func(ctx context.Context, c *core.App) error {
				return c.MoveToTodos(ctx, ch, ts, 0)
			})
		}})
	}
	if todo && !(eZ && saved) && !completed { // Add reminder / Edit reminder
		lbl, k := nsLater.T("Edit reminder"), "edit_reminder_submenu"
		if it.DateDue == it.DateCreated || it.DateDue <= 1 { // dK isTodoWithoutReminder
			lbl, k = nsLater.T("Add reminder"), "set_reminder_submenu"
		}
		add(popups.MenuItem{Key: k, Label: lbl, Icon: "clock", Sub: s.remindItems(ph, it)})
	}
	if !saved { // Move to saved
		add(popups.MenuItem{Key: "move_to_saved", Label: nsLater.T("Move to saved"), Icon: "bookmark", Do: func() {
			s.write1(it, true, nsLater.T("To-do moved to Saved"), func(ctx context.Context, c *core.App) error {
				return c.MoveToSaved(ctx, ch, ts)
			})
		}})
	}
	if !peek {
		add(popups.Sep())
	}
	if msg {
		add(openInHome)
	}
	if sp, ok := split(); ok {
		add(sp)
	}
	add(popups.Sep())
	if todo || completed || !s.todos || archived { // z || eV && eE || eT !== ew || eN || ej
		add(remove(nsLater.T("Clear")))
	}
	return stripTerminalSeps(out)
}

// remindItems is RemindMenu: the ReminderTimeMenu times, Custom… and, for
// an item with a real due date, Clear reminder.
func (s *savedBase) remindItems(ph popups.Host, it core.SavedItem) []popups.MenuItem {
	ch, ts := it.Channel, it.TS
	var sub []popups.MenuItem
	for _, p := range laterPresets {
		p := p
		sub = append(sub, popups.MenuItem{Label: p.text(), Do: func() {
			at := p.at(time.Now())
			due := at.Unix()
			s.write1(it, !s.todos, nsLater.T("Reminder set for {dueDate}", "dueDate", laterWhen(at)), func(ctx context.Context, c *core.App) error {
				return c.MoveToTodos(ctx, ch, ts, due)
			})
		}})
	}
	sub = append(sub, popups.MenuItem{Label: nsActivity.T("Custom…"), Do: func() { ph.ShowOverlay(popups.NewReminderModal(ph, ch, ts)) }})
	if it.DateDue > 1 && it.DateDue != it.DateCreated { // onClear: a due date that isn't the to-do's own
		sub = append(sub, popups.Sep(), popups.MenuItem{Label: nsLater.T("Clear reminder"), Do: func() {
			var zero int64
			s.write1(it, false, "Reminder removed", func(ctx context.Context, c *core.App) error {
				return c.UpdateSaved(ctx, ch, ts, core.SavedUpdate{DateDue: &zero})
			})
		}})
	}
	return sub
}

// toggleDoneKey is the row's Mark complete / Mark incomplete for the item
// listed under k.
func (s *savedBase) toggleDoneKey(k string) {
	if f, i := s.find(k); f != nil {
		s.toggleDone(f, i)
	}
}

// stripTerminalSeps is Menu's stripTerminalSeparators: no separator first,
// last or next to another.
func stripTerminalSeps(items []popups.MenuItem) []popups.MenuItem {
	out := items[:0:0]
	for _, m := range items {
		if m.Separator && (len(out) == 0 || out[len(out)-1].Separator) {
			continue
		}
		out = append(out, m)
	}
	for len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}
