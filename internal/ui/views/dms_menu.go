package views

// A DMs tab row's actions, on the page and on the rail's DMs peek (web
// client build f132f0e78468). DmsChannel (boot-render IazQ) wraps the row
// in the activity item container (boot-shared WbG1) with isDmTab, so the
// row has the activity row's hover actions and DmsListAll (eLDn) gives the
// list renderContextMenuForItem (0KJm with isDmTab, isRead: the DM has no
// unreads):
//
//   - The hover actions (WbG1 Actions): Mark as read (TQCD, only while the
//     DM is unread; markActivityItemAsRead with isDmTab marks the DM read
//     at its latest message, reason dms_tab), the to-do button (Yoyn:
//     "Add to-do" check-add, "Complete to-do" check-circle-filled once
//     the latest message is a to-do; without later_todos it's Save for
//     later's bookmark) and More actions (g9lY ActionsOverflowMenu: the
//     ⋮, ellipsis-vertical-filled, opening the menu 300px wide at its
//     bottom-right, offsetY 6).
//   - The menu, the ⋮'s and the right-click's alike: WabB
//     getActivityItemMenuTemplate with isDmTab and no activityItem, so:
//     mark_as_read (unread only; no mark_as_unread on the DMs tab),
//     save_for_later; remind_submenu, toggle_vip_user, copy_link as the
//     DMs tab's "Copy" ▸ (Copy name, Copy link, Copy huddle link);
//     open_in_home, open_in_split_view. notify_broadcast_mentions,
//     toggle_notifications_replies, mute_conversation, change_
//     notifications, move_to_section, summarize_thread and
//     clear_all_before all need an activity item's type or feed ts.
//   - open_in_home ends with maybeClosePeek: from the peek the card closes.
//
// Not ported: open_in_new_window (Clack has one window) and
// agents_in_dm_list ("Add/Remove agents from DMs": uXg5 Z3's agent DM
// eligibility, which Clack doesn't track).

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// dmTodo is the hover bar's to-do button for dm's latest message (Yoyn
// with later_todos, else ActivityItemSavedForLaterButton's bookmark):
// its glyph, whether the message is already a to-do (saved), and ok
// false when there is no message to act on.
func (d *DMs) dmTodo(dm *core.DM) (icon string, saved, ok bool) {
	if dm.Latest == nil {
		return "", false, false
	}
	c := d.h.Core()
	saved = c.SavedState(dm.Channel, dm.Latest.TS)
	if c.Experiment("later_todos") == "on" {
		if saved {
			return "check-circle-filled", true, true
		}
		return "todo-add", false, true
	}
	if saved {
		return "bookmark-filled", true, true
	}
	return "bookmark", false, true
}

// dmAct runs one of a row's writes off the UI goroutine (popups.ActDo: a
// failure toasts, on the UI goroutine).
func (d *DMs) dmAct(f func(ctx context.Context, c *core.App) error) {
	if ph, ok := d.h.(popups.Host); ok {
		popups.ActDo(ph, "", f)
		return
	}
	c := d.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = f(ctx, c)
		d.h.Invalidate()
	}()
}

// dmMarkRead is mark_as_read on the DMs tab: the DM read at its latest
// message (reason dms_tab).
func (d *DMs) dmMarkRead(dm *core.DM) {
	ch, ts := dm.Channel, dmsLatestTS(dm)
	if ts == "" {
		return
	}
	if i, ok := d.index[ch]; ok { // the row reads as read at once
		d.items[i].Count.Unread, d.items[i].Count.Mentions = false, 0
		d.dirty = true
	}
	d.dmAct(func(ctx context.Context, c *core.App) error {
		_, err := c.Mark(ctx, ch, ts)
		return err
	})
}

// dmToggleTodo is the to-do button and save_for_later: add the latest
// message (to-dos or Later), or with it already there, complete / remove
// it.
func (d *DMs) dmToggleTodo(dm *core.DM) {
	_, saved, ok := d.dmTodo(dm)
	if !ok {
		return
	}
	ch, ts := dm.Channel, dm.Latest.TS
	todos := d.h.Core().Experiment("later_todos") == "on"
	if todos && !saved {
		d.addTodo(ch) // with LaterTodoToast's "To-do added"
		return
	}
	d.dmAct(func(ctx context.Context, c *core.App) error {
		switch {
		case saved && todos:
			return c.CompleteTodo(ctx, ch, ts)
		case saved:
			return c.Unsave(ctx, ch, ts)
		}
		return c.Save(ctx, ch, ts, 0)
	})
}

// dmMenuOpen opens the row's menu: at the ⋮ (bottom-right, offsetY 6) or,
// for a right-click, at the pointer.
func (d *DMs) dmMenuOpen(dm *core.DM, btn *widget.Clickable) {
	ph, ok := d.h.(popups.Host)
	if !ok {
		return
	}
	items := d.dmMenu(ph, dm)
	if len(items) == 0 {
		return
	}
	o := popups.MenuOpts{Width: 300}
	var at image.Point
	if pp, ok := d.h.(interface{ PointerPos() image.Point }); ok {
		at = pp.PointerPos()
	}
	r := d.rowState(dm.Channel)
	if btn != nil && r.moreSz > 0 {
		if hs := btn.History(); len(hs) > 0 {
			at = at.Sub(hs[len(hs)-1].Position)
		}
		o.Anchor, o.Popover, o.OffsetY = image.Rectangle{Min: at, Max: at.Add(image.Pt(r.moreSz, r.moreSz))}, "bottom-right", 6
	} else {
		o.Anchor = image.Rectangle{Min: at, Max: at}
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, o))
}

