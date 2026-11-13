package views

// The Activity rows' marks and buttons from the gantry build's shipping
// rows (client-boot-render, 2026-11; docs/ACTIVITY.md "Row actions"):
//
//   - read-row marks   U5s4 ActivityRowContentStatusIndicatorWithResolvedReply
//     (StatusIcon, svza EngagementStatusBadge compact): the completed
//     to-do, Draft, You replied and You reacted, each a 20px plt-gray-10
//     circle with a 12px glyph
//   - reminders        xSv9 ActivityItemMarkCompleteButton (Detailed: a
//     small "✓ Mark Complete" button in the body; Dense: the hover bar's
//     icon button), Pck2 ActivityItemRescheduleButton → SP97 RemindMenu
//     (ccoC ReminderTimeMenu, 2a23 calculateDueDateTs)
//   - Slack Connect    Qt0R ActivityRowContentInviteActions → 3Oc+
//     SlackConnectInviteActions (Ignore, Accept), rPCk / hCGE the Ignore
//     modals, SlackConnectConfirmTrustModal; a channel invite's Accept
//     (dcZX SlackConnectAcceptChannelInviteAction, the multi-pane accept
//     modal in popups/sc_accept.go, g7VB and SBlm after it)
//   - status column    the typing dots (rZx3 z8, +SIc), ahead of the
//     unread badge
//   - entry_extras     SKIQ ActivityRowContentReactions under a reaction
//     row (rZx3 GY)
//   - subtext          /ad9 ActivityMessage's inline subtext span
//   - sticky headers   U5s4 ActivityList stickyItems (the VIP sort's
//     section headers) and useHideActionsBehindStickyHeader
//
// Build d3de3a487b1c (2026-11-12) moved U5s4's components into
// boot-shared module 5465; the ids above are the ones docs/ACTIVITY.md
// cites.
//   - clear all        ActivityClearAllToast (activity_clear_all_completed)
//   - offline          Rdc4 ActivityRowLoader → DXqe InlineOfflineIndicator

import (
	"context"
	"errors"
	"image"
	"image/color"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ---- saved items behind reminder rows ----

func savedKeyStr(k core.SavedKey) string {
	return k.ItemID + "-" + k.ItemType + "-" + k.TS + "-" + k.Detail
}

// reminderSaved is the saved item a reminder row is about (YeRq
// getMaybeUnknownSavedItemById): the item, whether it's still loading, and
// whether Slack said there's none (isNonExistent).
func (v *Activity) reminderSaved(it *core.ActivityItem) (s *core.SavedItem, loading, gone bool) {
	k, ok := core.ReminderKey(it)
	if !ok {
		return nil, false, false
	}
	key := savedKeyStr(k)
	if si, ok := v.saved[key]; ok {
		return &si, false, false
	}
	if v.savedGone[key] {
		return nil, false, true
	}
	return nil, true, false
}

// reminderIncomplete is rZx3 E8 (useIsReminderIncomplete): the saved item
// is known and isn't completed.
func (v *Activity) reminderIncomplete(it *core.ActivityItem) bool {
	s, _, _ := v.reminderSaved(it)
	return s != nil && !s.Completed()
}

// ensureSaved asks saved.get for every reminder row's saved item not yet
// asked for, in one call per load (fetchSavedReminders batches them).
func (v *Activity) ensureSaved(page *core.ActivityPage) {
	var keys []core.SavedKey
	for i := range page.Items {
		k, ok := core.ReminderKey(&page.Items[i])
		if !ok || v.savedReq[savedKeyStr(k)] {
			continue
		}
		v.savedReq[savedKeyStr(k)] = true
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return
	}
	c := v.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		got, err := c.SavedGet(ctx, keys)
		if err != nil {
			return
		}
		v.notes.mu.Lock()
		v.notes.fns = append(v.notes.fns, func() {
			found := map[string]bool{}
			for _, s := range got {
				k := core.SavedKey{ItemID: s.ItemID, ItemType: s.ItemType, TS: s.TS}
				for _, want := range keys {
					if want.ItemID == k.ItemID && want.ItemType == k.ItemType && (want.TS == "" || want.TS == k.TS) {
						v.saved[savedKeyStr(want)] = s
						found[savedKeyStr(want)] = true
					}
				}
			}
			for _, want := range keys {
				if !found[savedKeyStr(want)] {
					v.savedGone[savedKeyStr(want)] = true
				}
			}
			v.dirty = true
		})
		v.notes.mu.Unlock()
		v.h.Invalidate()
	}()
}

// setSaved replaces a known saved item (after a write) and redraws.
func (v *Activity) setSaved(it *core.ActivityItem, s core.SavedItem) {
	if k, ok := core.ReminderKey(it); ok {
		v.saved[savedKeyStr(k)] = s
		v.dirty = true
	}
}

// ensureFileTitles loads the titles of the files reminder rows name
// (VeYL FileName: the files store's title, nothing until it's known).
func (v *Activity) ensureFileTitles(page *core.ActivityPage) {
	var ids []string
	for i := range page.Items {
		it := &page.Items[i]
		if it.Type != "saved_reminder" || it.ReminderItem != "file" && it.ReminderItem != "canvas_section" || it.ReminderID == "" {
			continue
		}
		if v.fileReq[it.ReminderID] {
			continue
		}
		v.fileReq[it.ReminderID] = true
		ids = append(ids, it.ReminderID)
	}
	if len(ids) == 0 {
		return
	}
	c := v.h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		titles := map[string]string{}
		for _, id := range ids {
			if t, err := c.FileTitle(ctx, id); err == nil && t != "" {
				titles[id] = t
			}
		}
		v.notes.mu.Lock()
		v.notes.fns = append(v.notes.fns, func() {
			for id, t := range titles {
				v.fileTitles[id] = t
			}
			// the footers name the files now: re-describe the rows,
			// keeping their widgets (a press in flight stays theirs)
			for _, r := range v.rows {
				r.built = false
			}
			v.dirty = true
		})
		v.notes.mu.Unlock()
		v.h.Invalidate()
	}()
}

// ---- the status column's marks on read rows ----

// actMark is one of the status column's icons on a read row.
type actMark struct {
	icon, tip string
}

