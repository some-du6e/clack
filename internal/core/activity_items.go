package core

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
)

// activity.feed entries. Every item type keeps its subject in its own
// place; the field paths follow the web client's normaliser
// (client-boot module /29I, bulkAddActivityItems, 2026-11):
//
//	at_user, at_user_group, at_channel, at_everyone, keyword,
//	internal_channel_invite, message_reaction   item.message
//	thread_v2                                   item.bundle_info.payload.thread_entry
//	bot_dm_bundle                               item.bundle_info.payload.message
//	dm, mpdm                                    item.bundle_info.payload.dm_entry.latest_message
//	channel (a channel set to "all new posts")  item.bundle_info.payload.channel_entry.latest_message
//	quietly_added_to_channel                    item.quietly_added_to_channel_payload
//	unjoined_channel_mention                    item.unjoined_channel_mention_payload
//	code_review_requested                       item.code_review_requested_payload
//	team_joiner_invite                          item.team_joiner_invite_payload (+ item.invite)
//	prejoin_dm_welcome_party_alert              item.prejoin_dm_welcome_party_payload
//	external_channel_invite, external_dm_invite item.invite
//	list_record_assigned, list_record_edited,
//	list_user_mentioned, list_todo_notification,
//	list_approval_request, list_approval_reviewed item.list_*_payload
//	saved_reminder                              item.saved_reminder_payload
//	calendar_upcoming_event                     item.calendar_event_payload
//	generic_system_alert, example_user_alert    item.generic_system_alert_payload
//
// Anything else is located by a generic walk (activityGuess) so a new
// type still gets its channel, message and actor.

type msgRef struct {
	TS           string `json:"ts"`
	Channel      string `json:"channel"`
	ThreadTS     string `json:"thread_ts"`
	AuthorUserID string `json:"author_user_id"`
	User         string `json:"user"`
}

// actPayload is the shared shape of the *_payload objects that point at a
// channel (and maybe a message) plus the person behind them.
type actPayload struct {
	ChannelID     string `json:"channel_id"`
	MessageTS     string `json:"message_ts"`
	InviterUserID string `json:"inviter_user_id"`
	InviteeUserID string `json:"invitee_user_id"`
	UserID        string `json:"user_id"`
	JoinerTS      string `json:"joiner_notification_message_ts"`
}

type actListPayload struct {
	ListID      string `json:"list_id"`
	RowID       string `json:"row_id"`
	AssignedBy  string `json:"assigned_by"`
	EditedBy    string `json:"edited_by"`
	MentionedBy string `json:"mentioned_by"`
	UserID      string `json:"user_id"`
}

type actEntry struct {
	LatestMessage *msgRef `json:"latest_message"`
}

type actNamed struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type actInvite struct {
	Channel      *actNamed `json:"channel"`
	Team         *actNamed `json:"team"`
	Message      string    `json:"message"`
	InvitingUser *struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Profile struct {
			RealName    string `json:"real_name"`
			DisplayName string `json:"display_name"`
		} `json:"profile"`
	} `json:"inviting_user"`
}

type activityRaw struct {
	IsUnread bool   `json:"is_unread"`
	FeedTS   string `json:"feed_ts"`
	Key      string `json:"key"`
	Item     struct {
		Type     string  `json:"type"`
		Message  *msgRef `json:"message"`
		Reaction *struct {
			User string `json:"user"`
			Name string `json:"name"`
		} `json:"reaction"`
		Channel    string `json:"channel"`
		BundleInfo *struct {
			UnreadCount int `json:"unread_count"`
			Payload     struct {
				Message     *msgRef `json:"message"`
				ThreadEntry *struct {
					ChannelID      string `json:"channel_id"`
					LatestTS       string `json:"latest_ts"`
					MinUnreadTS    string `json:"min_unread_ts"`
					ThreadTS       string `json:"thread_ts"`
					UnreadMsgCount int    `json:"unread_msg_count"`
				} `json:"thread_entry"`
				DMEntry      *actEntry `json:"dm_entry"`
				ChannelEntry *actEntry `json:"channel_entry"`
			} `json:"payload"`
		} `json:"bundle_info"`
		QuietlyAdded  *actPayload     `json:"quietly_added_to_channel_payload"`
		Unjoined      *actPayload     `json:"unjoined_channel_mention_payload"`
		CodeReview    *actPayload     `json:"code_review_requested_payload"`
		TeamJoiner    *actPayload     `json:"team_joiner_invite_payload"`
		Prejoin       *actPayload     `json:"prejoin_dm_welcome_party_payload"`
		Invite        *actInvite      `json:"invite"`
		List          *actListPayload `json:"list_payload"`
		ListEdit      *actListPayload `json:"list_edit_payload"`
		ListMention   *actListPayload `json:"list_user_mention_payload"`
		ListTodo      *actListPayload `json:"list_todo_payload"`
		ListApproval  *actListPayload `json:"lists_approval_payload"`
		SavedReminder *struct {
			ItemType string `json:"item_type"`
			ItemID   string `json:"item_id"`
			ItemID2  string `json:"item_id_2"`
			Detail   string `json:"item_detail"`
		} `json:"saved_reminder_payload"`
		Calendar *struct {
			Title string `json:"title"`
		} `json:"calendar_event_payload"`
		Alert *struct {
			Category string          `json:"category"`
			Blocks   json.RawMessage `json:"blocks"`
		} `json:"generic_system_alert_payload"`
	} `json:"item"`
	item json.RawMessage // the raw item, for activityGuess
}

