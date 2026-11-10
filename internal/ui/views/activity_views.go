package views

// Saved views, as Slack's web client draws and writes them (gantry
// client-boot-render, webapp build a84662d2; docs/ACTIVITY.md "Saved
// views"):
//
//   - The tabs are ActivityInboxSavedViewTabs (GuYW): All, the default
//     views the user didn't hide (DMs only while DMs show in Activity),
//     then the custom views, each "{emoji or icon} {name}"
//     (ActivityViewNameWithIcon QIkC, the icon from BGxe B). A custom
//     tab's context menu is "Rename view" / "Delete view", a default
//     one's (not All) "Hide view" (ActivityHideViewConfirmationModal).
//   - The "+" opens ActivitySuggestedTabsMenu (9av4): Hidden views,
//     Default suggestions (vzS1 cv: Unreads, Mentions, Threads, Reactions,
//     VIP with VIPs, minus those a view already has), Suggestions from
//     your sidebar (Starred and the custom sections), "Create a custom
//     view"; with nothing to suggest it opens the modal straight away. A
//     suggestion is a view: createActivityViewsByApi with its filter.
//   - ActivityFilterSaveViewModal (fNIs): "Create a new view" (with
//     ActivityAddTabContent: layout, "Only include unreads", the filter
//     rows, the sort), "Save this view" (the filter menu's "Create Custom
//     View": the current filters) and "Rename this view".
//   - Each view keeps its own filters while the page is open (P0K2
//     views[]); a custom view starts from its saved ones (RY04).
//   - The VIP filter is filterVipSelected → activity.feed priority_only.
//   - Sort: "Newest" / "VIP unreads on top" (1p6b G3; the second only
//     with VIPs). The header's sort button (KJvs HEADER) with
//     activity_global_settings on, and ActivitySettingsMenu's Sort and
//     Density sections (vzfm) behind activity_in_home_prototype (6rqO j);
//     both write activity.prefs.update (UykM). VIP unreads on top puts the
//     unread VIP items first under "VIP unreads", then "Everything else"
//     (U5s4 ActivityList rows, agdG keys), and asks the feed for
//     priority_unreads_first_v1 (TfjX w).

import (
	"context"
	"image"
	"image/color"
	"slices"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// actSlotN is the default views' slots (actTabIDs: all, dms, mentions,
// threads); custom views take the slots after them.
const actSlotN = len(actTabIDs)

// actMaxViews is GuYW M / lCwf ActivityFilterSaveButton's limit.
const actMaxViews = 15

// actState is a view's filters while the page is open: the filter menu's
// rows, Unreads, Cleared items, VIP and the sections and channels a saved
// view names.
type actState struct {
	filters            [len(actFilters)]bool
	unread, cleared    bool
	vip                bool
	sections, channels []string
}

func (s actState) any() bool {
	for _, on := range s.filters {
		if on {
			return true
		}
	}
	return s.unread || s.cleared || s.vip || len(s.sections) > 0 || len(s.channels) > 0
}

func (s actState) equal(o actState) bool {
	return s.filters == o.filters && s.unread == o.unread && s.cleared == o.cleared && s.vip == o.vip &&
		slices.Equal(s.sections, o.sections) && slices.Equal(s.channels, o.channels)
}

func (v *Activity) curState() actState {
	return actState{filters: v.filters, unread: v.unread.Value, cleared: v.showCleared, vip: v.vip, sections: v.sections, channels: v.channels}
}

func (v *Activity) setState(s actState) {
	v.filters, v.unread.Value, v.showCleared, v.vip, v.sections, v.channels = s.filters, s.unread, s.cleared, s.vip, s.sections, s.channels
}

// actEntryTypes is RmAw r: the activity.feed types a filter row saves as
// a view's entry_types.
var actEntryTypes = [len(actFilters)][]string{
	{"dm", "mpdm"},
	{"at_user", "at_user_group", "at_channel", "at_everyone", "keyword", "list_record_assigned", "list_user_mentioned", "list_todo_notification"},
	{"thread_v2", "thread_reply"},
	{"channel"},
	{"message_reaction"},
	{"internal_channel_invite", "quietly_added_to_channel", "external_channel_invite", "shared_workspace_invite", "external_dm_invite", "team_joiner_invite"},
	{"bot_dm_bundle"},
	{"saved_reminder"},
}

// actStateOf is a saved view's filters as the page starts them (RY04
// formatFilterViewState: getActivityFilterFeedEntryType maps entry_types
// back to the rows).
func actStateOf(f core.ActivityViewFilters) actState {
	in := map[string]bool{}
	for _, t := range f.EntryTypes {
		in[t] = true
	}
	var s actState
	for i, ts := range actEntryTypes {
		for _, t := range ts {
			if in[t] {
				s.filters[i] = true
				break
			}
		}
	}
	s.unread, s.cleared, s.vip = f.UnreadOnly, f.ArchiveOnly, f.PriorityOnly
	s.sections, s.channels = f.SectionIDs, f.ChannelIDs
	return s
}

// viewFilters is birT's filters for a state.
func (s actState) viewFilters() core.ActivityViewFilters {
	f := core.ActivityViewFilters{UnreadOnly: s.unread, ArchiveOnly: s.cleared, PriorityOnly: s.vip,
		SectionIDs: s.sections, ChannelIDs: s.channels}
	for i, on := range s.filters {
		if on {
			f.EntryTypes = append(f.EntryTypes, actEntryTypes[i]...)
		}
	}
	return f
}

// ---- slots and the tab strip ----

// slot is the view shown: 0-3 the defaults (actTabIDs), actSlotN+k the
// k-th custom view.
func (v *Activity) slot() int {
	if v.tabs.Sel >= 0 && v.tabs.Sel < len(v.slots) {
		return v.slots[v.tabs.Sel]
	}
	return v.tabs.Sel
}

// slotKey identifies a slot's view across reorders: the default view's
// type, a custom view's id.
func (v *Activity) slotKey(slot int) string {
	if slot >= 0 && slot < actSlotN {
		return actTabIDs[slot]
	}
	if k := slot - actSlotN; k >= 0 && k < len(v.customs) {
		return v.customs[k].ID
	}
	return ""
}

// slotView is a slot's saved view (zero until activity.views answers).
func (v *Activity) slotView(slot int) core.ActivityView {
	if slot >= 0 && slot < actSlotN {
		return v.views[slot]
	}
	if k := slot - actSlotN; k >= 0 && k < len(v.customs) {
		return v.customs[k]
	}
	return core.ActivityView{}
}

// customView is the custom view shown, if one is.
func (v *Activity) customView() (core.ActivityView, bool) {
	if k := v.slot() - actSlotN; k >= 0 && k < len(v.customs) {
		return v.customs[k], true
	}
	return core.ActivityView{}, false
}

// dmsInActivity is 5Ns/ Vn: DMs show in Activity (activity_dms_opt_out
// off).
func (v *Activity) dmsInActivity() bool { return !v.h.Core().PrefBool("activity_dms_opt_out", false) }

// wantSlots is GuYW R: All, the default views not hidden (DMs while DMs
// show in Activity), then the custom views.
func (v *Activity) wantSlots() []int {
	out := []int{0}
	for i := 1; i < actSlotN; i++ {
		if v.hidden[actTabIDs[i]] || i == 1 && !v.dmsInActivity() {
			continue
		}
		out = append(out, i)
	}
	for k := range v.customs {
		out = append(out, actSlotN+k)
	}
	return out
}

// syncTabs rebuilds the tab strip when its views changed, keeping the one
// shown (All when it went away).
func (v *Activity) syncTabs() {
	want := v.wantSlots()
	labels := make([]string, len(want))
	for i, s := range want {
		labels[i] = v.slotLabel(s)
	}
	if slices.Equal(want, v.slots) && slices.Equal(labels, v.tabs.Labels) {
		return
	}
	prev := v.slot()
	cur := v.slotKey(prev)
	v.slots = want
	t := newTabs(labels...)
	found := false
	for i, s := range want {
		if v.slotKey(s) == cur {
			t.Sel, found = i, true
		}
	}
	v.tabs = t
	v.tabCtx = make([]actTabCtx, len(want))
	if !found {
		// the view shown was deleted or hidden: P0K2 falls back to All
		v.switchView(prev, false)
		v.load(actFresh)
	}
}

// slotLabel is GuYW F's text: the default views' names (x8u8 dr), a
// custom view's own.
func (v *Activity) slotLabel(slot int) string {
	if slot < actSlotN {
		return actTabLabels[slot]
	}
	return v.slotView(slot).Name
}

// switchView is handleViewChange (utzr I) after the tab moved from prev:
// the bulk selection ends, the detail pane closes, prev keeps its
// filters and the new view brings back its own (a custom view's saved
// ones the first time). save false drops prev's (a deleted view).
func (v *Activity) switchView(prev int, save ...bool) {
	if len(save) == 0 || save[0] {
		if k := v.slotKey(prev); k != "" {
			v.stateOf[k] = v.curState()
		}
	}
	s := v.slot()
	if st, ok := v.stateOf[v.slotKey(s)]; ok {
		v.setState(st)
	} else if s >= actSlotN {
		v.setState(actStateOf(v.slotView(s).Filters))
	} else {
		v.setState(actState{})
	}
	v.endSelect()
	v.selOK = false
	if !v.globalSettings() {
		// each saved view keeps its own layout
		if d := v.slotDense(s); d != v.dense {
			v.dense, v.dirty = d, true
			v.rows = map[string]*actRow{}
		}
	}
}

// slotDense is a view's own layout (activity_global_settings off).
func (v *Activity) slotDense(slot int) bool {
	if slot < actSlotN {
		return v.viewDense[slot]
	}
	return v.slotView(slot).Density == "dense"
}

// actTabCtx is a tab's context-menu press target (not zero-sized: each
// needs its own address).
type actTabCtx struct{ _ byte }

// viewTabs is ActivityInboxSavedViewTabs drawn like the other tab strips
// (tabStripIcons): 13px bold labels, 9px 8px padding, the 2px underline;
// a custom tab leads with its emoji or icon (16px, 4px gap), the default
// ones with theirs; unread dots after the default tabs' labels.
func (v *Activity) viewTabs(gtx C) D {
	t := v.tabs
	ch := make([]layout.FlexChild, 0, len(t.Labels))
	for i := range t.Labels {
		i := i
		slot := i
		if i < len(v.slots) {
			slot = v.slots[i]
		}
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return t.clicks[i].Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				sel := i == t.Sel
				fg := v.p.Sec
				if sel {
					fg = v.p.Text
				}
				m := op.Record(gtx.Ops)
				d := layout.Inset{Top: 9, Bottom: 8, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D { return v.tabLead(gtx, slot, fg) }),
						layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, fg, t.Labels[i]) }),
						layout.Rigid(func(gtx C) D {
							if slot >= len(v.dots) || v.dots[slot] <= 0 {
								return D{}
							}
							return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return v.dot(gtx, 8, v.p.Accent) })
						}),
					)
				})
				call := m.Stop()
				if t.clicks[i].Hovered() {
					r := gtx.Dp(8)
					rr := clip.RRect{Rect: image.Rectangle{Max: d.Size}, NE: r, NW: r}
					paint.FillShape(gtx.Ops, v.p.TabHover, rr.Op(gtx.Ops))
				}
				call.Add(gtx.Ops)
				if sel {
					h := gtx.Dp(2)
					fill(gtx, image.Rect(0, d.Size.Y-h, d.Size.X, d.Size.Y), v.p.TabLine)
				}
				if i < len(v.tabCtx) {
					// the tab's context menu (GuYW contextMenuTemplate)
					cl := clip.Rect(image.Rectangle{Max: d.Size}).Push(gtx.Ops)
					pass := pointer.PassOp{}.Push(gtx.Ops)
					event.Op(gtx.Ops, &v.tabCtx[i])
					pass.Pop()
					cl.Pop()
				}
				return d
			})
		}))
	}
	return row(gtx, layout.End, ch...)
}

