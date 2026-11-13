package demo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// api is fakeslack's SetHook: the Web API methods the demo answers from
// its own state. Anything else (prefs, drafts, canvases, lists, files,
// dnd, huddle menus…) falls through to the fake with ok=false.
func (w *World) api(method string, f url.Values) (any, bool) {
	switch method {
	case "auth.test":
		return okMap("url", w.ws.Base, "team", TeamName, "user", w.selfHandle(), "team_id", TeamID, "user_id", SelfID,
			"enterprise_id", fakeslack.EnterpriseID, "is_enterprise_install", false), true
	case "team.info":
		switch t := f.Get("team"); t {
		case "", TeamID, fakeslack.EnterpriseID: // the enterprise is Quillfeather's own org
			return okMap("team", w.teamInfo()), true
		case PartnerTeamID:
			return okMap("team", w.partnerTeamInfo()), true
		default:
			return failMap("team_not_found"), true
		}
	case "users.conversations":
		return w.usersConversations(f), true
	case "conversations.info":
		return w.convInfo(f)
	case "conversations.history":
		return w.history(f), true
	case "conversations.replies":
		return w.repliesPage(f), true
	case "conversations.mark":
		return w.mark(f.Get("channel"), f.Get("ts")), true
	case "conversations.members":
		return w.members(f.Get("channel")), true
	case "conversations.open":
		return w.open(f)
	case "conversations.close":
		return w.closeDM(f.Get("channel")), true
	case "conversations.create":
		return w.createChannel(f), true
	case "conversations.join":
		w.mu.Lock()
		c, ok := w.convLocked(f.Get("channel"))
		var cc slack.Conversation
		if ok {
			cc = *c
		}
		w.mu.Unlock()
		if ok {
			return okMap("channel", cc, "already_in_channel", true), true
		}
		return nil, false
	case "conversations.leave":
		return w.leave(f.Get("channel")), true
	case "conversations.setTopic", "conversations.setPurpose":
		return w.setTopic(method, f), true
	case "client.counts":
		return w.counts(), true
	case "sidebar.dms":
		return w.sidebarDMs(), true
	case "client.dms":
		return w.clientDMs(f), true
	case "users.channelSections.list":
		return w.sectionsList(), true
	case "users.channelSections.create", "users.channelSections.set", "users.channelSections.delete",
		"users.channelSections.channels.bulkUpdate":
		out, evs := w.sectionCall(method, f)
		w.pushAll(evs)
		return out, true
	case "stars.list":
		return w.starsList(), true
	case "stars.add", "stars.remove":
		if f.Get("channel") == "" || f.Get("timestamp") != "" {
			return nil, false
		}
		return w.star(f.Get("channel"), method == "stars.add"), true
	case "emoji.list":
		return okMap("emoji", w.ws.Emoji, "cache_ts", strconv.FormatInt(w.t0.Unix(), 10)+".000000"), true
	case "users.info":
		w.mu.Lock()
		u, ok := w.userLocked(f.Get("user"))
		w.mu.Unlock()
		if !ok {
			return failMap("user_not_found"), true
		}
		return okMap("user", u), true
	case "users.profile.get":
		id := orStr(f.Get("user"), SelfID)
		w.mu.Lock()
		u, ok := w.userLocked(id)
		w.mu.Unlock()
		if !ok {
			return failMap("user_not_found"), true
		}
		return okMap("profile", u.Profile), true
	case "users.profile.getSections":
		return w.profileSections(f), true
	case "users.getPresence":
		id := orStr(f.Get("user"), SelfID)
		w.mu.Lock()
		u, _ := w.userLocked(id)
		w.mu.Unlock()
		return okMap("presence", orStr(u.Presence, "away"), "online", u.Presence == "active"), true
	case "users.setPresence":
		w.setPresence(SelfID, map[string]string{"auto": "active", "away": "away"}[f.Get("presence")])
		return okMap(), true
	case "users.profile.set":
		return w.setSelfProfile(f), true
	case "bots.info":
		return w.botsInfo(f), true
	case "apps.profile.get":
		if p, ok := w.appProfile(f.Get("bot"), f.Get("app")); ok {
			return okMap("app_profile", p), true
		}
		return nil, false
	case "chat.postMessage":
		return w.postMessage(f)
	case "chat.meMessage":
		ch := f.Get("channel")
		ts, m := w.post(ch, slack.Message{User: SelfID, Subtype: "me_message", Text: f.Get("text")}, true)
		return okMap("channel", ch, "ts", ts, "message", m), true
	case "chat.update":
		return w.update(f), true
	case "chat.delete":
		ch, ts := f.Get("channel"), f.Get("ts")
		w.mu.Lock()
		m, ok := w.findLocked(ch, ts)
		w.mu.Unlock()
		if !ok {
			return failMap("message_not_found"), true
		}
		if m.User != SelfID {
			return failMap("cant_delete_message"), true
		}
		w.remove(ch, ts)
		return okMap("channel", ch, "ts", ts), true
	case "reactions.add", "reactions.remove":
		ch, ts, name := f.Get("channel"), f.Get("timestamp"), f.Get("name")
		w.mu.Lock()
		_, found := w.findLocked(ch, ts)
		w.mu.Unlock()
		if !found {
			return failMap("message_not_found"), true
		}
		add := method == "reactions.add"
		if add && !w.validEmoji(name) {
			return failMap("invalid_name"), true
		}
		if !w.react(SelfID, ch, ts, name, add) {
			if add {
				return failMap("already_reacted"), true
			}
			return failMap("no_reaction"), true
		}
		return okMap(), true
	case "reactions.get":
		w.mu.Lock()
		m, ok := w.findLocked(f.Get("channel"), f.Get("timestamp"))
		w.mu.Unlock()
		if !ok {
			return failMap("message_not_found"), true
		}
		return okMap("type", "message", "channel", f.Get("channel"), "message", m), true
	case "pins.list":
		return w.pinsList(f.Get("channel")), true
	case "pins.add", "pins.remove":
		if e := w.setPin(SelfID, f.Get("channel"), f.Get("timestamp"), method == "pins.add"); e != "" {
			return failMap(e), true
		}
		return okMap(), true
	case "bookmarks.list":
		return okMap("bookmarks", w.bookmarksFor(orStr(f.Get("channel_id"), f.Get("channel")))), true
	case "messages.list":
		return w.messagesList(f.Get("message_ids")), true
	case "activity.feed":
		return w.activityFeed(f), true
	case "activity.markRead", "activity.markUnread", "activity.archive", "activity.unarchive", "activity.markAllRead":
		return w.activityWrite(method, f), true
	case "subscriptions.thread.getView":
		return w.threadsView(f), true
	case "subscriptions.thread.mark":
		return w.threadMark(f), true
	case "saved.list":
		return w.savedList(f), true
	case "saved.get":
		return w.savedGet(f), true
	case "saved.add", "saved.delete", "saved.update":
		return w.savedWrite(method, f), true
	case "later.list":
		return okMap("items", []any{}, "saved_items", []any{}, "response_metadata", map[string]any{"next_cursor": ""}), true
	case "messages.sent.list":
		return w.sentList(f), true
	case "search.messages":
		return w.searchLegacy(f), true
	case "search.modules.messages":
		return w.searchMessages(f, false), true
	case "search.modules.dms":
		return w.searchMessages(f, true), true
	case "search.inline":
		return w.searchInline(f), true
	case "search.modules.channels":
		return w.searchChannels(f), true
	case "search.modules.people":
		return w.searchPeople(f), true
	case "blocks.actions":
		return w.blockAction(f)
	case "client.codeChannels.getDiff", "client.codeChannels.getPullRequest", "client.codeChannels.getBlockKitView",
		"client.codeChannels.getView", "client.codeChannels.action", "agents.sessions.stop":
		return w.codeCall(method, f)
	case "rooms.join":
		return w.roomsJoin(f), true
	case "rooms.inviteResponse":
		w.bots.onInviteResponse(f)
		return nil, false // fakeslack records and answers it
	case "search.modules.files":
		return w.searchFiles(f)
	case "files.info":
		return w.fileInfo(f)
	case "conversations.searchLinks":
		return w.searchLinks(f), true
	}
	return w.more(method, f)
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

func (w *World) selfHandle() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if u, ok := w.userLocked(SelfID); ok {
		return u.Name
	}
	return "jordan"
}

