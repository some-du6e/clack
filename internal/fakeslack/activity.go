package fakeslack

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// The Activity inbox (Size.Views), made-up content only:
//
//   - activity.feed {types, unread_only, archive_only, limit, cursor}:
//     one entry per item type Slack's Activity view renders (the shapes of
//     internal/core/testdata/activity_feed.json), about ViewsChannel's
//     messages, newest first, filtered by types / unread_only /
//     archive_only like Slack's.
//   - messages.list answers those messages (and any channel's history).
//   - activity.markRead / markUnread change an entry's read state,
//     archive / unarchive clear and restore it, clearBefore {before_ts}
//     clears everything older, markAllRead reads everything; all are
//     recorded (ActivityWrites).
//   - activity.views answers the default saved views and the global prefs
//     ({sort, density}); activity.prefs.update {density, sort} changes
//     them (recorded too).

// The Activity fixture's messages, in ViewsChannel and the first DM.
var activityMsgs = []slack.Message{
	viewsMsg(ViewsChannel, "1762700100.000100", "", "U0FAKE00001", "<@"+SelfID+"> can you look at the synthetic plan?"),
	viewsMsg(ViewsChannel, "1762700200.000100", "", "U0FAKE00002", "<!channel> synthetic heads-up for everyone"),
	viewsMsg(ViewsChannel, "1762700300.000100", "", "U0FAKE00001", "<!subteam^S0FAKE1> synthetic group ping"),
	viewsMsg(ViewsChannel, "1762700400.000100", "", "U0FAKE00002", "a synthetic keyword match"),
	viewsMsg(ViewsChannel, "1762700500.000100", "", SelfID, "a synthetic message people liked"),
	viewsMsg(ViewsChannel, "1762700600.000100", "", "U0FAKE00002", "a synthetic post in a channel set to all new posts"),
	viewsMsg("D0FAKE00000", "1762700700.000100", "", "U0FAKE00001", "a synthetic direct message"),
}

func init() {
	activityMsgs[4].Reactions = []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"U0FAKE00001", "U0FAKE00002"}}}
}

// activityEntries is the feed, newest first.
func activityEntries() []map[string]any {
	msg := func(ts, user string) map[string]any {
		return map[string]any{"ts": ts, "channel": ViewsChannel, "author_user_id": user}
	}
	e := func(key, feedTS string, unread bool, item map[string]any) map[string]any {
		return map[string]any{"key": key, "feed_ts": feedTS, "is_unread": unread, "item": item}
	}
	return []map[string]any{
		e("fa-alert", "1762701400.000100", true, map[string]any{"type": "generic_system_alert",
			"generic_system_alert_payload": map[string]any{"category": "CHANNEL", "click_target_id": ViewsChannel,
				"blocks": []any{map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "A synthetic channel was archived."}}}}}),
		e("fa-reminder-plain", "1762701300.000100", true, map[string]any{"type": "saved_reminder",
			"saved_reminder_payload": map[string]any{"item_type": "reminder", "item_id": "Rm0FAKE1", "item_detail": "water the synthetic plants"}}),
		e("fa-reminder", "1762701200.000100", true, map[string]any{"type": "saved_reminder",
			"saved_reminder_payload": map[string]any{"item_type": "message", "item_id": ViewsChannel, "item_id_2": "1762700100.000100"}}),
		e("fa-joiner", "1762701100.000100", true, map[string]any{"type": "team_joiner_invite",
			"team_joiner_invite_payload": map[string]any{"channel_id": ViewsChannel, "user_id": "U0FAKE00002", "is_reminder": false}}),
		e("fa-ext-invite", "1762701000.000100", true, map[string]any{"type": "external_channel_invite",
			"invite": map[string]any{"id": "I0FAKE1", "invite_type": "channel", "channel": map[string]any{"id": "C0PARTNER1", "name": "synthetic-partners"},
				"team":          map[string]any{"id": ExternalTeamID, "name": "Partner Co"},
				"inviting_user": map[string]any{"id": "U0PARTNER1", "name": "pat", "profile": map[string]any{"real_name": "Pat Partner", "display_name": "pat"}}}}),
		e("fa-invite", "1762700900.000100", true, map[string]any{"type": "internal_channel_invite", "message": msg("1762700100.000100", "U0FAKE00001")}),
		e("fa-dm", "1762700700.000100", true, map[string]any{"type": "dm", "bundle_info": map[string]any{"unread_count": 1,
			"payload": map[string]any{"dm_entry": map[string]any{"latest_message": map[string]any{"ts": "1762700700.000100", "channel": "D0FAKE00000"}}}}}),
		e("fa-channel", "1762700600.000100", false, map[string]any{"type": "channel", "bundle_info": map[string]any{"unread_count": 1,
			"payload": map[string]any{"channel_entry": map[string]any{"latest_message": map[string]any{"ts": "1762700600.000100", "channel": ViewsChannel}}}}}),
		e("fa-reaction", "1762700550.000100", true, map[string]any{"type": "message_reaction", "message": msg("1762700500.000100", SelfID),
			"reaction": map[string]any{"user": "U0FAKE00001", "name": "tada"}}),
		e("fa-keyword", "1762700400.000100", false, map[string]any{"type": "keyword", "message": msg("1762700400.000100", "U0FAKE00002")}),
		e("fa-thread", "1762600200.000100", true, map[string]any{"type": "thread_v2", "bundle_info": map[string]any{
			"payload": map[string]any{"thread_entry": map[string]any{"channel_id": ViewsChannel, "latest_ts": "1762600200.000100",
				"thread_ts": ViewsThreadUnread, "unread_msg_count": 1}}}}),
		e("fa-at-group", "1762700300.000100", true, map[string]any{"type": "at_user_group", "message": msg("1762700300.000100", "U0FAKE00001")}),
		e("fa-at-channel", "1762700200.000100", false, map[string]any{"type": "at_channel", "message": msg("1762700200.000100", "U0FAKE00002")}),
		e("fa-at-user", "1762700100.000100", true, map[string]any{"type": "at_user", "message": msg("1762700100.000100", "U0FAKE00001")}),
	}
}

