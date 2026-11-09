package popups

// The channel header bell: Slack's ChannelNotificationPrefMenu (boot-shared
// 4f8632cd9602d1fa, source "channel_header"), as it opens for a member of
// #clack on 2026-11-08: "Notify you about…", three radios with their
// descriptions, a separator, and "More options" (MoreOptionsSubmenu:
// "Temporarily mute…" Until tomorrow / 1 hour, then Edit default
// preferences).
//
//   - All new posts: desktop everything (mobile follows when it did),
//     unmuting a muted channel. Checked: desktop everything, not muted.
//   - Just mentions: desktop mentions_dms, likewise; the description is
//     "@you" when @channel is suppressed.
//   - Mute and hide (improve_mute_pref on; "Mute channel" otherwise):
//     toggles muted.
//   - Until tomorrow / 1 hour: temp_mute_expiration_ts at 09:00 tomorrow
//     (the default work start) or an hour from now, unsynced.

import (
	"context"
	"image"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// notifyChoice is the menu's checked row.
func notifyChoice(n core.ChannelNotify, global string, muted bool) string {
	if muted {
		return "mute"
	}
	v := n.Desktop
	if v == "" || v == "default" {
		v = global
	}
	if v == "" {
		v = "mentions_dms"
	}
	return v
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
	return []MenuItem{
		{Header: "Notify you about…"},
		{Key: "everything", Label: "All new posts", Subtitle: "Messages and threads you follow", Icon: "bell",
			Checked: cur == "everything", Do: set("everything")},
		{Key: "mentions_dms", Label: "Just mentions", Subtitle: mentions, Icon: "bell",
			Checked: cur == "mentions_dms", Do: set("mentions_dms")},
		{Key: "mute", Label: muteLabel, Subtitle: muteDesc, Icon: "bell-off", Checked: muted,
			Do: func() { actToggleMute(h, ch, "channel") }},
		Sep(),
		{Key: "more_options", Label: "More options", Sub: []MenuItem{
			{Header: "Temporarily mute…"},
			{Key: "channel_mute_until_tomorrow", Label: "Until tomorrow", Checked: temp == "tomorrow",
				Do: tempMute(func() time.Time { return tomorrowAt9(time.Now()) })},
			{Key: "channel_mute_1_hour", Label: "1 hour", Checked: temp == "1hour",
				Do: tempMute(func() time.Time { return time.Now().Add(time.Hour) })},
			Sep(),
			{Key: "channel_ctx_menu_edit_default_prefs", Label: "Edit default preferences", Do: func() { openPreferences(h, "notifications") }},
		}},
	}
}

// NewChannelNotifyMenu is the bell menu, 300px, right-aligned to anchor
// (Slack: x 1124–1424, y 77 under the bell at 1327, 1440×900).
func NewChannelNotifyMenu(h Host, anchor image.Point, ch string) *Menu {
	return NewMenuOpts(h, ChannelNotifyItems(h, ch), MenuOpts{Anchor: pointRect(anchor), AlignRight: true, Width: 300})
}
