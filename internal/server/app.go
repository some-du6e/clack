// Package server exposes the Slack client to the embedded web UI.
package server

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/render"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// App wires the Slack client, store, realtime connection and UI hub.
type App struct {
	C         *slack.Client
	S         *store.Store
	RTM       *slack.RTM
	Hub       *Hub
	ready     chan struct{}
	bootErr   error
	connected bool
	mu        sync.Mutex
	inflight  map[string]*call

	presSeen    map[string]time.Time // users the UI recently showed (presence subscriptions)
	presWaiters []chan struct{}
	countsTimer *time.Timer
	cmdCache    []slack.Command
	cmdAt       time.Time
}

type call struct {
	done chan struct{}
	err  error
}

func NewApp(c *slack.Client, s *store.Store) *App {
	a := &App{C: c, S: s, Hub: newHub(), ready: make(chan struct{}), inflight: map[string]*call{}}
	a.RTM = c.NewRTM()
	a.RTM.OnState = func(up bool) {
		a.mu.Lock()
		a.connected = up
		a.mu.Unlock()
		a.Hub.Broadcast(map[string]any{"type": "connection", "connected": up})
		if up {
			go a.refreshCounts(context.Background())
		}
	}
	return a
}

// once deduplicates concurrent identical fetches (e.g. hover prefetch + click).
func (a *App) once(key string, f func() error) error {
	a.mu.Lock()
	if c, ok := a.inflight[key]; ok {
		a.mu.Unlock()
		<-c.done
		return c.err
	}
	c := &call{done: make(chan struct{})}
	a.inflight[key] = c
	a.mu.Unlock()
	c.err = f()
	a.mu.Lock()
	delete(a.inflight, key)
	a.mu.Unlock()
	close(c.done)
	return c.err
}

// Boot loads everything the sidebar needs, in parallel, and logs how long
// each part took (timings only, never contents).
func (a *App) Boot(ctx context.Context) error {
	defer close(a.ready)
	t0 := time.Now()
	a.S.LoadCache()
	tCache := time.Since(t0)
	var (
		wg       sync.WaitGroup
		tmu      sync.Mutex
		timings  []string
		convs    []slack.Conversation
		counts   *slack.Counts
		sections []slack.Section
		errConvs error
	)
	step := func(name string, f func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := time.Now()
			err := f()
			d := time.Since(s).Round(time.Millisecond)
			tmu.Lock()
			if err != nil {
				timings = append(timings, name+"="+d.String()+"(err)")
				log.Printf("boot: %s: %v", name, err)
			} else {
				timings = append(timings, name+"="+d.String())
			}
			tmu.Unlock()
		}()
	}
	step("conversations", func() error {
		convs, errConvs = a.C.Conversations(ctx)
		return errConvs
	})
	step("counts", func() (err error) {
		counts, err = a.C.Counts(ctx)
		return err
	})
	step("sections", func() (err error) {
		sections, err = a.C.SectionsAll(ctx)
		return err
	})
	step("prefs", func() error {
		raw, err := a.C.Prefs(ctx)
		if err == nil {
			a.S.SetPrefs(store.ParsePrefs(raw))
		}
		return err
	})
	step("team", func() error {
		var r struct {
			Team struct {
				ID     string         `json:"id"`
				Name   string         `json:"name"`
				Domain string         `json:"domain"`
				Icon   map[string]any `json:"icon"`
			} `json:"team"`
		}
		err := a.C.Call(ctx, "team.info", nil, &r)
		if err == nil {
			a.S.Lock()
			a.S.TeamID, a.S.TeamName, a.S.TeamDomain = r.Team.ID, r.Team.Name, r.Team.Domain
			if s, ok := r.Team.Icon["image_88"].(string); ok {
				a.S.TeamIcon = s
			}
			a.S.Unlock()
		}
		return err
	})
	if _, ok := a.S.User(a.C.Creds.UserID); !ok {
		step("self", func() error {
			a.ensureUsers(ctx, []string{a.C.Creds.UserID})
			return nil
		})
	}
	if a.S.CustomEmoji.Len() == 0 {
		step("emoji", func() error { return a.loadEmoji(ctx) }) // first run: needed before rendering
	} else {
		go a.loadEmoji(context.Background())
	}
	// Usergroups only matter for unlabeled <!subteam^> tokens and autocomplete:
	// never block boot on them (they are cached on disk after the first run).
	go a.loadUsergroups(context.Background())
	wg.Wait()
	if errConvs != nil {
		a.bootErr = errConvs
		return errConvs
	}
	for _, c := range convs {
		a.S.PutConv(c)
	}
	if counts != nil {
		a.S.SetCounts(counts)
	}
	a.S.Lock()
	a.S.Sections = sections
	a.S.Unlock()
	ids := []string{a.C.Creds.UserID}
	for _, c := range convs {
		if c.IsIM {
			ids = append(ids, c.User)
		}
	}
	tu := time.Now()
	a.ensureUsers(ctx, ids)
	if u, ok := a.S.User(a.C.Creds.UserID); ok {
		a.S.Lock()
		a.S.Self = *u
		a.S.Unlock()
	}
	sort.Strings(timings)
	log.Printf("boot: ready in %s (cache %s, %s, dm-users=%s); %d conversations, %d sections, %d users, %d emoji",
		time.Since(t0).Round(time.Millisecond), tCache.Round(time.Millisecond), strings.Join(timings, " "),
		time.Since(tu).Round(time.Millisecond), len(convs), len(sections), len(a.S.Users), a.S.CustomEmoji.Len())
	a.RTM.OnHello = a.onHello
	go a.RTM.Run(context.Background())
	go a.pump()
	go a.prefetch(context.Background())
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for range t.C {
			_ = a.S.SaveCache()
		}
	}()
	return nil
}

