package taut

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// The Redux mirror's Go half. JS reads each slice (or one entry of an
// id-keyed slice) through taut_get / taut_keys on first use and caches it
// until Go invalidates it. The shapes are the ones Slack's web client keeps
// in its store (recorded from the live client, 2026-11): see
// docs/PLUGINS-state.md.

// mapSlices are id-keyed and read entry by entry.
var mapSlices = []string{
	"channels", "members", "messages", "bots", "membershipCounts", "membership",
	"readOnlyChannels", "threadOnlyChannels", "presence", "unreadChannelCounts",
	"channelCursors", "userGroups", "channelHistory", "teams", "reactions",
	"pendingFileUploads", "files", // the composer's attachments (files.go)
}

// wholeSlices are read whole.
var wholeSlices = []string{
	"bootData", "selfTeamIds", "prefs", "userPrefs", "teamPrefs", "channelSections", "typing",
	"dndV2", "drafts", "slashCommand", "experiments", "huddleEvents", "allCalls", "peers",
	"messageEditWindow", "searchResults", "huddlesCanvas", "alert", "unreadCounts",
	"mutedChannels", "starredChannels", "huddles", "messagesMeta", "threadSub", "userGroupMembership",
	"clientHuddles",
}

type change struct {
	Slice string   `json:"slice"`
	Keys  []string `json:"keys,omitempty"`
}

type typingRec struct {
	ChannelID    string  `json:"channelId"`
	ThreadTs     *string `json:"threadTs"`
	MemberID     string  `json:"memberId"`
	MemberIsSelf bool    `json:"memberIsSelf"`
	Timestamp    int64   `json:"timestamp"`
	expires      time.Time
}

type membershipRec struct {
	IsKnown  bool `json:"isKnown"`
	IsMember bool `json:"isMember"`
}

type mirror struct {
	t *Taut

	mu         sync.Mutex
	typing     map[string]typingRec
	membership map[string]map[string]membershipRec
	editing    map[string]map[string]any
	drafts     map[string]json.RawMessage
	search     json.RawMessage
	calls      map[string]json.RawMessage
	commands   json.RawMessage
	cmdLoading bool
	toasts     []any
	watched    map[string]map[string]uint64 // slice → key → fingerprint JS last saw

	pending []change
	flush   *time.Timer
	cancel  context.CancelFunc
	started bool
}

func newMirror(t *Taut) *mirror {
	return &mirror{
		t: t, typing: map[string]typingRec{}, membership: map[string]map[string]membershipRec{},
		editing: map[string]map[string]any{}, drafts: map[string]json.RawMessage{}, calls: map[string]json.RawMessage{},
		watched: map[string]map[string]uint64{},
	}
}

// start follows core's events and raw realtime frames while the runtime is up.
func (m *mirror) start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.mu.Unlock()

	evs, stop := m.t.app.Subscribe()
	raws, stopRaw := m.t.app.SubscribeRaw()
	go func() {
		defer stop()
		defer stopRaw()
		tick := time.NewTicker(750 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-evs:
				if !ok {
					return
				}
				m.onEvent(ev)
			case ev := <-raws:
				m.t.onRaw(ev)
			case <-tick.C:
				m.sweep()
			}
		}
	}()
}

func (m *mirror) stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
}

// invalidate queues a change for JS; changes are sent together a moment later.
func (m *mirror) invalidate(slice string, keys ...string) {
	if len(keys) == 0 {
		m.t.ui.views.stale(slice, "")
	}
	for _, k := range keys {
		m.t.ui.views.stale(slice, k)
	}
	m.mu.Lock()
	m.pending = append(m.pending, change{Slice: slice, Keys: keys})
	if w := m.watched[slice]; w != nil {
		if len(keys) == 0 {
			delete(m.watched, slice)
		} else {
			for _, k := range keys {
				delete(w, k)
			}
		}
	}
	if m.flush == nil {
		m.flush = time.AfterFunc(8*time.Millisecond, m.send)
	}
	m.mu.Unlock()
}

func (m *mirror) send() {
	m.mu.Lock()
	m.flush = nil
	batch := m.pending
	m.pending = nil
	m.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	// merge per slice: an all-keys change swallows key lists
	merged := map[string]*change{}
	var order []string
	for _, c := range batch {
		cur := merged[c.Slice]
		if cur == nil {
			cc := c
			merged[c.Slice] = &cc
			order = append(order, c.Slice)
			continue
		}
		if len(cur.Keys) == 0 {
			continue
		}
		if len(c.Keys) == 0 {
			cur.Keys = nil
			continue
		}
		cur.Keys = append(cur.Keys, c.Keys...)
	}
	out := make([]change, 0, len(order))
	for _, s := range order {
		out = append(out, *merged[s])
	}
	m.t.mu.Lock()
	rt := m.t.rt
	m.t.mu.Unlock()
	if rt != nil {
		rt.post("invalidate", out)
	}
}

// watch records what JS just read, so sweep can tell when it changes
// without an event (a history page core loaded on the UI's request).
func (m *mirror) watch(slice, key string, fp uint64) {
	m.mu.Lock()
	w := m.watched[slice]
	if w == nil {
		w = map[string]uint64{}
		m.watched[slice] = w
	}
	if len(w) > 4096 {
		for k := range w { // bounded: forget an arbitrary one
			delete(w, k)
			break
		}
	}
	w[key] = fp
	m.mu.Unlock()
}

