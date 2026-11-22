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

	"github.com/some-du6e/clack/internal/logtag"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// ---- history & threads ----

// History returns the newest page of a conversation (before == "") from
// memory when cached, else one conversations.history call; with before set
// it loads the next older page. Opening a cached channel takes ~1 ms: the
// header's side reads (the member count, a conversation outside the
// sidebar) run on their own and never hold a cached page back
// (history_side.go).
func (a *App) History(ctx context.Context, ch, before string) (Page, error) {
	t0 := time.Now()
	source := "cache"
	if before == "" {
		a.watch(ch, "")
	}
	a.noteAccess(ch)
	defer func() {
		log.Printf("history %s: %s (%s)", logtag.ID(ch), time.Since(t0).Round(time.Millisecond), source)
	}()
	var count, conv <-chan struct{}
	if before == "" {
		count, conv = a.historySide(ch)
	}
	// An error return waits for the side reads: after an auth failure
	// (which cancels them) a History that failed leaves nothing of its own
	// in flight that could reach Slack after sign-out.
	fail := func(err error) (Page, error) {
		waitSide(count, conv)
		return Page{}, err
	}
	var msgs []slack.Message
	var hasMore bool
	var epoch uint64
	if before == "" {
		t := a.S.Timeline(ch)
		if t == nil {
			source = "fetch"
			if err := a.loadLatestIfMissing(ctx, ch); err != nil {
				return fail(err)
			}
			t = a.S.Timeline(ch)
			// a fetched page comes with the conversation it resolved
			// alongside (Page.Conv); the count lands on its own
			waitSide(conv)
		}
		if t != nil {
			msgs, hasMore, epoch = t.Msgs, t.HasMore, t.Epoch
			if t.EndStale {
				// the window from before a slow connect, at once, as
				// Slack draws its outdated slice; read forward in the
				// background ("history_caught_up": reached_end.go)
				source = "cache, catching up"
				a.startCatchUp(ch)
			}
		}
	} else {
		source = "older"
		pins, subs, live := a.S.PinSeq(), a.S.SubSeq(), a.S.LiveSeq()
		p, err := a.C.History(ctx, ch, slack.HistoryOpts{Latest: before, Limit: 100})
		if err != nil {
			return fail(err)
		}
		// what the socket changed, deleted or reacted while it was out
		// stays, and a revision the store held another of reaches every
		// copy and the views (store PrependOlderSince, read_changes.go);
		// in front of a window read again since (one that starts after
		// before) it would leave a hole, so it stays out of that one
		// (store PrependOlderAt)
		kept := a.S.PrependOlderAt(ch, before, p.Messages, p.HasMore, live)
		a.S.SyncPins(ch, pins, p.Messages) // a pin event newer than the answer (store/pins.go)
		a.S.SyncSubs(ch, subs, p.Messages) // the roots' thread subscriptions (store/thread_subs.go)
		a.announceReads()                  // after the pins and subscriptions: the copies as they are now
		// the list takes what the read kept, as the window holds it now
		for i := range kept {
			if m, ok := a.S.Find(ch, kept[i].TS); ok {
				kept[i] = m
			}
		}
		msgs, hasMore = kept, p.HasMore
	}
	page := Page{Messages: a.timeline(ch, msgs), HasMore: hasMore, Epoch: epoch}
	if c, ok := a.Conv(ch); ok {
		page.Conv = &c
	}
	e, _ := a.S.Count(ch)
	page.LastRead = e.LastRead
	a.SubscribePresence(rowUsers(msgs))
	return page, nil
}

// Refresh refetches the newest page of ch (replacing the cached one).
func (a *App) Refresh(ctx context.Context, ch string) (Page, error) {
	if err := a.loadLatest(ctx, ch); err != nil {
		return Page{}, err
	}
	return a.History(ctx, ch, "")
}