// onHello runs after each realtime (re)connect: subscribe to presence and
// profile changes for the users the UI shows.
func (a *App) onHello() {
	ids := a.presenceIDs(nil)
	if err := a.RTM.SubscribePresence(ids); err != nil {
		log.Printf("rtm: presence_sub: %v", err)
	}
	if err := a.RTM.SubscribeUsers(ids); err != nil {
		log.Printf("rtm: user subscribe: %v", err)
	}
}

// presenceIDs merges add into the subscribed set (self + DM partners +
// recently visible users, newest first, capped) and returns the full set.
func (a *App) presenceIDs(add []string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.presSeen == nil {
		a.presSeen = map[string]time.Time{}
	}
	now := time.Now()
	for _, id := range add {
		if strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W") {
			a.presSeen[id] = now
		}
	}
	base := map[string]bool{a.C.Creds.UserID: true}
	a.S.RLock()
	for _, c := range a.S.Convs {
		if c.IsIM && c.User != "" {
			base[c.User] = true
		}
	}
	a.S.RUnlock()
	type kv struct {
		id string
		t  time.Time
	}
	var recent []kv
	for id, t := range a.presSeen {
		if !base[id] {
			recent = append(recent, kv{id, t})
		}
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].t.After(recent[j].t) })
	const maxSubs = 500
	out := make([]string, 0, len(base)+len(recent))
	for id := range base {
		out = append(out, id)
	}
	sort.Strings(out)
	for _, r := range recent {
		if len(out) >= maxSubs {
			delete(a.presSeen, r.id)
			continue
		}
		out = append(out, r.id)
	}
	return out
}