// tabLead is the icon (or emoji) before a tab's name: the default
// views' icons (actTabIcons), a custom view's emoji or its BGxe icon.
func (v *Activity) tabLead(gtx C, slot int, fg color.NRGBA) D {
	icon, emoji := "", ""
	if slot < actSlotN {
		icon = actTabIcons[slot]
	} else {
		vw := v.slotView(slot)
		emoji = vw.Emoji
		if emoji == "" {
			icon = actViewIcon(actStateOf(vw.Filters))
		}
	}
	switch {
	case emoji != "":
		if u, _, ok := v.h.Core().Emoji(strings.Trim(emoji, ":")); ok && u != "" {
			return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.emojiImg(gtx, u, 16) })
		}
	case icon != "":
		return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, icon, 16, fg) })
	}
	return D{}
}

// actViewIcon is BGxe B: a view's icon from its filters: Unreads, VIP or
// Cleared items alone, the Mentions types, or one type row; "" otherwise.
func actViewIcon(s actState) string {
	n := 0
	only := -1
	for i, on := range s.filters {
		if on {
			n, only = n+1, i
		}
	}
	flag := ""
	switch {
	case s.unread:
		flag = "mark-as-unread"
	case s.vip:
		flag = "vip"
	case s.cleared:
		flag = "eraser"
	}
	switch {
	case n > 0 && flag == "":
		if n == 1 {
			return actRowIcons[only]
		}
	case n == 0 && flag != "":
		return flag
	}
	return ""
}

// actRowIcons are BGxe's icons for the type rows (DMs has none there).
var actRowIcons = [len(actFilters)]string{"", "mentions", "threads", "", "emoji", "paper-plane", "apps", "reminder"}

// tabMenus handles the tabs' context menus: custom views' Rename view /
// Delete view, the default views' (not All) Hide view.
func (v *Activity) tabMenus(gtx C) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	for i := range v.tabCtx {
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: &v.tabCtx[i], Kinds: pointer.Press})
			if !ok {
				break
			}
			e, ok := ev.(pointer.Event)
			if !ok || !ctxclick.Press(e) || i >= len(v.slots) {
				continue
			}
			slot := v.slots[i]
			var items []popups.MenuItem
			switch {
			case slot >= actSlotN:
				vw := v.slotView(slot)
				items = []popups.MenuItem{
					{Key: "rename", Label: "Rename view", Do: func() { v.openViewModal(actRenameView, vw) }},
					{Key: "delete", Label: "Delete view", Danger: true, Do: func() { v.confirmDeleteView(vw) }},
				}
			case slot > 0:
				typ := actTabIDs[slot]
				items = []popups.MenuItem{{Key: "hide", Label: "Hide view", Danger: true, Do: func() { v.confirmHideView(typ) }}}
			}
			if len(items) > 0 {
				at := v.pointerAt()
				ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: image.Rectangle{Min: at, Max: at}, Width: 0}))
			}
		}
	}
}

// ---- loading the views ----

// loadViews is fetchActivityViews (rsjQ): the saved views, the hidden
// default types, the global prefs; each default tab's view (by
// view_type) and its density, and the custom views as tabs.
func (v *Activity) loadViews() {
	c := v.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		st, err := c.ActivityViewsState(ctx)
		if err != nil {
			return
		}
		v.notes.mu.Lock()
		v.notes.fns = append(v.notes.fns, func() { v.applyViews(st, true) })
		v.notes.mu.Unlock()
		v.h.Invalidate()
	}()
}