// readMark is ActivityRowContentStatusIndicatorWithResolvedReply after the
// unread badge (inline, as both shipping rows use it): a reminder whose
// saved item isn't incomplete ("You completed this to-do"), a draft in the
// conversation or thread, your reply ("You replied"), your reaction
// ("You reacted:"); none otherwise. Agent status and typing come before
// all of them and the unread badge (agentStatus, typingStatus).
func (v *Activity) readMark(it *core.ActivityItem) (actMark, bool) {
	c := v.h.Core()
	if it.Type == "saved_reminder" {
		// w && !j && !x: the message isn't deleted (tm), the to-do isn't
		// incomplete (E8: also when it isn't loaded yet)
		if it.ReminderItem == "message" && it.Msg != nil && it.Msg.Tombstone {
			return actMark{}, false
		}
		if !v.reminderIncomplete(it) {
			tip := nsActivity.T("You completed this to-do")
			if s, _, _ := v.reminderSaved(it); s != nil && s.DateCompleted > 0 {
				tip += "\n" + nsActivity.T("On {date}", "date", dateSlash(time.Unix(s.DateCompleted, 0)))
			}
			return actMark{"check-filled", tip}, true
		}
	}
	switch it.Type {
	case "thread_v2", "dm", "channel", "bot_dm_bundle":
		if d, ok := c.DraftFor(it.Channel, it.ThreadTS); ok && (strings.TrimSpace(d.Text) != "" || d.Files > 0) {
			tip := nsActivity.T("Draft")
			if t := actTrunc(oneLine(d.Text), 60); t != "" {
				tip = nsActivity.T(`Draft: "{message}"`, "message", t)
			}
			return actMark{"edit-filled", tip}, true
		}
	}
	if ts := c.YouRepliedTS(it); ts != "" {
		tip := nsActivity.T("You replied") // RepliedBadge
		if m, ok := c.S.Find(it.Channel, ts); ok {
			if t := actTrunc(oneLine(m.Text), 60); t != "" {
				tip += "\n“" + t + "”"
			}
		}
		return actMark{"check-filled", tip}, true
	}
	if names := c.YouReacted(it); len(names) > 0 {
		shown := names
		if len(shown) > 3 {
			shown = shown[:3]
		}
		tip := nsActivity.T("You reacted:") // ReactedBadge
		for _, n := range shown {
			tip += " :" + n + ":"
		}
		if more := len(names) - len(shown); more > 0 {
			tip += " " + nsActivity.T("+{count} more", "count", more)
		}
		return actMark{"check-filled", tip}, true
	}
	return actMark{}, false
}

// agentOn reports whether the row shows its agent's status now:
// processing holds until date_status_processing_expire (qYb/
// useProcessingExpiry, the redraw comes then), suspended until it changes.
func (r *actRow) agentOn(gtx C) bool {
	if r.agent == "" {
		return false
	}
	if !r.agentUntil.IsZero() {
		if !gtx.Now.Before(r.agentUntil) {
			return false
		}
		gtx.Execute(op.InvalidateCmd{At: r.agentUntil})
	}
	return true
}

// agentStatus is ActivityAgentStatusIndicator (BZ5B) in the status slot
// (activity_row_content__status_indicators: 24px wide, centred; the
// indicator content-pry): Processing is InfiniteSpinner (medium, 16px,
// slow, blue), Suspended the needs-attention glyph; the tooltip is
// getAgentStatusLabel's (UaSM): "Working", "Needs attention".
func (v *Activity) agentStatus(gtx C, r *actRow) D {
	w, h := gtx.Dp(24), gtx.Dp(20)
	s := gtx.Dp(16)
	if r.agent == core.AgentSuspended {
		s = gtx.Dp(15) // size "inherit": the row's 15px
	}
	func() {
		defer op.Offset(image.Pt((w-s)/2, (h-s)/2)).Push(gtx.Ops).Pop()
		if r.agent == core.AgentProcessing {
			actSpinner(gtx, s, v.p.Link)
		} else {
			g := gtx
			g.Constraints = layout.Exact(image.Pt(s, s))
			v.icon(g, "needs-attention", unit.Dp(float32(s)/gtx.Metric.PxPerDp), v.p.Text)
		}
		area := clip.Rect{Max: image.Pt(s, s)}.Push(gtx.Ops)
		r.agentHov.Add(gtx.Ops)
		area.Pop()
		popups.Tooltip(gtx, v.th, &r.agentTip, r.agentHov.Update(gtx.Source), image.Pt(s, s), popups.Below, core.AgentStatusLabel(r.agent), "")
	}()
	return D{Size: image.Pt(w, h)}
}

// actTrunc is s21a: s cut to n characters with an ellipsis.
func actTrunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// markIcon draws a read-row mark: activity_row_content__status_icon
// --circle --compact (engagement_status_badge_iconCompact alike), a 20px
// circle in plt-gray-10 with the 12px glyph in content-pry, inside the
// 24px-wide status_indicators slot, with its tooltip below (a hover
// target only: a click still reaches the row).
func (v *Activity) markIcon(gtx C, r *actRow) D {
	m := r.mark
	s := gtx.Dp(20)
	w := max(s, gtx.Dp(24))
	c := rgb(0xd7d7da) // plt-gray-10
	if v.th.Dark {
		c = rgb(0x212428)
	}
	func() {
		defer op.Offset(image.Pt((w-s)/2, 0)).Push(gtx.Ops).Pop()
		theme.Circle(gtx, image.Pt(s/2, s/2), s, c)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(s, s))
		layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, icons.Slack(m.icon), 12, v.p.Text) })
		area := clip.Rect{Max: image.Pt(s, s)}.Push(gtx.Ops)
		r.markHov.Add(gtx.Ops)
		area.Pop()
		popups.Tooltip(gtx, v.th, &r.markTipState, r.markHov.Update(gtx.Source), image.Pt(s, s), popups.Below, m.tip, "")
	}()
	return D{Size: image.Pt(w, s)}
}

// ---- reminders: Mark Complete, Reschedule ----

// markComplete is onToggleComplete on the row's saved item (saved.update
// mark completed); the row's mark turns into the completed check at once.
func (v *Activity) markComplete(it *core.ActivityItem) {
	s, _, _ := v.reminderSaved(it)
	if s == nil {
		return
	}
	item, was := *s, *s
	done := item
	done.TodoState, done.DateCompleted, done.DateDue = "completed", time.Now().Unix(), 0
	v.setSaved(it, done)
	v.write("Couldn’t mark the to-do complete: ", func(ctx context.Context, c *core.App) error {
		return c.CompleteSavedItem(ctx, item)
	}, func() { v.setSaved(it, was) })
}

// rescheduleMenu is RemindMenu (ccoC ReminderTimeMenu, 240px, "Set a
// reminder"): In 30 mins / 1 hour / 3 hours ("30 mins"… under "Snooze…"
// when overdue), Tomorrow and Monday at the reminder time pref, Custom…,
// then Clear reminder when the item has a due date.
func (v *Activity) rescheduleMenu(ph popups.Host, it *core.ActivityItem) []popups.MenuItem {
	s, _, _ := v.reminderSaved(it)
	if s == nil {
		return nil
	}
	item := *s
	c := v.h.Core()
	now := time.Now()
	overdue := item.DateDue > 0 && item.DateDue < now.Unix()
	pref := actReminderPref(c)
	set := func(due int64) func() {
		return func() {
			was := item
			nu := item
			nu.DateDue = due
			v.setSaved(it, nu)
			v.write("Couldn’t set the reminder: ", func(ctx context.Context, c *core.App) error {
				return c.SetSavedItemDue(ctx, item, due)
			}, func() { v.setSaved(it, was) })
		}
	}
	var out []popups.MenuItem
	l30, l1, l3 := nsLater.T("In 30 mins"), nsLater.T("In 1 hour"), nsLater.T("In 3 hours")
	if overdue {
		out = append(out, popups.MenuItem{Header: nsLater.T("Snooze…")})
		l30, l1, l3 = nsLater.T("30 mins"), nsLater.T("1 hour"), nsLater.T("3 hours")
	}
	h, m := actReminderClock(c)
	out = append(out,
		popups.MenuItem{Label: l30, Do: set(now.Add(30 * time.Minute).Unix())},
		popups.MenuItem{Label: l1, Do: set(now.Add(time.Hour).Unix())},
		popups.MenuItem{Label: l3, Do: set(now.Add(3 * time.Hour).Unix())},
		popups.MenuItem{Label: nsLater.T("Tomorrow at {reminderPref}", "reminderPref", pref), Do: set(actDueTomorrow(now, h, m).Unix())},
		popups.MenuItem{Label: nsLater.T("Monday at {reminderPref}", "reminderPref", pref), Do: set(actDueMonday(now, h, m).Unix())},
		popups.MenuItem{Label: nsLater.T("Custom…"), Do: func() {
			ph.ShowOverlay(popups.NewReminderDueModal(ph, func(due int64) { set(due)() }))
		}},
	)
	if item.DateDue > 0 {
		out = append(out, popups.Sep(), popups.MenuItem{Label: nsLater.T("Clear reminder"), Do: set(0)})
	}
	return out
}