func (w *World) teamInfo() map[string]any {
	icon := map[string]any{"image_default": false}
	for _, px := range []int{34, 44, 68, 88, 102, 132, 230} {
		icon["image_"+strconv.Itoa(px)] = w.ws.Base + "img/demo/team/" + strconv.Itoa(px) + ".png"
	}
	icon["image_original"] = w.ws.Base + "img/demo/team/230.png"
	return map[string]any{"id": TeamID, "name": TeamName, "domain": TeamDomain, "email_domain": "quillfeather.example",
		"url": "https://" + TeamDomain + ".slack.com/", "icon": icon, "enterprise_id": fakeslack.EnterpriseID,
		"enterprise_name": TeamName, "avatar_base_url": w.ws.Base + "img/"}
}

// partnerTeamInfo is Papertrail Freight, the Slack Connect org.
func (w *World) partnerTeamInfo() map[string]any {
	icon := map[string]any{"image_default": false}
	for _, px := range []int{34, 44, 68, 88, 102, 132, 230} {
		icon["image_"+strconv.Itoa(px)] = w.ws.Base + "img/" + imgPartner + strconv.Itoa(px) + ".png"
	}
	icon["image_original"] = w.ws.Base + "img/" + imgPartner + "230.png"
	return map[string]any{"id": PartnerTeamID, "name": PartnerTeamName, "domain": PartnerTeamDomain,
		"email_domain": "papertrail-freight.example", "url": "https://" + PartnerTeamDomain + ".slack.com/", "icon": icon,
		"is_verified": true, "date_created": w.now().AddDate(-6, 0, 0).Unix(), "avatar_base_url": w.ws.Base + "img/"}
}

// ---- conversations ----