// applyViews takes activity.views' answer (or activity_views_updated's
// views when prefs is false: the event carries no prefs).
func (v *Activity) applyViews(st core.ActivityViewsState, prefs bool) {
	v.viewsLoaded = true
	if prefs {
		v.globalSort = st.Sort
	}
	v.customs = v.customs[:0:0]
	for _, vw := range st.Views {
		if vw.Type == "custom" {
			v.customs = append(v.customs, vw)
			continue
		}
		for i, id := range actTabIDs {
			if vw.Type == id {
				v.views[i] = vw
				v.viewDense[i] = vw.Density == "dense" // getDensityFromApiDensity
			}
		}
	}
	// P0K2 ew: All first, the others by position
	slices.SortStableFunc(v.customs, func(a, b core.ActivityView) int { return strings.Compare(a.Position, b.Position) })
	v.hidden = map[string]bool{}
	for _, t := range st.Hidden {
		v.hidden[t] = true
	}
	v.syncTabs()
	d, ok := v.slotDense(v.slot()), true
	if v.globalSettings() {
		// formatFilterViewState lays prefs.density over every view
		d, ok = st.Density == "dense", prefs && st.Density != ""
	}
	if ok && !v.densitySet && d != v.dense {
		v.dense, v.dirty = d, true
		v.rows = map[string]*actRow{}
	}
	v.dirty = true
}

// viewNames is O.Gf: the saved views' names (the duplicate check).
func (v *Activity) viewNames() []string {
	out := append([]string(nil), actTabLabels...)
	for _, c := range v.customs {
		out = append(out, c.Name)
	}
	return out
}

func (v *Activity) viewCount() int {
	n := 1
	for i := 1; i < actSlotN; i++ {
		if !v.hidden[actTabIDs[i]] {
			n++
		}
	}
	return n + len(v.customs)
}

// ---- writes ----

// createView is createActivityViewsByApi (birT): activity.views.create,
// then the new tab is shown (utzr I) and the view the user was on loses
// its unsaved filters (P0K2 Lr "clearAllTab"). done gets Slack's error
// code ("" on success) on the UI goroutine.
func (v *Activity) createView(vw core.ActivityView, done func(code string)) {
	c := v.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		made, err := c.CreateActivityView(ctx, vw)
		v.notes.mu.Lock()
		v.notes.fns = append(v.notes.fns, func() {
			if err != nil {
				if done != nil {
					done(actViewErrCode(err))
				} else {
					v.h.Toast(actViewErrText(actViewErrCode(err)))
				}
				return
			}
			if made.ID == "" {
				made = vw
			}
			made.Type = "custom"
			prev := v.slot()
			if k := v.slotKey(prev); k != "" {
				delete(v.stateOf, k) // clearAllTab
			}
			v.customs = append(v.customs, made)
			v.syncTabs()
			for i, s := range v.slots {
				if v.slotKey(s) == made.ID {
					v.tabs.Sel = i
				}
			}
			v.switchView(prev, false)
			v.load(actFresh)
			if done != nil {
				done("")
			}
		})
		v.notes.mu.Unlock()
		v.h.Invalidate()
	}()
}

// actViewErrCode is the error code a views write failed with.
func actViewErrCode(err error) string {
	s := err.Error()
	for _, c := range []string{"over_view_maximum", "cannot_use_default_view_name", "duplicate_name", "cannot_change_default_view_name",
		"name_too_long", "cannot_delete_default_view", "view_not_found"} {
		if strings.Contains(s, c) {
			return c
		}
	}
	return "unknown"
}

// actViewErrText is RY04 U_.
func actViewErrText(code string) string {
	switch code {
	case "":
		return ""
	case "over_view_maximum":
		return "You’ve reached the maximum number of saved views."
	case "cannot_use_default_view_name", "duplicate_name":
		return "There’s already a view with this name."
	case "cannot_change_default_view_name":
		return "This is a default view, so the name can’t be changed."
	case "name_too_long":
		return "You can’t enter more than 80 characters."
	case "cannot_delete_default_view":
		return "Default views can’t be deleted."
	case "view_not_found":
		return "This view can’t be found."
	}
	return "Something went wrong. Please try again."
}

// replaceCustom swaps a custom view for its updated copy.
func (v *Activity) replaceCustom(vw core.ActivityView) {
	for i := range v.customs {
		if v.customs[i].ID == vw.ID {
			if vw.Type == "" {
				vw.Type = "custom"
			}
			v.customs[i] = vw
		}
	}
	v.syncTabs()
}

// confirmDeleteView is ActivityFilterSaveViewConfirmationModal:
// "Are you sure you want to delete “{view}”?", "This view will be removed
// from Activity.", Cancel / Delete (danger) → deleteActivityViewsByApi;
// then '"{view}" has been deleted successfully.' (or "There was an error
// deleting the view.").
func (v *Activity) confirmDeleteView(vw core.ActivityView) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	c := v.h.Core()
	popups.Confirm(ph, "Are you sure you want to delete “"+vw.Name+"”?", "This view will be removed from Activity.", "Delete", true, nil, func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err := c.DeleteActivityView(ctx, vw.ID)
			v.notes.mu.Lock()
			v.notes.fns = append(v.notes.fns, func() {
				if err != nil {
					v.h.Toast("There was an error deleting the view.")
					return
				}
				v.customs = slices.DeleteFunc(v.customs, func(x core.ActivityView) bool { return x.ID == vw.ID })
				delete(v.stateOf, vw.ID)
				v.syncTabs()
				v.h.Toast(`"` + vw.Name + `" has been deleted successfully.`)
			})
			v.notes.mu.Unlock()
			v.h.Invalidate()
		}()
	})
}

// confirmHideView is ActivityHideViewConfirmationModal: "Are you sure you
// want to hide “{view}”?", "This tab will be hidden from Activity.",
// Cancel / Hide (danger) → hideDefaultViewByApi (the tab goes at once and
// comes back if the write fails).
func (v *Activity) confirmHideView(typ string) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	name := ""
	for i, id := range actTabIDs {
		if id == typ {
			name = actTabLabels[i]
		}
	}
	popups.Confirm(ph, "Are you sure you want to hide “"+name+"”?", "This tab will be hidden from Activity.", "Hide", true, nil, func() {
		v.setHidden(typ, true)
	})
}

// setHidden is hide / unhideDefaultViewByApi: the tab changes at once,
// then activity.views.hide / unhide; a failed write puts it back.
func (v *Activity) setHidden(typ string, hide bool) {
	if v.hidden == nil {
		v.hidden = map[string]bool{}
	}
	v.hidden[typ] = hide
	v.syncTabs()
	v.write("", func(ctx context.Context, c *core.App) error {
		if hide {
			return c.HideActivityView(ctx, typ)
		}
		return c.UnhideActivityView(ctx, typ)
	}, func() {
		v.hidden[typ] = !hide
		v.syncTabs()
	})
}

// ---- the "+" menu (ActivitySuggestedTabsMenu) ----

// actPlusItem is one row of the "+" menu: a header, a separator or a
// clickable row with its 20px icon (or emoji).
type actPlusItem struct {
	header       string
	sep          bool
	icon, emoji  string
	label        string
	do           func()
	createCustom bool
}

// actSuggestion is a vzS1 suggestion: a name and the view's filters.
type actSuggestion struct {
	name, icon, emoji string
	st                actState
}