// Replies returns a thread as its pane would open on it, root first: the
// slice at its default start (the last read of a followed thread, else
// the root; thread_pages.go), from memory when held, else one or two
// replies reads of 28 (requestReplies). It is one slice, never the whole
// thread: the viewer's newest message reads the newest slice instead
// (ThreadSliceAt with no start, as Slack's undo send and edit-last
// selectors read dTxY tp(threadKey, null)), and a List item's comments
// page as the thread pane's do (ui/listview item.go).
// A reply's ts reads its parent's thread at the reply: conversations.replies
// answers a reply's ts with its parent's thread, which repliesOne and
// repliesAround keep under the parent's key (replyRooted), so the pane
// opened with the reply's ts finds the reply in what it draws and hands
// on to the parent's (ReplyAsThreadRoot), which then has the page without
// a second read, and the reply is never watched, polled or kept as a
// thread of its own.
func (a *App) Replies(ctx context.Context, ch, ts string) ([]Message, error) {
	if p, ok := a.ReplyParent(ch, ts); ok {
		return a.repliesAt(ctx, ch, p, ts)
	}
	sl, err := a.ThreadAt(ctx, ch, ts, a.threadDefaultStart(ch, ts))
	if err != nil {
		return nil, err
	}
	if p, ok := a.ReplyParent(ch, ts); ok { // the read found ts was a reply
		return a.repliesAt(ctx, ch, p, ts)
	}
	return threadMsgs(sl, ts), nil
}

// repliesAt is Replies for thread ts of ch started at its reply start.
func (a *App) repliesAt(ctx context.Context, ch, ts, start string) ([]Message, error) {
	sl, err := a.ThreadAt(ctx, ch, ts, start)
	if err != nil {
		return nil, err
	}
	return threadMsgs(sl, ts), nil
}

// RefreshReplies reads the thread's default start again from the server
// (requestReplies around it, the answers over the cached copies), then
// returns it as Replies does.
func (a *App) RefreshReplies(ctx context.Context, ch, ts string) ([]Message, error) {
	start := a.threadDefaultStart(ch, ts)
	if err := a.RequestReplies(ctx, ch, ts, start, start); err != nil {
		return nil, err
	}
	return a.Replies(ctx, ch, ts)
}

// hydrate fetches messages by (channel, ts): memory first, then one
// messages.list call for the rest.
func (a *App) hydrate(ctx context.Context, want map[string][]string) map[string]slack.Message {
	out, _ := a.hydrateOK(ctx, want)
	return out
}

// hydrateOK is hydrate, also reporting which channels answered: every
// channel of want found in memory, plus those whose messages.list group
// succeeded (slack.MessagesList). A message missing from the result is
// gone only when its channel answered; a failed group (too_many_channels,
// a reset connection) leaves its channels' messages unknown. The store
// keeps what it read (store/fetched.go) for the views that draw the
// socket's changes to it: Activity, Saved.
func (a *App) hydrateOK(ctx context.Context, want map[string][]string) (map[string]slack.Message, map[string]bool) {
	return a.hydrateKeep(ctx, want, true)
}

// hydrateKeep is hydrateOK; with keep false (a page that never draws the
// socket's changes: Sent) the store keeps none of it.
func (a *App) hydrateKeep(ctx context.Context, want map[string][]string, keep bool) (map[string]slack.Message, map[string]bool) {
	out, answered, _ := a.hydrateKeepErr(ctx, want, keep)
	return out, answered
}