func (w *World) usersConversations(f url.Values) any {
	types := map[string]bool{}
	for _, t := range strings.Split(f.Get("types"), ",") {
		if t != "" {
			types[t] = true
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := []slack.Conversation{}
	for _, c := range w.convs {
		if c.IsArchived && f.Get("exclude_archived") == "true" {
			continue
		}
		if !c.IsMember && !c.IsIM && !c.IsMPIM {
			continue
		}
		kind := "public_channel"
		switch {
		case c.IsIM:
			kind = "im"
		case c.IsMPIM:
			kind = "mpim"
		case c.IsPrivate || c.IsGroup:
			kind = "private_channel"
		}
		if len(types) > 0 && !types[kind] {
			continue
		}
		out = append(out, w.convOutLocked(c))
	}
	return okMap("channels", out, "response_metadata", map[string]string{"next_cursor": ""})
}

// convOutLocked is a conversation as the API serves it now: its live
// read cursor, member count and channel tabs.
func (w *World) convOutLocked(c slack.Conversation) slack.Conversation {
	c.LastRead = w.lastRead[c.ID]
	if c.NumMembers == 0 && !c.IsIM {
		c.NumMembers = len(w.membersLocked(&c))
	}
	c.Properties = w.withTabsLocked(c)
	return c
}

func (w *World) memberCountLocked(c slack.Conversation) int {
	if c.NumMembers > 0 {
		return c.NumMembers
	}
	return len(w.membersLocked(&c))
}

// membersLocked are a conversation's members: its Members, the DM's
// pair, or everyone (the code channel adds Inkwell).
func (w *World) membersLocked(c *slack.Conversation) []string {
	if len(c.Members) > 0 {
		// the apps are in their channels whatever the content lists
		app := map[string]string{CodeID: InkwellID, LunchID: PulseID}[c.ID]
		if app != "" && !hasStr(c.Members, app) {
			return append(append([]string(nil), c.Members...), app)
		}
		return c.Members
	}
	if c.IsIM {
		return []string{SelfID, c.User}
	}
	var out []string
	for _, u := range w.ws.Users {
		if !u.IsBot || (c.ID == CodeID && u.ID == InkwellID) || (c.ID == LunchID && u.ID == PulseID) {
			out = append(out, u.ID)
		}
	}
	return out
}

// withTabsLocked adds the channel tab bar Slack serves in properties.tabs
// (canvases and lists shared to the channel, the bookmarks folder, pins,
// files) when the Workspace didn't write one.
func (w *World) withTabsLocked(c slack.Conversation) json.RawMessage {
	if c.IsIM || c.IsMPIM {
		return c.Properties
	}
	props := map[string]any{}
	if len(c.Properties) > 0 && json.Unmarshal(c.Properties, &props) != nil {
		return c.Properties
	}
	if _, has := props["tabs"]; has {
		return c.Properties
	}
	if _, code := props["code_channel"]; code {
		return c.Properties // a code channel's tabs are its tabz (views)
	}
	var tabs []any
	for _, cv := range w.ws.Canvases {
		if cv.Channel == c.ID {
			tabs = append(tabs, map[string]any{"id": "Ct" + cv.ID, "type": "canvas", "label": "",
				"data": map[string]any{"file_id": cv.ID, "shared_ts": w.startTS}})
			if props["canvas"] == nil {
				props["canvas"] = map[string]any{"file_id": cv.ID, "is_empty": false}
			}
		}
	}
	for _, l := range w.ws.Lists {
		if l.Channel == c.ID {
			tabs = append(tabs, map[string]any{"id": "Ct" + l.ID, "type": "list", "label": "", "data": map[string]any{"file_id": l.ID}})
		}
	}
	if len(w.bookmarksFor(c.ID)) > 0 {
		tabs = append(tabs, map[string]any{"id": "Ft0DEMO" + c.ID[len(c.ID)-4:], "type": "folder", "label": ""})
	}
	if len(tabs) == 0 && len(w.pins[c.ID]) == 0 {
		return c.Properties
	}
	tabs = append(tabs, map[string]any{"id": "bookmarks", "type": "bookmarks", "label": ""},
		map[string]any{"id": "files", "type": "files", "label": ""})
	props["tabs"] = tabs
	b, _ := json.Marshal(props)
	return b
}

func (w *World) convInfo(f url.Values) (any, bool) {
	id := f.Get("channel")
	w.mu.Lock()
	c, ok := w.convLocked(id)
	if !ok {
		w.mu.Unlock()
		return nil, false
	}
	cc := w.convOutLocked(*c)
	if cc.IsIM {
		cc.IsOpen = c.IsOpen
	}
	tz := w.tzCountLocked(&cc)
	w.mu.Unlock()
	if f.Get("timezone_count") != "true" && f.Get("display_counts") != "true" {
		return okMap("channel", cc), true
	}
	b, _ := json.Marshal(cc)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if f.Get("timezone_count") == "true" {
		m["timezone_count"] = tz
	}
	if f.Get("display_counts") == "true" {
		m["display_counts"] = map[string]int{"display_counts": cc.NumMembers, "guest_counts": 0}
	}
	return okMap("channel", m), true
}

func (w *World) tzCountLocked(c *slack.Conversation) int {
	tzs := map[string]bool{}
	for _, id := range w.membersLocked(c) {
		if u, ok := w.users[id]; ok && !u.IsBot {
			tzs[orStr(u.TZ, "America/New_York")] = true
		}
	}
	return max(1, len(tzs))
}

// history answers conversations.history: newest first between oldest and
// latest (exclusive unless inclusive), limit at a time, with a cursor.
func (w *World) history(f url.Values) any {
	ch := f.Get("channel")
	w.mu.Lock()
	all := w.hist[ch]
	w.mu.Unlock()
	return pageMsgs(all, f, true, len(w.pinsOf(ch)))
}

func (w *World) pinsOf(ch string) []livePin {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]livePin(nil), w.pins[ch]...)
}

