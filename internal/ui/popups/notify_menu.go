package popups

// The channel header bell: Slack's ChannelNotificationPrefMenu (boot-shared
// 4f8632cd9602d1fa, source "channel_header"; PND5 in build 4d0c78085232),
// as it opens for a member of #clack on 2026-11-08: "Notify you about…",
// three radios with their descriptions, then what temp_mute (O2By VJ)
// picks:
//
//   - on: a separator and "More options" (MoreOptionsSubmenu, laNq:
//     "Temporarily mute…" Until tomorrow / 1 hour, a separator, Edit
//     default preferences and Advanced settings, the channel notifications
//     modal in channel_notify_modal.go);
//   - off: a separator and "Advanced options" (the filters glyph, with the
//     push status as its description: getChannelPushNotificationStatus),
//     then a separator and "Edit default preferences".
//
//   - All new posts: desktop everything (mobile follows when it did),
//     unmuting a muted channel. Checked: desktop everything, not muted.
//   - Just mentions: desktop mentions_dms, likewise; the description is
//     "@you" when @channel is suppressed.
//   - Mute and hide (improve_mute_pref on; "Mute channel" otherwise):
//     toggles muted.
//   - The checked All new posts / Just mentions row carries a
//     "Customized" tag when the channel's advanced settings differ from
//     that choice's defaults (notifyCustomized).
//   - Until tomorrow / 1 hour (useTempMute, RUvF): temp_mute_expiration_ts
//     at tomorrow's notification-schedule end (getDayOfWeekDndEndTime, 09:00
//     when unset) or an hour from now, unsynced; when that changed the
//     expiration, enqueueMuteChannelToast (vA1n): "#x muted for 1 hour." /
//     "#x muted until tomorrow." with Undo (muted false, unsynced), or
//     Slack's "Something went wrong." after a refused write.

import (
	"context"
	"image"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
)

// TempMuteNewBadge is PND5's badge name M.
const TempMuteNewBadge = "temp_mute_new_badge"

// notifyChoice is the menu's checked row.
func notifyChoice(n core.ChannelNotify, global string, muted bool) string {
	if muted {
		return "mute"
	}
	return calcNotify(n.Desktop, global)
}

// calcNotify is getCalculatedNotifySetting (client-boot gMhK) folded
// through pA (boot-shared n9iP): the channel's override, else the global
// default; an override of "nothing" counts as mentions_dms, and anything
// unknown as mentions_dms.
func calcNotify(override, global string) string {
	v := override
	if v == "" || v == "default" {
		v = global
	}
	switch v {
	case "everything":
		return v
	case "nothing":
		if override == "nothing" {
			return "mentions_dms"
		}
		return v
	}
	return "mentions_dms"
}

// notifyCustomized is n9iP Xq for one radio (setting everything or
// mentions_dms): the checked row, not muted, whose channel overrides
// (getCalculatedChannelNotificationOverrides: all_notifications_prefs
// .channels[ch], a mobile "nothing" read as mentions_dms) follow every
// thread, suppress @channel under Just mentions, badge every message
// under All new posts, or send mobile something other than the desktop
// setting. The row then carries ChannelNotificationPrefMenu's
// "Customized" tag.
func notifyCustomized(n core.ChannelNotify, desktop string, muted bool, setting string) bool {
	if muted || desktop != setting {
		return false
	}
	mobile := n.Mobile
	if mobile == "nothing" {
		mobile = "mentions_dms"
	}
	return n.FollowAllThreads ||
		(desktop == "mentions_dms" && n.SuppressAtChannel) ||
		(desktop == "everything" && n.BadgeAllUnreads) ||
		(mobile != "" && mobile != desktop)
}

// customizedTag is the radio's tag: PND5's {label "Customized", style
// "informative"} (the menu rows' tag slot, menu_badge.go), or none.
func customizedTag(n core.ChannelNotify, desktop string, muted bool, setting string) *MenuTag {
	if notifyCustomized(n, desktop, muted, setting) {
		return &MenuTag{Label: nsMenu.T("Customized"), Style: "informative"}
	}
	return nil
}

