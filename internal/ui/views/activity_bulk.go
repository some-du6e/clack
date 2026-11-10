package views

// Bulk selection ("triage mode"), as Slack's web client has it (gantry
// client-boot-render, webapp build a84662d2):
//
//   - Triage mode is 3x6A $Y: bulk action mode on (Custom select, P0K2 m1)
//     or anything selected (bulkItems). In it the filter bar holds only
//     ActivityFilterBulkAction (m+ho; Unreads, the filters, search and the
//     right-hand controls go: `!L &&`), every row leads with its checkbox
//     (O6z2 RowCheckbox in activity_multi_line_row_triage_container: 22px
//     in, the 16px checkbox, the row card 10px after it with 16px at its
//     right, activity_row_content_margin_triage), and
//     ActivityBulkSelectionBanner (i9bp) sits under the bar.
//   - A row's checkbox toggles it; with Shift it toggles the range from
//     the last one picked (0lkN ActivitySelectableRowsProvider). A click
//     on the row still opens it.
//   - The banner: "{n} items selected on page." and, while more is
//     loadable (or not all is picked) and no search is on, "Select
//     everything matching these filters." (filters changed from the
//     view's saved ones) or "Select everything in {view}"; that picks
//     every loaded row and turns on server-side select-all (P0K2 Ro):
//     "Everything matching these filters is selected." / "Everything in
//     {view} is selected.", later pages are picked as they load (eP2o),
//     the read buttons go and Clear becomes "Clear everything"
//     (ClearAllFilteredDialog → activity.clearAll {filters}).
//   - The buttons write once with every entry (bulk_mark_read,
//     bulk_archive, bulk_mark_unread); "Mark selected as unread" only with
//     activity_bulk_unread_tool (5Ns/ Yw). The toasts: "{n} items marked
//     as read" and "… marked as unread"; with the activity inbox, Clear
//     has none (`_ ||`).

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// triage is 3x6A $Y isTriageModeEnabled.
func (v *Activity) triage() bool { return v.bulkMode || len(v.picked) > 0 }

// bulkUnreadTool is 5Ns/ Yw: "Mark selected as unread" (and its toast).
func (v *Activity) bulkUnreadTool() bool {
	return v.h.Core().Experiment("activity_bulk_unread_tool") == "on"
}

// listKeys are the rows the list shows (3x6A j$ after Clack's
// client-side filters), in order: what Select all / reads / unreads,
// the banner's link and the Shift range pick from.
func (v *Activity) listKeys() []string {
	page, _, _ := v.data.get()
	var out []string
	for _, i := range v.vis {
		if i >= 0 && i < len(page.Items) {
			out = append(out, actKey(&page.Items[i]))
		}
	}
	return out
}

// pickRow is 0lkN onSelectRow: Shift with a last pick toggles the range
// to this row (selects it, or unselects it when the row was picked);
// otherwise the row toggles, and unpicking it hands "last" to the next
// picked row below.
func (v *Activity) pickRow(key string, shift bool) {
	keys := v.listKeys()
	on := v.picked[key]
	if shift && v.lastBulk != "" {
		a, b := -1, -1
		for i, k := range keys {
			if k == v.lastBulk {
				a = i
			}
			if k == key {
				b = i
			}
		}
		if a >= 0 && b >= 0 {
			if a > b {
				a, b = b, a
			}
			for _, k := range keys[a : b+1] {
				if on {
					delete(v.picked, k)
					v.serverAll = false
				} else {
					v.picked[k] = true
				}
			}
			v.lastBulk = key
			v.dirty = true
			return
		}
	}
	if on {
		delete(v.picked, key)
		v.serverAll = false
		v.lastBulk = ""
		after := false
		for _, k := range keys {
			if after && v.picked[k] {
				v.lastBulk = k
				break
			}
			after = after || k == key
		}
	} else {
		v.picked[key] = true
		v.lastBulk = key
	}
	v.dirty = true
}

// filtersChanged is i9bp A: the view's type filters differ from the ones
// it was saved with (none for All).
func (v *Activity) filtersChanged() bool {
	var saved actState
	if vw, ok := v.customView(); ok {
		saved = actStateOf(vw.Filters)
	}
	return saved.filters != v.filters
}

// bannerTexts is i9bp's text and link (link "" when it doesn't show) and
// whether the view's name is bold in them.
func (v *Activity) bannerTexts() (text, link string) {
	n := len(v.picked)
	name := v.slotLabel(v.slot())
	changed := v.filtersChanged()
	switch {
	case v.serverAll && changed:
		text = "Everything matching these filters is selected."
	case v.serverAll:
		text = "Everything in " + name + " is selected."
	default:
		text = strconv.Itoa(n) + " " + map[bool]string{true: "item", false: "items"}[n == 1] + " selected on page."
	}
	page, _, _ := v.data.get()
	search := v.searchOn && strings.TrimSpace(v.query.text()) != ""
	if !v.serverAll && !search && n >= 1 && (page.Next != "" || n < len(v.listKeys())) {
		if changed {
			link = "Select everything matching these filters."
		} else {
			link = "Select everything in " + name
		}
	}
	return text, link
}

