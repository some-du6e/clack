package demo

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// The Activity feed (activity.feed), in the item shapes the web client
// normalises (core/activity_items.go): mentions and reactions point at
// item.message, thread replies bundle under thread_v2, DMs under dm (or
// bot_dm_bundle for an app's DM). The feed starts with what the seeded
// history implies and grows as the bots talk to the viewer; every new
// item is also pushed as an "activity" event.

type actEntry struct {
	Key, FeedTS, Type string
	Unread, Archived  bool
	// bundle is the thread or DM a bundled item stands for ("ch/thread",
	// "ch"): a newer message replaces the item instead of adding one.
	bundle string
	Item   map[string]any
}

func (e *actEntry) json() map[string]any {
	return map[string]any{"key": e.Key, "feed_ts": e.FeedTS, "is_unread": e.Unread, "item": e.Item}
}

// addActivityLocked records an item: typ at_user / at_channel /
// message_reaction (a message), thread_v2 (thread: the reply's thread
// ts), dm / bot_dm_bundle (the latest message). It returns the
// "activity" event to push.
func (w *World) addActivityLocked(typ, ch, ts, thread, actor, reaction string) []any {
	w.actSeq++
	e := &actEntry{Type: typ, FeedTS: ts, Unread: true, Key: fmt.Sprintf("demo-%s-%d", typ, w.actSeq)}
	if typ == "message_reaction" {
		e.FeedTS = w.nextTSLocked()
	}
	msg := map[string]any{"ts": ts, "channel": ch, "author_user_id": actor, "user": actor}
	if thread != "" && thread != ts {
		msg["thread_ts"] = thread
	}
	switch typ {
	case "thread_v2":
		e.bundle = ch + "/" + thread
		n := 1
		for _, old := range w.activity {
			if old.bundle == e.bundle && old.Unread {
				n += nestedInt(old.Item["bundle_info"], "unread_count")
			}
		}
		e.Item = map[string]any{"type": typ, "bundle_info": map[string]any{"unread_count": n, "payload": map[string]any{
			"thread_entry": map[string]any{"channel_id": ch, "latest_ts": ts, "min_unread_ts": ts, "thread_ts": thread, "unread_msg_count": n}}}}
	case "dm", "bot_dm_bundle":
		e.bundle = ch
		n := 1
		for _, old := range w.activity {
			if old.bundle == e.bundle && old.Unread {
				n += nestedInt(old.Item["bundle_info"], "unread_count")
			}
		}
		payload := map[string]any{"dm_entry": map[string]any{"latest_message": msg}}
		if typ == "bot_dm_bundle" {
			payload = map[string]any{"message": msg}
		}
		e.Item = map[string]any{"type": typ, "bundle_info": map[string]any{"unread_count": n, "payload": payload}}
	case "message_reaction":
		e.Item = map[string]any{"type": typ, "message": msg, "reaction": map[string]any{"user": actor, "name": reaction}}
		msg["author_user_id"], msg["user"] = SelfID, SelfID
	default:
		msg["is_broadcast"] = typ == "at_channel"
		e.Item = map[string]any{"type": typ, "message": msg}
	}
	if e.bundle != "" {
		keep := w.activity[:0:0]
		for _, old := range w.activity {
			if old.bundle == e.bundle {
				e.Key = old.Key // the same row, updated
				continue
			}
			keep = append(keep, old)
		}
		w.activity = keep
	}
	w.activity = append([]*actEntry{e}, w.activity...)
	return []any{map[string]any{"type": "activity", "subtype": "upsert", "key": e.Key, "entry": e.json(), "event_ts": e.FeedTS}}
}

func nestedInt(v any, path ...string) int {
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return 0
		}
		v = m[p]
	}
	n, _ := v.(int)
	return n
}

// activityForPostLocked is what a new message adds to the viewer's
// Activity: a mention, a reply in a thread they're in, a DM.
func (w *World) activityForPostLocked(ch string, m slack.Message, parent *slack.Message) []any {
	if m.User == SelfID || m.Hidden {
		return nil
	}
	c, _ := w.convLocked(ch)
	switch {
	case hasStr(mentionIDs(m.Text), SelfID):
		return w.addActivityLocked("at_user", ch, m.TS, m.ThreadTS, m.User, "")
	case strings.Contains(m.Text, "<!channel") || strings.Contains(m.Text, "<!here"):
		return w.addActivityLocked("at_channel", ch, m.TS, m.ThreadTS, m.User, "")
	case w.mentionsMyGroup(m.Text):
		return w.addActivityLocked("at_user_group", ch, m.TS, m.ThreadTS, m.User, "")
	case parent != nil && w.inThreadLocked(ch, parent):
		return w.addActivityLocked("thread_v2", ch, m.TS, parent.TS, m.User, "")
	case c != nil && c.IsIM && parent == nil:
		if m.BotID != "" {
			return w.addActivityLocked("bot_dm_bundle", ch, m.TS, "", m.User, "")
		}
		return w.addActivityLocked("dm", ch, m.TS, "", m.User, "")
	}
	return nil
}

// mentionsMyGroup: text pings a user group the viewer is in.
func (w *World) mentionsMyGroup(text string) bool {
	for _, g := range w.ws.Usergroups {
		if strings.Contains(text, "<!subteam^"+g.Group.ID) && hasStr(g.Members, SelfID) {
			return true
		}
	}
	return false
}