// actReminderClock is calculateDueDateTs' hour and minute: 9:00, the DND
// end when DND is on and ends after 9:00, the reminder_notification_time
// pref when set.
func actReminderClock(c *core.App) (h, m int) {
	h, m = 9, 0
	if c.PrefBool("dnd_enabled", false) {
		if eh, em, ok := actClock(c.PrefString("dnd_end_hour")); ok && eh*60+em >= 9*60 {
			h, m = eh, em
		}
	}
	if ph, pm, ok := actClock(c.PrefString("reminder_notification_time")); ok {
		h, m = ph, pm
	}
	return h, m
}

func actClock(s string) (h, m int, ok bool) {
	hs, ms, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(hs)
	m, err2 := strconv.Atoi(ms)
	return h, m, err1 == nil && err2 == nil && h >= 0 && h < 24 && m >= 0 && m < 60
}

// actReminderPref is ReminderTimeMenu's {reminderPref}: the reminder time
// pref (default 9:00) as {time}, 24-hour with the time24 pref.
func actReminderPref(c *core.App) string {
	h, m := 9, 0
	if ph, pm, ok := actClock(c.PrefString("reminder_notification_time")); ok {
		h, m = ph, pm
	}
	t := time.Date(2000, 1, 1, h, m, 0, 0, time.Local)
	if l := i18n.Locale(); l != i18n.EnUS {
		return i18n.Time(l, t, c.PrefBool("time24", false), false) // toTime
	}
	if c.PrefBool("time24", false) {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}

// actDueTomorrow and actDueMonday are calculateDueDateTs' Tomorrow (the
// next day at h:m) and Monday (the next ISO week's Monday at h:m).
func actDueTomorrow(now time.Time, h, m int) time.Time {
	y, mo, d := now.Date()
	return time.Date(y, mo, d+1, h, m, 0, 0, now.Location())
}

func actDueMonday(now time.Time, h, m int) time.Time {
	wd := (int(now.Weekday()) + 6) % 7 // Monday 0
	y, mo, d := now.Date()
	return time.Date(y, mo, d-wd+7, h, m, 0, 0, now.Location())
}

// markCompleteButton is ActivityItemMarkCompleteButton in Detailed: a
// small button (28px, 13px bold, padding 0 12px, min-width 56px) with the
// check icon then "Mark Complete", 12px under the reminder's body.
func (v *Activity) markCompleteButton(gtx C, r *actRow) D {
	return layout.Inset{Top: 12}.Layout(gtx, func(gtx C) D {
		return v.smallButton(gtx, &r.complete, btnOutline, "check", nsActivity.T("Mark Complete"))
	})
}

// smallButton is c-button--small: 28px tall, padding 0 12px, min-width
// 56px, radius 8, a 13px bold label after an optional 16px icon (4px gap,
// margin_right_25).
func (v *Activity) smallButton(gtx C, c *widget.Clickable, kind btnKind, icon, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		tw := v.th.Measure(gtx, 13, wBold, label)
		iw := 0
		if icon != "" {
			iw = gtx.Dp(16) + gtx.Dp(4)
		}
		sz := image.Pt(max(gtx.Dp(56), tw+iw+2*gtx.Dp(12)), gtx.Dp(28))
		rr := image.Rectangle{Max: sz}
		rad := gtx.Dp(8)
		fg := v.p.Text
		switch kind {
		case btnPrimary:
			bg := v.p.Green
			if c.Hovered() {
				bg = v.p.GreenHover
			}
			rrect(gtx, rr, rad, bg)
			fg = rgb(0xffffff)
		default:
			rrect(gtx, rr, rad, v.p.Bg)
			if c.Hovered() {
				rrect(gtx, rr, rad, v.p.Hover)
			}
			border(gtx, rr, rad, max(1, gtx.Dp(1)), v.p.BorderStr)
		}
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					if icon == "" {
						return D{}
					}
					return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, icons.Slack(icon), 16, fg) })
				}),
				layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, fg, label) }),
			)
		})
		return D{Size: sz}
	})
}

// ---- Slack Connect invitations: Ignore, Accept ----

// inviteActions is FI's text actions for the row's invite, as the row
// shows them (Accept for a channel invite opens Slack's multi-step accept
// modal, which Clack doesn't have: left out).
func (v *Activity) inviteActions(it *core.ActivityItem) []string {
	if !actExternal(it.Type) || it.Invite == nil {
		return nil
	}
	return it.Invite.InviteActions(v.selfID(), time.Now())
}

// inviteButtons is SlackConnectInviteActions' text actions
// (p-slack_connect_invite_actions__text_actions: margin-left 4px, 4px
// between): Ignore (small outline), Accept (small primary).
func (v *Activity) inviteButtons(gtx C, r *actRow, acts []string) D {
	ch := []layout.FlexChild{layout.Rigid(func(gtx C) D { return D{Size: image.Pt(gtx.Dp(4), 0)} })}
	for i, a := range acts {
		i, a := i, a
		if i > 0 {
			ch = append(ch, hgap(4))
		}
		ch = append(ch, layout.Rigid(func(gtx C) D {
			if a == core.InviteActIgnore {
				return v.smallButton(gtx, &r.invC[i], btnOutline, "", nsSlackConnect.T("Ignore"))
			}
			return v.smallButton(gtx, &r.invC[i], btnPrimary, "", nsSlackConnect.T("Accept"))
		}))
	}
	return row(gtx, layout.Middle, ch...)
}

// inviteClicked runs the invite button i of the row.
func (v *Activity) inviteClicked(it *core.ActivityItem, act string) {
	ph, ok := v.h.(popups.Host)
	if !ok || it.Invite == nil {
		return
	}
	switch act {
	case core.InviteActIgnore:
		v.ignoreForked(ph, *it)
	case core.InviteActAcceptDM:
		v.acceptDM(ph, *it)
	case core.InviteActAcceptChannel:
		v.acceptChannel(ph, *it)
	}
}

// inviterName is the Ignore modals' {imMember} (forceRealName).
func (v *Activity) inviterName(it core.ActivityItem) string {
	if u, ok := v.user(it.Actor); ok && u.RealName != "" {
		return u.RealName
	}
	return firstNonEmpty(it.Invite.InviterName, it.ActorName, v.userName(it.Actor))
}

