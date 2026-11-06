package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/render"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Routes beyond the core timeline API: search, directory, channel details,
// activity, threads, saved, drafts, DMs, presence, prefs, commands, uploads
// and the small write actions. Contracts are documented in docs/ARCHITECTURE.md.
func (a *App) registerMore(mux *http.ServeMux) {
	mux.HandleFunc("/api/prefs", a.hPrefs)
	mux.HandleFunc("/api/search", a.hSearch)
	mux.HandleFunc("/api/search/people", a.hSearchPeople)
	mux.HandleFunc("/api/search/channels", a.hSearchChannels)
	mux.HandleFunc("/api/browse/channels", a.hBrowseChannels)
	mux.HandleFunc("/api/usergroups", a.hUsergroups)
	mux.HandleFunc("/api/members", a.hMembers)
	mux.HandleFunc("/api/pins", a.hPins)
	mux.HandleFunc("/api/pin", a.hPin)
	mux.HandleFunc("/api/bookmarks", a.hBookmarks)
	mux.HandleFunc("/api/channel", a.hChannel)
	mux.HandleFunc("/api/activity", a.hActivity)
	mux.HandleFunc("/api/threads", a.hThreads)
	mux.HandleFunc("/api/thread/subscribe", a.hThreadSubscribe)
	mux.HandleFunc("/api/saved", a.hSaved)
	mux.HandleFunc("/api/save", a.hSave)
	mux.HandleFunc("/api/drafts", a.hDrafts)
	mux.HandleFunc("/api/dms", a.hDMs)
	mux.HandleFunc("/api/presence", a.hPresence)
	mux.HandleFunc("/api/presence/sub", a.hPresenceSub)
	mux.HandleFunc("/api/presence/set", a.hPresenceSet)
	mux.HandleFunc("/api/status", a.hStatus)
	mux.HandleFunc("/api/dnd", a.hDND)
	mux.HandleFunc("/api/commands", a.hCommands)
	mux.HandleFunc("/api/command", a.hCommand)
	mux.HandleFunc("/api/join", a.hJoin)
	mux.HandleFunc("/api/leave", a.hLeave)
	mux.HandleFunc("/api/open", a.hOpen)
	mux.HandleFunc("/api/upload", a.hUpload)
}

func q(r *http.Request, k string) string { return strings.TrimSpace(r.URL.Query().Get(k)) }

func qInt(r *http.Request, k string, def int) int {
	if n, err := strconv.Atoi(q(r, k)); err == nil && n > 0 {
		return n
	}
	return def
}

func needPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func badRequest(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}

func writeOK(w http.ResponseWriter, r *http.Request) { writeJSON(w, r, map[string]any{"ok": true}) }

// ---- shared view helpers ----

// ConvRef is a lightweight conversation descriptor for results that span
// conversations the client may not be a member of (search, activity, saved).
type ConvRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	User string `json:"user,omitempty"`
}

func (a *App) convRef(id string) ConvRef {
	if c, ok := a.S.Conv(id); ok {
		v := a.convView(c)
		return ConvRef{ID: id, Name: v.Name, Kind: v.Kind, User: v.User}
	}
	kind := "channel"
	switch {
	case strings.HasPrefix(id, "D"):
		kind = "im"
	case strings.HasPrefix(id, "G"):
		kind = "private"
	}
	return ConvRef{ID: id, Name: a.S.ChannelName(id), Kind: kind}
}

func (a *App) convRefs(ids map[string]bool) map[string]ConvRef {
	out := make(map[string]ConvRef, len(ids))
	for id := range ids {
		if id != "" {
			out[id] = a.convRef(id)
		}
	}
	return out
}

// hydrate fetches messages by (channel, ts) in one messages.list call.
func (a *App) hydrate(ctx context.Context, want map[string][]string) map[string]slack.Message {
	out := map[string]slack.Message{}
	missing := map[string][]string{}
	for ch, tss := range want {
		for _, ts := range tss {
			if m, ok := a.S.Find(ch, ts); ok {
				out[ch+"/"+ts] = m
			} else {
				missing[ch] = append(missing[ch], ts)
			}
		}
	}
	if len(missing) == 0 {
		return out
	}
	got, err := a.C.MessagesList(ctx, missing)
	if err != nil {
		log.Printf("messages.list: %v", err)
	}
	var all []slack.Message
	for ch, ms := range got {
		for _, m := range ms {
			out[ch+"/"+m.TS] = m
			all = append(all, m)
		}
	}
	a.ensureUsers(ctx, referencedUsers(all))
	return out
}

func (a *App) viewPtr(ch string, m *slack.Message) *render.View {
	if m == nil || m.TS == "" {
		return nil
	}
	v := render.Message(a.S, ch, m)
	return &v
}

// ---- prefs ----

func (a *App) prefsView() store.Prefs {
	a.S.RLock()
	defer a.S.RUnlock()
	return a.S.Prefs
}

func (a *App) hPrefs(w http.ResponseWriter, r *http.Request) {
	if q(r, "fresh") == "1" {
		raw, err := a.C.Prefs(r.Context())
		if err != nil {
			writeErr(w, err)
			return
		}
		a.S.SetPrefs(store.ParsePrefs(raw))
	}
	writeJSON(w, r, map[string]any{"ok": true, "prefs": a.prefsView()})
}

// ---- search ----

