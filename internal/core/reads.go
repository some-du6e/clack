package core

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// ---- history & threads ----

// History returns the newest page of a conversation (before == "") from
// memory when cached, else one conversations.history call; with before set
// it loads the next older page. Opening a cached channel takes ~1 ms.
func (a *App) History(ctx context.Context, ch, before string) (Page, error) {
	t0 := time.Now()
	source := "cache"
	if before == "" {
		a.watch(ch, "")
	}
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
	a.watch(ch, ts)
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

// SlackbotCustomResponseInThread reports whether user (Slackbot) answered
// with a custom response (SubtypeSlackbotResponse) in the thread
// ch/threadTS: Slack's hasSlackbotCustomResponseInThread selector. It
// reads the cached replies only (no request, safe on the UI and JS
// threads), so it is false until the thread has been loaded. An empty
// user matches any sender.
func (a *App) SlackbotCustomResponseInThread(ch, threadTS, user string) bool {
	if ch == "" || threadTS == "" {
		return false
	}
	return a.S.ThreadHas(ch, threadTS, func(m *slack.Message) bool {
		return m.Subtype == SubtypeSlackbotResponse && (user == "" || m.User == user)
	})
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
	a.installPrefs(raw)
	return a.Prefs(), nil
}

// installPrefs keeps users.prefs.get's map and its parsed Prefs.
func (a *App) installPrefs(raw map[string]json.RawMessage) {
	a.S.RLock()
	old := a.S.RawPrefs
	a.S.RUnlock()
	a.frecencyPrefsChanged(old, raw)
	a.S.SetRawPrefs(raw)
	p, muted := store.ParsePrefs(raw)
	a.S.SetPrefs(p, a.withTempMutes(p, muted)) // temp_mute's muted-until (temp_mute.go)
}

// Pref returns one users.prefs.get value as Slack sent it (ok: present).
func (a *App) Pref(name string) (json.RawMessage, bool) {
	a.S.RLock()
	defer a.S.RUnlock()
	v, ok := a.S.RawPrefs[name]
	return v, ok
}

// PrefString is a string pref ("" when absent or not a string). JSON-typed
// prefs (ia_theme, …) come back as the JSON text Slack stores.
func (a *App) PrefString(name string) string {
	v, _ := a.Pref(name)
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	if len(v) > 0 && (v[0] == '{' || v[0] == '[') {
		return string(v)
	}
	return ""
}

// PrefBool is a boolean pref (def when absent). Slack sends some as 0/1
// or "true"/"false".
func (a *App) PrefBool(name string, def bool) bool {
	v, ok := a.Pref(name)
	if !ok {
		return def
	}
	return prefTruthy(v, def)
}

// PrefInt is a numeric pref (def when absent or not a number).
func (a *App) PrefInt(name string, def int) int {
	v, ok := a.Pref(name)
	if !ok {
		return def
	}
	var n float64
	if json.Unmarshal(v, &n) == nil {
		return int(n)
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		if i, err := strconv.Atoi(s); err == nil {
			return i
		}
	}
	return def
}

func prefTruthy(v json.RawMessage, def bool) bool {
	var b bool
	if json.Unmarshal(v, &b) == nil {
		return b
	}
	var n float64
	if json.Unmarshal(v, &n) == nil {
		return n != 0
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		switch s {
		case "true", "1":
			return true
		case "false", "0", "":
			return false
		}
	}
	return def
}

// SetPref writes one account pref the way Slack's setUserPrefByApi does
// (users.prefs.set name=…&value=…). value is a string, bool, number, or
// for Slack's JSON-typed prefs (ia_theme) any JSON-marshalable value,
// sent JSON-encoded like Slack's serializer. On success the local copy
// is updated at once (Slack's pref_change follows) and a "prefs" event
// fires.
func (a *App) SetPref(ctx context.Context, name string, value any) error {
	var form string
	var local json.RawMessage
	switch v := value.(type) {
	case string:
		form = v
		local, _ = json.Marshal(v)
	case bool:
		form = strconv.FormatBool(v)
		local, _ = json.Marshal(v)
	case int:
		form = strconv.Itoa(v)
		local, _ = json.Marshal(v)
	case nil:
		form, local = "", json.RawMessage("null")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		form = string(b)
		local, _ = json.Marshal(form) // users.prefs.get returns JSON prefs as strings
	}
	if err := a.C.SetPref(ctx, name, form); err != nil {
		return err
	}
	a.S.RLock()
	raw := make(map[string]json.RawMessage, len(a.S.RawPrefs)+1)
	for k, v := range a.S.RawPrefs {
		raw[k] = v
	}
	a.S.RUnlock()
	raw[name] = local
	a.installPrefs(raw)
	p := a.Prefs()
	a.emit(Event{Type: "prefs", Prefs: &p})
	return nil
}

// ---- search & directory ----

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

// AutocompleteUsers is the composer @ list's server search
// (getRemoteMembers): edge users/search with count 25, fuzz 1,
// enable_workspace_ranking, the current channel and default workspace.
// Deactivated people stay in (Slack's texty search includes them; the
// ranking demotes them); deactivated bots are dropped later.
func (a *App) AutocompleteUsers(ctx context.Context, query, channel string) ([]User, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	us, err := a.C.SearchUsersWith(ctx, query, slack.UserSearchOpts{Count: 25, CurrentChannel: channel,
		DefaultWorkspace: a.C.Creds.Team})
	if err != nil {
		return nil, err
	}
	a.S.PutUsers(us)
	out := make([]User, 0, len(us))
	ids := make([]string, 0, len(us))
	for i := range us {
		out = append(out, a.userOf(&us[i]))
		if !us[i].Deleted {
			ids = append(ids, us[i].ID)
		}
	}
	a.SubscribePresence(ids)
	return out, nil
}

// AutocompleteChannels is the composer # list's server search
// (getRemoteChannels): edge channels/search with count 25, fuzz 1 and
// filter xws (includeFromAnotherTeam).
func (a *App) AutocompleteChannels(ctx context.Context, query string) ([]ChannelHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	hs, err := a.C.SearchChannelsWith(ctx, query, slack.ChannelSearchOpts{Count: 25, Filter: "xws",
		DefaultWorkspace: a.C.Creds.Team})
	if err != nil {
		return nil, err
	}
	return a.channelHits(hs), nil
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

// ChannelApp is a row of the channel details "Agents & apps" tab: the bot
// user, its badge (AGENT for agent_status consistently_an_agent, else
// APP) and the app's tagline (apps.profile.get).
type ChannelApp struct {
	User    User
	Badge   string // core.BadgeAgent | core.BadgeApp
	Tagline string
}

// channelAppTaglines bounds the apps.profile.get calls one tab makes.
const channelAppTaglines = 30

// ChannelApps lists the agents and apps in a channel (edge users/list
// filter "apps", users_by_realname order), with each app's tagline.
func (a *App) ChannelApps(ctx context.Context, ch string) ([]ChannelApp, error) {
	us, _, err := a.C.ChannelApps(ctx, ch, "", 100)
	if err != nil {
		return nil, err
	}
	a.S.PutUsers(us)
	out := make([]ChannelApp, len(us))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i := range us {
		u := a.userOf(&us[i])
		out[i] = ChannelApp{User: u, Badge: BadgeApp}
		if u.Agent || u.Agentforce {
			out[i].Badge = BadgeAgent
		}
		if i >= channelAppTaglines || (u.BotID == "" && u.AppID == "") {
			continue
		}
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if card, err := a.AppCardOf(ctx, id, ""); err == nil {
				out[i].Tagline = card.Tagline
			}
		}(i, u.ID)
	}
	wg.Wait()
	return out, nil
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
			ShortcutID  string `json:"shortcut_id"`
		} `json:"bookmarks"`
	}
	if err := a.C.Call(ctx, "bookmarks.list", url.Values{"channel_id": {ch}, "channel": {ch}, "include_folders": {"true"}}, &res); err != nil {
		return nil, err
	}
	out := make([]Bookmark, 0, len(res.Bookmarks))
	for _, b := range res.Bookmarks {
		v := Bookmark{ID: b.ID, Title: b.Title, Link: b.Link, Emoji: b.Emoji, Icon: b.IconURL, Type: b.Type,
			EntityID: b.EntityID, Created: b.DateCreated, Rank: b.Rank, ParentID: b.ParentID, ShortcutID: b.ShortcutID}
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
	d.Teams = c.InternalTeamIDs
	if len(d.Teams) == 0 && c.ContextTeamID != "" {
		d.Teams = []string{c.ContextTeamID}
	}
	if len(d.Teams) > 8 {
		d.Teams = d.Teams[:8]
	}
	for _, t := range d.Teams {
		a.EnsureTeam(ctx, t)
	}
	if c.Topic != nil {
		d.TopicSetBy, d.TopicSetAt = c.Topic.Creator, c.Topic.LastSet
	}
	if c.Purpose != nil {
		d.PurposeSetBy, d.PurposeSetAt = c.Purpose.Creator, c.Purpose.LastSet
	}
	d.Props, d.HostID = ParseChannelProps(c.Properties), c.ConversationHostID
	return d, nil
}