// selectEverything is the banner's link (i9bp S): every loaded row, and
// server-side select-all on.
func (v *Activity) selectEverything() {
	for _, k := range v.listKeys() {
		v.picked[k] = true
	}
	v.serverAll, v.dirty = true, true
}

// banner is p-activity_ia4_page__bulk_selection_banner: margin 0 16px 8px,
// padding 8px, radius 8, light-dark(base-sec, base-ter), 13px/22px; the
// text, 4px, the link (the view's name bold in both).
func (v *Activity) banner(gtx C) D {
	if v.bannerLink.Clicked(gtx) {
		v.selectEverything()
	}
	text, link := v.bannerTexts()
	name := v.slotLabel(v.slot())
	bg := v.p.ReadFill
	if v.th.Dark {
		bg = v.p.Track // base-ter
	}
	return layout.Inset{Left: 16, Right: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		d := layout.UniformInset(8).Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.boldName(gtx, text, name, v.p.Text, false) }),
				layout.Rigid(func(gtx C) D {
					if link == "" {
						return D{}
					}
					return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D {
						return v.bannerLink.Layout(gtx, func(gtx C) D {
							pointerCursor(gtx)
							return v.boldName(gtx, link, name, v.p.Link, v.bannerLink.Hovered())
						})
					})
				}),
			)
		})
		call := m.Stop()
		rrect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(8), bg)
		call.Add(gtx.Ops)
		return d
	})
}

// boldName draws s (13px/22px) with the view's name in it bold (i9bp's
// <strong>{viewName}</strong>), underlined while hovered for links.
func (v *Activity) boldName(gtx C, s, name string, col color.NRGBA, under bool) D {
	pre, post, found := "", "", false
	if i := strings.LastIndex(s, name); name != "" && i >= 0 && (strings.HasPrefix(s, "Everything in ") || strings.HasPrefix(s, "Select everything in ")) {
		pre, post, found = s[:i], s[i+len(name):], true
	}
	if !found {
		d := v.para(gtx, 13, 22, wReg, col, 1, s)
		if under {
			fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(4), d.Size.X, d.Size.Y-gtx.Dp(3)), col)
		}
		return d
	}
	d := row(gtx, layout.Baseline,
		layout.Rigid(func(gtx C) D { return v.para(gtx, 13, 22, wReg, col, 1, pre) }),
		layout.Rigid(func(gtx C) D { return v.para(gtx, 13, 22, wBold, col, 1, name) }),
		layout.Rigid(func(gtx C) D { return v.para(gtx, 13, 22, wReg, col, 1, post) }),
	)
	if under {
		fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(4), d.Size.X, d.Size.Y-gtx.Dp(3)), col)
	}
	return d
}

// pickedItems are the picked rows in list order (the bulk writes'
// entries).
func (v *Activity) pickedItems() ([]core.ActivityItem, []string) {
	page, _, _ := v.data.get()
	var items []core.ActivityItem
	var keys []string
	for i := range page.Items {
		it := &page.Items[i]
		if k := actKey(it); v.picked[k] {
			items, keys = append(items, *it), append(keys, k)
		}
	}
	return items, keys
}

// bulkRows draws the filter bar's bulk buttons (triage mode): Mark
// selected as read, Clear selected (Clear everything under server-side
// select-all; none in Cleared items), Mark selected as unread (the
// experiment); icon-only under 415px (isSmallWidth), each 8px after the
// last; disabled with nothing picked.
func (v *Activity) bulkRows(gtx C, x int) int {
	on := len(v.picked) > 0
	small := gtx.Constraints.Max.X < gtx.Dp(415)
	type b struct {
		c           *widget.Clickable
		icon, label string
	}
	var bs []b
	if !v.serverAll {
		bs = append(bs, b{&v.bulkRead, "mark-as-read", "Mark selected as read"})
	}
	if !v.showCleared {
		label := "Clear selected"
		if v.serverAll {
			label = "Clear everything"
		}
		bs = append(bs, b{&v.bulkClear, "eraser", label})
	}
	if !v.serverAll && v.bulkUnreadTool() {
		bs = append(bs, b{&v.bulkUnread, "mark-as-unread", "Mark selected as unread"})
	}
	for _, bt := range bs {
		label := bt.label
		if small {
			label = ""
		}
		x += gtx.Dp(8)
		x += actAt(gtx, x, func(gtx C) D { return v.bulkButton(gtx, bt.c, bt.icon, label, on) }).Size.X
	}
	return x
}

