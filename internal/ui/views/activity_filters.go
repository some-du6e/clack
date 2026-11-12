package views

// The filter menu as Slack's activity inbox draws it (gantry
// client-boot-render, build 4d0c78085232; docs/ACTIVITY.md "Filters"):
//
//   - Its rows are useActivityFilterOptions (vQqF w): the types (sTqq
//     a6: Unreads, DMs, Mentions, Threads, Channels, Reactions,
//     Invitations, Apps, Reminders while reminders show in Activity), then
//     the additional filters (sTqq Fq: VIP, Cleared items, "Exclude
//     automations" with activity_bot_item_type_reads, "Automations only"
//     with activity_automations_only, "Specific channel or section").
//     With exclude_automations_filter the two automation rows give way to
//     the "Notifications from" select over the list (People, apps, and
//     agents / People only / Apps and agents only) and Unreads leads the
//     additional filters.
//   - ActivityFilterByMenu (lCwf) leaves Unreads out on the All tab and
//     the default views, whose bar has the Unreads button, and DMs out
//     while DMs don't show in Activity; draws a rule after Unreads and one
//     before the additional filters (with the select: one between the
//     types and the additional filters), "Filter by entry type" over the
//     first type when Unreads is listed, "Additional Filters" under the
//     last rule. Mentions has the "Toggle mention sub-filters" caret: @you,
//     @channel, @user-group, keyword (sTqq o$, 44px in, no icon); not under
//     "Apps and agents only". The list is ceil((rows + 1, or 1.85 with
//     Unreads) × 28)px and clips (p-activity_ia4_page__filter-menu__list-container).
//   - "People only" disables Apps, "Apps and agents only" Reminders and
//     DMs (sTqq pP / Fe: content-ter, unchecked, no clicks); choosing one
//     drops those types and widens a partial mention pick to all four
//     (m+ho onSourceChange).
//   - The menu stores what Slack's view stores (filterActivityTypes): the
//     Mentions row picks the four sub-filters, a sub-filter under the
//     whole Mentions type (a suggested Mentions view) takes the other
//     three (m+ho onFilterBySelected); the count after "Filters" counts
//     each one (3x6A ug).
//   - The feed's types are Ku1y j's (a sub-filter: at_user and
//     unjoined_channel_mention, at_channel and at_everyone, at_user_group,
//     keyword; the whole Mentions type also the list mentions); a saved
//     view keeps RmAw r's entry types and comes back through sTqq G$.