func (r *activityRaw) UnmarshalJSON(b []byte) error {
	type plain activityRaw
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var raw struct {
		Item json.RawMessage `json:"item"`
	}
	_ = json.Unmarshal(b, &raw)
	*r = activityRaw(p)
	r.item = raw.Item
	return nil
}

// activityKnown are the types activityRef understands; others are logged
// once and located by activityGuess.
var activityKnown = map[string]bool{
	"at_user": true, "at_user_group": true, "at_channel": true, "at_everyone": true, "keyword": true,
	"internal_channel_invite": true, "message_reaction": true, "thread_v2": true, "bot_dm_bundle": true,
	"dm": true, "mpdm": true, "channel": true, "quietly_added_to_channel": true, "unjoined_channel_mention": true,
	"code_review_requested": true, "team_joiner_invite": true, "prejoin_dm_welcome_party_alert": true,
	"external_channel_invite": true, "external_dm_invite": true, "shared_workspace_invite": true,
	"list_record_assigned": true, "list_record_edited": true, "list_user_mentioned": true, "list_todo_notification": true,
	"list_approval_request": true, "list_approval_reviewed": true, "saved_reminder": true,
	"calendar_upcoming_event": true, "generic_system_alert": true, "example_user_alert": true,
}

var activityUnknownSeen sync.Map

// activityItem converts one feed entry; ref is the message to hydrate
// (nil for items without one).
func activityItem(raw *activityRaw) (ActivityItem, *msgRef) {
	in := &raw.Item
	it := ActivityItem{Key: raw.Key, Type: in.Type, FeedTS: raw.FeedTS, Unread: raw.IsUnread, Channel: in.Channel}
	ref := in.Message
	if bi := in.BundleInfo; bi != nil {
		it.Count = bi.UnreadCount
		pl := &bi.Payload
		if ref == nil {
			ref = pl.Message
		}
		for _, e := range []*actEntry{pl.DMEntry, pl.ChannelEntry} {
			if ref == nil && e != nil && e.LatestMessage != nil {
				ref = e.LatestMessage
			}
		}
		if te := pl.ThreadEntry; te != nil {
			ref = &msgRef{Channel: te.ChannelID, TS: firstNonEmpty(te.LatestTS, te.MinUnreadTS), ThreadTS: te.ThreadTS}
			if te.UnreadMsgCount > 0 {
				it.Count = te.UnreadMsgCount
			}
		}
	}
	// channel-pointing payloads
	for _, p := range []*actPayload{in.QuietlyAdded, in.Unjoined, in.CodeReview, in.TeamJoiner, in.Prejoin} {
		if p == nil {
			continue
		}
		it.Channel = firstNonEmpty(it.Channel, p.ChannelID)
		it.Actor = firstNonEmpty(it.Actor, p.InviterUserID, p.InviteeUserID, p.UserID)
		if ts := firstNonEmpty(p.MessageTS, p.JoinerTS); ref == nil && ts != "" && p.ChannelID != "" {
			ref = &msgRef{Channel: p.ChannelID, TS: ts}
		}
	}
	if inv := in.Invite; inv != nil {
		// the channel isn't in this workspace yet: name only (Slack leaves
		// channelId unset for external invites)
		if inv.Channel != nil {
			it.Title = inv.Channel.Name
		}
		if inv.Team != nil {
			it.Team = inv.Team.Name
		}
		it.Text = inv.Message
		if u := inv.InvitingUser; u != nil {
			it.Actor = firstNonEmpty(it.Actor, u.ID)
			it.ActorName = firstNonEmpty(u.Profile.DisplayName, u.Profile.RealName, u.Name)
		}
	}
	for _, l := range []*actListPayload{in.List, in.ListEdit, in.ListMention, in.ListTodo, in.ListApproval} {
		if l != nil {
			it.Actor = firstNonEmpty(it.Actor, l.AssignedBy, l.EditedBy, l.MentionedBy, l.UserID)
		}
	}
	if sr := in.SavedReminder; sr != nil {
		it.Text = sr.Detail
		if sr.ItemType == "message" && sr.ItemID != "" && sr.ItemID2 != "" && ref == nil {
			ref = &msgRef{Channel: sr.ItemID, TS: sr.ItemID2}
		}
	}
	if c := in.Calendar; c != nil {
		it.Title = c.Title
	}
	if al := in.Alert; al != nil {
		it.Title, it.Text = al.Category, blocksText(al.Blocks)
	}
	if rx := in.Reaction; rx != nil {
		it.Reaction = &ActivityReaction{User: rx.User, Name: rx.Name}
	}
	if !activityKnown[in.Type] {
		if _, seen := activityUnknownSeen.LoadOrStore(in.Type, true); !seen {
			log.Printf("activity: unknown item type %q (located generically)", in.Type)
		}
		if ref == nil && it.Channel == "" {
			ref = activityGuess(raw.item)
		}
	}
	if ref != nil {
		it.Channel, it.TS, it.ThreadTS = firstNonEmpty(ref.Channel, it.Channel), ref.TS, ref.ThreadTS
		it.Author = firstNonEmpty(ref.AuthorUserID, ref.User)
	}
	return it, ref
}