// hydrateKeepErr is hydrateKeep with the error of the groups that failed
// (RefetchMessages waits out a rate limit).
func (a *App) hydrateKeepErr(ctx context.Context, want map[string][]string, keep bool) (map[string]slack.Message, map[string]bool, error) {
	out := map[string]slack.Message{}
	missing := map[string][]string{}
	answered := map[string]bool{}
	for ch, tss := range want {
		var held []string
		for _, ts := range tss {
			if m, ok := a.S.Find(ch, ts); ok {
				out[ch+"/"+ts] = m
				held = append(held, ts)
			} else {
				missing[ch] = append(missing[ch], ts)
			}
		}
		if keep {
			// a window's or a thread's message stays where the socket's
			// changes reach it once core evicts the window
			a.S.KeepFetched(ch, held)
		}
		if len(missing[ch]) == 0 {
			answered[ch] = true
		}
	}
	if len(missing) == 0 {
		return out, answered, nil
	}
	// a failed group is logged with its channels in MessagesList
	since := a.S.LiveSeq()
	got, asked, err := a.C.MessagesList(ctx, missing)
	for ch := range asked {
		answered[ch] = true
	}
	if keep {
		// a group that failed (a network blip) leaves its messages as the
		// store has them: the copies fetched by id that the socket kept
		// current, as Slack's rows draw what its store holds and fetch
		// only what it doesn't
		for ch, tss := range missing {
			if answered[ch] {
				continue
			}
			for _, ts := range tss {
				if m, ok := a.S.FindHeld(ch, ts); ok {
					out[ch+"/"+ts] = m
				}
			}
		}
	}
	if !keep {
		for ch, ms := range got {
			for _, m := range ms {
				out[ch+"/"+m.TS] = m
			}
		}
		return out, answered, err
	}
	// Slack's fetchObjectWithId puts what it fetched by id into its
	// messages store, where the socket's changes reach it: the store keeps
	// the answer (store/fetched.go), merged with what the socket changed,
	// deleted or reacted while the request was out, and a message a
	// channel that answered left out is gone, its fetched copy with it
	for ch, tss := range missing {
		if !answered[ch] {
			continue
		}
		for _, m := range a.S.PutFetchedAsked(ch, tss, got[ch], since) {
			out[ch+"/"+m.TS] = m
		}
	}
	a.announceReads() // read_changes.go
	// the messages' people are asked for as rows draw them (lazy_users.go)
	return out, answered, err
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
	a.prefsMu.Lock()
	defer a.prefsMu.Unlock()
	a.S.RLock()
	old := a.S.RawPrefs
	a.S.RUnlock()
	a.sectionCollapsePending(old, raw) // a collapse write still waiting (section_collapse.go)
	keepFrecency(old, raw)             // only a named fetch carries the account's frecency (frecency.go)
	a.installPrefsLocked(raw)
}

// mergePrefs sets kv over the current raw prefs and installs them. The
// copy and the install are one step: a write finishing meanwhile (another
// SetPref, a pref_change reload) can't be undone by this one's older copy.
func (a *App) mergePrefs(kv map[string]json.RawMessage) {
	a.prefsMu.Lock()
	defer a.prefsMu.Unlock()
	a.S.RLock()
	raw := make(map[string]json.RawMessage, len(a.S.RawPrefs)+len(kv))
	for k, v := range a.S.RawPrefs {
		raw[k] = v
	}
	a.S.RUnlock()
	for k, v := range kv {
		raw[k] = v
	}
	a.installPrefsLocked(raw)
}

// installPrefsLocked is installPrefs with prefsMu held.
func (a *App) installPrefsLocked(raw map[string]json.RawMessage) {
	a.S.RLock()
	old := a.S.RawPrefs
	a.S.RUnlock()
	a.frecencyPrefsChanged(old, raw)
	a.S.SetRawPrefs(raw)
	p, muted := store.ParsePrefs(raw)
	a.S.SetPrefs(p, a.withTempMutes(p, muted)) // temp_mute's muted-until (temp_mute.go)
	a.notePrefsInstalled()                     // what Docs converted until now read moved? (msg_refs.go)
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
	a.mergePrefs(map[string]json.RawMessage{name: local})
	p := a.Prefs()
	a.emit(Event{Type: "prefs", Prefs: &p})
	return nil
}