// suggestions is vzS1 cv: Unreads, Mentions, Threads, Reactions (and VIP
// with VIPs), then up to ten of the sidebar's sections (Starred and the
// custom ones), minus any a saved view already has by name or filters.
func (v *Activity) suggestions() (activity, sections []actSuggestion) {
	c := v.h.Core()
	type sug = actSuggestion
	var mentions, threads, reactions actState
	mentions.filters[1], threads.filters[2], reactions.filters[actFilterReactions] = true, true, true
	acts := []sug{{name: "Unreads", icon: "mark-as-unread", st: actState{unread: true}},
		{name: "Mentions", icon: "mentions", st: mentions}, {name: "Threads", icon: "threads", st: threads},
		{name: "Reactions", icon: "emoji", st: reactions}}
	if len(c.VIPs()) > 0 {
		acts = append(acts, sug{name: "VIP", icon: "vip", st: actState{vip: true}})
	}
	var secs []sug
	for _, s := range c.Snapshot().Sections {
		if s.Type != "standard" && s.Type != "stars" || len(secs) == 10 {
			continue
		}
		g := sug{name: s.Name, icon: "section", st: actState{sections: []string{s.ID}}}
		if s.Type == "stars" {
			g.name, g.icon = "Starred", "star"
		}
		if s.Emoji != "" {
			g.emoji = ":" + strings.Trim(s.Emoji, ":") + ":"
		}
		secs = append(secs, g)
	}
	have := func(g sug) bool {
		for slot := 0; slot < actSlotN+len(v.customs); slot++ {
			vw := v.slotView(slot)
			name := v.slotLabel(slot)
			if name == g.name {
				return true
			}
			if slot >= actSlotN && actStateOf(vw.Filters).equal(g.st) {
				return true
			}
		}
		return false
	}
	for _, g := range acts {
		if !have(g) {
			activity = append(activity, g)
		}
	}
	for _, g := range secs {
		if !have(g) {
			sections = append(sections, g)
		}
	}
	return activity, sections
}

// plusItems is 9av4's template: Hidden views, Default suggestions (or
// "Suggested views" without sidebar ones), Suggestions from your sidebar,
// then "Create a custom view".
func (v *Activity) plusItems() []actPlusItem {
	var out []actPlusItem
	var hidden []string
	for _, t := range core.ActivityDefaultViewTypes {
		if v.hidden[t] {
			hidden = append(hidden, t)
		}
	}
	if len(hidden) > 0 {
		out = append(out, actPlusItem{header: "Hidden views"})
		for _, t := range hidden {
			t := t
			i := slices.Index(actTabIDs[:], t)
			out = append(out, actPlusItem{icon: actHiddenIcons[i], label: actTabLabels[i], do: func() { v.setHidden(t, false) }})
		}
		out = append(out, actPlusItem{sep: true})
	}
	acts, secs := v.suggestions()
	if len(acts) > 0 {
		h := "Suggested views"
		if len(secs) > 0 {
			h = "Default suggestions"
		}
		out = append(out, actPlusItem{header: h})
		for _, g := range acts {
			g := g
			out = append(out, actPlusItem{icon: g.icon, label: g.name, do: func() { v.createSuggested(g) }})
		}
	}
	if len(acts) > 0 && len(secs) > 0 {
		out = append(out, actPlusItem{sep: true})
	}
	if len(secs) > 0 {
		out = append(out, actPlusItem{header: "Suggestions from your sidebar"})
		for _, g := range secs {
			g := g
			out = append(out, actPlusItem{icon: g.icon, emoji: g.emoji, label: g.name, do: func() { v.createSuggested(g) }})
		}
	}
	out = append(out, actPlusItem{sep: true},
		actPlusItem{icon: "plus-filled", label: "Create a custom view", createCustom: true, do: func() { v.openViewModal(actCreateTab, core.ActivityView{}) }})
	return out
}

// actHiddenIcons are x8u8 Gv's default view icons.
var actHiddenIcons = [actSlotN]string{"notifications", "direct-messages", "mentions", "threads"}

// createSuggested is GuYW ek: a suggestion becomes a view named after it
// (createActivityViewsByApi with the suggestion's filter; vzS1's views
// start from sTqq wn: newest, compact).
func (v *Activity) createSuggested(g actSuggestion) {
	v.createView(core.ActivityView{Name: g.name, Emoji: g.emoji, Sort: "newest", Density: "compact", Filters: g.st.viewFilters()}, nil)
}

// plusClick opens the "+": the menu when there's anything to suggest or
// unhide (GuYW T: em.d.MENU), else the modal straight away; nothing past
// 15 views.
func (v *Activity) plusClick() {
	if v.viewCount() >= actMaxViews {
		return
	}
	for _, it := range v.plusItems() {
		if it.do != nil && !it.createCustom {
			v.menu = actToggleMenu(v.menu, actMenuPlus)
			return
		}
	}
	v.menu = actMenuNone
	v.openViewModal(actCreateTab, core.ActivityView{})
}

// plusClicks runs the "+" menu's clicked row.
func (v *Activity) plusClicks(gtx C) {
	items := v.plusItems()
	if len(v.plusC) < len(items) {
		v.plusC = append(v.plusC, make([]widget.Clickable, len(items)-len(v.plusC))...)
	}
	for i, it := range items {
		if it.do != nil && v.plusC[i].Clicked(gtx) {
			v.menu = actMenuNone
			it.do()
		}
	}
}

func (v *Activity) plusMenu(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	items := v.plusItems()
	if len(v.plusC) < len(items) {
		v.plusC = append(v.plusC, make([]widget.Clickable, len(items)-len(v.plusC))...)
	}
	return actStack(gtx, len(items), func(gtx C, i int) D {
		it := items[i]
		switch {
		case it.sep:
			return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D { return hline(gtx, v.p.Border) })
		case it.header != "":
			return layout.Inset{Top: 4, Bottom: 4, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				return v.label(gtx, 13, wReg, v.p.Muted, it.header)
			})
		}
		return v.plusRow(gtx, &v.plusC[i], it)
	})
}

// plusRow: a 28px row, the 20px icon (or the section's emoji) and 8px,
// the 15px label.
func (v *Activity) plusRow(gtx C, c *widget.Clickable, it actPlusItem) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h := gtx.Constraints.Max.X, gtx.Dp(28)
		if c.Hovered() {
			fill(gtx, image.Rect(0, 0, w, h), v.actHover(v.p.Surface))
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					if it.emoji != "" {
						if u, _, ok := v.h.Core().Emoji(strings.Trim(it.emoji, ":")); ok && u != "" {
							return v.emojiImg(gtx, u, 20)
						}
					}
					return v.icon(gtx, it.icon, 20, v.p.Sec)
				}),
				hgap(8),
				layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wReg, v.p.Text, it.label) }),
			)
		})
		return D{Size: image.Pt(w, h)}
	})
}

// ---- sort ----

// actSortVIP is the API's "VIP unreads on top" (sTqq XS vip).
const actSortVIP = "vip_unreads_first"

// sortVIP reports whether the view shown sorts VIP unreads on top: the
// global prefs' sort with activity_global_settings on (laid over every
// view), else the view's own; only while the user has VIPs (1p6b G3
// offers the sort only then, q3 falls back to Newest).
func (v *Activity) sortVIP() bool {
	if len(v.h.Core().VIPs()) == 0 {
		return false
	}
	if v.globalSettings() {
		return v.globalSort == actSortVIP
	}
	return v.slotView(v.slot()).Sort == actSortVIP
}

// feedMode is TfjX w: priority_unreads_first_v1 for VIP unreads on top.
func (v *Activity) feedMode() string {
	if v.sortVIP() {
		return "priority_unreads_first_v1"
	}
	return ""
}

