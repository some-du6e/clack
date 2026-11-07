package core

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
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
		m, ok := a.S.Mutate(e.Item.Channel, e.Item.TS, func(m *slack.Message) { applyReaction(m, e.Reaction, e.User, add) })
		a.EnsureUsers(ctx, []string{e.User})
		if ok {
			a.emitUpdate(e.Item.Channel, &m)
		}
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
		}
		_ = json.Unmarshal(ev.Raw, &e)
		a.refreshConv(ctx, firstNonEmpty(e.Channel.ID, ch))
	case "channel_left", "group_left", "channel_archive", "group_archive", "channel_deleted", "im_close":
		a.S.Lock()
		delete(a.S.Convs, ch)
		a.S.Unlock()
		a.emit(Event{Type: "conv_removed", Channel: ch})
	case "pin_added", "pin_removed":
		var e struct {
			ChannelID string `json:"channel_id"`
			Item      struct {
				Channel string        `json:"channel"`
				Message slack.Message `json:"message"`
			} `json:"item"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		ch = firstNonEmpty(ch, e.ChannelID, e.Item.Channel)
		if e.Item.Message.TS != "" {
			m, ok := a.S.Mutate(ch, e.Item.Message.TS, func(m *slack.Message) {
				if ev.Type == "pin_added" {
					m.PinnedTo = []string{ch}
				} else {
					m.PinnedTo = nil
				}
			})
			if ok {
				a.emitUpdate(ch, &m)
			}
		}
	case "thread_marked", "update_thread_state", "dnd_updated", "dnd_updated_user", "pref_change", "badge_counts_updated",
		"channel_sections_upserted", "channel_sections_channels_upserted", "channel_sections_channels_removed", "channel_sections_deleted":
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
				a.S.SetPrefs(store.ParsePrefs(raw))
				p := a.Prefs()
				a.emit(Event{Type: "prefs", Prefs: &p})
			}
		}
	case "subteam_created", "subteam_updated", "subteam_members_changed", "subteam_self_added", "subteam_self_removed":
		var e struct {
			Subteam slack.Usergroup `json:"subteam"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
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
	case "bookmark_added", "bookmark_removed", "channel_updated", "channel_tab_added", "channel_tab_removed":
		// the channel tab bar changed: the UI re-reads Tabs / Bookmarks
		a.emit(Event{Type: "tabs", Channel: ch, Name: ev.Type, Raw: ev.Raw})
	case "thread_subscribed", "thread_unsubscribed":
		a.onThreadSubscription(ev.Raw, ev.Type == "thread_subscribed")
	case "star_added", "star_removed":
		a.onStarEvent(ev.Raw, ev.Type == "star_added")
	default:
		if strings.HasPrefix(ev.Type, "sh_") || ev.Type == "huddle_invite" {
			a.emit(Event{Type: "huddle", Name: ev.Type, Channel: ch, Raw: ev.Raw})
		}
	}
}

