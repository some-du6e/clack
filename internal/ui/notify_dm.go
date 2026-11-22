package ui

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// Which messages get a desktop notification, and what it says, as Slack's
// web client decides (CDN build 35b42de8b870, 2026-11-19; chunks fetched
// without a session). Karim, α.113 Windows, 2026-11-21: a message in the
// DM with Slack's system user (USLACK) raised no toast. That DM's messages
// are an app's: subtype bot_message with no user, a username and an
// icons override, a bot_profile and blocks, in an org-shared IM.
//
//   - maybeShowNotification (boot-render /+fI) first skips a conversation
//     whose desktop push is off: getCalculatedDesktopPushNotificationsEnabledSetting
//     (client-boot Fpyc u0) reads the conversation's desktop_push_enabled,
//     else global_desktop_push_enabled, on when unset ("Skip pref-disabled
//     push notification"). It goes on for a conversation the viewer is in
//     (isMemberOfChannel; every IM and group DM is one, since
//     processNewChannelForUpserting, 340b, sets their is_member). An IM is
//     shouldShowNotificationForDirectMessage (Fpyc RM), whoever its partner
//     is (a person, an app's bot user, USLACK). Any other conversation is
//     shouldShowNotificationForChannelMessage (Fpyc rr).
//   - RM has no level and no bot rule. The DM level it reads,
//     getDefaultDesktopNotifySetting, is getGlobalDmNotifySetting's
//     global_mpdm_desktop, which reads "nothing" as mentions_dms and unset
//     as everything, so it never holds a DM back. A bot_message (reason 9)
//     and a message with no user are rr's concern only. RM holds a message
//     back for:
//       - no_display (4) and no_notifications (5);
//       - a thread reply (6, isReply);
//       - the DM open in the focused window (16);
//       - the viewer's own message (11);
//       - a muted DM (12);
//       - Do Not Disturb (13), unless the message ignores it (a sender's
//         notify anyway: dnd_override's _ignore_dnd) or a VIP sent it
//         (shouldOverrideDndForMessage, Mvkl Wh);
//       - a sender the viewer hid (29, shouldSupressHiddenSender:
//         hidden_users, unless the org's locked hide_person_opt_out;
//         maybeNotifyAfter's core.UserHidden, before any lookup);
//       - the canvas, tab, folder and assistant thread subtypes (30, 31,
//         32, 34, 35).
//     Two reasons don't reach Clack. 22, an email shared into Slackbot's
//     own DM, needs the files' mode, which Clack's messages don't carry.
//     17, shouldSuppressOrgChannel, holds an org-shared conversation back
//     in every signed-in workspace of the org but the most recently used
//     one, and Clack signs into one workspace.
//   - rr reads the conversation's level, getCalculatedDesktopNotifySetting
//     (getCalculatedNotifySetting): its override, else global_desktop
//     (getGlobalChannelNotifySetting). It reads "nothing" as mentions_dms
//     either way, so no level is "nothing" in this build: Notifications
//     offers Everything and Mentions, and the push switch turns
//     notifications off. Then: a mention, or everything; no_display,
//     no_notifications, Do Not Disturb, the hidden sender and the
//     subtypes, as in RM.
//   - The title (Eley getNotificationTitle) names the conversation. For a
//     DM it is "New message from {getDisplayNameForChannel}", the
//     partner's name (getPrefCompliantMemberNameById of the IM's user,
//     hsKo, R6Qy), never the sender's username. It is "{name} is trying to
//     reach you" when the message ignores Do Not Disturb.
//   - The body (getContentForNotification, extractTextFromMessage) is
//     convertMessageToPlainText (2xQY) with blockSpacing "single": the
//     blocks' text, each block trimmed, the empty ones dropped, one per
//     line. An image block gives its title, never its alt text, and an
//     actions block nothing. Without blocks the body is the text. Only a
//     message with neither reads its files or attachments. Outside a DM
//     the sender's name comes first, getPrefCompliantMemberNameById of the
//     message's user: nothing for a message with no user, never a bot's
//     username. With no_text_in_notifications a DM's body is empty, and
//     any other conversation's is "a message from {name}".
//   - The icon is no sender's. desktopNotificationOptions' avatarImage is
//     getProfileImageForMember of the message's user (DOS4 c2), which is
//     none for a message with no user. Web Slack's toast in Chrome shows
//     the app's icon (browserNotificationOptions), and so do Clack's
//     (notify_toast.go, notify_mac.go): never the username's icons
//     override, USLACK's picture or the bot_profile's icons.

// notePushOn is getCalculatedDesktopPushNotificationsEnabledSetting for
// conversation ch: its desktop_push_enabled override, else the global
// switch (on unless global_desktop_push_enabled is false).
func (b *backend) notePushOn(ch string) bool {
	p := b.core.Prefs()
	if v := p.ChannelNotify[ch].DesktopPush; v != "" {
		return v == "true"
	}
	return !p.NoDesktopPush
}

