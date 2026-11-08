// Package store keeps the client's state in memory.
//
// Scraping policy: message contents are only ever held in memory for the
// lifetime of the process (like any Slack client). Only metadata — the user
// directory, custom emoji, and channel names — is cached on disk to make
// startup fast.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Timeline is a contiguous window of a conversation's newest messages.
type Timeline struct {
	Msgs    []slack.Message // ascending by ts
	HasMore bool            // older messages exist beyond Msgs[0]
	Loaded  bool
}

type Store struct {
	mu          sync.RWMutex
	Self        slack.User
	TeamID      string
	TeamName    string
	TeamIcon    string
	TeamDomain  string
	Users       map[string]*slack.User
	Convs       map[string]*slack.Conversation
	Sections    []slack.Section
	Counts      map[string]*slack.CountEntry
	Usergroups  map[string]string
	Presence    map[string]string
	timelines   map[string]*Timeline
	threads     map[string][]slack.Message // key channel/ts
	CustomEmoji emoji.Custom
	EmojiStyle  string
	cacheDir    string

	// Groups holds full usergroup records (Usergroups maps id -> handle).
	Groups []slack.Usergroup
	// Muted is the set of muted conversation ids (from prefs).
	Muted map[string]bool
	// Prefs is the curated set of user prefs that change rendering.
	Prefs Prefs
	// ThreadsUnread / ThreadMentions mirror client.counts "threads".
	ThreadsUnread  bool
	ThreadMentions int
	// names of conversations we are not a member of (from search/browse)
	names       map[string]string
	prefsCached bool
	ext         map[string]*slack.Conversation // non-sidebar conversations
	bots        map[string]BotInfo

	// gen counts changes to what the disk cache holds; SaveCache writes
	// only when it (or the emoji map's version) moved since the last load
	// or save.
	gen       atomic.Uint64
	saveMu    sync.Mutex
	saved     bool
	savedGen  uint64
	savedEmo  uint64
	emojiAt   time.Time // when the cached emoji map was fetched (emoji.list)
	emojiAtMu sync.Mutex

	missMu     sync.Mutex
	missSeen   map[string]bool
	missing    []string // "c"+id | "u"+id | "b"+id
	missNotify chan struct{}
}

// PutName remembers the name of a conversation outside the sidebar.
func (s *Store) PutName(id, name string) {
	if id == "" || name == "" {
		return
	}
	s.mu.Lock()
	if s.names == nil {
		s.names = map[string]string{}
	}
	s.names[id] = name
	s.mu.Unlock()
	s.gen.Add(1)
}

// Prefs are the user preferences the UI needs for rendering.
type Prefs struct {
	EmojiStyle       string `json:"emoji_style"` // apple | google
	EmojiMode        string `json:"emoji_mode,omitempty"`
	SkinTone         string `json:"skin_tone,omitempty"`
	Time24           bool   `json:"time24"`
	DisplayRealNames bool   `json:"display_real_names"`
	Jumbomoji        bool   `json:"jumbomoji"`
	ConvertEmoticons bool   `json:"convert_emoticons"`
	SidebarBehavior  string `json:"sidebar_behavior,omitempty"`
	ChannelSort      string `json:"channel_sort,omitempty"`
	Locale           string `json:"locale,omitempty"`
	Theme            string `json:"theme,omitempty"`
	DarkMode         string `json:"dark_mode,omitempty"`
	SidebarTheme     string `json:"sidebar_theme,omitempty"`
	MessagesTheme    string `json:"messages_theme,omitempty"`
	EnterIsSpecial   bool   `json:"enter_is_special_in_tbt"`
	// PlainTextMode is Preferences > Advanced > "Format messages with
	// markup": the composer keeps markup as typed (no live formatting, no
	// toolbar) and formats it when the message is sent.
	PlainTextMode     bool     `json:"plain_text_mode"`
	ShowTyping        bool     `json:"show_typing"`
	ColorNames        bool     `json:"color_names_in_list"`
	FrequentEmoji     []string `json:"frequent_emoji,omitempty"`
	Muted             []string `json:"muted"`
	HideInactiveConvs bool     `json:"hide_inactive"`
	// Notifications: global defaults and per-conversation overrides
	// (users.prefs.get all_notifications_prefs).
	GlobalDesktop string                   `json:"global_desktop,omitempty"` // everything | mentions_dms | nothing
	GlobalMobile  string                   `json:"global_mobile,omitempty"`
	ChannelNotify map[string]ChannelNotify `json:"channel_notify,omitempty"`
}

