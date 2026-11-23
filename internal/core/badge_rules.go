package core

import (
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// What a socket message does to its conversation's sidebar row, as Slack's
// client counts a message it hears (client-boot qxvW
// canCountAsUnreadForCurrentUser for the row's bold, eqVu
// shouldMessageBadgeChannel for its mention badge, build a09b625d90aa;
// docs/NOTIFY-RULES.md §7). Clack made every message bold its row (a
// join, a huddle's post too) and counted a mention as its toasts did: any
// <!here> or <!channel> anywhere and while away, never a user group's or
// one in an attachment, a keyword in any bot's message.

// ruleMessage is what the rules read of a socket message, without the
// conversion the views' copy needs.
func ruleMessage(m *slack.Message) *Message {
	v := &Message{TS: m.TS, User: m.User, BotID: m.BotID, Subtype: m.Subtype, Text: m.Text, ThreadTS: m.ThreadTS,
		Blocks: m.Blocks, Attachments: m.RawAttachments, NoDisplay: m.NoDisplay, WorkflowID: m.WorkflowID,
		Inviter: m.Inviter, DNDSuppressed: m.DNDSuppressed, Metadata: m.Metadata}
	if bp := m.BotProfile; bp != nil {
		v.BotProfile = &BotProfile{ID: bp.ID, Workflow: bp.IsWorkflowBot}
	}
	return v
}

// countsInSidebar is countUnreadMessages' (EJMC) test of a message heard
// in ch: a broadcast counts until its thread is read past it
// (calcThreadBroadcastMessages, B95B: on arrival it is), any other message
// as canCountAsUnreadForCurrentUser says with
// systemMessagesRedesignM1Enabled, and not an AI app's thread root or
// first message (assistant_app_thread, assistant_app_initial_thread) but
// a workflow's (isAiAppsFeatureEnabled: they come only from AI apps). (A
// streamed message in an agent's session channel counts only behind
// session_channels_streaming_unread, which Clack's experiments leave
// off.)
func (a *App) countsInSidebar(m *Message) bool {
	switch m.Subtype {
	case "thread_broadcast":
		return true
	case "assistant_app_thread", "assistant_app_initial_thread":
		if m.WorkflowID == "" {
			return false
		}
	}
	return a.countsAsUnread(m, a.experiment("system_messages_redesign_m1") == "on")
}

// messageBadges is shouldMessageBadgeChannel (eqVu) for message m of ch: a
// conversation set to badge every new message (badge_all_unreads) that
// isn't muted badges on any (getShouldBadgeAllUnreadsForChannelId, EmXR:
// `n && !isMuted`; a muted one goes on to the rules below). A DM, and any other conversation that takes at-channel mentions
// (can_channel_have_at_channel_mentions) unless it's muted with muted
// items hidden (improve_mute_pref), badges on the viewer's mention, a user
// group of theirs, @channel, @everyone and @group (not in a reply) and
// @here (not while they're away, nor when it's older than the session);
// any other conversation on the viewer's mention and a user group of
// theirs. Their highlight words count except in a bot's or an app's
// message. (A DM's every message counts besides: the store's countsAsDM.)
// The loud-mentions limit (canMsgMentionChannelIfMention) reads a feature
// Clack doesn't have.
func (a *App) messageBadges(ch string, m *Message) bool {
	if a.NotifyPrefs(ch).BadgeAllUnreads && !a.S.IsMuted(ch) {
		return true
	}
	c, ok := a.S.Conv(ch)
	if !ok || c == nil {
		c = &slack.Conversation{} // not known yet: a DM by its id
	}
	im := c.IsIM || c.ID == "" && strings.HasPrefix(ch, "D")
	muted := a.S.IsMuted(ch)
	hideMuted := muted && a.experiment("improve_mute_pref") == "on" && !a.Prefs().ShowMutedOutside // getSidebarShouldHideMutedItems
	keywords := !a.ignoresHighlightWords(m)
	if im || !hideMuted && a.canHaveAtChannelMentions(ch, c, muted) {
		here := !a.viewerAway() && !a.beforePresenceChange(m.TS)
		return a.mentionsViewer(m, mentionOpts{here: here, broadcast: true, keywords: keywords})
	}
	return a.mentionsViewer(m, mentionOpts{keywords: keywords})
}

// canHaveAtChannelMentions is can_channel_have_at_channel_mentions (QWNA):
// not a DM; a conversation at Everything that isn't muted, or one that
// doesn't suppress_at_channel. (A #general the viewer can't post in takes
// them anyway: Clack keeps no posting permission for it.)
func (a *App) canHaveAtChannelMentions(ch string, c *slack.Conversation, muted bool) bool {
	if c.IsIM {
		return false
	}
	return a.desktopLevel(ch, c) == "everything" && !muted || !a.NotifyPrefs(ch).SuppressAtChannel
}

// desktopLevel is getCalculatedDesktopNotifySetting: the conversation's
// desktop override, else global_mpdm_desktop for a group DM (Everything
// when unset) or global_desktop; "nothing" reads as mentions_dms.
func (a *App) desktopLevel(ch string, c *slack.Conversation) string {
	level := a.NotifyPrefs(ch).Desktop
	if level == "" || level == "default" {
		if c.IsMPIM {
			if level = a.GlobalNotifyString("global_mpdm_desktop"); level == "" {
				level = "everything"
			}
		} else {
			level = a.Prefs().GlobalDesktop
		}
	}
	if level == "nothing" {
		level = "mentions_dms"
	}
	return level
}

// beforePresenceChange reports whether ts is older than
// getPresenceCountingInputs' userPresenceLastChanged: the viewer's
// _presence_last_changed, which nothing in Slack's build sets, else the
// session's start (start_ms). An @here from before it doesn't badge.
func (a *App) beforePresenceChange(ts string) bool {
	sec, err := strconv.ParseFloat(strings.TrimSpace(ts), 64)
	if err != nil || a.startedAt.IsZero() {
		return false
	}
	return int64(sec*1000) < a.startedAt.UnixMilli()
}
