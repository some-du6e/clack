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

// Boot loads everything the sidebar needs, in parallel.
func (a *App) Boot(ctx context.Context) error {
	defer close(a.ready)
	t0 := time.Now()
	a.S.LoadCache()
	var wg sync.WaitGroup
	var convs []slack.Conversation
	var counts *slack.Counts
	var sections []slack.Section
	var errConvs error
	wg.Add(4)
	go func() {
		defer wg.Done()
		convs, errConvs = a.C.Conversations(ctx)
	}()
	go func() {
		defer wg.Done()
		var err error
		if counts, err = a.C.Counts(ctx); err != nil {
			log.Printf("boot: counts: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if sections, err = a.C.Sections(ctx); err != nil {
			log.Printf("boot: sections: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		var r struct {
			Team struct {
				ID     string         `json:"id"`
				Name   string         `json:"name"`
				Domain string         `json:"domain"`
				Icon   map[string]any `json:"icon"`
			} `json:"team"`
		}
		if err := a.C.Call(ctx, "team.info", nil, &r); err == nil {
			a.S.Lock()
			a.S.TeamID, a.S.TeamName, a.S.TeamDomain = r.Team.ID, r.Team.Name, r.Team.Domain
			if s, ok := r.Team.Icon["image_88"].(string); ok {
				a.S.TeamIcon = s
			}
			a.S.Unlock()
		}
	}()
	if a.S.CustomEmoji.Len() == 0 {
		a.loadEmoji(ctx) // first run: needed before rendering
	} else {
		go a.loadEmoji(context.Background())
	}
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
	a.ensureUsers(ctx, ids)
	if u, ok := a.S.User(a.C.Creds.UserID); ok {
		a.S.Lock()
		a.S.Self = *u
		a.S.Unlock()
	}
	log.Printf("boot: %d conversations, %d sections in %s", len(convs), len(sections), time.Since(t0).Round(time.Millisecond))
	go a.RTM.Run(context.Background())
	go a.pump()
	go func() {
		for range time.Tick(5 * time.Minute) {
			_ = a.S.SaveCache()
		}
	}()
	return nil
}

func (a *App) loadEmoji(ctx context.Context) {
	m, err := a.C.EmojiList(ctx)
	if err != nil {
		log.Printf("emoji.list: %v", err)
		return
	}
	a.S.CustomEmoji.Set(m)
	_ = a.S.SaveCache()
}

// ensureUsers fetches unknown users in one edge round trip.
func (a *App) ensureUsers(ctx context.Context, ids []string) {
	miss := a.S.MissingUsers(ids)
	for len(miss) > 0 {
		n := len(miss)
		if n > 100 {
			n = 100
		}
		us, err := a.C.UsersInfo(ctx, miss[:n])
		if err != nil {
			log.Printf("users/info: %v", err)
			return
		}
		a.S.PutUsers(us)
		miss = miss[n:]
	}
}

func (a *App) refreshCounts(ctx context.Context) {
	if c, err := a.C.Counts(ctx); err == nil {
		a.S.SetCounts(c)
		a.Hub.Broadcast(map[string]any{"type": "counts", "counts": a.countsView()})
	}
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
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"` // channel | private | im | mpim
	User       string   `json:"user,omitempty"`
	Members    []string `json:"members,omitempty"`
	Topic      string   `json:"topic,omitempty"`
	TopicHTML  string   `json:"topic_html,omitempty"`
	Purpose    string   `json:"purpose,omitempty"`
	NumMembers int      `json:"num_members,omitempty"`
	IsArchived bool     `json:"is_archived,omitempty"`
	IsGeneral  bool     `json:"is_general,omitempty"`
	IsShared   bool     `json:"is_shared,omitempty"`
	Created    int64    `json:"created,omitempty"`
	Creator    string   `json:"creator,omitempty"`
	Priority   float64  `json:"priority,omitempty"`
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
	return v
}

type CountView struct {
	LastRead string `json:"last_read"`
	Latest   string `json:"latest"`
	Unread   bool   `json:"unread"`
	Mentions int    `json:"mentions"`
}

func (a *App) countsView() map[string]CountView {
	a.S.RLock()
	defer a.S.RUnlock()
	out := make(map[string]CountView, len(a.S.Counts))
	for id, e := range a.S.Counts {
		out[id] = CountView{LastRead: e.LastRead, Latest: e.Latest, Unread: e.HasUnreads, Mentions: e.MentionCount}
	}
	return out
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
	if len(ordered) == 0 {
		ordered = secs
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
		case "direct_messages":
			dmIdx = len(out)
		case "stars":
			sv.Name = "Starred"
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