// ChannelNotify is a conversation's notification override.
type ChannelNotify struct {
	Desktop           string `json:"desktop,omitempty"` // everything | mentions_dms | nothing | default
	Mobile            string `json:"mobile,omitempty"`
	Muted             bool   `json:"muted,omitempty"`
	FollowAllThreads  bool   `json:"follow_all_threads,omitempty"`
	SuppressAtChannel bool   `json:"suppress_at_channel,omitempty"`
}

// ParsePrefs extracts Prefs (and the muted set) from users.prefs.get.
func ParsePrefs(raw map[string]json.RawMessage) (Prefs, map[string]bool) {
	str := func(k string) string {
		var v string
		_ = json.Unmarshal(raw[k], &v)
		return v
	}
	boolean := func(k string, def bool) bool {
		v, ok := raw[k]
		if !ok {
			return def
		}
		var b bool
		if json.Unmarshal(v, &b) == nil {
			return b
		}
		var n int
		if json.Unmarshal(v, &n) == nil {
			return n != 0
		}
		return def
	}
	p := Prefs{
		EmojiMode: str("emoji_mode"), SkinTone: str("preferred_skin_tone"), Time24: boolean("time24", false),
		DisplayRealNames: boolean("display_real_names_override", false) || boolean("display_real_names", false),
		Jumbomoji:        boolean("jumbomoji", true), ConvertEmoticons: boolean("convert_emoticons", true),
		SidebarBehavior: str("sidebar_behavior"), ChannelSort: str("channel_sort"), Locale: str("locale"),
		Theme: str("theme"), DarkMode: str("dark_mode"), SidebarTheme: str("sidebar_theme"), MessagesTheme: str("messages_theme"),
		EnterIsSpecial: boolean("enter_is_special_in_tbt", false), ShowTyping: boolean("show_typing", true),
		PlainTextMode: boolean("plain_text_mode", false),
		ColorNames:    boolean("color_names_in_list", true),
	}
	p.HideInactiveConvs = p.SidebarBehavior == "hide_inactive_channels"
	// Slack calls the Google set "android"/"google"; everything else is Apple.
	switch p.EmojiMode {
	case "android", "google":
		p.EmojiStyle = "google"
	default:
		p.EmojiStyle = "apple"
	}
	var use struct {
		Recent []string `json:"recent"`
	}
	if s := str("emoji_use"); s != "" {
		var counts map[string]int
		if json.Unmarshal([]byte(s), &counts) == nil {
			for n := range counts {
				use.Recent = append(use.Recent, n)
			}
			sort.Slice(use.Recent, func(i, j int) bool {
				a, b := counts[use.Recent[i]], counts[use.Recent[j]]
				if a != b {
					return a > b
				}
				return use.Recent[i] < use.Recent[j]
			})
			if len(use.Recent) > 40 {
				use.Recent = use.Recent[:40]
			}
		}
	}
	p.FrequentEmoji = use.Recent
	muted := map[string]bool{}
	for _, id := range strings.Split(str("muted_channels"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			muted[id] = true
		}
	}
	if s := str("all_notifications_prefs"); s != "" {
		var n struct {
			Channels map[string]struct {
				Desktop           string          `json:"desktop"`
				Mobile            string          `json:"mobile"`
				Muted             json.RawMessage `json:"muted"`
				FollowAllThreads  json.RawMessage `json:"follow_all_threads"`
				SuppressAtChannel json.RawMessage `json:"suppress_at_channel"`
			} `json:"channels"`
			Global map[string]json.RawMessage `json:"global"`
		}
		if json.Unmarshal([]byte(s), &n) == nil {
			truthy := func(r json.RawMessage) bool {
				v := strings.Trim(string(r), `"`)
				return v == "true" || v == "1"
			}
			p.ChannelNotify = map[string]ChannelNotify{}
			for id, c := range n.Channels {
				cn := ChannelNotify{Desktop: c.Desktop, Mobile: c.Mobile, Muted: truthy(c.Muted),
					FollowAllThreads: truthy(c.FollowAllThreads), SuppressAtChannel: truthy(c.SuppressAtChannel)}
				p.ChannelNotify[id] = cn
				if cn.Muted {
					muted[id] = true
				} else {
					delete(muted, id)
				}
			}
			var gd, gm string
			_ = json.Unmarshal(n.Global["global_desktop"], &gd)
			_ = json.Unmarshal(n.Global["global_mobile"], &gm)
			p.GlobalDesktop, p.GlobalMobile = gd, gm
		}
	}
	p.Muted = make([]string, 0, len(muted))
	for id := range muted {
		p.Muted = append(p.Muted, id)
	}
	sort.Strings(p.Muted)
	return p, muted
}

