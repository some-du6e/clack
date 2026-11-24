package popups

import (
	"image"
	"slices"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
)

// A focused message's keys, Slack's MessageActions key commands (RwAt
// getContextualShortcutCommands, boot-shared build 83aff18a1847b8e9).
// While the client is in keyboard mode (FocusEventManager's
// hasKeyboardFocus, withKeyboardFocusContext) the message's actions bind
// their shortcuts on the message's own element (bindContextualShortcuts:
// a Mousetrap on parentId), so they fire while the message or a button
// in it has the focus, never from an input. The actions are
// getActionsForKeyboardShortcuts' list, each by its own rule, with a
// shortcut key (P2, module ORgj): edit e (canEditMessage), add reaction r
// (canAddReaction), the thread action t (canStartOrOpenThread; none in
// the thread pane), pin p (canPin), share s and f (canBeShared), mark
// unread u (the conversation's, or in a thread the thread's), save
// mod+shift+s (a with later_todos off), add to list v, delete del (and
// backspace on macOS; canDeleteMessageHelper), remind m (canAddReminder),
// copy link l (canCopyLink), copy message mod+c (canCopyMessageText;
// none while the window's selection reaches into the message) and add
// to-do a. A key no action of the message takes isn't bound: it goes on
// to the conversation's input like any other key (MessagePaneInput's
// document keydown, shouldInputStealKeyEvent). Mousetrap matches a key's
// modifiers exactly, so Shift+R or Alt+R is no r.
//
// The handlers are the actions' own (onReact, onComment, onShare… →
// dispatchMessageAction), what the menus' rows run, but remind: M is
// onRemindShortcut, remindLater with time "Custom", which opens the
// custom reminder dialog (DhMv) rather than the submenu.

// MessageKeyAction is what key k (a P2 key id: "r", "mod+c", "del"…) does
// on the focused message m in where; ok false when none of m's actions
// takes k. at is where a popup it opens goes (the message, window
// coordinates); o the list's inline hooks, as its menus get them.
func MessageKeyAction(h Host, at image.Point, m core.Message, where MessageContext, o MessageMenuOpts, k string) (run func(gtx layout.Context), ok bool) {
	o.Where = where
	items := append(ctxHoverItems(h, at, m, where, o), actMessageItems(h, m, o)...)
	it, ok := keyedItem(items, k)
	if !ok {
		return nil, false
	}
	switch {
	case it.Action == "remind":
		ch, ts := m.Channel, m.TS
		return func(layout.Context) { h.ShowOverlay(actNewReminderModal(h, ch, ts)) }, true
	case it.DoCtx != nil:
		return it.DoCtx, true
	case it.Do != nil:
		do := it.Do
		return func(layout.Context) { do() }, true
	}
	return nil, false
}

// keyedItem is the enabled item that takes key k: the rows first, then
// the submenus' (Organize ▸ Pin).
func keyedItem(items []MenuItem, k string) (MenuItem, bool) {
	for _, it := range items {
		if !it.Separator && !it.Disabled && slices.Contains(it.Keys, k) {
			return it, true
		}
	}
	for _, it := range items {
		if it.Disabled {
			continue
		}
		for _, sub := range it.Sub {
			if !sub.Separator && !sub.Disabled && slices.Contains(sub.Keys, k) {
				return sub, true
			}
		}
	}
	return MenuItem{}, false
}