// prefetch warms the history of unread conversations in the background so
// switching to them is instant. Bounded concurrency, capped count.
func (a *App) prefetch(ctx context.Context) {
	t0 := time.Now()
	type cand struct {
		id     string
		latest string
		score  int
	}
	var cs []cand
	a.S.RLock()
	for id, e := range a.S.Counts {
		if !e.HasUnreads && e.MentionCount == 0 {
			continue
		}
		if _, ok := a.S.Convs[id]; !ok || a.S.Muted[id] {
			continue
		}
		cs = append(cs, cand{id, e.Latest, e.MentionCount})
	}
	a.S.RUnlock()
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].score != cs[j].score {
			return cs[i].score > cs[j].score
		}
		return store.TSLess(cs[j].latest, cs[i].latest)
	})
	const maxPrefetch = 24
	if len(cs) > maxPrefetch {
		cs = cs[:maxPrefetch]
	}
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	n := 0
	for _, c := range cs {
		if a.S.Timeline(c.id) != nil {
			continue
		}
		n++
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			_ = a.loadLatest(ctx, id)
		}(c.id)
	}
	wg.Wait()
	if n > 0 {
		log.Printf("prefetch: %d unread conversations in %s", n, time.Since(t0).Round(time.Millisecond))
	}
}

// loadLatest fetches (deduplicated) the newest page of a conversation.
func (a *App) loadLatest(ctx context.Context, ch string) error {
	return a.once("h:"+ch, func() error {
		p, err := a.C.History(ctx, ch, slack.HistoryOpts{Limit: 50})
		if err != nil {
			return err
		}
		a.ensureUsers(ctx, referencedUsers(p.Messages))
		a.S.SetLatest(ch, p.Messages, p.HasMore)
		for i := range p.Messages {
			if needsFileInfo(&p.Messages[i]) {
				go a.fillFiles(ch, p.Messages[i].TS)
			}
		}
		return nil
	})
}

func (a *App) loadUsergroups(ctx context.Context) error {
	gs, err := a.C.Usergroups(ctx)
	if err != nil {
		return err
	}
	a.S.SetUsergroups(gs)
	return nil
}

func (a *App) loadEmoji(ctx context.Context) error {
	m, err := a.C.EmojiList(ctx)
	if err != nil {
		log.Printf("emoji.list: %v", err)
		return err
	}
	a.S.CustomEmoji.Set(m)
	_ = a.S.SaveCache()
	return nil
}

// ensureUsers fetches unknown users via the edge cache (parallel batches of 100).
func (a *App) ensureUsers(ctx context.Context, ids []string) {
	miss := a.S.MissingUsers(ids)
	if len(miss) == 0 {
		return
	}
	us, err := a.C.UsersInfoParallel(ctx, miss)
	if err != nil {
		log.Printf("users/info: %v", err)
	}
	a.S.PutUsers(us)
}

func (a *App) refreshCounts(ctx context.Context) {
	if c, err := a.C.Counts(ctx); err == nil {
		a.S.SetCounts(c)
		a.Hub.Broadcast(map[string]any{"type": "counts", "counts": a.countsView(), "threads": a.threadsView()})
	}
}

// refreshCountsSoon coalesces bursts of events into one client.counts call.
func (a *App) refreshCountsSoon() {
	a.mu.Lock()
	if a.countsTimer != nil {
		a.mu.Unlock()
		return
	}
	a.countsTimer = time.AfterFunc(1500*time.Millisecond, func() {
		a.mu.Lock()
		a.countsTimer = nil
		a.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		a.refreshCounts(ctx)
	})
	a.mu.Unlock()
}

// ThreadsView is the unread state of the Threads view.
type ThreadsView struct {
	Unread   bool `json:"unread"`
	Mentions int  `json:"mentions"`
}

func (a *App) threadsView() ThreadsView {
	a.S.RLock()
	defer a.S.RUnlock()
	return ThreadsView{Unread: a.S.ThreadsUnread, Mentions: a.S.ThreadMentions}
}

// referencedUsers collects user ids a message list needs for rendering.
func referencedUsers(msgs []slack.Message) []string {
	var ids []string
	for i := range msgs {
		m := &msgs[i]
		ids = append(ids, m.User)
		ids = append(ids, m.ReplyUsers...)
		if m.Edited != nil {
			ids = append(ids, m.Edited.User)
		}
		for _, re := range m.Reactions {
			if len(re.Users) > 0 {
				ids = append(ids, re.Users...)
			}
		}
		for _, mm := range mentionRe.FindAllStringSubmatch(m.Text, -1) {
			ids = append(ids, mm[1])
		}
		if m.Root != nil {
			ids = append(ids, m.Root.User)
		}
	}
	return ids
}

