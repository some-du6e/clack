package ui

// To-dos and Saved open their rows beside the list (gap 11, work package
// W42; views/later_beside.go has the rows' side), from Slack's client
// (CDN build fa938a086569):
//
//   - getTargetContainer (client-boot L1Ms J): on the Later tab with kl02
//     eU (isNestedSecondaryViewEnabled and later_todos) every view but the
//     LaterList and pages goes in the Secondary, and the tab stays Later
//     (qBXZ: the Primary is the LaterList, Primary to Secondary). The
//     Saved page (Page Bookmarks in the Home tab's Primary) sends a
//     conversation there under 6owI hq (later_todos and the
//     has_starred_item pref); a thread is a secondary view everywhere
//     (+zKh aK). So the message opens in the secondary column with the
//     list kept: a conversation at the message (OpenMessageSecondary,
//     jpad R9), a thread at it (OpenThreadAt, jpad UX toThread).
//   - The layout around them (b2oS useCalculateMainWindowClientLayout):
//     isSavedListLayoutClamped, eU with the LaterList in the Primary or hq
//     with the Saved page there, gives the column 55% of the region
//     (defaultWidthPercentageOfRegionOverride .55) and stops it where the
//     list would keep less than 320 (getLaterSavedListMinWidth, IgMS o0:
//     autoMaxWidthOverride and manualMaxWidthOverride); with later_todos
//     and the list in the Primary (he3E isLaterTodosListInPrimaryView) the
//     primary's floor is 320 (primaryMinWidthOverride, IgMS yH), which the
//     squish (xeD9) gives way at. secondary.go applies them.

import (
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/views"
)

// laterTodosOn is 6owI tj: later_todos.
func (a *App) laterTodosOn() bool {
	c := a.Core()
	return c != nil && c.Experiment("later_todos") == "on"
}

// laterNested is kl02 eU: nested secondary views (W7) with later_todos.
func (a *App) laterNested() bool { return a.nestedSecondary() && a.laterTodosOn() }

// savedPageOn is 6owI hq, isLaterTodosSavedSynthPageFeatureEnabled:
// later_todos and the has_starred_item pref.
func (a *App) savedPageOn() bool {
	c := a.Core()
	return c != nil && a.laterTodosOn() && c.PrefBool("has_starred_item", false)
}

// laterListPrimary is he3E isLaterTodosListInPrimaryView: the To-dos page
// (the LaterList) is the primary view, or with eU the Saved page.
func (a *App) laterListPrimary() bool {
	switch a.router.Current().Page() {
	case "later":
		return true
	case "saved":
		return a.laterNested()
	}
	return false
}

// savedListClamped is b2oS isSavedListLayoutClamped: with eU To-dos, under
// hq the Saved page, in the primary.
func (a *App) savedListClamped() bool {
	switch a.router.Current().Page() {
	case "later":
		return a.laterNested()
	case "saved":
		return a.savedPageOn()
	}
	return false
}

// primaryFloor is the width (px) the primary view keeps before a nested
// column gives way (b2oS eW, getSquishedPrimaryFloorForFilledSecondary
// and primaryMinWidthOverride): 320 for the To-dos list under
// later_todos (IgMS yH), else 300.
func (a *App) primaryFloor(gtx layout.Context) int {
	if a.laterTodosOn() && a.laterListPrimary() {
		return gtx.Dp(320)
	}
	return gtx.Dp(300)
}

// OpenMessageSecondary shows conversation ch at message ts in the
// secondary column (views.SavedSecondary): jpad R9(ch, ts, ts), the
// conversation started at the message (startTs) and the message
// highlighted (highlightTs), as a permalink is. Inside the split view it
// goes there, as OpenChannelSecondary does.
func (a *App) OpenMessageSecondary(ch, ts string) {
	if ch == "" {
		return
	}
	if a.pane == paneSplit && ts != "" {
		a.OpenSplit("c/" + ch + "/m/" + ts)
		return
	}
	a.OpenChannelSecondary(ch)
	s := &a.secondary
	if s.kind != secChannel || s.channel != ch {
		return
	}
	s.at = ts
	if h, ok := a.convoFor(ch, "").list.(interface{ Highlight(string) }); ok && ts != "" {
		h.Highlight(ts)
	}
	a.Invalidate()
}

// OpenThreadAt shows thread (its root's ts) in the secondary column at
// message ts (views.SavedSecondary): jpad UX toThread with replyTs ts,
// uiState.highlightTs ts and highlightRoot. The navigation starts from the
// page shown now (Slack's prevViewSet is the active tab's view set when
// it starts).
func (a *App) OpenThreadAt(ch, thread, ts string) {
	a.openThreadAt(ch, thread, ts, a.router.Current())
}

// openThreadAt is OpenThreadAt from route prev, then Slack's
// jumpToThreadReply (client-boot 6IaZ, a navigation's side effect):
//
//   - with uiState.highlightTs the pane's thread metadata (CSo3 GC) starts
//     at ts and highlights it unless, under the nested view (and not in
//     single-column mode, which Clack doesn't have), the navigation left
//     Threads or Unreads or the thread's own conversation: highlight is
//     `!nested || singleColumn || !(prev is Threads, Unreads, quick read)
//     && prev primary id !== the thread's channel`.
//   - highlightRoot, for a thread in the Secondary, moves the primary
//     conversation to the root (startTs) and highlights it there (lazily),
//     then is cleared. Its callers here have the To-dos list or the Saved
//     page in the primary, never a conversation, so it has nothing to
//     move.
func (a *App) openThreadAt(ch, thread, ts string, prev Route) {
	if ch == "" || thread == "" {
		return
	}
	split := a.pane == paneSplit || a.splitClick() // OpenThread puts it in the split view
	a.OpenThread(ch, thread)
	s := &a.secondary
	if split || ts == "" || s.kind != secThread || s.channel != ch || s.ts != thread {
		return
	}
	s.at = ts
	p := prev.Page()
	flash := !a.nestedSecondary() || p != "threads" && p != "unreads" && prev.Channel() != ch
	if j, ok := a.convoFor(ch, thread).list.(interface{ JumpTo(ts string, flash bool) }); ok {
		j.JumpTo(ts, flash)
	}
	a.Invalidate()
}

// SecondaryAt is the message the secondary column was opened at
// (views.SavedSecondary): a conversation's startTs or a thread's replyTs,
// "" when it shows neither. qb0V Ub isSavedMessageSelectedInPrimaryView
// selects a saved message's row by them.
func (a *App) SecondaryAt() (ch, ts string) {
	s := &a.secondary
	if !s.open() || s.at == "" || s.kind != secChannel && s.kind != secThread {
		return "", ""
	}
	return s.channel, s.at
}

var _ views.SavedSecondary = (*App)(nil)
