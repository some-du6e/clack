package ui

import (
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/views"
)

// The view container with the focus, as Slack's focus manager knows it
// (getFocusedContainer: the container of the last focused element, kept
// while the focus is nowhere). MessagePane keeps it (focusedContainer,
// subscribeToFocus) to count no "Unread" on the reply bar of the thread
// the focused secondary view shows (MessageWrapper: unreadThreadsCount
// is 0 when activeThreadKeyFromSecondaryView is the message's thread and
// isSecondaryViewFocused), and Mod+F finds in the thread of the focused
// container (findInThread).

// noteFocusedContainer follows the focus into and out of the side pane,
// the split view's thread and the thread the Activity tab's own pane
// draws (keys.process, once keyFocus and findFocus are this frame's): the
// one the key focus is in is the focused container. With the focus
// nowhere, or the window inactive, none is: Slack's ViewContainer onBlur
// sets the focused container to null when the focus leaves for nothing
// (relatedTarget null), a window losing its focus too.
func (a *App) noteFocusedContainer(gtx layout.Context) {
	if in := a.focused && a.secondaryHasFocus(gtx); in != a.secFocused {
		a.secFocused = in
		a.Invalidate() // the reply bars count again
	}
	// for Mod+F (findInThread: a key, so the window has the focus): the
	// split view's thread, the Activity tab's Primary view
	a.splitFocus = a.splitThreadFocused(gtx)
	a.actPaneFocus = a.activityPaneThreadFocused(gtx)
}

// splitThreadFocused reports whether the key focus is in the split
// view's thread: its reply box, an edit composer in its place, its list,
// its find bar (getFocusedContainer() === Split showing a thread).
func (a *App) splitThreadFocused(gtx layout.Context) bool {
	r := a.splitTop()
	if !a.splitOpen() || r.Thread() == "" {
		return false
	}
	in := false
	a.inPane(paneSplit, func() {
		c := a.convos[r.Channel()+"/"+r.Thread()]
		if c == nil {
			return
		}
		for _, cv := range []composerView{c.comp, c.edit} {
			if ft, ok := cv.(interface{ FocusTag() any }); ok && gtx.Focused(ft.FocusTag()) {
				in = true
			}
		}
		if tl, ok := c.list.(threadList); ok && (tl.FindFocused(gtx) || tl.ListKeyFocus(gtx)) {
			in = true
		}
		if ec, ok := c.list.(interface{ EditComposer() *composer.Composer }); ok {
			if e := ec.EditComposer(); e != nil && gtx.Focused(e.FocusTag()) {
				in = true
			}
		}
	})
	return in
}

// secondaryHasFocus reports whether the key focus is anywhere in the open
// side pane: keyFocus and findFocus, or its reply box or channel input
// with a popup open, or a message's inline edit there (the pane's Escape
// leaves those to their own Escape; the container counts them).
func (a *App) secondaryHasFocus(gtx layout.Context) bool {
	s := &a.secondary
	if !s.open() {
		return false
	}
	if s.keyFocus || s.findFocus {
		return true
	}
	var c *convo
	switch s.kind {
	case secThread:
		c = a.convos[s.channel+"/"+s.ts]
	case secChannel:
		c = a.convos[s.channel+"/"]
	}
	if c == nil {
		return false
	}
	for _, cv := range []composerView{c.comp, c.edit} {
		if ft, ok := cv.(interface{ FocusTag() any }); ok && gtx.Focused(ft.FocusTag()) {
			return true
		}
	}
	ec, ok := c.list.(interface{ EditComposer() *composer.Composer })
	if !ok {
		return false
	}
	e := ec.EditComposer()
	return e != nil && gtx.Focused(e.FocusTag())
}

// SecondaryThreadFocused reports whether ch's thread ts is the one the
// side pane shows while it is the focused container
// (messages.SecondaryThreadFocuser: its reply bar counts no "Unread").
func (a *App) SecondaryThreadFocused(ch, ts string) bool {
	s := &a.secondary
	return a.secFocused && s.kind == secThread && s.open() && s.channel == ch && s.ts == ts
}

// activityPaneThreadFocused reports whether the key focus is in the
// thread the Activity tab's own pane draws beside the inbox (the
// persistent detail panel: Slack's Primary view on the Activity tab), in
// its reply box or its find bar.
func (a *App) activityPaneThreadFocused(gtx layout.Context) bool {
	if !a.activityTab() {
		return false
	}
	v, ok := a.views[Route("activity")].(*views.Activity)
	if !ok {
		return false
	}
	ch, ts := v.PaneThread()
	if ts == "" {
		return false
	}
	c := a.convos[ch+"/"+ts]
	if c == nil {
		return false
	}
	for _, cv := range []composerView{c.comp, c.edit} {
		if ft, ok := cv.(interface{ FocusTag() any }); ok && gtx.Focused(ft.FocusTag()) {
			return true
		}
	}
	tl, ok := c.list.(threadList)
	return ok && tl.FindFocused(gtx)
}
