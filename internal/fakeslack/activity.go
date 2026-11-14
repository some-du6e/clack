package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
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
//     sections' members, users.channelSections.list), and by the
//     entries' is_bot: exclude_automations drops the automations' (your
//     Slackbot DM is the one), automations_only keeps only them.
//   - activity.views.update {next_view_id} moves the view before that
//     one ("" to the end) and numbers every view's position again (GuYW's
//     tab drag, 4gYA R).
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
//   - The channel invite's Accept (core/connect_accept.go):
//     conversations.sharedInviteAcceptanceEligibility (eligible unless
//     SetChannelInviteEligibility says otherwise),
//     conversations.acceptSharedInvite / joinConnectedShared (the
//     invite leaves the feed) and connectInvites.list (the accepted
//     channel, pending until SetChannelInviteApproved), all recorded.

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
	// the Slack Connect channel invite (#synthetic-partners from Partner Co)
	ActivityInviteChannel    = "I0FAKE1"
	ActivityInviteChannelSig = "sig-fake-channel"
	ActivityInviteChannelID  = "C0PARTNER1"
)

func init() {
	activityMsgs[4].Reactions = []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"U0FAKE00001", "U0FAKE00002"}}}
	activityMsgs[7].Reactions = []slack.Reaction{{Name: "eyes", Count: 1, Users: []string{SelfID}}}
	activityMsgs = append(activityMsgs, activityAgentMsg())
}

// The agent entry SetActivityAgentActions adds: an agent's DM whose
// latest message asks for a decision with one actions block of two
// buttons (docs/ACTIVITY.md "Leftovers 2": Slack's rZx3 Rq shows such a
// block under the row with agent_command_center).
const (
	ActivityAgentDM      = "D0FAKEAGNT1"
	ActivityAgentUser    = "U0FAKEAGENT"
	ActivityAgentTS      = "1762702050.000100"
	ActivityAgentBlockID = "agent-decision"
)

func activityAgentMsg() slack.Message {
	m := viewsMsg(ActivityAgentDM, ActivityAgentTS, "", ActivityAgentUser, "A synthetic agent needs a decision.")
	m.BotID = "B0FAKEAGENT"
	m.Blocks = json.RawMessage(`[{"type":"section","block_id":"agent-question","text":{"type":"mrkdwn","text":"A synthetic agent needs a decision."}},` +
		`{"type":"actions","block_id":"` + ActivityAgentBlockID + `","elements":[` +
		`{"type":"button","action_id":"agent-approve","style":"primary","value":"approve","text":{"type":"plain_text","text":"Approve"}},` +
		`{"type":"button","action_id":"agent-decline","value":"decline","text":{"type":"plain_text","text":"Decline"}}]}]`)
	return m
}

// activityAgentEntry is the agent DM's feed entry (newest in the feed).
func activityAgentEntry() map[string]any {
	return map[string]any{"key": "fa-agent", "feed_ts": ActivityAgentTS, "is_unread": true, "is_bot": true,
		"item": map[string]any{"type": "dm", "bundle_info": map[string]any{"unread_count": 1,
			"payload": map[string]any{"dm_entry": map[string]any{"latest_message": map[string]any{"ts": ActivityAgentTS, "channel": ActivityAgentDM}}}}}}
}

// SetActivityAgentActions puts the agent entry (fa-agent) at the top of
// the feed.
func (s *Server) SetActivityAgentActions(on bool) {
	v := s.views()
	v.mu.Lock()
	v.act().agent = on
	v.mu.Unlock()
}