// ignoreForked is IgnoreForkedModal ("What would you like to do?"): two
// IgnoreForkedModalButton rows, Ignore this invitation (times-circle) and
// Block invitations from this person (disable), each opening its modal.
func (v *Activity) ignoreForked(ph popups.Host, it core.ActivityItem) {
	name := v.inviterName(it)
	var m *popups.Modal
	var rows [2]widget.Clickable
	choices := []struct {
		icon, text, sub string
		next            func()
	}{
		{"times-circle-medium", nsIntro.T("Ignore this invitation"),
			nsIntro.T("This invite simply disappears, as if nothing ever happened. But {imMember} can reach out again.", "imMember", name),
			func() { v.ignoreThis(ph, it) }},
		{"disable-font", nsIntro.T("Block invitations from this person"),
			nsIntro.T("You’ll no longer receive Slack Connect invitations sent from {imMember}.", "imMember", name),
			func() { v.ignoreAll(ph, it) }},
	}
	for i := range choices {
		next := choices[i].next
		v.forked[i] = func() {
			if m != nil {
				m.Dismiss()
			}
			next()
		}
	}
	m = popups.NewModal(ph, popups.ModalOpts{
		Title: nsIntro.T("What would you like to do?"),
		Body: func(gtx C) D {
			for i := range rows {
				if rows[i].Clicked(gtx) {
					v.forked[i]()
				}
			}
			// p-ignore_forked_modal__content: padding 0 14px 24px, the
			// rows' 4px padding-top
			return layout.Inset{Top: 4, Bottom: 24}.Layout(gtx, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D { return v.forkedRow(gtx, &rows[0], choices[0].icon, choices[0].text, choices[0].sub) }),
					layout.Rigid(func(gtx C) D { return v.forkedRow(gtx, &rows[1], choices[1].icon, choices[1].text, choices[1].sub) }),
				)
			})
		},
	})
	ph.ShowOverlay(m)
}

// forkedRow is IgnoreForkedModalButton: padding 12px 14px, radius 4px, a
// 36px round icon well (foreground-min .04), the bold text over its
// subtext (padding 0 16px), a chevron at the end.
func (v *Activity) forkedRow(gtx C, c *widget.Clickable, icon, text, sub string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		m := op.Record(gtx.Ops)
		d := layout.Inset{Top: 12, Bottom: 12, Left: 14, Right: 14}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					s := gtx.Dp(36)
					theme.Circle(gtx, image.Pt(s/2, s/2), s, theme4(v, 0x0a))
					g := gtx
					g.Constraints = layout.Exact(image.Pt(s, s))
					return layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, icon, 20, v.p.Sec) })
				}),
				layout.Flexed(1, func(gtx C) D {
					return layout.Inset{Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
						return col(gtx,
							layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, wBold, v.p.Text, 0, text) }),
							layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, wReg, v.p.Sec, 0, sub) }),
						)
					})
				}),
				layout.Rigid(func(gtx C) D { return v.icon(gtx, "chevron-medium-right", 20, v.p.Sec) }),
			)
		})
		call := m.Stop()
		if c.Hovered() {
			rrect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(4), theme4(v, 0x0a))
		}
		call.Add(gtx.Ops)
		return d
	})
}

// theme4 is foreground-min at alpha a over the page (rgba(29,28,29,a)).
func theme4(v *Activity, a uint8) color.NRGBA {
	c := v.p.Text
	c.A = a
	return c
}

// ignoreThis is IgnoreThisModal: "Ignore this invitation from {name}?",
// "{name} ({email}) won’t be notified you’ve done this." (plus the search
// hint for a DM introduction or channel invite), Cancel / Ignore Invitation
// (danger) → conversations.ignoreInvite or sharedInvites.ignoreInvite.
func (v *Activity) ignoreThis(ph popups.Host, it core.ActivityItem) {
	name := v.inviterName(it)
	email := ""
	if it.Invite.InviterEmail != "" {
		email = " (" + it.Invite.InviterEmail + ")"
	}
	channel := it.Invite.Kind == "channel"
	body := nsIntro.T("{imMember}{maybeImMemberEmail} won’t be notified you’ve done this.", "imMember", name, "maybeImMemberEmail", email)
	if channel {
		body = nsIntro.T("{imMember}{maybeImMemberEmail} won’t be notified you’ve done this. If you change your mind, you can search for their name to start the conversation.", "imMember", name, "maybeImMemberEmail", email)
	}
	id, key := it.Invite.ID, actKey(&it)
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: nsIntro.T("Ignore this invitation from {imMember}?", "imMember", name),
		Body:  func(gtx C) D { return v.para(gtx, 15, 22, wReg, v.p.Text, 0, body) },
		Buttons: []*popups.ModalButton{
			{Label: nsIntro.T("Cancel"), Kind: popups.BtnOutline},
			{Label: nsIntro.T("Ignore Invitation"), Kind: popups.BtnDanger, Do: func() bool {
				v.write("Hmm, something went wrong. Try again? ", func(ctx context.Context, c *core.App) error {
					if err := c.IgnoreConnectInvite(ctx, id, channel); err != nil {
						return err
					}
					v.load(actRefresh)
					return nil
				}, func() {})
				_ = key
				return true
			}},
		},
	}))
}

// ignoreAll is IgnoreAllModal: "Block invitations from {name}?", Cancel /
// Block Invitations (danger) → conversations.ignoreUser or
// sharedInvites.ignoreUser, then IgnoreAllSuccessModal ("You won’t hear
// from {name} again", Got It).
func (v *Activity) ignoreAll(ph popups.Host, it core.ActivityItem) {
	name := v.inviterName(it)
	email := ""
	if it.Invite.InviterEmail != "" {
		email = " (" + it.Invite.InviterEmail + ")"
	}
	channel := it.Invite.Kind == "channel"
	id := it.Invite.ID
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: nsIntro.T("Block invitations from {imMember}?", "imMember", name),
		Body: func(gtx C) D {
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return v.para(gtx, 15, 22, wReg, v.p.Text, 0, nsIntro.T("You’ll no longer receive Slack Connect invitations from {imMember}{maybeImMemberEmail}.", "imMember", name, "maybeImMemberEmail", email))
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
						return v.para(gtx, 15, 22, wReg, v.p.Text, 0, nsIntro.T("They won’t be notified you’ve done this."))
					})
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
						return v.para(gtx, 15, 22, wReg, v.p.Text, 0, i18n.StripTags(nsIntro.T("To undo this, you can adjust the setting under <strong>privacy & visibility</strong> in your preferences.")))
					})
				}),
			)
		},
		Buttons: []*popups.ModalButton{
			{Label: nsIntro.T("Cancel"), Kind: popups.BtnOutline},
			{Label: nsIntro.T("Block Invitations"), Kind: popups.BtnDanger, Do: func() bool {
				v.write("Hmm, something went wrong. Try again? ", func(ctx context.Context, c *core.App) error {
					if err := c.BlockConnectInvites(ctx, id, channel); err != nil {
						return err
					}
					v.notes.mu.Lock()
					v.notes.fns = append(v.notes.fns, func() {
						ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
							Title: nsIntro.T("You won\u2019t hear from {imMember} again", "imMember", name),
							Body: func(gtx C) D {
								return v.para(gtx, 15, 22, wReg, v.p.Text, 0, i18n.StripTags(nsIntro.T("If you change your mind, you can adjust the setting under <strong>privacy & visibility</strong> in your preferences.")))
							},
							Buttons: []*popups.ModalButton{{Label: nsIntro.T("Got It"), Kind: popups.BtnOutline}},
						}))
					})
					v.notes.mu.Unlock()
					v.load(actRefresh)
					return nil
				}, func() {})
				return true
			}},
		},
	}))
}