// SetPrefs installs parsed prefs (and the emoji style used for rendering).
func (s *Store) SetPrefs(p Prefs, muted map[string]bool) {
	s.mu.Lock()
	if reflect.DeepEqual(s.Prefs, p) && reflect.DeepEqual(s.Muted, muted) {
		s.mu.Unlock()
		return // refresh with no change: keep the cache clean
	}
	s.Prefs = p
	s.Muted = muted
	if p.EmojiStyle != "" {
		s.EmojiStyle = p.EmojiStyle
	}
	s.mu.Unlock()
	s.gen.Add(1)
}

func New(cacheDir string) *Store {
	return &Store{
		Users: map[string]*slack.User{}, Convs: map[string]*slack.Conversation{},
		Counts: map[string]*slack.CountEntry{}, Usergroups: map[string]string{},
		Presence:  map[string]string{},
		timelines: map[string]*Timeline{}, threads: map[string][]slack.Message{},
		cacheDir: cacheDir, EmojiStyle: "apple", Muted: map[string]bool{},
		Prefs: Prefs{EmojiStyle: "apple", Jumbomoji: true, ShowTyping: true, Muted: []string{}},
	}
}

func (s *Store) Lock()    { s.mu.Lock() }
func (s *Store) Unlock()  { s.mu.Unlock() }
func (s *Store) RLock()   { s.mu.RLock() }
func (s *Store) RUnlock() { s.mu.RUnlock() }

// ---- users ----

func (s *Store) PutUsers(us []slack.User) {
	s.mu.Lock()
	for i := range us {
		u := us[i]
		if old, ok := s.Users[u.ID]; ok && old.Presence != "" {
			u.Presence = old.Presence
		}
		s.Users[u.ID] = &u
	}
	s.mu.Unlock()
	if len(us) > 0 {
		s.gen.Add(1)
	}
}

func (s *Store) User(id string) (*slack.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.Users[id]
	return u, ok
}

