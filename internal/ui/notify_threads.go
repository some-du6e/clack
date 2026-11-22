package ui

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/store"
)

// Thread reply notifications, from Slack's notification decisions
// (client-boot gMhK, client-boot-render maybeShowNotification and
// client-boot-shared gaJC's titles; build 407aff01646b):
//
//   - A reply that arrives as a message (shouldShowNotificationForChannelMessage)
//     notifies only when it mentions the viewer (checkForMentionInMessage
//     without highlight words, core MentionsViewerInReply: the viewer's
//     <@id>, a user group they're in and @here, from the blocks or the
//     text and the attachments; a reply's @channel and @everyone don't
//     count, build 2cd9270e786c); a DM's reply never does
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
func replyNotifies(app *core.App, ev core.Event) bool {
	if !ev.Reply || ev.Msg == nil {
		return true
	}
	if ev.ConvKind == "im" {
		return false
	}
	return app != nil && app.MentionsViewerInReply(ev.Msg)
}

// notifyChannelName is getDisplayNameForChannel for the title
// (imIncludeAtSign off): "#name" for a public channel, a private
// channel's bare name, a group DM's members; formatChannelName's
// "unknown-channel" for a channel whose name isn't known, never the raw
// id (notify_conv.go). A group DM's name is its members' alone
// (getMpimDisplayNameAlphabetical, which never goes through
// formatChannelName): "" when none of them is known.
func notifyChannelName(c *convInfo) string {
	if c == nil {
		return ""
	}
	name := c.Name
	if !noteNamed(c) && c.Kind != "mpim" {
		name = noteUnknownChannel()
	}
	if c.Kind == "channel" {
		return "#" + name
	}
	return name
}

// desktopNotification shows the server's desktop_notification for a
// followed thread's reply, after Slack's checks. Its conversation comes
// from core (a followed thread's channel may be one the sidebar doesn't
// list), looked up first when the store can't name it (notify_conv.go).
func (b *backend) desktopNotification(a *App, ev core.Event) {
	b.desktopNotificationAfter(a, ev, false)
}

// desktopNotificationAfter is desktopNotification; resolved: the
// conversation was looked up already. Each decision writes its line
// (notify_decisions.go), the one that waits for the lookup once it ran.
func (b *backend) desktopNotificationAfter(a *App, ev core.Event, resolved bool) {
	var e struct {
		Channel         string  `json:"channel"`
		TS              string  `json:"ts"`
		Msg             string  `json:"msg"`
		ThreadTS        string  `json:"thread_ts"`
		IsChannelInvite bool    `json:"is_channel_invite"`
		User            string  `json:"user"`
		Text            *string `json:"text"`
	}
	if json.Unmarshal(ev.Raw, &e) != nil || e.Channel == "" {
		noteDecided(ev.Channel, "", noteSkipNoModel) // no channel (3)
		return
	}
	// Slack's checks read the event's own ts (gA's {ts, thread_ts}): one
	// without it is a reply to its thread_ts (isReply: !!thread_ts &&
	// thread_ts !== ts), never newer than maxTs ("a > r" of undefined) and
	// never read to ("o >= a"). The decision lines and the toast's target
	// fall back to msg
	ts := e.TS
	at := e.TS
	if at == "" {
		at = e.Msg
	}
	skip := func(why noteSkip) { noteDecided(e.Channel, at, why) }
	key := noteShownKey(e.Channel, e.Msg) // a desktop_notification's msg (notify_dm.go)
	if b.noteWasShown(key) {
		skip(noteSkipShown) // shown already: the reply's own message event came first (it may have while a lookup ran)
		return
	}
	if !b.notePushOn(e.Channel) {
		skip(noteSkipPushOff) // maybeShowNotification's "Skip pref-disabled push notification" (notify_dm.go)
		return
	}
	// shouldShowNotificationForDesktopNotification's order: an invite
	// shows (0) before any check of the reply's own (7, 16, 29, 19, 20)
	if !e.IsChannelInvite {
		if e.ThreadTS == "" || e.ThreadTS == ts {
			skip(noteSkipNotReply) // not a reply (7)
			return
		}
		if noteThreadOnScreen(a, e.Channel, e.ThreadTS, e.Text) {
			skip(noteSkipOnScreen) // the thread is on screen (16)
			return
		}
		if b.core.UserHidden(e.User) {
			skip(noteSkipHidden) // from a member the viewer hid ("user" in it: isUserHidden, 29)
			return
		}
		// notifications.maxTs: a reply newer than every message the store
		// took is one the client never had (store/notifications_maxts.go;
		// "!r || a > r", the max starting at "0")
		if ts != "" && b.core.S.NotificationAhead(ts) {
			skip(noteSkipTooNew) // 19
			return
		}
		if root, ok := b.core.S.Find(e.Channel, e.ThreadTS); ok && ts != "" && root.LastRead != "" && !store.TSLess(root.LastRead, ts) {
			skip(noteSkipRead) // read already (20)
			return
		}
	}
	c, named := b.noteConv(e.Channel)
	if !named && !resolved && b.core != nil {
		b.noteResolve(a, e.Channel, []string{e.Channel}, nil, nil, func() { b.desktopNotificationAfter(a, ev, true) })
		return
	}
	// in order with the conversation's notifications still waiting for
	// their lookups (noteOrder)
	if !resolved && b.notes.later(a, e.Channel, func() { b.desktopNotificationAfter(a, ev, true) }) {
		return
	}
	if c == nil {
		skip(noteSkipNoModel) // an unknown conversation (3)
		return
	}
	if c.Preview {
		skip(noteSkipNotMember) // a channel the viewer isn't in (isMemberOfChannel: 2)
		return
	}
	title := nsGrowl.T("New message in {channel}", "channel", notifyChannelName(c))
	if c.Kind == "im" || c.Kind == "app" { // isIm (an app's DM too)
		title = nsGrowl.T("New message from {name}", "name", c.Name)
	}
	note := a.messageNote(e.Channel, at, e.ThreadTS) // the toast's payload (notify.go): its click opens the thread at ts
	note.pushChecked = true                          // notePushOn above
	b.noteMarkShown(key)
	skip(noteShow)
	// showNotification gets what maybeShowNotification hands it for a
	// desktop_notification: {message: the event, modelObject: its
	// conversation, options: {skip_formatting, launch_uri, image_uri,
	// avatar_image}}. Plugins read the event (BetterBlock hides a blocked
	// sender's by its user), and what they pass on is shown: the body is
	// the message's content as the server wrote it (getServerFormattedContent),
	// none when a plugin took the message away (StreamerMode); in order
	// with the other notifications (backend.go showNotification)
	a.showNotification(b.desktopNotificationArgs(ev.Raw, e.Channel), func(ctx context.Context, args map[string]any) (any, error) {
		m, _ := args["message"].(map[string]any)
		if m == nil {
			return nil, nil
		}
		body, _ := m["content"].(string)
		a.notifyMessage(title, body, note)
		return nil, nil
	})
}