import (
	"image"
	"math"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// actSubN is the mention sub-filters (sTqq o$): mention_at_user,
// mention_at_channel, mention_at_user_group, mention_keyword.
const actSubN = 4

// actSubLabels are g7lN's labels; the first is "@" and the viewer's name.
var actSubLabels = [actSubN]string{"@", "@channel", "@user-group", "keyword"}

// actSubFeedTypes are Ku1y's activity.feed types for the sub-filters,
// actSubEntryTypes RmAw r's (a saved view's entry_types).
var (
	actSubFeedTypes  = [actSubN]string{"at_user,unjoined_channel_mention", "at_channel,at_everyone", "at_user_group", "keyword"}
	actSubEntryTypes = [actSubN][]string{{"at_user"}, {"at_channel", "at_everyone"}, {"at_user_group"}, {"keyword"}}
)

// actMentionsFeed is Ku1y's whole "mentions": what a suggested Mentions
// view asks for until a reload maps its entry types to the sub-filters.
const actMentionsFeed = "at_user,at_user_group,at_channel,at_everyone,keyword,list_record_assigned,list_user_mentioned,list_todo_notification,list_approval_request,list_approval_reviewed,unjoined_channel_mention"

// actRowMentions is the Mentions row in actFilters.
const actRowMentions = 1

// actOpt is one of the menu's entries (Slack's uScM Li ids).
type actOpt uint8

const (
	optUnreads actOpt = iota
	optType           // a type row (actFilters[i])
	optSub            // a mention sub-filter (i)
	optVIP
	optCleared
	optExcludeAuto
	optAutoOnly
	optSpecific
	optDivider
)

type actEntry struct {
	opt actOpt
	i   int
}

// Slack's "Notifications from" sources (lCwf ep).
const (
	srcEveryone    = "everyone"
	srcUsers       = "users"
	srcAutomations = "automations"
)

// actSrcLabels are the select's options, in order.
var actSrcLabels = []string{"People, apps, and agents", "People only", "Apps and agents only"}

func actSrcIndex(s string) int {
	switch s {
	case srcUsers:
		return 1
	case srcAutomations:
		return 2
	}
	return 0
}

// actFilterUI is the menu's own state: the sub-filters' expander, its
// rows' click targets and the source select.
type actFilterUI struct {
	subOpen   bool // the Mentions row's caret is open (lCwf G)
	unreadC   widget.Clickable
	caretC    widget.Clickable
	subC      [actSubN]widget.Clickable
	autoC     [2]widget.Clickable // Exclude automations, Automations only
	src       dirDropdown         // "Notifications from"
	srcInit   bool
	srcShown  int                // the source the select shows
	menuRowsN int                // rows the menu listed last (tests)
	segTips   [2]popups.TipState // the layout toggle's tooltips (FgHy)
}

// source is lCwf ep: "Apps and agents only" with filterAutomationsOnlySelected,
// "People only" with filterExcludeAutomationsSelected, else everyone.
func (s actState) source() string {
	switch {
	case s.autoOnly:
		return srcAutomations
	case s.exclAuto:
		return srcUsers
	}
	return srcEveryone
}

// actSourceDisables is sTqq Fe: what a source disables (pP: users →
// Apps; automations → Reminders, DMs).
func actSourceDisables(src string, e actEntry) bool {
	if e.opt != optType {
		return false
	}
	switch src {
	case srcUsers:
		return e.i == 6 // Apps
	case srcAutomations:
		return e.i == 7 || e.i == 0 // Reminders, DMs
	}
	return false
}

// remindersInActivity is 5Ns/ ah's global_reminders_in_activity.
func (v *Activity) remindersInActivity() bool {
	return v.h.Core().GlobalNotifyBool("global_reminders_in_activity", true)
}

// filterOptions is vQqF w: the type options and the additional ones.
func (v *Activity) filterOptions() (types, extras []actEntry) {
	c := v.h.Core()
	uk := c.ActivityExcludeAutomationsFilter()
	if !uk {
		types = append(types, actEntry{opt: optUnreads})
	}
	for i := range actFilters {
		if i == len(actFilters)-1 && c.ActivityInbox() && !v.remindersInActivity() {
			continue
		}
		types = append(types, actEntry{opt: optType, i: i})
	}
	if uk {
		extras = append(extras, actEntry{opt: optUnreads})
	}
	extras = append(extras, actEntry{opt: optVIP}, actEntry{opt: optCleared})
	if !uk && c.ActivityBotItemTypeReads() {
		extras = append(extras, actEntry{opt: optExcludeAuto})
	}
	if !uk && c.ActivityAutomationsOnly() {
		extras = append(extras, actEntry{opt: optAutoOnly})
	}
	return types, append(extras, actEntry{opt: optSpecific})
}

// actMenuList is the menu's list: its entries (lCwf eN, with the rules),
// which entries get "Filter by entry type" over them and "Additional
// Filters" under them, and the list's height in dp.
type actMenuList struct {
	entries  []actEntry
	typeHead int // the entry under "Filter by entry type" (-1: none)
	extHead  int // the rule over "Additional Filters" (-1: none)
	height   int // dp
}

// menuEntries is lCwf's list for the page's menu (ed: the All tab or a
// default view, whose bar has Unreads; the ADD_TAB grid always is).
func (v *Activity) menuEntries(ed bool, st actState) actMenuList {
	c := v.h.Core()
	uk := c.ActivityExcludeAutomationsFilter()
	types, extras := v.filterOptions()
	all := append(append([]actEntry(nil), types...), extras...) // O
	inD := func(e actEntry) bool {
		for _, d := range extras {
			if d == e {
				return true
			}
		}
		return false
	}
	var es []actEntry // eS
	for _, e := range all {
		if ed && e.opt == optUnreads {
			continue // eu ? filter Unreads : slice(1)
		}
		if e.opt == optType && e.i == 0 && !v.dmsInActivity() {
			continue // Vn
		}
		es = append(es, e)
	}
	m := actMenuList{typeHead: -1, extHead: -1}
	var withRules []actEntry // ex
	for n, a := range es {
		s := !uk && a.opt == optUnreads
		r := false
		if n+1 < len(es) {
			if uk {
				r = !inD(a) && inD(es[n+1])
			} else {
				r = es[n+1] == extras[0]
			}
		}
		withRules = append(withRules, a)
		if s || r {
			withRules = append(withRules, actEntry{opt: optDivider})
		}
	}
	// eN: the sub-filters under Mentions while its caret is open (eI: not
	// under "Apps and agents only")
	open := v.fl.subOpen && (!uk || st.source() != srcAutomations)
	for _, e := range withRules {
		m.entries = append(m.entries, e)
		if open && e.opt == optType && e.i == actRowMentions {
			for i := 0; i < actSubN; i++ {
				m.entries = append(m.entries, actEntry{opt: optSub, i: i})
			}
		}
	}
	// "Filter by entry type": eu ? !ed && O[0] : O[+!ed]
	var first actEntry
	switch {
	case uk && !ed && len(all) > 0:
		first = all[0]
	case !uk && !ed && len(all) > 1:
		first = all[1]
	case !uk && ed && len(all) > 0:
		first = all[0] // Unreads, which ed left out: no heading
	default:
		first = actEntry{opt: optDivider}
	}
	for k, e := range m.entries {
		if e.opt != optDivider && e == first && m.typeHead < 0 {
			m.typeHead = k
		}
		if e.opt == optDivider && k+1 < len(m.entries) {
			next := m.entries[k+1]
			if uk && inD(next) || !uk && next == extras[0] {
				m.extHead = k
			}
		}
	}
	extra := 1.85
	if ed {
		extra = 1
	}
	m.height = int(math.Ceil((float64(len(m.entries)) + extra) * 28))
	return m
}

// mentionsOn is lCwf ee: the whole Mentions type or all four sub-filters;
// mentionsSome et: some of them.
func (s actState) mentionsOn() bool {
	return s.filters[actRowMentions] || s.subs == [actSubN]bool{true, true, true, true}
}

func (s actState) mentionsSome() bool {
	if s.mentionsOn() {
		return false
	}
	for _, on := range s.subs {
		if on {
			return true
		}
	}
	return false
}

// subOn is lCwf eB: a sub-filter is on, or the whole Mentions type is.
func (s actState) subOn(i int) bool { return s.filters[actRowMentions] || s.subs[i] }

// toggleMentions is m+ho onFilterBySelected for Mentions: all four
// sub-filters, or none when they (or the whole type) were on.
func (s *actState) toggleMentions() {
	on := s.mentionsOn()
	s.filters[actRowMentions] = false
	for i := range s.subs {
		s.subs[i] = !on
	}
}

// toggleSub is onFilterBySelected for a sub-filter: under the whole
// Mentions type the other three, else the sub-filter flips.
func (s *actState) toggleSub(i int) {
	if s.filters[actRowMentions] {
		s.filters[actRowMentions] = false
		for k := range s.subs {
			s.subs[k] = k != i
		}
		return
	}
	s.subs[i] = !s.subs[i]
}

// setSource is m+ho onSourceChange (the bar) and fNIs W (a new view):
// the automation flags follow the source, the types it disables go, and
// "Apps and agents only" widens a partial mention pick to all four.
func (s *actState) setSource(src string) {
	s.exclAuto, s.autoOnly = src == srcUsers, src == srcAutomations
	switch src {
	case srcUsers:
		s.filters[6] = false
	case srcAutomations:
		s.filters[7], s.filters[0] = false, false
		some := false
		for _, on := range s.subs {
			some = some || on
		}
		if some && s.subs != [actSubN]bool{true, true, true, true} {
			s.subs = [actSubN]bool{true, true, true, true}
		}
	}
}

// filterChecked is lCwf eF for one entry: on in the state and not
// disabled by the source.
func (v *Activity) filterChecked(st actState, e actEntry) bool {
	uk := v.h.Core().ActivityExcludeAutomationsFilter()
	if uk && actSourceDisables(st.source(), e) {
		return false
	}
	switch e.opt {
	case optUnreads:
		return st.unread
	case optType:
		if e.i == actRowMentions {
			return st.mentionsOn()
		}
		return st.filters[e.i]
	case optSub:
		return st.subOn(e.i)
	case optVIP:
		return st.vip
	case optCleared:
		return st.cleared
	case optExcludeAuto:
		return st.exclAuto
	case optAutoOnly:
		return st.autoOnly
	case optSpecific:
		return st.showIn
	}
	return false
}

// entryLabel is g7lN's label for an entry.
func (v *Activity) entryLabel(e actEntry) string {
	switch e.opt { // g7lN's labels (the mentions namespace)
	case optUnreads:
		return nsMentions.T("Unreads")
	case optType:
		return nsMentions.T(actFilters[e.i].label)
	case optSub:
		if e.i == 0 {
			c := v.h.Core()
			return "@" + c.S.UserName(c.S.SelfID())
		}
		return nsMentions.T(actSubLabels[e.i])
	case optVIP:
		return nsMentions.T(actExtras[0].label)
	case optCleared:
		return nsMentions.T(actExtras[1].label)
	case optExcludeAuto:
		return nsMentions.T("Exclude automations")
	case optAutoOnly:
		return nsMentions.T("Automations only")
	case optSpecific:
		return nsMentions.T(actExtras[2].label)
	}
	return ""
}

// entryIcon is /7yB L's icon (none for the sub-filters).
func entryIcon(e actEntry) string {
	switch e.opt {
	case optUnreads:
		return "mark-as-unread"
	case optType:
		return actFilters[e.i].icon
	case optVIP:
		return actExtras[0].icon
	case optCleared:
		return actExtras[1].icon
	case optExcludeAuto, optAutoOnly:
		return "apps"
	case optSpecific:
		return actExtras[2].icon
	}
	return ""
}

// edView is lCwf ed for the page: the All tab or a default view.
func (v *Activity) edView() bool { return v.slot() < actSlotN }

// menuClicks applies the menu's clicks to the shown view's filters (m+ho
// onFilterBySelected / onSourceChange) and reports whether the feed must
// load again.
func (v *Activity) menuClicks(gtx C) bool {
	st := v.curState()
	uk := v.h.Core().ActivityExcludeAutomationsFilter()
	src := st.source()
	reload, changed := false, false
	if v.fl.caretC.Clicked(gtx) {
		v.fl.subOpen = !v.fl.subOpen
	}
	if v.fl.unreadC.Clicked(gtx) {
		st.unread, reload = !st.unread, true
	}
	for i := range v.filterC {
		if !v.filterC[i].Clicked(gtx) || actFilters[i].off {
			continue
		}
		if uk && actSourceDisables(src, actEntry{opt: optType, i: i}) {
			continue
		}
		if i == actRowMentions {
			st.toggleMentions()
		} else {
			st.filters[i] = !st.filters[i]
		}
		changed = true
		if !actFilters[i].dm {
			reload = true
		}
	}
	for i := range v.fl.subC {
		if v.fl.subC[i].Clicked(gtx) {
			st.toggleSub(i)
			reload = true
		}
	}
	if v.extraC[0].Clicked(gtx) {
		// VIP (filterVipSelected: activity.feed priority_only)
		st.vip, reload = !st.vip, true
	}
	if v.extraC[1].Clicked(gtx) {
		// "Cleared items" shows the archive (activity.feed archive_only)
		st.cleared, reload = !st.cleared, true
	}
	if v.extraC[2].Clicked(gtx) {
		// "Specific channel or section": showFilterIn and the picker
		// opens (m+ho te: e6("showFilterIn", true), ec(true))
		st.showIn = true
		v.menu = actMenuNone
		v.in.openSoon = true
		changed = true
	}
	if v.fl.autoC[0].Clicked(gtx) {
		st.exclAuto, reload = !st.exclAuto, true
	}
	if v.fl.autoC[1].Clicked(gtx) {
		st.autoOnly, reload = !st.autoOnly, true
	}
	if uk {
		v.syncSourceSelect(st.source())
		if v.fl.src.update(gtx) {
			if s := []string{srcEveryone, srcUsers, srcAutomations}[v.fl.src.sel]; s != src {
				st.setSource(s)
				v.fl.srcShown = v.fl.src.sel
				reload = true
			}
		}
	}
	if reload || changed {
		v.setState(st)
		if !reload {
			v.dirty = true
		}
	}
	return reload
}

// syncSourceSelect keeps the select on the view's source.
func (v *Activity) syncSourceSelect(src string) {
	if !v.fl.srcInit {
		v.fl.src = dirDropdown{labels: actSrcLabels, ns: nsActivity}
		v.fl.srcInit = true
	}
	if i := actSrcIndex(src); i != v.fl.src.sel && !v.fl.src.open {
		v.fl.src.sel = i
	}
	v.fl.srcShown = v.fl.src.sel
}

// ---- drawing ----

// filterMenu is ActivityFilterByMenu's popover (300px): "Filter by" and
// Close, the source select with exclude_automations_filter, the list,
// then ActivityFilterActionButtons.
func (v *Activity) filterMenu(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	st := v.curState()
	ed := v.edView()
	m := v.menuEntries(ed, st)
	v.fl.menuRowsN = len(m.entries)
	uk := v.h.Core().ActivityExcludeAutomationsFilter()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16, Right: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
				g := gtx
				g.Constraints.Min.Y = gtx.Dp(32)
				return row(g, layout.Middle,
					layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, nsActivity.T("Filter by")) }),
					layout.Rigid(func(gtx C) D { return v.iconButton(gtx, &v.menuClose, "close", 28, 16, v.p.Sec) }),
				)
			})
		}),
		layout.Rigid(func(gtx C) D {
			if !uk {
				return D{}
			}
			// margin_left_100 margin_right_100 margin_bottom_75: the
			// label (p-activity_ia4_page__filter-menu__text, 8px under)
			// and the BasicSelect (medium, 100%)
			return layout.Inset{Left: 16, Right: 16, Bottom: 12}.Layout(gtx, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
							return v.label(gtx, 13, wReg, v.p.Muted, nsActivity.T("Notifications from"))
						})
					}),
					layout.Rigid(func(gtx C) D {
						v.syncSourceSelect(st.source())
						w := unit.Dp(float32(gtx.Constraints.Max.X) / gtx.Metric.PxPerDp)
						return v.fl.src.layoutSelect(&v.base, gtx, w)
					}),
				)
			})
		}),
		layout.Rigid(func(gtx C) D { return v.menuList(gtx, m, st) }),
		// ActivityFilterActionButtons (activity_views.go)
		layout.Rigid(v.filterActions),
	)
}