// MissingUsers filters ids to those not yet known.
func (s *Store) MissingUsers(ids []string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if id == "" || seen[id] || !(strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W")) {
			continue
		}
		seen[id] = true
		if _, ok := s.Users[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

// DisplayName mirrors Slack: display_name, else real_name, else name.
func DisplayName(u *slack.User) string {
	if u == nil {
		return ""
	}
	if n := strings.TrimSpace(u.Profile.DisplayName); n != "" {
		return n
	}
	if n := strings.TrimSpace(u.Profile.RealName); n != "" && n != "​" {
		return n
	}
	if n := strings.TrimSpace(u.RealName); n != "" && n != "​" {
		return n
	}
	return u.Name
}

// ---- richtext.Resolver ----

func (s *Store) UserName(id string) string {
	u, ok := s.User(id)
	if !ok {
		s.noteMissing('u', id)
	}
	return DisplayName(u)
}

func (s *Store) ChannelName(id string) string {
	s.mu.RLock()
	c, ok := s.Convs[id]
	if !ok {
		c, ok = s.ext[id]
	}
	if ok {
		defer s.mu.RUnlock()
		if c.IsIM {
			if u, ok := s.Users[c.User]; ok {
				return DisplayName(u)
			}
			s.noteMissing('u', c.User)
		}
		return c.Name
	}
	n, named := s.names[id]
	s.mu.RUnlock()
	if named {
		return n
	}
	s.noteMissing('c', id)
	return id
}

// ---- unresolved ids (resolved in the background by core) ----

// noteMissing records an id that rendering could not name. Ids are only
// recorded once per session (see TakeMissing).
func (s *Store) noteMissing(kind byte, id string) {
	if id == "" {
		return
	}
	switch kind {
	case 'u':
		if !(strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W")) {
			return
		}
	case 'c':
		if !(strings.HasPrefix(id, "C") || strings.HasPrefix(id, "G") || strings.HasPrefix(id, "D")) {
			return
		}
	}
	key := string(kind) + id
	s.missMu.Lock()
	if s.missSeen == nil {
		s.missSeen = map[string]bool{}
		s.missNotify = make(chan struct{}, 1)
	}
	if !s.missSeen[key] {
		s.missSeen[key] = true
		s.missing = append(s.missing, key)
		select {
		case s.missNotify <- struct{}{}:
		default:
		}
	}
	s.missMu.Unlock()
}

// NoteMissingBot records a bot id whose name/icon is unknown.
func (s *Store) NoteMissingBot(id string) {
	if strings.HasPrefix(id, "B") {
		s.noteMissing('b', id)
	}
}

// MissingNotify fires (coalesced) when new unresolved ids were recorded.
func (s *Store) MissingNotify() <-chan struct{} {
	s.missMu.Lock()
	defer s.missMu.Unlock()
	if s.missNotify == nil {
		s.missSeen = map[string]bool{}
		s.missNotify = make(chan struct{}, 1)
	}
	return s.missNotify
}

// TakeMissing returns and clears the unresolved channel, user and bot ids.
func (s *Store) TakeMissing() (chans, users, bots []string) {
	s.missMu.Lock()
	keys := s.missing
	s.missing = nil
	s.missMu.Unlock()
	for _, k := range keys {
		switch k[0] {
		case 'c':
			chans = append(chans, k[1:])
		case 'u':
			users = append(users, k[1:])
		case 'b':
			bots = append(bots, k[1:])
		}
	}
	return
}

// ForgetMissing lets ids be recorded (and resolved) again, e.g. after a
// failed lookup that may succeed later.
func (s *Store) ForgetMissing(kind byte, ids ...string) {
	s.missMu.Lock()
	for _, id := range ids {
		delete(s.missSeen, string(kind)+id)
	}
	s.missMu.Unlock()
}

// ---- conversations outside the sidebar (closed DMs, previews) ----

// PutExtConv remembers a conversation the viewer is not showing in the
// sidebar (metadata, memory only).
func (s *Store) PutExtConv(c slack.Conversation) {
	if c.ID == "" {
		return
	}
	s.mu.Lock()
	if s.ext == nil {
		s.ext = map[string]*slack.Conversation{}
	}
	s.ext[c.ID] = &c
	if c.Name != "" && !c.IsIM {
		if s.names == nil {
			s.names = map[string]string{}
		}
		s.names[c.ID] = c.Name
		s.gen.Add(1)
	}
	s.mu.Unlock()
}

// ExtConv returns a non-sidebar conversation.
func (s *Store) ExtConv(id string) (*slack.Conversation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.ext[id]
	return c, ok
}

// ---- bots ----

// BotInfo is a bot's display metadata (bots.info), cached on disk.
type BotInfo struct {
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
}

func (s *Store) PutBot(id string, b BotInfo) {
	s.mu.Lock()
	if s.bots == nil {
		s.bots = map[string]BotInfo{}
	}
	s.bots[id] = b
	s.mu.Unlock()
	s.gen.Add(1)
}

func (s *Store) Bot(id string) (BotInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.bots[id]
	return b, ok
}

func (s *Store) UsergroupName(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n, ok := s.Usergroups[id]; ok {
		return n
	}
	return id
}

func (s *Store) Emoji(name string) (string, string, bool) {
	s.mu.RLock()
	style := s.EmojiStyle
	s.mu.RUnlock()
	return s.CustomEmoji.Resolve(name, style)
}

func (s *Store) SelfID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Self.ID
}

// ---- conversations ----

func (s *Store) PutConv(c slack.Conversation) {
	s.mu.Lock()
	if old, ok := s.Convs[c.ID]; ok {
		if c.NumMembers == 0 {
			c.NumMembers = old.NumMembers
		}
		if c.LastRead == "" {
			c.LastRead = old.LastRead
		}
	}
	s.Convs[c.ID] = &c
	s.mu.Unlock()
}

func (s *Store) Conv(id string) (*slack.Conversation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.Convs[id]
	return c, ok
}

// ---- timelines ----

func (s *Store) Timeline(ch string) *Timeline {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t := s.timelines[ch]
	if t == nil {
		return nil
	}
	cp := *t
	cp.Msgs = append([]slack.Message(nil), t.Msgs...)
	return &cp
}

// TimelineIDs lists the conversations with a cached newest window.
func (s *Store) TimelineIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.timelines))
	for id := range s.timelines {
		ids = append(ids, id)
	}
	return ids
}

