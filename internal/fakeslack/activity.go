package fakeslack

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
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
//   - activity.views answers the saved views (the defaults the user
//     didn't hide, then the custom ones) and the global prefs ({sort,
//     density}); activity.prefs.update {density, sort} changes them,
//     activity.views.update {view_id, density, sort, name, emoji,
//     filters} one view; activity.views.create {name, emoji, sort,
//     density, filters} adds a custom view (Slack's errors for a taken or
//     default name and past 15 views), activity.views.delete {view_id}
//     drops one and activity.views.hide / unhide {view_type} hide and
//     bring back a default one (all recorded too).
//   - activity.feed also filters by priority_only (the entries marked
//     "priority", from a VIP), channel_ids and channel_section_ids (the
//     sections' members, users.channelSections.list).
//   - The bulk writes: activity.markRead / activity.archive {reason,
//     entries} and activity.clearAll {reason, filters} (clears every
//     entry the filters match).
//   - The rows' own actions (docs/ACTIVITY.md "Row actions"): saved.get
//     answers the reminder rows' saved items and saved.update changes them
//     (Mark Complete, Reschedule); files.info names the file reminder's
//     file; the Slack Connect invites' sharedInvites.canAccept / accept /
//     ignoreInvite / ignoreUser and conversations.ignoreInvite /
//     ignoreUser; activity.clearAll {undo_key} (the clear-all toast's
//     Undo). Every one is recorded (ActivityWrites).

// The Activity fixture's messages, in ViewsChannel and the first DM.
var activityMsgs = []slack.Message{
	viewsMsg(ViewsChannel, "1762700100.000100", "", "U0FAKE00001", "<@"+SelfID+"> can you look at the synthetic plan?"),
	viewsMsg(ViewsChannel, "1762700200.000100", "", "U0FAKE00002", "<!channel> synthetic heads-up for everyone"),
	viewsMsg(ViewsChannel, "1762700300.000100", "", "U0FAKE00001", "<!subteam^S0FAKE1> synthetic group ping"),
	viewsMsg(ViewsChannel, "1762700400.000100", "", "U0FAKE00002", "a synthetic keyword match"),
	viewsMsg(ViewsChannel, "1762700500.000100", "", SelfID, "a synthetic message people liked"),
	viewsMsg(ViewsChannel, "1762700600.000100", "", "U0FAKE00002", "a synthetic post in a channel set to all new posts"),
	viewsMsg("D0FAKE00000", "1762700700.000100", "", "U0FAKE00001", "a synthetic direct message"),
	viewsMsg("D0FAKE00000", "1762700750.000100", "", "U0FAKE00001", "a synthetic direct message you reacted to"),
	viewsMsg(ActivitySlackbotDM, "1762701900.000100", "", "USLACKBOT", "a synthetic note from Slackbot"),
}

// The Activity fixture's extra conversations and ids.
const (
	ActivitySlackbotDM   = "D0FAKESLKBT" // your DM with Slackbot
	ActivityReminderFile = "F0FAKEREM1"  // the file a reminder is about
	ActivityInviteDM     = "I0FAKE2"     // the Slack Connect DM invite
	ActivityInviteSig    = "sig-fake-dm"
)

func init() {
	activityMsgs[4].Reactions = []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"U0FAKE00001", "U0FAKE00002"}}}
	activityMsgs[7].Reactions = []slack.Reaction{{Name: "eyes", Count: 1, Users: []string{SelfID}}}
}

