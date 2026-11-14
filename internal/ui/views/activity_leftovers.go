package views

// Activity's leftovers (docs/ACTIVITY.md "Leftovers 2", 2026-11-14,
// branch activity-leftovers), read from Slack's public CDN build
// 2c0d02ca130f (boot-shared mMdU: the list and rows, 5465 in d3de3a487b1c;
// GuYW, 6IMW, fNIs, IKpm, KJvs, m+ho, rZx3) and its client-boot-styles:
//
//   - the list's lead-in and the sticky headers' stuck mark (leadIn,
//     stickyHeader: activity_rows.go)
//   - agent entries' Block Kit actions (rZx3 Rq, mMdU's activity_actions)
//   - the tab strip's More (6IMW DraggableDroppableTabs'
//     DroppableOverflowMenuTab, useTabsOverflowMenuRenderer,
//     measureVisibleTabs)
//   - the sort button's "new" badge (KJvs useActivitySortNewBadge) and its
//     "Sort by" tooltip; the "+"'s tooltip (GuYW eT tooltipText)
//   - the bulk buttons' tooltips (m+ho ActivityFilterBulkAction)
//
// The In picker's blank-query suggestions are activity_in.go's, the
// "Discard changes?" speed bump activity_views.go's.

import (
	"context"
	"image"
	"image/color"
	"runtime"
	"strconv"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// actLeftovers is the view's state for the pieces above.
type actLeftovers struct {
	// the tab strip's More: GuYW's eo (the last tab shown; -1 before
	// the first measure, which shows them all), the tab's click and
	// press target (its window position), its x in the strip last frame
	// and the menu it opened
	last     int
	measured bool
	moreBtn  widget.Clickable
	moreTag  struct{}
	moreOff  image.Point
	moreX    int
	moreW    int
	moreMenu *popups.Menu
	hidden   []int // the tabs in the menu (tests)

	// tooltips: the sort button's "Sort by", the "+"'s, the bulk buttons'
	sortTip, plusTip popups.TipState
	bulkTips         [3]popups.TipState

	// the sort button's menu (ActivityFilterSort HEADER: f, between its
	// onPopoverOpen and onPopoverClose)
	sortMenu *popups.Menu
}

// ---- agent entries' Block Kit actions ----

// actActionsMaxValue is Rq's limit on a button's value (e.value.length:
// UTF-16 code units).
const actActionsMaxValue = 50

// actJSLen is a string's JavaScript length (UTF-16 code units).
func actJSLen(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// agentActionsBlock is rZx3 Rq: with agent_command_center (HLQ9 Lq), a
// row's message with blocks that is the entry's own (no feed_ts, or the
// message's ts is it) and has exactly one actions block, every element a
// button whose value (when it has one) is at most 50 characters, shows
// that block ({...a, blocks: [i]}). It returns the block's id and the
// block as the message's document has it.
func (v *Activity) agentActionsBlock(it *core.ActivityItem, m *core.Message) (string, richtext.Block, bool) {
	if it == nil || m == nil || len(m.Blocks) == 0 || !v.h.Core().AgentCommandCenter() {
		return "", richtext.Block{}, false
	}
	if it.FeedTS != "" && m.TS != it.FeedTS {
		return "", richtext.Block{}, false
	}
	var acts []blockkit.Block
	for _, b := range blockkit.ParseBlocks(m.Blocks) {
		if b.Type == "actions" {
			acts = append(acts, b)
		}
	}
	if len(acts) != 1 {
		return "", richtext.Block{}, false
	}
	for _, e := range acts[0].Elements {
		if e.Type != "button" || actJSLen(e.Value) > actActionsMaxValue {
			return "", richtext.Block{}, false
		}
	}
	bid := acts[0].BlockID
	for _, b := range m.Doc.Blocks {
		if b.Kind == richtext.Actions && len(b.Buttons) > 0 && b.Buttons[0].BlockID == bid {
			return bid, b, true
		}
	}
	return "", richtext.Block{}, false // no buttons to draw
}

// actRowActions is a row's last Rq answer, for the message's blocks
// (blocksSig) and the experiment's group.
type actRowActions struct {
	sig uint64
	on  bool
	ok  bool
	bid string
	blk richtext.Block
}

// agentActions is agentActionsBlock for the row's message, kept while its
// blocks and the experiment stay the same.
func (r *actRow) agentActions(m *core.Message) (string, richtext.Block, bool) {
	on := r.v.h.Core().AgentCommandCenter()
	if !on || len(m.Blocks) == 0 {
		return "", richtext.Block{}, false
	}
	sig := blocksSig(m)
	for _, c := range m.TS + "\x00" + r.it.FeedTS { // FNV-1a on: the ts check's inputs
		sig ^= uint64(c)
		sig *= 1099511628211
	}
	if c := &r.actRq; c.on != on || c.sig != sig {
		bid, blk, ok := r.v.agentActionsBlock(r.it, m)
		*c = actRowActions{sig: sig, on: on, ok: ok, bid: bid, blk: blk}
	}
	return r.actRq.bid, r.actRq.blk, r.actRq.ok
}

// actActionsEls is the agent row's element host: blockkit's for the
// message (live: activity_actions isn't one of the contexts that disable
// elements, unlike the body's "activity"), with a press target over each
// element it drew.
type actActionsEls struct {
	me  *blockkit.Message
	bid string
	tag *struct{}
}

func (e *actActionsEls) ElementsSize(bs []richtext.Button, maxW int, accessory bool, m unit.Metric) (image.Point, bool) {
	return e.me.ElementsSize(bs, maxW, accessory, m)
}

func (e *actActionsEls) ElementsVersion() uint32 { return e.me.ElementsVersion() }

func (e *actActionsEls) LayoutElements(gtx layout.Context, bs []richtext.Button, accessory bool) layout.Dimensions {
	d := e.me.LayoutElements(gtx, bs, accessory)
	for _, r := range e.me.RunRects(e.bid) {
		area := clip.Rect(r).Push(gtx.Ops)
		pass := pointer.PassOp{}.Push(gtx.Ops)
		event.Op(gtx.Ops, e.tag)
		pass.Pop()
		area.Pop()
	}
	return d
}

var _ richview.Elements = (*actActionsEls)(nil)

// agentActions draws the actions block in entry_extras (x48o's renderer
// for the message, blocksContainerContext "activity_actions").
func (v *Activity) agentActions(gtx C, r *actRow, m *core.Message, bid string, blk richtext.Block) D {
	o := richview.Options{Channel: m.Channel, TS: m.TS}
	if el := v.liveElements(m); el != nil {
		if me, ok := el.(*blockkit.Message); ok {
			if r.actEls == nil || r.actEls.me != me || r.actEls.bid != bid {
				r.actEls = &actActionsEls{me: me, bid: bid, tag: &r.actTag}
			}
			o.Elements = r.actEls
		} else {
			o.Elements = el
		}
	}
	key := "activity_actions/" + m.Channel + "/" + m.TS
	ver := strconv.FormatUint(blocksSig(m), 36)
	gtx.Constraints.Min.X = 0
	d := v.richDoc(gtx, key, ver, richtext.Doc{Blocks: []richtext.Block{blk}}, o, 0)
	r.actSize = d.Size
	return d
}

// agentActionsClicked reads the press target over the actions' buttons:
// a release on one is a click on a BUTTON, which the row's own click
// (ControlledClick) skips; the wrapper's onClickCapture marks an unread
// row read (Japw m$).
func (v *Activity) agentActionsClicked(gtx C, r *actRow) bool {
	hit := false
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &r.actTag, Kinds: pointer.Release})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || r.actEls == nil {
			continue
		}
		p := image.Pt(int(e.Position.X), int(e.Position.Y))
		for _, rc := range r.actEls.me.RunRects(r.actEls.bid) {
			if p.In(rc) {
				hit = true
			}
		}
	}
	return hit
}