// SetLatest replaces the newest window (from a fresh history fetch).
func (s *Store) SetLatest(ch string, msgs []slack.Message, hasMore bool) {
	SortAsc(msgs)
	s.mu.Lock()
	s.timelines[ch] = &Timeline{Msgs: msgs, HasMore: hasMore, Loaded: true}
	s.mu.Unlock()
}

// PrependOlder adds an older page to the front.
func (s *Store) PrependOlder(ch string, msgs []slack.Message, hasMore bool) {
	SortAsc(msgs)
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.timelines[ch]
	if t == nil {
		s.timelines[ch] = &Timeline{Msgs: msgs, HasMore: hasMore, Loaded: true}
		return
	}
	merged := append(append([]slack.Message(nil), msgs...), t.Msgs...)
	t.Msgs = dedupe(merged)
	t.HasMore = hasMore
	// bound memory per channel
	if len(t.Msgs) > 5000 {
		t.Msgs = t.Msgs[len(t.Msgs)-5000:]
	}
}

// Upsert inserts or replaces a top-level message in a loaded timeline.
func (s *Store) Upsert(ch string, m slack.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.timelines[ch]
	if t == nil {
		return
	}
	for i := range t.Msgs {
		if t.Msgs[i].TS == m.TS {
			t.Msgs[i] = m
			return
		}
	}
	t.Msgs = append(t.Msgs, m)
	SortAsc(t.Msgs)
}

// Mutate applies f to a message in the timeline and its thread cache.
func (s *Store) Mutate(ch, ts string, f func(*slack.Message)) (slack.Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out slack.Message
	found := false
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			if t.Msgs[i].TS == ts {
				f(&t.Msgs[i])
				out, found = t.Msgs[i], true
			}
		}
	}
	for k, th := range s.threads {
		if !strings.HasPrefix(k, ch+"/") {
			continue
		}
		for i := range th {
			if th[i].TS == ts {
				f(&th[i])
				if !found {
					out, found = th[i], true
				}
			}
		}
	}
	return out, found
}