// acceptDM is SlackConnectAcceptDmInviteAction: sharedInvites.canAccept
// {sig}; eligible and from a verified organisation, sharedInvites.accept
// and the DM with the inviter opens; eligible otherwise, the trust modal;
// not eligible, "Hmm, something went wrong" (hasConnectInviteAcceptanceFailed).
func (v *Activity) acceptDM(ph popups.Host, it core.ActivityItem) {
	inv := *it.Invite
	inviter := it.Actor
	v.write("Hmm, something went wrong. Try again? ", func(ctx context.Context, c *core.App) error {
		ok, err := c.ConnectDMInviteEligible(ctx, inv.Sig)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("this invitation can’t be accepted")
		}
		if !inv.TeamVerified {
			v.notes.mu.Lock()
			v.notes.fns = append(v.notes.fns, func() { v.trustModal(ph, it) })
			v.notes.mu.Unlock()
			v.h.Invalidate()
			return nil
		}
		if err := c.AcceptConnectDMInvite(ctx, inv.Sig, inv.ID); err != nil {
			return err
		}
		v.openDMWith(inviter)
		return nil
	}, func() {})
}

// openDMWith opens your DM with user (Y1Qm openDmWithMember,
// shouldNavigateToChannel: conversations.open). Called off the UI
// goroutine.
func (v *Activity) openDMWith(user string) {
	c := v.h.Core()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conv, err := c.OpenDM(ctx, user)
	v.notes.mu.Lock()
	v.notes.fns = append(v.notes.fns, func() {
		if err == nil && conv.ID != "" {
			v.h.Navigate("c/" + conv.ID)
		}
		v.load(actRefresh)
	})
	v.notes.mu.Unlock()
	v.h.Invalidate()
}

// trustModal is SlackConnectConfirmTrustModal: "Before you accept this
// invitation…" ("This invitation is from a new account" for a suspicious
// one isn't in the feed's invite), "Make sure you recognize the sender’s
// email address and other info.", the inviter's photo, "{name} from
// {team}.", the email, "This organization has used Slack since {MMM
// YYYY}", Ignore (the forked modal) / Accept Invitation.
func (v *Activity) trustModal(ph popups.Host, it core.ActivityItem) {
	inv := *it.Invite
	name := v.inviterName(it)
	av := ""
	if u, ok := v.user(it.Actor); ok {
		av = u.Avatar
	}
	inviter := it.Actor
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: nsSharedInvites.T("Before you accept this invitation…"), // SlackConnectConfirmTrustModal
		Body: func(gtx C) D {
			ln := func(s string, size, lh unit.Sp, w font.Weight, top unit.Dp) layout.FlexChild {
				return layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: top}.Layout(gtx, func(gtx C) D {
						return layout.Center.Layout(gtx, func(gtx C) D { return v.para(gtx, size, lh, w, v.p.Text, 0, s) })
					})
				})
			}
			ch := []layout.FlexChild{
				ln(nsSharedInvites.T("Make sure you recognize the sender’s email address and other info."), 15, 22, wReg, 0),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 24}.Layout(gtx, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return layout.Center.Layout(gtx, func(gtx C) D { return v.avatar(gtx, av, 50) })
					})
				}),
				ln(i18n.StripTags(nsSharedInvites.T("<strong>{invitingUserName}</strong> from {invitingTeamName}.", "invitingUserName", name, "invitingTeamName", it.Team)), 15, 22, wReg, 8),
			}
			if inv.InviterEmail != "" {
				ch = append(ch, ln(inv.InviterEmail, 13, 18, wReg, 0))
			}
			if inv.TeamCreated > 0 {
				ch = append(ch, ln(nsSharedInvites.T("This organization has used Slack since {teamDuration}", "teamDuration", monthYear(time.Unix(inv.TeamCreated, 0))), 15, 22, wReg, 4))
			}
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return col(gtx, ch...) })
		},
		Buttons: []*popups.ModalButton{
			{Label: nsSharedInvites.T("Ignore"), Kind: popups.BtnOutline, Do: func() bool {
				v.ignoreForked(ph, it)
				return true
			}},
			{Label: nsSharedInvites.T("Accept Invitation"), Kind: popups.BtnPrimary, Do: func() bool {
				v.write("Hmm, something went wrong. Try again? ", func(ctx context.Context, c *core.App) error {
					if err := c.AcceptConnectDMInvite(ctx, inv.Sig, inv.ID); err != nil {
						return err
					}
					v.openDMWith(inviter)
					return nil
				}, func() {})
				return true
			}},
		},
	}))
}

// ---- "Notifications cleared." ----

// actClearToast is ActivityClearAllToast (ActivityConfirmationToast,
// bottom-right): "Notifications cleared. Undo" until Undo, then "We brought
// back your notifications", or "There was an issue clearing some
// notifications" when the undo fails.
type actClearToast struct {
	key   string
	text  string // after Undo: the outcome
	until time.Time
	undo  widget.Clickable
}

// actToastLife is how long the confirmation toast stays (c5oy's default
// duration).
const actToastLife = 5 * time.Second

func (v *Activity) clearAllCompleted(undoKey string) {
	v.clearAll = actClearToast{key: undoKey, until: time.Now().Add(actToastLife)}
}

// clearAllToast draws the toast at the list's bottom right.
func (v *Activity) clearAllToast(gtx C) {
	t := &v.clearAll
	if t.until.IsZero() {
		return
	}
	if !gtx.Now.Before(t.until) {
		*t = actClearToast{}
		return
	}
	gtx.Execute(op.InvalidateCmd{At: t.until})
	if t.text == "" && t.undo.Clicked(gtx) {
		key := t.key
		t.text, t.until = nsActivity.T("We brought back your notifications"), gtx.Now.Add(actToastLife)
		v.write("", func(ctx context.Context, c *core.App) error {
			err := c.UndoClearAll(ctx, key)
			v.notes.mu.Lock()
			v.notes.fns = append(v.notes.fns, func() {
				if err != nil {
					v.clearAll.text = nsActivity.T("There was an issue clearing some notifications")
				}
				v.load(actRefresh)
			})
			v.notes.mu.Unlock()
			v.h.Invalidate()
			return nil
		}, func() {})
	}
	bg, fg, link := rgb(0x1d1c1d), rgb(0xffffff), rgb(0x1d9bd1)
	if v.th.Dark {
		bg, fg, link = rgb(0x212428), rgb(0xf8f8f8), rgb(0x2ba5ce)
	}
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	d := layout.Inset{Top: 10, Bottom: 10, Left: 16, Right: 16}.Layout(g, func(gtx C) D {
		if t.text != "" {
			return v.label(gtx, 15, wReg, fg, t.text)
		}
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wReg, fg, nsActivity.T("Notifications cleared.")+" ") }),
			layout.Rigid(func(gtx C) D {
				return t.undo.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					dd := v.label(gtx, 15, wReg, link, nsActivity.T("Undo"))
					rrect(gtx, image.Rect(0, dd.Size.Y-gtx.Dp(2), dd.Size.X, dd.Size.Y-gtx.Dp(1)), 0, link) // underline
					return dd
				})
			}),
		)
	})
	call := m.Stop()
	size := gtx.Constraints.Max
	at := image.Pt(max(0, size.X-d.Size.X-gtx.Dp(24)), size.Y-d.Size.Y-gtx.Dp(24))
	defer op.Offset(at).Push(gtx.Ops).Pop()
	r := image.Rectangle{Max: d.Size}
	rrect(gtx, r, gtx.Dp(8), bg)
	call.Add(gtx.Ops)
}

