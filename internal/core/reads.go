package core

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// ---- history & threads ----

// History returns the newest page of a conversation (before == "") from
// memory when cached, else one conversations.history call; with before set
// it loads the next older page. Opening a cached channel takes ~1 ms.
func (a *App) History(ctx context.Context, ch, before string) (Page, error) {
	t0 := time.Now()
	source := "cache"
	defer func() { log.Printf("history %s: %s (%s)", ch, time.Since(t0).Round(time.Millisecond), source) }()
	// Fetch the member count (people, like Slack's header) alongside the history.
	var countDone chan struct{}
	a.mu.Lock()
	_, haveCount := a.memberN[ch]
	a.mu.Unlock()
	if c, ok := a.S.Conv(ch); ok && !haveCount && !c.IsIM && !c.IsMPIM && before == "" {
		countDone = make(chan struct{})
		go func() {
			defer close(countDone)
			a.loadMemberCount(ctx, ch)
		}()
	}
	// A conversation outside the sidebar (closed DM, channel from a link or
	// search): resolve it alongside so the header shows its name.
	var convDone chan struct{}
	if _, ok := a.Conv(ch); !ok && before == "" {
		convDone = make(chan struct{})
		go func() {
			defer close(convDone)
			a.ResolveConv(ctx, ch)
		}()
	}
	// Wait for the side fetches on every return path, errors included: a
	// History that returned must have no requests left in flight (after an
	// auth failure, one could otherwise reach Slack after sign-out).
	waitSide := func() {
		if countDone != nil {
			<-countDone
			countDone = nil
		}
		if convDone != nil {
			<-convDone
			convDone = nil
		}
	}
	defer waitSide()
	var msgs []slack.Message
	var hasMore bool
	if before == "" {
		t := a.S.Timeline(ch)
		if t == nil {
			source = "fetch"
			if err := a.loadLatest(ctx, ch); err != nil {
				return Page{}, err
			}
			t = a.S.Timeline(ch)
		}
		if t != nil {
			msgs, hasMore = t.Msgs, t.HasMore
		}
	} else {
		source = "older"
		p, err := a.C.History(ctx, ch, slack.HistoryOpts{Latest: before, Limit: 100})
		if err != nil {
			return Page{}, err
		}
		a.EnsureUsers(ctx, referencedUsers(p.Messages))
		a.S.PrependOlder(ch, p.Messages, p.HasMore)
		msgs, hasMore = p.Messages, p.HasMore
		store.SortAsc(msgs)
	}
	page := Page{Messages: a.timeline(ch, msgs), HasMore: hasMore}
	waitSide()
	if c, ok := a.Conv(ch); ok {
		page.Conv = &c
	}
	e, _ := a.S.Count(ch)
	page.LastRead = e.LastRead
	a.SubscribePresence(referencedUsers(msgs))
	return page, nil
}

// Refresh refetches the newest page of ch (replacing the cached one).
func (a *App) Refresh(ctx context.Context, ch string) (Page, error) {
	if err := a.loadLatest(ctx, ch); err != nil {
		return Page{}, err
	}
	return a.History(ctx, ch, "")
}

// Replies returns a thread, parent first. Cached threads are live-updated
// from realtime events; fresh forces a refetch.
func (a *App) Replies(ctx context.Context, ch, ts string) ([]Message, error) {
	msgs, ok := a.S.Thread(ch, ts)
	if !ok {
		if err := a.fetchThread(ctx, ch, ts); err != nil {
			return nil, err
		}
		msgs, _ = a.S.Thread(ch, ts)
	}
	a.SubscribePresence(referencedUsers(msgs))
	return a.messages(ch, msgs), nil
}

// RefreshReplies refetches a thread (replacing the cached copy).
func (a *App) RefreshReplies(ctx context.Context, ch, ts string) ([]Message, error) {
	if err := a.fetchThread(ctx, ch, ts); err != nil {
		return nil, err
	}
	return a.Replies(ctx, ch, ts)
}

