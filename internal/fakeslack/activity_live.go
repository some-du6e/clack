package fakeslack

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// The Activity feed's live entries, made-up content only: what the server
// lists after the fixture (UpsertActivity), one per key. A thread_v2
// bundle keeps its key and comes back with each new reply's ts as its
// feed_ts and latest_ts, as Slack's server sends it next to the reply's
// thread_reply entry (which Slack's client drops). activity.feed answers
// them with the fixture's, newest feed_ts first (Slack's chrono_v1).
// PostAtQuiet and ReplyAtQuiet put the entries' messages in the history
// at the times the entries name, so messages.list finds them.

// UpsertActivity lists entry in activity.feed, in place of the entry with
// its key (the fixture's too), as Slack's server does before it pushes
// the entry's activity_updated. It returns that event, unpushed.
func (s *Server) UpsertActivity(entry map[string]any) map[string]any {
	v := s.views()
	v.mu.Lock()
	st := v.act()
	key, _ := entry["key"].(string)
	stored := activityEntryClone(entry)
	at := slicesIndexKey(st.live, key)
	if at >= 0 {
		st.live[at] = stored
	} else {
		st.live = append(st.live, stored)
	}
	v.mu.Unlock()
	return map[string]any{"type": "activity", "subtype": "activity_updated", "entry": activityEntryClone(entry)}
}

func slicesIndexKey(es []map[string]any, key string) int {
	for i, e := range es {
		if k, _ := e["key"].(string); k == key {
			return i
		}
	}
	return -1
}

// activityEntryClone copies an entry and its item (activity.feed sets an
// entry's is_unread / is_archived and an invite's id on the copies it
// answers).
func activityEntryClone(e map[string]any) map[string]any {
	out := make(map[string]any, len(e))
	for k, x := range e {
		out[k] = x
	}
	if it, ok := e["item"].(map[string]any); ok {
		c := make(map[string]any, len(it))
		for k, x := range it {
			c[k] = x
		}
		out["item"] = c
	}
	return out
}

// activityWithLive is the feed's entries with the live ones in (a live
// entry replaces the one with its key), newest feed_ts first.
func activityWithLive(entries, live []map[string]any) []map[string]any {
	for _, e := range live {
		key, _ := e["key"].(string)
		c := activityEntryClone(e)
		if at := slicesIndexKey(entries, key); at >= 0 {
			entries[at] = c
		} else {
			entries = append(entries, c)
		}
	}
	ts := func(e map[string]any) float64 {
		s, _ := e["feed_ts"].(string)
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	sort.SliceStable(entries, func(i, j int) bool { return ts(entries[i]) > ts(entries[j]) })
	return entries
}

// PostAtQuiet puts user's message text at ts in ch's history (newest
// first, by ts) and returns it, unpushed: an Activity entry's message
// from a given time.
func (s *Server) PostAtQuiet(ch, ts, user, text string) slack.Message {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	m := slack.Message{Type: "message", TS: ts, User: user, Text: text, ClientMsgID: "quiet-" + ts}
	at := sort.Search(len(h), func(i int) bool { return tsAfterEq(ts, h[i].TS) })
	h = append(h, slack.Message{})
	copy(h[at+1:], h[at:])
	h[at] = m
	s.hist[ch] = h
	return m
}

// ReplyAtQuiet puts user's reply text at ts in the thread under root (a
// message of ch's history, which gets the reply metadata: reply_count,
// latest_reply, reply_users) and returns it, unpushed, as PostReplyQuiet
// does with a ts of its own. conversations.replies serves the thread.
func (s *Server) ReplyAtQuiet(ch, root, ts, user, text string) slack.Message {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	k := ch + "/" + root
	m := slack.Message{Type: "message", TS: ts, User: user, Text: text, ThreadTS: root, ClientMsgID: "quiet-" + ts}
	if s.quietReplies == nil {
		s.quietReplies = map[string][]slack.Message{}
	}
	rs := s.quietReplies[k]
	at := sort.Search(len(rs), func(i int) bool { return !tsAfterEq(ts, rs[i].TS) })
	rs = append(rs, slack.Message{})
	copy(rs[at+1:], rs[at:])
	rs[at] = m
	s.quietReplies[k] = rs
	for i := range h {
		if h[i].TS != root {
			continue
		}
		m.ParentUser = h[i].User
		rs[at].ParentUser = h[i].User
		h[i].ThreadTS = root
		h[i].ReplyCount = len(rs)
		h[i].LatestReply = rs[len(rs)-1].TS
		var users []string
		for _, r := range rs {
			if r.User != "" && !strings.Contains(","+strings.Join(users, ",")+",", ","+r.User+",") {
				users = append(users, r.User)
			}
		}
		h[i].ReplyUsers, h[i].ReplyUsersN = users, len(users)
	}
	return m
}

// activityVIPFirst reports whether activity.feed's priority_unreads_first_v1
// (the VIP sort) lists entry first: an unread one from a VIP.
func activityVIPFirst(e map[string]any) bool {
	return e["priority"] != nil && e["is_unread"] == true
}

// tsAfterEq reports whether Slack timestamp a is at or after b.
func tsAfterEq(a, b string) bool {
	fa, _ := strconv.ParseFloat(a, 64)
	fb, _ := strconv.ParseFloat(b, 64)
	return fa >= fb
}

// ActivityThreadKey is the key of the thread_v2 bundle about root in ch.
func ActivityThreadKey(ch, root string) string { return "thread_v2-" + ch + "-" + root }

// ActivityThreadEntry is the thread_v2 bundle about root in ch whose
// latest reply (the one that notified you: its feed_ts) is latest, with
// unread unread replies from minUnread on (read when unread is 0).
func ActivityThreadEntry(ch, root, latest, minUnread string, unread int) map[string]any {
	te := map[string]any{"channel_id": ch, "thread_ts": root, "latest_ts": latest, "unread_msg_count": unread}
	if unread > 0 && minUnread != "" {
		te["min_unread_ts"] = minUnread
	}
	return map[string]any{"key": ActivityThreadKey(ch, root), "feed_ts": latest, "is_unread": unread > 0, "is_bot": false,
		"item": map[string]any{"type": "thread_v2", "bundle_info": map[string]any{"payload": map[string]any{"thread_entry": te}}}}
}

// PushMentionAtLive posts user's message text at ts in ch (PostAtQuiet)
// and pushes it, then its at_user entry, which the feed lists from then
// on (UpsertActivity). It returns the entry's key.
func (s *Server) PushMentionAtLive(ch, ts, user, text string) string {
	m := s.PostAtQuiet(ch, ts, user, text)
	s.Push(map[string]any{"type": "message", "channel": ch, "user": user, "text": text, "ts": ts, "event_ts": ts,
		"client_msg_id": m.ClientMsgID})
	key := "at_user-" + ch + "-" + ts
	s.Push(s.UpsertActivity(map[string]any{"key": key, "feed_ts": ts, "is_unread": true, "is_bot": false,
		"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": ts, "channel": ch, "author_user_id": user}}}))
	return key
}