// ---- activity ----

var activityTypes = map[string]string{
	// the web client's Activity inbox list (2026-11)
	"all":       "at_user,at_user_group,at_channel,at_everyone,keyword,list_record_assigned,list_user_mentioned,list_todo_notification,list_approval_request,list_approval_reviewed,unjoined_channel_mention,thread_v2,message_reaction,bot_dm_bundle,dm,prejoin_dm_welcome_party_alert,internal_channel_invite,external_channel_invite,external_dm_invite,quietly_added_to_channel,channel,saved_reminder,list_record_edited",
	"dms":       "dm",
	"mentions":  "at_user,at_user_group,at_channel,at_everyone,keyword,unjoined_channel_mention",
	"threads":   "thread_v2",
	"reactions": "message_reaction",
	"invites":   "internal_channel_invite,external_channel_invite,external_dm_invite,quietly_added_to_channel",
	"channels":  "channel",
	"apps":      "bot_dm_bundle",
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
		limit = 20
	}
	// the web client's params (activity inbox, 2026-11-09)
	mode := o.Mode
	if mode == "" {
		mode = "chrono_v1"
	}
	p := url.Values{"limit": {strconv.Itoa(limit)}, "types": {types}, "mode": {mode},
		"archive_only": {strconv.FormatBool(o.Archived)}, "priority_only": {strconv.FormatBool(o.Priority)}, "is_activity_inbox": {"true"},
		"unread_only": {strconv.FormatBool(o.Unread)}, "only_salesforce_channels": {"false"},
		"exclude_automations": {"false"}, "automations_only": {"false"}}
	if o.Cursor != "" {
		p.Set("cursor", o.Cursor)
	}
	// eP2o fetchActivityFeed: a view's channels and sections, when it has any
	if len(o.ChannelIDs) > 0 {
		p.Set("channel_ids", strings.Join(o.ChannelIDs, ","))
	}
	if len(o.SectionIDs) > 0 {
		p.Set("channel_section_ids", strings.Join(o.SectionIDs, ","))
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
	a.memberG[ch] = mc.Guests
	a.memberA[ch] = mc.Apps
	a.mu.Unlock()
}