// ---- offline ----

// actOffline reports whether err is the network being unreachable: the
// signal Clack has for Slack's clientOnline being false.
func actOffline(err error) bool {
	var ne net.Error
	var ue *url.Error
	var oe *net.OpError
	var de *net.DNSError
	return errors.As(err, &oe) || errors.As(err, &de) || errors.As(err, &ue) && errors.As(ue.Err, &ne)
}

// offlineFooter is ActivityRowLoader offline: InlineOfflineIndicator
// (96px, centred, 13px: the cloud-offline icon, "No connection." in bold,
// "Go online to load more activity."), margin 0 16px.
func (v *Activity) offlineFooter(gtx C) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	g := gtx
	g.Constraints.Min.Y, g.Constraints.Max.Y = gtx.Dp(96), gtx.Dp(96)
	return layout.Inset{Left: 16, Right: 16}.Layout(g, func(gtx C) D {
		return layout.Center.Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.icon(gtx, "cloud-offline", 13, v.p.Text) }),
				layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Text, " ") }),
				layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, v.p.Text, i18n.T("offline_mode", "No connection.")) }), // InlineOfflineIndicator
				layout.Rigid(func(gtx C) D {
					return v.label(gtx, 13, wReg, v.p.Text, " "+nsMentions.T("Go online to load more activity."))
				}), // ActivityRowLoader
			)
		})
	})
}

// teamIcon is BaseInviteSenderName's TeamIcon (16px, margin_right_25):
// the inviting organisation's icon after the inviter's name.
func (v *Activity) teamIcon(gtx C, d *actDesc) D {
	if d.teamIcon == "" {
		return D{}
	}
	return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D { return v.avatar(gtx, d.teamIcon, 16) })
}

// ---- the typing indicator in the status column ----

// actTypingTTL is removeOldTypingRecords' 6 s for other people (the
// composer's typing line keeps the same, composer/typing.go).
const actTypingTTL = 6 * time.Second

// actTyping is the typing store the status column reads (HCiS): who is
// typing in each conversation or thread, until when. Realtime user_typing
// events add a record (never your own: memberIsSelf), a message from that
// person ends it.
type actTyping struct {
	mu sync.Mutex
	m  map[string]map[string]time.Time // channel + "/" + thread ts → user → until
}

// apply takes a typing or message event; it reports a change.
func (t *actTyping) apply(ev core.Event, self string) bool {
	k := ev.Channel + "/" + ev.ThreadTS
	t.mu.Lock()
	defer t.mu.Unlock()
	switch ev.Type {
	case "typing":
		if ev.UserID == "" || ev.UserID == self || ev.Channel == "" {
			return false
		}
		if t.m == nil {
			t.m = map[string]map[string]time.Time{}
		}
		us := t.m[k]
		if us == nil {
			us = map[string]time.Time{}
			t.m[k] = us
		}
		us[ev.UserID] = time.Now().Add(actTypingTTL)
		return true
	case "message":
		uid := ev.UserID
		if uid == "" && ev.Msg != nil {
			uid = ev.Msg.User
		}
		if us := t.m[k]; us != nil {
			if _, ok := us[uid]; ok {
				delete(us, uid)
				return true
			}
		}
	}
	return false
}

// on is HCiS rg (thread "": someone typing in the channel itself) and Oc
// (someone typing in that thread) at now, with the soonest expiry.
func (t *actTyping) on(ch, thread string, now time.Time) (bool, time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var next time.Time
	for _, until := range t.m[ch+"/"+thread] {
		if until.After(now) && (next.IsZero() || until.Before(next)) {
			next = until
		}
	}
	return !next.IsZero(), next
}

// typingOn is rZx3 z8: a DM or a channel post while someone types in the
// conversation (not in a thread), a thread while someone types in it. (Slack
// also needs the thread's subscription not to be off, which Clack's store
// doesn't keep apart from "unknown", and the agent command center's
// assistant status, which Clack doesn't have.) While it shows, the dots
// animate: the next frame is asked for, and the record's expiry ends it.
func (v *Activity) typingOn(gtx C, it *core.ActivityItem) bool {
	var ch, thread string
	switch it.Type {
	case "dm", "channel":
		ch = it.Channel
	case "thread_v2":
		ch, thread = it.Channel, it.ThreadTS
		if thread == "" {
			return false
		}
	default:
		return false
	}
	if ch == "" {
		return false
	}
	on, until := v.typing.on(ch, thread, gtx.Now)
	if on {
		next := gtx.Now.Add(actTypingFrame)
		if until.Before(next) {
			next = until
		}
		gtx.Execute(op.InvalidateCmd{At: next})
	}
	return on
}

// actTypingFrame paces the dots' animation (the SVG's opacity ramps).
const actTypingFrame = 33 * time.Millisecond

// typingStatus is the status slot's typing state
// (ActivityRowContentStatusIndicatorWithResolvedReply's `if (_)`): +SIc
// BaseChannelPrefixTyping (the three animated dots, its className
// activity_row_content__typing_status: 15px, content-sec) in the inline,
// standalone slot (20px tall, at least 20px wide), the tooltip "Typing…"
// below.
func (v *Activity) typingStatus(gtx C, r *actRow) D {
	w, h := gtx.Dp(20), gtx.Dp(20)
	s := gtx.Dp(15)
	func() {
		defer op.Offset(image.Pt((w-s)/2, (h-s)/2)).Push(gtx.Ops).Pop()
		actTypingDots(gtx, s, v.p.Sec, v.p.Bg)
		area := clip.Rect{Max: image.Pt(s, s)}.Push(gtx.Ops)
		r.typeHov.Add(gtx.Ops)
		area.Pop()
		popups.Tooltip(gtx, v.th, &r.typeTip, r.typeHov.Update(gtx.Source), image.Pt(s, s), popups.Below, nsActivity.T("Typing…"), "")
	}()
	return D{Size: image.Pt(w, h)}
}

// actTypingDots draws +SIc's SVG at s px: in a 24-unit box, circles r 3 at
// x 4, 12 and 20 (y 12), resting at opacity 1, .4 and .3; each animates
// opacity 1 → .2 over .75 s, the second .15 s and the third .3 s after the
// first, which starts again .25 s before the third ends (a .8 s cycle).
// The fill is c at that opacity over bg, blended the way the browser
// does.
func actTypingDots(gtx C, s int, c, bg color.NRGBA) {
	t := float64(gtx.Now.UnixMilli()%800) / 1000
	k := float32(s) / 24
	for i := range 3 {
		a := actTypingOpacity(t, i)
		col := theme.Over(theme.WithAlpha(c, uint8(a*255+0.5)), bg)
		cx := (4 + 8*float32(i)) * k
		rect := image.Rect(int(cx-3*k+0.5), int(9*k+0.5), int(cx+3*k+0.5), int(15*k+0.5))
		theme.Circle(gtx, rect.Min.Add(rect.Max).Div(2), rect.Dx(), col)
	}
}