// ---- the tab strip's More ----

// actMoreReserve is measureVisibleTabs' 80px kept clear once the tabs
// overflow.
const actMoreReserve = 80

// actStripLast is measureVisibleTabs on the strip as it is laid out with
// prev the last tab shown: tab i's button is bw[i] wide with mr[i] after
// it, and the tabs after prev sit past the More tab and the "+" (extra
// px), as they do in Slack's DOM. When every button is within the strip
// (right ≤ width + 1) all show; else those whose right edge is within
// width − 80 (+1), the first at least (Math.max(eo, 0)).
func actStripLast(bw, mr []int, prev, extra, width, reserve int) int {
	n := len(bw)
	if n == 0 {
		return -1
	}
	if prev < 0 || prev >= n {
		prev = n - 1
	}
	rights := make([]int, n)
	x := 0
	for i, w := range bw {
		if i == prev+1 {
			x += extra
		}
		rights[i] = x + w
		x += w + mr[i]
	}
	fit := 0
	for _, r := range rights {
		if r <= width+1 {
			fit++
		}
	}
	if fit >= n {
		return n - 1
	}
	c := 0
	for _, r := range rights {
		if r <= width-reserve+1 {
			c++
		}
	}
	return max(c-1, 0)
}

// moreTabWidth is the More tab with its margin: 8px (margin-left), then
// the button (c-tabs__tab padding 8px 0) round overflow_label (padding
// 8px): "More" (13px bold) and the caret-down at 1em.
func (v *Activity) moreTabWidth(gtx C) int {
	return gtx.Dp(8) + gtx.Dp(8) + v.th.Measure(gtx, 13, wBold, actMoreLabel()) + gtx.Dp(13) + gtx.Dp(8)
}