// pageMsgs pages msgs (newest first when newestFirst, else oldest first)
// as conversations.history / replies do.
func pageMsgs(msgs []slack.Message, f url.Values, newestFirst bool, pinCount int) any {
	latest, oldest := f.Get("latest"), f.Get("oldest")
	incl := f.Get("inclusive") == "true" || f.Get("inclusive") == "1"
	var in []slack.Message
	for _, m := range msgs {
		if latest != "" && (tsBefore(latest, m.TS) || (!incl && m.TS == latest)) {
			continue
		}
		if oldest != "" && (tsBefore(m.TS, oldest) || (!incl && m.TS == oldest)) {
			continue
		}
		in = append(in, copyMsg(m))
	}
	limit, _ := strconv.Atoi(f.Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	off := 0
	if c := f.Get("cursor"); strings.HasPrefix(c, "demo:") {
		off, _ = strconv.Atoi(strings.TrimPrefix(c, "demo:"))
	} else if !newestFirst && oldest == "" && latest != "" {
		// replies with only latest: the newest page before it
		off = max(0, len(in)-limit)
	}
	off = min(off, len(in))
	end := min(len(in), off+limit)
	page := in[off:end]
	if page == nil {
		page = []slack.Message{}
	}
	next := ""
	if end < len(in) {
		next = "demo:" + strconv.Itoa(end)
	}
	out := okMap("messages", page, "has_more", next != "", "response_metadata", map[string]any{"next_cursor": next})
	if newestFirst {
		out["pin_count"] = pinCount
	}
	return out
}

// repliesPage answers conversations.replies: the parent, then the
// replies oldest first.
func (w *World) repliesPage(f url.Values) any {
	ch, ts := f.Get("channel"), f.Get("ts")
	w.mu.Lock()
	p := w.msgPtrLocked(ch, ts)
	if p != nil && p.ThreadTS != "" && p.ThreadTS != p.TS {
		p = w.msgPtrLocked(ch, p.ThreadTS)
	}
	if p == nil {
		w.mu.Unlock()
		return failMap("thread_not_found")
	}
	parent := copyMsg(*p)
	rs := append([]slack.Message(nil), w.replies[ch][parent.TS]...)
	w.mu.Unlock()
	out := pageMsgs(rs, f, false, 0).(map[string]any)
	page := out["messages"].([]slack.Message)
	out["messages"] = append([]slack.Message{parent}, page...)
	return out
}

// mark is conversations.mark; Slack echoes it as *_marked to every client.
func (w *World) mark(ch, ts string) any {
	w.mu.Lock()
	c, ok := w.convLocked(ch)
	if !ok {
		w.mu.Unlock()
		return failMap("channel_not_found")
	}
	typ := "channel_marked"
	switch {
	case c.IsIM:
		typ = "im_marked"
	case c.IsMPIM:
		typ = "mpim_marked"
	case c.IsPrivate:
		typ = "group_marked"
	}
	w.lastRead[ch] = ts
	unread, ment := w.unreadLocked(c)
	w.mu.Unlock()
	w.fs.Push(map[string]any{"type": typ, "channel": ch, "ts": ts, "unread_count": unread, "unread_count_display": unread,
		"mention_count": ment, "mention_count_display": ment, "event_ts": ts})
	return okMap()
}

// unreadLocked counts a conversation's unread messages and mentions:
// messages from others after the read cursor. In a DM or group DM each
// one counts as a mention (Slack's badge); in a channel the live ones
// that mention the viewer do, plus the seed's mentions while the cursor
// hasn't moved.
func (w *World) unreadLocked(c *slack.Conversation) (unread, mentions int) {
	lr := w.lastRead[c.ID]
	for _, m := range w.hist[c.ID] {
		if !tsBefore(lr, m.TS) {
			break
		}
		if m.User == SelfID || m.Hidden {
			continue
		}
		unread++
		if !c.IsIM && !c.IsMPIM && tsBefore(w.startTS, m.TS) && textMentionsSelf(m.Text) {
			mentions++
		}
	}
	switch {
	case c.IsIM || c.IsMPIM:
		mentions = unread
	case lr == w.seedRead[c.ID]:
		mentions += w.seedMent[c.ID]
	}
	return unread, mentions
}

func (w *World) counts() any {
	w.mu.Lock()
	defer w.mu.Unlock()
	var chans, ims, mpims []slack.CountEntry
	for i := range w.convs {
		c := &w.convs[i]
		if !c.IsMember && !c.IsIM && !c.IsMPIM {
			continue
		}
		unread, ment := w.unreadLocked(c)
		latest := orStr(w.latestLocked(c.ID), w.lastRead[c.ID])
		e := slack.CountEntry{ID: c.ID, LastRead: w.lastRead[c.ID], Latest: latest, Updated: latest,
			HasUnreads: unread > 0, MentionCount: ment}
		switch {
		case c.IsIM:
			ims = append(ims, e)
		case c.IsMPIM:
			mpims = append(mpims, e)
		default:
			chans = append(chans, e)
		}
	}
	tu, tm := w.threadUnreadsLocked()
	return okMap("channels", nz(chans), "ims", nz(ims), "mpims", nz(mpims),
		"threads", map[string]any{"has_unreads": tu > 0, "mention_count": tm, "unread_count": tu},
		"channel_badges", map[string]any{"channels": 0, "dms": 0, "app_dms": 0, "thread_mentions": tm, "thread_unreads": tu})
}

func nz(es []slack.CountEntry) []slack.CountEntry {
	if es == nil {
		return []slack.CountEntry{}
	}
	return es
}

// threadUnreadsLocked counts the viewer's threads with unread replies
// from others, and the unread replies that mention them.
func (w *World) threadUnreadsLocked() (threads, mentions int) {
	for ch, th := range w.replies {
		for parent, rs := range th {
			p := w.msgPtrLocked(ch, parent)
			if p == nil || !w.inThreadLocked(ch, p) {
				continue
			}
			rd := orStr(w.threadRd[ch+"/"+parent], w.seedRead[ch])
			unread := false
			for _, r := range rs {
				if r.User != SelfID && tsBefore(rd, r.TS) {
					unread = true
					if textMentionsSelf(r.Text) {
						mentions++
					}
				}
			}
			if unread {
				threads++
			}
		}
	}
	return
}

func (w *World) threadMark(f url.Values) any {
	ch, th, ts := f.Get("channel"), f.Get("thread_ts"), f.Get("ts")
	w.mu.Lock()
	if ts == "" {
		if rs := w.replies[ch][th]; len(rs) > 0 {
			ts = rs[len(rs)-1].TS
		}
	}
	w.threadRd[ch+"/"+th] = ts
	w.mu.Unlock()
	w.fs.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ch,
		"thread_ts": th, "last_read": ts, "active": true}, "event_ts": ts})
	return okMap()
}