// NotifyIcon is Slack's notification glyph for a calculated desktop
// setting (n9iP Km / Nl, the header bell's and the menu rows' icons):
// muted or "nothing" notifications-off, "everything" the ringing bell
// notifications-all-new-posts, else notifications.
func NotifyIcon(setting string, muted bool) string {
	switch {
	case muted || setting == "nothing":
		return "bell-off" // notifications-off
	case setting == "everything":
		return "notifications-all-new-posts"
	}
	return "bell" // notifications
}

// ChannelNotifyIcon is the header bell's icon for ch.
func ChannelNotifyIcon(h Host, ch string, muted bool) string {
	a := h.Core()
	if a == nil {
		return NotifyIcon("", muted)
	}
	return NotifyIcon(calcNotify(a.NotifyPrefs(ch).Desktop, a.Prefs().GlobalDesktop), muted)
}

// tempMuteChoice is calculateTempMuteDuration: "tomorrow" when the mute
// ends within 5 minutes of tomorrow at hhmm (the day's notification
// schedule end, "" for 09:00), "1hour" for another future end, "" for
// none.
func tempMuteChoice(muted bool, exp int64, now time.Time, hhmm string) string {
	if !muted || exp == 0 || exp <= now.Unix() {
		return ""
	}
	if d := exp - tomorrowAt(now, hhmm).Unix(); d > -300 && d < 300 {
		return "tomorrow"
	}
	return "1hour"
}

// tomorrowAt is tomorrow at hhmm ("HH:MM"; 09:00 when it doesn't parse).
func tomorrowAt(now time.Time, hhmm string) time.Time {
	t := now.AddDate(0, 0, 1)
	h, m := 9, 0
	if pt, err := time.Parse("15:04", hhmm); err == nil {
		h, m = pt.Hour(), pt.Minute()
	}
	return time.Date(t.Year(), t.Month(), t.Day(), h, m, 0, 0, now.Location())
}

// untilTomorrow is muteUntilTomorrow's value: now plus calculateSnoozeDurationInMinutes
// (whole minutes, rounded up) to tomorrow at hhmm.
func untilTomorrow(now time.Time, hhmm string) int64 {
	mins := (tomorrowAt(now, hhmm).Unix() - now.Unix() + 59) / 60
	return now.Unix() + 60*mins
}

// pushStatus is getChannelPushNotificationStatus: the "Advanced options"
// description when the channel's desktop push or mobile override is off.
func pushStatus(n core.ChannelNotify) string {
	desktopOff, mobileOff := n.DesktopPush == "false", n.Mobile == "nothing"
	// the prefs I18n's "… are <strong>off</strong>" (the row's description
	// is plain text)
	plain := func(src string) string { return i18n.Plain(nsPrefs.RT(src)) }
	switch {
	case desktopOff && mobileOff:
		return plain("Push notifications are <strong>off</strong>")
	case desktopOff:
		return plain("Desktop push notifications are <strong>off</strong>")
	case mobileOff:
		return plain("Mobile push notifications are <strong>off</strong>")
	}
	return ""
}