func (a *App) hSearch(w http.ResponseWriter, r *http.Request) {
	query := q(r, "q")
	if query == "" {
		badRequest(w, "q required")
		return
	}
	t0 := time.Now()
	res, err := a.C.SearchMessages(r.Context(), query, qInt(r, "page", 1), qInt(r, "count", 20), q(r, "sort"))
	if err != nil {
		writeErr(w, err)
		return
	}
	msgs := make([]slack.Message, 0, len(res.Matches))
	for _, m := range res.Matches {
		msgs = append(msgs, m.Message)
	}
	a.ensureUsers(r.Context(), referencedUsers(msgs))
	type hit struct {
		render.View
		Permalink string `json:"permalink,omitempty"`
	}
	hits := make([]hit, 0, len(res.Matches))
	chans := map[string]bool{}
	for i := range res.Matches {
		m := &res.Matches[i]
		ch := m.ChannelO.ID
		chans[ch] = true
		if m.ChannelO.Name != "" && !m.ChannelO.IsIM {
			a.S.PutName(ch, m.ChannelO.Name) // names of channels we are not in (metadata only)
		}
		hits = append(hits, hit{View: render.Message(a.S, ch, &m.Message), Permalink: m.Permalink})
	}
	log.Printf("search: %d results in %s", len(hits), time.Since(t0).Round(time.Millisecond))
	writeJSON(w, r, map[string]any{
		"ok": true, "messages": hits, "total": res.Total, "page": res.Page, "pages": res.Pages,
		"users": a.usersView(referencedUsers(msgs)), "convs": a.convRefs(chans),
	})
}

func (a *App) hSearchPeople(w http.ResponseWriter, r *http.Request) {
	query := q(r, "q")
	count := qInt(r, "count", 20)
	if query == "" {
		writeJSON(w, r, map[string]any{"ok": true, "users": []*UserView{}})
		return
	}
	us, err := a.C.SearchUsers(r.Context(), query, count)
	if err != nil {
		writeErr(w, err)
		return
	}
	a.S.PutUsers(us)
	out := make([]*UserView, 0, len(us))
	var ids []string
	for _, u := range us {
		if u.Deleted && q(r, "deleted") != "1" {
			continue
		}
		if v := a.userView(u.ID); v != nil {
			out = append(out, v)
			ids = append(ids, u.ID)
		}
	}
	a.subscribePresence(ids)
	writeJSON(w, r, map[string]any{"ok": true, "users": out})
}

// ChannelHitView is a channel search / browse result.
type ChannelHitView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	IsMember   bool   `json:"is_member"`
	IsArchived bool   `json:"is_archived,omitempty"`
	NumMembers int    `json:"num_members"`
	Purpose    string `json:"purpose,omitempty"`
	Topic      string `json:"topic,omitempty"`
	Created    int64  `json:"created,omitempty"`
}

func (a *App) channelHits(hs []slack.ChannelHit) []ChannelHitView {
	out := make([]ChannelHitView, 0, len(hs))
	for _, h := range hs {
		if h.IsMPIM {
			continue
		}
		v := ChannelHitView{ID: h.ID, Name: h.Name, Kind: "channel", IsMember: h.IsMember, IsArchived: h.IsArchived, NumMembers: h.NumMembers, Created: h.Created}
		if v.NumMembers == 0 {
			v.NumMembers = h.MemberCount
		}
		if h.IsPrivate {
			v.Kind = "private"
		}
		if _, ok := a.S.Conv(h.ID); ok {
			v.IsMember = true
		}
		a.S.PutName(h.ID, h.Name)
		if h.Purpose != nil {
			v.Purpose = h.Purpose.Value
		}
		if h.Topic != nil {
			v.Topic = h.Topic.Value
		}
		out = append(out, v)
	}
	return out
}

func (a *App) hSearchChannels(w http.ResponseWriter, r *http.Request) {
	query := q(r, "q")
	if query == "" {
		writeJSON(w, r, map[string]any{"ok": true, "channels": []ChannelHitView{}})
		return
	}
	hs, err := a.C.SearchChannels(r.Context(), query, qInt(r, "count", 20))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, r, map[string]any{"ok": true, "channels": a.channelHits(hs)})
}

