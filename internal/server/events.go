package server

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/render"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

var mentionRe = regexp.MustCompile(`<@([UW][A-Z0-9]+)`)

func (a *App) handle(ev slack.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
		a.ensureUsers(ctx, []string{e.User})
		if ok {
			a.Hub.Broadcast(map[string]any{"type": "update", "channel": e.Item.Channel, "msg": render.Message(a.S, e.Item.Channel, &m)})
		}
	case "user_typing":
		var e struct {
			User     string `json:"user"`
			ThreadTS string `json:"thread_ts"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		a.ensureUsers(ctx, []string{e.User})
		a.Hub.Broadcast(map[string]any{"type": "typing", "channel": ch, "user": e.User, "name": a.S.UserName(e.User), "thread_ts": e.ThreadTS})
	case "channel_marked", "im_marked", "group_marked", "mpim_marked":
		var e struct {
			TS           string `json:"ts"`
			UnreadCount  int    `json:"unread_count_display"`
			MentionCount int    `json:"mention_count_display"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		a.S.Lock()
		if c, ok := a.S.Counts[ch]; ok {
			c.LastRead = e.TS
			c.HasUnreads = e.UnreadCount > 0
			c.MentionCount = e.MentionCount
		}
		a.S.Unlock()
		a.Hub.Broadcast(map[string]any{"type": "marked", "channel": ch, "ts": e.TS, "unread": e.UnreadCount > 0, "mentions": e.MentionCount})
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
		}
		a.S.Unlock()
		a.Hub.Broadcast(map[string]any{"type": "presence", "users": us, "presence": e.Presence})
	case "user_change", "user_profile_changed":
		var e struct {
			User slack.User `json:"user"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		if e.User.ID != "" {
			a.S.PutUsers([]slack.User{e.User})
			a.Hub.Broadcast(map[string]any{"type": "user", "user": a.userView(e.User.ID)})
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
		a.Hub.Broadcast(map[string]any{"type": "emoji_changed"})
	case "channel_joined", "group_joined", "im_created", "mpim_joined", "channel_created", "channel_rename", "group_rename", "channel_unarchive":
		var e struct {
			Channel slack.Conversation `json:"channel"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		id := e.Channel.ID
		if id == "" {
			id = ch
		}
		if c, err := a.C.ConversationInfo(ctx, id); err == nil {
			a.S.PutConv(*c)
			if c.IsIM {
				a.ensureUsers(ctx, []string{c.User})
			}
			a.Hub.Broadcast(map[string]any{"type": "conv", "conv": a.convView(c)})
		}
	case "channel_left", "group_left", "channel_archive", "group_archive", "channel_deleted", "im_close":
		a.S.Lock()
		delete(a.S.Convs, ch)
		a.S.Unlock()
		a.Hub.Broadcast(map[string]any{"type": "conv_removed", "channel": ch})
	case "pin_added", "pin_removed":
		var e struct {
			Item struct {
				Message slack.Message `json:"message"`
			} `json:"item"`
		}
		_ = json.Unmarshal(ev.Raw, &e)
		if e.Item.Message.TS != "" {
			m, ok := a.S.Mutate(ch, e.Item.Message.TS, func(m *slack.Message) {
				if ev.Type == "pin_added" {
					m.PinnedTo = []string{ch}
				} else {
					m.PinnedTo = nil
				}
			})
			if ok {
				a.Hub.Broadcast(map[string]any{"type": "update", "channel": ch, "msg": render.Message(a.S, ch, &m)})
			}
		}
	case "thread_marked", "update_thread_state", "dnd_updated", "dnd_updated_user", "pref_change", "badge_counts_updated", "channel_sections_upserted", "channel_sections_channels_upserted", "channel_sections_channels_removed", "channel_sections_deleted":
		a.Hub.Broadcast(map[string]any{"type": ev.Type, "raw": ev.Raw})
		if ev.Type == "channel_sections_upserted" || ev.Type == "channel_sections_channels_upserted" || ev.Type == "channel_sections_channels_removed" || ev.Type == "channel_sections_deleted" {
			if secs, err := a.C.Sections(ctx); err == nil {
				a.S.Lock()
				a.S.Sections = secs
				a.S.Unlock()
				a.Hub.Broadcast(map[string]any{"type": "sidebar_changed"})
			}
		}
	default:
		if len(ev.Type) > 3 && ev.Type[:3] == "sh_" || ev.Type == "huddle_invite" {
			a.Hub.Broadcast(map[string]any{"type": "huddle", "event": ev.Type, "raw": ev.Raw})
		}
	}
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
		Subtype         string         `json:"subtype"`
		Message         *slack.Message `json:"message"`
		PreviousMessage *slack.Message `json:"previous_message"`
		DeletedTS       string         `json:"deleted_ts"`
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
		a.ensureUsers(ctx, referencedUsers([]slack.Message{m}))
		if old, ok := a.S.Find(ch, m.TS); ok {
			// RTM edits may omit thread metadata; keep it.
			if m.ReplyCount == 0 && old.ReplyCount > 0 {
				m.ReplyCount, m.ReplyUsers, m.LatestReply = old.ReplyCount, old.ReplyUsers, old.LatestReply
			}
		}
		_, found := a.S.Mutate(ch, m.TS, func(x *slack.Message) { *x = m })
		if !found {
			a.S.Upsert(ch, m)
		}
		a.Hub.Broadcast(map[string]any{"type": "update", "channel": ch, "msg": render.Message(a.S, ch, &m)})
		return
	case "message_deleted":
		a.S.Remove(ch, e.DeletedTS)
		a.Hub.Broadcast(map[string]any{"type": "delete", "channel": ch, "ts": e.DeletedTS})
		return
	case "message_replied":
		if e.Message == nil {
			return
		}
		m := *e.Message
		a.ensureUsers(ctx, m.ReplyUsers)
		upd, ok := a.S.Mutate(ch, m.TS, func(x *slack.Message) {
			x.ReplyCount, x.ReplyUsers, x.LatestReply = m.ReplyCount, m.ReplyUsers, m.LatestReply
		})
		if ok {
			a.Hub.Broadcast(map[string]any{"type": "update", "channel": ch, "msg": render.Message(a.S, ch, &upd)})
		}
		return
	}
	var m slack.Message
	if json.Unmarshal(ev.Raw, &m) != nil || m.TS == "" {
		return
	}
	a.ensureUsers(ctx, referencedUsers([]slack.Message{m}))
	isReply := m.ThreadTS != "" && m.ThreadTS != m.TS
	if isReply {
		a.S.AddReply(ch, m)
		// bump the parent's reply metadata locally (message_replied may lag)
		upd, ok := a.S.Mutate(ch, m.ThreadTS, func(x *slack.Message) {
			x.ReplyCount++
			x.LatestReply = m.TS
			seen := false
			for _, u := range x.ReplyUsers {
				if u == m.User {
					seen = true
				}
			}
			if !seen && m.User != "" {
				x.ReplyUsers = append(x.ReplyUsers, m.User)
			}
		})
		if ok {
			a.Hub.Broadcast(map[string]any{"type": "update", "channel": ch, "msg": render.Message(a.S, ch, &upd)})
		}
	}
	if !isReply || m.Subtype == "thread_broadcast" {
		a.S.Upsert(ch, m)
	}
	a.S.Lock()
	c, ok := a.S.Counts[ch]
	if !ok {
		c = &slack.CountEntry{ID: ch}
		a.S.Counts[ch] = c
	}
	if !isReply || m.Subtype == "thread_broadcast" {
		c.Latest = m.TS
		if m.User != a.S.Self.ID {
			c.HasUnreads = true
			if mentionsSelf(&m, a.S.Self.ID) {
				c.MentionCount++
			}
		}
	}
	a.S.Unlock()
	if _, known := a.S.Conv(ch); !known {
		if cv, err := a.C.ConversationInfo(ctx, ch); err == nil {
			a.S.PutConv(*cv)
			if cv.IsIM {
				a.ensureUsers(ctx, []string{cv.User})
			}
			a.Hub.Broadcast(map[string]any{"type": "conv", "conv": a.convView(cv)})
		}
	}
	a.Hub.Broadcast(map[string]any{
		"type": "message", "channel": ch, "msg": render.Message(a.S, ch, &m),
		"reply": isReply, "user": a.userView(m.User), "mention": mentionsSelf(&m, a.S.Self.ID),
		"conv_kind": a.kindOf(ch),
	})
}

func (a *App) kindOf(ch string) string {
	if c, ok := a.S.Conv(ch); ok {
		return a.convView(c).Kind
	}
	return ""
}

func mentionsSelf(m *slack.Message, self string) bool {
	for _, mm := range mentionRe.FindAllStringSubmatch(m.Text, -1) {
		if mm[1] == self {
			return true
		}
	}
	return false
}