func (a *App) emitUpdate(ch string, m *slack.Message) {
	v := a.message(ch, m)
	a.emit(Event{Type: "update", Channel: ch, TS: m.TS, ThreadTS: m.ThreadTS, Msg: &v})
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

func applyReaction(m *slack.Message, name, user string, add bool) {
	for i := range m.Reactions {
		r := &m.Reactions[i]
		if r.Name != name {
			continue
		}
		if add {
			for _, u := range r.Users {
				if u == user {
					return
				}
			}
			r.Users = append(r.Users, user)
			r.Count++
			return
		}
		for j, u := range r.Users {
			if u == user {
				r.Users = append(r.Users[:j], r.Users[j+1:]...)
				r.Count--
				break
			}
		}
		if r.Count <= 0 {
			m.Reactions = append(m.Reactions[:i], m.Reactions[i+1:]...)
		}
		return
	}
	if add {
		m.Reactions = append(m.Reactions, slack.Reaction{Name: name, Count: 1, Users: []string{user}})
	}
}

func (a *App) onMessage(ctx context.Context, ch string, ev slack.Event) {
	var e struct {
		Subtype   string         `json:"subtype"`
		Message   *slack.Message `json:"message"`
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
		if old, ok := a.S.Find(ch, m.TS); ok && m.ReplyCount == 0 && old.ReplyCount > 0 {
			// RTM edits may omit thread metadata; keep it.
			m.ReplyCount, m.ReplyUsers, m.LatestReply = old.ReplyCount, old.ReplyUsers, old.LatestReply
		}
		if _, found := a.S.Mutate(ch, m.TS, func(x *slack.Message) { *x = m }); !found {
			a.S.Upsert(ch, m)
		}
		a.emitUpdate(ch, &m)
		return
	case "message_deleted":
		a.S.Remove(ch, e.DeletedTS)
		a.emit(Event{Type: "delete", Channel: ch, TS: e.DeletedTS})
		return
	case "message_replied":
		if e.Message == nil {
			return
		}
		m := *e.Message
		a.EnsureUsers(ctx, m.ReplyUsers)
		if upd, ok := a.S.Mutate(ch, m.TS, func(x *slack.Message) {
			x.ReplyCount, x.ReplyUsers, x.LatestReply = m.ReplyCount, m.ReplyUsers, m.LatestReply
		}); ok {
			a.emitUpdate(ch, &upd)
		}
		return
	}
	var m slack.Message
	if json.Unmarshal(ev.Raw, &m) != nil || m.TS == "" {
		return
	}
	a.EnsureUsers(ctx, referencedUsers([]slack.Message{m}))
	isReply := m.ThreadTS != "" && m.ThreadTS != m.TS
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
		a.bumpParent(ch, &m) // message_replied may lag
	}
	if !isReply || m.Subtype == "thread_broadcast" {
		a.S.Upsert(ch, m)
	}
	self := a.S.SelfID()
	mention := mentionsSelf(&m, self)
	if !isReply || m.Subtype == "thread_broadcast" {
		a.S.NoteMessage(ch, &m, mention)
	} else if m.User != self {
		a.refreshCountsSoon() // thread unread state lives server-side
	}
	if _, known := a.S.Conv(ch); !known {
		a.refreshConv(ctx, ch)
	}
	if needsFileInfo(&m) {
		go a.fillFiles(ch, m.TS)
	}
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

// needsFileInfo reports whether a message carries file stubs that must be
// completed with files.info (fresh uploads arrive as file_access=check_file_info).
func needsFileInfo(m *slack.Message) bool {
	for _, f := range m.Files {
		if f.URLPrivate == "" && f.Mode != "tombstone" && f.Mode != "hidden_by_limit" && f.ID != "" {
			return true
		}
	}
	return false
}

// fillFiles completes the file stubs of one message and emits an update.
func (a *App) fillFiles(ch, ts string) {
	ctx, cancel := context.WithTimeout(a.bg, 30*time.Second)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		time.Sleep(time.Duration(attempt+1) * 700 * time.Millisecond) // uploads take a moment to process
		m, ok := a.S.Find(ch, ts)
		if !ok {
			return
		}
		files := append([]slack.File(nil), m.Files...)
		done := true
		for i := range files {
			if files[i].URLPrivate != "" || files[i].Mode == "tombstone" || files[i].ID == "" {
				continue
			}
			var r struct {
				File slack.File `json:"file"`
			}
			if err := a.C.Call(ctx, "files.info", url.Values{"file": {files[i].ID}}, &r); err == nil && r.File.ID != "" {
				files[i] = r.File
			}
			if files[i].URLPrivate == "" {
				done = false
			}
		}
		if upd, ok := a.S.Mutate(ch, ts, func(x *slack.Message) { x.Files = files }); ok {
			a.emitUpdate(ch, &upd)
		}
		if done {
			return
		}
	}
}