func (a *App) fetchThread(ctx context.Context, ch, ts string) error {
	return a.once("r:"+ch+"/"+ts, func() error {
		var all []slack.Message
		cursor := ""
		for {
			page, next, err := a.C.Replies(ctx, ch, ts, cursor)
			if err != nil {
				return err
			}
			all = append(all, page...)
			if next == "" {
				break
			}
			cursor = next
		}
		a.EnsureUsers(ctx, referencedUsers(all))
		a.S.SetThread(ch, ts, all)
		for i := range all {
			if needsFileInfo(&all[i]) {
				go a.fillFiles(ch, all[i].TS)
			}
		}
		return nil
	})
}

// hydrate fetches messages by (channel, ts): memory first, then one
// messages.list call for the rest.
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
	a.EnsureUsers(ctx, referencedUsers(all))
	return out
}

// ---- prefs ----

// Prefs returns the rendering-relevant user prefs.
func (a *App) Prefs() Prefs {
	a.S.RLock()
	defer a.S.RUnlock()
	return a.S.Prefs
}

// RefreshPrefs reloads prefs from Slack.
func (a *App) RefreshPrefs(ctx context.Context) (Prefs, error) {
	raw, err := a.C.Prefs(ctx)
	if err != nil {
		return a.Prefs(), err
	}
	a.S.SetPrefs(store.ParsePrefs(raw))
	return a.Prefs(), nil
}

// ---- search & directory ----

// SearchMessages runs a message search (search.messages, ~0.8 s).
func (a *App) SearchMessages(ctx context.Context, o SearchOpts) (SearchResult, error) {
	t0 := time.Now()
	res, err := a.C.SearchMessages(ctx, o.Query, o.Page, o.Count, o.Sort)
	if err != nil {
		return SearchResult{}, err
	}
	msgs := make([]slack.Message, 0, len(res.Matches))
	for _, m := range res.Matches {
		msgs = append(msgs, m.Message)
	}
	a.EnsureUsers(ctx, referencedUsers(msgs))
	out := SearchResult{Total: res.Total, Page: res.Page, Pages: res.Pages}
	chans := map[string]bool{}
	for i := range res.Matches {
		m := &res.Matches[i]
		ch := m.ChannelO.ID
		chans[ch] = true
		if m.ChannelO.Name != "" && !m.ChannelO.IsIM {
			a.S.PutName(ch, m.ChannelO.Name) // names of channels we are not in (metadata)
		}
		v := a.message(ch, &m.Message)
		v.Permalink = m.Permalink
		out.Messages = append(out.Messages, v)
	}
	out.Convs = a.convRefs(chans)
	log.Printf("search: %d results in %s", len(out.Messages), time.Since(t0).Round(time.Millisecond))
	return out, nil
}

// SearchUsers queries the org directory (edge users/search, ~80 ms): for
// @-autocomplete, the DM picker and people search. Deactivated users are
// left out. Results are cached in memory and subscribed for presence.
func (a *App) SearchUsers(ctx context.Context, query string, count int) ([]User, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	us, err := a.C.SearchUsers(ctx, query, count)
	if err != nil {
		return nil, err
	}
	a.S.PutUsers(us)
	out := make([]User, 0, len(us))
	ids := make([]string, 0, len(us))
	for i := range us {
		if us[i].Deleted {
			continue
		}
		out = append(out, a.userOf(&us[i]))
		ids = append(ids, us[i].ID)
	}
	a.SubscribePresence(ids)
	return out, nil
}

func (a *App) channelHits(hs []slack.ChannelHit) []ChannelHit {
	out := make([]ChannelHit, 0, len(hs))
	for _, h := range hs {
		if h.IsMPIM {
			continue
		}
		v := ChannelHit{ID: h.ID, Name: h.Name, Kind: "channel", IsMember: h.IsMember, IsArchived: h.IsArchived,
			NumMembers: h.NumMembers, Created: h.Created}
		if v.NumMembers == 0 {
			v.NumMembers = h.MemberCount
		}
		if h.IsPrivate {
			v.Kind = "private"
		}
		if _, ok := a.S.Conv(h.ID); ok {
			v.IsMember = true
		}
		if h.Purpose != nil {
			v.Purpose = h.Purpose.Value
		}
		if h.Topic != nil {
			v.Topic = h.Topic.Value
		}
		a.S.PutName(h.ID, h.Name)
		out = append(out, v)
	}
	return out
}

