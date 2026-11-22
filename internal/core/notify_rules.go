package core

// What the desktop notification rules read of a message (client-boot
// Fpyc shouldShowNotificationForChannelMessage and its helpers, Slack's
// client build a09b625d90aa; the table in docs/NOTIFY-RULES.md).
// ui/notify_dm.go asks them as rr does.

// NotifyMention is checkMessageAndUpdateMentions (Fpyc) for a message of a
// conversation that isn't a 1:1 DM. With atChannel
// (can_channel_have_at_channel_mentions, QWNA: ui's noteAtChannel) a
// mention of the viewer or of a user group they're in counts, and so do
// @channel, @everyone and @group (not in a thread reply: isReply) and
// @here, unless the viewer is away; without it, only the viewer's own
// mention does (rr asks that of a muted channel's message too). The
// viewer's highlight words count either way, but never in a bot's message
// (a bot_message without a workflow bot's profile) or an app's
// (shouldIgnoreHighlightWords, 6o3B). Clack counted @here, @channel and
// @everyone anywhere and away, never a user group or a mention in an
// attachment, and a keyword in any bot's message.
func (a *App) NotifyMention(m *Message, atChannel bool) bool {
	if m == nil {
		return false
	}
	o := mentionOpts{keywords: !a.ignoresHighlightWords(m)}
	if atChannel {
		o.broadcast, o.here = true, !a.viewerAway()
	} else {
		o.noUsergroups = true // ignoreUserGroupMentions: rr passes no fourth argument
	}
	return a.mentionsViewer(m, o)
}

// viewerAway is getPresenceByMemberId for the viewer (DuLq hH) reading
// "away": the last presence the socket gave them, set by hand or not.
func (a *App) viewerAway() bool {
	self := a.S.SelfID()
	a.S.RLock()
	defer a.S.RUnlock()
	return self != "" && a.S.Presence[self] == "away"
}

// ignoresHighlightWords is shouldIgnoreHighlightWords (6o3B) with
// isAppMessage: a bot_message whose profile isn't a workflow bot's
// (isWorkflowBot), or an app's message.
func (a *App) ignoresHighlightWords(m *Message) bool {
	return m.Subtype == "bot_message" && !(m.BotProfile != nil && m.BotProfile.Workflow) || a.IsAppMessage(m)
}

// IsAppMessage is isAppMessage (tNG+): a message with a bot_id whose
// sender is an app user (is_app_user) as the store holds them; one it
// doesn't hold isn't (getMemberById's fetchIfMissing false).
func (a *App) IsAppMessage(m *Message) bool {
	if m == nil || m.BotID == "" || m.User == "" {
		return false
	}
	u, ok := a.S.RawUser(m.User)
	return ok && u != nil && u.IsAppUser
}

// countsAsUnreadIgnored are canCountAsUnreadForCurrentUser's
// ignoredMessageTypes (qxvW r).
var countsAsUnreadIgnored = map[string]bool{
	"channel_canvas_updated_unread_ignore": true, "tabbed_canvas_updated_unread_ignore": true,
	"channel_join": true, "channel_leave": true, "group_join": true, "group_leave": true,
	"huddle_message": true, "huddle_thread": true, "mpim_notify_disabled": true,
	"app_conversation_join": true, "app_conversation_leave": true, "app_conversation_created": true,
	"channel_tab_added_unread_ignore": true, "folder_item_added_unread_ignore": true,
}

// CountsAsUnreadForViewer is canCountAsUnreadForCurrentUser (qxvW Ay) as
// rr asks it of a mention: a message that isn't hidden (no_display; a
// socket message is never a hidden stub or a hidden reply) and that
// invites the viewer (a join with an inviter, isInviteForCurrentUser), or
// isn't the viewer's own (a workflow's post as them is), isn't one of
// the ignored types (a huddle's post, a join) and wasn't held for Do Not
// Disturb (dnd_suppressed).
func (a *App) CountsAsUnreadForViewer(m *Message) bool {
	if m == nil || m.NoDisplay {
		return false
	}
	self := a.S.SelfID()
	if (m.Subtype == "channel_join" || m.Subtype == "group_join") && m.Inviter != "" && m.User == self {
		return true
	}
	if m.User == self && m.WorkflowID == "" || countsAsUnreadIgnored[m.Subtype] {
		return false
	}
	return !m.DNDSuppressed
}