// setSort is NYgk onSortChange: updateAllActivityViewsByApi {sortOrder}
// (activity.prefs.update {sort}) and a fresh feed in the new mode.
func (v *Activity) setSort(vip bool) {
	s := "newest"
	if vip {
		s = actSortVIP
	}
	if s == v.globalSort || v.globalSort == "" && !vip {
		v.menu = actMenuNone
		return
	}
	was := v.globalSort
	v.globalSort, v.menu = s, actMenuNone
	// UykM: every saved view takes the sort too
	sorts := make([]string, actSlotN+len(v.customs))
	for i := range v.views {
		sorts[i], v.views[i].Sort = v.views[i].Sort, s
	}
	for k := range v.customs {
		sorts[actSlotN+k], v.customs[k].Sort = v.customs[k].Sort, s
	}
	v.write("", func(ctx context.Context, c *core.App) error { return c.SetActivityPrefs(ctx, s, "") }, func() {
		v.globalSort = was
		for i := range v.views {
			v.views[i].Sort = sorts[i]
		}
		for k := range v.customs {
			if actSlotN+k < len(sorts) {
				v.customs[k].Sort = sorts[actSlotN+k]
			}
		}
		v.load(actFresh)
	})
	v.load(actFresh)
}

// setDensityPref is the settings menu's Density radio (UykM: every saved
// view takes it, activity.prefs.update {density}).
func (v *Activity) setDensityPref(dense bool) {
	v.menu = actMenuNone
	if v.dense == dense {
		return
	}
	was := v.dense
	v.dense, v.densitySet, v.dirty = dense, true, true
	v.rows = map[string]*actRow{}
	for i := range v.viewDense {
		v.viewDense[i] = dense
	}
	d := "compact"
	if dense {
		d = "dense"
	}
	v.write("Couldn’t save the layout: ", func(ctx context.Context, c *core.App) error { return c.SetActivityPrefs(ctx, "", d) },
		func() { v.dense, v.dirty = was, true })
}

// inHomePrototype is 6rqO j: activity_in_home_prototype, which shows the
// settings menu's Sort and Density sections.
func (v *Activity) inHomePrototype() bool {
	return v.h.Core().Experiment("activity_in_home_prototype") == "on"
}

// headerSort reports whether the header carries ActivityFilterSort
// (HEADER entry point: activity_global_settings on and the prototype off,
// shown only with the VIP sort to offer).
func (v *Activity) headerSort() bool {
	return v.globalSettings() && !v.inHomePrototype() && len(v.h.Core().VIPs()) > 0
}

// sortMenu is KJvs's HEADER menu: radio rows "Newest" and "VIP unreads on
// top" (popups' c-menu radio items).
func (v *Activity) sortMenu(at image.Point) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	vip := v.sortVIP()
	items := []popups.MenuItem{
		{Key: "activity-sort-newest", Label: "Newest", Radio: true, Checked: !vip, Do: func() { v.setSort(false) }},
		{Key: "activity-sort-vip", Label: "VIP unreads on top", Radio: true, Checked: vip, Do: func() { v.setSort(true) }},
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: image.Rectangle{Min: at, Max: at}, AlignRight: true, Width: 240}))
}

// actVIPOrder is the ActivityList selector Q with VIP unreads on top:
// the unread VIP items first (the opened one stays in its group while it
// was unread when opened), then everything else, each in feed order;
// split is where everything else starts (-1 when nothing goes first).
func (v *Activity) actVIPOrder(page *core.ActivityPage) (order []int, split int) {
	var first, rest []int
	for i := range page.Items {
		it := &page.Items[i]
		key := actKey(it)
		unread := v.isUnread(it, key)
		if v.selOK && v.selKey == key {
			unread = v.selUnread
		}
		if it.Priority && unread {
			first = append(first, i)
		} else {
			rest = append(rest, i)
		}
	}
	if len(first) == 0 {
		return append(first, rest...), -1
	}
	return append(first, rest...), len(first)
}

// sectionHeader is ActivitySectionHeader (oDbF) in the bordered list with
// layout revisions: 42px (--activity-section-header-height), base-pry
// under a 1px otl-ter border, the 20px tile (6px radius) with the 16px
// icon, 12px, the 13px/18px bold label in content-sec, 20px in: "VIP
// unreads" (the important tile: campfire-0 under campfire-30) and
// "Everything else" (base-ter under content-sec).
func (v *Activity) sectionHeader(gtx C, vip bool) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(42)
	fill(gtx, image.Rect(0, 0, w, h), v.p.Bg)
	fill(gtx, image.Rect(0, h-max(1, gtx.Dp(1)), w, h), v.p.Border)
	label, icon := "Everything else", "notifications-filled"
	bg, fg := v.p.Track, v.p.Sec
	if vip {
		label, icon = "VIP unreads", "mark-as-important-filled"
		bg, fg = rgb(0xfff2e0), rgb(0xed9027) // plt-campfire-0 / plt-campfire-30
		if v.th.Dark {
			bg, fg = rgb(0x321600), rgb(0x954700)
		}
	}
	t := gtx.Dp(20)
	y := (h - t) / 2
	x := gtx.Dp(20)
	func() {
		defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
		rrect(gtx, image.Rect(0, 0, t, t), gtx.Dp(6), bg)
		defer op.Offset(image.Pt((t-gtx.Dp(16))/2, (t-gtx.Dp(16))/2)).Push(gtx.Ops).Pop()
		v.icon(gtx, icon, 16, fg)
	}()
	func() {
		defer op.Offset(image.Pt(x+t+gtx.Dp(12), (h-gtx.Dp(18))/2)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(max(0, w-x-t-gtx.Dp(32)), gtx.Dp(18))}
		v.label(g, 13, wBold, v.p.Sec, label)
	}()
	return D{Size: image.Pt(w, h)}
}

// ---- the settings menu's Sort and Density sections (vzfm) ----

// settingsSortRows is vzfm's Sort section ("Sort by", Newest / VIP unreads
// on top), shown with activity_in_home_prototype and the VIP sort to
// offer; settingsDensityRows its Density section ("Multi-line rows" =
// compact, "Single-line rows" = dense) with the prototype.
func (v *Activity) settingsSortOn() bool {
	return v.inHomePrototype() && len(v.h.Core().VIPs()) > 0
}

func (v *Activity) settingsSectionClicks(gtx C) {
	if v.setSortC[0].Clicked(gtx) {
		v.setSort(false)
	}
	if v.setSortC[1].Clicked(gtx) {
		v.setSort(true)
	}
	if v.setDenseC[0].Clicked(gtx) {
		v.setDensityPref(false)
	}
	if v.setDenseC[1].Clicked(gtx) {
		v.setDensityPref(true)
	}
}

// settingsSections are the rows the menu shows above "What to show in
// Activity": [Sort by …] [Density …], each with its separator after
// (vzfm em; stripTerminalSeparators drops the leading ones).
func (v *Activity) settingsSections() []layout.FlexChild {
	var out []layout.FlexChild
	header := func(s string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 4, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				return v.label(gtx, 13, wBold, v.p.Muted, s)
			})
		})
	}
	sep := layout.Rigid(func(gtx C) D {
		return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D { return hline(gtx, v.p.Border) })
	})
	if v.settingsSortOn() {
		vip := v.sortVIP()
		out = append(out, header("Sort by"),
			layout.Rigid(func(gtx C) D { return v.radioRow(gtx, &v.setSortC[0], "Newest", !vip) }),
			layout.Rigid(func(gtx C) D { return v.radioRow(gtx, &v.setSortC[1], "VIP unreads on top", vip) }),
			sep)
	}
	if v.inHomePrototype() {
		out = append(out, header("Density"),
			layout.Rigid(func(gtx C) D { return v.radioRow(gtx, &v.setDenseC[0], "Multi-line rows", !v.dense) }),
			layout.Rigid(func(gtx C) D { return v.radioRow(gtx, &v.setDenseC[1], "Single-line rows", v.dense) }),
			sep)
	}
	return out
}