// activityGuess finds the first object in an unknown item that names a
// conversation, and its message ts and person when present.
func activityGuess(raw json.RawMessage) *msgRef {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return nil
	}
	str := func(m map[string]any, keys ...string) string {
		for _, k := range keys {
			if s, ok := m[k].(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
	var walk func(v any, depth int) *msgRef
	walk = func(v any, depth int) *msgRef {
		if depth > 6 {
			return nil
		}
		switch x := v.(type) {
		case map[string]any:
			if ch := str(x, "channel", "channel_id"); ch != "" && strings.ContainsAny(ch[:1], "CDG") {
				return &msgRef{Channel: ch, TS: str(x, "ts", "message_ts", "latest_ts"), ThreadTS: str(x, "thread_ts"),
					AuthorUserID: str(x, "author_user_id", "user", "user_id", "inviter_user_id")}
			}
			for _, k := range sortedKeys(x) {
				if r := walk(x[k], depth+1); r != nil {
					return r
				}
			}
		case []any:
			for _, e := range x {
				if r := walk(e, depth+1); r != nil {
					return r
				}
			}
		}
		return nil
	}
	return walk(v, 0)
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	// deterministic, with the usual subject keys first
	rank := func(k string) int {
		switch {
		case k == "message" || k == "latest_message":
			return 0
		case strings.HasSuffix(k, "_payload") || strings.HasSuffix(k, "_entry") || k == "bundle_info" || k == "payload":
			return 1
		}
		return 2
	}
	for i := 1; i < len(ks); i++ {
		for j := i; j > 0 && (rank(ks[j]) < rank(ks[j-1]) || rank(ks[j]) == rank(ks[j-1]) && ks[j] < ks[j-1]); j-- {
			ks[j], ks[j-1] = ks[j-1], ks[j]
		}
	}
	return ks
}

// blocksText is the plain text of Block Kit blocks (every "text" string,
// in order, one line).
func blocksText(raw json.RawMessage) string {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return ""
	}
	var parts []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if s, ok := x["text"].(string); ok && strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
			}
			for _, k := range sortedKeys(x) {
				if _, isStr := x[k].(string); !isStr {
					walk(x[k])
				}
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(v)
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

// activityItems converts feed entries and hydrates their messages (one
// messages.list round trip for everything not cached).
func (a *App) activityItems(ctx context.Context, raws []activityRaw) []ActivityItem {
	items := make([]ActivityItem, 0, len(raws))
	want := map[string][]string{}
	for i := range raws {
		it, ref := activityItem(&raws[i])
		if ref != nil && it.Channel != "" && it.TS != "" {
			want[it.Channel] = append(want[it.Channel], it.TS)
		}
		if it.Channel != "" && it.ThreadTS != "" && it.ThreadTS != it.TS {
			want[it.Channel] = append(want[it.Channel], it.ThreadTS)
		}
		if it.Reaction != nil {
			it.Reaction.URL, _, _ = a.S.Emoji(it.Reaction.Name)
		}
		items = append(items, it)
	}
	got := a.hydrate(ctx, want)
	ids := []string{}
	for i := range items {
		ids = append(ids, items[i].Author, items[i].Actor)
		if items[i].Reaction != nil {
			ids = append(ids, items[i].Reaction.User)
		}
	}
	a.EnsureUsers(ctx, ids)
	for i := range items {
		it := &items[i]
		if m, ok := got[it.Channel+"/"+it.TS]; ok {
			it.Msg = a.messagePtr(it.Channel, &m)
		}
		if it.ThreadTS != "" && it.ThreadTS != it.TS {
			if m, ok := got[it.Channel+"/"+it.ThreadTS]; ok {
				it.Root = a.messagePtr(it.Channel, &m)
			}
		}
	}
	return items
}
