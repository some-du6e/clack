package views

// Live updates, as Slack's activity inbox takes realtime `activity`
// events (gantry client-boot-render sD/F handleActivityRTMEvent):
//
//   - activity_updated (activityUpdatedV2 → 46Vh pushActivityItemToStore
//     → P0K2 LE / F): an entry already listed is replaced (its read state
//     included); a new one goes into the list at its feed_ts, unless the
//     view can't show it: reminders and DMs while they're off in
//     Activity, a cleared entry outside Cleared items (or the reverse: it
//     leaves the list), a reaction under Unreads, an entry that isn't a
//     VIP's under the VIP filter, an automation's under "Exclude
//     automations" (activity_bot_item_type_reads) or a person's under
//     "Automations only" (activity_automations_only), another type than
//     the view's types, or a conversation outside its channels and
//     sections.
//   - activity_deleted drops the entry; activity_invalidated fetches the
//     feed again; activity_views_updated (its own event) replaces the
//     saved views (activity_views.go).
//   - A new unread entry while the list is scrolled down raises "New
//     activity ↑" (U5s4 ActivityUnreadsBanner): 12px from the top,
//     centred; it scrolls back up, and goes once the list is near the top
//     (48px).

import (
	"image"
	"slices"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// liveAccepts reports whether the view shown takes a new realtime entry.
func (v *Activity) liveAccepts(it *core.ActivityItem) bool {
	c := v.h.Core()
	switch {
	case it.Type == "":
		return false
	case it.Type == "saved_reminder" && !c.GlobalNotifyBool("global_reminders_in_activity", true),
		it.Type == "dm" && !v.dmsInActivity():
		return false
	case it.Archived != v.showCleared:
		return false
	case v.unread.Value && (it.Type == "message_reaction" || !it.Unread):
		return false
	case v.vip && !it.Priority:
		return false
	case c.ActivityBotItemTypeReads() && v.exclAuto && it.IsBot,
		c.ActivityAutomationsOnly() && v.autoOnly && !it.IsBot:
		// activityUpdatedV2: p && w && P, _ && x && !P
		return false
	}
	types, none := v.types()
	if none || types != "" && !slices.Contains(strings.Split(types, ","), it.Type) {
		return false
	}
	if v.dmOnly() && it.Type != "dm" && it.Type != "mpdm" {
		return false
	}
	if len(v.sections) > 0 || len(v.channels) > 0 {
		in := map[string]bool{}
		for _, ch := range v.channels {
			in[ch] = true
		}
		for _, s := range c.Snapshot().Sections {
			if slices.Contains(v.sections, s.ID) {
				for _, ch := range s.All {
					in[ch] = true
				}
				for _, ch := range s.Channels {
					in[ch] = true
				}
			}
		}
		if !in[it.Channel] {
			return false
		}
	}
	return true
}

// liveUpsert applies one realtime entry to the loaded page (a new Items
// slice: a load in flight may still read the old one).
func (v *Activity) liveUpsert(p *core.ActivityPage, in core.ActivityItem) {
	key := actKey(&in)
	for i := range p.Items {
		if actKey(&p.Items[i]) != key {
			continue
		}
		delete(v.override, key)
		if in.Archived != v.showCleared {
			// activityUpdatedV2: cleared elsewhere (or restored) leaves the list
			p.Items = slices.Delete(slices.Clone(p.Items), i, i+1)
			return
		}
		if in.Msg == nil {
			in.Msg, in.Root = p.Items[i].Msg, p.Items[i].Root
		}
		items := slices.Clone(p.Items)
		items[i] = in
		p.Items = items
		if r := v.rows[key]; r != nil {
			r.built = false
		}
		return
	}
	if !v.liveAccepts(&in) {
		return
	}
	at := len(p.Items)
	for i := range p.Items {
		if actTSLess(p.Items[i].FeedTS, in.FeedTS) {
			at = i
			break
		}
	}
	if at == len(p.Items) && p.Next != "" {
		return // older than everything loaded: a later page has it
	}
	p.Items = slices.Insert(slices.Clone(p.Items), at, in)
	if v.serverAll {
		v.picked[key] = true // eP2o: new rows join select-all
	}
	// ActivityUnreadsBanner: a newer unread entry (hasNewUnreadsFeedTs)
	if in.Unread && (v.newUnreadTS == "" || actTSLess(v.newUnreadTS, in.FeedTS)) {
		v.newUnreadTS = in.FeedTS
	}
}

// newActivityBanner is ActivityUnreadsBanner over the list: shown while a
// newer unread entry came in and the list is scrolled down past it; a
// 28px #1264a3 pill, radius 16, padding 0 12 0 16, 13px bold white "New
// activity" and the arrow, 0 4px 12px #00000014.
func (v *Activity) newActivityBanner(gtx C, listTop int) {
	if v.newUnreadTS == "" {
		return
	}
	pos := v.list.Position
	if pos.First == 0 && pos.Offset < gtx.Dp(48) {
		v.newUnreadTS = "" // scrolled back near the top
		return
	}
	if v.bannerNew.Clicked(gtx) {
		v.list.Position = layout.Position{}
		v.newUnreadTS = ""
		gtx.Execute(op.InvalidateCmd{})
		return
	}
	// only while the first row shown is older than the new entry
	page, _, _ := v.data.get()
	if pos.First < len(v.vis) {
		if i := v.vis[pos.First]; i >= 0 && i < len(page.Items) && !actTSLess(page.Items[i].FeedTS, v.newUnreadTS) {
			return
		}
	}
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	d := v.bannerNew.Layout(g, func(gtx C) D {
		pointerCursor(gtx)
		bg := rgb(0x1264a3)
		if v.bannerNew.Hovered() {
			bg = rgb(0x1e328f) // rgb(var(--dt_color-plt-indigo-80))
			if v.th.Dark {
				bg = rgb(0xabb7e1)
			}
		}
		mm := op.Record(gtx.Ops)
		dd := layout.Inset{Left: 16, Right: 12}.Layout(gtx, func(gtx C) D {
			g := gtx
			g.Constraints.Min.Y = gtx.Dp(28)
			return layout.W.Layout(g, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, rgb(0xffffff), "New activity") }),
					hgap(4),
					layout.Rigid(func(gtx C) D { return v.icon(gtx, "arrow-up", 13, rgb(0xffffff)) }),
				)
			})
		})
		call := mm.Stop()
		r := image.Rectangle{Max: dd.Size}
		theme.Shadow(gtx, r, gtx.Dp(16), gtx.Dp(12), argb(0x14000000))
		rrect(gtx, r, gtx.Dp(16), bg)
		call.Add(gtx.Ops)
		return dd
	})
	call := m.Stop()
	defer op.Offset(image.Pt((gtx.Constraints.Max.X-d.Size.X)/2, listTop+gtx.Dp(12))).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}