// activityEntries is the feed, newest first; base is the server's URL
// (the inviting organisations' icons).
func activityEntries(base string) []map[string]any {
	msg := func(ts, user string) map[string]any {
		return map[string]any{"ts": ts, "channel": ViewsChannel, "author_user_id": user}
	}
	e := func(key, feedTS string, unread bool, item map[string]any) map[string]any {
		return map[string]any{"key": key, "feed_ts": feedTS, "is_unread": unread, "item": item}
	}
	// priority marks an entry from a VIP (Slack's entry.priority)
	priority := func(m map[string]any) map[string]any {
		m["priority"] = map[string]any{"score": 1}
		return m
	}
	team := func(id, name string, verified bool) map[string]any {
		return map[string]any{"id": id, "name": name, "is_verified": verified, "date_created": 1546300800,
			"icon": map[string]any{"image_34": base + "img/34x34/partner.png", "image_44": base + "img/44x44/partner.png"}}
	}
	return []map[string]any{
		e("fa-ext-dm-invite", "1762702000.000100", true, map[string]any{"type": "external_dm_invite",
			"invite": map[string]any{"id": ActivityInviteDM, "sig": ActivityInviteSig, "invite_type": "im", "direction": "incoming", "status": "sent",
				"inviting_team": team(ExternalTeamID, "Partner Co", false),
				"inviting_user": map[string]any{"id": "U0PARTNER1", "name": "pat",
					"profile": map[string]any{"real_name": "Pat Partner", "display_name": "pat", "email": "pat@partner.example"}}}}),
		e("fa-slackbot", "1762701900.000100", false, map[string]any{"type": "dm", "bundle_info": map[string]any{"unread_count": 0,
			"payload": map[string]any{"dm_entry": map[string]any{"latest_message": map[string]any{"ts": "1762701900.000100", "channel": ActivitySlackbotDM}}}}}),
		e("fa-reminder-file", "1762701800.000100", false, map[string]any{"type": "saved_reminder",
			"saved_reminder_payload": map[string]any{"item_type": "file", "item_id": ActivityReminderFile}}),
		e("fa-reminder-done", "1762701700.000100", false, map[string]any{"type": "saved_reminder",
			"saved_reminder_payload": map[string]any{"item_type": "reminder", "item_id": "Rm0FAKE2", "item_detail": "send the synthetic report"}}),
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
			"invite": map[string]any{"id": "I0FAKE1", "sig": "sig-fake-channel", "invite_type": "channel", "direction": "incoming", "status": "sent",
				"channel":       map[string]any{"id": "C0PARTNER1", "name": "synthetic-partners"},
				"team":          map[string]any{"id": ExternalTeamID, "name": "Partner Co"},
				"inviting_team": team(ExternalTeamID, "Partner Co", true),
				"inviting_user": map[string]any{"id": "U0PARTNER1", "name": "pat", "profile": map[string]any{"real_name": "Pat Partner", "display_name": "pat"}}}}),
		e("fa-invite", "1762700900.000100", true, map[string]any{"type": "internal_channel_invite", "message": msg("1762700100.000100", "U0FAKE00001")}),
		e("fa-dm-reacted", "1762700750.000100", false, map[string]any{"type": "dm", "bundle_info": map[string]any{"unread_count": 0,
			"payload": map[string]any{"dm_entry": map[string]any{"latest_message": map[string]any{"ts": "1762700750.000100", "channel": "D0FAKE00000"}}}}}),
		priority(e("fa-dm", "1762700700.000100", true, map[string]any{"type": "dm", "bundle_info": map[string]any{"unread_count": 1,
			"payload": map[string]any{"dm_entry": map[string]any{"latest_message": map[string]any{"ts": "1762700700.000100", "channel": "D0FAKE00000"}}}}})),
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
		priority(e("fa-at-user", "1762700100.000100", true, map[string]any{"type": "at_user", "message": msg("1762700100.000100", "U0FAKE00001")})),
	}
}

// ActivityKeys is every key in the fixture feed, newest first.
func ActivityKeys() []string {
	var out []string
	for _, e := range activityEntries("") {
		out = append(out, e["key"].(string))
	}
	return out
}

