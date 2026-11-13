package core

import (
	"context"
	"encoding/json"

	"github.com/some-du6e/clack/internal/slack"
)

// A thread parent's reply metadata (reply_count, reply_users,
// reply_users_count, latest_reply) the way Slack's web client keeps it
// (build fa938a086569, 2026-11-12). The client never counts replies
// itself: no reducer increments reply_count when a reply arrives, is sent
// or is deleted. It takes the server's values from three places:
//
//   - message_replied (boot-render messageReplied): the event's message is
//     the parent with its new metadata, merged into the stored parent
//     (udbq makeThreadObject via addMessages). It is dropped when the
//     stored parent has replies and a newer latest_reply than the event:
//     "Not processing an older message_replied event".
//   - a thread_broadcast's root (udbq makeBroadcastRootObject +
//     z+/K addOrReplaceMessages): the root is merged into the stored
//     parent, minus the thread fields root doesn't carry.
//   - the parent's own message_changed (a deleted reply, an edit), which
//     replaces it.
//
// A reply's own message event (the viewer's echo included) and its
// message_deleted (setMessageDeleted → removeMessageAndTimestamp) leave
// the parent alone.

// threadFields are udbq's M: the reply metadata makeBroadcastRootObject
// keeps from a broadcast's root only when root carries them.
var threadFields = []string{"thread_ts", "reply_count", "replies", "latest_reply", "reply_users", "reply_users_count"}

// replyTS is the ts of a message's latest reply as messageReplied reads
// it: latest_reply, else the last of an old-style replies list.
type replyTS struct {
	LatestReply string `json:"latest_reply"`
	Replies     []struct {
		TS string `json:"ts"`
	} `json:"replies"`
}

func (r replyTS) latest() string {
	if r.LatestReply != "" {
		return r.LatestReply
	}
	l := ""
	for _, x := range r.Replies { // lodash maxBy(replies, "ts")
		if x.TS > l {
			l = x.TS
		}
	}
	return l
}

// staleReplied is messageReplied's guard: incoming is the event's latest
// reply ts, stored the cached parent. JS compares the ts strings (an
// undefined side is never "less").
func staleReplied(incoming string, stored *slack.Message) bool {
	if stored == nil || stored.ReplyCount == 0 || incoming == "" {
		return false
	}
	have := stored.LatestReply
	return have != "" && incoming < have
}

// onMessageReplied applies a message_replied event (raw is the event).
func (a *App) onMessageReplied(ctx context.Context, ch string, raw json.RawMessage) {
	var e struct {
		Message *json.RawMessage `json:"message"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Message == nil {
		return
	}
	var m slack.Message
	var r replyTS
	if json.Unmarshal(*e.Message, &m) != nil || json.Unmarshal(*e.Message, &r) != nil || m.TS == "" {
		return
	}
	if stored, ok := a.S.Find(ch, m.TS); ok && staleReplied(r.latest(), &stored) {
		return
	}
	latest := m.LatestReply
	if latest == "" {
		latest = r.latest()
	}
	a.EnsureUsers(ctx, m.ReplyUsers)
	if upd, ok := a.S.Mutate(ch, m.TS, func(x *slack.Message) {
		// makeThreadObject: reply_count parseInt(...) || 0; the rest as sent
		x.ReplyCount, x.ReplyUsers, x.LatestReply = m.ReplyCount, m.ReplyUsers, latest
		if m.ReplyUsersN > 0 {
			x.ReplyUsersN = m.ReplyUsersN
		}
		if m.ThreadTS != "" {
			x.ThreadTS = m.ThreadTS
		}
	}); ok {
		a.emitUpdate(ch, &upd)
	}
}

// mergeBroadcastRoot applies a thread_broadcast's root to its cached
// parent: the reply metadata root carries (raw is the broadcast). Root's
// other fields are the parent as it was; Clack keeps reactions, pins and
// file state on the message itself, so they stay as cached (Slack's
// reaction and file stores don't take them from root either).
func (a *App) mergeBroadcastRoot(ch string, m *slack.Message, raw json.RawMessage) {
	if m.Subtype != "thread_broadcast" || m.Root == nil || m.ThreadTS == "" {
		return
	}
	var e struct {
		Root map[string]json.RawMessage `json:"root"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Root == nil {
		return
	}
	has := func(k string) bool { _, ok := e.Root[k]; return ok }
	root := m.Root
	var r replyTS
	if b, err := json.Marshal(e.Root); err == nil {
		_ = json.Unmarshal(b, &r)
	}
	carried := false
	for _, k := range threadFields {
		carried = carried || has(k)
	}
	if !carried {
		return
	}
	pts := root.TS
	if pts == "" {
		pts = m.ThreadTS
	}
	if upd, ok := a.S.Mutate(ch, pts, func(x *slack.Message) {
		if has("thread_ts") {
			x.ThreadTS = root.ThreadTS
		}
		if has("reply_count") {
			x.ReplyCount = root.ReplyCount
		}
		if has("reply_users") {
			x.ReplyUsers = root.ReplyUsers
		}
		if has("reply_users_count") {
			x.ReplyUsersN = root.ReplyUsersN
		}
		if has("latest_reply") || has("replies") {
			x.LatestReply = r.latest()
		}
	}); ok {
		a.emitUpdate(ch, &upd)
	}
}