func (a *App) views(ch string, msgs []slack.Message) []render.View {
	out := make([]render.View, 0, len(msgs))
	for i := range msgs {
		if msgs[i].Subtype == "thread_broadcast" || msgs[i].ThreadTS == "" || msgs[i].ThreadTS == msgs[i].TS || msgs[i].ReplyBroadcast {
			out = append(out, render.Message(a.S, ch, &msgs[i]))
		}
	}
	return out
}

func (a *App) threadViews(ch string, msgs []slack.Message) []render.View {
	out := make([]render.View, 0, len(msgs))
	for i := range msgs {
		out = append(out, render.Message(a.S, ch, &msgs[i]))
	}
	return out
}

// ---- sidebar view model ----

type ConvView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"` // channel | private | im | mpim
	User        string   `json:"user,omitempty"`
	Members     []string `json:"members,omitempty"`
	Topic       string   `json:"topic,omitempty"`
	TopicHTML   string   `json:"topic_html,omitempty"`
	Purpose     string   `json:"purpose,omitempty"`
	PurposeHTML string   `json:"purpose_html,omitempty"`
	Muted       bool     `json:"muted,omitempty"`
	NumMembers  int      `json:"num_members,omitempty"`
	IsArchived  bool     `json:"is_archived,omitempty"`
	IsGeneral   bool     `json:"is_general,omitempty"`
	IsShared    bool     `json:"is_shared,omitempty"`
	Created     int64    `json:"created,omitempty"`
	Creator     string   `json:"creator,omitempty"`
	Priority    float64  `json:"priority,omitempty"`
}

func (a *App) convView(c *slack.Conversation) ConvView {
	v := ConvView{ID: c.ID, Name: c.Name, User: c.User, NumMembers: c.NumMembers, IsArchived: c.IsArchived, IsGeneral: c.IsGeneral, IsShared: c.IsExtShared, Created: c.Created, Creator: c.Creator, Priority: c.Priority}
	switch {
	case c.IsIM:
		v.Kind = "im"
		v.Name = a.S.UserName(c.User)
	case c.IsMPIM:
		v.Kind = "mpim"
		v.Members = c.Members
	case c.IsPrivate || c.IsGroup:
		v.Kind = "private"
	default:
		v.Kind = "channel"
	}
	if c.Topic != nil {
		v.Topic = c.Topic.Value
		if v.Topic != "" {
			v.TopicHTML = render.Mrkdwn(a.S, v.Topic)
		}
	}
	if c.Purpose != nil {
		v.Purpose = c.Purpose.Value
	}
	a.S.RLock()
	v.Muted = a.S.Muted[c.ID]
	a.S.RUnlock()
	return v
}

type CountView struct {
	LastRead string `json:"last_read"`
	Latest   string `json:"latest"`
	Unread   bool   `json:"unread"`
	Mentions int    `json:"mentions"`
	Muted    bool   `json:"muted,omitempty"`
}

func countView(e slack.CountEntry, muted bool) CountView {
	return CountView{LastRead: e.LastRead, Latest: e.Latest, Unread: e.HasUnreads, Mentions: e.MentionCount, Muted: muted}
}

func (a *App) countsView() map[string]CountView {
	a.S.RLock()
	defer a.S.RUnlock()
	out := make(map[string]CountView, len(a.S.Counts))
	for id, e := range a.S.Counts {
		out[id] = countView(*e, a.S.Muted[id])
	}
	return out
}

func (a *App) countOf(ch string) CountView {
	e, _ := a.S.Count(ch)
	a.S.RLock()
	m := a.S.Muted[ch]
	a.S.RUnlock()
	return countView(e, m)
}