// actMoreLabel is DroppableOverflowMenuTab's label ("More", tabbed-channel).
func actMoreLabel() string { return nsTabbed.T("More") }

// plusTabWidth is the "+" with its margin (p-activity_ia4_page__tab_container__add-tab
// button: margin-left 8px; the 24px icon button).
func plusTabWidth(gtx C) int { return gtx.Dp(8) + gtx.Dp(24) }

// moreTab draws the More tab at x (strip coordinates, h tall) for the
// tabs after last; cur is the tab shown. It opens the overflow menu.
func (v *Activity) moreTab(gtx C, x, h, last, cur int) int {
	lo := &v.lo
	w := v.moreTabWidth(gtx) - gtx.Dp(8)
	x += gtx.Dp(8)
	lo.moreX, lo.moreW = x, w
	sel := cur > last // ex: the tab shown is in the menu
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &lo.moreTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			lo.moreOff = v.pointerAt().Sub(image.Pt(int(e.Position.X+.5), int(e.Position.Y+.5)))
		}
	}
	// before its Layout, which drains the clickable's events
	if lo.moreBtn.Clicked(gtx) {
		// moreOff is the tab's window origin (its press: window − local)
		v.openMoreMenu(last, cur, image.Rectangle{Max: image.Pt(w, h)}.Add(lo.moreOff))
	}
	func() {
		defer op.Offset(image.Pt(x, 0)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		lo.moreBtn.Layout(g, func(gtx C) D {
			pointerCursor(gtx)
			hov := lo.moreBtn.Hovered()
			fg := v.p.Sec
			if hov || sel {
				fg = v.p.Text // :hover, c-tabs__tab--active
			}
			if hov {
				// theme-surf-sec, radius 8px 8px 0 0
				r := gtx.Dp(8)
				rr := clip.RRect{Rect: image.Rectangle{Max: image.Pt(w, h)}, NE: r, NW: r}
				paint.FillShape(gtx.Ops, v.p.TabHover, rr.Op(gtx.Ops))
			}
			if sel {
				lh := gtx.Dp(2) // inset 0 -2px 0 0 theme-base-pry
				fill(gtx, image.Rect(0, h-lh, w, h), v.p.TabLine)
			}
			label := actMoreLabel()
			lw := v.th.Measure(gtx, 13, wBold, label)
			dirAt(gtx, image.Pt(gtx.Dp(8), 0), func(gtx C) D {
				gtx.Constraints = layout.Exact(image.Pt(lw+1, h))
				return layout.W.Layout(gtx, func(gtx C) D { return v.label(gtx, 13, wBold, fg, label) })
			})
			s := gtx.Dp(13)
			dirAt(gtx, image.Pt(gtx.Dp(8)+lw, (h-s)/2), func(gtx C) D { return v.icon(gtx, "caret-down", 13, fg) })
			return D{Size: image.Pt(w, h)}
		})
		area := clip.Rect(image.Rectangle{Max: image.Pt(w, h)}).Push(gtx.Ops)
		pass := pointer.PassOp{}.Push(gtx.Ops)
		event.Op(gtx.Ops, &lo.moreTag)
		pass.Pop()
		area.Pop()
	}()
	return gtx.Dp(8) + w
}