// ActivityKeys is every key in the fixture feed, newest first.
func ActivityKeys() []string {
	var out []string
	for _, e := range activityEntries() {
		out = append(out, e["key"].(string))
	}
	return out
}

type activityState struct {
	read     map[string]bool // true read, false unread (overrides the fixture)
	archived map[string]bool
	density  string // activity.prefs.update's density ("" = "compact")
}

func (v *viewsState) act() *activityState {
	if v.activity == nil {
		v.activity = &activityState{read: map[string]bool{}, archived: map[string]bool{}}
	}
	return v.activity
}

// handleActivity answers the Activity fixture's methods.
func (s *Server) handleActivity(method string, f url.Values) (any, bool) {
	v := s.views()
	switch method {
	case "activity.feed":
		types := map[string]bool{}
		for _, t := range strings.Split(f.Get("types"), ",") {
			if t != "" {
				types[t] = true
			}
		}
		unreadOnly, archiveOnly := f.Get("unread_only") == "true", f.Get("archive_only") == "true"
		v.mu.Lock()
		st := v.act()
		var items []map[string]any
		for _, e := range activityEntries() {
			key := e["key"].(string)
			typ := e["item"].(map[string]any)["type"].(string)
			if len(types) > 0 && !types[typ] {
				continue
			}
			if r, ok := st.read[key]; ok {
				e["is_unread"] = !r
			}
			if st.archived[key] != archiveOnly {
				continue
			}
			if unreadOnly && e["is_unread"] != true {
				continue
			}
			e["is_archived"] = st.archived[key]
			items = append(items, e)
		}
		v.mu.Unlock()
		return ok("items", items, "response_metadata", map[string]any{"next_cursor": ""}), true
	case "messages.list":
		var want []struct {
			Channel    string   `json:"channel"`
			Timestamps []string `json:"timestamps"`
		}
		if json.Unmarshal([]byte(f.Get("message_ids")), &want) != nil {
			return nil, false
		}
		out := map[string][]slack.Message{}
		for _, w := range want {
			for _, ts := range w.Timestamps {
				for _, m := range activityMsgs {
					if m.Channel == w.Channel && m.TS == ts {
						out[w.Channel] = append(out[w.Channel], m)
					}
				}
			}
		}
		return ok("messages", out), true
	case "activity.views":
		v.mu.Lock()
		d := v.act().density
		v.mu.Unlock()
		if d == "" {
			d = "compact"
		}
		var views []map[string]any
		for i, t := range []string{"all", "dms", "mentions", "threads"} {
			views = append(views, map[string]any{"id": "Vw0FAKE" + strconv.Itoa(i), "view_type": t, "position": i, "density": d, "sort": "newest"})
		}
		return ok("views", views, "prefs", map[string]any{"sort": "newest", "density": d}), true
	case "activity.prefs.update":
		v.mu.Lock()
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		if d := f.Get("density"); d != "" {
			v.act().density = d
		}
		v.mu.Unlock()
		return ok(), true
	case "activity.markRead", "activity.markUnread", "activity.archive", "activity.unarchive", "activity.markAllRead", "activity.clearBefore":
		v.mu.Lock()
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		st := v.act()
		switch method {
		case "activity.markRead":
			st.read[f.Get("key")] = true
		case "activity.markUnread":
			var es []struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal([]byte(f.Get("entries")), &es)
			for _, e := range es {
				st.read[e.Key] = false
			}
		case "activity.archive":
			st.archived[f.Get("key")] = true
		case "activity.unarchive":
			delete(st.archived, f.Get("key"))
		case "activity.markAllRead":
			for _, k := range ActivityKeys() {
				st.read[k] = true
			}
		case "activity.clearBefore":
			before := f.Get("before_ts")
			for _, e := range activityEntries() {
				if ts := e["feed_ts"].(string); before != "" && ts < before {
					st.archived[e["key"].(string)] = true
				}
			}
		}
		v.mu.Unlock()
		return ok(), true
	}
	return nil, false
}

// ActivityArchived returns the cleared keys, sorted.
func (s *Server) ActivityArchived() []string {
	v := s.views()
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []string
	for k, on := range v.act().archived {
		if on {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