// desktopNotificationArgs is maybeShowNotification's showNotification
// arguments for a desktop_notification (boot-render /+fI): the event
// itself as message, the conversation the store holds as modelObject (an
// unknown one has none), and the options it sets: skip_formatting, and
// the event's launchUri, imageUri and avatarImage where it has them.
func (b *backend) desktopNotificationArgs(raw json.RawMessage, ch string) map[string]any {
	msg := map[string]any{}
	_ = json.Unmarshal(raw, &msg)
	opts := map[string]any{"skip_formatting": true}
	for from, to := range map[string]string{"launchUri": "launch_uri", "imageUri": "image_uri", "avatarImage": "avatar_image"} {
		if v, ok := msg[from]; ok {
			opts[to] = v
		}
	}
	args := map[string]any{"message": msg, "options": opts}
	if conv, ok := b.core.S.ConvJSON(ch); ok {
		args["modelObject"] = conv
	}
	return args
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

// noteThreadOnScreen is shouldSuppressNotificationInActiveThread (client-boot
// Fpyc, build 548618a2169f2a90) as it applies to Clack's windows: a reply
// in the thread open in the main window's side pane while that window has
// the focus (isMessageThreadInSidePane: a focused window that isn't a
// huddle's), unless the event's text holds an at-command
// (containsAtCommand). Slack's other branches read views Clack doesn't
// have: a thread active in an agent's home or a Slackbot AI DM in the
// main or split view, an app space's chat tab, and, in
// shouldSuppressNotificationInActiveHuddle, the huddle window's own
// thread view (Clack's huddle opens its thread in the main window's pane,
// huddleui window.go) or a fullscreen container over the pane.
func noteThreadOnScreen(a *App, ch, thread string, text *string) bool {
	if text != nil && containsAtCommand(*text) {
		return false
	}
	return a.focused && a.secondary.kind == secThread && a.secondary.open() &&
		a.secondary.channel == ch && a.secondary.ts == thread
}

// atGroup is containsAtCommand's legacy @group (iub5 H).
var atGroup = regexp.MustCompile(`<!group\b`)

// atCommands are the broadcast keywords containsAtCommand looks for, all
// but @here (kSm/ N: everyone, channel, the Salesforce teams, Einstein).
var atCommands = []string{"<!everyone>", "<!channel>", "<!sf:accountteam>", "<!sf:dealteam>", "<!assistant:einstein>"}

// containsAtCommand is iub5 containsAtCommand: a message whose text holds
// one is never kept quiet because its conversation or thread is on screen.
func containsAtCommand(text string) bool {
	if text == "" {
		return false
	}
	if atGroup.MatchString(text) {
		return true
	}
	for _, c := range atCommands {
		if strings.Contains(text, c) {
			return true
		}
	}
	return false
}