func (a *App) hBrowseChannels(w http.ResponseWriter, r *http.Request) {
	hs, total, pages, err := a.C.BrowseChannels(r.Context(), q(r, "q"), qInt(r, "page", 1), q(r, "sort"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, r, map[string]any{"ok": true, "channels": a.channelHits(hs), "total": total, "pages": pages, "page": qInt(r, "page", 1)})
}

// ---- usergroups ----

type UsergroupView struct {
	ID          string `json:"id"`
	Handle      string `json:"handle"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	UserCount   int    `json:"user_count"`
}

func (a *App) hUsergroups(w http.ResponseWriter, r *http.Request) {
	if q(r, "fresh") == "1" || len(a.S.Groups) == 0 {
		if err := a.loadUsergroups(r.Context()); err != nil {
			writeErr(w, err)
			return
		}
	}
	needle := strings.ToLower(q(r, "q"))
	limit := qInt(r, "limit", 0)
	a.S.RLock()
	out := make([]UsergroupView, 0, len(a.S.Groups))
	for _, g := range a.S.Groups {
		if needle != "" && !strings.Contains(strings.ToLower(g.Handle), needle) && !strings.Contains(strings.ToLower(g.Name), needle) {
			continue
		}
		out = append(out, UsergroupView{ID: g.ID, Handle: g.Handle, Name: g.Name, Description: g.Description, UserCount: g.UserCount})
	}
	a.S.RUnlock()
	if needle != "" {
		sort.SliceStable(out, func(i, j int) bool {
			pi, pj := strings.HasPrefix(strings.ToLower(out[i].Handle), needle), strings.HasPrefix(strings.ToLower(out[j].Handle), needle)
			if pi != pj {
				return pi
			}
			return out[i].UserCount > out[j].UserCount
		})
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	writeJSON(w, r, map[string]any{"ok": true, "usergroups": out})
}

// ---- channel details: members, pins, bookmarks, info ----

func (a *App) hMembers(w http.ResponseWriter, r *http.Request) {
	ch := q(r, "channel")
	if ch == "" {
		badRequest(w, "channel required")
		return
	}
	us, next, err := a.C.ChannelMembers(r.Context(), ch, q(r, "marker"), qInt(r, "count", 100))
	if err != nil {
		writeErr(w, err)
		return
	}
	a.S.PutUsers(us)
	ids := make([]string, 0, len(us))
	for _, u := range us {
		ids = append(ids, u.ID)
	}
	a.subscribePresence(ids)
	writeJSON(w, r, map[string]any{"ok": true, "members": ids, "users": a.usersView(ids), "next": next})
}

func (a *App) hPins(w http.ResponseWriter, r *http.Request) {
	ch := q(r, "channel")
	var res struct {
		Items []struct {
			Type      string         `json:"type"`
			Created   int64          `json:"created"`
			CreatedBy string         `json:"created_by"`
			Message   *slack.Message `json:"message"`
		} `json:"items"`
	}
	if err := a.C.Call(r.Context(), "pins.list", url.Values{"channel": {ch}}, &res); err != nil {
		writeErr(w, err)
		return
	}
	type pin struct {
		Type      string       `json:"type"`
		Created   int64        `json:"created"`
		CreatedBy string       `json:"created_by"`
		Msg       *render.View `json:"msg,omitempty"`
	}
	var msgs []slack.Message
	for _, it := range res.Items {
		if it.Message != nil {
			msgs = append(msgs, *it.Message)
		}
	}
	a.ensureUsers(r.Context(), referencedUsers(msgs))
	out := make([]pin, 0, len(res.Items))
	ids := []string{}
	for _, it := range res.Items {
		out = append(out, pin{Type: it.Type, Created: it.Created, CreatedBy: it.CreatedBy, Msg: a.viewPtr(ch, it.Message)})
		ids = append(ids, it.CreatedBy)
	}
	ids = append(ids, referencedUsers(msgs)...)
	writeJSON(w, r, map[string]any{"ok": true, "items": out, "users": a.usersView(ids)})
}

func (a *App) hPin(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var req struct {
		Channel string `json:"channel"`
		TS      string `json:"ts"`
		Add     bool   `json:"add"`
	}
	if decode(r, &req) != nil || req.Channel == "" || req.TS == "" {
		badRequest(w, "channel and ts required")
		return
	}
	m := "pins.remove"
	if req.Add {
		m = "pins.add"
	}
	if err := a.C.Call(r.Context(), m, url.Values{"channel": {req.Channel}, "timestamp": {req.TS}}, nil); err != nil {
		var se *slack.Error
		if !(errors.As(err, &se) && (se.Code == "already_pinned" || se.Code == "no_pin")) {
			writeErr(w, err)
			return
		}
	}
	writeOK(w, r)
}

func (a *App) hBookmarks(w http.ResponseWriter, r *http.Request) {
	ch := q(r, "channel")
	var res struct {
		Bookmarks []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Link        string `json:"link"`
			Emoji       string `json:"emoji"`
			IconURL     string `json:"icon_url"`
			Type        string `json:"type"`
			EntityID    string `json:"entity_id"`
			DateCreated int64  `json:"date_created"`
			Rank        string `json:"rank"`
			ParentID    string `json:"parent_id"`
		} `json:"bookmarks"`
	}
	if err := a.C.Call(r.Context(), "bookmarks.list", url.Values{"channel_id": {ch}, "channel": {ch}, "include_folders": {"true"}}, &res); err != nil {
		writeErr(w, err)
		return
	}
	type bm struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Link     string `json:"link"`
		Emoji    string `json:"emoji,omitempty"`
		EmojiURL string `json:"emoji_url,omitempty"`
		Icon     string `json:"icon,omitempty"`
		Type     string `json:"type"`
		EntityID string `json:"entity_id,omitempty"`
		Created  int64  `json:"created"`
		Rank     string `json:"rank,omitempty"`
		ParentID string `json:"parent_id,omitempty"`
	}
	out := make([]bm, 0, len(res.Bookmarks))
	for _, b := range res.Bookmarks {
		v := bm{ID: b.ID, Title: b.Title, Link: render.SafeURL(b.Link), Emoji: b.Emoji, Icon: render.SafeImg(b.IconURL), Type: b.Type, EntityID: b.EntityID, Created: b.DateCreated, Rank: b.Rank, ParentID: b.ParentID}
		if b.Emoji != "" {
			v.EmojiURL, _, _ = a.S.Emoji(strings.Trim(b.Emoji, ":"))
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	writeJSON(w, r, map[string]any{"ok": true, "bookmarks": out})
}

func (a *App) hChannel(w http.ResponseWriter, r *http.Request) {
	ch := q(r, "channel")
	if ch == "" {
		badRequest(w, "channel required")
		return
	}
	c, err := a.C.ConversationInfo(r.Context(), ch)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, member := a.S.Conv(ch); member || c.IsMember || c.IsIM {
		a.S.PutConv(*c)
	}
	a.ensureUsers(r.Context(), []string{c.Creator, c.User})
	v := a.convView(c)
	info := map[string]any{"is_member": c.IsMember || c.IsIM, "is_org_shared": c.IsOrgShared, "is_ext_shared": c.IsExtShared, "last_read": c.LastRead}
	if c.Topic != nil {
		info["topic_set_by"], info["topic_set_at"] = c.Topic.Creator, c.Topic.LastSet
	}
	if c.Purpose != nil {
		v.PurposeHTML = render.Mrkdwn(a.S, c.Purpose.Value)
		info["purpose_set_by"], info["purpose_set_at"] = c.Purpose.Creator, c.Purpose.LastSet
	}
	writeJSON(w, r, map[string]any{"ok": true, "conv": v, "info": info, "users": a.usersView([]string{c.Creator, c.User})})
}

// ---- activity ----

var activityTypes = map[string]string{
	"all":       "at_user,at_user_group,at_channel,at_everyone,keyword,thread_v2,message_reaction,internal_channel_invite,external_channel_invite,bot_dm_bundle,list_record_assigned,list_user_mentioned",
	"mentions":  "at_user,at_user_group,at_channel,at_everyone,keyword",
	"threads":   "thread_v2",
	"reactions": "message_reaction",
	"invites":   "internal_channel_invite,external_channel_invite",
	"apps":      "bot_dm_bundle",
}

type msgRef struct {
	TS           string `json:"ts"`
	Channel      string `json:"channel"`
	ThreadTS     string `json:"thread_ts"`
	AuthorUserID string `json:"author_user_id"`
	User         string `json:"user"`
	IsBroadcast  bool   `json:"is_broadcast"`
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
		Inviter    string `json:"inviter"`
		BundleInfo *struct {
			UnreadCount int `json:"unread_count"`
			Payload     struct {
				Message     *msgRef `json:"message"`
				ThreadEntry *struct {
					ChannelID      string `json:"channel_id"`
					LatestTS       string `json:"latest_ts"`
					ThreadTS       string `json:"thread_ts"`
					UnreadMsgCount int    `json:"unread_msg_count"`
				} `json:"thread_entry"`
			} `json:"payload"`
		} `json:"bundle_info"`
	} `json:"item"`
}

// ActivityItem is one row of the Activity view.
type ActivityItem struct {
	Key      string       `json:"key"`
	Type     string       `json:"type"`
	FeedTS   string       `json:"feed_ts"`
	Unread   bool         `json:"unread"`
	Channel  string       `json:"channel,omitempty"`
	TS       string       `json:"ts,omitempty"`
	ThreadTS string       `json:"thread_ts,omitempty"`
	Author   string       `json:"author,omitempty"`
	Msg      *render.View `json:"msg,omitempty"`
	Root     *render.View `json:"root,omitempty"` // thread parent, for thread items
	Reaction *struct {
		User string `json:"user"`
		Name string `json:"name"`
		URL  string `json:"url,omitempty"`
	} `json:"reaction,omitempty"`
	Count int `json:"count,omitempty"` // unread replies / bundled messages
}

func (a *App) hActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t0 := time.Now()
	types := q(r, "types")
	if types == "" {
		types = activityTypes[q(r, "view")]
	}
	if types == "" {
		types = activityTypes["all"]
	}
	p := url.Values{"limit": {strconv.Itoa(qInt(r, "limit", 30))}, "types": {types}, "mode": {"chrono_v1"},
		"archive_only": {"false"}, "priority_only": {"false"}, "is_activity_inbox": {"true"},
		"unread_only": {strconv.FormatBool(q(r, "unread") == "1")}}
	if c := q(r, "cursor"); c != "" {
		p.Set("cursor", c)
	}
	var res struct {
		Items []activityRaw `json:"items"`
		Meta  struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := a.C.Call(ctx, "activity.feed", p, &res); err != nil {
		writeErr(w, err)
		return
	}
	items := make([]ActivityItem, 0, len(res.Items))
	want := map[string][]string{}
	for _, raw := range res.Items {
		it := ActivityItem{Key: raw.Key, Type: raw.Item.Type, FeedTS: raw.FeedTS, Unread: raw.IsUnread, Channel: raw.Item.Channel}
		ref := raw.Item.Message
		if raw.Item.BundleInfo != nil {
			it.Count = raw.Item.BundleInfo.UnreadCount
			if ref == nil {
				ref = raw.Item.BundleInfo.Payload.Message
			}
			if te := raw.Item.BundleInfo.Payload.ThreadEntry; te != nil {
				ref = &msgRef{Channel: te.ChannelID, TS: te.LatestTS, ThreadTS: te.ThreadTS}
				if te.UnreadMsgCount > 0 {
					it.Count = te.UnreadMsgCount
				}
			}
		}
		if ref != nil {
			it.Channel, it.TS, it.ThreadTS = ref.Channel, ref.TS, ref.ThreadTS
			it.Author = firstNonEmpty(ref.AuthorUserID, ref.User)
			if it.Channel != "" && it.TS != "" {
				want[it.Channel] = append(want[it.Channel], it.TS)
			}
			if it.ThreadTS != "" && it.ThreadTS != it.TS {
				want[it.Channel] = append(want[it.Channel], it.ThreadTS)
			}
		}
		if rx := raw.Item.Reaction; rx != nil {
			it.Reaction = &struct {
				User string `json:"user"`
				Name string `json:"name"`
				URL  string `json:"url,omitempty"`
			}{User: rx.User, Name: rx.Name}
			it.Reaction.URL, _, _ = a.S.Emoji(rx.Name)
		}
		items = append(items, it)
	}
	got := a.hydrate(ctx, want)
	ids := []string{}
	chans := map[string]bool{}
	for i := range items {
		it := &items[i]
		chans[it.Channel] = true
		ids = append(ids, it.Author)
		if it.Reaction != nil {
			ids = append(ids, it.Reaction.User)
		}
		if m, ok := got[it.Channel+"/"+it.TS]; ok {
			it.Msg = a.viewPtr(it.Channel, &m)
			ids = append(ids, referencedUsers([]slack.Message{m})...)
		}
		if it.ThreadTS != "" && it.ThreadTS != it.TS {
			if m, ok := got[it.Channel+"/"+it.ThreadTS]; ok {
				it.Root = a.viewPtr(it.Channel, &m)
				ids = append(ids, referencedUsers([]slack.Message{m})...)
			}
		}
	}
	a.ensureUsers(ctx, ids)
	log.Printf("activity: %d items in %s", len(items), time.Since(t0).Round(time.Millisecond))
	writeJSON(w, r, map[string]any{"ok": true, "items": items, "next": res.Meta.NextCursor, "users": a.usersView(ids), "convs": a.convRefs(chans)})
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// ---- threads view ----

func (a *App) hThreads(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := url.Values{"limit": {strconv.Itoa(qInt(r, "limit", 10))}, "fetch_threads_state": {"true"}}
	if c := q(r, "cursor"); c != "" {
		p.Set("max_ts", c)
	}
	var res struct {
		Threads []struct {
			Root          slack.Message   `json:"root_msg"`
			LatestReplies []slack.Message `json:"latest_replies"`
			UnreadReplies []slack.Message `json:"unread_replies"`
		} `json:"threads"`
		HasMore            bool   `json:"has_more"`
		MaxTS              string `json:"max_ts"`
		TotalUnreadReplies int    `json:"total_unread_replies"`
		NewThreadsCount    int    `json:"new_threads_count"`
		State              struct {
			HasUnreads   bool `json:"has_unreads"`
			MentionCount int  `json:"mention_count"`
		} `json:"threads_state"`
	}
	if err := a.C.Call(ctx, "subscriptions.thread.getView", p, &res); err != nil {
		writeErr(w, err)
		return
	}
	type threadView struct {
		Channel     string        `json:"channel"`
		Root        render.View   `json:"root"`
		Replies     []render.View `json:"replies"`
		UnreadCount int           `json:"unread_count"`
		LastRead    string        `json:"last_read,omitempty"`
	}
	var all []slack.Message
	for _, t := range res.Threads {
		all = append(all, t.Root)
		all = append(all, t.LatestReplies...)
		all = append(all, t.UnreadReplies...)
	}
	a.ensureUsers(ctx, referencedUsers(all))
	out := make([]threadView, 0, len(res.Threads))
	for _, t := range res.Threads {
		ch := t.Root.Channel
		replies := t.LatestReplies
		if len(t.UnreadReplies) > len(replies) {
			replies = t.UnreadReplies
		}
		store.SortAsc(replies)
		tv := threadView{Channel: ch, Root: render.Message(a.S, ch, &t.Root), UnreadCount: len(t.UnreadReplies)}
		for i := range replies {
			tv.Replies = append(tv.Replies, render.Message(a.S, firstNonEmpty(replies[i].Channel, ch), &replies[i]))
		}
		out = append(out, tv)
	}
	a.S.Lock()
	a.S.ThreadsUnread, a.S.ThreadMentions = res.State.HasUnreads, res.State.MentionCount
	a.S.Unlock()
	chans := map[string]bool{}
	for _, t := range out {
		chans[t.Channel] = true
	}
	writeJSON(w, r, map[string]any{
		"ok": true, "threads": out, "has_more": res.HasMore, "next": res.MaxTS, "total_unread_replies": res.TotalUnreadReplies,
		"threads_state": a.threadsView(), "users": a.usersView(referencedUsers(all)), "convs": a.convRefs(chans),
	})
}

func (a *App) hThreadSubscribe(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var req struct {
		Channel  string `json:"channel"`
		ThreadTS string `json:"thread_ts"`
		Add      bool   `json:"add"`
		Read     string `json:"read_ts"` // optional: mark the thread read up to this reply
	}
	if decode(r, &req) != nil || req.Channel == "" || req.ThreadTS == "" {
		badRequest(w, "channel and thread_ts required")
		return
	}
	var err error
	switch {
	case req.Read != "":
		err = a.C.Call(r.Context(), "subscriptions.thread.mark", url.Values{"channel": {req.Channel}, "thread_ts": {req.ThreadTS}, "ts": {req.Read}, "read": {"1"}}, nil)
	case req.Add:
		err = a.C.Call(r.Context(), "subscriptions.thread.add", url.Values{"channel": {req.Channel}, "thread_ts": {req.ThreadTS}}, nil)
	default:
		err = a.C.Call(r.Context(), "subscriptions.thread.remove", url.Values{"channel": {req.Channel}, "thread_ts": {req.ThreadTS}}, nil)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	a.refreshCountsSoon()
	writeOK(w, r)
}

// ---- saved / later ----

func (a *App) hSaved(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := url.Values{"limit": {strconv.Itoa(qInt(r, "limit", 50))}, "include_tombstones": {"true"}}
	switch f := q(r, "filter"); f {
	case "", "saved", "in_progress":
		p.Set("filter", "saved")
	default:
		p.Set("filter", f) // completed | archived
	}
	if c := q(r, "cursor"); c != "" {
		p.Set("cursor", c)
	}
	var res struct {
		Items []struct {
			ItemID        string `json:"item_id"`
			ItemType      string `json:"item_type"`
			TS            string `json:"ts"`
			State         string `json:"state"`
			TodoState     string `json:"todo_state"`
			DateCreated   int64  `json:"date_created"`
			DateDue       int64  `json:"date_due"`
			DateCompleted int64  `json:"date_completed"`
			IsArchived    bool   `json:"is_archived"`
		} `json:"saved_items"`
		Counts json.RawMessage `json:"counts"`
		Meta   struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := a.C.Call(ctx, "saved.list", p, &res); err != nil {
		writeErr(w, err)
		return
	}
	want := map[string][]string{}
	for _, it := range res.Items {
		if it.ItemType == "message" && it.TS != "" {
			want[it.ItemID] = append(want[it.ItemID], it.TS)
		}
	}
	got := a.hydrate(ctx, want)
	type savedView struct {
		ItemID        string       `json:"item_id"`
		ItemType      string       `json:"item_type"`
		Channel       string       `json:"channel,omitempty"`
		TS            string       `json:"ts,omitempty"`
		State         string       `json:"state"`
		DateCreated   int64        `json:"date_created"`
		DateDue       int64        `json:"date_due,omitempty"`
		DateCompleted int64        `json:"date_completed,omitempty"`
		Archived      bool         `json:"archived,omitempty"`
		Msg           *render.View `json:"msg,omitempty"`
	}
	out := make([]savedView, 0, len(res.Items))
	ids := []string{}
	chans := map[string]bool{}
	for _, it := range res.Items {
		v := savedView{ItemID: it.ItemID, ItemType: it.ItemType, TS: it.TS, State: firstNonEmpty(it.TodoState, it.State),
			DateCreated: it.DateCreated, DateDue: it.DateDue, DateCompleted: it.DateCompleted, Archived: it.IsArchived}
		if it.ItemType == "message" {
			v.Channel = it.ItemID
			chans[it.ItemID] = true
			if m, ok := got[it.ItemID+"/"+it.TS]; ok {
				v.Msg = a.viewPtr(it.ItemID, &m)
				ids = append(ids, referencedUsers([]slack.Message{m})...)
			}
		}
		out = append(out, v)
	}
	writeJSON(w, r, map[string]any{"ok": true, "items": out, "counts": res.Counts, "next": res.Meta.NextCursor, "users": a.usersView(ids), "convs": a.convRefs(chans)})
}

func (a *App) hSave(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var req struct {
		Channel  string `json:"channel"`
		TS       string `json:"ts"`
		Add      *bool  `json:"add"`
		Complete *bool  `json:"completed"`
		Archive  *bool  `json:"archived"`
		DateDue  *int64 `json:"date_due"`
	}
	if decode(r, &req) != nil || req.Channel == "" || req.TS == "" {
		badRequest(w, "channel and ts required")
		return
	}
	p := url.Values{"item_type": {"message"}, "item_id": {req.Channel}, "ts": {req.TS}}
	method := "saved.update"
	switch {
	case req.Add != nil && *req.Add:
		method = "saved.add"
		if req.DateDue != nil {
			p.Set("date_due", strconv.FormatInt(*req.DateDue, 10))
		}
	case req.Add != nil && !*req.Add:
		method = "saved.delete"
	default:
		if req.Complete != nil {
			p.Set("mark", map[bool]string{true: "completed", false: "uncompleted"}[*req.Complete])
		}
		if req.Archive != nil {
			p.Set("mark", map[bool]string{true: "archived", false: "unarchived"}[*req.Archive])
		}
		if req.DateDue != nil {
			p.Set("date_due", strconv.FormatInt(*req.DateDue, 10))
		}
	}
	var res json.RawMessage
	if err := a.C.Call(r.Context(), method, p, &res); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, r)
}

// ---- drafts ----

func (a *App) hDrafts(w http.ResponseWriter, r *http.Request) {
	var res struct {
		Drafts []struct {
			ID           string          `json:"id"`
			ClientMsgID  string          `json:"client_msg_id"`
			Blocks       json.RawMessage `json:"blocks"`
			Destinations []struct {
				ChannelID string   `json:"channel_id"`
				UserIDs   []string `json:"user_ids"`
				ThreadTS  string   `json:"thread_ts"`
				Broadcast bool     `json:"broadcast"`
			} `json:"destinations"`
			DateCreated   int64    `json:"date_created"`
			DateScheduled int64    `json:"date_scheduled"`
			LastUpdatedTS string   `json:"last_updated_ts"`
			IsSent        bool     `json:"is_sent"`
			FileIDs       []string `json:"file_ids"`
		} `json:"drafts"`
	}
	p := url.Values{"is_active": {"true"}, "limit": {strconv.Itoa(qInt(r, "limit", 50))}}
	if err := a.C.Call(r.Context(), "drafts.list", p, &res); err != nil {
		writeErr(w, err)
		return
	}
	type draftView struct {
		ID            string   `json:"id"`
		Channel       string   `json:"channel,omitempty"`
		Users         []string `json:"users,omitempty"`
		ThreadTS      string   `json:"thread_ts,omitempty"`
		Broadcast     bool     `json:"broadcast,omitempty"`
		HTML          string   `json:"html"`
		Blocks        any      `json:"blocks,omitempty"`
		DateCreated   int64    `json:"date_created"`
		DateScheduled int64    `json:"date_scheduled,omitempty"`
		LastUpdated   string   `json:"last_updated,omitempty"`
		Files         int      `json:"files,omitempty"`
	}
	out := make([]draftView, 0, len(res.Drafts))
	ids := []string{}
	chans := map[string]bool{}
	for _, d := range res.Drafts {
		if d.IsSent {
			continue
		}
		v := draftView{ID: d.ID, HTML: render.Blocks(a.S, d.Blocks), Blocks: d.Blocks, DateCreated: d.DateCreated, DateScheduled: d.DateScheduled, LastUpdated: d.LastUpdatedTS, Files: len(d.FileIDs)}
		if len(d.Destinations) > 0 {
			dst := d.Destinations[0]
			v.Channel, v.Users, v.ThreadTS, v.Broadcast = dst.ChannelID, dst.UserIDs, dst.ThreadTS, dst.Broadcast
			ids = append(ids, dst.UserIDs...)
			chans[dst.ChannelID] = true
		}
		out = append(out, v)
	}
	a.ensureUsers(r.Context(), ids)
	writeJSON(w, r, map[string]any{"ok": true, "drafts": out, "users": a.usersView(ids), "convs": a.convRefs(chans)})
}

// ---- DMs tab ----

func (a *App) hDMs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t0 := time.Now()
	p := url.Values{"count": {strconv.Itoa(qInt(r, "count", 50))}, "include_closed": {"true"}, "include_channel": {"true"}, "exclude_bots": {strconv.FormatBool(q(r, "bots") == "0")}}
	if c := q(r, "cursor"); c != "" {
		p.Set("cursor", c)
	}
	type dmRaw struct {
		ID      string              `json:"id"`
		Message *slack.Message      `json:"message"`
		Channel *slack.Conversation `json:"channel"`
		Latest  string              `json:"latest"`
	}
	var res struct {
		IMs   []dmRaw `json:"ims"`
		MPIMs []dmRaw `json:"mpims"`
		Meta  struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := a.C.Call(ctx, "client.dms", p, &res); err != nil {
		writeErr(w, err)
		return
	}
	all := append(append([]dmRaw(nil), res.IMs...), res.MPIMs...)
	var msgs []slack.Message
	ids := []string{}
	for _, d := range all {
		if d.Message != nil {
			msgs = append(msgs, *d.Message)
		}
		if d.Channel != nil {
			ids = append(ids, d.Channel.User)
			ids = append(ids, d.Channel.Members...)
		}
	}
	ids = append(ids, referencedUsers(msgs)...)
	a.ensureUsers(ctx, ids)
	type dmView struct {
		Channel string       `json:"channel"`
		Conv    ConvView     `json:"conv"`
		Latest  *render.View `json:"latest,omitempty"`
		Count   CountView    `json:"count"`
	}
	out := make([]dmView, 0, len(all))
	for _, d := range all {
		id := d.ID
		var c *slack.Conversation
		if known, ok := a.S.Conv(id); ok {
			c = known
		} else if d.Channel != nil {
			// closed DMs aren't in the sidebar: describe them without adding them
			if d.Channel.ID == "" {
				d.Channel.ID = id
			}
			c = d.Channel
		} else {
			continue
		}
		v := dmView{Channel: id, Conv: a.convView(c), Latest: a.viewPtr(id, d.Message), Count: a.countOf(id)}
		if v.Count.Latest == "" && d.Message != nil {
			v.Count.Latest = d.Message.TS
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := "", ""
		if out[i].Latest != nil {
			li = out[i].Latest.TS
		}
		if out[j].Latest != nil {
			lj = out[j].Latest.TS
		}
		return store.TSLess(lj, li)
	})
	a.subscribePresence(ids)
	log.Printf("dms: %d conversations in %s", len(out), time.Since(t0).Round(time.Millisecond))
	writeJSON(w, r, map[string]any{"ok": true, "dms": out, "next": res.Meta.NextCursor, "users": a.usersView(ids)})
}

// ---- presence ----

// subscribePresence adds ids to the realtime presence subscription.
func (a *App) subscribePresence(ids []string) {
	if len(ids) == 0 {
		return
	}
	a.mu.Lock()
	newIDs := false
	for _, id := range ids {
		if _, ok := a.presSeen[id]; !ok {
			newIDs = true
			break
		}
	}
	a.mu.Unlock()
	all := a.presenceIDs(ids)
	if newIDs && a.RTM.Connected() {
		_ = a.RTM.SubscribePresence(all)
		_ = a.RTM.SubscribeUsers(all)
	}
}

// presenceArrived wakes requests waiting in hPresence.
func (a *App) presenceArrived() {
	a.mu.Lock()
	ws := a.presWaiters
	a.presWaiters = nil
	a.mu.Unlock()
	for _, c := range ws {
		close(c)
	}
}

func (a *App) presenceMap(ids []string) (map[string]string, []string) {
	out := map[string]string{}
	var missing []string
	a.S.RLock()
	for _, id := range ids {
		if p, ok := a.S.Presence[id]; ok {
			out[id] = p
		} else if id != "" {
			missing = append(missing, id)
		}
	}
	a.S.RUnlock()
	return out, missing
}

func (a *App) hPresence(w http.ResponseWriter, r *http.Request) {
	ids := splitIDs(q(r, "ids"))
	out, missing := a.presenceMap(ids)
	if len(missing) > 0 {
		a.subscribePresence(missing)
		// The gateway answers a subscription with presence_change events; wait briefly.
		deadline := time.After(1500 * time.Millisecond)
		for len(missing) > 0 {
			c := make(chan struct{})
			a.mu.Lock()
			a.presWaiters = append(a.presWaiters, c)
			a.mu.Unlock()
			select {
			case <-c:
			case <-deadline:
				missing = nil
				continue
			case <-r.Context().Done():
				return
			}
			out, missing = a.presenceMap(ids)
		}
	}
	writeJSON(w, r, map[string]any{"ok": true, "presence": out})
}

func splitIDs(s string) []string {
	var out []string
	for _, id := range strings.Split(s, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func (a *App) hPresenceSub(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if decode(r, &req) != nil {
		badRequest(w, "ids required")
		return
	}
	a.subscribePresence(req.IDs)
	out, _ := a.presenceMap(req.IDs)
	writeJSON(w, r, map[string]any{"ok": true, "presence": out})
}

func (a *App) hPresenceSet(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var req struct {
		Presence string `json:"presence"` // auto | away
	}
	if decode(r, &req) != nil || (req.Presence != "auto" && req.Presence != "away") {
		badRequest(w, "presence must be auto or away")
		return
	}
	if err := a.C.Call(r.Context(), "users.setPresence", url.Values{"presence": {req.Presence}}, nil); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, r)
}

// ---- status & DND ----

func (a *App) hStatus(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var req struct {
		Text       string `json:"text"`
		Emoji      string `json:"emoji"`
		Expiration int64  `json:"expiration"`
	}
	if decode(r, &req) != nil {
		badRequest(w, "bad body")
		return
	}
	if req.Emoji != "" && !strings.HasPrefix(req.Emoji, ":") {
		req.Emoji = ":" + req.Emoji + ":"
	}
	prof, _ := json.Marshal(map[string]any{"status_text": req.Text, "status_emoji": req.Emoji, "status_expiration": req.Expiration})
	if err := a.C.Call(r.Context(), "users.profile.set", url.Values{"profile": {string(prof)}}, nil); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, r)
}

func (a *App) hDND(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method == http.MethodPost {
		var req struct {
			Minutes int `json:"minutes"` // 0 ends the snooze
		}
		if decode(r, &req) != nil {
			badRequest(w, "bad body")
			return
		}
		var err error
		if req.Minutes > 0 {
			err = a.C.Call(ctx, "dnd.setSnooze", url.Values{"num_minutes": {strconv.Itoa(req.Minutes)}}, nil)
		} else {
			err = a.C.Call(ctx, "dnd.endSnooze", nil, nil)
		}
		if err != nil {
			writeErr(w, err)
			return
		}
	}
	var info json.RawMessage
	if err := a.C.Call(ctx, "dnd.info", url.Values{"user": {a.S.SelfID()}}, &info); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, r, map[string]any{"ok": true, "dnd": info})
}

// ---- slash commands ----

func (a *App) commands(ctx context.Context) ([]slack.Command, error) {
	a.mu.Lock()
	if a.cmdCache != nil && time.Since(a.cmdAt) < 30*time.Minute {
		c := a.cmdCache
		a.mu.Unlock()
		return c, nil
	}
	a.mu.Unlock()
	cmds, err := a.C.Commands(ctx)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.cmdCache, a.cmdAt = cmds, time.Now()
	a.mu.Unlock()
	return cmds, nil
}

func (a *App) hCommands(w http.ResponseWriter, r *http.Request) {
	cmds, err := a.commands(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	needle := strings.TrimPrefix(strings.ToLower(q(r, "q")), "/")
	out := make([]slack.Command, 0, len(cmds))
	for _, c := range cmds {
		if needle == "" || strings.Contains(strings.ToLower(c.Name), needle) {
			out = append(out, c)
		}
	}
	writeJSON(w, r, map[string]any{"ok": true, "commands": out})
}

func (a *App) hCommand(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var req struct {
		Channel  string `json:"channel"`
		Command  string `json:"command"`
		Text     string `json:"text"`
		ThreadTS string `json:"thread_ts"`
	}
	if decode(r, &req) != nil || req.Channel == "" || req.Command == "" {
		badRequest(w, "channel and command required")
		return
	}
	if !strings.HasPrefix(req.Command, "/") {
		req.Command = "/" + req.Command
	}
	ctx := r.Context()
	// A few core commands are implemented by the client itself.
	switch req.Command {
	case "/shrug":
		text := strings.TrimSpace(req.Text + ` ¯\_(ツ)_/¯`)
		m, err := a.C.Post(ctx, slack.PostOpts{Channel: req.Channel, Text: text, ThreadTS: req.ThreadTS})
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, r, map[string]any{"ok": true, "msg": render.Message(a.S, req.Channel, m)})
		return
	case "/me":
		if err := a.C.Call(ctx, "chat.meMessage", url.Values{"channel": {req.Channel}, "text": {req.Text}}, nil); err != nil {
			writeErr(w, err)
			return
		}
		writeOK(w, r)
		return
	case "/topic":
		if err := a.C.Call(ctx, "conversations.setTopic", url.Values{"channel": {req.Channel}, "topic": {req.Text}}, nil); err != nil {
			writeErr(w, err)
			return
		}
		writeOK(w, r)
		return
	}
	p := url.Values{"channel": {req.Channel}, "command": {req.Command}, "text": {req.Text}, "disp": {req.Command}}
	if req.ThreadTS != "" {
		p.Set("thread_ts", req.ThreadTS)
	}
	var res json.RawMessage
	if err := a.C.Call(ctx, "chat.command", p, &res); err != nil {
		writeErr(w, err)
		return
	}
	var out struct {
		Response string `json:"response"`
	}
	_ = json.Unmarshal(res, &out)
	writeJSON(w, r, map[string]any{"ok": true, "response": out.Response, "response_html": render.Mrkdwn(a.S, out.Response)})
}

// ---- membership & DMs ----

func (a *App) hJoin(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var req struct {
		Channel string `json:"channel"`
	}
	if decode(r, &req) != nil || req.Channel == "" {
		badRequest(w, "channel required")
		return
	}
	var res struct {
		Channel slack.Conversation `json:"channel"`
	}
	if err := a.C.Call(r.Context(), "conversations.join", url.Values{"channel": {req.Channel}}, &res); err != nil {
		writeErr(w, err)
		return
	}
	c := res.Channel
	if full, err := a.C.ConversationInfo(r.Context(), req.Channel); err == nil {
		c = *full
	}
	a.S.PutConv(c)
	v := a.convView(&c)
	a.Hub.Broadcast(map[string]any{"type": "conv", "conv": v})
	writeJSON(w, r, map[string]any{"ok": true, "conv": v})
}

func (a *App) hLeave(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var req struct {
		Channel string `json:"channel"`
	}
	if decode(r, &req) != nil || req.Channel == "" {
		badRequest(w, "channel required")
		return
	}
	method := "conversations.leave"
	if strings.HasPrefix(req.Channel, "D") {
		method = "conversations.close"
	}
	if err := a.C.Call(r.Context(), method, url.Values{"channel": {req.Channel}}, nil); err != nil {
		writeErr(w, err)
		return
	}
	a.S.Lock()
	delete(a.S.Convs, req.Channel)
	a.S.Unlock()
	a.Hub.Broadcast(map[string]any{"type": "conv_removed", "channel": req.Channel})
	writeOK(w, r)
}

func (a *App) hOpen(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	var req struct {
		Users []string `json:"users"`
	}
	if decode(r, &req) != nil || len(req.Users) == 0 {
		badRequest(w, "users required")
		return
	}
	var res struct {
		Channel slack.Conversation `json:"channel"`
	}
	ctx := r.Context()
	if err := a.C.Call(ctx, "conversations.open", url.Values{"users": {strings.Join(req.Users, ",")}, "return_im": {"true"}}, &res); err != nil {
		writeErr(w, err)
		return
	}
	c := res.Channel
	if c.ID != "" && (c.User == "" && !c.IsMPIM) {
		if full, err := a.C.ConversationInfo(ctx, c.ID); err == nil {
			c = *full
		}
	}
	if len(req.Users) == 1 && c.User == "" {
		c.IsIM, c.User = true, req.Users[0]
	}
	a.ensureUsers(ctx, append(req.Users, c.User))
	a.S.PutConv(c)
	v := a.convView(&c)
	a.Hub.Broadcast(map[string]any{"type": "conv", "conv": v})
	writeJSON(w, r, map[string]any{"ok": true, "channel": c.ID, "conv": v, "users": a.usersView(req.Users)})
}

// ---- uploads ----

// maxUpload bounds a single upload (it is held in memory; uploads are never
// spooled to disk).
const maxUpload = 512 << 20

func (a *App) hUpload(w http.ResponseWriter, r *http.Request) {
	if !needPost(w, r) {
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		badRequest(w, "multipart body required")
		return
	}
	var files []slack.UploadFile
	fields := map[string]string{}
	total := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			badRequest(w, "bad multipart body")
			return
		}
		data, err := io.ReadAll(io.LimitReader(part, int64(maxUpload-total)+1))
		if err != nil {
			badRequest(w, "read failed")
			return
		}
		total += len(data)
		if total > maxUpload {
			badRequest(w, "file too large")
			return
		}
		if part.FileName() != "" {
			files = append(files, slack.UploadFile{Name: part.FileName(), Data: data})
		} else {
			fields[part.FormName()] = string(data)
		}
	}
	if len(files) == 0 {
		badRequest(w, "file required")
		return
	}
	if t := fields["title"]; t != "" && len(files) == 1 {
		files[0].Title = t
	}
	comment := firstNonEmpty(fields["initial_comment"], fields["comment"], fields["text"])
	t0 := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	out, err := a.C.Upload(ctx, fields["channel"], fields["thread_ts"], comment, files)
	if err != nil {
		writeErr(w, err)
		return
	}
	log.Printf("upload: %d file(s), %d bytes in %s", len(files), total, time.Since(t0).Round(time.Millisecond))
	writeJSON(w, r, map[string]any{"ok": true, "files": out})
}
