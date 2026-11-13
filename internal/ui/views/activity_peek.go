package views

// The rail's Activity peek: boot-shared gOQ8 ActivityPeekTrigger (web
// client build f132f0e78468), on the Activity tab (Activity, the inbox or
// the inbox-with-sidebar prototype), and on Home instead while Activity
// lives in Home (activity_in_home_prototype, 6rqO j) and Home isn't the
// active tab (Zxh6's Home renderWrapper):
//
//   - The view (es) is a PeekView with classNameSuffix "activity" and the
//     header "Activity" (ia4). Its header button is the Unreads toggle
//     (x96h; ActivityUnreadsToggleButtonV2 under the activity inbox), the
//     page's own Unreads filter (the same redux state: the page's
//     instance here).
//   - The content is the Activity page (boot-shared 5465 with
//     p-activity_ia4_page--peek, shouldTrackScroll false): the page's
//     list on the card, its own scroll position, the page's skeleton rows
//     while it loads and its empty states.
//   - .p-peek_card .p-activity_ia4_page__item:hover: base-pry under
//     base-pry-hover.
//   - A row closes the peek (maybeClosePeek) and opens the item in the
//     Activity tab (forceTabWhenNavigating: the tab, or Home while
//     Activity lives there).

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
)

// actPeek is the peek's state on the page's Activity.
type actPeek struct {
	peeking bool
	list    widget.List
	close   func()
	route   string // the tab a row opens the item in
	lastUnr bool
}

// PeekTitle is the card's header.
func (v *Activity) PeekTitle() string { return nsIA4.T("Activity") }

// PeekOpened is the view's mount (es: fetchActivity's refresh, rsjQ a):
// the list starts at the top; route is the tab the rows open in.
func (v *Activity) PeekOpened(closePeek func(), route string) {
	v.pk.close, v.pk.route = closePeek, route
	v.pk.list.Position = layout.Position{}
	v.pk.list.Axis = layout.Vertical
	v.pk.lastUnr = v.unread.Value
	if v.data.started() && !v.data.busy() {
		v.load(actFresh)
	}
}

// LayoutPeekButtons is the Unreads toggle.
func (v *Activity) LayoutPeekButtons(gtx C) D {
	v.frame()
	return v.peekToggle(gtx, &v.unread, nsMentions.T("Unreads"))
}

// LayoutPeek draws the page's list on the card.
func (v *Activity) LayoutPeek(gtx C) D {
	v.pk.peeking = true
	defer func() { v.pk.peeking = false }()
	v.frame()
	v.watchItems()
	v.loadDensity()
	if !v.data.started() {
		v.load(actFresh)
	}
	v.notes.drain(v.h)
	v.applyPatches()
	if v.unread.Value != v.pk.lastUnr { // the header's toggle: the page's reload
		v.pk.lastUnr = v.unread.Value
		v.endSelect()
		v.emptyPick = int(time.Now().UnixNano() % int64(len(actUnreadsEmpty)))
		v.pk.list.Position = layout.Position{}
		v.load(actFresh)
	}
	v.settle()
	size := gtx.Constraints.Max
	gtx.Constraints = layout.Exact(size)
	fill(gtx, image.Rectangle{Max: size}, peekPalOf(v.th).bg)
	// the page's list code with the peek's scroll position
	v.list, v.pk.list = v.pk.list, v.list
	defer func() { v.list, v.pk.list = v.pk.list, v.list }()
	v.items(gtx)
	return D{Size: size}
}

// openFromPeek is a row's click on the card.
func (v *Activity) openFromPeek(it *core.ActivityItem, key string, r *actRow) {
	if v.pk.close != nil {
		v.pk.close()
	}
	route := v.pk.route
	if route == "" {
		route = "activity"
	}
	v.h.Navigate(route)
	v.focusKey = key
	v.choose(it, key, r)
}

// PeekCount is the rows the card lists (-1 while it loads).
func (v *Activity) PeekCount() int {
	page, ok, _ := v.data.get()
	if !ok && len(page.Items) == 0 {
		return -1
	}
	return len(v.vis)
}

// Unreads reports the Unreads filter (the tab's and the peek's).
func (v *Activity) Unreads() bool { return v.unread.Value }

// ShowTab switches to the tab strip's i'th view (All, DMs, Mentions,
// Threads, then saved views) as a click on it does (the demo's surface
// walk, internal/ui TestDemoSurfaces).
func (v *Activity) ShowTab(i int) {
	if i < 0 || i >= len(v.slots) || i == v.tabs.Sel {
		return
	}
	prev := v.slot()
	v.tabs.Sel = i
	v.switchView(prev)
	v.load(actFresh)
}