type activityState struct {
	read      map[string]bool // true read, false unread (overrides the fixture)
	archived  map[string]bool
	density   string            // activity.prefs.update's density ("" = "compact")
	sort      string            // activity.prefs.update's sort ("" = "newest")
	viewDense map[string]string // activity.views.update's density by view id
	saved     map[string]map[string]any
	ignored   map[string]bool // invites ignored or accepted

	// the saved views: activity.views.create's, in order, and the default
	// view types activity.views.hide hid
	custom []map[string]any
	hidden map[string]bool
	seq    int
}

func (v *viewsState) act() *activityState {
	if v.activity == nil {
		v.activity = &activityState{read: map[string]bool{}, archived: map[string]bool{}, viewDense: map[string]string{}, ignored: map[string]bool{},
			hidden: map[string]bool{}}
		v.activity.saved = activitySaved()
	}
	return v.activity
}

// activitySaved is the reminder rows' saved items, by item id (and ts for
// messages): the message reminder is a to-do due in 2030, the file
// reminder and the plain reminder are overdue to-dos, Rm0FAKE2 is done.
func activitySaved() map[string]map[string]any {
	item := func(id, typ, ts, state string, due, done int64) map[string]any {
		return map[string]any{"item_id": id, "item_type": typ, "ts": ts, "state": "in_progress", "todo_state": state,
			"date_created": int64(1762600000), "date_due": due, "date_completed": done}
	}
	return map[string]map[string]any{
		ViewsChannel + "/1762700100.000100": item(ViewsChannel, "message", "1762700100.000100", "to_do", 1893456000, 0), // due 2030
		ActivityReminderFile:                item(ActivityReminderFile, "file", "", "to_do", 1762790000, 0),
		"Rm0FAKE1":                          item("Rm0FAKE1", "reminder", "", "to_do", 1762701300, 0),
		"Rm0FAKE2":                          item("Rm0FAKE2", "reminder", "", "completed", 0, 1762701750),
	}
}

