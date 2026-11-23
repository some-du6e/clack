package huddleui

import (
	"context"
	"encoding/json"
	"log"
	"runtime"
	"time"

	"github.com/some-du6e/clack/internal/logtag"
)

// A "join the huddle" suggestion, as Slack's client shows it (build
// a09b625d90aa; docs/NOTIFY-RULES.md §1, §6):
//
//   - huddle_recommend {channel_id, talking_users} (boot-render
//     huddleRecommendRtmHandler) is ignored for the conversation of the
//     huddle the viewer is in ("Ignoring recommend notification for active
//     huddle"); shouldShowNotificationForHuddle decides as for an invite
//     (Do Not Disturb 13, muted 12, a conversation the client doesn't know
//     3), and a suppressed one is logged with its reason.
//   - showHuddleRecommendNotification (boot-shared) waits up to 2 s for
//     the conversation and the talking members
//     (ensureModelsForIdsArePresent), bounces the dock and shows
//     sharedHuddleRecommendNotificationOptions: no title, the
//     conversation's display name as the subtitle, and "{A and B} are
//     talking in {channelDisplayName}." (the topic form reads a huddle
//     topic Clack doesn't keep), with desktopHuddleRecommendNotificationOptions:
//     the huddle's tag as msg, notification id type hudrec, no reply box,
//     and desktop_sound, which only the macOS app plays
//     (shouldDesktopHuddleNotificationBeSilent: isDesktopApp && isMac).
//     The click opens the conversation (notificationClicked: a msg that
//     starts "huddle"). Clack had no suggestion at all.

// recommendEvent is huddle_recommend's payload.
type recommendEvent struct {
	Channel string   `json:"channel_id"`
	Talking []string `json:"talking_users"`
}

// huddleSkipWhy is messageForSuppressedNotification for the reasons
// shouldShowNotificationForHuddle gives Clack.
var huddleSkipWhy = map[int]string{
	3:  "the conversation isn't known",
	12: "the conversation is muted",
	13: "Do Not Disturb is on",
}

// onRecommend is huddleRecommendRtmHandler.
func (c *Controller) onRecommend(raw json.RawMessage) {
	var ev recommendEvent
	if json.Unmarshal(raw, &ev) != nil || ev.Channel == "" {
		return
	}
	c.mu.Lock()
	active := c.s != nil && c.s.Channel() == ev.Channel
	c.mu.Unlock()
	if active {
		log.Printf("huddle: ignoring recommend notification for active huddle")
		return
	}
	go c.presentRecommend(ev)
}

// presentRecommend is the handler's shouldShowNotificationForHuddle and
// showHuddleRecommendNotification, off the event pump.
func (c *Controller) presentRecommend(ev recommendEvent) {
	app := c.h.Core()
	if app == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	dnd := app.DNDActive(ctx)
	cancel()
	if why := c.huddleNotifyReason(ev.Channel, dnd); why != 0 {
		log.Printf("huddle: notification (huddle recommendation) for %s suppressed because: %d, %s", logtag.ID(ev.Channel), why, huddleSkipWhy[why])
		return
	}
	log.Printf("huddle: showing huddle recommendation notification for %s", logtag.ID(ev.Channel))
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	app.EnsureUsers(ctx, ev.Talking)
	cancel()
	c.notify(c.recommendNotification(ev))
}

// recommendNotification is sharedHuddleRecommendNotificationOptions and
// desktopHuddleRecommendNotificationOptions for ev.
func (c *Controller) recommendNotification(ev recommendEvent) Notification {
	// the names as the loop has them: each but the last with " and "
	// after it ("A and B and C")
	talking := ""
	for i, u := range ev.Talking {
		talking += c.displayName(u)
		if i < len(ev.Talking)-1 {
			talking += " " + nsGrowl.T("and ")
		}
	}
	place := c.inviteSubtitle(ev.Channel) // getDisplayNameForChannel, for a conversation that exists
	body := nsGrowl.T("{talkingUsersFormatted} are talking in {channelDisplayName}.", "talkingUsersFormatted", talking, "channelDisplayName", place)
	sound := ""
	if app := c.h.Core(); app != nil && runtime.GOOS == "darwin" {
		if s := app.PrefString("desktop_sound"); s != "" && s != "none" {
			sound = s // the macOS app plays it with the notification
		}
	}
	return Notification{Body: body, PlainBody: body, Channel: ev.Channel, Subtitle: place, Recommend: true, Sound: sound}
}