// threadsView answers subscriptions.thread.getView: the threads the
// viewer is in, latest reply first.
func (w *World) threadsView(f url.Values) any {
	limit, _ := strconv.Atoi(f.Get("limit"))
	if limit <= 0 {
		limit = 10
	}
	maxTS := f.Get("max_ts")
	type th struct {
		latest string
		v      map[string]any
		unread int
	}
	w.mu.Lock()
	var all []th
	for ch, ths := range w.replies {
		for parent, rs := range ths {
			p := w.msgPtrLocked(ch, parent)
			if p == nil || len(rs) == 0 || !w.inThreadLocked(ch, p) {
				continue
			}
			latest := rs[len(rs)-1].TS
			if maxTS != "" && !tsBefore(latest, maxTS) {
				continue
			}
			root := copyMsg(*p)
			root.Channel = ch
			rd := orStr(w.threadRd[ch+"/"+parent], w.seedRead[ch])
			var last, unread []slack.Message
			for i := max(0, len(rs)-3); i < len(rs); i++ {
				r := copyMsg(rs[i])
				r.Channel = ch
				last = append(last, r)
			}
			for _, r := range rs {
				if r.User != SelfID && tsBefore(rd, r.TS) {
					r := copyMsg(r)
					r.Channel = ch
					unread = append(unread, r)
				}
			}
			if unread == nil {
				unread = []slack.Message{}
			}
			all = append(all, th{latest, map[string]any{"root_msg": root, "latest_replies": last, "unread_replies": unread}, len(unread)})
		}
	}
	tu, tm := w.threadUnreadsLocked()
	w.mu.Unlock()
	sort.Slice(all, func(i, j int) bool { return tsBefore(all[j].latest, all[i].latest) })
	total := 0
	for _, t := range all {
		total += t.unread
	}
	more := len(all) > limit
	if more {
		all = all[:limit]
	}
	threads := []any{}
	next := ""
	for _, t := range all {
		threads = append(threads, t.v)
		next = t.latest
	}
	return okMap("threads", threads, "has_more", more, "max_ts", next, "total_unread_replies", total,
		"threads_state", map[string]any{"has_unreads": tu > 0, "mention_count": tm})
}

func (w *World) sidebarDMs() any {
	w.mu.Lock()
	defer w.mu.Unlock()
	ims, mpdms := []map[string]any{}, []map[string]any{}
	for _, c := range w.convs {
		switch {
		case c.IsIM:
			ims = append(ims, map[string]any{"id": c.ID, "user": c.User, "is_open": c.IsOpen || len(w.hist[c.ID]) > 0,
				"is_im": true, "last_read": w.lastRead[c.ID]})
		case c.IsMPIM:
			mpdms = append(mpdms, map[string]any{"id": c.ID, "is_open": true, "is_mpim": true, "last_read": w.lastRead[c.ID]})
		}
	}
	return okMap("ims", ims, "mpdms", mpdms, "mpims", mpdms, "response_metadata", map[string]any{"next_cursor": ""})
}

// clientDMs answers client.dms (the DMs tab): each DM with its latest
// message, newest first.
func (w *World) clientDMs(f url.Values) any {
	w.mu.Lock()
	defer w.mu.Unlock()
	type row struct {
		ts string
		v  map[string]any
	}
	var ims, mpims []row
	for _, c := range w.convs {
		if !c.IsIM && !c.IsMPIM {
			continue
		}
		h := w.hist[c.ID]
		if len(h) == 0 {
			continue
		}
		if f.Get("exclude_bots") == "true" && c.IsIM {
			if u, ok := w.users[c.User]; ok && u.IsBot {
				continue
			}
		}
		m := copyMsg(h[0])
		m.Channel = c.ID
		r := row{m.TS, map[string]any{"id": c.ID, "message": m, "channel": w.convOutLocked(c), "latest": m.TS}}
		if c.IsIM {
			ims = append(ims, r)
		} else {
			mpims = append(mpims, r)
		}
	}
	out := func(rs []row) []any {
		sort.Slice(rs, func(i, j int) bool { return tsBefore(rs[j].ts, rs[i].ts) })
		o := []any{}
		for _, r := range rs {
			o = append(o, r.v)
		}
		return o
	}
	return okMap("ims", out(ims), "mpims", out(mpims), "response_metadata", map[string]any{"next_cursor": ""})
}

func (w *World) members(ch string) any {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, ok := w.convLocked(ch)
	if !ok {
		return failMap("channel_not_found")
	}
	return okMap("members", w.membersLocked(c), "response_metadata", map[string]any{"next_cursor": ""})
}