// menuList is the list container: m.height tall, clipping its rows.
func (v *Activity) menuList(gtx C, m actMenuList, st actState) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(unit.Dp(m.height))
	defer clip.Rect(image.Rect(0, 0, w, h)).Push(gtx.Ops).Pop()
	y := 0
	at := func(wd layout.Widget) {
		st := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		g := gtx
		g.Constraints.Min = image.Point{}
		y += wd(g).Size.Y
		st.Pop()
	}
	uk := v.h.Core().ActivityExcludeAutomationsFilter()
	src := st.source()
	for k, e := range m.entries {
		if k == m.typeHead {
			at(func(gtx C) D { return v.menuHeading(gtx, nsActivity.T("Filter by entry type")) })
		}
		switch e.opt {
		case optDivider:
			at(func(gtx C) D {
				// margin_bottom_25 margin_top_25 around the Divider (its
				// own 8px padding: c-menu_separator__li)
				return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D { return hline(gtx, v.p.Border) })
			})
			if k == m.extHead {
				at(func(gtx C) D { return v.menuHeading(gtx, nsActivity.T("Additional Filters")) })
			}
			continue
		}
		e := e
		at(func(gtx C) D { return v.menuEntry(gtx, e, st, uk && actSourceDisables(src, e), uk) })
	}
	return D{Size: image.Pt(w, h)}
}