// radioRow is a c-menu radio item (popups' radioRow geometry): 28px, the
// 16px check at x 8 when checked, the 15px label at x 24, link blue while
// checked.
func (v *Activity) radioRow(gtx C, c *widget.Clickable, label string, on bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h := gtx.Constraints.Max.X, gtx.Dp(28)
		if c.Hovered() {
			fill(gtx, image.Rect(0, 0, w, h), v.actHover(v.p.Surface))
		}
		fg := v.p.Text
		if on {
			fg = v.p.Link
			func() {
				defer op.Offset(image.Pt(gtx.Dp(8), (h-gtx.Dp(16))/2)).Push(gtx.Ops).Pop()
				v.icon(gtx, "check", 16, fg)
			}()
		}
		func() {
			defer op.Offset(image.Pt(gtx.Dp(24), 0)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(max(0, w-gtx.Dp(48)), h))
			layout.W.Layout(g, func(gtx C) D { return v.label(gtx, 15, wReg, fg, label) })
		}()
		return D{Size: image.Pt(w, h)}
	})
}

// ---- the filter menu's action buttons (lCwf ActivityFilterActionButtons) ----

// hasUnsavedChanges is ActivityFilterBar e0: in a custom view, filters
// that differ from its saved ones; elsewhere any filter on.
func (v *Activity) hasUnsavedChanges() bool {
	if vw, ok := v.customView(); ok {
		return !actStateOf(vw.Filters).equal(v.curState())
	}
	return v.curState().any()
}

// filterActions is ActivityFilterActionButtons under the filter menu: the
// primary "Create Custom View" (disabled without unsaved filters or at 15
// views), in a custom view the "Save as …" dropdown (New view / Current
// view) and the danger "Delete View".
func (v *Activity) filterActions(gtx C) D {
	_, custom := v.customView()
	on := v.hasUnsavedChanges() && v.viewCount() < actMaxViews
	if v.saveViewBtn.Clicked(gtx) && on {
		v.menu = actMenuNone
		if custom {
			v.saveAsMenu()
		} else {
			v.openViewModal(actSaveView, core.ActivityView{})
		}
	}
	if v.deleteViewBtn.Clicked(gtx) {
		if vw, ok := v.customView(); ok {
			v.menu = actMenuNone
			v.confirmDeleteView(vw)
		}
	}
	label := "Create Custom View"
	if custom {
		label = "Save as …"
	}
	return layout.Inset{Top: 8, Bottom: 8, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Flexed(1, func(gtx C) D { return v.primarySmall(gtx, &v.saveViewBtn, label, custom, on) }),
			layout.Rigid(func(gtx C) D {
				if !custom {
					return D{}
				}
				return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return v.button(gtx, &v.deleteViewBtn, btnDanger, true, "Delete View") })
			}),
		)
	})
}

// primarySmall is a small primary c-button (28px, 13px bold), full width,
// with a 16px caret for "Save as …"; disabled is base-ter under content-sec.
func (v *Activity) primarySmall(gtx C, c *widget.Clickable, label string, caret, on bool) D {
	return c.Layout(gtx, func(gtx C) D {
		w, h := gtx.Constraints.Max.X, gtx.Dp(28)
		bg, fg := v.p.Green, rgb(0xffffff)
		switch {
		case !on:
			bg, fg = v.p.Track, v.p.Sec
		case c.Hovered():
			bg = v.p.GreenHover
			pointerCursor(gtx)
		default:
			pointerCursor(gtx)
		}
		rrect(gtx, image.Rect(0, 0, w, h), gtx.Dp(4), bg)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Center.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, fg, label) }),
				layout.Rigid(func(gtx C) D {
					if !caret {
						return D{}
					}
					return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, "caret-down", 16, fg) })
				}),
			)
		})
		return D{Size: image.Pt(w, h)}
	})
}

// saveAsMenu is ActivityFilterSaveButton's "Save as …" popover: "New
// view" (the save modal) or "Current view" (updateActivityViewsByApi
// with the filters, then '"{view}" has been saved successfully.').
func (v *Activity) saveAsMenu() {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	at := v.pointerAt()
	items := []popups.MenuItem{
		{Key: "new-view", Label: "New view", Do: func() { v.openViewModal(actSaveView, core.ActivityView{}) }},
		{Key: "current-view", Label: "Current view", Do: v.saveCurrentView},
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: image.Rectangle{Min: at, Max: at}, Width: 0}))
}

func (v *Activity) saveCurrentView() {
	vw, ok := v.customView()
	if !ok {
		return
	}
	vw.Filters = v.curState().viewFilters()
	c := v.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		got, err := c.SaveActivityViewFilters(ctx, vw)
		v.notes.mu.Lock()
		v.notes.fns = append(v.notes.fns, func() {
			if err != nil {
				v.h.Toast("There was an error saving the view.")
				return
			}
			if got.ID == "" {
				got = vw
			}
			v.replaceCustom(got)
			v.h.Toast(`"` + vw.Name + `" has been saved successfully.`)
		})
		v.notes.mu.Unlock()
		v.h.Invalidate()
	}()
}

// ---- ActivityFilterSaveViewModal ----

const (
	actCreateTab  = iota // new_name_new_tab: "Create a new view" with ActivityAddTabContent
	actSaveView          // new_name: "Save this view" (the current filters)
	actRenameView        // edit_name: "Rename this view"
)

// actViewModal is the modal's state.
type actViewModal struct {
	v       *Activity
	kind    int
	view    core.ActivityView // rename: the view
	modal   *popups.Modal
	ed      widget.Editor
	name0   string
	emoji0  string
	emoji   string
	focused bool
	err     string // Slack's error code from the last save
	pending bool

	emojiBtn, clearBtn widget.Clickable

	// ActivityAddTabContent (the new tab's state, sTqq wn)
	st       actState
	dense    bool
	vipSort  bool
	unreadSw widget.Clickable
	layoutC  [2]widget.Clickable
	rowC     [len(actFilters) + 2]widget.Clickable
	sortC    [2]widget.Clickable
}

// openViewModal opens ActivityFilterSaveViewModal for kind.
func (v *Activity) openViewModal(kind int, vw core.ActivityView) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	m := &actViewModal{v: v, kind: kind, view: vw}
	m.ed.SingleLine, m.ed.Submit = true, true
	if kind == actRenameView {
		m.name0, m.emoji0, m.emoji = vw.Name, vw.Emoji, vw.Emoji
		m.ed.SetText(vw.Name)
		n := len([]rune(vw.Name))
		m.ed.SetCaret(n, n)
	}
	title, sub := "Save this view", "Name it something you’ll remember - your future self will thank you"
	switch kind {
	case actCreateTab:
		title, sub = "Create a new view", ""
	case actRenameView:
		title = "Rename this view"
	}
	save := &popups.ModalButton{Label: "Save", Kind: popups.BtnPrimary, Enabled: m.canSave, Do: func() bool { m.save(); return false }}
	m.modal = popups.NewModal(ph, popups.ModalOpts{Title: title, Subtitle: sub, Width: 520, Body: m.layout,
		Buttons:    []*popups.ModalButton{{Label: "Cancel", Kind: popups.BtnOutline}, save},
		FocusStops: func() []event.Tag { return []event.Tag{&m.ed} }})
	v.lastViewModal = m
	ph.ShowOverlay(m.modal)
}

// name is the name the modal saves.
func (m *actViewModal) name() string { return m.ed.Text() }

// changed is fNIs Q: the new tab's state, the name or the emoji changed.
func (m *actViewModal) changed() bool {
	if m.kind == actCreateTab && (m.st.any() || m.dense || m.vipSort) {
		return true
	}
	return m.name() != m.name0 && m.name() != "" || m.emoji != m.emoji0
}

// alert is the text under the input: Slack's answer, a taken name (ea),
// or "View name is required." for a new tab with changes and no name
// (el).
func (m *actViewModal) alert() string {
	if m.err != "" {
		return actViewErrText(m.err)
	}
	if n := m.name(); n != m.name0 {
		for _, x := range m.v.viewNames() {
			if x == n && n != "" {
				return actViewErrText("duplicate_name")
			}
		}
	}
	if m.kind == actCreateTab && m.changed() && m.name() == "" && m.emoji == "" {
		return "View name is required."
	}
	return ""
}