func (m *mirror) sweep() {
	m.mu.Lock()
	now := time.Now()
	typingGone := false
	for k, r := range m.typing {
		if now.After(r.expires) {
			delete(m.typing, k)
			typingGone = true
		}
	}
	type item struct {
		slice, key string
		fp         uint64
	}
	var items []item
	for s, w := range m.watched {
		for k, fp := range w {
			items = append(items, item{s, k, fp})
		}
	}
	m.mu.Unlock()
	if typingGone {
		m.invalidate("typing")
	}
	for _, it := range items {
		var fp uint64
		switch it.slice {
		case "messages":
			fp = m.messagesFP(it.key)
		case "members":
			fp = m.memberFP(it.key)
		case "channels":
			fp = m.channelFP(it.key)
		default:
			continue
		}
		if fp != it.fp {
			m.invalidate(it.slice, it.key)
			if it.slice == "messages" {
				m.invalidate("channelHistory")
				m.invalidate("reactions")
			}
		}
	}
}

// ---- events ----

func (m *mirror) onEvent(ev core.Event) {
	switch ev.Type {
	case "message", "update", "delete", "sent":
		m.invalidate("messages", ev.Channel)
		m.invalidate("channelHistory")
		m.invalidate("reactions")
		if ev.Count != nil {
			m.invalidate("unreadChannelCounts", ev.Channel)
			m.invalidate("channelCursors", ev.Channel)
		}
		if ev.Type == "message" && ev.Msg != nil && ev.Msg.User != "" {
			m.stopTyping(ev.Channel, ev.Msg.ThreadTS, ev.Msg.User)
		}
	case "typing":
		m.startTyping(ev.Channel, ev.ThreadTS, ev.UserID)
	case "marked":
		m.invalidate("unreadChannelCounts", ev.Channel)
		m.invalidate("channelCursors", ev.Channel)
	case "counts":
		m.invalidate("unreadChannelCounts")
		m.invalidate("channelCursors")
		m.invalidate("unreadCounts")
	case "presence":
		m.invalidate("presence", ev.Users...)
	case "user":
		id := ev.UserID
		if id == "" && ev.User != nil {
			id = ev.User.ID
		}
		if id != "" {
			m.invalidate("members", id)
		} else {
			m.invalidate("members")
		}
		if id == m.t.app.S.SelfID() {
			m.invalidate("dndV2")
		}
	case "conv", "conv_removed":
		id := ev.Channel
		if id == "" && ev.Conv != nil {
			id = ev.Conv.ID
		}
		for _, s := range []string{"channels", "readOnlyChannels", "threadOnlyChannels", "membershipCounts", "membership"} {
			if id != "" {
				m.invalidate(s, id)
			} else {
				m.invalidate(s)
			}
		}
	case "sidebar_changed", "channel_sections_upserted", "channel_sections_channels_upserted", "channel_sections_channels_removed", "channel_sections_deleted":
		m.invalidate("channelSections")
		m.invalidate("starredChannels")
	case "prefs", "pref_change":
		m.invalidate("prefs")
		m.invalidate("userPrefs")
		m.invalidate("mutedChannels")
	case "huddle":
		m.noteHuddle(ev)
	case "draft":
		m.noteDraft(ev)
	case "dnd_updated", "dnd_updated_user":
		m.invalidate("dndV2")
	case "emoji_changed":
	case "resync", "connection":
		if ev.Type == "resync" {
			for _, s := range mapSlices {
				m.invalidate(s)
			}
			for _, s := range wholeSlices {
				m.invalidate(s)
			}
		}
	}
}

func typingKey(ch, thread, user string) string { return ch + "|" + thread + "|" + user }

func (m *mirror) startTyping(ch, thread, user string) {
	if ch == "" || user == "" {
		return
	}
	r := typingRec{ChannelID: ch, MemberID: user, MemberIsSelf: user == m.t.app.S.SelfID(), Timestamp: time.Now().UnixMilli(), expires: time.Now().Add(6 * time.Second)}
	if thread != "" {
		th := thread
		r.ThreadTs = &th
	}
	m.mu.Lock()
	m.typing[typingKey(ch, thread, user)] = r
	m.mu.Unlock()
	m.invalidate("typing")
}

func (m *mirror) stopTyping(ch, thread, user string) {
	m.mu.Lock()
	_, had := m.typing[typingKey(ch, thread, user)]
	delete(m.typing, typingKey(ch, thread, user))
	m.mu.Unlock()
	if had {
		m.invalidate("typing")
	}
}

func (m *mirror) noteHuddle(ev core.Event) {
	h, ok := core.ParseHuddle(ev.Raw)
	if !ok || h.ID == "" {
		return
	}
	call := map[string]any{"id": h.ID, "name": h.Name, "channels": h.Channels, "createdBy": h.CreatedBy, "hasEnded": !h.Active && h.End > 0, "participants": h.Participants, "dateStart": h.Start, "dateEnd": h.End, "isDmCall": h.DM}
	b, _ := json.Marshal(call)
	m.mu.Lock()
	m.calls[h.ID] = b
	m.mu.Unlock()
	m.invalidate("allCalls")
}