// SearchChannels finds channels by name (edge channels/search, ~90 ms).
func (a *App) SearchChannels(ctx context.Context, query string, count int) ([]ChannelHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	hs, err := a.C.SearchChannels(ctx, query, count)
	if err != nil {
		return nil, err
	}
	return a.channelHits(hs), nil
}

// BrowseChannels is the channel browser; query may be empty.
// sort: "" (relevance) | name | created | members.
func (a *App) BrowseChannels(ctx context.Context, query string, page int, sort string) (ChannelPage, error) {
	hs, total, pages, err := a.C.BrowseChannels(ctx, query, page, sort)
	if err != nil {
		return ChannelPage{}, err
	}
	if page <= 0 {
		page = 1
	}
	return ChannelPage{Channels: a.channelHits(hs), Total: total, Pages: pages, Page: page}, nil
}

// Usergroups lists usergroups matching query (prefix matches first).
func (a *App) Usergroups(ctx context.Context, query string, limit int) ([]Usergroup, error) {
	a.S.RLock()
	empty := len(a.S.Groups) == 0
	a.S.RUnlock()
	if empty {
		if err := a.loadUsergroups(ctx); err != nil {
			return nil, err
		}
	}
	needle := strings.ToLower(strings.TrimPrefix(query, "@"))
	a.S.RLock()
	out := make([]Usergroup, 0, len(a.S.Groups))
	for _, g := range a.S.Groups {
		if needle != "" && !strings.Contains(strings.ToLower(g.Handle), needle) && !strings.Contains(strings.ToLower(g.Name), needle) {
			continue
		}
		out = append(out, Usergroup{ID: g.ID, Handle: g.Handle, Name: g.Name, Description: g.Description, UserCount: g.UserCount})
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
	return out, nil
}

// ---- channel details ----

// Members pages a conversation's members (edge users/list). Pass the
// returned next marker back to continue; peopleOnly drops bots and apps.
func (a *App) Members(ctx context.Context, ch, marker string, peopleOnly bool) ([]User, string, error) {
	us, next, err := a.C.ChannelMembers(ctx, ch, marker, 100, peopleOnly)
	if err != nil {
		return nil, "", err
	}
	a.S.PutUsers(us)
	out := make([]User, 0, len(us))
	ids := make([]string, 0, len(us))
	for i := range us {
		out = append(out, a.userOf(&us[i]))
		ids = append(ids, us[i].ID)
	}
	a.SubscribePresence(ids)
	return out, next, nil
}

// Pins lists a conversation's pinned items.
func (a *App) Pins(ctx context.Context, ch string) ([]Pin, error) {
	var res struct {
		Items []struct {
			Type      string         `json:"type"`
			Created   int64          `json:"created"`
			CreatedBy string         `json:"created_by"`
			Message   *slack.Message `json:"message"`
		} `json:"items"`
	}
	if err := a.C.Call(ctx, "pins.list", url.Values{"channel": {ch}}, &res); err != nil {
		return nil, err
	}
	var msgs []slack.Message
	ids := []string{}
	for _, it := range res.Items {
		if it.Message != nil {
			msgs = append(msgs, *it.Message)
		}
		ids = append(ids, it.CreatedBy)
	}
	a.EnsureUsers(ctx, append(ids, referencedUsers(msgs)...))
	out := make([]Pin, 0, len(res.Items))
	for _, it := range res.Items {
		out = append(out, Pin{Type: it.Type, Created: it.Created, CreatedBy: it.CreatedBy, Msg: a.messagePtr(ch, it.Message)})
	}
	return out, nil
}

// Bookmarks lists the channel tab bar's bookmarks, in rank order.
func (a *App) Bookmarks(ctx context.Context, ch string) ([]Bookmark, error) {
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
	if err := a.C.Call(ctx, "bookmarks.list", url.Values{"channel_id": {ch}, "channel": {ch}, "include_folders": {"true"}}, &res); err != nil {
		return nil, err
	}
	out := make([]Bookmark, 0, len(res.Bookmarks))
	for _, b := range res.Bookmarks {
		v := Bookmark{ID: b.ID, Title: b.Title, Link: b.Link, Emoji: b.Emoji, Icon: b.IconURL, Type: b.Type,
			EntityID: b.EntityID, Created: b.DateCreated, Rank: b.Rank, ParentID: b.ParentID}
		if b.Emoji != "" {
			v.EmojiURL, _, _ = a.S.Emoji(strings.Trim(b.Emoji, ":"))
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	return out, nil
}

// ChannelInfo loads the channel-details view (conversations.info).
func (a *App) ChannelInfo(ctx context.Context, ch string) (ChannelDetails, error) {
	c, err := a.C.ConversationInfo(ctx, ch)
	if err != nil {
		return ChannelDetails{}, err
	}
	if _, member := a.S.Conv(ch); member {
		a.S.PutConv(*c)
	}
	a.EnsureUsers(ctx, []string{c.Creator, c.User})
	var mgrs []string
	a.loadMemberCount(ctx, ch)
	if ms, err := a.Managers(ctx, ch); err == nil {
		mgrs = ms
	}
	d := ChannelDetails{Conv: a.conv(c), Managers: mgrs, IsMember: c.IsMember || c.IsIM, IsOrgShared: c.IsOrgShared, IsExtShared: c.IsExtShared, LastRead: c.LastRead}
	if c.Topic != nil {
		d.TopicSetBy, d.TopicSetAt = c.Topic.Creator, c.Topic.LastSet
	}
	if c.Purpose != nil {
		d.PurposeSetBy, d.PurposeSetAt = c.Purpose.Creator, c.Purpose.LastSet
	}
	return d, nil
}

// ---- activity ----

var activityTypes = map[string]string{
	// the web client's Activity inbox list (2026-11)
	"all":       "at_user,at_user_group,at_channel,at_everyone,keyword,list_record_assigned,list_user_mentioned,list_todo_notification,list_approval_request,list_approval_reviewed,unjoined_channel_mention,thread_v2,message_reaction,bot_dm_bundle,dm,prejoin_dm_welcome_party_alert,internal_channel_invite,external_channel_invite,external_dm_invite,quietly_added_to_channel,channel,saved_reminder,list_record_edited",
	"dms":       "dm",
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
					ThreadTS       string `json:"thread_ts"`
					UnreadMsgCount int    `json:"unread_msg_count"`
				} `json:"thread_entry"`
			} `json:"payload"`
		} `json:"bundle_info"`
	} `json:"item"`
}