func savedID(f url.Values) string {
	if f.Get("item_type") == "message" {
		return f.Get("item_id") + "/" + f.Get("ts")
	}
	return f.Get("item_id")
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
		priorityOnly := f.Get("priority_only") == "true"
		inChans := s.activityChannels(f.Get("channel_ids"), f.Get("channel_section_ids"))
		v.mu.Lock()
		st := v.act()
		var items []map[string]any
		for _, e := range activityEntries(s.URL) {
			key := e["key"].(string)
			typ := e["item"].(map[string]any)["type"].(string)
			if len(types) > 0 && !types[typ] {
				continue
			}
			if priorityOnly && e["priority"] == nil {
				continue
			}
			if inChans != nil && !inChans[activityEntryChannel(e)] {
				continue
			}
			if r, ok := st.read[key]; ok {
				e["is_unread"] = !r
			}
			if st.archived[key] != archiveOnly || st.ignored[key] {
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
		views, prefs := v.act().viewsLocked()
		v.mu.Unlock()
		return ok("views", views, "prefs", prefs), true
	case "activity.prefs.update":
		v.mu.Lock()
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		if d := f.Get("density"); d != "" {
			v.act().density = d
		}
		if x := f.Get("sort"); x != "" {
			v.act().sort = x
		}
		v.mu.Unlock()
		return ok(), true
	case "activity.views.create", "activity.views.update", "activity.views.delete", "activity.views.hide", "activity.views.unhide":
		v.mu.Lock()
		defer v.mu.Unlock()
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		out, errCode := v.act().viewWrite(method, f)
		if errCode != "" {
			return map[string]any{"ok": false, "error": errCode}, true
		}
		if out == nil {
			return ok(), true
		}
		return ok("view", out), true
	case "saved.get":
		var want []struct {
			ItemID   string `json:"item_id"`
			ItemType string `json:"item_type"`
			TS       string `json:"ts"`
		}
		_ = json.Unmarshal([]byte(f.Get("items")), &want)
		v.mu.Lock()
		st := v.act()
		var items []map[string]any
		for _, w := range want {
			id := w.ItemID
			if w.ItemType == "message" {
				id += "/" + w.TS
			}
			if it, found := st.saved[id]; found {
				items = append(items, it)
			}
		}
		v.mu.Unlock()
		return ok("saved_items", items), true
	case "saved.update":
		v.mu.Lock()
		st := v.act()
		it, found := st.saved[savedID(f)]
		if !found {
			v.mu.Unlock()
			return nil, false // the saved fixture's own items
		}
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		if ts := f.Get("todo_state"); ts != "" {
			it["todo_state"] = ts
		}
		if f.Get("mark") == "completed" {
			it["date_completed"] = int64(1762702100)
		}
		if d := f.Get("date_due"); d != "" {
			n, _ := strconv.ParseInt(d, 10, 64)
			it["date_due"] = n
		}
		v.mu.Unlock()
		return ok(), true
	case "files.info":
		if f.Get("file") != ActivityReminderFile {
			return nil, false
		}
		return ok("file", map[string]any{"id": ActivityReminderFile, "name": "synthetic-notes.txt", "title": "synthetic-notes.txt",
			"mimetype": "text/plain", "filetype": "text", "pretty_type": "Plain Text", "mode": "hosted", "user": "U0FAKE00001"}), true
	case "sharedInvites.canAccept":
		v.mu.Lock()
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		v.mu.Unlock()
		return ok("is_eligible", f.Get("sig") == ActivityInviteSig), true
	case "sharedInvites.accept", "sharedInvites.ignoreInvite", "sharedInvites.ignoreUser", "conversations.ignoreInvite", "conversations.ignoreUser":
		v.mu.Lock()
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		for _, e := range activityEntries("") {
			if inv, _ := e["item"].(map[string]any)["invite"].(map[string]any); inv != nil && inv["id"] == f.Get("invite") {
				v.act().ignored[e["key"].(string)] = true
			}
		}
		v.mu.Unlock()
		if method == "sharedInvites.accept" {
			return ok("channel", "D0PARTNER1"), true
		}
		return ok(), true
	case "activity.clearAll":
		var cf activityClearFilters
		hasFilters := json.Unmarshal([]byte(f.Get("filters")), &cf) == nil
		inChans := s.activityChannels(strings.Join(cf.ChannelIDs, ","), strings.Join(cf.SectionIDs, ","))
		v.mu.Lock()
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		st := v.act()
		switch {
		case f.Get("undo_key") != "":
			st.archived = map[string]bool{}
		case hasFilters:
			// ClearAllFilteredDialog: everything the filters match
			types := map[string]bool{}
			for _, t := range cf.EntryTypes {
				types[t] = true
			}
			for _, e := range activityEntries(s.URL) {
				key := e["key"].(string)
				typ := e["item"].(map[string]any)["type"].(string)
				unread := e["is_unread"] == true
				if r, ok := st.read[key]; ok {
					unread = !r
				}
				if len(types) > 0 && !types[typ] || cf.UnreadOnly && !unread || cf.PriorityOnly && e["priority"] == nil ||
					inChans != nil && !inChans[activityEntryChannel(e)] || st.archived[key] != cf.ArchiveOnly {
					continue
				}
				st.archived[key] = true
			}
		}
		v.mu.Unlock()
		return ok(), true
	case "activity.markRead", "activity.markUnread", "activity.archive", "activity.unarchive", "activity.markAllRead", "activity.clearBefore":
		v.mu.Lock()
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		st := v.act()
		// the bulk writes send {reason, entries} (bulk_mark_read, bulk_archive)
		var bulk []struct {
			Key string `json:"key"`
		}
		_ = json.Unmarshal([]byte(f.Get("entries")), &bulk)
		switch method {
		case "activity.markRead":
			if k := f.Get("key"); k != "" {
				st.read[k] = true
			}
			for _, e := range bulk {
				st.read[e.Key] = true
			}
		case "activity.markUnread":
			var es []struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal([]byte(f.Get("entries")), &es)
			for _, e := range es {
				st.read[e.Key] = false
			}
		case "activity.archive":
			if k := f.Get("key"); k != "" {
				st.archived[k] = true
			}
			for _, e := range bulk {
				st.archived[e.Key] = true
			}
		case "activity.unarchive":
			delete(st.archived, f.Get("key"))
		case "activity.markAllRead":
			for _, k := range ActivityKeys() {
				st.read[k] = true
			}
		case "activity.clearBefore":
			before := f.Get("before_ts")
			for _, e := range activityEntries(s.URL) {
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

// activityDefaultViews are the default saved views, by view type.
var activityDefaultViews = []string{"all", "dms", "mentions", "threads"}

// activityDefaultNames are the default views' names, which a custom view
// can't take (cannot_use_default_view_name).
var activityDefaultNames = map[string]bool{"all": true, "dms": true, "mentions": true, "threads": true}

// viewsLocked is activity.views' answer: the default views the user didn't
// hide, then the custom ones, and the global prefs.
func (st *activityState) viewsLocked() ([]map[string]any, map[string]any) {
	d := st.density
	if d == "" {
		d = "compact"
	}
	sort := st.sort
	if sort == "" {
		sort = "newest"
	}
	var views []map[string]any
	for i, t := range activityDefaultViews {
		if st.hidden[t] {
			continue
		}
		id, vd := "Vw0FAKE"+strconv.Itoa(i), d
		if x := st.viewDense[id]; x != "" {
			vd = x
		}
		views = append(views, map[string]any{"id": id, "view_type": t, "position": strconv.Itoa(i), "density": vd, "sort": sort})
	}
	for _, c := range st.custom {
		cp := map[string]any{}
		for k, x := range c {
			cp[k] = x
		}
		views = append(views, cp)
	}
	return views, map[string]any{"sort": sort, "density": d}
}

func (st *activityState) customIndex(id string) int {
	for i, c := range st.custom {
		if c["id"] == id {
			return i
		}
	}
	return -1
}

// viewWrite is activity.views.create / update / delete / hide / unhide:
// the view Slack answers (nil for none) or an error code.
func (st *activityState) viewWrite(method string, f url.Values) (map[string]any, string) {
	filters := func() map[string]any {
		var m map[string]any
		if json.Unmarshal([]byte(f.Get("filters")), &m) != nil || m == nil {
			m = map[string]any{}
		}
		return m
	}
	taken := func(name, except string) bool {
		if activityDefaultNames[strings.ToLower(name)] {
			return true
		}
		for _, c := range st.custom {
			if c["id"] != except && strings.EqualFold(c["name"].(string), name) {
				return true
			}
		}
		return false
	}
	switch method {
	case "activity.views.create":
		name := f.Get("name")
		switch {
		case len([]rune(name)) > 80:
			return nil, "name_too_long"
		case activityDefaultNames[strings.ToLower(name)]:
			return nil, "cannot_use_default_view_name"
		case taken(name, ""):
			return nil, "duplicate_name"
		case len(activityDefaultViews)+len(st.custom) >= 15:
			return nil, "over_view_maximum"
		}
		st.seq++
		vw := map[string]any{"id": "Vw0FAKEC" + strconv.Itoa(st.seq), "name": name, "view_type": "custom",
			"position": strconv.Itoa(len(activityDefaultViews) + len(st.custom)), "sort": f.Get("sort"), "density": f.Get("density"),
			"filters": filters()}
		if e := f.Get("emoji"); e != "" {
			vw["emoji"] = e
		}
		st.custom = append(st.custom, vw)
		return vw, ""
	case "activity.views.update":
		id := f.Get("view_id")
		i := st.customIndex(id)
		if i < 0 {
			// a default view: only its density and sort
			for n, t := range activityDefaultViews {
				if "Vw0FAKE"+strconv.Itoa(n) == id {
					if f.Has("name") {
						return nil, "cannot_change_default_view_name"
					}
					if d := f.Get("density"); d != "" {
						st.viewDense[id] = d
					}
					return map[string]any{"id": id, "view_type": t, "density": f.Get("density"), "sort": f.Get("sort")}, ""
				}
			}
			return nil, "view_not_found"
		}
		vw := st.custom[i]
		if f.Has("name") {
			name := f.Get("name")
			if len([]rune(name)) > 80 {
				return nil, "name_too_long"
			}
			if taken(name, id) {
				return nil, "duplicate_name"
			}
			vw["name"] = name
		}
		if f.Has("emoji") {
			if e := f.Get("emoji"); e != "" {
				vw["emoji"] = e
			} else {
				delete(vw, "emoji")
			}
		}
		if f.Has("filters") {
			vw["filters"] = filters()
		}
		if d := f.Get("density"); d != "" {
			vw["density"] = d
		}
		if x := f.Get("sort"); x != "" {
			vw["sort"] = x
		}
		return vw, ""
	case "activity.views.delete":
		i := st.customIndex(f.Get("view_id"))
		if i < 0 {
			return nil, "cannot_delete_default_view"
		}
		st.custom = append(st.custom[:i], st.custom[i+1:]...)
	case "activity.views.hide", "activity.views.unhide":
		t := f.Get("view_type")
		if t != "dms" && t != "mentions" && t != "threads" {
			return nil, "invalid_view_type"
		}
		st.hidden[t] = method == "activity.views.hide"
	}
	return nil, ""
}

// activityClearFilters are activity.clearAll's filters.
type activityClearFilters struct {
	EntryTypes   []string `json:"entry_types"`
	ChannelIDs   []string `json:"channel_ids"`
	SectionIDs   []string `json:"channel_section_ids"`
	UnreadOnly   bool     `json:"unread_only"`
	PriorityOnly bool     `json:"priority_only"`
	ArchiveOnly  bool     `json:"archive_only"`
}

// activityChannels is the conversations channel_ids and
// channel_section_ids (comma-separated) name: the ids and the sections'
// members; nil when neither is set.
func (s *Server) activityChannels(chanIDs, sectionIDs string) map[string]bool {
	if chanIDs == "" && sectionIDs == "" {
		return nil
	}
	out := map[string]bool{}
	for _, id := range strings.Split(chanIDs, ",") {
		if id != "" {
			out[id] = true
		}
	}
	want := map[string]bool{}
	for _, id := range strings.Split(sectionIDs, ",") {
		if id != "" {
			want[id] = true
		}
	}
	if len(want) > 0 {
		for _, sec := range s.Sections() {
			if want[sec.ID] {
				for _, ch := range sec.Channels {
					out[ch] = true
				}
			}
		}
	}
	return out
}

// activityEntryChannel is the conversation a feed entry is about (sD/F's
// channelsAndOrSectionsInFilter lookup).
func activityEntryChannel(e map[string]any) string {
	it, _ := e["item"].(map[string]any)
	if m, _ := it["message"].(map[string]any); m != nil {
		if ch, _ := m["channel"].(string); ch != "" {
			return ch
		}
	}
	bi, _ := it["bundle_info"].(map[string]any)
	pl, _ := bi["payload"].(map[string]any)
	for _, k := range []string{"dm_entry", "channel_entry"} {
		if en, _ := pl[k].(map[string]any); en != nil {
			if lm, _ := en["latest_message"].(map[string]any); lm != nil {
				if ch, _ := lm["channel"].(string); ch != "" {
					return ch
				}
			}
		}
	}
	if te, _ := pl["thread_entry"].(map[string]any); te != nil {
		ch, _ := te["channel_id"].(string)
		return ch
	}
	return ""
}