// openMoreMenu opens useTabsOverflowMenuRenderer's menu
// (c-channel_tabs_overflow_menu, 200px, "bottom-left" of the More tab):
// each hidden tab as a radio row (checked when it's the tab shown) whose
// label is the tab's own (.c-menu_item__label .p-activity_ia4_page__tab_label:
// the 16px icon or emoji, 4px, the name; min-height 28px), and a
// right-click on a row opens that tab's context menu.
func (v *Activity) openMoreMenu(last, cur int, anchor image.Rectangle) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	m := popups.NewMenuOpts(ph, v.moreItems(last, cur), popups.MenuOpts{Anchor: anchor, Width: 200, Popover: "bottom-left",
		MenuClass: "c-channel_tabs_overflow_menu"})
	v.lo.moreMenu = m
	ph.ShowOverlay(m)
}

// moreItems are the menu's rows for the tabs after last.
func (v *Activity) moreItems(last, cur int) []popups.MenuItem {
	t := v.tabs
	xi := v.h.Core().ActivityLayoutRevisions()
	right := v.h.Core().TabOverflowRightClick()
	counts := v.tabCounts()
	var items []popups.MenuItem
	for i := last + 1; i < len(t.Labels); i++ {
		i := i
		slot := i
		if i < len(v.slots) {
			slot = v.slots[i]
		}
		count := 0
		if slot < len(counts) {
			count = counts[slot]
		}
		label := t.label(i)
		it := popups.MenuItem{Key: "activity_tab_" + strconv.Itoa(slot), Label: label, Radio: true, Checked: i == cur,
			Do: func() { v.pickTab(i) }}
		it.LabelText = func(gtx layout.Context, fg color.NRGBA) layout.Dimensions {
			return v.moreItemLabel(gtx, slot, label, fg, count, xi)
		}
		if right { // channel_tab_overflow_right_click: the row's tab menu
			it.Context = func(at image.Point) { v.tabMenuAt(i, at) }
		}
		items = append(items, it)
	}
	return items
}

// moreItemLabel is a menu row's label: the tab's lead (16px), 4px, the
// name; without layout revisions a default view's count follows it
// (the badge span keeps no margin out of the tab strip).
func (v *Activity) moreItemLabel(gtx C, slot int, label string, fg color.NRGBA, count int, xi bool) D {
	h := gtx.Dp(28)
	g := gtx
	g.Constraints.Min.Y = h
	return layout.Flex{Alignment: layout.Middle}.Layout(g,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.Y = h
			return layout.W.Layout(gtx, func(gtx C) D { return v.tabLead(gtx, slot, fg) }) // 16px, 4px after
		}),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.Y = h
			return layout.W.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wReg, fg, label) })
		}),
		layout.Rigid(func(gtx C) D {
			if actTabBadgeKind(slot, count, xi) != "count" {
				return D{}
			}
			gtx.Constraints.Min.Y = h
			return layout.W.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wReg, fg, strconv.Itoa(count)) })
		}),
	)
}

// pickTab shows tab i, as a click on it does (onTabChange).
func (v *Activity) pickTab(i int) {
	if i >= 0 && i < len(v.tabs.clicks) {
		v.tabs.clicks[i].Click()
		v.h.Invalidate()
	}
}