func (m *mirror) noteDraft(ev core.Event) {
	m.mu.Lock()
	if ev.Name == "delete" || ev.Name == "send" || ev.Draft == nil {
		delete(m.drafts, ev.ID)
	} else {
		m.drafts[ev.Draft.ID] = draftJSON(ev.Draft)
	}
	m.mu.Unlock()
	m.invalidate("drafts")
}

// SetDrafts gives the mirror the drafts the UI loaded (core.Drafts), so
// plugins see `drafts.unifiedDrafts`.
func (t *Taut) SetDrafts(ds []core.Draft) {
	t.mirror.mu.Lock()
	t.mirror.drafts = map[string]json.RawMessage{}
	for i := range ds {
		t.mirror.drafts[ds[i].ID] = draftJSON(&ds[i])
	}
	t.mirror.mu.Unlock()
	t.mirror.invalidate("drafts")
}

func draftJSON(d *core.Draft) json.RawMessage {
	dest := []map[string]any{}
	if d.Channel != "" {
		e := map[string]any{"channel_id": d.Channel}
		if d.ThreadTS != "" {
			e["thread_ts"] = d.ThreadTS
		}
		if d.Broadcast {
			e["broadcast"] = true
		}
		dest = append(dest, e)
	} else if len(d.Users) > 0 {
		dest = append(dest, map[string]any{"user_ids": d.Users})
	}
	var blocks any = []any{}
	if len(d.Blocks) > 0 {
		blocks = d.Blocks
	}
	fileIDs := d.FileIDs
	if fileIDs == nil {
		fileIDs = []string{}
	}
	b, _ := json.Marshal(map[string]any{
		"id": d.ID, "client_draft_id": d.ClientMsgID, "destinations": dest, "blocks": blocks,
		"ops": []map[string]any{{"insert": d.Text + "\n"}}, "file_ids": fileIDs, "date_created": d.DateCreated,
		"date_scheduled": d.DateScheduled, "last_updated_ts": d.LastUpdated, "is_from_composer": false,
	})
	return b
}

// SetEditing tells plugins a message is open in an editor (Slack's
// messageEditWindow); patchMessageText leaves it unchanged meanwhile.
func (t *Taut) SetEditing(windowID, ch, ts string, open bool) {
	if !t.Started() {
		return
	}
	t.mirror.mu.Lock()
	if open {
		t.mirror.editing[windowID] = map[string]any{"channelId": ch, "ts": ts}
	} else {
		delete(t.mirror.editing, windowID)
	}
	t.mirror.mu.Unlock()
	t.mirror.invalidate("messageEditWindow")
}

// SetSearchResults shows a search response (Slack's search.modules /
// search.messages JSON) to plugins under searchResults[instance].
func (t *Taut) SetSearchResults(instance string, response json.RawMessage) {
	if !t.Started() {
		return
	}
	wrapped, _ := json.Marshal(map[string]any{instance: map[string]any{"response": json.RawMessage(response)}})
	t.mirror.mu.Lock()
	t.mirror.search = wrapped
	t.mirror.mu.Unlock()
	t.mirror.invalidate("searchResults")
}

// noteMembership records answers of getChannelMembershipInfoForUsers.
func (m *mirror) noteMembership(ch string, users map[string]bool) {
	m.mu.Lock()
	b := m.membership[ch]
	if b == nil {
		b = map[string]membershipRec{}
		m.membership[ch] = b
	}
	for u, in := range users {
		b[u] = membershipRec{IsKnown: true, IsMember: in}
	}
	m.mu.Unlock()
	m.invalidate("membership", ch)
}

// ---- reads (JS thread, through host functions) ----

func (m *mirror) slices() map[string][]string {
	return map[string][]string{"maps": mapSlices, "wholes": wholeSlices}
}