// menuHeading is "Filter by entry type" / "Additional Filters":
// margin_left_100 margin_bottom_50 p-activity_ia4_page__filter-menu__text.
func (v *Activity) menuHeading(gtx C, s string) D {
	return layout.Inset{Left: 16, Bottom: 8}.Layout(gtx, func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Muted, s) })
}

// entryClick is an entry's click target.
func (v *Activity) entryClick(e actEntry) *widget.Clickable {
	switch e.opt {
	case optUnreads:
		return &v.fl.unreadC
	case optType:
		return &v.filterC[e.i]
	case optSub:
		return &v.fl.subC[e.i]
	case optVIP:
		return &v.extraC[0]
	case optCleared:
		return &v.extraC[1]
	case optSpecific:
		return &v.extraC[2]
	case optExcludeAuto:
		return &v.fl.autoC[0]
	case optAutoOnly:
		return &v.fl.autoC[1]
	}
	return nil
}

// menuEntry is one row: c-menu_item__button p-lists_filters__menu-item,
// 28px, 16px in (a sub-filter 44px, no icon), the 13px checkbox and 12px,
// the 20px icon and 8px, the 15px label; Mentions' caret at the end;
// "Specific channel or section" without a checkbox (its icon 4px left)
// and "Active" while the picker is on. A disabled row is content-ter and
// takes no clicks.
func (v *Activity) menuEntry(gtx C, e actEntry, st actState, disabled, uk bool) D {
	c := v.entryClick(e)
	if c == nil {
		return D{}
	}
	w, h := gtx.Constraints.Max.X, gtx.Dp(28)
	body := func(gtx C) D {
		fg, ic := v.p.Text, v.p.Sec
		if disabled {
			fg, ic = v.p.Muted, v.p.Muted
		} else {
			pointerCursor(gtx)
			if c.Hovered() {
				fill(gtx, image.Rect(0, 0, w, h), v.actHover(v.p.Surface))
			}
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		left := unit.Dp(16)
		switch e.opt {
		case optSub:
			left = 44 // p-activity_ia4_page__filter-menu__sub-item
		case optSpecific:
			left = 12 // p-activity_ia4_page__filter-menu__icon: translate(-4px)
		}
		mentions := e.opt == optType && e.i == actRowMentions
		layout.Inset{Left: left, Right: 16}.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					if e.opt == optSpecific {
						return D{}
					}
					return layout.Inset{Right: 12}.Layout(gtx, func(gtx C) D {
						switch {
						case disabled:
							return v.actCheckOff(gtx, 13)
						case mentions && st.mentionsSome():
							return v.actCheckDash(gtx, 13)
						}
						return v.actCheck(gtx, 13, v.filterChecked(st, e))
					})
				}),
				layout.Rigid(func(gtx C) D {
					ico := entryIcon(e)
					if ico == "" {
						return D{}
					}
					return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return v.icon(gtx, ico, 20, ic) })
				}),
				layout.Flexed(1, func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, fg, v.entryLabel(e)) }),
						layout.Rigid(func(gtx C) D {
							if e.opt != optSpecific || !st.showIn {
								return D{}
							}
							// "Active" (margin_left_25 normal)
							return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wReg, fg, nsActivity.T("Active")) })
						}),
					)
				}),
				layout.Rigid(func(gtx C) D {
					if !mentions || uk && st.source() == srcAutomations {
						return D{}
					}
					// "Toggle mention sub-filters": padding 2px, caret-down
					// 20px in content-sec, turned over while open
					ico := "caret-down"
					if v.fl.subOpen {
						ico = "caret-up"
					}
					return v.fl.caretC.Layout(gtx, func(gtx C) D {
						pointerCursor(gtx)
						return layout.UniformInset(2).Layout(gtx, func(gtx C) D { return v.icon(gtx, ico, 20, v.p.Sec) })
					})
				}),
			)
		})
		return D{Size: image.Pt(w, h)}
	}
	if disabled {
		return body(gtx)
	}
	return c.Layout(gtx, body)
}