// PushReactionAtLive adds user's reaction name, at ts, to the message
// msgTS of ch (by author) and pushes reaction_added, then the
// message_reaction entry the feed lists from then on. It returns the
// entry's key.
func (s *Server) PushReactionAtLive(ch, msgTS, author, ts, user, name string) string {
	s.ReactQuiet(ch, msgTS, name, user)
	s.Push(map[string]any{"type": "reaction_added", "user": user, "reaction": name, "item_user": author, "event_ts": ts,
		"item": map[string]any{"type": "message", "channel": ch, "ts": msgTS}})
	key := "message_reaction-" + ch + "-" + msgTS + "-" + user + "-" + name
	s.Push(s.UpsertActivity(map[string]any{"key": key, "feed_ts": ts, "is_unread": true, "is_bot": false,
		"item": map[string]any{"type": "message_reaction", "message": map[string]any{"ts": msgTS, "channel": ch, "author_user_id": author},
			"reaction": map[string]any{"user": user, "name": name}}}))
	return key
}

// PushReplyAtLive is PushReplyLive for a reply at ts (ReplyAtQuiet) that
// the feed lists too: the reply's message event and the root's
// message_replied, then the thread_reply entry (Slack's client drops it)
// and the thread's thread_v2 bundle with the reply as its feed_ts and
// latest_ts (UpsertActivity), unread replies from minUnread on. It
// returns the bundle's key.
func (s *Server) PushReplyAtLive(ch, root, ts, user, text, minUnread string, unread int) string {
	m := s.ReplyAtQuiet(ch, root, ts, user, text)
	var rm map[string]any
	s.histMu.Lock()
	for _, r := range s.hist[ch] {
		if r.TS == root {
			raw, _ := json.Marshal(r)
			_ = json.Unmarshal(raw, &rm)
			break
		}
	}
	s.histMu.Unlock()
	s.Push(map[string]any{"type": "message", "channel": ch, "user": user, "text": text, "ts": ts, "event_ts": ts,
		"thread_ts": root, "parent_user_id": m.ParentUser, "client_msg_id": m.ClientMsgID})
	if rm != nil {
		rm["channel"] = ch
		s.Push(map[string]any{"type": "message", "subtype": "message_replied", "channel": ch, "hidden": true,
			"message": rm, "event_ts": ts, "ts": ts})
	}
	s.Push(map[string]any{"type": "activity", "subtype": "activity_updated", "entry": map[string]any{
		"key": "thread_reply-" + ch + "-" + ts, "feed_ts": ts, "is_unread": true,
		"item": map[string]any{"type": "thread_reply", "message": map[string]any{
			"ts": ts, "channel": ch, "thread_ts": root, "author_user_id": user, "is_broadcast": false}}}})
	s.Push(s.UpsertActivity(ActivityThreadEntry(ch, root, ts, minUnread, unread)))
	return ActivityThreadKey(ch, root)
}