func (m *mirror) keys(slice string) []string {
	a := m.t.app
	switch slice {
	case "pendingFileUploads", "files":
		return pendingKeys(slice)
	case "channels", "readOnlyChannels", "threadOnlyChannels", "membershipCounts", "unreadChannelCounts", "channelCursors":
		ids := a.S.ConvIDs()
		if slice == "readOnlyChannels" || slice == "threadOnlyChannels" {
			out := ids[:0:0]
			for _, id := range ids {
				if m.get(slice, id) != nil {
					out = append(out, id)
				}
			}
			return out
		}
		return ids
	case "members", "presence":
		return a.S.UserIDs()
	case "messages":
		return a.S.MessageChannels()
	case "channelHistory":
		var out []string
		for _, ch := range a.S.MessageChannels() {
			out = append(out, ch)
			for root := range a.S.ThreadsIn(ch) {
				out = append(out, ch+"-"+root)
			}
		}
		return out
	case "reactions":
		// Slack's reactions slice: _rxn_key → the message's reactions
		var out []string
		for _, ch := range a.S.MessageChannels() {
			for ts, msg := range m.bucket(ch) {
				if mm, ok := msg.(map[string]any); ok && mm["reactions"] != nil {
					out = append(out, "message-"+ts+"-"+ch)
				}
			}
		}
		sort.Strings(out)
		return out
	case "bots":
		return a.S.BotIDs()
	case "membership":
		m.mu.Lock()
		defer m.mu.Unlock()
		out := make([]string, 0, len(m.membership))
		for k := range m.membership {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	case "userGroups":
		a.S.RLock()
		defer a.S.RUnlock()
		out := make([]string, 0, len(a.S.Groups))
		for _, g := range a.S.Groups {
			out = append(out, g.ID)
		}
		return out
	case "teams":
		return m.teamIDs()
	}
	return nil
}

// get returns slice[key] (key "" for whole slices) as JSON-able data, or nil.
func (m *mirror) get(slice, key string) any {
	a := m.t.app
	switch slice {
	// ---- id-keyed ----
	case "pendingFileUploads", "files":
		return pendingEntry(slice, key)
	case "channels":
		c, ok := a.S.Conv(key)
		if !ok {
			if c, ok = a.S.ExtConv(key); !ok {
				return nil
			}
		}
		m.watch("channels", key, m.channelFP(key))
		return m.channel(c)
	case "members":
		u, ok := a.S.User(key)
		if !ok {
			return nil
		}
		m.watch("members", key, m.memberFP(key))
		return m.member(u)
	case "messages":
		b := m.bucket(key)
		m.watch("messages", key, m.messagesFP(key))
		if b == nil {
			return nil
		}
		return b
	case "reactions":
		rest, ok := strings.CutPrefix(key, "message-")
		i := strings.LastIndex(rest, "-")
		if !ok || i < 0 {
			return nil
		}
		msg, _ := m.bucket(rest[i+1:])[rest[:i]].(map[string]any)
		if msg == nil || msg["reactions"] == nil {
			return nil
		}
		return msg["reactions"]
	case "bots":
		bi, ok := a.S.Bot(key)
		if !ok {
			return nil
		}
		bot := map[string]any{"id": key, "name": bi.Name, "deleted": false}
		if bi.Icon != "" {
			bot["icons"] = map[string]any{"image_36": bi.Icon, "image_48": bi.Icon, "image_72": bi.Icon}
		}
		return pruneEmpty(bot, false)
	case "membershipCounts":
		n, ok := a.MemberCountCached(key)
		guests := 0
		if ok {
			guests = a.GuestCountCached(key)
		}
		if !ok {
			c, have := a.S.Conv(key)
			if !have || c.NumMembers == 0 {
				return nil
			}
			n = c.NumMembers
		}
		team := a.C.Creds.EnterpriseID
		if team == "" {
			team = a.C.Creds.Team
		}
		return map[string]any{"counts": map[string]any{"member_count": n, "current_team_member_count": n, "restricted_member_count": guests, "member_count_by_team": map[string]int{team: n}}}
	case "membership":
		out := map[string]membershipRec{}
		if c, ok := a.S.Conv(key); ok {
			for _, u := range c.Members {
				out[u] = membershipRec{IsKnown: true, IsMember: true}
			}
		}
		m.mu.Lock()
		for u, r := range m.membership[key] {
			out[u] = r
		}
		m.mu.Unlock()
		if len(out) == 0 {
			return nil
		}
		return out
	case "readOnlyChannels", "threadOnlyChannels":
		c, ok := a.S.Conv(key)
		if !ok {
			return nil
		}
		post, thread := m.restrictions(c)
		if slice == "readOnlyChannels" && post && thread {
			return map[string]bool{"isReadOnly": true}
		}
		if slice == "threadOnlyChannels" && post && !thread {
			return map[string]bool{"isThreadOnly": true}
		}
		return nil
	case "presence":
		a.S.RLock()
		p := a.S.Presence[key]
		a.S.RUnlock()
		if p == "" {
			return nil
		}
		return map[string]string{"id": key, "presence": p}
	case "unreadChannelCounts":
		e, ok := a.S.Count(key)
		if !ok {
			return nil
		}
		unread := 0
		if e.HasUnreads {
			unread = 1
		}
		return map[string]any{"unreadCnt": unread, "unreadHighlightCnt": e.MentionCount, "unreadVIPMentionCnt": 0}
	case "channelCursors":
		e, ok := a.S.Count(key)
		if !ok || e.LastRead == "" {
			return nil
		}
		return e.LastRead
	case "userGroups":
		a.S.RLock()
		defer a.S.RUnlock()
		for _, g := range a.S.Groups {
			if g.ID == key {
				return map[string]any{"id": g.ID, "handle": g.Handle, "name": g.Name, "description": g.Description, "user_count": g.UserCount,
					"date_delete": g.DateDelete, "is_usergroup": true, "is_subteam": true, "_name_lc": strings.ToLower(g.Name), "isNonExistent": false}
			}
		}
		return nil
	case "channelHistory":
		return m.history(key)
	case "teams":
		return m.team(key)

	// ---- whole ----
	case "bootData":
		return pruneEmpty(map[string]any{"user_id": a.S.SelfID(), "team_id": a.C.Creds.Team, "enterprise_id": a.C.Creds.EnterpriseID,
			"page_needs_enterprise": a.C.Creds.EnterpriseID != "", "version_ts": time.Now().Unix(), "version_uid": "clack"}, false)
	case "selfTeamIds":
		ent := a.C.Creds.EnterpriseID
		team := a.C.Creds.Team
		top := team
		if ent != "" {
			top = ent
		}
		ws := []string{}
		if team != "" {
			ws = append(ws, team)
		}
		return pruneEmpty(map[string]any{"teamId": top, "enterpriseId": ent, "internalWorkspaceIds": ws, "defaultWorkspaceId": team}, false)
	case "prefs":
		return map[string]any{"user": m.userPrefs(), "team": map[string]any{}, "channel": map[string]any{}, "enterprise": map[string]any{}}
	case "userPrefs":
		return m.userPrefs()
	case "teamPrefs":
		return map[string]any{}
	case "channelSections":
		return m.sections()
	case "starredChannels":
		ids := []string{}
		for _, id := range a.S.ConvIDs() {
			if a.Starred(id) {
				ids = append(ids, id)
			}
		}
		return ids
	case "mutedChannels":
		a.S.RLock()
		defer a.S.RUnlock()
		out := map[string]bool{}
		for id, mu := range a.S.Muted {
			if mu {
				out[id] = true
			}
		}
		return out
	case "typing":
		m.mu.Lock()
		defer m.mu.Unlock()
		out := make(map[string]typingRec, len(m.typing))
		for k, r := range m.typing {
			out[k] = r
		}
		return out
	case "dndV2":
		return m.dnd()
	case "drafts":
		m.mu.Lock()
		defer m.mu.Unlock()
		ids := make([]string, 0, len(m.drafts))
		for id := range m.drafts {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return map[string]any{"unifiedDrafts": m.drafts, "channelIds": ids, "withheldDraftIds": []string{}, "isEditingDraftPage": false, "selectedDraftIds": []string{}}
	case "slashCommand":
		return m.slashCommands()
	case "experiments":
		return map[string]any{}
	case "huddleEvents":
		return m.t.huddleEventsSlice()
	case "allCalls":
		m.mu.Lock()
		defer m.mu.Unlock()
		return map[string]any{"byId": m.calls, "inviteResponseById": map[string]any{}}
	case "peers":
		return m.t.peersSlice()
	case "clientHuddles":
		return m.t.clientHuddlesSlice()
	case "messageEditWindow":
		m.mu.Lock()
		defer m.mu.Unlock()
		out := make(map[string]any, len(m.editing))
		for k, v := range m.editing {
			out[k] = v
		}
		return out
	case "searchResults":
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.search == nil {
			return map[string]any{}
		}
		return m.search
	case "huddlesCanvas":
		return map[string]any{"backgrounds": []any{}}
	case "alert":
		return map[string]any{"alerts": []any{}}
	case "unreadCounts":
		counts := a.Counts()
		total, chans := 0, 0
		for _, c := range counts {
			if c.Unread && !c.Muted {
				chans++
				total += c.Mentions
			}
		}
		return map[string]any{"totalUnreads": total, "totalUnreadsInChannels": total, "totalUnreadChannelConversations": chans}
	case "huddles", "messagesMeta", "threadSub", "userGroupMembership":
		return map[string]any{}
	}
	return nil
}

// ---- shapes ----

var nonSpacing = unicode.Mn

func deburr(s string) string {
	d := norm.NFKD.String(s)
	return strings.Map(func(r rune) rune {
		if unicode.Is(nonSpacing, r) {
			return -1
		}
		return r
	}, d)
}

func toMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}

func (m *mirror) channel(c *slack.Conversation) map[string]any {
	e := toMap(c)
	delete(e, "latest")
	name := c.Name
	e["name_normalized"] = deburr(name)
	e["_name_lc"] = deburr(strings.ToLower(name))
	team := c.ContextTeamID
	if team == "" {
		team = m.t.app.C.Creds.Team
	}
	ids := []string{}
	if team != "" {
		ids = append(ids, team)
	}
	for k, v := range map[string]any{
		"is_channel": c.IsChannel, "is_group": c.IsGroup, "is_im": c.IsIM, "is_mpim": c.IsMPIM, "is_private": c.IsPrivate,
		"is_archived": c.IsArchived, "is_general": c.IsGeneral, "is_member": c.IsMember || c.IsIM, "is_ext_shared": c.IsExtShared,
		"is_org_shared": c.IsOrgShared, "isNonExistent": false, "isUnknown": false, "fromAnotherTeam": false,
		"internalTeamIds": ids, "internal_team_ids": ids, "previous_names": []string{}, "unread_highlights": []any{}, "unreads": []any{},
		"connectedTeamIds": []string{}, "connectedLimitedTeamIds": []string{},
	} {
		if _, has := e[k]; !has || k == "is_member" {
			e[k] = v
		}
	}
	if team != "" {
		e["context_team_id"] = team
	}
	if _, ok := e["topic"]; !ok {
		e["topic"] = map[string]any{"value": "", "creator": "", "last_set": 0}
	}
	if _, ok := e["purpose"]; !ok {
		e["purpose"] = map[string]any{"value": "", "creator": "", "last_set": 0}
	}
	if len(c.Properties) > 0 {
		var props map[string]any
		if json.Unmarshal(c.Properties, &props) == nil {
			e["properties"] = props
		}
	}
	return e
}

// restrictions reports whether the viewer can't post at the top level /
// can't reply in threads, the source of readOnlyChannels and
// threadOnlyChannels. The rule is Slack's (core/posting.go:
// setPostingPermissions + computeChannelPostingPermissions).
func (m *mirror) restrictions(c *slack.Conversation) (postRestricted, threadRestricted bool) {
	p := m.t.app.PostingFor(c)
	return p.ReadOnly || p.ThreadOnly, p.ReadOnly
}

func (m *mirror) member(u *slack.User) map[string]any {
	e := toMap(u)
	p, _ := e["profile"].(map[string]any)
	if p == nil {
		p = map[string]any{}
		e["profile"] = p
	}
	pr := u.Profile
	p["real_name_normalized"] = deburr(pr.RealName)
	p["display_name_normalized"] = deburr(pr.DisplayName)
	// Slack always sends these as strings, "" when unset. Avatar URLs it
	// sends only when it has them, so absent ones stay absent (plugins
	// fall back with ??).
	for _, k := range []string{"real_name", "display_name", "status_text", "status_emoji", "first_name", "last_name", "title"} {
		if _, ok := p[k]; !ok {
			p[k] = ""
		}
	}
	if _, ok := p["status_expiration"]; !ok {
		p["status_expiration"] = 0
	}
	// the edge API's slim profiles carry avatar_hash but no image_* URLs;
	// Slack's client fills them in, and plugins read them (WhoReacted's
	// image_24 ?? image_48)
	if pr.AvatarHash != "" {
		team := u.TeamID
		if team == "" {
			team = pr.Team
		}
		if team == "" {
			m.t.app.S.RLock()
			team = m.t.app.S.TeamID
			m.t.app.S.RUnlock()
		}
		for _, size := range []int{24, 32, 48, 72, 192, 512} {
			k := "image_" + strconv.Itoa(size)
			if v, _ := p[k].(string); v == "" {
				p[k] = "https://ca.slack-edge.com/" + team + "-" + u.ID + "-" + pr.AvatarHash + "-" + strconv.Itoa(size)
			}
		}
	}
	realName := u.RealName
	if realName == "" {
		realName = pr.RealName
	}
	e["real_name"] = realName
	lcs := map[string]string{
		"_name_lc": u.Name, "_real_name_lc": realName, "_display_name_lc": pr.DisplayName,
		"_first_name_lc": pr.FirstName, "_last_name_lc": pr.LastName,
	}
	for k, v := range lcs {
		e[k] = strings.ToLower(v)
	}
	e["_real_name_normalized_lc"] = deburr(strings.ToLower(realName))
	e["_display_name_normalized_lc"] = deburr(strings.ToLower(pr.DisplayName))
	for k, v := range map[string]any{"deleted": u.Deleted, "is_admin": u.IsAdmin, "is_owner": u.IsOwner, "is_bot": u.IsBot,
		"is_app_user": u.IsAppUser, "is_restricted": u.IsRestricted, "is_ultra_restricted": u.IsUltraRestricted,
		"isUnknown": false, "isNonExistent": false, "isExternal": false, "is_stranger": u.IsStranger} {
		e[k] = v
	}
	// Slack's computeLocality: a Slack Connect member is isExternal
	if cu, ok := m.t.app.User(u.ID); ok && cu.External {
		e["isExternal"] = true
	}
	if e["team_id"] == nil && m.t.app.C.Creds.EnterpriseID != "" {
		e["team_id"] = m.t.app.C.Creds.EnterpriseID
	}
	if u.ID == m.t.app.S.SelfID() {
		e["is_self"] = true
		mp := "active"
		m.t.app.S.RLock()
		if m.t.app.S.Presence[u.ID] == "away" {
			mp = "away"
		}
		m.t.app.S.RUnlock()
		e["manual_presence"] = mp
	}
	return e
}

// messageJSON is a stored message as Slack's store keeps it.
func messageJSON(ch string, sm *slack.Message) map[string]any {
	e := toMap(sm)
	// attachments exactly as Slack sent them (the typed ones drop fields
	// and add zero values); blocks, metadata and bot_profile are already
	// Slack's own JSON
	delete(e, "attachments")
	if len(sm.RawAttachments) > 0 {
		var raw any
		if json.Unmarshal(sm.RawAttachments, &raw) == nil {
			e["attachments"] = raw
		}
	} else if len(sm.Attachments) > 0 {
		e["attachments"] = pruneEmpty(toMap(map[string]any{"a": sm.Attachments})["a"], true)
	}
	if files, ok := e["files"]; ok {
		e["files"] = pruneEmpty(files, true)
	}
	if bp, ok := e["bot_profile"]; ok {
		e["bot_profile"] = pruneEmpty(bp, false)
	}
	if sm.Root != nil {
		e["root"] = messageJSON(ch, sm.Root)
	}
	e["channel"] = ch
	if e["type"] == nil {
		e["type"] = "message"
	}
	if _, ok := e["reply_count"]; !ok {
		e["reply_count"] = 0
	}
	e["_rxn_key"] = "message-" + sm.TS + "-" + ch
	e["no_display"] = false
	if _, ok := e["text"]; !ok {
		e["text"] = ""
	}
	return e
}

// pruneEmpty drops map entries that are "" or null (and, with zeros, 0),
// recursively: Go zero values Slack would leave out, which would defeat
// plugins' `??` fallbacks. Array elements are kept.
func pruneEmpty(v any, zeros bool) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			switch y := val.(type) {
			case nil:
				delete(x, k)
			case string:
				if y == "" {
					delete(x, k)
				}
			case float64:
				if zeros && y == 0 {
					delete(x, k)
				}
			default:
				x[k] = pruneEmpty(val, zeros)
			}
		}
		return x
	case []any:
		for i := range x {
			x[i] = pruneEmpty(x[i], zeros)
		}
		return x
	}
	return v
}