// actCheckOff is a disabled checkbox (c-input_checkbox:disabled:
// base-ter, no border).
func (b *base) actCheckOff(gtx C, size unit.Dp) D {
	s := gtx.Dp(size)
	rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(4), b.p.Track)
	return D{Size: image.Pt(s, s)}
}

// ---- the feed's types and the saved view's ----

// mentionFeedTypes are the Mentions row's feed types (Ku1y j): the whole
// type, else the sub-filters'; "" with none.
func (s actState) mentionFeedTypes() string {
	if s.filters[actRowMentions] {
		return actMentionsFeed
	}
	var out []string
	for i, on := range s.subs {
		if on {
			out = append(out, actSubFeedTypes[i])
		}
	}
	return strings.Join(out, ",")
}

// actG is sTqq G$: an entry type's row (or sub-filter, as actSubRow+i),
// -1 for the types a view's filters drop (mpdm, thread_reply, the list
// mentions, approvals, system alerts).
const actSubRow = 100

var actG = map[string]int{
	"bot_dm_bundle": 6, "at_user": actSubRow, "keyword": actSubRow + 3, "at_everyone": actSubRow + 1,
	"at_user_group": actSubRow + 2, "at_channel": actSubRow + 1, "unjoined_channel_mention": actSubRow,
	"code_review_requested": actSubRow, "message_reaction": 4, "internal_channel_invite": 5,
	"quietly_added_to_channel": 5, "external_channel_invite": 5, "external_dm_invite": 5, "dm": 0,
	"thread_v2": 2, "saved_reminder": 7, "channel": 3, "prejoin_dm_welcome_party_alert": 0,
}