// noteIsDM reports whether the message's conversation is an IM (isIm): a
// person's DM, an app's (the model's "app") or USLACK's.
func noteIsDM(ev core.Event, c *convInfo) bool {
	return ev.ConvKind == "im" || c != nil && (c.Kind == "im" || c.Kind == "app")
}

// noteQuietSubtypes are the subtypes both rules hold back, with their
// reasons: a channel canvas's update (30, isChannelCanvasUpdateMessage), a
// tab canvas's (31), a tab added (34) and a folder's item added (35), with
// their _unread_ignore forms (BiCp).
var noteQuietSubtypes = map[string]noteSkip{
	"channel_canvas_updated": noteSkipCanvasUpd, "channel_canvas_updated_unread_ignore": noteSkipCanvasUpd,
	"tabbed_canvas_updated": noteSkipTabCanvas, "tabbed_canvas_updated_unread_ignore": noteSkipTabCanvas,
	"channel_tab_added": noteSkipTabAdded, "channel_tab_added_unread_ignore": noteSkipTabAdded,
	"folder_item_added": noteSkipFolder, "folder_item_added_unread_ignore": noteSkipFolder,
}

// noteDMReason is RM for a DM's message: why Slack holds its notification
// back (noteShow: it doesn't), in RM's order. ignoreDND: a dnd_override
// asked for it.
func (b *backend) noteDMReason(a *App, ev core.Event, c *convInfo, ignoreDND bool) noteSkip {
	m := ev.Msg
	switch {
	case m.NoDisplay:
		return noteSkipNoDisplay
	case ev.Reply:
		return noteSkipReply
	case b.noteOnScreen(a, ev.Channel):
		return noteSkipOnScreen
	case m.User != "" && m.User == b.selfID:
		return noteSkipOwn
	case c != nil && c.Muted:
		return noteSkipMuted
	case m.NoNotifications:
		return noteSkipNoNotify
	case b.noteDNDHolds(ev, c, ignoreDND):
		return noteSkipDND
	}
	if why, ok := noteQuietSubtypes[m.Subtype]; ok {
		return why
	}
	// isAiAssistantRootMsg, isAiAssistantInitialMsg (32): a DM's only
	if m.Subtype == "assistant_app_thread" || m.Subtype == "assistant_app_initial_thread" {
		return noteSkipAssistant
	}
	return noteShow
}

// noteChannelReason is rr for any other conversation's message: its level
// (a channel's wantsNotify, a group DM's noteGroupDMLevel), a reply only
// when it mentions the viewer (notify_threads.go), and the holds RM shares
// with it, in rr's order.
func (b *backend) noteChannelReason(a *App, ev core.Event, c *convInfo, ignoreDND bool) noteSkip {
	m := ev.Msg
	if m.NoDisplay {
		return noteSkipNoDisplay
	}
	if !replyNotifies(b.core, ev) {
		return noteSkipReply
	}
	wants := ev.Mention
	if ev.ConvKind == "mpim" || c != nil && c.Kind == "mpim" {
		wants = wants || b.noteGroupDMLevel(ev.Channel) == "everything"
	} else {
		wants = b.wantsNotify(ev.Channel, ev.Mention)
	}
	switch {
	case !wants:
		return noteSkipNoMention
	case b.noteOnScreen(a, ev.Channel):
		return noteSkipOnScreen
	case m.User != "" && m.User == b.selfID:
		return noteSkipOwn
	case m.NoNotifications:
		return noteSkipNoNotify
	case c != nil && c.Muted:
		return noteSkipMuted
	case b.noteDNDHolds(ev, c, ignoreDND):
		return noteSkipDND
	}
	if why, ok := noteQuietSubtypes[m.Subtype]; ok {
		return why
	}
	return noteShow
}

// noteGroupDMLevel is getCalculatedDesktopNotifySetting for group DM ch,
// which rr reads as a channel's: the group DM's own desktop override,
// else global_mpdm_desktop (getDefaultNotifySetting's
// getGlobalDmNotifySetting, everything when unset). "nothing" reads as
// mentions_dms either way, so the level is everything or mentions; a
// "default" override is none, as wantsNotify reads it.
func (b *backend) noteGroupDMLevel(ch string) string {
	level := b.core.Prefs().ChannelNotify[ch].Desktop
	if level == "" || level == "default" {
		if level = b.core.GlobalNotifyString("global_mpdm_desktop"); level == "" {
			level = "everything"
		}
	}
	if level == "nothing" {
		level = "mentions_dms"
	}
	return level
}