// bucket is messages[ch]: the cached timeline plus open threads' replies.
func (m *mirror) bucket(ch string) map[string]any {
	a := m.t.app
	tl := a.S.Timeline(ch)
	threads := a.S.ThreadsIn(ch)
	if tl == nil && len(threads) == 0 {
		return nil
	}
	out := map[string]any{}
	if tl != nil {
		for i := range tl.Msgs {
			out[tl.Msgs[i].TS] = messageJSON(ch, &tl.Msgs[i])
		}
	}
	for _, msgs := range threads {
		for i := range msgs {
			if _, ok := out[msgs[i].TS]; !ok {
				out[msgs[i].TS] = messageJSON(ch, &msgs[i])
			}
		}
	}
	return out
}

// history is channelHistory[ch] or channelHistory[ch-threadTs].
func (m *mirror) history(key string) any {
	a := m.t.app
	ch, thread, isThread := strings.Cut(key, "-")
	var ts []string
	reachedStart := false
	if isThread {
		msgs, ok := a.S.Thread(ch, thread)
		if !ok {
			return nil
		}
		for _, x := range msgs {
			ts = append(ts, x.TS)
		}
		reachedStart = true
	} else {
		tl := a.S.Timeline(ch)
		if tl == nil {
			return nil
		}
		for _, x := range tl.Msgs {
			if x.ThreadTS == "" || x.ThreadTS == x.TS || x.Subtype == "thread_broadcast" {
				ts = append(ts, x.TS)
			}
		}
		reachedStart = !tl.HasMore
	}
	if ts == nil {
		ts = []string{}
	}
	slice := map[string]any{"timestamps": ts, "start": nil, "end": nil}
	if len(ts) > 0 {
		slice["start"], slice["end"] = ts[0], ts[len(ts)-1]
	}
	return map[string]any{"reachedStart": reachedStart, "reachedEnd": true, "slices": []any{slice}, "prevReachedEnd": true}
}

