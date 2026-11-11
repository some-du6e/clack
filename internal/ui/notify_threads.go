package ui

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/some-du6e/clack/internal/core"
)

// Thread reply notifications, from Slack's notification decisions
// (client-boot gMhK, client-boot-render maybeShowNotification and
// client-boot-shared gaJC's titles; build 407aff01646b):
//
//   - A reply that arrives as a message (shouldShowNotificationForChannelMessage)
//     notifies only when it mentions the viewer (checkForMentionInMessage
//     without highlight words, where a reply's @channel and @everyone
//     don't count and @here does); a DM's reply never does
//     (shouldShowNotificationForDirectMessage returns 6 for any reply).
//   - Replies in threads the viewer follows come from the server as
//     desktop_notification events (shouldShowNotificationForDesktopNotification):
//     a reply ({ts, thread_ts}) in a known conversation, unless that thread
//     is open with the window focused (shouldSuppressNotificationInActiveThread)
//     or already read to it (the subscription's lastRead); the event's
//     content is the body as the server wrote it (skip_formatting).
//   - The title (getNotificationTitle, one signed-in account,
//     grouped_notifications off): "New message from {name}" in a DM, else
//     "New message in {channel}" (getDisplayNameForChannel: "#name" for a
//     public channel, a private one's bare name, a group DM's members);
//     the body outside DMs is "{sender}: {text}" (getContentForNotification).

// replyNotifies is the message path's rule for replies.
func replyNotifies(ev core.Event, self string) bool {
	if !ev.Reply || ev.Msg == nil {
		return true
	}
	if ev.ConvKind == "im" {
		return false
	}
	return replyMentions(ev.Msg.Text, self)
}

// replyMentions is checkForMentionInMessage for a reply: the viewer named
// (<@U…>) or @here. User groups count in Slack too, but core doesn't know
// the viewer's groups per message.
func replyMentions(text, self string) bool {
	return (self != "" && strings.Contains(text, "<@"+self)) || strings.Contains(text, "<!here")
}

// notifyChannelName is getDisplayNameForChannel for the title.
func notifyChannelName(c *convInfo) string {
	if c == nil {
		return ""
	}
	if c.Kind == "channel" {
		return "#" + c.Name
	}
	return c.Name
}

// desktopNotification shows the server's desktop_notification for a
// followed thread's reply, after Slack's checks.
func (b *backend) desktopNotification(a *App, ev core.Event) {
	var e struct {
		Channel         string `json:"channel"`
		TS              string `json:"ts"`
		Msg             string `json:"msg"`
		ThreadTS        string `json:"thread_ts"`
		Content         string `json:"content"`
		IsChannelInvite bool   `json:"is_channel_invite"`
	}
	if json.Unmarshal(ev.Raw, &e) != nil || e.Channel == "" {
		return
	}
	c := b.m.conv(e.Channel)
	if c == nil {
		return // an unknown conversation (3)
	}
	if !e.IsChannelInvite {
		ts := e.TS
		if ts == "" {
			ts = e.Msg
		}
		if e.ThreadTS == "" || e.ThreadTS == ts {
			return // not a reply (7)
		}
		if a.focused && a.secondary.kind == secThread && a.secondary.open() &&
			a.secondary.channel == e.Channel && a.secondary.ts == e.ThreadTS {
			return // the thread is on screen (16)
		}
		if root, ok := b.core.S.Find(e.Channel, e.ThreadTS); ok && root.LastRead != "" && !tsBefore(root.LastRead, ts) {
			return // read already (20)
		}
	}
	title := "New message in " + notifyChannelName(c)
	if c.Kind == "im" || c.Kind == "app" { // isIm (an app's DM too)
		title = "New message from " + c.Name
	}
	body := e.Content
	a.dispatch("showNotification", map[string]any{"title": title, "message": body, "body": body, "channelId": e.Channel, "ts": e.TS},
		func(ctx context.Context, args map[string]any) (any, error) {
			if t, bd, ok := notificationText(args, nil, body); ok {
				a.notifyMessage(t, bd)
			}
			return nil, nil
		})
}

// tsBefore reports a < b for Slack timestamps ("1700000000.000100").
func tsBefore(a, b string) bool {
	as, af, _ := strings.Cut(a, ".")
	bs, bf, _ := strings.Cut(b, ".")
	if len(as) != len(bs) {
		return len(as) < len(bs)
	}
	if as != bs {
		return as < bs
	}
	return af < bf
}