// actTypingOpacity is dot i's opacity t seconds into the .8 s cycle.
func actTypingOpacity(t float64, i int) float64 {
	if p := math.Mod(t-0.15*float64(i)+0.8, 0.8); p < 0.75 {
		return 1 - 0.8*p/0.75
	}
	return [3]float64{1, .4, .3}[i]
}

// ---- reactions under a reaction row (entry_extras) ----

// showsReactions is rZx3 GY: a reaction entry (getActivityIconType
// "reaction") whose message has reactions.
func (v *Activity) showsReactions(r *actRow) bool {
	if r.it.Type != "message_reaction" {
		return false
	}
	m := r.msg()
	return m != nil && len(m.Reactions) > 0
}

// entryExtras is ActivityRowContentMultiLine's entry_extras (a column,
// 12px between its children): SKIQ ActivityRowContentReactions (the
// message's MessageReactionBar, every reaction shown, the add button
// hidden: p-activity_ia4_page__item__reactions_bar margin-top 8px,
// --add_button_hidden; the multi-line row's margin-bottom -4px), then the
// Slack Connect invite's text actions (Qt0R ActivityRowContentInviteActions).
func (v *Activity) entryExtras(gtx C, r *actRow) D {
	var kids []layout.FlexChild
	if v.showsReactions(r) {
		m := r.msg()
		kids = append(kids, layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = 0
			d := layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return v.actReactionsBar(gtx, m, true) })
			d.Size.Y = max(0, d.Size.Y-gtx.Dp(4))
			return d
		}))
	}
	if acts := v.inviteActions(r.it); len(acts) > 0 {
		if len(kids) > 0 {
			kids = append(kids, layout.Rigid(func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(12))} }))
		}
		kids = append(kids, layout.Rigid(func(gtx C) D {
			// the text actions' ul is align-self: center
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.N.Layout(gtx, func(gtx C) D { return v.inviteButtons(gtx, r, acts) })
		}))
	}
	if len(kids) == 0 {
		return D{}
	}
	return col(gtx, kids...)
}

// ---- the subtext run inline ----

// previewSub is ActivityMessage (/ad9) with a subtext: the
// p-activity_ia4_page__item__activity_subtext span ("You: ", "Ana: ")
// sits inside the line-clamped message, before its text, so a second line
// starts under the subtext, not under the message.
func (r *actRow) previewSub(gtx C, lines int, sub string) D {
	st := actMsgClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	draw := func(gtx C) D { return r.v.msgBodySub(gtx, r.msg(), !r.unread, lines, sub) }
	if !st.Active() {
		return draw(gtx)
	}
	return st.Wrap(gtx, draw)
}

// msgBodySub is an Activity row's ActivityMessage: Slack's
// SimpleMessagePreview of the message (core.PreviewDoc: its section,
// header and rich_text blocks, else its text; never a context line,
// Block Kit, files or "(edited)"), with sub run into it (actSubtextDoc).
// Read rows draw it in the secondary colour. Cached apart from the plain
// body (the same message can be another row's body without a subtext).
func (b *base) msgBodySub(gtx C, m *core.Message, muted bool, maxLines int, sub string) D {
	if m == nil {
		return D{}
	}
	orig := m
	m = b.viewMsg(m)
	doc := b.actPreviewDoc(m)
	// the row's colour: content-pry unread, content-sec read (the message
	// inherits it; richview's palette Text is the message list's)
	col := b.p.Text
	if muted {
		col = b.p.Sec
	}
	if len(doc.Blocks) == 0 {
		t := richtext.PlainText(doc)
		if strings.TrimSpace(t) == "" {
			t = sub + m.Text
		}
		return b.para(gtx, 15, 22, wReg, col, maxLines, t)
	}
	key := "sub:" + sub + "\x00" + m.Channel + "/" + m.TS
	ver := m.EditedTS + ":" + strconv.Itoa(len(m.Text)) + ":" + strconv.Itoa(len(m.Blocks)) + ":" + strconv.Itoa(len(doc.Blocks))
	if m != orig {
		ver += ":p" + strconv.FormatUint(uint64(textHash(m.Text)), 36)
	}
	o := richview.Options{Muted: m.System, Secondary: muted && !m.System, Channel: m.Channel, TS: m.TS, Truncate: true}
	if !m.System {
		o.Color = col
	}
	return b.richDoc(gtx, key, ver, actSubtextDoc(doc, sub), o, maxLines)
}

// actPreviewDoc is m's preview document (core.PreviewDoc), m.Doc when
// Slack's preview isn't made from its blocks. Kept per message until the
// message changes.
func (b *base) actPreviewDoc(m *core.Message) richtext.Doc {
	c := b.h.Core()
	if c == nil || len(m.Blocks) == 0 {
		return m.Doc
	}
	ver := m.EditedTS + ":" + strconv.Itoa(len(m.Text)) + ":" + strconv.Itoa(len(m.Blocks)) + ":" +
		strconv.FormatUint(uint64(textHash(string(m.Blocks))), 36)
	k := m.Channel + "/" + m.TS
	if e, ok := b.pv[k]; ok && e.ver == ver {
		return e.doc
	}
	doc, ok := c.PreviewDoc(m)
	if !ok {
		doc = m.Doc
	}
	if b.pv == nil || len(b.pv) > 2000 {
		b.pv = map[string]pvEntry{}
	}
	b.pv[k] = pvEntry{ver: ver, doc: doc}
	return doc
}

// pvEntry is one cached preview document.
type pvEntry struct {
	ver string
	doc richtext.Doc
}

// actSubtextDoc is doc with sub before its text: inline at the start of a
// first paragraph (the span and the rich text section share the line
// box); before any other first block (a list, a quote, a code block, an
// attachment) it's a line of its own, as an inline span before a block
// element is in the browser.
func actSubtextDoc(doc richtext.Doc, sub string) richtext.Doc {
	if sub == "" {
		return doc
	}
	lead := richtext.Span{Kind: richtext.Text, Text: sub}
	out := doc
	out.Blocks = make([]richtext.Block, 0, len(doc.Blocks)+1)
	if len(doc.Blocks) > 0 && doc.Blocks[0].Kind == richtext.Paragraph {
		first := doc.Blocks[0]
		first.Spans = append([]richtext.Span{lead}, first.Spans...)
		out.Blocks = append(append(out.Blocks, first), doc.Blocks[1:]...)
		return out
	}
	out.Blocks = append(append(out.Blocks, richtext.Block{Kind: richtext.Paragraph, Spans: []richtext.Span{lead}}), doc.Blocks...)
	return out
}

// ---- the sticky section headers ----