// canSave is fNIs X: a name of 1-80 characters (or an emoji), something
// changed, no error.
func (m *actViewModal) canSave() bool {
	n := len([]rune(m.name()))
	valid := n > 0 && n <= 80 || m.emoji != ""
	changed := m.name() != m.name0 || m.emoji != m.emoji0
	return valid && changed && m.alert() == "" && !m.pending
}

// save is fNIs et: create (a new tab with its state, or the current
// filters), or rename; then '"{name}" has been saved successfully.'.
func (m *actViewModal) save() {
	if !m.canSave() {
		return
	}
	v := m.v
	name, emoji := m.name(), m.emoji
	m.pending = true
	done := func(code string) {
		m.pending = false
		if code != "" {
			m.err = code
			return
		}
		m.modal.Dismiss()
		v.h.Toast(`"` + name + `" has been saved successfully.`)
	}
	switch m.kind {
	case actRenameView:
		vw := m.view
		nm := name
		if nm == m.name0 {
			nm = "" // only a changed name goes out
		}
		c := v.h.Core()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			got, err := c.RenameActivityView(ctx, vw, nm, emoji)
			v.notes.mu.Lock()
			v.notes.fns = append(v.notes.fns, func() {
				if err != nil {
					done(actViewErrCode(err))
					return
				}
				if got.ID == "" {
					got = vw
					got.Name, got.Emoji = name, emoji
				}
				v.replaceCustom(got)
				done("")
			})
			v.notes.mu.Unlock()
			v.h.Invalidate()
		}()
	case actCreateTab:
		d := "compact"
		if m.dense {
			d = "dense"
		}
		s := "newest"
		if m.vipSort {
			s = actSortVIP
		}
		v.createView(core.ActivityView{Name: name, Emoji: emoji, Sort: s, Density: d, Filters: m.st.viewFilters()}, done)
	default:
		// the view shown's sort and density, with its filters
		vw := v.slotView(v.slot())
		d := "compact"
		if v.dense {
			d = "dense"
		}
		v.createView(core.ActivityView{Name: name, Emoji: emoji, Sort: firstNonEmpty(vw.Sort, "newest"), Density: d,
			Filters: v.curState().viewFilters()}, done)
	}
}

// layout is the modal's body: margins 24px each side inside the modal's
// own (fNIs: margin_left_150 margin_right_150), 8px down; "View name
// (required)" for a new tab; the input container; the alert; the new
// tab's content.
func (m *actViewModal) layout(gtx C) D {
	v := m.v
	if !m.focused {
		gtx.Execute(key.FocusCmd{Tag: &m.ed})
		m.focused = true
	}
	for {
		e, ok := m.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			m.err = ""
		case widget.SubmitEvent:
			m.save()
		}
	}
	ph, _ := v.h.(popups.Host)
	if m.emojiBtn.Clicked(gtx) && ph != nil {
		at := popups.ModalBodyPoint(gtx, m.modal, image.Pt(gtx.Dp(24), gtx.Dp(8+38+5)))
		ph.OpenEmojiPicker(at, func(name string) {
			v.notes.mu.Lock()
			v.notes.fns = append(v.notes.fns, func() { m.emoji, m.err = ":"+strings.Trim(name, ":")+":", "" })
			v.notes.mu.Unlock()
			v.h.Invalidate()
		})
	}
	if m.clearBtn.Clicked(gtx) {
		m.emoji = ""
	}
	if m.kind == actCreateTab {
		m.addTabClicks(gtx)
	}
	return layout.Inset{Left: 24, Right: 24, Top: 8, Bottom: 12}.Layout(gtx, func(gtx C) D {
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				if m.kind != actCreateTab {
					return D{}
				}
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
					return row(gtx, layout.Baseline,
						layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, "View name") }),
						layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, v.p.Text, " (required)") }),
					)
				})
			}),
			layout.Rigid(m.nameField),
			layout.Rigid(func(gtx C) D {
				a := m.alert()
				if a == "" {
					return D{}
				}
				return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D {
					return row(gtx, layout.Start,
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Top: 2, Right: 6}.Layout(gtx, func(gtx C) D { return v.icon(gtx, "warning", 14, v.p.Red) })
						}),
						layout.Flexed(1, func(gtx C) D { return v.para(gtx, 13, 18, wReg, v.p.Red, 0, a) }),
					)
				})
			}),
			layout.Rigid(func(gtx C) D {
				if m.kind != actCreateTab {
					return D{}
				}
				return m.addTab(gtx)
			}),
		)
	})
}

// nameField is p-activity_ia4_page__save_view_modal__input_container: 38px,
// 1px otl-pry border (4px radius), the emoji picker (20px, 8px each side;
// the view's icon or "emoji" until one is picked), the 36px input
// (placeholder "New view"), "Remove emoji" (close-circle-filled,
// content-sec) once one is set; the focus ring while typing; 16px under.
func (m *actViewModal) nameField(gtx C) D {
	v := m.v
	w, h := gtx.Constraints.Max.X, gtx.Dp(38)
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(4)
	rrect(gtx, r, rad, v.p.Bg)
	if gtx.Focused(&m.ed) {
		border(gtx, r.Inset(-gtx.Dp(2)), rad+gtx.Dp(2), gtx.Dp(3), v.p.Focus)
		border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.Link)
	} else {
		border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.BorderStr)
	}
	icon := "emoji"
	if m.kind == actRenameView {
		if ic := actViewIcon(actStateOf(m.view.Filters)); ic != "" {
			icon = ic
		}
	} else if m.kind == actCreateTab {
		if ic := actViewIcon(m.st); ic != "" {
			icon = ic
		}
	}
	bw := gtx.Dp(36)
	func() {
		g := gtx
		g.Constraints = layout.Exact(image.Pt(bw, h))
		m.emojiBtn.Layout(g, func(gtx C) D {
			pointerCursor(gtx)
			fg := v.p.Text
			if m.emojiBtn.Hovered() {
				fg = v.p.Link
			}
			return layout.Center.Layout(gtx, func(gtx C) D {
				if m.emoji != "" {
					if u, _, ok := v.h.Core().Emoji(strings.Trim(m.emoji, ":")); ok && u != "" {
						return v.emojiImg(gtx, u, 20)
					}
				}
				return v.icon(gtx, icon, 20, fg)
			})
		})
	}()
	right := 0
	if m.emoji != "" {
		right = bw
		func() {
			defer op.Offset(image.Pt(w-bw, 0)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(bw, h))
			m.clearBtn.Layout(g, func(gtx C) D {
				pointerCursor(gtx)
				fg := v.p.Sec
				if m.clearBtn.Hovered() {
					fg = v.p.Text
				}
				return layout.Center.Layout(gtx, func(gtx C) D { return v.icon(gtx, "close-circle-filled", 20, fg) })
			})
		}()
	}
	func() {
		defer op.Offset(image.Pt(bw, 0)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(max(0, w-bw-right), h))
		layout.W.Layout(g, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Stack{Alignment: layout.W}.Layout(gtx,
				layout.Stacked(func(gtx C) D {
					if m.ed.Len() > 0 {
						return D{}
					}
					return v.label(gtx, 15, wReg, v.p.Faint, "New view")
				}),
				layout.Stacked(func(gtx C) D { return v.th.Editor(gtx, &m.ed, 15, v.p.Text, v.p.Mention) }),
			)
		})
	}()
	return D{Size: image.Pt(w, h+gtx.Dp(16))}
}