// ChannelNotifyItems are the bell menu's rows for ch.
func ChannelNotifyItems(h Host, ch string) []MenuItem {
	a := h.Core()
	if a == nil {
		return []MenuItem{MenuItem{Header: "Notify you about…"}.In(nsMenu)}
	}
	n := a.NotifyPrefs(ch)
	muted := actIsMuted(h, ch)
	cur := notifyChoice(n, a.Prefs().GlobalDesktop, muted)
	desktop := calcNotify(n.Desktop, a.Prefs().GlobalDesktop) // getCalculatedDesktopNotifySetting
	set := func(level string) func() {
		return func() {
			nn := n
			if nn.Mobile == "" || nn.Mobile == "default" || nn.Mobile == nn.Desktop {
				nn.Mobile = level
			}
			nn.Desktop, nn.Muted = level, false
			run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.SetNotifyPrefs(ctx, ch, nn) },
				func(_ struct{}, err error) {
					if err != nil {
						h.Toast("Couldn’t change notifications: " + errText(err))
						return
					}
					actMuteSet[ch] = false
				})
		}
	}
	// the option labels are n9iP's (channel_sidebar), the rest PND5's (menu)
	mentions := nsSidebar.T("@you, @channel, @here")
	if n.SuppressAtChannel {
		mentions = nsSidebar.T("@you")
	}
	muteLabel, muteDesc := "Mute channel", "Only get notified if someone @mentions you personally"
	if a.Experiment("improve_mute_pref") == "on" {
		muteLabel, muteDesc = "Mute and hide", "Only badge the channel when someone @mentions you"
	}
	muteDesc = nsMenu.T(muteDesc)
	tomorrowEnd := a.DNDEndHour(time.Now().AddDate(0, 0, 1).Weekday())
	temp := tempMuteChoice(muted, n.TempMuteExp, time.Now(), tomorrowEnd)
	tempMute := func(until func() int64, duration string) func() {
		return func() {
			ts := until()
			changed := n.TempMuteExp != ts
			run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.TempMute(ctx, ch, ts) },
				func(_ struct{}, err error) {
					if err == nil {
						delete(actMuteSet, ch) // muted now comes from the prefs (core temp_mute.go), not the last toggle
					}
					if changed {
						tempMuteToast(h, ch, duration, err)
					}
				})
		}
	}
	// "Advanced settings" / "Advanced options" (onOpenAdvancedOptions, the
	// channel notifications modal) aren't offered in a group DM without
	// mpdm_notifications_advanced_option.
	advanced := true
	if c, ok := a.Conv(ch); ok && c.Kind == "mpim" && a.Experiment("mpdm_notifications_advanced_option") != "on" {
		advanced = false
	}
	openAdvanced := func() { h.ShowOverlay(NewChannelNotifyModal(h, ch)) }
	editDefaults := MenuItem{Key: "channel_ctx_menu_edit_default_prefs", Label: "Edit default preferences", Do: func() { openPreferences(h, "notifications") }}
	var tail []MenuItem
	if a.TempMuteOn() {
		// MoreOptionsSubmenu (laNq, channel_sidebar)
		more := inAll(nsSidebar, []MenuItem{
			{Header: "Temporarily mute…"},
			{Key: "channel_mute_until_tomorrow", Label: "Until tomorrow", Radio: true, Checked: temp == "tomorrow",
				Do: tempMute(func() int64 {
					return untilTomorrow(time.Now(), a.DNDEndHour(time.Now().AddDate(0, 0, 1).Weekday()))
				}, "until_tomorrow")},
			{Key: "channel_mute_1_hour", Label: "1 hour", Radio: true, Checked: temp == "1hour",
				Do: tempMute(func() int64 { return time.Now().Add(time.Hour).Unix() }, "one_hour")},
			Sep(),
			editDefaults,
		})
		if advanced {
			more = append(more, MenuItem{Key: "channel_ctx_menu_advanced_options", Label: "Advanced settings", Do: openAdvanced}.In(nsSidebar))
		}
		mo := MenuItem{Key: "more_options", Label: "More options", Sub: more}.In(nsMenu)
		// temp_mute_new_badge: with temp_mute, the megaphone's
		// generic_new_badge space turns on EducationTag's "New" (Yu9P) on
		// More options; opening the submenu clears it (clearNewBadge). The
		// impression PND5 records names NEW_BADGE_GENERIC, which hro4's
		// table lacks in this build, so recordNotificationEvent sends
		// nothing (NznF: no notificationName).
		if a.MegaphoneNewBadge(TempMuteNewBadge) {
			mo.Tag = &MenuTag{Label: i18n.T("education", "New"), Style: "inverse-education", HighStyle: "education"}
			mo.OnSubOpen = func() { a.ClearNewBadge(TempMuteNewBadge) }
		}
		tail = []MenuItem{Sep(), mo}
	} else {
		if advanced {
			tail = append(tail, Sep(), MenuItem{Key: "more_options", Label: "Advanced options", Icon: "filter-sliders",
				Subtitle: pushStatus(n), Do: openAdvanced}.In(nsMenu))
		}
		tail = append(tail, Sep(), editDefaults.In(nsMenu))
	}
	return append([]MenuItem{
		MenuItem{Header: "Notify you about…"}.In(nsMenu),
		MenuItem{Key: "everything", Label: "All new posts", Subtitle: nsSidebar.T("Messages and threads you follow"), Icon: NotifyIcon("everything", false), Radio: true,
			Checked: cur == "everything", Tag: customizedTag(n, desktop, muted, "everything"), Do: set("everything")}.In(nsSidebar),
		MenuItem{Key: "mentions_dms", Label: "Just mentions", Subtitle: mentions, Icon: NotifyIcon("mentions_dms", false), Radio: true,
			Checked: cur == "mentions_dms", Tag: customizedTag(n, desktop, muted, "mentions_dms"), Do: set("mentions_dms")}.In(nsSidebar),
		MenuItem{Key: "mute", Label: muteLabel, Subtitle: muteDesc, Icon: NotifyIcon("nothing", true), Radio: true, Checked: muted,
			Do: func() { actToggleMute(h, ch, "channel") }}.In(nsMenu),
	}, tail...)
}