// open is conversations.open: the DM with one user (opened if new), or
// the group DM with exactly those users.
func (w *World) open(f url.Values) (any, bool) {
	if ch := f.Get("channel"); ch != "" {
		w.mu.Lock()
		c, ok := w.convLocked(ch)
		var cc slack.Conversation
		if ok {
			c.IsOpen = true
			cc = w.convOutLocked(*c)
		}
		w.mu.Unlock()
		if !ok {
			return nil, false
		}
		return okMap("channel", cc, "already_open", true), true
	}
	var users []string
	for _, u := range strings.Split(f.Get("users"), ",") {
		if u = strings.TrimSpace(u); u != "" && u != SelfID {
			users = append(users, u)
		}
	}
	if len(users) == 0 {
		users = []string{SelfID} // your own DM
	}
	if len(users) == 1 {
		w.mu.Lock()
		if _, ok := w.users[users[0]]; !ok {
			w.mu.Unlock()
			return failMap("user_not_found"), true
		}
		id := w.dmWithLocked(users[0])
		already := id != ""
		if !already {
			id = w.newDMLocked(users[0]).ID
		}
		c, _ := w.convLocked(id)
		c.IsOpen = true
		cc := w.convOutLocked(*c)
		w.mu.Unlock()
		if !already {
			w.fs.Push(map[string]any{"type": "im_created", "user": SelfID, "channel": cc})
		}
		return okMap("channel", cc, "already_open", already, "no_op", already), true
	}
	want := append([]string{SelfID}, users...)
	sort.Strings(want)
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range w.convs {
		if !c.IsMPIM {
			continue
		}
		have := append([]string(nil), c.Members...)
		sort.Strings(have)
		if strings.Join(have, ",") == strings.Join(want, ",") {
			return okMap("channel", w.convOutLocked(c), "already_open", true), true
		}
	}
	var names []string
	for _, id := range want {
		if u, ok := w.users[id]; ok {
			names = append(names, u.Name)
		}
	}
	c := slack.Conversation{ID: fmt.Sprintf("G0DEMONEW%d", len(w.convs)), Name: "mpdm-" + strings.Join(names, "--") + "-1",
		IsMPIM: true, IsGroup: true, IsPrivate: true, IsMember: true, IsOpen: true, Members: want, NumMembers: len(want),
		Created: w.now().Unix(), Creator: SelfID, ContextTeamID: TeamID}
	w.convs = append(w.convs, c)
	w.lastRead[c.ID], w.seedRead[c.ID] = w.nextTSLocked(), ""
	return okMap("channel", c), true
}

func (w *World) closeDM(ch string) any {
	w.mu.Lock()
	c, ok := w.convLocked(ch)
	if ok {
		c.IsOpen = false
	}
	w.mu.Unlock()
	if !ok {
		return failMap("channel_not_found")
	}
	typ := "im_close"
	if !strings.HasPrefix(ch, "D") {
		typ = "mpim_close"
	}
	w.fs.Push(map[string]any{"type": typ, "channel": ch, "user": SelfID})
	return okMap()
}

func (w *World) leave(ch string) any {
	w.mu.Lock()
	c, ok := w.convLocked(ch)
	if !ok {
		w.mu.Unlock()
		return okMap("not_in_channel", true)
	}
	if c.IsGeneral {
		w.mu.Unlock()
		return failMap("cant_leave_general")
	}
	c.IsMember = false
	w.mu.Unlock()
	w.fs.Push(map[string]any{"type": "channel_left", "channel": ch})
	return okMap()
}