// MediaProgressUpdate tells Slack where the viewer is in a video (the
// players' saveMediaProgress, gnNF and gBWG): offset_ms of duration_ms.
func (a *App) MediaProgressUpdate(ctx context.Context, file string, offsetMS, durationMS int64) error {
	return a.C.MediaProgressUpdate(ctx, file, offsetMS, durationMS)
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
		if g.DateDelete != 0 {
			continue // a deactivated group the directory or subteam_updated stored: nothing to mention
		}
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
	// pins.list fetches nobody (shouldSkipMemberFetch): the pins' rows ask
	// for their people and "Pinned by" as they draw (lazy_users.go)
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
	if err := a.C.Call(ctx, "bookmarks.list", bookmarksListArgs(ch), &res); err != nil {
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
	d := ChannelDetails{Conv: a.conv(c), Managers: mgrs, IsMember: c.IsMember || c.IsIM, IsOrgShared: c.IsOrgShared, IsExtShared: c.IsExtShared, LastRead: c.LastRead,
		OrgMandatory: c.IsOrgMandatory, OrgDefault: c.IsOrgDefault}
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
		"exclude_automations": {strconv.FormatBool(o.ExcludeAutomations)}, "automations_only": {strconv.FormatBool(o.AutomationsOnly)}}
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
	seq := a.MessageChangeSeq() // what changed after this the items may not have (msg_changes.go)
	if err := a.C.Call(ctx, "activity.feed", p, &res); err != nil {
		return ActivityPage{}, err
	}
	items := a.activityItems(ctx, res.Items)
	stampItems(items, seq)
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
	subs := a.S.SubSeq() // a subscription event while it is out is newer (SyncSubs)
	seq, since := a.MessageChangeSeq(), a.S.LiveSeq()
	threadsSince := a.S.Seq() // an update_thread_state while it is out is newer (SetThreadsStateSince)
	if err := a.C.Call(ctx, "subscriptions.thread.getView", p, &res); err != nil {
		return ThreadsPage{}, err
	}
	// processThreads runs every root through slurpThreadSubscriptions
	// (udbq, preprocessImsgs): each listed thread's subscription is the
	// answer's (subscribed, last_read), so a later mark or thread_marked
	// only moves its last read (store/thread_subs.go); before the copies
	// below read it
	for _, t := range res.Threads {
		a.S.SyncSubs(t.Root.Channel, subs, []slack.Message{t.Root})
	}
	// the threads' people are asked for as rows draw them (lazy_users.go)
	out := ThreadsPage{HasMore: res.HasMore, Next: res.MaxTS, TotalUnread: res.TotalUnreadReplies, MsgSeq: seq}
	chans := map[string]bool{}
	for _, t := range res.Threads {
		ch := t.Root.Channel
		chans[ch] = true
		replies := t.LatestReplies
		if len(t.UnreadReplies) > len(replies) {
			replies = t.UnreadReplies
		}
		store.SortAsc(replies)
		// Slack's Threads view draws its threads from the messages store,
		// where the socket's changes reach them (a reaction too): the
		// store keeps what getView brought (store/fetched.go), merged with
		// what changed while it was out; a reply deleted meanwhile stays
		// out, and a root keeps the answer's copy (its thread is the card)
		kept := a.S.PutFetched(ch, append([]slack.Message{t.Root}, replies...), since)
		a.announceReads() // read_changes.go
		root := t.Root
		if len(kept) > 0 && kept[0].TS == root.TS {
			root, kept = kept[0], kept[1:]
		}
		// the unread replies the card keeps: one deleted meanwhile takes
		// its count with it, so the New line sits on an unread reply
		have := make(map[string]bool, len(kept))
		for i := range kept {
			have[kept[i].TS] = true
		}
		unread := 0
		for i := range t.UnreadReplies {
			if have[t.UnreadReplies[i].TS] {
				unread++
			}
		}
		out.Threads = append(out.Threads, Thread{Channel: ch, Root: a.message(ch, &root), Replies: a.messages(ch, kept), UnreadCount: unread})
	}
	a.S.SetThreadsStateSince(res.State.HasUnreads, res.State.MentionCount, threadsSince)
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
	got, answered := a.hydrateOK(ctx, want)
	var counts struct {
		Overdue *int `json:"uncompleted_overdue_count"`
	}
	if json.Unmarshal(res.Counts, &counts) == nil && counts.Overdue != nil {
		a.mu.Lock()
		a.savedOverdue = *counts.Overdue
		a.mu.Unlock()
	}
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
			} else if answered[v.Channel] && v.ItemType == "message" && v.TS != "" {
				v.Gone = true
			}
		}
		out.Items = append(out.Items, v)
	}
	out.Convs = a.convRefs(chans)
	return out, nil
}

// ---- DMs tab ----