// activityEntries is the feed, newest first; base is the server's URL
// (the inviting organisations' icons).
func activityEntries(base string) []map[string]any {
	msg := func(ts, user string) map[string]any {
		return map[string]any{"ts": ts, "channel": ViewsChannel, "author_user_id": user}
	}
	e := func(key, feedTS string, unread bool, item map[string]any) map[string]any {
		return map[string]any{"key": key, "feed_ts": feedTS, "is_unread": unread, "is_bot": false, "item": item}
	}
	// bot marks an automation's entry (Slack's entry.is_bot)
	bot := func(m map[string]any) map[string]any {
		m["is_bot"] = true
		return m
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
		bot(e("fa-slackbot", "1762701900.000100", false, map[string]any{"type": "dm", "bundle_info": map[string]any{"unread_count": 0,
			"payload": map[string]any{"dm_entry": map[string]any{"latest_message": map[string]any{"ts": "1762701900.000100", "channel": ActivitySlackbotDM}}}}})),
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
			// item.invite is the joiner's user id (Slack's senderId and the
			// row's avatar), a string like every feed item's invite
			"invite":                     "U0FAKE00002",
			"team_joiner_invite_payload": map[string]any{"channel_id": ViewsChannel, "is_reminder": false}}),
		e("fa-ext-invite", "1762701000.000100", true, map[string]any{"type": "external_channel_invite",
			"invite": map[string]any{"id": ActivityInviteChannel, "sig": ActivityInviteChannelSig, "invite_type": "channel", "direction": "incoming", "status": "sent",
				"channel":       map[string]any{"id": ActivityInviteChannelID, "name": "synthetic-partners"},
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
	inviteIDs bool            // external invites' item.invite is the id string (SetActivityInviteIDs)
	bulk      int             // extra unread mentions, and a paged feed (SetActivityBulk)
	agent     bool            // the agent entry first (SetActivityAgentActions)

	// the channel invite's accept: the eligibility answer a test set (nil:
	// eligible), the name and privacy it was accepted with, and whether
	// its acceptance is approved
	scElig     map[string]any
	scAccepted string
	scPrivate  bool
	scApproved bool

	// the saved views: activity.views.create's, in order, and the default
	// view types activity.views.hide hid
	custom []map[string]any
	hidden map[string]bool
	seq    int
	// order is every view's id in position order once a view moved
	// (activity.views.update next_view_id); nil keeps the defaults first
	order []string
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

// SetChannelInviteEligibility makes conversations.sharedInviteAcceptanceEligibility
// answer el (is_eligible, reason, requirements, alternative_actions) for
// the channel invite; nil goes back to eligible.
func (s *Server) SetChannelInviteEligibility(el map[string]any) {
	v := s.views()
	v.mu.Lock()
	v.act().scElig = el
	v.mu.Unlock()
}

// SetActivityInviteIDs makes the feed send each external invite's
// item.invite as the connect invite's id, a string, the way Slack's feed
// does (Karim's All + Unreads failed on it, 2026-11-13); connectInvites.list
// (direction incoming, no invite_ids) then answers the invites
// themselves, as Slack's inbox reads them.
func (s *Server) SetActivityInviteIDs(on bool) {
	v := s.views()
	v.mu.Lock()
	v.act().inviteIDs = on
	v.mu.Unlock()
}

// SetActivityBulk adds n unread at_user entries after the fixture's
// (older), makes activity.feed answer in pages of its limit with a
// next_cursor, as the server does, and sets client.counts' activity_v2
// to the feed's unread entries by type (what the server counts). Karim
// saw a badge of 27 over a list of 3, 2026-11-13.
func (s *Server) SetActivityBulk(n int) {
	v := s.views()
	v.mu.Lock()
	v.act().bulk = n
	v.mu.Unlock()
	counts := map[string]int{}
	for _, e := range append(activityEntries(""), activityBulk(n)...) {
		if e["is_unread"] == true {
			counts[e["item"].(map[string]any)["type"].(string)]++
		}
	}
	s.SetActivityBadges(counts)
}

// activityBulk is SetActivityBulk's entries, newest first.
func activityBulk(n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := range n {
		ts := fmt.Sprintf("%d.000100", 1762590000-i*10)
		out = append(out, map[string]any{"key": fmt.Sprintf("fa-bulk-%02d", i), "feed_ts": ts, "is_unread": true, "is_bot": false,
			"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": "1762700100.000100", "channel": ViewsChannel, "author_user_id": "U0FAKE00001"}}})
	}
	return out
}

// SetChannelInviteApproved approves the channel invite's acceptance in
// connectInvites.list (both organisations reviewed it).
func (s *Server) SetChannelInviteApproved(on bool) {
	v := s.views()
	v.mu.Lock()
	v.act().scApproved = on
	v.mu.Unlock()
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
		exclAuto, autoOnly := f.Get("exclude_automations") == "true", f.Get("automations_only") == "true"
		inChans := s.activityChannels(f.Get("channel_ids"), f.Get("channel_section_ids"))
		v.mu.Lock()
		st := v.act()
		var items []map[string]any
		entries := activityEntries(s.URL)
		if st.agent {
			entries = append([]map[string]any{activityAgentEntry()}, entries...)
		}
		bulk := st.bulk
		if bulk > 0 {
			entries = append(entries, activityBulk(bulk)...)
		}
		for _, e := range entries {
			key := e["key"].(string)
			typ := e["item"].(map[string]any)["type"].(string)
			if len(types) > 0 && !types[typ] {
				continue
			}
			if priorityOnly && e["priority"] == nil {
				continue
			}
			if isBot := e["is_bot"] == true; exclAuto && isBot || autoOnly && !isBot {
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
			if it := e["item"].(map[string]any); st.inviteIDs {
				if inv, _ := it["invite"].(map[string]any); inv != nil {
					it["invite"] = inv["id"]
				}
			}
			items = append(items, e)
		}
		v.mu.Unlock()
		next := ""
		if bulk > 0 {
			// pages of limit, the cursor an offset
			off, _ := strconv.Atoi(strings.TrimPrefix(f.Get("cursor"), "o:"))
			limit, _ := strconv.Atoi(f.Get("limit"))
			if limit <= 0 {
				limit = 20
			}
			off = min(off, len(items))
			end := min(off+limit, len(items))
			if end < len(items) {
				next = "o:" + strconv.Itoa(end)
			}
			items = items[off:end]
		}
		return ok("items", items, "response_metadata", map[string]any{"next_cursor": next}), true
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
	case "saved.list": // the reminder rows' saved items: to-dos for the to-do filters, nothing else saved
		v.mu.Lock()
		st := v.act()
		items := []map[string]any{}
		if strings.HasPrefix(f.Get("filter"), "todo") {
			keys := make([]string, 0, len(st.saved))
			for k := range st.saved {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if it := st.saved[k]; it["todo_state"] != "" && it["todo_state"] != nil {
					items = append(items, it)
				}
			}
		}
		v.mu.Unlock()
		return ok("saved_items", items, "counts", map[string]any{"todo_count": len(items)}), true
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
	case "conversations.sharedInviteAcceptanceEligibility":
		// the channel invite's Accept (Me1V): eligible unless a test set
		// another answer (SetChannelInviteEligibility)
		v.mu.Lock()
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		st := v.act()
		el := st.scElig
		v.mu.Unlock()
		if f.Get("sig") != ActivityInviteChannelSig {
			return ok("is_eligible", false, "reason", "unknown"), true
		}
		if el == nil {
			return ok("is_eligible", true, "team_id", TeamID, "requirements", map[string]any{}), true
		}
		out := ok()
		for k, x := range el {
			out[k] = x
		}
		return out, true
	case "conversations.acceptSharedInvite", "conversations.joinConnectedShared":
		v.mu.Lock()
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		st := v.act()
		if f.Get("invite_id") == ActivityInviteChannel {
			st.scAccepted = f.Get("channel_name")
			if st.scAccepted == "" {
				st.scAccepted = "synthetic-partners"
			}
			st.scPrivate = f.Get("is_private") == "true"
			for _, e := range activityEntries("") {
				if inv, _ := e["item"].(map[string]any)["invite"].(map[string]any); inv != nil && inv["id"] == ActivityInviteChannel {
					st.ignored[e["key"].(string)] = true
				}
			}
		}
		v.mu.Unlock()
		if method == "conversations.joinConnectedShared" {
			return ok("channel_id", ActivityInviteChannelID, "team_id", TeamID), true
		}
		return ok("can_open_scdm", false), true
	case "connectInvites.list":
		// g7VB after an accept: the invite with its acceptance (pending
		// until a test approves it, SetChannelInviteApproved)
		v.mu.Lock()
		v.writes = append(v.writes, ActivityWrite{Method: method, Form: f})
		st := v.act()
		name, private, approved := st.scAccepted, st.scPrivate, st.scApproved
		v.mu.Unlock()
		var ids []string
		_ = json.Unmarshal([]byte(f.Get("invite_ids")), &ids)
		if len(ids) == 0 && f.Get("direction") == "incoming" {
			// the inbox's fetch (slack-connect-fetch-invites-for-activity-feed):
			// every incoming invite, channel and note in invite_payload
			var out []any
			for _, e := range activityEntries(s.URL) {
				inv, _ := e["item"].(map[string]any)["invite"].(map[string]any)
				if inv == nil {
					continue
				}
				cp := map[string]any{}
				for k, x := range inv {
					cp[k] = x
				}
				pl := map[string]any{}
				if ch, ok := cp["channel"]; ok {
					pl["channel"] = ch
					delete(cp, "channel")
				}
				if m, ok := cp["message"]; ok {
					pl["message"] = m
					delete(cp, "message")
				}
				cp["invite_payload"] = pl
				out = append(out, cp)
			}
			return ok("connect_invites", out), true
		}
		if len(ids) != 1 || ids[0] != ActivityInviteChannel || name == "" {
			return ok("connect_invites", []any{}), true
		}
		status, reviews := "pending_approval", []any{}
		if approved {
			status = "approved"
			reviews = []any{map[string]any{"reviewing_team": map[string]any{"id": TeamID}}, map[string]any{"reviewing_team": map[string]any{"id": ExternalTeamID}}}
		}
		return ok("connect_invites", []any{map[string]any{"id": ActivityInviteChannel, "invite_type": "channel", "direction": "incoming",
			"invite_payload": map[string]any{"channel": map[string]any{"id": ActivityInviteChannelID, "name": name, "is_private": private}},
			"inviting_team":  map[string]any{"id": ExternalTeamID, "name": "Partner Co"},
			"inviting_user":  map[string]any{"id": "U0PARTNER1"},
			"acceptances": []any{map[string]any{"approval_status": status, "accepting_team": map[string]any{"id": TeamID},
				"accepting_user": map[string]any{"id": SelfID}, "reviews": reviews}}}}), true
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
				if !st.read[k] {
					s.readActivityBadge(f.Get("type")) // activity_badges.go
				}
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
	if st.order != nil {
		at := map[string]int{}
		for i, id := range st.order {
			at[id] = i
		}
		for _, vw := range views {
			if i, ok := at[vw["id"].(string)]; ok {
				vw["position"] = strconv.Itoa(i)
			}
		}
		slices.SortStableFunc(views, func(a, b map[string]any) int { return at[a["id"].(string)] - at[b["id"].(string)] })
	}
	return views, map[string]any{"sort": sort, "density": d}
}

// allViewIDs is every view's id in position order (hidden default views
// keep their place).
func (st *activityState) allViewIDs() []string {
	if st.order != nil {
		return st.order
	}
	var ids []string
	for i := range activityDefaultViews {
		ids = append(ids, "Vw0FAKE"+strconv.Itoa(i))
	}
	for _, c := range st.custom {
		ids = append(ids, c["id"].(string))
	}
	return ids
}

// moveView is activity.views.update's next_view_id: id goes before next,
// or to the end when next is "".
func (st *activityState) moveView(id, next string) {
	var ids []string
	for _, x := range st.allViewIDs() {
		if x != id {
			ids = append(ids, x)
		}
	}
	at := len(ids)
	for i, x := range ids {
		if x == next {
			at = i
		}
	}
	ids = append(ids[:at], append([]string{id}, ids[at:]...)...)
	st.order = ids
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
		if st.order != nil {
			st.order = append(st.order, vw["id"].(string))
			vw["position"] = strconv.Itoa(len(st.order) - 1)
		}
		return vw, ""
	case "activity.views.update":
		id := f.Get("view_id")
		if f.Has("next_view_id") {
			st.moveView(id, f.Get("next_view_id"))
		}
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
		if st.order != nil {
			st.order = slices.DeleteFunc(st.order, func(x string) bool { return x == f.Get("view_id") })
		}
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