// addTabRows are ActivityFilterByMenu's ADD_TAB rows: the type rows
// (DMs while DMs show in Activity, Reminders while reminders do) then VIP
// and Cleared items; index len(actFilters) is VIP, +1 Cleared items.
func (m *actViewModal) addTabRows() []int {
	c := m.v.h.Core()
	var out []int
	for i := range actFilters {
		if i == 0 && !m.v.dmsInActivity() || i == len(actFilters)-1 && !c.GlobalNotifyBool("global_reminders_in_activity", true) {
			continue
		}
		out = append(out, i)
	}
	return append(out, len(actFilters), len(actFilters)+1)
}

func (m *actViewModal) addTabClicks(gtx C) {
	if m.unreadSw.Clicked(gtx) {
		m.st.unread = !m.st.unread
	}
	for i := range m.layoutC {
		if m.layoutC[i].Clicked(gtx) {
			m.dense = i == 1
		}
	}
	for i := range m.sortC {
		if m.sortC[i].Clicked(gtx) {
			m.vipSort = i == 1
		}
	}
	for _, r := range m.addTabRows() {
		if !m.rowC[r].Clicked(gtx) {
			continue
		}
		switch {
		case r < len(actFilters):
			m.st.filters[r] = !m.st.filters[r]
		case r == len(actFilters):
			m.st.vip = !m.st.vip
		default:
			m.st.cleared = !m.st.cleared
		}
	}
}

// addTab is ActivityAddTabContent: without activity_global_settings
// "Layout" and its two cards (FgHy ADD_TAB) and a rule; "Only include
// unreads" with its switch (16px above and below); "Filter by" and the
// rows in two columns (12px above and below); "Sort by" Newest / VIP
// unreads on top with VIPs (KJvs ADD_TAB). Specific channels ("In") isn't
// in Clack.
func (m *actViewModal) addTab(gtx C) D {
	v := m.v
	var ch []layout.FlexChild
	if !v.globalSettings() {
		ch = append(ch,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, "Layout") })
			}),
			layout.Rigid(m.layoutCards),
			layout.Rigid(func(gtx C) D { return hline(gtx, v.p.Border) }),
		)
	}
	ch = append(ch,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 16, Bottom: 16}.Layout(gtx, func(gtx C) D {
				return m.unreadSw.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return row(gtx, layout.Middle,
						layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, "Only include unreads") }),
						layout.Rigid(func(gtx C) D { return v.switchKnob(gtx, m.st.unread) }),
					)
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 12, Bottom: 12}.Layout(gtx, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, "Filter by") })
					}),
					layout.Rigid(m.rowGrid),
				)
			})
		}),
	)
	if len(v.h.Core().VIPs()) > 0 {
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, "Sort by") })
					}),
					layout.Rigid(func(gtx C) D { return m.radio(gtx, &m.sortC[0], "Newest", !m.vipSort) }),
					layout.Rigid(func(gtx C) D { return m.radio(gtx, &m.sortC[1], "VIP unreads on top", m.vipSort) }),
				)
			})
		}))
	}
	return col(gtx, ch...)
}

// rowGrid is p-activity_ia4_page__filter-menu__add-tab__container: two
// columns filled top to bottom, each row a 14px checkbox, 8px, the 20px
// icon, 8px, the label.
func (m *actViewModal) rowGrid(gtx C) D {
	v := m.v
	rows := m.addTabRows()
	n := (len(rows) + 1) / 2
	w := gtx.Constraints.Max.X
	cw := w / 2
	rh := gtx.Dp(28)
	for k, r := range rows {
		x, y := (k/n)*cw, (k%n)*rh
		label, icon, on := "", "", false
		switch {
		case r < len(actFilters):
			label, icon, on = actFilters[r].label, actFilters[r].icon, m.st.filters[r]
		case r == len(actFilters):
			label, icon, on = actExtras[0].label, actExtras[0].icon, m.st.vip
		default:
			label, icon, on = actExtras[1].label, actExtras[1].icon, m.st.cleared
		}
		func() {
			defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(cw, rh))
			m.rowC[r].Layout(g, func(gtx C) D {
				pointerCursor(gtx)
				return layout.W.Layout(gtx, func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D { return v.actCheck(gtx, 14, on) }),
						hgap(8),
						layout.Rigid(func(gtx C) D { return v.icon(gtx, icon, 20, v.p.Sec) }),
						hgap(8),
						layout.Flexed(1, func(gtx C) D { return v.para(gtx, 15, 22, wReg, v.p.Text, 1, label) }),
					)
				})
			})
		}()
	}
	return D{Size: image.Pt(w, n*rh)}
}

// layoutCards is FgHy's ADD_TAB toggle: two 76px cards 8px apart (1px
// otl-ter border, radius 8; the chosen one 2px otl-hgl-1), the 20px icon
// over the bold label 8px under it.
func (m *actViewModal) layoutCards(gtx C) D {
	v := m.v
	w := gtx.Constraints.Max.X
	cw := (w - gtx.Dp(8)) / 2
	h := gtx.Dp(76)
	for i, c := range [2]struct{ icon, label string }{{"table", "Detailed"}, {"menu", "Dense"}} {
		on := m.dense == (i == 1)
		func() {
			defer op.Offset(image.Pt(i*(cw+gtx.Dp(8)), gtx.Dp(4))).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(cw, h))
			m.layoutC[i].Layout(g, func(gtx C) D {
				pointerCursor(gtx)
				r := image.Rect(0, 0, cw, h)
				rrect(gtx, r, gtx.Dp(8), v.p.Bg)
				if on {
					border(gtx, r, gtx.Dp(8), gtx.Dp(2), v.p.Link)
				} else {
					border(gtx, r, gtx.Dp(8), max(1, gtx.Dp(1)), v.p.Border)
				}
				return layout.Center.Layout(gtx, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return v.icon(gtx, c.icon, 20, v.p.Text) }),
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, c.label) })
						}),
					)
				})
			})
		}()
	}
	return D{Size: image.Pt(w, h+gtx.Dp(4+16))}
}

// radio is a c-input_radio row (16px, 12px before the 15px label).
func (m *actViewModal) radio(gtx C, c *widget.Clickable, label string, on bool) D {
	v := m.v
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					s := gtx.Dp(16)
					r := image.Rect(0, 0, s, s)
					if on {
						rrect(gtx, r, s/2, v.p.Link)
						d := gtx.Dp(6)
						rrect(gtx, image.Rect((s-d)/2, (s-d)/2, (s+d)/2, (s+d)/2), d/2, rgb(0xffffff))
					} else {
						rrect(gtx, r, s/2, v.p.Bg)
						border(gtx, r, s/2, max(1, gtx.Dp(1)), v.p.BorderStr)
					}
					return D{Size: image.Pt(s, s)}
				}),
				hgap(12),
				layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, v.p.Text, label) }),
			)
		})
	})
}

// switchKnob is the c-toggle switch the modal's "Only include unreads"
// uses (kit toggleSwitch's track).
func (b *base) switchKnob(gtx C, on bool) D {
	w, h := gtx.Dp(28), gtx.Dp(16)
	r := image.Rect(0, 0, w, h)
	if on {
		rrect(gtx, r, h/2, b.p.Green)
	} else {
		rrect(gtx, r, h/2, b.p.Faint)
	}
	k := h - gtx.Dp(4)
	x := gtx.Dp(2)
	if on {
		x = w - k - gtx.Dp(2)
	}
	theme.Circle(gtx, image.Pt(x+k/2, h/2), k, rgb(0xffffff))
	return D{Size: image.Pt(w, h)}
}

// ---- realtime: activity_views_updated ----

// viewsEvent is sD/F activityViewsUpdated: the views the event carries
// replace the saved ones (the shown one stays if it's still there).
func (v *Activity) viewsEvent(raw []byte) {
	st, ok := core.ActivityViewsEvent(raw)
	if !ok {
		return
	}
	v.applyViews(st, false)
}