// tempMuteToast is enqueueMuteChannelToast (vA1n): the bold "<channel>
// muted for 1 hour." / "… until tomorrow." with Undo, which unmutes
// (muted false, unsynced); a refused write is Slack's generic error.
func tempMuteToast(h Host, ch, duration string, err error) {
	a := h.Core()
	if a == nil || !a.TempMuteOn() {
		return
	}
	if err != nil {
		h.Toast("Something went wrong. Reloading Slack might clear things up. If it doesn’t, drop us a line.")
		return
	}
	name := ch
	if c, ok := a.Conv(ch); ok {
		name = channelEntityName(c)
	}
	// enqueueMuteChannelToast's "<strong>{channelEntity} muted.</strong>"
	// (channel_sidebar; the toast's text is bold already)
	src := "<strong>{channelEntity} muted.</strong>"
	switch duration {
	case "one_hour":
		src = "<strong>{channelEntity} muted for 1 hour.</strong>"
	case "until_tomorrow":
		src = "<strong>{channelEntity} muted until tomorrow.</strong>"
	}
	msg := i18n.Plain(nsSidebar.RT(src, "channelEntity", name))
	undo := func() {
		run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.Mute(ctx, ch, false) },
			func(_ struct{}, err error) {
				if err != nil {
					h.Toast("Something went wrong. Reloading Slack might clear things up. If it doesn’t, drop us a line.")
					return
				}
				delete(actMuteSet, ch)
			})
	}
	if u, ok := h.(interface{ ToastUndo(string, func()) }); ok {
		u.ToastUndo(msg, undo)
		return
	}
	h.Toast(msg)
}

// channelEntityName is the toast's ChannelEntity (X3qV): "#name" for a
// channel, the conversation's name otherwise.
func channelEntityName(c core.Conv) string {
	switch c.Kind {
	case "channel", "private":
		return "#" + c.Name
	}
	return c.Name
}

// NewChannelNotifyMenu is the bell menu, 300px, right-aligned to anchor
// (Slack: x 1124–1424, y 77 under the bell at 1327, 1440×900).
func NewChannelNotifyMenu(h Host, anchor image.Point, ch string) *Menu {
	return NewMenuOpts(h, ChannelNotifyItems(h, ch), MenuOpts{Anchor: pointRect(anchor), AlignRight: true, Width: 300})
}