func (m *mirror) teamIDs() []string {
	a := m.t.app
	var ids []string
	if a.C.Creds.Team != "" {
		ids = append(ids, a.C.Creds.Team)
	}
	if e := a.C.Creds.EnterpriseID; e != "" && e != a.C.Creds.Team {
		ids = append(ids, e)
	}
	return ids
}

func (m *mirror) team(id string) any {
	a := m.t.app
	a.S.RLock()
	name, icon, domain, teamID := a.S.TeamName, a.S.TeamIcon, a.S.TeamDomain, a.S.TeamID
	a.S.RUnlock()
	if teamID == "" {
		teamID = a.C.Creds.Team
	}
	if id != teamID && id != a.C.Creds.EnterpriseID {
		return nil
	}
	t := map[string]any{"id": id, "name": name, "domain": domain, "url": a.C.Creds.TeamURL, "enterprise_id": a.C.Creds.EnterpriseID,
		"enterprise_name": name, "isUnknown": false, "isNonExistent": false}
	if icon != "" {
		t["icon"] = map[string]any{"image_68": icon, "image_132": icon}
	}
	return pruneEmpty(t, false)
}

func (m *mirror) userPrefs() map[string]any {
	p := m.t.app.Prefs()
	muted := strings.Join(p.Muted, ",")
	realNames := 0
	if p.DisplayRealNames {
		realNames = 1
	}
	return map[string]any{
		"emoji_mode": p.EmojiMode, "preferred_skin_tone": p.SkinTone, "time24": p.Time24,
		"display_real_names_override": realNames, "display_real_names": p.DisplayRealNames, "jumbomoji": p.Jumbomoji,
		"convert_emoticons": p.ConvertEmoticons, "sidebar_behavior": p.SidebarBehavior, "channel_sort": p.ChannelSort,
		"locale": p.Locale, "theme": p.Theme, "dark_mode": p.DarkMode, "sidebar_theme": p.SidebarTheme,
		"messages_theme": p.MessagesTheme, "enter_is_special_in_tbt": p.EnterIsSpecial, "show_typing": p.ShowTyping,
		"color_names_in_list": p.ColorNames, "muted_channels": muted, "global_desktop": p.GlobalDesktop,
		"emoji_use": "{}",
	}
}