// DMs lists direct messages newest first with their latest message
// (client.dms), filtered as Slack's DMs tab filters them (dmsTabShows).
// Closed DMs are included but not added to the sidebar.
//
// DmsListAll (gantry dms-list-view eLDn) asks client.dms for {count: 250,
// include_closed, include_channel, exclude_bots: !agents_in_dm_sidebar}.
func (a *App) DMs(ctx context.Context, cursor string) (DMPage, error) {
	t0 := time.Now()
	excludeBots := "true"
	if a.experiment("agents_in_dm_sidebar") == "on" { // O2By Mq
		excludeBots = "false"
	}
	p := url.Values{"count": {"250"}, "include_closed": {"true"}, "include_channel": {"true"}, "exclude_bots": {excludeBots}}
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
	ids := []string{}
	for _, d := range all {
		if d.Channel != nil {
			ids = append(ids, d.Channel.User)
			ids = append(ids, d.Channel.Members...)
		}
	}
	// the DMs' people first: dmsTabShows reads them (deleted members and
	// bots are left out); the latest messages' people are asked for as
	// the rows draw them (lazy_users.go)
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
		if !a.dmsTabShows(c, v.Count.Latest) {
			continue
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

// dmsTabShows is the DMs tab's row filter for one IM or group DM
// (gantry dms-list-view uV3r useChannelIds, upCu useSortedChannels). An IM
// goes when its member is deleted, when it's frozen or a pending Slack
// Connect IM, when the member is the security bot (USLACKSECURITY),
// USEARCH or UCHANNELEXPERT, and when it's a bot DM (DUy0: Slackbot's IM,
// USLACK, or a member Slack counts as a bot: is_bot, is_slackbot) unless
// the bot is an agent shown with the DMs (uXg5 isAgentInDmSidebar). Any
// DM without a message goes too (no latest ts), except the self-DM and
// for a new account (shouldShowAllDms: onboarding not complete).
func (a *App) dmsTabShows(c *slack.Conversation, latest string) bool {
	if c.IsIM {
		switch c.User {
		case "USLACKSECURITY", "USEARCH", "UCHANNELEXPERT":
			return false
		}
		if c.IsFrozen || c.IsPendingExtShared {
			return false
		}
		u, known := a.S.User(c.User)
		if known && u.Deleted {
			return false
		}
		if isBotDM(c.User, u) && !a.agentInDMSidebarUser(c.User) { // rail.go
			return false
		}
	}
	if latest == "" || strings.Trim(latest, "0.") == "" {
		return c.IsIM && c.User == a.S.SelfID() || !a.PrefBool("onboarding_complete", true)
	}
	return true
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
				AppName: c.AppName, IconURL: c.IconURL(), Teams: c.Teams})
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

// loadMemberCount caches the people count of ch (edge users/counts). One
// read per channel at a time: History's side read and the header's
// ChannelInfo, both on a first open, share it, each waiting only as long
// as its own ctx allows (onceCtx). A count that arrived or changed is
// announced ("member_count"): the header repaints.
func (a *App) loadMemberCount(ctx context.Context, ch string) {
	_ = a.onceCtx(ctx, "mc:"+ch, func() error { return a.loadMemberCountNow(ctx, ch) })
}

// loadMemberCountNow reads ch's counts and caches them, without joining
// a read already out: a stale count's refetch (membership_counts.go)
// must not take the answer of a read that started before the join.
func (a *App) loadMemberCountNow(ctx context.Context, ch string) error {
	mc, err := a.MemberCounts(ctx, ch)
	if err != nil {
		return err
	}
	a.mu.Lock()
	old, had := a.memberN[ch]
	changed := !had || old != mc.People || a.memberA[ch] != mc.Apps
	a.memberN[ch] = mc.People
	a.memberG[ch] = mc.Guests
	a.memberA[ch] = mc.Apps
	if a.memberMC == nil {
		a.memberMC = map[string]MemberCounts{}
	}
	a.memberMC[ch] = mc
	a.mu.Unlock()
	if changed {
		a.memberCountChanged(ch) // history_side.go
	}
	return nil
}
