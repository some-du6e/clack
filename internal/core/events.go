package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

var mentionRe = regexp.MustCompile(`<@([UW][A-Z0-9]+)`)

// pump applies realtime events to the store and forwards them to
// subscribers, until the session's traffic stops (StopTraffic, a sign-out)
// or the realtime events end. It used to wait on the events alone, and an
// App that never connected (no realtime) kept it, and itself, for good.
func (a *App) pump() {
	done := a.bg.Done()
	for {
		select {
		case ev, ok := <-a.RTM.Events:
			if !ok {
				return
			}
			a.handle(ev)
		case <-done:
			return
		}
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
			a.pumpLater(func(ctx context.Context) { _ = a.LoadFrecencyPrefs(ctx) }) // pump_later.go
		}
	case "slack_connect_pref_changed": // handleSlackConnectPrefsUpdate (upload_gates.go)
		a.onSlackConnectPrefChanged(ev.Raw)
	case "search_recents": // handleSearchRecentsPrefsUpdate (search_recents.go)
		var e struct {
			Value string `json:"value"`
		}
		if json.Unmarshal(ev.Raw, &e) == nil && e.Value == "updated" && a.RecentsReadEnabled() {
			a.pumpLater(func(ctx context.Context) { _ = a.LoadRecentsPrefs(ctx) }) // pump_later.go
		}
	case "reaction_added", "reaction_removed":
		var e struct {
			User     string `json:"user"`
			Reaction string `json:"reaction"`
			URL      string `json:"url"` // reaction_added: an emoji the workspace hasn't got (a Slack Connect partner's)
			Item     struct {
				Channel string `json:"channel"`
				TS      string `json:"ts"`
			} `json:"item"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		add := ev.Type == "reaction_added"
		if !add {
			e.URL = "" // removeReaction takes none
		}
		// every copy takes it and a fetch out now keeps it, under one lock
		// (store/reacts.go); its url too (REACTIONS-SHARED-RTM passes the
		// event's url to cBAo addReaction)
		m, ok, changed := a.S.ApplyReactionNotedURL(e.Item.Channel, e.Item.TS, e.Reaction, e.User, e.URL, add)
		// the reactor isn't fetched: only the reaction's tooltip names it
		// (ReactionTip's TooltipMemberName, lazy_users.go)
		if ok && changed { // the echo of the viewer's own (optimistic) reaction changes nothing
			v := a.message(e.Item.Channel, &m)
			a.emit(Event{Type: "update", Name: UpdateReactions, Channel: e.Item.Channel, TS: m.TS, ThreadTS: m.ThreadTS, Msg: &v})
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
		// Slack's typing line maps its typers through getMemberById as it
		// renders (MessagePaneInput, cRQG): the record goes out at once
		// with the name the store has, nobody asked for, and the line asks
		// for a typer the store doesn't hold as it draws (composer typers'
		// User), naming them once "names" brings them. Fetched here, on the
		// pump, every event behind the typing waited for users/info (a
		// reply in the thread open beside it too); asked for here, a typer
		// in a watched conversation nothing shows (a thread whose pane
		// closed, a line with typing display off) brought a "names" that
		// made the UI read everything on screen again. A busy workspace
		// types in hundreds of channels.
		name := ""
		if u, ok := a.S.RawUser(e.User); ok {
			name = store.DisplayName(u)
		}
		a.emit(Event{Type: "typing", Channel: ch, UserID: e.User, Name: name, ThreadTS: e.ThreadTS})
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
		if cv.Unread || cv.Mentions > 0 { // marked unread elsewhere: the DM badges read its member (members.go)
			a.readBadgePerson(ch)
		}
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
			// Seen: when a view last read the member (store.MemberAccessed:
			// a row its sender, a mention its member, as they draw), so the
			// views know whether one draws them now. One the store didn't
			// hold, or no view read, draws nowhere (a row that wants it
			// draws the unknown stub, and its fetch's "names" follows), as
			// most of a large workspace's profile changes are. The event is
			// read without a lookup, which would count as a view's.
			seen := a.S.MemberAccessed(e.User.ID)
			// what message Docs read of the member, against both the last
			// event's and the stored copy's (a status or huddle change
			// converts no message again; a name stored anew since without
			// an event, an older answer stored after it included, does)
			before, had := a.S.RawUser(e.User.ID)
			var beforeFP uint64
			if had && before != nil {
				beforeFP = userDocFP(before)
			}
			a.S.PutUsers([]slack.User{e.User})
			if su, ok := a.S.RawUser(e.User.ID); ok {
				u := a.userOf(su)
				same := a.noteUserDoc(su) && had && beforeFP == userDocFP(su)
				a.emit(Event{Type: "user", UserID: u.ID, User: &u, Seen: seen, SameNames: same})
			}
		}
	case "team_icon_change":
		a.onTeamIconChange(ev.Raw)
	case "feature_access_policies_updated":
		a.onFeatureAccessUpdated(ev.Raw) // chposting.go
	case "apps_installed", "apps_changed", "apps_uninstalled", "bot_changed":
		a.onAppCardEvent(ev.Type, ev.Raw) // app_card.go: the app's card goes stale
	case "team_pref_change":
		a.onTeamPrefChange(ev.Raw) // teamprefs.go
	case "workflow", "hermes_trigger_updated":
		a.onAgentTaskEvent(ev.Type, ev.Raw) // agent_tasks.go: the Tasks tab
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
		// the names, a removal's too: the messages that drew them convert
		// again, not every loaded one (a removal named none)
		a.emit(Event{Type: "emoji_changed", Name: e.Name, Users: e.Names})
	case "member_joined_channel":
		var e struct {
			User string `json:"user"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		// anyone's join: the channel's member counts are stale
		// (batchProcessChannelMembers, membership_counts.go)
		a.membershipChanged(ch)
		if e.User != a.S.SelfID() {
			return
		}
		// off the pump (pump_later.go); a leave the pump applies meanwhile
		// stands (refreshConvSince)
		since := a.S.Seq()
		a.pumpLater(func(ctx context.Context) { a.refreshConvSince(ctx, ch, since) })
	case "member_left_channel":
		a.membershipChanged(ch) // anyone's leave, as a join
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
			// a new membership: Slack upserts the event's channel, is_open
			// with it; Clack keeps no is_open from it, so an old close of
			// a code channel no longer counts (codechannel_close.go)
			a.forgetCodeChannelClosed(id)
		}
		// the conversation as the event has it, now (noteJoinedConv); its
		// conversations.info after, off the pump, unless the pump changes
		// it meanwhile (a leave, an archive: refreshConvSince)
		a.noteJoinedConv(ev.Type, e.Channel, id)
		since := a.S.Seq()
		a.pumpLater(func(ctx context.Context) { a.refreshConvSince(ctx, id, since) }) // pump_later.go
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
			a.S.DropConv(ch)
			a.emit(Event{Type: "conv_removed", Channel: ch})
			to, since := e.ConvertedTo, a.S.Seq()
			a.pumpLater(func(ctx context.Context) { a.refreshConvSince(ctx, to, since) }) // pump_later.go
			return
		}
		a.emit(Event{Type: "sidebar_changed"})
	case "channel_left", "group_left", "channel_archive", "group_archive", "channel_deleted":
		if ev.Type == "channel_deleted" { // channelDeleted: the list prefs are stale (upload_gates.go)
			a.listPrefsStale(ch)
		}
		a.dropMembership(ch, ev.Type == "channel_archive" || ev.Type == "group_archive")
		a.forgetCodeChannelClosed(ch) // no membership, no is_open to keep (codechannel_close.go)
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
		// the Pins tab's rows (prependPinnedItem / removePinnedItem while
		// they're loaded, channel_pins.go)
		var it struct {
			Item pinsListItem `json:"item"`
		}
		if json.Unmarshal(ev.Raw, &it) == nil {
			a.pinsTabEvent(ch, ev.Type == "pin_added", it.Item)
		}
	case "thread_marked", "update_thread_state", "dnd_updated", "dnd_updated_user", "pref_change", "badge_counts_updated",
		"channel_sections_upserted", "channel_sections_channels_upserted", "channel_sections_channels_removed", "channel_sections_deleted":
		if ev.Type == "dnd_updated" {
			a.onDNDUpdated(nil, ev.Raw) // the event's status, before the UI hears of it
			if raw := ev.Raw; a.dndUpdateLoads(raw) {
				// then dnd.info (pump_later.go), whose answer gives way to a
				// newer dnd_updated (loadDNDAfter)
				gen := a.dndEvents()
				a.pumpLater(func(ctx context.Context) { _ = a.loadDNDAfter(ctx, gen) })
			}
		}
		if ev.Type == "dnd_updated_user" {
			a.onDNDUpdatedUser(ev.Raw) // stale: the next presence read asks dnd.teamInfo again
		}
		if ev.Type == "thread_marked" {
			a.onThreadMarked(ev.Raw) // the root's last_read, before the UI hears of it
		}
		if ev.Type == "update_thread_state" && a.onUpdateThreadState(ev.Raw) {
			a.emit(Event{Type: "threads_state"}) // the threads' unread state moved: at once (thread_unreads.go)
		}
		if ev.Type == "badge_counts_updated" {
			// handleBadgeCountsUpdatedRTMEvent's activity_v2, in order with
			// the activity entries before it (activity_later.go): its zero
			// marks the rows they made unread read
			raw := ev.Raw
			a.activityInOrder(ctx, func(context.Context) { a.onActivityBadges(raw) })
		}
		a.emit(Event{Type: ev.Type, Raw: ev.Raw})
		switch ev.Type {
		case "channel_sections_upserted", "channel_sections_channels_upserted", "channel_sections_channels_removed", "channel_sections_deleted":
			// read off the pump (pump_later.go), one reread at a time, each
			// as of when it starts: a section write this session makes
			// while it is out stays (SetSectionsSince). Taken when the event
			// came, the journal position was older than the install of the
			// reread queued before it, and a second sections event's newer
			// list was always thrown away
			a.pumpLater(func(ctx context.Context) {
				since := a.S.Seq()
				if secs, err := a.C.SectionsAll(ctx); err == nil && a.S.SetSectionsSince(secs, since) {
					a.emit(Event{Type: "sidebar_changed"})
				}
			})
		case "thread_marked", "update_thread_state", "badge_counts_updated":
			a.refreshCountsSoon()
		case "pref_change":
			var pc struct {
				Name  string          `json:"name"`
				Value json.RawMessage `json:"value"`
			}
			_ = json.Unmarshal(ev.Raw, &pc)
			if pc.Name != "" && len(pc.Value) > 0 && pc.Name != prefFrecency && pc.Name != prefEmojiUse && pc.Name != prefChannelSections {
				// Slack's prefChange sets the event's value at once: the
				// messages right behind it notify and draw with it, not
				// with the prefs from before users.prefs answers. The
				// frecency and the sections' collapse state wait for the
				// read, which keeps a frecency only a named fetch carries
				// (keepFrecency) and a collapse write still out
				// (sectionCollapsePending)
				a.mergePrefs(map[string]json.RawMessage{pc.Name: pc.Value})
				p := a.Prefs()
				a.emit(Event{Type: "prefs", Prefs: &p})
			}
			a.pumpLater(func(ctx context.Context) { // pump_later.go
				raw, err := a.C.Prefs(ctx)
				if err != nil {
					return
				}
				a.installPrefs(raw)
				if pc.Name == prefFrecency || pc.Name == prefEmojiUse {
					// Slack upserts the event's value; the full map keeps
					// the frecency in use, so read it as Slack serves it
					go func() { _ = a.LoadFrecencyPrefs(a.bg) }()
				} else {
					a.frecencyAfterPrefs()
				}
				p := a.Prefs()
				a.emit(Event{Type: "prefs", Prefs: &p})
			})
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
		if e.Subteam.ID != "" && e.Subteam.Handle != "" && ev.Type != "subteam_members_changed" {
			// subteamUpdated (known groups only) / subteamCreated
			// (usergroup_edit.go). subteamMembersChanged returns at once in
			// the desktop client (isSonic, client-boot QWaK): the User
			// Group pane refetches the members when subteam_updated
			// changes the group.
			a.noteUsergroupEvent(ev.Raw, ev.Type == "subteam_created")
		}
	case "saved_added", "saved_updated", "saved_deleted":
		// the store's saved state now, on the pump; its message's lookup
		// and the event after it, in order with the saved events before
		// (pump_later.go)
		typ, raw := ev.Type, ev.Raw
		it := a.noteSavedEvent(typ, raw)
		a.pumpLater(func(ctx context.Context) { a.announceSaved(ctx, typ, raw, it) })
	case "draft_create", "draft_update", "draft_delete", "draft_send":
		a.onDraftEvent(ev.Type, ev.Raw)
	case "activity":
		raw := ev.Raw
		// stamped here, in socket order, not when its turn comes: a
		// deletion or an edit dropping the viewer's mention that lands
		// while the entry waits for the lookups ahead of it is newer than
		// the entry, and takes it out (Activity drops an entry whose
		// message changed after its stamp: RemoveSeq > MsgSeq). Stamped
		// in the worker, the entry came out newer than the deletion and
		// stayed, a deleted reply's thread row (Karim, 2026-11-17)
		seq := a.MessageChangeSeq()                                                     // msg_changes.go
		a.activityLater(func(ctx context.Context) { a.onActivityEvent(ctx, raw, seq) }) // activity_later.go
	case "activity_clear_all_completed":
		// handleActivityRTMEvent's sibling (client-boot-render
		// activityClearAllCompleted): with an undo_key, the clear-all
		// toast and its Undo (activity.clearAll).
		var e struct {
			UndoKey string `json:"undo_key"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		if e.UndoKey != "" {
			raw := ev.Raw // in order with the activity entries before it (activity_later.go)
			a.activityInOrder(ctx, func(context.Context) {
				a.emit(Event{Type: "activity", Name: "activity_clear_all_completed", ID: e.UndoKey, Raw: raw})
			})
		}
	case "activity_views_updated":
		// sD/F activityViewsUpdated: the saved views changed (another
		// client made, renamed, hid or deleted one); ActivityViewsEvent
		// reads them
		a.emit(Event{Type: "activity", Name: ev.Type, Raw: ev.Raw})
	case "bookmark_added", "bookmark_removed", "bookmark_updated", "channel_updated", "channel_tab_added", "channel_tab_removed":
		if strings.HasPrefix(ev.Type, "bookmark_") { // the folders' bookmarks store (bookmark_folders.go)
			if c := a.bookmarkEvent(ev.Type, ev.Raw); c != "" && ch == "" {
				ch = c
			}
		}
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
		// viewer): the UI decides and shows it (ui/notify_threads.go).
		// Behind the events of its conversation still held for its
		// lookup, as Slack's routeMessages dispatches the socket's events
		// in order once the message before it has its channel: let
		// through, a held group DM's first message toasted after its
		// thread's reply that came later, and the UI looked the
		// conversation up a second time (pump_later.go)
		raw := ev.Raw
		if a.holdBehindConv(ch) {
			a.convLater(ch, func(context.Context) {
				defer a.releaseConv(ch)
				a.emit(Event{Type: "desktop_notification", Channel: ch, Raw: raw})
			})
			return
		}
		a.emit(Event{Type: ev.Type, Channel: ch, Raw: raw})
	case "dnd_override":
		// a sender's "notify anyway" for a message Do Not Disturb held
		// back (boot-render handleDndOverride): the UI shows the stored
		// message's notification with DND ignored (ui/notify_dm.go),
		// after the message itself when that waits for its conversation
		var e struct {
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(ev.Raw, &e) == nil && ch != "" && e.Timestamp != "" {
			over := Event{Type: ev.Type, Channel: ch, TS: e.Timestamp}
			if a.holdBehindConv(ch) {
				a.convLater(ch, func(context.Context) {
					defer a.releaseConv(ch)
					a.emit(over)
				})
				return
			}
			a.emit(over)
		}
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

func (a *App) emitUpdate(ch string, m *slack.Message) { a.emitUpdateWith(ch, m, false) }

// emitUpdateWith is emitUpdate; removes: the update removes the Activity
// entries about the message (Event.Removes: an edit took the viewer's
// mention out).
func (a *App) emitUpdateWith(ch string, m *slack.Message, removes bool) {
	v := a.message(ch, m)
	a.emit(Event{Type: "update", Channel: ch, TS: m.TS, ThreadTS: m.ThreadTS, Msg: &v, Ahead: a.S.IsAhead(ch, m.TS), Removes: removes})
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
func (a *App) emitStreamDone(ch string, m *slack.Message) { a.emitStreamDoneWith(ch, m, false) }

// emitStreamDoneWith is emitStreamDone; removes as emitUpdateWith's.
func (a *App) emitStreamDoneWith(ch string, m *slack.Message, removes bool) {
	v := a.message(ch, m)
	self := a.S.SelfID()
	ev := Event{Type: "update", Channel: ch, TS: m.TS, ThreadTS: m.ThreadTS, UserID: m.User, Msg: &v, StreamDone: true, Removes: removes,
		Reply:    m.ThreadTS != "" && m.ThreadTS != m.TS,
		Mention:  mentionsSelf(m, self) || (m.User != self && keywordMention(m, a.S.HighlightWords())),
		ConvKind: a.kindOf(ch)}
	if u, ok := a.RawUser(m.User); ok { // as the store has them: see onMessage
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
	if c.ID == "" {
		c.ID = id
	}
	a.putInfo(c)
	// An IM's partner is neither fetched nor asked for here: refreshConv
	// runs on the event pump (im_created, im_open, a message in a
	// conversation core didn't know), where a fetch held every event
	// behind it for users/info, and an ask brought a "names" pass for a DM
	// nothing may show. What draws the partner asks as it draws (the
	// sidebar's DM row, the DMs page), and a notification waits for them
	// itself (the UI's noteMembers).
	v := a.conv(c)
	a.emit(Event{Type: "conv", Channel: id, Conv: &v})
}

// refreshConvSince is refreshConv for a lookup queued off the event pump
// (pump_later.go) at journal position since: the answer stays out when the
// pump changed the conversation after since (a channel_left's or an
// archive's DropConv, a join's PutConvLocal: store PutInfoSince). Read
// before a leave the pump applied meanwhile, it put the left channel back
// in the member set: its sidebar row, the composer, not_in_channel on send.
func (a *App) refreshConvSince(ctx context.Context, id string, since uint64) {
	if id == "" {
		return
	}
	c, err := a.C.ConversationInfo(ctx, id)
	if err != nil {
		return
	}
	if c.ID == "" {
		c.ID = id
	}
	if !a.S.PutInfoSince(*c, since) {
		return
	}
	v := a.conv(c)
	a.emit(Event{Type: "conv", Channel: id, Conv: &v})
}

// noteJoinedConv puts the conversation a join or an open names into the
// member set now, ahead of its conversations.info (refreshConvSince): the
// event's own channel object for channel_joined, group_joined, im_created
// and mpim_joined (Slack's channelJoined upserts it), or for im_open and
// mpim_open the closed DM the store keeps outside the sidebar. On ui the
// lookup ran on the pump before the next event; queued, the messages right
// behind it came first, and a new group DM's first message counted as a
// channel's (no badge: NoteMessage) and waited for its own lookup. A
// conversation the member set holds already stays as it is until the lookup
// answers.
func (a *App) noteJoinedConv(typ string, c slack.Conversation, id string) {
	switch typ {
	case "channel_joined", "group_joined":
		c.IsMember = true
	case "im_created":
		c.IsIM = true
	case "mpim_joined":
		c.IsMPIM = true
	case "im_open", "mpim_open":
		x, ok := a.S.ExtConv(id)
		if !ok || !x.IsIM && !x.IsMPIM {
			return
		}
		c = *x
	default:
		return
	}
	if c.ID == "" || c.ID != id {
		return
	}
	if _, member := a.S.Conv(c.ID); member {
		return
	}
	a.S.PutConvLocal(c)
	v := a.conv(&c)
	a.emit(Event{Type: "conv", Channel: c.ID, Conv: &v})
}

// putInfo stores a conversations.info answer. Its is_member is the
// membership (Slack's store keeps the object as fetched, and Opi6
// isPreview reads is_member off it): a channel the viewer isn't in stays
// out of the member set, a known non-member (PutExtConv) like
// ResolveConv's. refreshConv runs on events every member of the team
// gets (channel_created, channel_rename, channel_unarchive) and on a
// message in a channel outside the member set; storing those answers with
// PutConv made the channel a member's: a sidebar row, the composer in
// place of JoinChannelPane, and not_in_channel on send (Karim, α.90,
// #僕のゴミ). A conversation already in the member set stays there
// (PutConv keeps its is_member; leaving is channel_left's
// dropMembership), and DMs carry no is_member.
func (a *App) putInfo(c *slack.Conversation) {
	if _, member := a.S.Conv(c.ID); member || c.IsMember || c.IsIM || c.IsMPIM {
		a.S.PutConv(*c)
		return
	}
	a.S.PutExtConv(*c)
}

// knownNonMember reports a channel known from conversations.info as one
// the viewer isn't in (putInfo, ResolveConv): its messages (a preview's)
// need no new lookup each. A DM outside the member set still gets one, so
// a new message brings it in.
func (a *App) knownNonMember(ch string) bool {
	c, ok := a.S.ExtConv(ch)
	return ok && c.Name != "" && !c.IsIM && !c.IsMPIM && !c.IsMember
}

func (a *App) onMessage(ctx context.Context, ch string, ev slack.Event) {
	var e msgSubEvent
	if json.Unmarshal(ev.Raw, &e) != nil {
		return
	}
	switch e.Subtype {
	case "message_changed", "message_deleted", "message_locked", "message_replied":
		// A change to a message of a conversation whose messages wait for
		// its lookup (holdForConv) waits behind them, as Slack's
		// preprocessRtmMessage holds each of the conversation's message
		// events until it is fetched: applied at once, a deletion came
		// before the reply it deleted, whose late announcement put it back
		// in the open thread, and an edit before the copy announced as it
		// was (pump_later.go)
		if a.holdBehindConv(ch) {
			raw := ev.Raw
			a.convLater(ch, func(ctx context.Context) {
				defer a.releaseConv(ch)
				a.onMessageChange(ctx, ch, &e, raw)
			})
			return
		}
		a.onMessageChange(ctx, ch, &e, ev.Raw)
		return
	}
	var m slack.Message
	if json.Unmarshal(ev.Raw, &m) != nil || m.TS == "" {
		return
	}
	// the notifications' maxTs takes every message the socket brings, as
	// Slack's store adds them all (store/notifications_maxts.go): the
	// desktop_notification for it that comes next isn't one for a
	// message the client never took (19)
	a.S.NoteAdded(m.TS)
	// appendMessages merges a socket message into the stored one: a pin
	// that came first stays (store/pins.go), and a thread's subscription
	// changes only with a "subscribed" of the copy's own (thread_subs.go)
	a.S.KeepPin(ch, &m)
	a.S.SlurpSub(ch, &m)
	a.streams.seen(ch, m.TS, m.StreamingState == "in_progress")
	// the DM badges read the member of the IM this makes unread (members.go
	// readBadgePerson: the badge's own ask, which names them)
	if m.ThreadTS == "" || m.ThreadTS == m.TS || m.Subtype == "thread_broadcast" {
		a.readBadgePerson(ch)
	}
	// Nobody the message names is fetched or asked for on its way out:
	// Slack's messageReceived puts it into the store the moment it
	// arrives, its rows ask for the people they draw (row.resolve's User,
	// a mention's MemberRead; the conversion and the event read the store
	// as it is: msgNames, RawUser), and only the desktop notification
	// waits for them (showNotification's ensureModelsArePresent: the UI's
	// maybeNotify, noteMembers). Fetched here, on the event pump, a reply
	// from someone the client never saw waited for users/info, and every
	// event behind it with it: an open thread drew replies seconds late,
	// after Activity had them (Karim, 2026-11-18, "threads are a shit show
	// rn"); asked for here, every sender in any channel brought a "names"
	// pass for people nothing drew.
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
			a.noteCodeChannelMessage(ch, m.TS, a.RTM.Connected()) // heard live (codechannel_close.go)
		}
		return
	}
	if a.wasConfirmed(&m) { // late echo after the API reply confirmed it
		a.putLocal(ch, m)
		a.emitUpdate(ch, &m)
		return
	}
	if isReply {
		a.S.AddReplyNoted(ch, m) // a replies answer older than this reply keeps it (store/live.go)
	}
	ahead := false
	if !isReply || m.Subtype == "thread_broadcast" {
		// past the end of a window whose end went stale at a slow
		// connect it waits until the window is read forward to it
		// (store/reached_end.go): appended now, it drew right after the
		// window with what the socket missed left out
		if ahead = a.S.UpsertHeard(ch, m); ahead {
			a.mu.Lock()
			open := a.watchCh == ch
			a.mu.Unlock()
			if open {
				a.startCatchUp(ch) // reached_end.go
			}
		}
	}
	self := a.S.SelfID()
	mention := mentionsSelf(&m, self) || (m.User != self && keywordMention(&m, a.S.HighlightWords()))
	if !isReply || m.Subtype == "thread_broadcast" {
		a.S.NoteMessage(ch, &m, mention)
		a.noteCodeChannelMessage(ch, m.TS, a.RTM.Connected()) // heard live (codechannel_close.go)
	} else if m.User != self {
		a.refreshCountsSoon() // thread unread state lives server-side
	}
	// a conversation core doesn't know yet is looked up first
	// (conversations.info), as Slack's preprocessRtmMessage fetches an
	// unknown channel before the message: off the pump, so no other
	// conversation's events wait for it, and the later messages and changes
	// of the same conversation wait behind it in order (pump_later.go). The
	// store holds the message already (above): only its announcement waits
	if a.holdForConv(ch) {
		mm, since := m, a.S.Seq()
		countedDM := mention || a.S.CountsAsDM(ch) // as NoteMessage counted it, above
		a.convLater(ch, func(ctx context.Context) {
			defer a.releaseConv(ch)
			if _, known := a.S.Conv(ch); !known && !a.knownNonMember(ch) {
				a.refreshConvSince(ctx, ch, since)
			}
			if !countedDM && (!isReply || mm.Subtype == "thread_broadcast") {
				// counted before the store knew ch: a group DM's message
				// counts as one (NoteLateDM), as it would have on ui, where
				// the lookup ran first
				a.S.NoteLateDM(ch, &mm)
			}
			a.announceMessage(ch, &mm, isReply, mention, ahead)
		})
		return
	}
	a.announceMessage(ch, &m, isReply, mention, ahead)
}

// announceMessage is onMessage's last part: the message event, once its
// conversation is known.
func (a *App) announceMessage(ch string, mp *slack.Message, isReply, mention, ahead bool) {
	m := *mp
	if !isReply || m.Subtype == "thread_broadcast" {
		if a.reopenActive() { // a new message brings a closed DM back
			a.emit(Event{Type: "sidebar_changed"})
		}
	}
	a.clearAssistantStatusOnMessage(ch, m.ThreadTS) // agent.go
	v := a.message(ch, &m)
	cv := a.Count(ch)
	ev2 := Event{Type: "message", Channel: ch, TS: m.TS, ThreadTS: m.ThreadTS, UserID: m.User, Msg: &v, Count: &cv,
		Reply: isReply, Mention: mention, ConvKind: a.kindOf(ch), Ahead: ahead}
	// the sender as the store has them, nothing asked: messageReceived asks
	// for no one, the rows ask as they draw (row.resolve's User), the
	// notification for itself (the UI's noteMembers). Read through
	// getMemberById, every never-fetched sender in any channel went to
	// users/info, and its "names" answer made the UI read every
	// conversation on screen again (a resnapshot, the open channel's
	// history converted afresh, every row resolved again) for people
	// nothing drew (α.110's review)
	if u, ok := a.RawUser(m.User); ok {
		ev2.User = &u
	}
	a.emit(ev2)
}

// msgSubEvent is a message event's fields for its change subtypes.
type msgSubEvent struct {
	Subtype   string         `json:"subtype"`
	Message   *slack.Message `json:"message"`
	Previous  *slack.Message `json:"previous_message"`
	DeletedTS string         `json:"deleted_ts"`
}

// onMessageChange is onMessage for the subtypes that change a message the
// store may hold: an edit, a deletion, a thread's lock or its replies.
func (a *App) onMessageChange(ctx context.Context, ch string, e *msgSubEvent, raw json.RawMessage) {
	switch e.Subtype {
	case "message_changed":
		if e.Message == nil {
			return
		}
		m := *e.Message // its rows ask for its people as they draw (lazy_users.go)
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
		if old, ok := a.S.FindHeld(ch, m.TS); ok { // a copy fetched by id too (store/fetched.go)
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
			// a root's thread subscription isn't the message's: Slack's
			// replaceMessages swaps the message, and only a copy with a
			// "subscribed" field of its own changes the subscription
			// (store/thread_subs.go), so the cached copies keep theirs
			if !m.SubscribedSet {
				sub := a.S.ThreadSubOf(ch, &old)
				m.Subscribed, m.LastRead = sub.Subscribed, sub.LastRead
			}
		}
		a.S.SlurpSub(ch, &m)
		a.forgetMsgThread(ch, m.TS) // its thread_ts as this copy has it, not as a lookup found it
		// every stored copy takes it, a copy fetched by id included (an
		// Activity entry's message from a conversation nobody opened:
		// store/fetched.go), and a read out now keeps it (store/live.go),
		// noted with the write so no read lands between; a window or a
		// cached thread that holds none gets it inside its range
		// (putChanged)
		if _, found := a.S.NoteMutate(ch, m.TS, &m, func(x *slack.Message) { *x = m }); !found || !a.S.Holds(ch, m.TS, "") {
			a.putChanged(ch, m) // a reply goes to its (cached) thread, never the channel
		}
		if m.ThreadTS == "" || m.ThreadTS == m.TS {
			// a root's copy replaces the stored root: its broadcasts'
			// root copies take its reply metadata (broadcast_footer.go)
			a.syncBroadcastRoots(ch, m.TS, func(x *slack.Message) {
				x.ThreadTS, x.ReplyCount, x.ReplyUsers, x.ReplyUsersN, x.LatestReply = m.ThreadTS, m.ReplyCount, m.ReplyUsers, m.ReplyUsersN, m.LatestReply
			})
		}
		if needsFileInfo(&m) {
			go a.fillFiles(ch, m.TS)
		}
		a.woMessageUpdated(ch, &m) // a work object's refreshed unfurl (workobjects.go)
		// the update says it removes the Activity entries about the message
		// (activity_removes.go) on itself: through one slot App-wide, a
		// held conversation's edit and another's, applied at once on two
		// goroutines, could take each other's
		removes := a.removesActivity(e.Previous, &m)
		if streamed && m.StreamingState != "in_progress" {
			a.emitStreamDoneWith(ch, &m, removes)
			return
		}
		a.emitUpdateWith(ch, &m, removes)
		return
	case "message_deleted":
		// Slack's setMessageDeleted just removes the message; the parent's
		// reply_count / latest_reply come with the parent's message_changed.
		thread := ""
		if e.Previous != nil && e.Previous.ThreadTS != e.DeletedTS {
			thread = e.Previous.ThreadTS
		} else if old, ok := a.S.FindHeld(ch, e.DeletedTS); ok && old.ThreadTS != old.TS {
			thread = old.ThreadTS
		}
		// noted with the copies going: a fetch landing after leaves it out
		// (store/live.go)
		a.S.NoteRemove(ch, e.DeletedTS)
		a.emit(Event{Type: "delete", Channel: ch, TS: e.DeletedTS, ThreadTS: thread})
		return
	case "message_locked":
		a.onMessageLocked(ch, raw)
		return
	case "message_replied":
		a.onMessageReplied(ctx, ch, raw) // thread_meta.go
		return
	}
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
func fileStub(f *slack.File) bool { return store.FileStub(f) }

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
			m, ok := a.S.FindHeld(ch, ts) // a message fetched by id too (store/fetched.go)
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
	if upd, ok := a.S.MutateNoted(ch, ts, func(x *slack.Message) { // a read out now keeps them
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
// refreshCanvasRef asks files.info for canvas id's title again, once for
// the file_change events of 400 ms (people editing a canvas send many;
// each cost a files.info, and answers landing out of order could install
// an older title), and has the messages that read it converted again
// when it changed.
func (a *App) refreshCanvasRef(id string) {
	st := a.canvases()
	st.mu.Lock()
	if st.titles[id] {
		st.mu.Unlock()
		return
	}
	st.titles[id] = true
	st.mu.Unlock()
	time.AfterFunc(400*time.Millisecond, func() {
		st.mu.Lock()
		delete(st.titles, id)
		st.mu.Unlock()
		old, _ := a.S.KnownCanvasRef(id)
		ctx, cancel := context.WithTimeout(a.bg, 15*time.Second)
		defer cancel()
		if a.resolveCanvasRef(ctx, id) {
			if now, _ := a.S.KnownCanvasRef(id); !reflect.DeepEqual(now, old) {
				a.emit(Event{Type: "names", Users: []string{id}})
			}
		}
	})
}

func (a *App) onFileChange(ev slack.Event) {
	id := canvasEventFile(ev)
	if id == "" {
		return
	}
	// a canvas or list that titles tab slugs and links (its CanvasRef,
	// asked for once): its title as files.info has it now, and the
	// messages whose conversion read it (Message.Refs) converted again.
	// Slack's useTabLabel reads the files store, which file_change
	// updates; Clack kept a renamed canvas's first title for the session.
	if _, ok := a.S.KnownCanvasRef(id); ok {
		a.refreshCanvasRef(id)
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