func (m *mirror) sections() map[string]any {
	snap := m.t.app.Snapshot()
	byID := map[string]any{}
	ids := map[string][]string{}
	byChannel := map[string][]string{}
	ordered := make([]any, 0, len(snap.Sections))
	for i, s := range snap.Sections {
		next := ""
		if i+1 < len(snap.Sections) {
			next = snap.Sections[i+1].ID
		}
		e := map[string]any{"id": s.ID, "type": s.Type, "name": s.Name, "emoji": s.Emoji, "nextChannelSectionId": next}
		byID[s.ID] = e
		ordered = append(ordered, e)
		ids[s.ID] = append([]string{}, s.Channels...)
		for _, c := range s.Channels {
			byChannel[c] = append(byChannel[c], s.ID)
		}
	}
	return map[string]any{"channelSectionById": byID, "orderedChannelSectionList": ordered, "channelIdsByChannelSectionId": ids,
		"channelSectionIdsByChannelId": byChannel, "autoSortedSectionIdByChannelId": map[string]any{}}
}

func (m *mirror) dnd() map[string]any {
	// Clack keeps no DND cache; plugins that need it ask profile.getSnooze,
	// which this answers as "not paused" until a dnd event says otherwise.
	snooze := map[string]any{"snoozeEnabled": false}
	return map[string]any{"currentMemberSnooze": snooze, "currentStatuses": map[string]bool{}, "schedules": map[string]any{}, "profileStatuses": map[string]any{}}
}