// Activity loads the Activity view (activity.feed, hydrated with
// messages.list in one extra round trip).
func (a *App) Activity(ctx context.Context, o ActivityOpts) (ActivityPage, error) {
	t0 := time.Now()
	types := o.Types
	if types == "" {
		types = activityTypes[o.View]
	}
	if types == "" {
		types = activityTypes["all"]
	}
	limit := o.Limit
	if limit <= 0 {
		limit = 30
	}
	p := url.Values{"limit": {strconv.Itoa(limit)}, "types": {types}, "mode": {"chrono_v1"},
		"archive_only": {strconv.FormatBool(o.Archived)}, "priority_only": {"false"}, "is_activity_inbox": {"true"},
		"unread_only": {strconv.FormatBool(o.Unread)}}
	if o.Cursor != "" {
		p.Set("cursor", o.Cursor)
	}
	var res struct {
		Items []activityRaw `json:"items"`
		Meta  struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := a.C.Call(ctx, "activity.feed", p, &res); err != nil {
		return ActivityPage{}, err
	}
	items := a.activityItems(ctx, res.Items)
	chans := map[string]bool{}
	for _, it := range items {
		chans[it.Channel] = true
	}
	log.Printf("activity: %d items in %s", len(items), time.Since(t0).Round(time.Millisecond))
	return ActivityPage{Items: items, Next: res.Meta.NextCursor, Convs: a.convRefs(chans)}, nil
}

// activityItems converts feed entries and hydrates their messages (one
// messages.list round trip for everything not cached).
func (a *App) activityItems(ctx context.Context, raws []activityRaw) []ActivityItem {
	items := make([]ActivityItem, 0, len(raws))
	want := map[string][]string{}
	for _, raw := range raws {
		it := ActivityItem{Key: raw.Key, Type: raw.Item.Type, FeedTS: raw.FeedTS, Unread: raw.IsUnread, Channel: raw.Item.Channel}
		ref := raw.Item.Message
		if bi := raw.Item.BundleInfo; bi != nil {
			it.Count = bi.UnreadCount
			if ref == nil {
				ref = bi.Payload.Message
			}
			if te := bi.Payload.ThreadEntry; te != nil {
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
			it.Reaction = &ActivityReaction{User: rx.User, Name: rx.Name}
			it.Reaction.URL, _, _ = a.S.Emoji(rx.Name)
		}
		items = append(items, it)
	}
	got := a.hydrate(ctx, want)
	ids := []string{}
	for i := range items {
		ids = append(ids, items[i].Author)
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

// ---- threads view ----

// Threads loads the Threads view (subscriptions.thread.getView). Pass
// ThreadsPage.Next back as cursor for older threads.
func (a *App) Threads(ctx context.Context, cursor string, limit int) (ThreadsPage, error) {
	if limit <= 0 {
		limit = 10
	}
	p := url.Values{"limit": {strconv.Itoa(limit)}, "fetch_threads_state": {"true"}}
	if cursor != "" {
		p.Set("max_ts", cursor)
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
		State              struct {
			HasUnreads   bool `json:"has_unreads"`
			MentionCount int  `json:"mention_count"`
		} `json:"threads_state"`
	}
	if err := a.C.Call(ctx, "subscriptions.thread.getView", p, &res); err != nil {
		return ThreadsPage{}, err
	}
	var all []slack.Message
	for _, t := range res.Threads {
		all = append(all, t.Root)
		all = append(all, t.LatestReplies...)
		all = append(all, t.UnreadReplies...)
	}
	a.EnsureUsers(ctx, referencedUsers(all))
	out := ThreadsPage{HasMore: res.HasMore, Next: res.MaxTS, TotalUnread: res.TotalUnreadReplies}
	chans := map[string]bool{}
	for _, t := range res.Threads {
		ch := t.Root.Channel
		chans[ch] = true
		replies := t.LatestReplies
		if len(t.UnreadReplies) > len(replies) {
			replies = t.UnreadReplies
		}
		store.SortAsc(replies)
		out.Threads = append(out.Threads, Thread{Channel: ch, Root: a.message(ch, &t.Root), Replies: a.messages(ch, replies), UnreadCount: len(t.UnreadReplies)})
	}
	a.S.Lock()
	a.S.ThreadsUnread, a.S.ThreadMentions = res.State.HasUnreads, res.State.MentionCount
	a.S.Unlock()
	out.State = a.ThreadsState()
	out.Convs = a.convRefs(chans)
	return out, nil
}

// ---- saved / later ----

// Saved lists saved items. filter: SavedFilterSaved (default, the Saved
// page), SavedFilterTodos (the To-dos page), or any saved.list filter
// (saved, todo, completed, archived, …).
func (a *App) Saved(ctx context.Context, filter, cursor string) (SavedPage, error) {
	if filter == "" {
		filter = SavedFilterSaved
	}
	p := url.Values{"limit": {"50"}, "include_tombstones": {"true"}, "filter": {filter}}
	if cursor != "" {
		p.Set("cursor", cursor)
	}
	var res struct {
		Items  []savedRaw      `json:"saved_items"`
		Counts json.RawMessage `json:"counts"`
		Meta   struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := a.C.Call(ctx, "saved.list", p, &res); err != nil {
		return SavedPage{}, err
	}
	want := map[string][]string{}
	for _, it := range res.Items {
		if it.ItemType == "message" && it.TS != "" {
			want[it.ItemID] = append(want[it.ItemID], it.TS)
		}
	}
	got := a.hydrate(ctx, want)
	out := SavedPage{Counts: res.Counts, Next: res.Meta.NextCursor}
	chans := map[string]bool{}
	a.mu.Lock()
	for _, it := range res.Items {
		if it.ItemType == "message" {
			a.saved[savedKey(it.ItemID, it.TS)] = true
		}
	}
	a.mu.Unlock()
	for i := range res.Items {
		v := savedItemOf(&res.Items[i])
		if v.Channel != "" {
			chans[v.Channel] = true
			if m, ok := got[savedKey(v.Channel, v.TS)]; ok {
				v.Msg = a.messagePtr(v.Channel, &m)
			}
		}
		out.Items = append(out.Items, v)
	}
	out.Convs = a.convRefs(chans)
	return out, nil
}

// ---- DMs tab ----

// DMs lists direct messages newest first with their latest message
// (client.dms). Closed DMs are included but not added to the sidebar.
func (a *App) DMs(ctx context.Context, cursor string) (DMPage, error) {
	t0 := time.Now()
	p := url.Values{"count": {"50"}, "include_closed": {"true"}, "include_channel": {"true"}, "exclude_bots": {"false"}}
	if cursor != "" {
		p.Set("cursor", cursor)
	}
	type dmRaw struct {
		ID      string              `json:"id"`
		Message *slack.Message      `json:"message"`
		Channel *slack.Conversation `json:"channel"`
	}
	var res struct {
		IMs   []dmRaw `json:"ims"`
		MPIMs []dmRaw `json:"mpims"`
		Meta  struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := a.C.Call(ctx, "client.dms", p, &res); err != nil {
		return DMPage{}, err
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
	a.EnsureUsers(ctx, ids)
	out := DMPage{Next: res.Meta.NextCursor}
	for _, d := range all {
		var c *slack.Conversation
		if known, ok := a.S.Conv(d.ID); ok {
			c = known
		} else if d.Channel != nil {
			if d.Channel.ID == "" {
				d.Channel.ID = d.ID
			}
			c = d.Channel
		} else {
			continue
		}
		v := DM{Channel: d.ID, Conv: a.conv(c), Latest: a.messagePtr(d.ID, d.Message), Count: a.Count(d.ID)}
		if v.Count.Latest == "" && d.Message != nil {
			v.Count.Latest = d.Message.TS
		}
		out.DMs = append(out.DMs, v)
	}
	sort.SliceStable(out.DMs, func(i, j int) bool {
		var li, lj string
		if out.DMs[i].Latest != nil {
			li = out.DMs[i].Latest.TS
		}
		if out.DMs[j].Latest != nil {
			lj = out.DMs[j].Latest.TS
		}
		return store.TSLess(lj, li)
	})
	a.SubscribePresence(ids)
	log.Printf("dms: %d conversations in %s", len(out.DMs), time.Since(t0).Round(time.Millisecond))
	return out, nil
}

// ---- commands & DND ----

// Commands lists slash commands matching query (cached for 30 minutes).
func (a *App) Commands(ctx context.Context, query string) ([]Command, error) {
	a.mu.Lock()
	cmds := a.cmdCache
	fresh := cmds != nil && time.Since(a.cmdAt) < 30*time.Minute
	a.mu.Unlock()
	if !fresh {
		var err error
		if cmds, err = a.C.Commands(ctx); err != nil {
			return nil, err
		}
		a.mu.Lock()
		a.cmdCache, a.cmdAt = cmds, time.Now()
		a.mu.Unlock()
	}
	needle := strings.TrimPrefix(strings.ToLower(query), "/")
	out := make([]Command, 0, len(cmds))
	for _, c := range cmds {
		if needle == "" || strings.Contains(strings.ToLower(c.Name), needle) {
			out = append(out, Command{Name: c.Name, Desc: c.Desc, Usage: c.Usage, Type: c.Type, AppID: c.AppID,
				AppName: c.AppName, IconURL: c.IconURL()})
		}
	}
	return out, nil
}

// DND returns dnd.info for the viewer.
func (a *App) DND(ctx context.Context) (json.RawMessage, error) {
	var info json.RawMessage
	err := a.C.Call(ctx, "dnd.info", url.Values{"user": {a.S.SelfID()}}, &info)
	return info, err
}

// loadMemberCount caches the people count of ch (edge users/counts).
func (a *App) loadMemberCount(ctx context.Context, ch string) {
	mc, err := a.MemberCounts(ctx, ch)
	if err != nil {
		return
	}
	a.mu.Lock()
	a.memberN[ch] = mc.People
	a.mu.Unlock()
}
