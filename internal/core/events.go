package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

var mentionRe = regexp.MustCompile(`<@([UW][A-Z0-9]+)`)

// pump applies realtime events to the store and forwards them to subscribers.
func (a *App) pump() {
	for ev := range a.RTM.Events {
		a.handle(ev)
	}
}

func (a *App) handle(ev slack.Event) {
	a.tapRaw(ev) // Taut's rtm.on (taut_raw.go)
	ctx, cancel := context.WithTimeout(a.bg, 15*time.Second)
	defer cancel()
	ch := chanID(ev.Channel)
	switch ev.Type {
	case "message":
		a.onMessage(ctx, ch, ev)
	case "search_frecency": // handleSearchFrecencyPrefsUpdate
		var e struct {
			Value string `json:"value"`
		}
		if json.Unmarshal(ev.Raw, &e) == nil && e.Value == "updated" {
			_ = a.LoadFrecencyPrefs(ctx)
		}
	case "search_recents": // handleSearchRecentsPrefsUpdate (search_recents.go)
		var e struct {
			Value string `json:"value"`
		}
		if json.Unmarshal(ev.Raw, &e) == nil && e.Value == "updated" && a.RecentsReadEnabled() {
			_ = a.LoadRecentsPrefs(ctx)
		}
	case "reaction_added", "reaction_removed":
		var e struct {
			User     string `json:"user"`
			Reaction string `json:"reaction"`
			Item     struct {
				Channel string `json:"channel"`
				TS      string `json:"ts"`
			} `json:"item"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		add := ev.Type == "reaction_added"
		changed := false
		m, ok := a.S.Mutate(e.Item.Channel, e.Item.TS, func(m *slack.Message) { changed = applyReaction(m, e.Reaction, e.User, add) || changed })
		a.EnsureUsers(ctx, []string{e.User})
		if ok && changed { // the echo of the viewer's own (optimistic) reaction changes nothing
			a.emitUpdate(e.Item.Channel, &m)
		}
	case "ai_assistant_status", "ai_assistant_threads_suggested_prompts":
		a.onAssistant(ctx, ev) // agent.go
	case "user_typing":
		var e struct {
			User     string `json:"user"`
			ThreadTS string `json:"thread_ts"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		if e.User == a.S.SelfID() {
			return
		}
		a.EnsureUsers(ctx, []string{e.User})
		a.emit(Event{Type: "typing", Channel: ch, UserID: e.User, Name: a.S.UserName(e.User), ThreadTS: e.ThreadTS})
	case "channel_marked", "im_marked", "group_marked", "mpim_marked":
		var e struct {
			TS           string `json:"ts"`
			UnreadCount  int    `json:"unread_count_display"`
			MentionCount int    `json:"mention_count_display"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		a.S.MarkRead(ch, e.TS)
		a.S.Lock()
		if c, ok := a.S.Counts[ch]; ok {
			c.HasUnreads = e.UnreadCount > 0
			c.MentionCount = e.MentionCount
		}
		a.S.Unlock()
		cv := a.Count(ch)
		a.emit(Event{Type: "marked", Channel: ch, TS: e.TS, Count: &cv})
	case "presence_change":
		var e struct {
			User     string   `json:"user"`
			Users    []string `json:"users"`
			Presence string   `json:"presence"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		us := e.Users
		if e.User != "" {
			us = append(us, e.User)
		}
		a.S.Lock()
		for _, u := range us {
			a.S.Presence[u] = e.Presence
			if usr, ok := a.S.Users[u]; ok {
				usr.Presence = e.Presence
			}
		}
		a.S.Unlock()
		a.presenceArrived()
		a.emit(Event{Type: "presence", Users: us, Presence: e.Presence})
	case "user_change", "user_profile_changed":
		var e struct {
			User slack.User `json:"user"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		if e.User.ID != "" {
			a.S.PutUsers([]slack.User{e.User})
			if u, ok := a.User(e.User.ID); ok {
				a.emit(Event{Type: "user", UserID: u.ID, User: &u})
			}
		}
	case "team_icon_change":
		a.onTeamIconChange(ev.Raw)
	case "team_pref_change":
		a.onTeamPrefChange(ev.Raw) // teamprefs.go
	case "team_rename":
		var e struct {
			Name   string `json:"name"`
			TeamID string `json:"team_id"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		a.S.Lock()
		if e.Name != "" && (e.TeamID == "" || e.TeamID == a.S.TeamID) {
			a.S.TeamName = e.Name
		}
		cur := a.S.TeamID
		a.S.Unlock()
		a.setTeamName(firstNonEmpty(e.TeamID, cur), e.Name)
		a.emit(Event{Type: "team"})
	case "emoji_changed":
		var e struct {
			Subtype string   `json:"subtype"`
			Name    string   `json:"name"`
			Names   []string `json:"names"`
			Value   string   `json:"value"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		switch e.Subtype {
		case "add":
			a.S.CustomEmoji.Put(e.Name, e.Value)
		case "remove":
			a.S.CustomEmoji.Remove(e.Names...)
		}
		a.emit(Event{Type: "emoji_changed", Name: e.Name})
	case "member_joined_channel":
		var e struct {
			User string `json:"user"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		if e.User != a.S.SelfID() {
			return
		}
		a.refreshConv(ctx, ch)
	case "channel_joined", "group_joined", "im_created", "mpim_joined", "channel_created", "channel_rename",
		"group_rename", "channel_unarchive", "im_open", "mpim_open":
		var e struct {
			Channel slack.Conversation `json:"channel"`
			TempExp *int64             `json:"temp_membership_expiration_ts"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		id := firstNonEmpty(e.Channel.ID, ch)
		switch ev.Type {
		case "im_created", "mpim_joined", "im_open", "mpim_open": // Slack marks these open
			a.openDM(id) // before refreshConv, whose "conv" event resnapshots the sidebar
		case "channel_joined", "group_joined": // channelJoined: a "Join…" membership (preview.go)
			if e.TempExp != nil {
				a.setTemp(id, *e.TempExp)
			}
		}
		a.refreshConv(ctx, id)
	case "record_channel_close": // channelClosed for a record (code) channel (codechannel_close.go)
		a.onRecordChannelClose(ch)
	case "record_channel_open": // recordChannelOpened
		a.onRecordChannelOpen(ctx, ch)
	case "im_close", "mpim_close":
		// Closed from some client: out of the sidebar until a new message,
		// but still a known conversation (search, switcher, history).
		var e struct {
			ConvertedTo string `json:"converted_to"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		a.markDMClosed(ch)
		if e.ConvertedTo != "" { // group DM turned into a private channel
			a.S.Lock()
			delete(a.S.Convs, ch)
			a.S.Unlock()
			a.emit(Event{Type: "conv_removed", Channel: ch})
			a.refreshConv(ctx, e.ConvertedTo)
			return
		}
		a.emit(Event{Type: "sidebar_changed"})
	case "channel_left", "group_left", "channel_archive", "group_archive", "channel_deleted":
		a.dropMembership(ch, ev.Type == "channel_archive" || ev.Type == "group_archive")
		a.emit(Event{Type: "conv_removed", Channel: ch})
	case "pin_added", "pin_removed":
		var e struct {
			ChannelID  string            `json:"channel_id"`
			User       string            `json:"user"` // who pinned (or unpinned) it
			EventTS    string            `json:"event_ts"`
			PinnedInfo *slack.PinnedInfo `json:"pinned_info"`
			Item       struct {
				Channel   string        `json:"channel"`
				CreatedBy string        `json:"created_by"`
				Created   int64         `json:"created"`
				Message   slack.Message `json:"message"`
			} `json:"item"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		ch = firstNonEmpty(ch, e.ChannelID, e.Item.Channel)
		if e.Item.Message.TS != "" {
			// Slack's pinAdded / pinRemoved → upsertMessagePinnedStatus
			// (boot-shared 1dqP): the message's pinned_to gains or loses the
			// channel and pin_added sets its pinned_info {pinned_by,
			// pinned_ts} (the event's own, MessageLabels' "Pinned by …"),
			// whether or not the message is loaded yet. One that isn't
			// takes the pin when it arrives (store/pins.go).
			own := e.PinnedInfo
			if own == nil || own.PinnedBy == "" {
				own = e.Item.Message.PinnedInfo
			}
			info := pinnedInfoOf(ch, own, firstNonEmpty(e.Item.CreatedBy, e.User), e.Item.Created, e.EventTS)
			if m, ok := a.S.SetPin(ch, e.Item.Message.TS, ev.Type == "pin_added", info); ok {
				a.emitUpdate(ch, &m)
			}
		}
	case "thread_marked", "update_thread_state", "dnd_updated", "dnd_updated_user", "pref_change", "badge_counts_updated",
		"channel_sections_upserted", "channel_sections_channels_upserted", "channel_sections_channels_removed", "channel_sections_deleted":
		if ev.Type == "dnd_updated" {
			a.onDNDUpdated(ctx, ev.Raw) // before the UI hears of it
		}
		if ev.Type == "thread_marked" {
			a.onThreadMarked(ev.Raw) // the root's last_read, before the UI hears of it
		}
		if ev.Type == "badge_counts_updated" {
			a.onActivityBadges(ev.Raw) // handleBadgeCountsUpdatedRTMEvent's activity_v2
		}
		a.emit(Event{Type: ev.Type, Raw: ev.Raw})
		switch ev.Type {
		case "channel_sections_upserted", "channel_sections_channels_upserted", "channel_sections_channels_removed", "channel_sections_deleted":
			if secs, err := a.C.SectionsAll(ctx); err == nil {
				a.S.Lock()
				a.S.Sections = secs
				a.S.Unlock()
				a.emit(Event{Type: "sidebar_changed"})
			}
		case "thread_marked", "update_thread_state", "badge_counts_updated":
			a.refreshCountsSoon()
		case "pref_change":
			if raw, err := a.C.Prefs(ctx); err == nil {
				a.installPrefs(raw)
				a.frecencyAfterPrefs(raw)
				p := a.Prefs()
				a.emit(Event{Type: "prefs", Prefs: &p})
			}
		}
	case "subteam_created", "subteam_updated", "subteam_members_changed", "subteam_self_added", "subteam_self_removed":
		var e struct {
			Subteam   slack.Usergroup `json:"subteam"`
			SubteamID string          `json:"subteam_id"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		if e.SubteamID != "" && (ev.Type == "subteam_self_added" || ev.Type == "subteam_self_removed") {
			// subteamSelfAdded / subteamSelfRemoved: userGroupMembership
			a.S.SetMyUsergroup(e.SubteamID, ev.Type == "subteam_self_added")
			a.emit(Event{Type: "names"})
		}
		if e.Subteam.ID != "" && e.Subteam.Handle != "" {
			a.S.Lock()
			a.S.Usergroups[e.Subteam.ID] = e.Subteam.Handle
			found := false
			for i := range a.S.Groups {
				if a.S.Groups[i].ID == e.Subteam.ID {
					a.S.Groups[i], found = e.Subteam, true
				}
			}
			if !found {
				a.S.Groups = append(a.S.Groups, e.Subteam)
			}
			a.S.Unlock()
		}
	case "saved_added", "saved_updated", "saved_deleted":
		a.onSavedEvent(ctx, ev.Type, ev.Raw)
	case "draft_create", "draft_update", "draft_delete", "draft_send":
		a.onDraftEvent(ev.Type, ev.Raw)
	case "activity":
		a.onActivityEvent(ctx, ev.Raw)
	case "activity_clear_all_completed":
		// handleActivityRTMEvent's sibling (client-boot-render
		// activityClearAllCompleted): with an undo_key, the clear-all
		// toast and its Undo (activity.clearAll).
		var e struct {
			UndoKey string `json:"undo_key"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		if e.UndoKey != "" {
			a.emit(Event{Type: "activity", Name: ev.Type, ID: e.UndoKey, Raw: ev.Raw})
		}
	case "activity_views_updated":
		// sD/F activityViewsUpdated: the saved views changed (another
		// client made, renamed, hid or deleted one); ActivityViewsEvent
		// reads them
		a.emit(Event{Type: "activity", Name: ev.Type, Raw: ev.Raw})
	case "bookmark_added", "bookmark_removed", "channel_updated", "channel_tab_added", "channel_tab_removed":
		if ev.Type == "channel_updated" { // properties (a code channel's agent session) merge in
			for _, id := range a.onChannelUpdated(ev.Raw) {
				if c, ok := a.Conv(id); ok {
					a.emit(Event{Type: "conv", Channel: id, Conv: &c})
				}
			}
		}
		// the channel tab bar changed: the UI re-reads Tabs / Bookmarks
		a.emit(Event{Type: "tabs", Channel: ch, Name: ev.Type, Raw: ev.Raw})
	case "thread_subscribed", "thread_unsubscribed":
		a.onThreadSubscription(ev.Raw, ev.Type == "thread_subscribed")
	case "desktop_notification":
		// the server's notification for a followed thread's reply (the
		// client's message path skips replies that don't mention the
		// viewer): the UI decides and shows it (ui/notify_threads.go)
		a.emit(Event{Type: ev.Type, Channel: ch, Raw: ev.Raw})
	case "star_added", "star_removed":
		a.onStarEvent(ev.Raw, ev.Type == "star_added")
	case "file_change", "file_shared", "file_public":
		go a.onFileChange(ev) // files.info off the event pump
	case "file_deleted", "file_undeleted":
		// filesDeleted / filesUndeleted: the Files view's deleted list
		// follows them (under file_trash_updates); Raw has file_id(s)
		a.emit(Event{Type: "file_trash", Name: ev.Type, Raw: ev.Raw})
	case "file_created":
		a.onFileCreated(ev) // an upload in PROCESSING resolves (upload_send.go)
	case "entity_details_present", "entity_comments_present", "entity_comment_thread_present", "entity_comment_action_acknowledged":
		a.onWorkObjectEvent(ev) // work objects (workobjects.go)
	case "megaphone_delayed_notification_ready", "clear_megaphone_notification",
		"megaphone_notifications_user_refresh", "megaphone_notifications_team_refresh":
		a.onMegaphoneEvent(ev.Type, ev.Raw) // megaphone.go
	default:
		// sh_room_*, huddle_invite, huddle_invite_cancel (Slack's
		// huddleInviteCancelRtmHandler), huddle_start…
		if strings.HasPrefix(ev.Type, "sh_") || strings.HasPrefix(ev.Type, "huddle_") {
			a.emit(Event{Type: "huddle", Name: ev.Type, Channel: ch, Raw: ev.Raw})
		}
	}
}

func (a *App) emitUpdate(ch string, m *slack.Message) {
	v := a.message(ch, m)
	a.emit(Event{Type: "update", Channel: ch, TS: m.TS, ThreadTS: m.ThreadTS, Msg: &v})
}

// openStreams remembers the messages last seen with streaming_state
// in_progress (bounded: a stream that never settles is dropped after
// maxOpenStreams others).
type openStreams struct {
	mu   sync.Mutex
	open map[string]bool
	q    []string
}

const maxOpenStreams = 256

// seen records ch/ts's state and reports whether it was open before.
func (o *openStreams) seen(ch, ts string, open bool) bool {
	k := ch + "/" + ts
	o.mu.Lock()
	defer o.mu.Unlock()
	was := o.open[k]
	switch {
	case open && !was:
		if o.open == nil {
			o.open = map[string]bool{}
		}
		o.open[k] = true
		o.q = append(o.q, k)
		if len(o.q) > maxOpenStreams {
			delete(o.open, o.q[0])
			o.q = o.q[1:]
		}
	case !open && was:
		delete(o.open, k)
	}
	return was
}

// emitStreamDone is emitUpdate for the message_changed that ends a chat
// stream, with what a message event carries for its notification.
func (a *App) emitStreamDone(ch string, m *slack.Message) {
	v := a.message(ch, m)
	self := a.S.SelfID()
	ev := Event{Type: "update", Channel: ch, TS: m.TS, ThreadTS: m.ThreadTS, UserID: m.User, Msg: &v, StreamDone: true,
		Reply:    m.ThreadTS != "" && m.ThreadTS != m.TS,
		Mention:  mentionsSelf(m, self) || (m.User != self && keywordMention(m, a.S.HighlightWords())),
		ConvKind: a.kindOf(ch)}
	if u, ok := a.User(m.User); ok {
		ev.User = &u
	}
	a.emit(ev)
}

func (a *App) refreshConv(ctx context.Context, id string) {
	if id == "" {
		return
	}
	c, err := a.C.ConversationInfo(ctx, id)
	if err != nil {
		return
	}
	a.S.PutConv(*c)
	if c.IsIM {
		a.EnsureUsers(ctx, []string{c.User})
	}
	v := a.conv(c)
	a.emit(Event{Type: "conv", Channel: id, Conv: &v})
}

func (a *App) onMessage(ctx context.Context, ch string, ev slack.Event) {
	var e struct {
		Subtype   string         `json:"subtype"`
		Message   *slack.Message `json:"message"`
		Previous  *slack.Message `json:"previous_message"`
		DeletedTS string         `json:"deleted_ts"`
	}
	if json.Unmarshal(ev.Raw, &e) != nil {
		return
	}
	switch e.Subtype {
	case "message_changed":
		if e.Message == nil {
			return
		}
		m := *e.Message
		a.EnsureUsers(ctx, referencedUsers([]slack.Message{m}))
		// Slack's messageChanged is replaceMessages (replace: true, client-boot
		// z+/K processAndMergeMessages): the event's message replaces the
		// stored one wholesale, thread metadata included. When a thread's
		// last reply is deleted the parent's message_changed has no
		// reply_count, and the reply bar goes; a deleted parent with
		// replies arrives here as subtype "tombstone".
		// the earlier copy was an open chat stream: the stored one, or one
		// seen in a thread that isn't cached (Slack's store keeps every
		// message the socket brings)
		streamed := a.streams.seen(ch, m.TS, m.StreamingState == "in_progress")
		if old, ok := a.S.Find(ch, m.TS); ok {
			keepFiles(&m, &old)
			streamed = streamed || old.StreamingState == "in_progress"
			// Slack's store keeps a thread's replies apart from the
			// messages (the thread's reply list), so a replaced reply
			// stays in its thread whatever the copy says: one without
			// thread_ts must not become a channel message here, nor miss
			// the open thread pane.
			if m.ThreadTS == "" && old.ThreadTS != "" && old.ThreadTS != old.TS {
				m.ThreadTS, m.ParentUser = old.ThreadTS, firstNonEmpty(m.ParentUser, old.ParentUser)
			}
		}
		a.S.NoteLive(ch, m.TS, &m) // a replies answer older than this keeps it (store/live.go)
		if _, found := a.S.Mutate(ch, m.TS, func(x *slack.Message) { *x = m }); !found {
			a.putLocal(ch, m) // a reply goes to its (cached) thread, never the channel
		}
		if needsFileInfo(&m) {
			go a.fillFiles(ch, m.TS)
		}
		a.woMessageUpdated(ch, &m) // a work object's refreshed unfurl (workobjects.go)
		if streamed && m.StreamingState != "in_progress" {
			a.emitStreamDone(ch, &m)
			return
		}
		a.emitUpdate(ch, &m)
		return
	case "message_deleted":
		// Slack's setMessageDeleted just removes the message; the parent's
		// reply_count / latest_reply come with the parent's message_changed.
		thread := ""
		if e.Previous != nil && e.Previous.ThreadTS != e.DeletedTS {
			thread = e.Previous.ThreadTS
		} else if old, ok := a.S.Find(ch, e.DeletedTS); ok && old.ThreadTS != old.TS {
			thread = old.ThreadTS
		}
		a.S.Remove(ch, e.DeletedTS)
		a.S.NoteLive(ch, e.DeletedTS, nil)
		a.emit(Event{Type: "delete", Channel: ch, TS: e.DeletedTS, ThreadTS: thread})
		return
	case "message_locked":
		a.onMessageLocked(ch, ev.Raw)
		return
	case "message_replied":
		a.onMessageReplied(ctx, ch, ev.Raw) // thread_meta.go
		return
	}
	var m slack.Message
	if json.Unmarshal(ev.Raw, &m) != nil || m.TS == "" {
		return
	}
	// appendMessages merges a socket message into the stored one: a pin
	// that came first stays (store/pins.go)
	a.S.KeepPin(ch, &m)
	a.streams.seen(ch, m.TS, m.StreamingState == "in_progress")
	a.EnsureUsers(ctx, referencedUsers([]slack.Message{m}))
	if needsFileInfo(&m) { // the echo of the viewer's own upload too
		go a.fillFiles(ch, m.TS)
	}
	isReply := m.ThreadTS != "" && m.ThreadTS != m.TS
	// a broadcast's root carries its parent's reply metadata; a plain
	// reply leaves the parent to message_replied (thread_meta.go)
	a.mergeBroadcastRoot(ch, &m, ev.Raw)
	// Echo of our own optimistic send: swap the pending row instead of
	// announcing a second message.
	if a.confirmPending(ch, &m) {
		if isTopLevel(&m) {
			a.S.NoteMessage(ch, &m, false)
		}
		return
	}
	if a.wasConfirmed(&m) { // late echo after the API reply confirmed it
		a.putLocal(ch, m)
		a.emitUpdate(ch, &m)
		return
	}
	if isReply {
		a.S.AddReply(ch, m)
		a.S.NoteLive(ch, m.TS, &m) // a replies answer older than this reply keeps it (store/live.go)
	}
	if !isReply || m.Subtype == "thread_broadcast" {
		a.S.Upsert(ch, m)
	}
	self := a.S.SelfID()
	mention := mentionsSelf(&m, self) || (m.User != self && keywordMention(&m, a.S.HighlightWords()))
	if !isReply || m.Subtype == "thread_broadcast" {
		a.S.NoteMessage(ch, &m, mention)
	} else if m.User != self {
		a.refreshCountsSoon() // thread unread state lives server-side
	}
	if _, known := a.S.Conv(ch); !known {
		a.refreshConv(ctx, ch)
	}
	if !isReply || m.Subtype == "thread_broadcast" {
		if a.reopenActive() { // a new message brings a closed DM back
			a.emit(Event{Type: "sidebar_changed"})
		}
	}
	a.clearAssistantStatusOnMessage(ch, m.ThreadTS) // agent.go
	v := a.message(ch, &m)
	cv := a.Count(ch)
	ev2 := Event{Type: "message", Channel: ch, TS: m.TS, ThreadTS: m.ThreadTS, UserID: m.User, Msg: &v, Count: &cv,
		Reply: isReply, Mention: mention, ConvKind: a.kindOf(ch)}
	if u, ok := a.User(m.User); ok {
		ev2.User = &u
	}
	a.emit(ev2)
}

// mentionsSelf reports whether m mentions the viewer directly or via
// @here/@channel/@everyone. (Usergroup mentions come from client.counts.)
func mentionsSelf(m *slack.Message, self string) bool {
	for _, mm := range mentionRe.FindAllStringSubmatch(m.Text, -1) {
		if mm[1] == self {
			return true
		}
	}
	return strings.Contains(m.Text, "<!here") || strings.Contains(m.Text, "<!channel") || strings.Contains(m.Text, "<!everyone")
}

// keywordMention is checkForMentionInMessage's highlight-words test: one
// of the viewer's Channel keywords (global_keywords) in the text, or in an
// attachment's title, pretext or text that isn't a link preview.
func keywordMention(m *slack.Message, words []string) bool {
	if len(words) == 0 {
		return false
	}
	if len(richtext.LocateHighlightWords(m.Text, words, false)) > 0 {
		return true
	}
	for _, at := range m.Attachments {
		if at.FromURL != "" {
			continue
		}
		for _, t := range []string{at.Title, at.Pretext, at.Text} {
			if len(richtext.LocateHighlightWords(t, words, false)) > 0 {
				return true
			}
		}
	}
	return false
}

// needsFileInfo reports whether a message carries file stubs that must be
// completed with files.info.
func needsFileInfo(m *slack.Message) bool {
	for i := range m.Files {
		if fileStub(&m.Files[i]) {
			return true
		}
	}
	return false
}

// fileStub reports whether f is a file object Slack sent before it had the
// file: file_access=check_file_info (shouldCheckFileInfo), no url_private,
// or an upload it hasn't processed yet. A pasted screenshot's message
// event often beats Slack's processing; its file then has a name and
// url_private but an empty mimetype, filetype and pretty_type, size 0 and
// media_display_type "unknown", which draws as a generic "FILE" card.
// Slack's web client refetches the file on file_change (filesChanged)
// once processing is done.
func fileStub(f *slack.File) bool {
	if f.ID == "" || f.Mode == "tombstone" || f.Mode == "hidden_by_limit" {
		return false
	}
	if f.FileAccess == "check_file_info" || f.URLPrivate == "" {
		return true
	}
	return !f.IsExternal && f.Mimetype == "" && f.Filetype == ""
}

// fileFillDelays are fillFiles' waits before each files.info round: Slack
// takes a few seconds to process an upload (thumbnails, mimetype); a
// file_change after that refetches it too (onFileChange).
var fileFillDelays = []time.Duration{700 * time.Millisecond, 1400 * time.Millisecond, 2100 * time.Millisecond,
	3 * time.Second, 5 * time.Second, 8 * time.Second}

// fillFiles completes the file stubs of one message and emits an update.
func (a *App) fillFiles(ch, ts string) {
	_ = a.once("files:"+ch+"/"+ts, func() error {
		ctx, cancel := context.WithTimeout(a.bg, time.Minute)
		defer cancel()
		for _, d := range fileFillDelays {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(d): // uploads take a moment to process
			}
			m, ok := a.S.Find(ch, ts)
			if !ok {
				return nil
			}
			files := append([]slack.File(nil), m.Files...)
			done, changed := true, false
			for i := range files {
				if !fileStub(&files[i]) {
					continue
				}
				if f, err := a.fileInfo(ctx, files[i].ID); err == nil {
					files[i], changed = f, true
				}
				if fileStub(&files[i]) {
					done = false
				}
			}
			if changed {
				a.setFiles(ch, ts, files)
			}
			if done {
				return nil
			}
		}
		return nil
	})
}

// fileInfo is files.info's file object.
func (a *App) fileInfo(ctx context.Context, id string) (slack.File, error) {
	var r struct {
		File slack.File `json:"file"`
	}
	if err := a.C.Call(ctx, "files.info", url.Values{"file": {id}}, &r); err != nil {
		return slack.File{}, err
	}
	if r.File.ID == "" {
		return slack.File{}, errors.New("core: files.info returned no file")
	}
	return r.File, nil
}

// setFiles replaces the files of a cached message (by id; files the
// message no longer has are left out) and emits an update.
func (a *App) setFiles(ch, ts string, files []slack.File) {
	byID := make(map[string]slack.File, len(files))
	for _, f := range files {
		byID[f.ID] = f
	}
	if upd, ok := a.S.Mutate(ch, ts, func(x *slack.Message) {
		fs := append([]slack.File(nil), x.Files...)
		for i := range fs {
			if f, ok := byID[fs[i].ID]; ok {
				fs[i] = f
			}
		}
		x.Files = fs
	}); ok {
		a.emitUpdate(ch, &upd)
	}
}

// onFileChange is Slack's filesChanged / filesShared: the file was
// processed, renamed or shared, so every loaded message carrying it gets
// files.info's current object (Slack keeps files in one store by id;
// Clack keeps copies in the messages).
func (a *App) onFileChange(ev slack.Event) {
	id := canvasEventFile(ev)
	if id == "" {
		return
	}
	refs := a.S.WithFile(id)
	if len(refs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(a.bg, 15*time.Second)
	defer cancel()
	f, err := a.fileInfo(ctx, id)
	if err != nil {
		return
	}
	for _, r := range refs {
		a.setFiles(r.Channel, r.TS, []slack.File{f})
	}
}

// keepFiles gives m (a message_changed copy) the stored message's complete
// file objects where m only has stubs of them: Slack's messages don't
// carry their files' state, its file store does.
func keepFiles(m, old *slack.Message) {
	if old == nil || len(old.Files) == 0 || !needsFileInfo(m) {
		return
	}
	byID := make(map[string]*slack.File, len(old.Files))
	for i := range old.Files {
		if !fileStub(&old.Files[i]) {
			byID[old.Files[i].ID] = &old.Files[i]
		}
	}
	fs := append([]slack.File(nil), m.Files...)
	for i := range fs {
		if f, ok := byID[fs[i].ID]; ok && fileStub(&fs[i]) {
			fs[i] = *f
		}
	}
	m.Files = fs
}