func (s *Store) Remove(ch, ts string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.timelines[ch]; t != nil {
		for i := range t.Msgs {
			if t.Msgs[i].TS == ts {
				t.Msgs = append(t.Msgs[:i], t.Msgs[i+1:]...)
				break
			}
		}
	}
	for k, th := range s.threads {
		if !strings.HasPrefix(k, ch+"/") {
			continue
		}
		for i := range th {
			if th[i].TS == ts {
				s.threads[k] = append(th[:i], th[i+1:]...)
				break
			}
		}
	}
}

func (s *Store) Find(ch, ts string) (slack.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if t := s.timelines[ch]; t != nil {
		for _, m := range t.Msgs {
			if m.TS == ts {
				return m, true
			}
		}
	}
	for k, th := range s.threads {
		if strings.HasPrefix(k, ch+"/") {
			for _, m := range th {
				if m.TS == ts {
					return m, true
				}
			}
		}
	}
	return slack.Message{}, false
}

// ---- threads ----

func (s *Store) Thread(ch, ts string) ([]slack.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	th, ok := s.threads[ch+"/"+ts]
	return append([]slack.Message(nil), th...), ok
}

func (s *Store) SetThread(ch, ts string, msgs []slack.Message) {
	SortAsc(msgs)
	s.mu.Lock()
	s.threads[ch+"/"+ts] = msgs
	s.mu.Unlock()
}

// AddReply appends a reply to a cached thread (if open) and bumps the parent.
func (s *Store) AddReply(ch string, m slack.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ch + "/" + m.ThreadTS
	if th, ok := s.threads[key]; ok {
		replaced := false
		for i := range th {
			if th[i].TS == m.TS {
				th[i] = m
				replaced = true
			}
		}
		if !replaced {
			th = append(th, m)
			SortAsc(th)
		}
		s.threads[key] = th
	}
}

// ---- counts ----

func (s *Store) SetCounts(c *slack.Counts) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Counts = map[string]*slack.CountEntry{}
	for _, l := range [][]slack.CountEntry{c.Channels, c.IMs, c.MPIMs} {
		for i := range l {
			e := l[i]
			s.Counts[e.ID] = &e
		}
	}
	s.ThreadsUnread = c.Threads.HasUnreads
	s.ThreadMentions = c.Threads.MentionCount
}

// Count returns a copy of a conversation's unread state.
func (s *Store) Count(ch string) (slack.CountEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.Counts[ch]; ok {
		return *e, true
	}
	return slack.CountEntry{ID: ch}, false
}

// MarkRead moves a conversation's read cursor to ts. Marking at or after
// the latest message clears it; marking earlier (mark unread) leaves it
// unread. Mentions before the new cursor are unknown locally, so they are
// cleared on read and kept on mark-unread until counts are refreshed.
func (s *Store) MarkRead(ch, ts string) slack.CountEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.Counts[ch]
	if !ok {
		e = &slack.CountEntry{ID: ch}
		s.Counts[ch] = e
	}
	e.LastRead = ts
	if e.Latest == "" || !TSLess(ts, e.Latest) {
		e.HasUnreads = false
		e.MentionCount = 0
	} else {
		e.HasUnreads = true
	}
	return *e
}