// dmMenu is getActivityItemMenuTemplate for a DMs tab row.
func (d *DMs) dmMenu(ph popups.Host, dm *core.DM) []popups.MenuItem {
	c := d.h.Core()
	ch := dm.Channel
	m := dm.Latest
	closePeek := func() {}
	if d.peeking && d.peekClose != nil {
		closePeek = d.peekClose
	}
	var groups [][]popups.MenuItem

	var g1 []popups.MenuItem
	if dmsUnread(dm) {
		g1 = append(g1, popups.MenuItem{Key: "mark_as_read", Label: nsActivity.T("Mark as read"), Icon: "mark-as-read", Hint: "U", Keys: []string{"u"},
			Do: func() { d.dmMarkRead(dm) }})
	}
	if _, saved, ok := d.dmTodo(dm); ok { // renderSavedForLaterMenuItem
		var lbl, icon string
		switch todos := c.Experiment("later_todos") == "on"; {
		case todos && saved:
			lbl, icon = nsActivity.T("Remove to-do"), "check-circle-filled"
		case todos:
			lbl, icon = nsActivity.T("Add to-do"), "todo-add"
		case saved:
			lbl, icon = nsActivity.T("Remove from Later"), "bookmark-filled"
		default:
			lbl, icon = nsActivity.T("Save for later"), "bookmark"
		}
		ts := m.TS
		g1 = append(g1, popups.MenuItem{Key: "save_for_later", Label: lbl, Icon: icon, Hint: "A", Keys: []string{"a"},
			Do: func() {
				// toggleAddToLaterTodos / toggleSaveForLater: off again
				// when it's there
				if !saved && c.Experiment("later_todos") == "on" {
					d.addTodo(ch)
					return
				}
				d.dmAct(func(ctx context.Context, c *core.App) error {
					if saved {
						return c.Unsave(ctx, ch, ts)
					}
					return c.Save(ctx, ch, ts, 0)
				})
			}})
	}
	groups = append(groups, g1)

	var g2 []popups.MenuItem
	if m != nil {
		ts := m.TS
		var sub []popups.MenuItem
		for _, p := range laterPresets {
			p := p
			sub = append(sub, popups.MenuItem{Label: p.text(), Do: func() {
				due := p.at(time.Now()).Unix()
				d.dmAct(func(ctx context.Context, c *core.App) error {
					return c.Save(ctx, ch, ts, due)
				})
			}})
		}
		sub = append(sub, popups.MenuItem{Label: nsActivity.T("Custom…"), Do: func() { ph.ShowOverlay(popups.NewReminderModal(ph, ch, ts)) }})
		g2 = append(g2, popups.MenuItem{Key: "remind_submenu", Label: nsActivity.T("Remind me about this"), Sub: sub})
	}
	if vip, ok := d.vipItem(ph, ch); ok {
		g2 = append(g2, vip)
	}
	if m != nil { // copy_link on the DMs tab: the Copy ▸ submenu
		name := d.dmCopyName(dm)
		link := popups.ChannelLink(ph, ch)
		sub := []popups.MenuItem{
			{Key: "copy_name", Label: nsActivity.T("Copy name"), DoCtx: func(gtx C) { popups.CopyText(gtx, name) }},
			{Key: "copy_link", Label: nsActivity.T("Copy link"), DoCtx: func(gtx C) { popups.CopyText(gtx, link) }},
		}
		if hl, ok := d.h.(huddleLinkCopier); ok {
			sub = append(sub, popups.MenuItem{Key: "copy_huddle_link", Label: nsActivity.T("Copy huddle link"), Do: func() { hl.CopyHuddleLink(ch) }})
		}
		g2 = append(g2, popups.MenuItem{Key: "copy_link", Label: nsActivity.T("Copy"), Sub: sub})
	}
	groups = append(groups, g2)

	if m != nil {
		route := "c/" + ch
		g := []popups.MenuItem{{Key: "open_in_home", Label: nsActivity.T("Open in home"), Icon: "home", Do: func() {
			closePeek()
			d.h.Navigate(route)
		}}}
		if split, ok := popups.SplitViewItem(ph, route, true); ok {
			g = append(g, split)
		}
		groups = append(groups, g)
	}

	var out []popups.MenuItem
	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, popups.Sep())
		}
		out = append(out, g...)
	}
	return out
}

// dmCopyName is copyChannelNameById's text for a DM: the person with the
// @ (imIncludeAtSign), a group DM's names.
func (d *DMs) dmCopyName(dm *core.DM) string {
	name := d.dmName(dm)
	if dm.Conv.User != "" && !strings.HasPrefix(name, "@") {
		return "@" + name
	}
	return name
}
