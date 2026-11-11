package popups

// The channel header bell: Slack's ChannelNotificationPrefMenu (boot-shared
// 4f8632cd9602d1fa, source "channel_header"), as it opens for a member of
// #clack on 2026-11-08: "Notify you about…", three radios with their
// descriptions, a separator, and "More options" (MoreOptionsSubmenu:
// "Temporarily mute…" Until tomorrow / 1 hour, then Edit default
// preferences and Advanced settings, the channel notifications modal in
// channel_notify_modal.go).
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
//   - Until tomorrow / 1 hour: temp_mute_expiration_ts at 09:00 tomorrow
//     (the default work start) or an hour from now, unsynced.

import (
	"context"
	"image"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

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

// customizedTag is the radio's tag label.
func customizedTag(n core.ChannelNotify, desktop string, muted bool, setting string) string {
	if notifyCustomized(n, desktop, muted, setting) {
		return "Customized"
	}
	return ""
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
// ends within 5 minutes of 09:00 tomorrow, "1hour" for another future
// end, "" for none.
func tempMuteChoice(muted bool, exp int64, now time.Time) string {
	if !muted || exp == 0 || exp <= now.Unix() {
		return ""
	}
	if d := exp - tomorrowAt9(now).Unix(); d > -300 && d < 300 {
		return "tomorrow"
	}
	return "1hour"
}

func tomorrowAt9(now time.Time) time.Time {
	t := now.AddDate(0, 0, 1)
	return time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, now.Location())
}

// ChannelNotifyItems are the bell menu's rows for ch.
func ChannelNotifyItems(h Host, ch string) []MenuItem {
	a := h.Core()
	if a == nil {
		return []MenuItem{{Header: "Notify you about…"}}
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
	mentions := "@you, @channel, @here"
	if n.SuppressAtChannel {
		mentions = "@you"
	}
	muteLabel, muteDesc := "Mute channel", "Only get notified if someone @mentions you personally"
	if a.Experiment("improve_mute_pref") == "on" {
		muteLabel, muteDesc = "Mute and hide", "Only badge the channel when someone @mentions you"
	}
	temp := tempMuteChoice(muted, n.TempMuteExp, time.Now())
	tempMute := func(until func() time.Time) func() {
		return func() {
			ts := until().Unix()
			run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.TempMute(ctx, ch, ts) },
				func(_ struct{}, err error) {
					if err != nil {
						h.Toast("Couldn’t mute this channel: " + errText(err))
					}
				})
		}
	}
	// MoreOptionsSubmenu ends with "Advanced settings" (onOpenAdvancedOptions,
	// the channel notifications modal) unless this is a group DM without
	// mpdm_notifications_advanced_option.
	more := []MenuItem{
		{Header: "Temporarily mute…"},
		{Key: "channel_mute_until_tomorrow", Label: "Until tomorrow", Radio: true, Checked: temp == "tomorrow",
			Do: tempMute(func() time.Time { return tomorrowAt9(time.Now()) })},
		{Key: "channel_mute_1_hour", Label: "1 hour", Radio: true, Checked: temp == "1hour",
			Do: tempMute(func() time.Time { return time.Now().Add(time.Hour) })},
		Sep(),
		{Key: "channel_ctx_menu_edit_default_prefs", Label: "Edit default preferences", Do: func() { openPreferences(h, "notifications") }},
	}
	if c, ok := a.Conv(ch); !ok || c.Kind != "mpim" || a.Experiment("mpdm_notifications_advanced_option") == "on" {
		more = append(more, MenuItem{Key: "channel_ctx_menu_advanced_options", Label: "Advanced settings",
			Do: func() { h.ShowOverlay(NewChannelNotifyModal(h, ch)) }})
	}
	return []MenuItem{
		{Header: "Notify you about…"},
		{Key: "everything", Label: "All new posts", Subtitle: "Messages and threads you follow", Icon: NotifyIcon("everything", false), Radio: true,
			Checked: cur == "everything", Tag: customizedTag(n, desktop, muted, "everything"), Do: set("everything")},
		{Key: "mentions_dms", Label: "Just mentions", Subtitle: mentions, Icon: NotifyIcon("mentions_dms", false), Radio: true,
			Checked: cur == "mentions_dms", Tag: customizedTag(n, desktop, muted, "mentions_dms"), Do: set("mentions_dms")},
		{Key: "mute", Label: muteLabel, Subtitle: muteDesc, Icon: NotifyIcon("nothing", true), Radio: true, Checked: muted,
			Do: func() { actToggleMute(h, ch, "channel") }},
		Sep(),
		{Key: "more_options", Label: "More options", Sub: more},
	}
}

// NewChannelNotifyMenu is the bell menu, 300px, right-aligned to anchor
// (Slack: x 1124–1424, y 77 under the bell at 1327, 1440×900).
func NewChannelNotifyMenu(h Host, anchor image.Point, ch string) *Menu {
	return NewMenuOpts(h, ChannelNotifyItems(h, ch), MenuOpts{Anchor: pointRect(anchor), AlignRight: true, Width: 300})
}