// NoteMessage updates unread state for a new top-level message (or
// broadcast) in ch. DMs count every message from others as a mention, the
// way Slack badges them.
func (s *Store) NoteMessage(ch string, m *slack.Message, mentionsSelf bool) slack.CountEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.Counts[ch]
	if !ok {
		e = &slack.CountEntry{ID: ch}
		s.Counts[ch] = e
	}
	if e.Latest == "" || TSLess(e.Latest, m.TS) {
		e.Latest = m.TS
	}
	self := s.Self.ID
	if m.User == self && self != "" {
		// Our own message (from any client) reads the conversation.
		e.LastRead, e.HasUnreads, e.MentionCount = m.TS, false, 0
		return *e
	}
	if e.LastRead != "" && !TSLess(e.LastRead, m.TS) {
		return *e
	}
	e.HasUnreads = true
	c := s.Convs[ch]
	isDM := c != nil && (c.IsIM || c.IsMPIM) || strings.HasPrefix(ch, "D")
	if mentionsSelf || isDM {
		e.MentionCount++
	}
	return *e
}

// ---- disk cache (metadata only) ----

type diskCache struct {
	Users []slack.User      `json:"users"`
	Emoji map[string]string `json:"emoji,omitempty"` // caches written before 2026-11-07
	// EmojiPacked is the custom emoji map as "name\nvalue\n" pairs. Loaded
	// as one string whose substrings become the map's keys and values: one
	// allocation instead of 130k, and ~10x faster to parse than an object.
	EmojiPacked string             `json:"emoji_packed,omitempty"`
	EmojiAt     int64              `json:"emoji_at,omitempty"` // unix time of the emoji.list it came from
	Usergroups  []slack.Usergroup  `json:"usergroups,omitempty"`
	Prefs       *Prefs             `json:"prefs,omitempty"`
	Names       map[string]string  `json:"channel_names,omitempty"` // channels outside the sidebar
	Bots        map[string]BotInfo `json:"bots,omitempty"`
}

// sameGroups reports whether gs (minus deleted groups) equals s.Groups.
func (s *Store) sameGroups(gs []slack.Usergroup) bool {
	i := 0
	for _, g := range gs {
		if g.DateDelete != 0 {
			continue
		}
		if i >= len(s.Groups) || s.Groups[i] != g {
			return false
		}
		i++
	}
	return i == len(s.Groups)
}

// HasCachedPrefs reports whether prefs came from the disk cache.
func (s *Store) HasCachedPrefs() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prefsCached
}

// SetUsergroups installs usergroups (for <!subteam^> names and autocomplete).
func (s *Store) SetUsergroups(gs []slack.Usergroup) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sameGroups(gs) {
		return // the usual refresh: nothing to install or save
	}
	s.Groups = s.Groups[:0]
	for _, g := range gs {
		if g.DateDelete != 0 {
			continue
		}
		s.Groups = append(s.Groups, g)
		s.Usergroups[g.ID] = g.Handle
	}
	s.gen.Add(1)
}

func (s *Store) LoadCache() {
	if s.cacheDir == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(s.cacheDir, "meta.json"))
	if err != nil {
		return
	}
	var d diskCache
	if json.Unmarshal(b, &d) != nil {
		return
	}
	b = nil
	s.PutUsers(d.Users)
	if d.EmojiPacked != "" {
		s.CustomEmoji.Set(unpackEmoji(d.EmojiPacked))
	} else if len(d.Emoji) > 0 {
		s.CustomEmoji.Set(d.Emoji)
	}
	if d.EmojiAt > 0 {
		s.emojiAtMu.Lock()
		s.emojiAt = time.Unix(d.EmojiAt, 0)
		s.emojiAtMu.Unlock()
	}
	if len(d.Usergroups) > 0 {
		s.SetUsergroups(d.Usergroups)
	}
	s.mu.Lock()
	if len(d.Names) > 0 {
		if s.names == nil {
			s.names = map[string]string{}
		}
		for k, v := range d.Names {
			s.names[k] = v
		}
	}
	if len(d.Bots) > 0 {
		s.bots = d.Bots
	}
	s.mu.Unlock()
	if d.Prefs != nil {
		muted := map[string]bool{}
		for _, id := range d.Prefs.Muted {
			muted[id] = true
		}
		s.SetPrefs(*d.Prefs, muted)
		s.mu.Lock()
		s.prefsCached = true
		s.mu.Unlock()
	}
	// what was just read is what is on disk
	s.saveMu.Lock()
	s.saved, s.savedGen, s.savedEmo = true, s.gen.Load(), s.CustomEmoji.Version()
	s.saveMu.Unlock()
}