// noteOnScreen is shouldSuppressNotificationInActiveChannel: the
// conversation is the one open, in the focused window.
func (b *backend) noteOnScreen(a *App, ch string) bool {
	return a.router.Current().Channel() == ch && a.focused
}

// noteDNDHolds is Do Not Disturb (13): paused notifications
// (getCurrentMemberDndStatus, the viewer's held status: core.DNDHeld)
// hold the message back unless it ignores them or a VIP sent it. A
// message it holds back is kept for a dnd_override (noteDNDOverride).
func (b *backend) noteDNDHolds(ev core.Event, c *convInfo, ignore bool) bool {
	if ignore || !b.core.DNDHeld() || b.noteVIPOverride(ev, c) {
		return false
	}
	k := ev.Channel + "/" + ev.TS
	if _, ok := b.noteDNDHeld[k]; !ok {
		if b.noteDNDHeld == nil {
			b.noteDNDHeld = map[string]core.Event{}
		}
		if len(b.noteDNDHeldOrder) >= noteShownMax {
			delete(b.noteDNDHeld, b.noteDNDHeldOrder[0])
			b.noteDNDHeldOrder = b.noteDNDHeldOrder[1:]
		}
		b.noteDNDHeldOrder = append(b.noteDNDHeldOrder, k)
	}
	b.noteDNDHeld[k] = ev
	return true
}

// noteVIPOverride is shouldOverrideDndForMessage (Mvkl Wh). It applies on
// a paid team (isPriorityVipAvailableForCurrentTeam), with "Messages from
// VIPs, even if notifications are paused" on (priority_dnd_override) and
// VIPs set. Then a bot message whose bot_profile.user_id is a VIP
// overrides DND. So does a VIP's own message in a DM or group DM, or one
// that mentions anyone: a member, a user group or a channel.
func (b *backend) noteVIPOverride(ev core.Event, c *convInfo) bool {
	cr := b.core
	if !cr.PaidTeam() || !cr.PrefBool("priority_dnd_override", false) {
		return false
	}
	vips := cr.VIPs()
	if len(vips) == 0 {
		return false
	}
	m := ev.Msg
	if m.Subtype == "bot_message" && m.BotProfile != nil && m.BotProfile.UserID != "" && slices.Contains(vips, m.BotProfile.UserID) {
		return true
	}
	if m.User == "" || !slices.Contains(vips, m.User) {
		return false
	}
	return noteIsDM(ev, c) || ev.ConvKind == "mpim" || c != nil && c.Kind == "mpim" || noteMentionsAny(m)
}

// noteMentionsAny reports whether m mentions anyone: a member, a user
// group or a channel (s1s/ Z's channelIds, userGroupIds and userIds).
func noteMentionsAny(m *core.Message) bool {
	mentions := false
	richtext.PlainTextWith(m.Doc, func(s richtext.Span) (string, bool) {
		switch s.Kind {
		case richtext.User, richtext.Usergroup, richtext.Channel:
			mentions = mentions || s.ID != ""
		}
		return "", false
	})
	return mentions
}

// notePrioritySound is Mvkl Tk, getNewMsgSoundPath's choice of
// priority_desktop_sound over desktop_sound: VIPs set, both sounds set
// and the priority one not "none" while the other isn't; then a bot
// message whose bot_profile.user_id is a VIP, or a VIP's message in a DM
// or group DM or that mentions anyone, with a priority sound that isn't
// "none".
func (b *backend) notePrioritySound(ev core.Event, c *convInfo) bool {
	cr := b.core
	vips := cr.VIPs()
	n, p := cr.GlobalNotifyString("desktop_sound"), cr.GlobalNotifyString("priority_desktop_sound")
	if len(vips) == 0 || n == "" || p == "" || n != "none" && p == "none" {
		return false
	}
	m := ev.Msg
	if m.Subtype == "bot_message" && m.BotProfile != nil && m.BotProfile.UserID != "" && slices.Contains(vips, m.BotProfile.UserID) {
		return true
	}
	if m.User == "" || !slices.Contains(vips, m.User) || p == "none" {
		return false
	}
	return noteIsDM(ev, c) || ev.ConvKind == "mpim" || c != nil && c.Kind == "mpim" || noteMentionsAny(m)
}

// noteShownMax is how many shown notifications Slack remembers
// (trackProcessedNotification, WgbD: the oldest goes at 100).
const noteShownMax = 100

// noteShownKey is stableKeyForNotificationMessage (WgbD): "channel:ts"
// for a message, "channel:msg" for a desktop_notification (its msg);
// "" when it has neither, which Slack keys by the clock (never a repeat).
func noteShownKey(ch, ts string) string {
	if ch == "" || ts == "" {
		return ""
	}
	return ch + ":" + ts
}