// inThreadLocked: the viewer started or replied in the thread.
func (w *World) inThreadLocked(ch string, parent *slack.Message) bool {
	if parent.User == SelfID {
		return true
	}
	for _, r := range w.replies[ch][parent.TS] {
		if r.User == SelfID {
			return true
		}
	}
	return false
}

// dropActivityLocked removes items about a deleted message.
func (w *World) dropActivityLocked(ch, ts string) {
	keep := w.activity[:0:0]
	for _, e := range w.activity {
		if m, ok := e.Item["message"].(map[string]any); ok && m["channel"] == ch && m["ts"] == ts {
			continue
		}
		keep = append(keep, e)
	}
	w.activity = keep
}

// seedActivity derives the feed's history from the seeded messages.
func (w *World) seedActivity() {
	type cand struct {
		typ, ch, ts, thread, actor, reaction string
	}
	var cs []cand
	for _, c := range w.convs {
		ch := c.ID
		var all []slack.Message
		all = append(all, w.hist[ch]...)
		for _, rs := range w.replies[ch] {
			all = append(all, rs...)
		}
		lastDM := map[string]bool{}
		for _, m := range all {
			if m.User != SelfID {
				switch {
				case hasStr(mentionIDs(m.Text), SelfID):
					cs = append(cs, cand{"at_user", ch, m.TS, m.ThreadTS, m.User, ""})
				case strings.Contains(m.Text, "<!channel") || strings.Contains(m.Text, "<!here"):
					cs = append(cs, cand{"at_channel", ch, m.TS, m.ThreadTS, m.User, ""})
				case w.mentionsMyGroup(m.Text):
					cs = append(cs, cand{"at_user_group", ch, m.TS, m.ThreadTS, m.User, ""})
				}
			}
			if m.User == SelfID {
				for _, r := range m.Reactions {
					for _, u := range r.Users {
						if u != SelfID {
							cs = append(cs, cand{"message_reaction", ch, m.TS, m.ThreadTS, u, r.Name})
						}
					}
				}
			}
		}
		for parent, rs := range w.replies[ch] {
			p := w.msgPtrLocked(ch, parent)
			if p == nil || !w.inThreadLocked(ch, p) {
				continue
			}
			for i := len(rs) - 1; i >= 0; i-- {
				if rs[i].User != SelfID {
					cs = append(cs, cand{"thread_v2", ch, rs[i].TS, parent, rs[i].User, ""})
					break
				}
			}
		}
		if c.IsIM {
			for _, m := range w.hist[ch] { // newest first: the latest from them
				if m.User != SelfID && !lastDM[ch] {
					lastDM[ch] = true
					typ := "dm"
					if m.BotID != "" {
						typ = "bot_dm_bundle"
					}
					cs = append(cs, cand{typ, ch, m.TS, "", m.User, ""})
				}
			}
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return tsBefore(cs[i].ts, cs[j].ts) })
	for _, c := range cs {
		w.addActivityLocked(c.typ, c.ch, c.ts, c.thread, c.actor, c.reaction)
		e := w.activity[0]
		if c.typ == "message_reaction" {
			e.FeedTS = c.ts // the seed's reactions date from their message
		}
		e.Unread = tsBefore(w.seedRead[c.ch], c.ts)
	}
	sort.SliceStable(w.activity, func(i, j int) bool { return tsBefore(w.activity[j].FeedTS, w.activity[i].FeedTS) })
}

// activityFeed answers activity.feed.
func (w *World) activityFeed(f url.Values) any {
	types := map[string]bool{}
	for _, t := range strings.Split(f.Get("types"), ",") {
		if t != "" {
			types[t] = true
		}
	}
	limit, _ := strconv.Atoi(f.Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	off, _ := strconv.Atoi(strings.TrimPrefix(f.Get("cursor"), "demo:"))
	archived := f.Get("archive_only") == "true"
	unreadOnly := f.Get("unread_only") == "true"
	w.mu.Lock()
	defer w.mu.Unlock()
	var all []map[string]any
	for _, e := range w.activity {
		if (len(types) > 0 && !types[e.Type]) || e.Archived != archived || (unreadOnly && !e.Unread) {
			continue
		}
		all = append(all, e.json())
	}
	items := []map[string]any{}
	if off < len(all) {
		items = all[off:min(len(all), off+limit)]
	}
	next := ""
	if off+limit < len(all) {
		next = "demo:" + strconv.Itoa(off+limit)
	}
	return okMap("items", items, "response_metadata", map[string]any{"next_cursor": next})
}

// activityWrite answers activity.markRead / markUnread / archive /
// unarchive / markAllRead.
func (w *World) activityWrite(method string, f url.Values) any {
	w.mu.Lock()
	defer w.mu.Unlock()
	keys := map[string]bool{}
	if k := f.Get("key"); k != "" {
		keys[k] = true
	}
	if es := f.Get("entries"); es != "" {
		var list []struct {
			Key string `json:"key"`
		}
		_ = jsonUnmarshal(es, &list)
		for _, e := range list {
			keys[e.Key] = true
		}
	}
	types := map[string]bool{}
	for _, t := range strings.Split(f.Get("types"), ",") {
		types[t] = true
	}
	for _, e := range w.activity {
		if method == "activity.markAllRead" {
			if types[e.Type] || f.Get("types") == "" {
				e.Unread = false
			}
			continue
		}
		if !keys[e.Key] {
			continue
		}
		switch method {
		case "activity.markRead":
			e.Unread = false
		case "activity.markUnread":
			e.Unread = true
		case "activity.archive":
			e.Archived = true
		case "activity.unarchive":
			e.Archived = false
		}
	}
	return okMap()
}