func (w *World) createChannel(f url.Values) any {
	name := f.Get("name")
	if name == "" {
		return failMap("invalid_name_required")
	}
	if len(name) > 80 {
		return failMap("invalid_name_maxlength")
	}
	for _, r := range name {
		if !(unicode.IsLower(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			return failMap("invalid_name_specials")
		}
	}
	w.mu.Lock()
	for _, c := range w.convs {
		if c.Name == name {
			w.mu.Unlock()
			return failMap("name_taken")
		}
	}
	c := slack.Conversation{ID: fmt.Sprintf("C0DEMONEW%d", len(w.convs)), Name: name, IsChannel: true,
		IsPrivate: f.Get("is_private") == "true", IsMember: true, Created: w.now().Unix(), Creator: SelfID,
		NumMembers: 1, Members: []string{SelfID}, ContextTeamID: TeamID,
		Topic: &slack.Purpose{}, Purpose: &slack.Purpose{}}
	if c.IsPrivate {
		c.IsChannel, c.IsGroup = false, true
	}
	w.convs = append(w.convs, c)
	ts := w.nextTSLocked()
	w.lastRead[c.ID], w.seedRead[c.ID] = ts, ts
	w.mu.Unlock()
	typ := "channel_created"
	if c.IsPrivate {
		typ = "group_joined"
	}
	w.fs.Push(map[string]any{"type": typ, "channel": c})
	return okMap("channel", c)
}

func (w *World) setTopic(method string, f url.Values) any {
	ch := f.Get("channel")
	val := orStr(f.Get("topic"), f.Get("purpose"))
	sub, what := "channel_topic", "topic"
	w.mu.Lock()
	c, ok := w.convLocked(ch)
	if !ok {
		w.mu.Unlock()
		return failMap("channel_not_found")
	}
	p := &slack.Purpose{Value: val, Creator: SelfID, LastSet: w.now().Unix()}
	if method == "conversations.setPurpose" {
		c.Purpose = p
		sub, what = "channel_purpose", "description"
	} else {
		c.Topic = p
	}
	cc := *c
	w.mu.Unlock()
	w.post(ch, slack.Message{User: SelfID, Subtype: sub, Text: "set the channel " + what + ": " + val}, false)
	w.fs.Push(map[string]any{"type": "channel_updated", "channel": ch, "updates": map[string]any{what: p}})
	return okMap("channel", cc)
}

// ---- stars and saved ----

func (w *World) starsSectionLocked() *liveSection {
	for _, s := range w.sections {
		if s.Type == "stars" {
			return s
		}
	}
	return nil
}

func (w *World) starsList() any {
	w.mu.Lock()
	defer w.mu.Unlock()
	items := []any{}
	if s := w.starsSectionLocked(); s != nil {
		for _, id := range s.Channels {
			typ := "channel"
			if c, ok := w.convLocked(id); ok {
				switch {
				case c.IsIM:
					typ = "im"
				case c.IsPrivate || c.IsMPIM:
					typ = "group"
				}
			}
			items = append(items, map[string]any{"type": typ, "channel": id, "date_create": w.t0.Unix()})
		}
	}
	return okMap("items", items, "paging", map[string]any{"count": 100, "total": len(items), "page": 1, "pages": 1})
}

func (w *World) star(ch string, on bool) any {
	w.mu.Lock()
	s := w.starsSectionLocked()
	if s == nil {
		s = &liveSection{ID: "L0DEMOSTAR", Type: "stars"}
		w.sections = append([]*liveSection{s}, w.sections...)
	}
	var keep []string
	for _, id := range s.Channels {
		if id != ch {
			keep = append(keep, id)
		}
	}
	if on {
		keep = append([]string{ch}, keep...)
	}
	s.Channels = keep
	w.mu.Unlock()
	typ := "star_removed"
	if on {
		typ = "star_added"
	}
	w.fs.Push(map[string]any{"type": typ, "user": SelfID, "item": map[string]any{"type": "channel", "channel": ch}})
	w.fs.Push(map[string]any{"type": "channel_sections_channels_upserted", "channel_section_id": s.ID})
	return okMap()
}

// ---- users ----

func (w *World) setPresence(id, presence string) {
	if presence == "" {
		return
	}
	w.mu.Lock()
	u, ok := w.users[id]
	if ok {
		u.Presence = presence
	}
	w.mu.Unlock()
	if ok {
		w.fs.Push(map[string]any{"type": "presence_change", "user": id, "users": []string{id}, "presence": presence})
	}
}

func (w *World) setSelfProfile(f url.Values) any {
	var p map[string]any
	if s := f.Get("profile"); s != "" {
		_ = json.Unmarshal([]byte(s), &p)
	}
	w.mu.Lock()
	u := w.users[SelfID]
	if u == nil {
		w.mu.Unlock()
		return failMap("user_not_found")
	}
	str := func(k string) (string, bool) {
		v, ok := p[k].(string)
		return v, ok
	}
	if v, ok := str("status_text"); ok {
		u.Profile.StatusText = v
	}
	if v, ok := str("status_emoji"); ok {
		u.Profile.StatusEmoji = v
	}
	if v, ok := p["status_expiration"].(float64); ok {
		u.Profile.StatusExpiration = int64(v)
	}
	if v, ok := str("display_name"); ok {
		u.Profile.DisplayName = v
	}
	if v, ok := str("title"); ok {
		u.Profile.Title = v
	}
	if v, ok := str("pronouns"); ok {
		u.Profile.Pronouns = v
	}
	cp := *u
	w.mu.Unlock()
	w.fs.Push(map[string]any{"type": "user_change", "user": cp})
	return okMap("profile", cp.Profile)
}

func (w *World) botsInfo(f url.Values) any {
	ids := strings.Split(orStr(f.Get("bots"), f.Get("bot")), ",")
	var out []map[string]any
	for _, id := range ids {
		for _, b := range w.ws.Bots {
			if b.BotID == id {
				icons := map[string]any{}
				for k, v := range b.Icons {
					icons[k] = v
				}
				out = append(out, map[string]any{"id": b.BotID, "deleted": false, "name": b.Name, "updated": w.t0.Unix(),
					"app_id": b.AppID, "user_id": b.UserID, "icons": icons, "is_workflow_bot": false})
			}
		}
	}
	if len(out) == 0 {
		return failMap("bot_not_found")
	}
	return okMap("bots", out, "bot", out[0])
}

func (w *World) appProfile(bot, app string) (map[string]any, bool) {
	for _, b := range w.ws.Bots {
		if (bot != "" && b.BotID == bot) || (app != "" && b.AppID == app) {
			icons := map[string]any{}
			for k, v := range b.Icons {
				icons[k] = v
			}
			handle := strings.ToLower(b.Name)
			return map[string]any{"id": b.AppID, "name": b.Name, "desc": b.Desc, "app_card_color": b.Color,
				"app_user_id": b.UserID, "team_id": TeamID, "enterprise_id": fakeslack.EnterpriseID,
				"is_xoxa_app": false, "is_slack_integration": false, "is_workflow_app": false, "is_directory_published": false,
				"is_installed": true, "is_shown_as_agent": b.UserID == InkwellID, "icons": icons,
				"bot_user": map[string]any{"id": b.UserID, "username": handle},
				"auth":     map[string]any{"created_by": "U0DEMOTHEO", "date_created": strconv.FormatInt(w.t0.Add(-90*24*3600e9).Unix(), 10), "revoked": false},
				"config":   map[string]any{"real_name": b.Name, "username": handle, "descriptive_label": b.Desc},
				"commands": map[string]any{}}, true
		}
	}
	return nil, false
}

// ---- messages ----

func (w *World) postMessage(f url.Values) (any, bool) {
	ch := f.Get("channel")
	w.mu.Lock()
	_, known := w.convLocked(ch)
	w.mu.Unlock()
	if !known {
		return nil, false // a list record's comments and the like: the fake's
	}
	m := slack.Message{User: SelfID, Text: f.Get("text"), ThreadTS: f.Get("thread_ts"), ClientMsgID: f.Get("client_msg_id")}
	if b := f.Get("blocks"); b != "" && json.Valid([]byte(b)) {
		m.Blocks = json.RawMessage(b)
	}
	if m.ThreadTS != "" && (f.Get("reply_broadcast") == "true" || f.Get("reply_broadcast") == "1") {
		m.Subtype = "thread_broadcast"
	}
	if m.Text == "" && len(m.Blocks) == 0 {
		return failMap("no_text"), true
	}
	ts, stored := w.post(ch, m, true)
	return okMap("channel", ch, "ts", ts, "message", stored), true
}

func (w *World) update(f url.Values) any {
	ch, ts := f.Get("channel"), f.Get("ts")
	w.mu.Lock()
	old, ok := w.findLocked(ch, ts)
	w.mu.Unlock()
	if !ok {
		return failMap("message_not_found")
	}
	if old.User != SelfID {
		return failMap("cant_update_message")
	}
	text := f.Get("text")
	var blocks json.RawMessage
	if b := f.Get("blocks"); b != "" && json.Valid([]byte(b)) {
		blocks = json.RawMessage(b)
	}
	w.mu.Lock()
	edTS := w.nextTSLocked()
	w.mu.Unlock()
	m, _ := w.change(ch, ts, func(m *slack.Message) {
		m.Text = text
		m.Blocks = blocks
		m.Edited = &slack.Edited{User: SelfID, TS: edTS}
	})
	return okMap("channel", ch, "ts", ts, "text", text, "message", m)
}

func (w *World) pinsList(ch string) any {
	w.mu.Lock()
	defer w.mu.Unlock()
	items := []any{}
	ps := append([]livePin(nil), w.pins[ch]...)
	sort.Slice(ps, func(i, j int) bool { return ps[i].At > ps[j].At })
	for _, p := range ps {
		m, ok := w.findLocked(ch, p.TS)
		if !ok {
			continue
		}
		m.PinnedTo = []string{ch}
		m.PinnedInfo = &slack.PinnedInfo{Channel: ch, PinnedBy: p.By, PinnedTS: p.At}
		items = append(items, map[string]any{"type": "message", "channel": ch, "created": p.At, "created_by": p.By, "message": m})
	}
	return okMap("items", items)
}

func (w *World) messagesList(ids string) any {
	var want []struct {
		Channel    string   `json:"channel"`
		Timestamps []string `json:"timestamps"`
	}
	_ = json.Unmarshal([]byte(ids), &want)
	out := map[string][]slack.Message{}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, q := range want {
		for _, ts := range q.Timestamps {
			if m, ok := w.findLocked(q.Channel, ts); ok {
				m = copyMsg(m)
				m.Channel = q.Channel
				out[q.Channel] = append(out[q.Channel], m)
			}
		}
	}
	return okMap("messages", out)
}

// roomsJoin: the demo has no calls. The toast says why, and whoever you
// tried to call answers in the chat.
func (w *World) roomsJoin(f url.Values) any {
	w.bots.onJoinAttempt(f.Get("channel_id"))
	return map[string]any{"ok": false, "error": "huddles_unavailable_in_demo"}
}

// profileSections answers users.profile.getSections (the full profile)
// in fakeslack's profile.go shape: title, the viewer's email, manager
// (Gordon, for everyone but Gordon) and a start date.
func (w *World) profileSections(f url.Values) any {
	id := f.Get("user")
	w.mu.Lock()
	u, found := w.userLocked(id)
	w.mu.Unlock()
	if !found {
		return failMap("user_not_found")
	}
	self := id == SelfID
	el := func(section, eid, typename, typ, key, label string, missing bool, extra map[string]any) map[string]any {
		e := map[string]any{"id": id + ":section:" + section + ":" + eid, "hint": "", "isFilterable": false, "isScimManaged": false,
			"isHidden": false, "isMissing": missing, "label": label, "order": 0, "type": typ, "elementKey": key, "elementId": eid,
			"legacyFieldId": "Xf" + eid[2:], "defaultVisibility": "INTERNAL", "__typename": typename,
			"permissions": map[string]any{"api": []any{}, "ui": true, "scim": false, "__typename": "ProfileElementWritePermissions"}}
		for k, v := range extra {
			e[k] = v
		}
		return e
	}
	sec := func(sid, typ, label string, canEdit bool, els ...map[string]any) map[string]any {
		if els == nil {
			els = []map[string]any{}
		}
		return map[string]any{"id": id + ":section:" + sid, "sectionId": sid, "label": label, "order": 0, "type": typ,
			"canEdit": canEdit && self, "profileElements": els, "__typename": "ProfileSection"}
	}
	text := func(v string) map[string]any { return map[string]any{"text": v, "options": nil} }
	var hdr, contact, people, about []map[string]any
	if u.Profile.Title != "" {
		hdr = append(hdr, el("PsHEADER", "PeTITLE", "ProfileTextElement", "TEXT", "title", "Title", false, text(u.Profile.Title)))
	}
	if self {
		contact = append(contact, el("PsCONTACT", "PeEMAIL", "ProfileTextElement", "TEXT", "email", "Email Address", false,
			text(orStr(u.Profile.Email, "jordan.pike@quillfeather.example"))))
	}
	if !u.IsBot && id != "U0DEMOGORD" {
		people = append(people, el("PsPEOPLE", "PeMANAGER", "ProfilePersonElement", "PERSON", "custom_1000000000001", "Manager", false,
			map[string]any{"persons": []string{"U0DEMOGORD"}}))
	}
	if !u.IsBot {
		start := w.now().AddDate(0, 0, -9)
		if !self {
			n := 0
			for _, r := range id {
				n = n*31 + int(r)
			}
			start = w.now().AddDate(-(1 + n%11), -(n % 12), 0)
		}
		about = append(about, el("PsABOUT", "PeSTART", "ProfileDateElement", "DATE", "Start Date", "Start Date", false,
			map[string]any{"date": start.Format("2006-01-02")}))
	}
	sections := []map[string]any{
		sec("PsHEADER", "HEADER", "Header", false, hdr...),
		sec("PsCONTACT", "CONTACT", "Contact information", true, contact...),
		sec("PsPEOPLE", "PEOPLE", "People", true, people...),
		sec("PsABOUT", "ADDITIONAL_INFO", "About me", true, about...),
	}
	return okMap("result", map[string]any{"data": map[string]any{"user": map[string]any{
		"id": id, "isSelf": self, "profileSections": sections, "__typename": "User"}}})
}