// slashCommands is Slack's slashCommand slice. The list needs a request,
// so the first read starts it and invalidates when it lands.
func (m *mirror) slashCommands() any {
	m.mu.Lock()
	cmds := m.commands
	loading := m.cmdLoading
	if cmds == nil && !loading {
		m.cmdLoading = true
	}
	m.mu.Unlock()
	if cmds == nil && !loading {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			list, err := m.t.app.Commands(ctx, "")
			out := map[string]any{}
			for _, c := range list {
				key := c.Name
				e := map[string]any{"name": c.Name, "desc": c.Desc, "usage": c.Usage, "type": c.Type, "autocomplete": true, "isServer": true}
				if c.AppID != "" {
					key = c.AppID + "-" + c.Name
					e["app"] = c.AppID
					e["app_name"] = c.AppName
				}
				out[key] = e
			}
			b, _ := json.Marshal(map[string]any{"commands": out})
			m.mu.Lock()
			if err == nil {
				m.commands = b
			}
			m.cmdLoading = false
			m.mu.Unlock()
			if err == nil {
				m.invalidate("slashCommand")
			}
		}()
	}
	if cmds == nil {
		return map[string]any{"commands": map[string]any{}}
	}
	return cmds
}

// ---- fingerprints (cheap change detection for watched entries) ----

func (m *mirror) messagesFP(ch string) uint64 {
	h := fnv.New64a()
	tl := m.t.app.S.Timeline(ch)
	if tl != nil {
		for i := range tl.Msgs {
			fpMsg(h, &tl.Msgs[i])
		}
	}
	for root, msgs := range m.t.app.S.ThreadsIn(ch) {
		h.Write([]byte(root))
		for i := range msgs {
			fpMsg(h, &msgs[i])
		}
	}
	return h.Sum64()
}

type hashWriter interface{ Write([]byte) (int, error) }

func fpMsg(h hashWriter, sm *slack.Message) {
	h.Write([]byte(sm.TS))
	if sm.Edited != nil {
		h.Write([]byte(sm.Edited.TS))
	}
	h.Write([]byte(strconv.Itoa(sm.ReplyCount)))
	h.Write([]byte(strconv.Itoa(len(sm.Text))))
	for _, r := range sm.Reactions {
		h.Write([]byte(r.Name))
		h.Write([]byte(strconv.Itoa(r.Count)))
	}
	h.Write([]byte(strconv.Itoa(len(sm.Files))))
	h.Write([]byte(sm.Subtype))
}

func (m *mirror) memberFP(id string) uint64 {
	u, ok := m.t.app.S.User(id)
	if !ok {
		return 0
	}
	h := fnv.New64a()
	h.Write([]byte(strconv.FormatInt(u.Updated, 10)))
	h.Write([]byte(u.Name + "\x00" + u.RealName + "\x00" + u.Profile.DisplayName + "\x00" + u.Profile.StatusText + "\x00" + u.Profile.StatusTextCanonical + "\x00" + u.Profile.StatusEmoji + "\x00" + u.Profile.Image72))
	return h.Sum64()
}

func (m *mirror) channelFP(id string) uint64 {
	c, ok := m.t.app.S.Conv(id)
	if !ok {
		if c, ok = m.t.app.S.ExtConv(id); !ok {
			return 0
		}
	}
	h := fnv.New64a()
	b, _ := json.Marshal(c)
	h.Write(b)
	return h.Sum64()
}

// SlackMessage is message ts in ch as Slack's store keeps it (what thunks
// such as showNotification carry as `message`), or nil when it isn't cached.
func (t *Taut) SlackMessage(ch, ts string) map[string]any {
	if t == nil || t.app == nil || t.app.S == nil {
		return nil
	}
	sm, ok := t.app.S.Find(ch, ts)
	if !ok {
		return nil
	}
	return messageJSON(ch, &sm)
}