// bulkClicks runs the bulk buttons and the select menu.
func (v *Activity) bulkClicks(gtx C) {
	if v.selAllBtn.Clicked(gtx) {
		// ActivityFilterBulkAction R: everything shown picked unpicks
		// (kA), anything else picks every row shown (kl)
		keys := v.listKeys()
		all := len(keys) > 0
		for _, k := range keys {
			all = all && v.picked[k]
		}
		if all {
			v.endSelect()
		} else {
			for _, k := range keys {
				v.picked[k] = true
			}
		}
		v.dirty = true
	}
	if v.selMenuBtn.Clicked(gtx) {
		v.menu = actToggleMenu(v.menu, actMenuSelect)
	}
	for i := range v.selC {
		if !v.selC[i].Clicked(gtx) {
			continue
		}
		v.menu = actMenuNone
		switch i {
		case 0: // Select all
			v.pick(func(*core.ActivityItem, bool) bool { return true }, false)
		case 1: // Select reads
			v.pick(func(_ *core.ActivityItem, unread bool) bool { return !unread }, true)
		case 2: // Select unreads
			v.pick(func(_ *core.ActivityItem, unread bool) bool { return unread }, true)
		case 3: // Custom select: bulk action mode, nothing picked
			v.endSelect()
			v.bulkMode = true
		}
		v.dirty = true
	}
	if v.bulkRead.Clicked(gtx) {
		v.bulk(0)
	}
	if v.bulkClear.Clicked(gtx) {
		if v.serverAll {
			v.clearEverything()
		} else {
			v.bulk(1)
		}
	}
	if v.bulkUnread.Clicked(gtx) {
		v.bulk(2)
	}
}

// pick is BulkActionsSelect's rows: Select all adds every row shown;
// Select reads / unreads start over (kA) with those.
func (v *Activity) pick(want func(it *core.ActivityItem, unread bool) bool, reset bool) {
	if reset {
		v.endSelect()
	}
	page, _, _ := v.data.get()
	for _, i := range v.vis {
		if i < 0 || i >= len(page.Items) {
			continue
		}
		it := &page.Items[i]
		k := actKey(it)
		if want(it, v.isUnread(it, k)) {
			v.picked[k] = true
		}
	}
}

// endSelect is P0K2 kA: nothing picked, bulk action mode and server-side
// select-all off.
func (v *Activity) endSelect() {
	clear(v.picked)
	v.bulkMode, v.serverAll, v.lastBulk = false, false, ""
	v.dirty = true
}

// bulk runs a bulk action on the picked rows: 0 mark read (bulk_mark_read),
// 1 clear (bulk_archive), 2 mark unread (bulk_mark_unread), one write with
// every entry; the selection ends right away, as in Slack.
func (v *Activity) bulk(action int) {
	items, keys := v.pickedItems()
	if len(items) == 0 {
		return
	}
	v.endSelect()
	n := len(items)
	noun := "items"
	if n == 1 {
		noun = "item"
	}
	switch action {
	case 0, 2:
		read := action == 0
		prev := make(map[string]bool, len(keys))
		for _, k := range keys {
			if r, ok := v.override[k]; ok {
				prev[k] = r
			}
			v.override[k] = read
		}
		v.dirty = true
		if read {
			v.h.Toast(strconv.Itoa(n) + " " + noun + " marked as read")
		} else {
			v.h.Toast(strconv.Itoa(n) + " " + noun + " marked as unread")
		}
		v.write("Couldn’t update the items: ", func(ctx context.Context, c *core.App) error {
			if read {
				return c.MarkActivityReadBulk(ctx, items...)
			}
			return c.MarkActivityUnreadBulk(ctx, items...)
		}, func() {
			for _, k := range keys {
				if r, ok := prev[k]; ok {
					v.override[k] = r
				} else {
					delete(v.override, k)
				}
			}
			v.dirty = true
		})
	case 1:
		for _, k := range keys {
			v.cleared[k] = true
			if v.selOK && v.selKey == k {
				v.selOK = false
			}
		}
		v.dirty = true
		v.write("Couldn’t clear the items: ", func(ctx context.Context, c *core.App) error {
			return c.ClearActivityBulk(ctx, items...)
		}, func() {
			for _, k := range keys {
				delete(v.cleared, k)
			}
			v.dirty = true
		})
	}
}