// noteWasShown reports whether the notification with key was shown
// lately: maybeShowNotification goes no further for it, whatever brings
// the message again (a replayed event, its settled stream, a followed
// reply's desktop_notification after the message's own, a dnd_override
// after it showed).
func (b *backend) noteWasShown(key string) bool { return key != "" && b.noteShown[key] }

// noteMarkShown records key as shown (trackProcessedNotification, right
// after the showNotification dispatch).
func (b *backend) noteMarkShown(key string) {
	if key == "" || b.noteShown[key] {
		return
	}
	if b.noteShown == nil {
		b.noteShown = map[string]bool{}
	}
	if len(b.noteShownOrder) >= noteShownMax {
		delete(b.noteShown, b.noteShownOrder[0])
		b.noteShownOrder = b.noteShownOrder[1:]
	}
	b.noteShownOrder = append(b.noteShownOrder, key)
	b.noteShown[key] = true
}

// noteDNDIgnored reports whether a dnd_override asked for the message's
// notification (_ignore_dnd).
func (b *backend) noteDNDIgnored(ch, ts string) bool { return b.noteIgnoreDND[ch+"/"+ts] }

// noteDNDOverride is handleDndOverride: a sender's "notify anyway" for a
// message DND held back. It shows that message's notification with DND
// ignored, at most once (a message that already ignores it changes
// nothing). Slack reads the message as its store has it now (an edit
// since shows). Clack's store keeps a conversation's messages only while
// a window of it is loaded, so when it has none the UI's copy of the
// event DND held back stands in (noteDNDHolds). A message that was shown
// isn't shown again (noteWasShown), and the other holds still apply, so a
// held-back one is the only kind that can show.
func (b *backend) noteDNDOverride(a *App, ev core.Event) {
	k := ev.Channel + "/" + ev.TS
	if ev.Channel == "" || ev.TS == "" || b.noteIgnoreDND[k] {
		return
	}
	held, kept := b.noteDNDHeld[k]
	if m, found := b.core.HeldMessage(ev.Channel, ev.TS); found {
		kind := held.ConvKind
		if v, known := b.core.Conv(ev.Channel); known {
			kind = v.Kind
		}
		held = core.Event{Type: "message", Channel: ev.Channel, TS: m.TS, ThreadTS: m.ThreadTS, UserID: m.User,
			Msg: &m, Reply: m.ThreadTS != "" && m.ThreadTS != m.TS, Mention: b.core.MentionsViewer(&m), ConvKind: kind}
	} else if !kept {
		return // Slack's getMessage finds nothing to show
	}
	if b.noteIgnoreDND == nil {
		b.noteIgnoreDND = map[string]bool{}
	}
	if len(b.noteIgnoreDNDOrder) >= noteShownMax {
		delete(b.noteIgnoreDND, b.noteIgnoreDNDOrder[0])
		b.noteIgnoreDNDOrder = b.noteIgnoreDNDOrder[1:]
	}
	b.noteIgnoreDNDOrder = append(b.noteIgnoreDNDOrder, k)
	b.noteIgnoreDND[k] = true
	b.maybeNotify(a, held)
}

// noteMissingText is extractTextFromMessage's last line, for a message
// with no text, blocks, files or attachments: "message {subtype} (message
// missing text)", the subtype "undefined" when there is none, as
// Slack's template literal writes it.
func noteMissingText(subtype string) string {
	if subtype == "" {
		subtype = "undefined"
	}
	return "message " + subtype + " " + nsGrowl.T("(message missing text)")
}

// notifyText is getContentForNotification's text for m (Eley):
// extractTextFromMessage over the message as sent: its converted text
// (richtext.NotificationMessageText: its blocks, each trimmed, else its
// text as written); the files' line (noteFilesText, notify_file.go: a
// clip's line, else, with no text or under a file subtype, the first
// file's title or meta line); else the text; else its attachments
// (richtext.NotificationAttachmentText); with noteMissingText for a
// message that has nothing to read and no files or attachments. The
// first ":simple_smile:" becomes ":)".
func notifyText(r richtext.Resolver, m *core.Message, st richtext.SpanText) string {
	sm := slack.Message{TS: m.TS, Channel: m.Channel, User: m.User, BotID: m.BotID, Subtype: m.Subtype, Text: m.Text, Blocks: m.Blocks}
	_ = json.Unmarshal(m.Attachments, &sm.Attachments)
	s := richtext.NotificationMessageText(r, &sm, st)
	if fs, ok := noteFilesText(r, m, s); ok {
		s = fs
	} else if s == "" {
		s = richtext.NotificationAttachmentText(r, &sm, st)
		if s == "" && len(sm.Attachments) == 0 {
			s = noteMissingText(m.Subtype)
		}
	}
	return strings.Replace(s, ":simple_smile:", ":)", 1)
}
