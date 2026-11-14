package listview

// A record's menus (CDN build fa938a086569, 2026-11-13):
//
//   - The grid row's and board card's right-click menu is
//     BaseRecordContextMenu (boot-shared Yja+, "Item actions"). Its rows,
//     in order: Open item (Hi0F) · Open subtasks (when the item has
//     subtasks; subtask 18) · View comment(s) / Add comment (7EO4) ·
//     Copy link to item (A3Rr) ─ Duplicate item (rJ) ─ Remind me about
//     this ▸ (Jf4A, lists_in_later) ─ Archive item (tAP0) · Delete item
//     (hWxs), the last group only when the list isn't view-only.
//   - The record pane's More actions (2kGA BaseListRecordOverflowMenuTrigger,
//     the ⋮ beside Add Comment in ListRecordDetailActions): Copy link to
//     item ─ Remind me about this ▸ ─ Archive item · Delete item.
//
// Rows Slack shows that Clack has no screen or state for are left out:
// Open in new window (one window), Share
// item (no file share dialog), View history, Report to Slack, Get
// notifications ▸ (record and thread subscriptions aren't loaded), the
// grid's Copy and Paste ▸, Move item ▸ and Insert item ▸ (no positions
// between rows), Field options ▸ (no column menu) and Group options.

import (
	"context"
	"image"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// nsSaved is the reminder menu's strings (ReminderTimeMenu and Jf4A's
// description: saved_for_later), then the lists'.
var nsSaved = i18n.NewChain("saved_for_later", "lists")

// recordMenu is the record's menu: the grid or board one, or with pane
// the record pane's More actions.
func (c *ctl) recordMenu(d *core.ListDoc, rec *lists.Record, pane bool) []MenuItem {
	a := c.h.Core()
	if d == nil || rec == nil || a == nil {
		return nil
	}
	readOnly := !d.Info.Editable // el.I9 isListViewOnly
	subtask := rec.ParentRecordID != ""
	listID, recID := d.Info.ID, rec.ID
	var items []MenuItem
	sep := func() {
		if n := len(items); n > 0 && !items[n-1].Divider {
			items = append(items, MenuItem{Divider: true})
		}
	}
	open := func() {
		if c.act.OpenItem != nil {
			c.act.OpenItem(listID, recID)
		}
	}
	if !pane {
		items = append(items, MenuItem{Label: nsLists.T("Open item"), Icon: "sidebar", Do: open})
		if hasSubtasks(d, recID) { // OpenItemSubtasksMenuItem (Prtm eD; any group)
			items = append(items, MenuItem{Label: nsLists.T("Open subtasks"), Icon: "subtask", Do: open})
		}
		// AddCommentMenuItemBase: none for an archived item without comments
		if n := c.commentCount(d, rec); !(rec.Archived && n == 0) {
			label := nsLists.T("Add comment")
			if n > 0 {
				label = nsLists.T("{count, plural, =1 {View comment} other {View comments}}", "count", n)
			}
			items = append(items, MenuItem{Label: label, Icon: "add-comment", Do: open})
		}
	}
	if link := core.ListRecordLink(d.Info.Permalink, recID); link != "" {
		label := nsLists.T("Copy link to item")
		if subtask {
			label = nsLists.T("Copy link to subtask")
		}
		items = append(items, MenuItem{Label: label, Icon: "link", Do: func() {
			if c.act.Copy != nil {
				c.act.Copy(link)
			}
			c.h.Toast(nsLists.T("Link copied to clipboard"))
		}})
	}
	sep()
	if !pane && !readOnly {
		items = append(items, MenuItem{Label: nsLists.T("Duplicate item"), Do: func() { c.duplicate(d, rec) }})
	}
	sep()
	if c.exp("lists_in_later") {
		items = append(items, c.remindItem(listID, recID))
	}
	if !readOnly {
		sep()
		if !subtask {
			items = append(items, MenuItem{Label: nsLists.T("Archive item"), Do: func() {
				c.recordWrite(func(ctx context.Context, a *core.App) error { return a.ArchiveListRecords(ctx, listID, recID) })
			}})
		}
		label := nsLists.T("Delete item")
		if subtask {
			label = nsLists.T("Delete subtask")
		}
		items = append(items, MenuItem{Label: label, Danger: true, Do: func() {
			c.recordWrite(func(ctx context.Context, a *core.App) error { return a.DeleteListRecord(ctx, listID, recID) })
		}})
	}
	for len(items) > 0 && items[len(items)-1].Divider {
		items = items[:len(items)-1]
	}
	return items
}

// commentCount is the item's comment count (Prtm nZ): its thread root's
// reply count when the client holds it.
func (c *ctl) commentCount(d *core.ListDoc, rec *lists.Record) int {
	a := c.h.Core()
	if a == nil || rec.ThreadTS == "" {
		return 0
	}
	if m, ok := a.S.Find(core.ListCommentsChannel(d.Info.ID), rec.ThreadTS); ok {
		return m.ReplyCount
	}
	return 0
}

// duplicate is Duplicate item: the copy goes after the record
// (position between it and the next one is the server's call: Clack
// sends none).
func (c *ctl) duplicate(d *core.ListDoc, rec *lists.Record) {
	listID, recID := d.Info.ID, rec.ID
	c.recordWrite(func(ctx context.Context, a *core.App) error {
		_, err := a.DuplicateListRecord(ctx, listID, recID, "")
		return err
	})
}

// recordWrite runs a record write off the UI goroutine; a failure is a
// toast.
func (c *ctl) recordWrite(f func(ctx context.Context, a *core.App) error) {
	a := c.h.Core()
	if a == nil {
		return
	}
	go func() {
		ctx, cancel := ctxTimeout(30 * time.Second)
		defer cancel()
		if err := f(ctx, a); err != nil {
			c.h.Toast("Something went wrong: " + err.Error())
		}
		c.h.Invalidate()
	}()
}

// remindItem is RemindMeAboutThisMenuItem: "Remind me about this", or
// "Edit reminder" for a record in Later, with the reminder's description
// ("Archived", "Completed", "Due …" / "Overdue by …"), and
// ListRecordRemindMenu's ReminderTimeMenu as its submenu.
func (c *ctl) remindItem(listID, recID string) MenuItem {
	a := c.h.Core()
	m, saved := a.ListRecordSaved(listID, recID)
	label := nsLists.T("Remind me about this")
	if saved {
		label = nsLists.T("Edit reminder")
	}
	return MenuItem{Label: label, Subtitle: reminderDescription(m, saved, time.Now()), Sub: c.remindTimes(listID, recID, saved)}
}

// remindTimes is ReminderTimeMenu (width 240): the five times, Custom…
// (the custom reminder modal, 4hLh) and, for a record in Later, Clear
// reminder.
func (c *ctl) remindTimes(listID, recID string, saved bool) []MenuItem {
	labels, due := popups.ReminderPresets(time.Now())
	var sub []MenuItem
	for i := range labels {
		at := due[i].Unix()
		sub = append(sub, MenuItem{Label: labels[i], Do: func() {
			c.recordWrite(func(ctx context.Context, a *core.App) error { return a.RemindListRecord(ctx, listID, recID, at) })
		}})
	}
	sub = append(sub, MenuItem{Label: nsSaved.T("Custom…"), Do: func() {
		ph, ok := c.h.(popups.Host)
		if !ok {
			return
		}
		ph.ShowOverlay(popups.NewReminderDueModal(ph, func(at int64) {
			c.recordWrite(func(ctx context.Context, a *core.App) error { return a.RemindListRecord(ctx, listID, recID, at) })
		}))
	}})
	if saved {
		sub = append(sub, MenuItem{Divider: true}, MenuItem{Label: nsLists.T("Clear reminder"), Do: func() {
			c.recordWrite(func(ctx context.Context, a *core.App) error { return a.ClearListRecordReminder(ctx, listID, recID) })
		}})
	}
	return sub
}

// reminderDescription is Jf4A's d: nothing without a due date;
// "Archived", "Completed"; else T8t/ mk(dateDue, true, overdue): "Due
// {in 3 hours}" or "Overdue by {3 hours}".
func reminderDescription(m core.SavedMeta, saved bool, now time.Time) string {
	if !saved || m.DateDue == 0 {
		return ""
	}
	switch {
	case m.Archived:
		return nsSaved.T("Archived")
	case m.DateCompleted > 0:
		return nsSaved.T("Completed")
	}
	real := m.DateDue // qb0V Z1 getRealDueDate
	if real <= 1 || real == m.DateCreated {
		real = 0
	}
	overdue := real != 0 && real <= now.Unix()
	s := strings.ToLower(i18n.TimeAgo(i18n.Locale(), time.Unix(m.DateDue, 0), now, false, false))
	if overdue {
		return strings.TrimSpace(strings.Replace(nsSaved.T("Overdue by {dateString}", "dateString", s), "ago", "", 1))
	}
	return nsSaved.T("Due {dateString}", "dateString", s)
}

// openRecordMenu shows the grid's or board's record menu at the pointer.
func (v *View) openRecordMenu(d *core.ListDoc, rec *lists.Record) {
	if v.mode == Peek || v.act.Menu == nil {
		return
	}
	if items := v.recordMenu(d, rec, false); len(items) > 0 {
		v.act.Menu(items)
	}
}

// openPaneMenu shows the record pane's More actions under the ⋮ (Slack:
// bottom-right, 4px below).
func (it *Item) openPaneMenu(d *core.ListDoc, rec *lists.Record, anchor image.Point, ok bool) {
	items := it.recordMenu(d, rec, true)
	if len(items) == 0 {
		return
	}
	if ok && it.act.MenuAt != nil {
		it.act.MenuAt(anchor, items)
		return
	}
	if it.act.Menu != nil {
		it.act.Menu(items)
	}
}

// RowPoint is a point inside record rec's row in window coordinates
// (80px into its first column), once the pointer has shown where the grid
// is; ok is false before that or when the row isn't drawn (tests).
func (v *View) RowPoint(rec string) (image.Point, bool) {
	g := &v.grid
	if !g.originOK {
		return image.Point{}, false
	}
	for _, ln := range g.lines {
		if ln.kind == lineRow && ln.row.Rec != nil && ln.row.Rec.ID == rec {
			return g.origin.Add(image.Pt(g.x0+80, ln.y-g.scrollY+ln.h/2)), true
		}
	}
	return image.Point{}, false
}