// actEveryType is every activity item type (the 3x6A badge switch's
// cases, the uZNp enum): FtTX PK's list when no type filter narrows the
// view.
var actEveryType = []string{"thread_v2", "channel", "message_reaction", "internal_channel_invite", "list_record_edited",
	"bot_dm_bundle", "at_user", "at_user_group", "at_channel", "at_everyone", "keyword", "list_record_assigned",
	"list_user_mentioned", "external_channel_invite", "external_dm_invite", "shared_workspace_invite",
	"unjoined_channel_mention", "quietly_added_to_channel", "list_todo_notification", "list_approval_request",
	"list_approval_reviewed", "saved_reminder", "generic_system_alert", "dm", "team_joiner_invite",
	"code_review_requested", "prejoin_dm_welcome_party_alert", "mpdm", "bot_dm", "thread_reply",
	"example_user_alert", "calendar_upcoming_event", "file_deleted"}

// clearAllFilters is ClearAllFilteredDialog's filters: FtTX oe on the
// view (entry types from the rows, the flags, its sections and channels,
// read_state reads_and_unreads), then without reminders in Activity PK
// drops saved_reminder (from every type when no row narrows it). ok is
// false when nothing is left to clear (Mh).
func (v *Activity) clearAllFilters() (core.ActivityClearFilters, bool) {
	s := v.curState()
	f := core.ActivityClearFilters{UnreadOnly: s.unread, PriorityOnly: s.vip, ArchiveOnly: s.cleared,
		SectionIDs: s.sections, ChannelIDs: s.channels, ReadState: "reads_and_unreads"}
	f.EntryTypes = s.viewFilters().EntryTypes
	if !v.h.Core().GlobalNotifyBool("global_reminders_in_activity", true) {
		types := f.EntryTypes
		if types == nil {
			types = actEveryType
		}
		var keep []string
		for _, t := range types {
			if t != "saved_reminder" {
				keep = append(keep, t)
			}
		}
		if len(keep) > 0 {
			f.EntryTypes = keep
		} else if f.EntryTypes != nil {
			return f, false
		}
	}
	return f, true
}

// clearEverything is ClearAllFilteredDialog: "Are you sure you want to
// clear everything matching this filter?", 'Use the "Cleared items"
// filter to access these once cleared.' (or "Something went wrong. Please
// try again." after a failure), Cancel / Yes.
func (v *Activity) clearEverything() {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	f, ok := v.clearAllFilters()
	if !ok {
		v.endSelect()
		return
	}
	failed, pending := false, false
	body := func(gtx C) D {
		s := `Use the "Cleared items" filter to access these once cleared.`
		if failed {
			s = "Something went wrong. Please try again."
		}
		return v.para(gtx, 15, 22, wReg, v.p.Text, 0, s)
	}
	var m *popups.Modal
	yes := &popups.ModalButton{Label: "Yes", Kind: popups.BtnPrimary, Enabled: func() bool { return !pending }, Do: func() bool {
		pending, failed = true, false
		_, keys := v.pickedItems()
		c := v.h.Core()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err := c.ClearAllActivity(ctx, f)
			v.notes.mu.Lock()
			v.notes.fns = append(v.notes.fns, func() {
				pending = false
				if err != nil {
					failed = true
					return
				}
				for _, k := range keys {
					v.cleared[k] = true
				}
				v.endSelect()
				v.load(actRefresh)
				m.Dismiss()
			})
			v.notes.mu.Unlock()
			v.h.Invalidate()
		}()
		return false
	}}
	m = popups.NewModal(ph, popups.ModalOpts{Title: "Are you sure you want to clear everything matching this filter?", Body: body,
		Buttons: []*popups.ModalButton{{Label: "Cancel", Kind: popups.BtnOutline}, yes}, AutoFocus: yes})
	ph.ShowOverlay(m)
}

// rowCheck is O6z2 RowCheckbox in the triage grid: the 16px checkbox
// centred on the row, 22px in; Shift held toggles the range.
func (v *Activity) rowCheck(gtx C, r *actRow, h int) {
	for {
		c, ok := r.check.Update(gtx)
		if !ok {
			break
		}
		v.pickRow(r.key, c.Modifiers.Contain(key.ModShift))
		gtx.Execute(op.InvalidateCmd{})
	}
	s := gtx.Dp(16)
	defer op.Offset(image.Pt(gtx.Dp(22), (h-s)/2)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(s, s))
	r.check.Layout(g, func(gtx C) D {
		pointerCursor(gtx)
		return v.actCheck(gtx, 16, v.picked[r.key])
	})
}

// actTriageLead is the triage grid's room before the row card: 22px, the
// 16px checkbox, the card's 10px margin; actTriageEnd the card's 16px
// right margin (activity_row_content_margin_triage: 0 16px 0 10px).
const (
	actTriageLead = 22 + 16 + 10
	actTriageEnd  = 16
)