// actStickyHdr is the VIP sort's sticky section header (U5s4 ActivityList's
// stickyItems e5, with layout revisions: every "non_collapsible_header",
// i.e. "VIP unreads" and "Everything else"; RIad BaseStickyList): the
// header of the section the list's top is in stays at the top
// (c-virtual_list__item--sticky, z-index 199) until the next header
// pushes it up (its sticky container ends where the next one starts).
// While stuck it shows oDbF's stuck shadow
// (non-collapsible__header_container--stuck:after, bordered separators:
// 0 2px 4px #0000000a under the 1px otl-ter line). A row under it hides
// its hover actions (useHideActionsBehindStickyHeader: the row's top
// above the header's bottom less 2px).
type actStickyHdr struct {
	h    map[int]int // this frame's laid-out heights by list index
	tops map[int]int // the last frame's tops of the visible items
	on   bool
	y    int // the stuck header's top in the list (0, or less when pushed)
	vip  bool
	hh   int

	scroll gesture.Scroll
	tag    struct{} // the header's own hit area: rows under it get no clicks
}

// actStickyH is --activity-section-header-height.
const actStickyH unit.Dp = 42

func (s *actStickyHdr) begin() {
	if s.h == nil {
		s.h = map[int]int{}
	}
	clear(s.h)
}

func (v *Activity) isHeader(i int) bool {
	return i >= 0 && i < len(v.vis) && (v.vis[i] == actVisVIP || v.vis[i] == actVisElse)
}

// stickyHeader draws the stuck header over the list (size is the list's)
// after the list laid out this frame.
func (v *Activity) stickyHeader(gtx C, size image.Point) {
	s := &v.stk
	s.on = false
	pos := v.list.Position
	tops := map[int]int{}
	y := -pos.Offset
	for i := pos.First; i < pos.First+pos.Count; i++ {
		tops[i] = y
		y += s.h[i]
	}
	s.tops = tops
	if !v.h.Core().ActivityLayoutRevisions() {
		return // e5: no sticky items without the revisions
	}
	hdr := -1
	for i := min(pos.First, len(v.vis)-1); i >= 0; i-- {
		if v.isHeader(i) {
			hdr = i
			break
		}
	}
	if hdr < 0 || hdr == pos.First && pos.Offset <= 0 {
		return // in its place
	}
	s.hh = gtx.Dp(actStickyH)
	s.y = 0
	for i := hdr + 1; i < pos.First+pos.Count; i++ {
		if v.isHeader(i) {
			if t := tops[i]; t < s.hh {
				s.y = t - s.hh
			}
			break
		}
	}
	if s.y <= -s.hh {
		return
	}
	s.on, s.vip = true, v.vis[hdr] == actVisVIP
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	defer op.Offset(image.Pt(0, s.y)).Push(gtx.Ops).Pop()
	r := image.Rect(0, 0, size.X, s.hh)
	// the stuck shadow below the header
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(2))), 0, gtx.Dp(4), argb(0x0a000000))
	g := gtx
	g.Constraints = layout.Exact(image.Pt(size.X, s.hh))
	v.sectionHeader(g, s.vip)
	// its hit area: the rows under it don't take presses, and a wheel
	// over it still scrolls the list (gesture.Scroll, as the list's own)
	area := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, &s.tag)
	s.scroll.Add(gtx.Ops)
	area.Pop()
	if d := s.scroll.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical,
		pointer.ScrollRange{}, pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}); d != 0 {
		v.list.Position.Offset += d
		v.list.Position.BeforeEnd = true
		gtx.Execute(op.InvalidateCmd{})
	}
}

// underSticky reports whether list item i (a row) is under the stuck
// header this frame (from the last frame's positions): its hover actions
// hide.
func (v *Activity) underSticky(gtx C, i int) bool {
	s := &v.stk
	if !s.on {
		return false
	}
	top, ok := s.tops[i]
	if !ok {
		return false
	}
	return top < s.y+s.hh-gtx.Dp(2) && top+s.h[i] > s.y
}

// ---- accepting a Slack Connect channel invite ----

// acceptChannel is SlackConnectAcceptChannelInviteAction's click (dcZX):
// the eligibility check (core ChannelInviteEligible), then the upgrade
// modal, the accept modal (popups/sc_accept.go; a trial adds its note
// and "Accept and Start Trial"), or the join_channel alternative's
// conversations.joinConnectedShared; anything else is
// hasConnectInviteAcceptanceFailed. Grid's "accept on another
// workspace" (SlackConnectConfirmWorkspaceModal), the start-trial modal
// before the accept modal and the join-when-connected and join-workspace
// alternatives aren't in Clack: Clack signs in to one workspace.
func (v *Activity) acceptChannel(ph popups.Host, it core.ActivityItem) {
	inv := *it.Invite
	team, name := it.Team, strings.TrimPrefix(it.Title, "#")
	inviter := v.inviterName(it)
	v.write("Hmm, something went wrong. Try again? ", func(ctx context.Context, c *core.App) error {
		el, err := c.ChannelInviteEligible(ctx, inv.Sig)
		if err != nil {
			return err
		}
		show := func(f func()) {
			v.notes.mu.Lock()
			v.notes.fns = append(v.notes.fns, f)
			v.notes.mu.Unlock()
			v.h.Invalidate()
		}
		switch {
		case el.IsEligible && el.Requirements.Upgrade || el.Reason == core.SCReasonUpgradeButCannot:
			show(func() { ph.ShowOverlay(popups.NewSCUpgradeModal(ph, inv.ID)) })
		case el.IsEligible:
			show(func() {
				ph.ShowOverlay(popups.NewSCAcceptModal(ph, popups.SCAcceptOpts{Invite: inv, ChannelName: name, InvitingTeam: team,
					RequiredAccess: el.RequiredPrivacy(), FreeTrial: el.Requirements.Trial,
					OnAccepted: func(string, bool) { v.afterChannelAccept(ph, inv, inviter, team) }}))
			})
		case el.CanJoinChannel():
			if err := c.JoinConnectedChannel(ctx, inv.Sig, inv.ID); err != nil {
				return err
			}
			show(func() { v.afterChannelAccept(ph, inv, inviter, team) })
		default:
			return errors.New("this invitation can’t be accepted")
		}
		return nil
	}, func() {})
}

// afterChannelAccept is g7VB acceptSharedInviteAndMaybeOpenWelcomeModal:
// connectInvites.list for the invite; an approved acceptance opens the
// channel; then SharedInviteCreatorWelcomeModal; the feed reloads (the
// invite's row leaves it).
func (v *Activity) afterChannelAccept(ph popups.Host, inv core.ActivityInvite, inviter, team string) {
	v.write("", func(ctx context.Context, c *core.App) error {
		st, err := c.ConnectChannelInvite(ctx, inv.ID)
		v.notes.mu.Lock()
		v.notes.fns = append(v.notes.fns, func() {
			if err == nil && st.Found {
				if st.Approved && st.ChannelID != "" {
					v.h.Navigate("c/" + st.ChannelID)
				}
				ph.ShowOverlay(popups.NewSCWelcomeModal(ph, popups.SCWelcome{Connected: st.Approved, ChannelName: st.ChannelName,
					InvitingTeam: firstNonEmpty(st.InvitingTeam, team), InvitingTeamID: st.InvitingTeamID, InvitingName: inviter,
					PendingTeams: st.PendingTeams}))
			}
			v.load(actRefresh)
		})
		v.notes.mu.Unlock()
		v.h.Invalidate()
		return err
	}, func() {})
}