// ---- tooltips and the sort button's new badge ----

// ActivitySortNewBadge is KJvs's badge name: the megaphone's
// generic_new_badge space turns it on (NWiV ap).
const ActivitySortNewBadge = "activity_sort_new_badge"

// sortNewBadge is useActivitySortNewBadge: with activity_global_settings
// (5Ns/ XW) and the badge in the space.
func (v *Activity) sortNewBadge() bool {
	return v.globalSettings() && v.h.Core().MegaphoneNewBadge(ActivitySortNewBadge)
}

// actAltKey is ActivityFilterBulkAction's modifier name: "Option" on a
// Mac, else "Alt" (cX isMac).
func actAltKey() string {
	if runtime.GOOS == "darwin" {
		return "Option"
	}
	return "Alt"
}

// sortTrigger is ActivityFilterSort's HEADER trigger (rTMD, 28px,
// radius 8): the 20px sort icon in content-sec; hovered, base-ter under
// content-pry; base-ter while its menu is open (sortTriggerActive); the
// "Sort by" tooltip below. While the new badge shows it's sortTriggerGlow
// instead (no hover fill): a 1px otl-education border, a 0 0 2px
// plt-indigo-30 glow, padding 0 8px, content-pry, the icon, 4px (gap),
// then EducationTag's micro inverse "New". Its menu's close
// (onPopoverClose) dismisses the badge (recordAndClearNewBadge).
func (v *Activity) sortTrigger(gtx C) D {
	lo := &v.lo
	glow := v.sortNewBadge()
	if lo.sortMenu != nil && lo.sortMenu.Dismissed() {
		lo.sortMenu = nil
		if glow {
			c := v.h.Core()
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				_ = c.DismissNewBadge(ctx, ActivitySortNewBadge)
			}()
		}
	}
	open := lo.sortMenu != nil
	h := gtx.Dp(28)
	d := v.sortBtn.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		hov := v.sortBtn.Hovered()
		rad := gtx.Dp(8)
		if !glow {
			sz := image.Pt(h, h)
			fg := v.p.Sec
			if hov || open {
				rrect(gtx, image.Rectangle{Max: sz}, rad, v.p.Track) // base-ter
			}
			if hov {
				fg = v.p.Text
			}
			dirAt(gtx, image.Pt((h-gtx.Dp(20))/2, (h-gtx.Dp(20))/2), func(gtx C) D { return v.icon(gtx, "sort", 20, fg) })
			return D{Size: sz}
		}
		tag := popups.MenuTag{Label: i18n.T("education", "New"), Style: "inverse-education"}
		tw := popups.MicroTagWidth(gtx, v.th, tag.Label)
		one := max(1, gtx.Dp(1))
		w := 2*one + gtx.Dp(8+20+4) + tw + gtx.Dp(8)
		sz := image.Pt(w, h)
		r := image.Rectangle{Max: sz}
		glowCol, edu := rgb(0x7d95eb), rgb(0x5f7be1) // rgb(var(--dt_color-plt-indigo-30)), --dt_color-otl-education
		if v.th.Dark {
			glowCol, edu = rgb(0x263b9c), rgb(0xe4e8f4)
		}
		theme.Shadow(gtx, r, rad, gtx.Dp(2), glowCol)
		border(gtx, r, rad, one, edu)
		dirAt(gtx, image.Pt(one+gtx.Dp(8), (h-gtx.Dp(20))/2), func(gtx C) D { return v.icon(gtx, "sort", 20, v.p.Text) })
		dirAt(gtx, image.Pt(one+gtx.Dp(8+20+4), 0), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(tw, h))
			return popups.DrawMicroTag(gtx, v.th, &tag)
		})
		return D{Size: sz}
	})
	popups.Tooltip(gtx, v.th, &lo.sortTip, v.sortBtn.Hovered(), d.Size, popups.Below, nsActivity.T("Sort by"), "")
	return d
}