type SectionView struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Emoji    string   `json:"emoji,omitempty"`
	Channels []string `json:"channels"`
}

// sectionsView orders sections by their linked list and assigns every
// conversation to exactly one section (custom > default buckets).
func (a *App) sectionsView(convs []ConvView) []SectionView {
	a.S.RLock()
	secs := append([]slack.Section(nil), a.S.Sections...)
	a.S.RUnlock()
	byID := map[string]slack.Section{}
	pointed := map[string]bool{}
	for _, s := range secs {
		byID[s.ID] = s
		if s.Next != "" {
			pointed[s.Next] = true
		}
	}
	var ordered []slack.Section
	for _, s := range secs {
		if !pointed[s.ID] {
			for cur, ok := s, true; ok && len(ordered) <= len(secs); cur, ok = byID[cur.Next] {
				ordered = append(ordered, cur)
				if cur.Next == "" {
					break
				}
			}
			break
		}
	}
	// Sections not reachable from the head (broken links) still show, last.
	visited := map[string]bool{}
	for _, s := range ordered {
		visited[s.ID] = true
	}
	for _, s := range secs {
		if !visited[s.ID] {
			ordered = append(ordered, s)
			visited[s.ID] = true
		}
	}
	assigned := map[string]bool{}
	var out []SectionView
	var chIdx, dmIdx = -1, -1
	for _, s := range ordered {
		switch s.Type {
		case "salesforce_records", "slack_connect", "temporary_channels", "agents", "recent_apps":
			if len(s.ChannelIDsPage.ChannelIDs) == 0 {
				continue
			}
		}
		sv := SectionView{ID: s.ID, Name: s.Name, Type: s.Type, Emoji: s.Emoji, Channels: []string{}}
		for _, id := range s.ChannelIDsPage.ChannelIDs {
			if _, ok := a.S.Conv(id); ok && !assigned[id] {
				sv.Channels = append(sv.Channels, id)
				assigned[id] = true
			}
		}
		switch s.Type {
		case "channels":
			chIdx = len(out)
			if sv.Name == "" {
				sv.Name = "Channels"
			}
		case "direct_messages":
			dmIdx = len(out)
		case "stars":
			sv.Name = "Starred"
		case "recent_apps":
			if sv.Name == "" {
				sv.Name = "Apps"
			}
		}
		out = append(out, sv)
	}
	if chIdx < 0 {
		out = append(out, SectionView{ID: "channels", Name: "Channels", Type: "channels"})
		chIdx = len(out) - 1
	}
	if dmIdx < 0 {
		out = append(out, SectionView{ID: "dms", Name: "Direct messages", Type: "direct_messages"})
		dmIdx = len(out) - 1
	}
	out[dmIdx].Name = "Direct messages"
	var apps []string
	for _, c := range convs {
		if assigned[c.ID] {
			continue
		}
		switch c.Kind {
		case "im", "mpim":
			if u, ok := a.S.User(c.User); ok && (u.IsBot || u.ID == "USLACKBOT") && c.Kind == "im" {
				apps = append(apps, c.ID)
			} else {
				out[dmIdx].Channels = append(out[dmIdx].Channels, c.ID)
			}
		default:
			out[chIdx].Channels = append(out[chIdx].Channels, c.ID)
		}
	}
	names := map[string]string{}
	for _, c := range convs {
		names[c.ID] = strings.ToLower(c.Name)
	}
	sort.SliceStable(out[chIdx].Channels, func(i, j int) bool {
		return names[out[chIdx].Channels[i]] < names[out[chIdx].Channels[j]]
	})
	if len(apps) > 0 {
		out = append(out, SectionView{ID: "apps", Name: "Apps", Type: "apps", Channels: apps})
	}
	return out
}

// pump applies realtime events to the store and forwards them to the UI.
func (a *App) pump() {
	for ev := range a.RTM.Events {
		a.handle(ev)
	}
}

func chanID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var c struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &c)
	return c.ID
}