// SetEmojiList installs a fresh emoji.list result, compacted the way the
// disk cache stores it (see diskCache.EmojiPacked), and stamps its age.
func (s *Store) SetEmojiList(m map[string]string) {
	s.CustomEmoji.Set(unpackEmoji(packEmoji(m)))
	s.emojiAtMu.Lock()
	s.emojiAt = time.Now()
	s.emojiAtMu.Unlock()
}

// EmojiAge is how old the custom emoji map is (a large value when unknown).
func (s *Store) EmojiAge() time.Duration {
	s.emojiAtMu.Lock()
	defer s.emojiAtMu.Unlock()
	if s.emojiAt.IsZero() {
		return 1 << 62
	}
	return time.Since(s.emojiAt)
}

func packEmoji(m map[string]string) string {
	n := 0
	for k, v := range m {
		n += len(k) + len(v) + 2
	}
	var b strings.Builder
	b.Grow(n)
	for k, v := range m {
		if strings.ContainsRune(k, '\n') || strings.ContainsRune(v, '\n') {
			continue
		}
		b.WriteString(k)
		b.WriteByte('\n')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return b.String()
}

// unpackEmoji builds the map from packed pairs; keys and values share p's
// memory.
func unpackEmoji(p string) map[string]string {
	m := make(map[string]string, strings.Count(p, "\n")/2)
	for len(p) > 0 {
		i := strings.IndexByte(p, '\n')
		if i < 0 {
			break
		}
		k := p[:i]
		p = p[i+1:]
		j := strings.IndexByte(p, '\n')
		if j < 0 {
			break
		}
		m[k] = p[:j]
		p = p[j+1:]
	}
	return m
}

func (s *Store) SaveCache() error {
	if s.cacheDir == "" {
		return nil
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	gen, emo := s.gen.Load(), s.CustomEmoji.Version()
	if s.saved && gen == s.savedGen && emo == s.savedEmo {
		return nil // unchanged since the last load or save
	}
	s.emojiAtMu.Lock()
	emojiAt := s.emojiAt
	s.emojiAtMu.Unlock()
	s.mu.RLock()
	prefs := s.Prefs
	d := diskCache{EmojiPacked: packEmoji(s.CustomEmoji.Snapshot()), Usergroups: append([]slack.Usergroup(nil), s.Groups...), Prefs: &prefs,
		Names: map[string]string{}, Bots: map[string]BotInfo{}}
	if !emojiAt.IsZero() {
		d.EmojiAt = emojiAt.Unix()
	}
	for k, v := range s.names {
		d.Names[k] = v
	}
	for k, v := range s.bots {
		d.Bots[k] = v
	}
	for _, u := range s.Users {
		d.Users = append(d.Users, *u)
	}
	s.mu.RUnlock()
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.cacheDir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(s.cacheDir, "meta.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(s.cacheDir, "meta.json")); err != nil {
		return err
	}
	s.saved, s.savedGen, s.savedEmo = true, gen, emo
	return nil
}

func SortAsc(m []slack.Message) {
	sort.SliceStable(m, func(i, j int) bool { return TSLess(m[i].TS, m[j].TS) })
}

// TSLess compares Slack timestamps ("1234567890.123456").
func TSLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func dedupe(m []slack.Message) []slack.Message {
	SortAsc(m)
	out := m[:0]
	for i := range m {
		if i > 0 && m[i].TS == out[len(out)-1].TS {
			out[len(out)-1] = m[i]
			continue
		}
		out = append(out, m[i])
	}
	return out
}
