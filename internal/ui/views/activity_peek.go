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
//   - Under the activity inbox (5Ns/ Lh) the header buttons are
//     ActivityPeekViewTabs in a .margin_left_50 div (8px) and then the
//     toggle, and .activityPeekView__FjmQY makes the buttons div flex 1,
//     space-between: the badges sit 8px after the title, the toggle at the
//     right. With the inbox on, ActivityPeekViewTabs is always the unread
//     badges (its All | DMs tab menu is the branch for !Lh && Vn, which
//     es never renders): DMs, Mentions and Threads, each only when its
//     count (3x6A cR) is above 0, nothing at all when none is
//     (.peekBadgeContainer: 22px tall; .peekBadgeItem: content-ter, a 16px
//     direct-messages / mentions / threads glyph 4px before the count,
//     13px bold, line-height 1; .peekBadgeSeparator: 1x18 otl-sec, margin
//     0 8px). Each badge's tooltip is "# unread DM(s)", "# unread
//     mention(s)" or "# unread thread(s)".
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
	"strconv"
	"time"

	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// actPeek is the peek's state on the page's Activity.
type actPeek struct {
	peeking bool
	list    widget.List
	close   func()
	route   string // the tab a row opens the item in
	lastUnr bool
	badges  [3]actPeekBadge // DMs, Mentions, Threads
}

// actPeekBadge is one .peekBadgeItem's hover and tooltip (F2XN).
type actPeekBadge struct {
	hv  gesture.Hover
	tip popups.TipState
}

// actPeekBadgeKinds are ActivityPeekViewTabs' badges in order: the
// viewType's index in cR (All, DMs, Mentions, Threads), the glyph, the
// tooltip.
var actPeekBadgeKinds = [3]struct {
	idx  int
	icon string
	tip  string
}{
	{1, "direct-messages", "{count, plural, =1 {# unread DM} other {# unread DMs}}"},
	{2, "mentions", "{count, plural, =1 {# unread mention} other {# unread mentions}}"},
	{3, "threads", "{count, plural, =1 {# unread thread} other {# unread threads}}"},
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

// PeekBadgeCounts are the badges ActivityPeekViewTabs shows (DMs,
// Mentions, Threads; 0 for one it leaves out), all 0 without the inbox.
func (v *Activity) PeekBadgeCounts() [3]int {
	var out [3]int
	c := v.h.Core()
	if c == nil || !c.ActivityInbox() {
		return out
	}
	counts, _ := c.ActivityTabCounts()
	for i, k := range actPeekBadgeKinds {
		out[i] = max(0, counts[k.idx])
	}
	return out
}

// LayoutPeekLead is ActivityPeekViewTabs: the unread badges after the
// title (the card puts it 8px after; nothing without the inbox or with
// every count 0).
func (v *Activity) LayoutPeekLead(gtx C) D {
	counts := v.PeekBadgeCounts()
	pp := peekPalOf(v.th)
	h := gtx.Dp(22)
	x := 0
	first := true
	for i, k := range actPeekBadgeKinds {
		n := counts[i]
		if n <= 0 {
			continue
		}
		if !first { // .peekBadgeSeparator: margin 0 8px
			x += gtx.Dp(8)
			sh := gtx.Dp(18)
			fill(gtx, image.Rect(x, (h-sh)/2, x+max(1, gtx.Dp(1)), (h-sh)/2+sh), pp.otlSec)
			x += max(1, gtx.Dp(1)) + gtx.Dp(8)
		}
		first = false
		b := &v.pk.badges[i]
		rec := recordTo(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.icon(gtx, k.icon, 16, pp.ter) }),
				hgap(4),
				layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, pp.ter, strconv.Itoa(n)) }),
			)
		})
		d := rec.d
		off := image.Pt(x, (h-d.Size.Y)/2)
		hov := b.hv.Update(gtx.Source)
		t := op.Offset(off).Push(gtx.Ops)
		rec.call.Add(gtx.Ops)
		st := clip.Rect{Max: d.Size}.Push(gtx.Ops)
		b.hv.Add(gtx.Ops)
		st.Pop()
		popups.Tooltip(gtx, v.th, &b.tip, hov, d.Size, popups.Above, nsActivity.T(k.tip, "count", n), "")
		t.Pop()
		x += d.Size.X
	}
	if first {
		return D{}
	}
	return D{Size: image.Pt(x, h)}
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

// peekCloser is PeekContext's maybeClosePeek for a menu opened now: the
// card's close while the list draws on it, else nothing.
func (v *Activity) peekCloser() func() {
	if c := v.pk.close; v.pk.peeking && c != nil {
		return c
	}
	return func() {}
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
