// Package store keeps the client's state in memory.
//
// Scraping policy: message contents are only ever held in memory for the
// lifetime of the process (like any Slack client). Only metadata — the user
// directory, custom emoji, and channel names — is cached on disk to make
// startup fast.
package store

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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
}

// Prefs are the user preferences the UI needs for rendering.
type Prefs struct {
	EmojiStyle        string   `json:"emoji_style"` // apple | google
	EmojiMode         string   `json:"emoji_mode,omitempty"`
	SkinTone          string   `json:"skin_tone,omitempty"`
	Time24            bool     `json:"time24"`
	DisplayRealNames  bool     `json:"display_real_names"`
	Jumbomoji         bool     `json:"jumbomoji"`
	ConvertEmoticons  bool     `json:"convert_emoticons"`
	SidebarBehavior   string   `json:"sidebar_behavior,omitempty"`
	ChannelSort       string   `json:"channel_sort,omitempty"`
	Locale            string   `json:"locale,omitempty"`
	Theme             string   `json:"theme,omitempty"`
	DarkMode          string   `json:"dark_mode,omitempty"`
	SidebarTheme      string   `json:"sidebar_theme,omitempty"`
	MessagesTheme     string   `json:"messages_theme,omitempty"`
	EnterIsSpecial    bool     `json:"enter_is_special_in_tbt"`
	ShowTyping        bool     `json:"show_typing"`
	ColorNames        bool     `json:"color_names_in_list"`
	FrequentEmoji     []string `json:"frequent_emoji,omitempty"`
	Muted             []string `json:"muted"`
	HideInactiveConvs bool     `json:"hide_inactive"`
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
		ColorNames: boolean("color_names_in_list", true),
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
				Muted bool `json:"muted"`
			} `json:"channels"`
		}
		if json.Unmarshal([]byte(s), &n) == nil {
			for id, c := range n.Channels {
				if c.Muted {
					muted[id] = true
				} else {
					delete(muted, id)
				}
			}
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
	s.Prefs = p
	s.Muted = muted
	if p.EmojiStyle != "" {
		s.EmojiStyle = p.EmojiStyle
	}
	s.mu.Unlock()
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

// ---- render.Resolver ----

func (s *Store) UserName(id string) string {
	u, _ := s.User(id)
	return DisplayName(u)
}

func (s *Store) ChannelName(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if c, ok := s.Convs[id]; ok {
		if c.IsIM {
			if u, ok := s.Users[c.User]; ok {
				return DisplayName(u)
			}
		}
		return c.Name
	}
	if n, ok := s.names[id]; ok {
		return n
	}
	return id
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

// Proxy rewrites authenticated Slack file URLs to the local web proxy. It
// only exists for the legacy HTML renderer (internal/render) and goes away
// with it; native code fetches such URLs through core.Fetch.
func (s *Store) Proxy(u string) string {
	if strings.HasPrefix(u, "https://files.slack.com/") || strings.Contains(u, ".slack.com/files") {
		return "/file?u=" + url.QueryEscape(u)
	}
	return u
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
	Users      []slack.User      `json:"users"`
	Emoji      map[string]string `json:"emoji"`
	Usergroups []slack.Usergroup `json:"usergroups,omitempty"`
	Prefs      *Prefs            `json:"prefs,omitempty"`
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
	s.Groups = s.Groups[:0]
	for _, g := range gs {
		if g.DateDelete != 0 {
			continue
		}
		s.Groups = append(s.Groups, g)
		s.Usergroups[g.ID] = g.Handle
	}
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
	s.PutUsers(d.Users)
	if len(d.Emoji) > 0 {
		s.CustomEmoji.Set(d.Emoji)
	}
	if len(d.Usergroups) > 0 {
		s.SetUsergroups(d.Usergroups)
	}
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
}

func (s *Store) SaveCache() error {
	if s.cacheDir == "" {
		return nil
	}
	s.mu.RLock()
	prefs := s.Prefs
	d := diskCache{Emoji: s.CustomEmoji.Snapshot(), Usergroups: append([]slack.Usergroup(nil), s.Groups...), Prefs: &prefs}
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
	return os.Rename(tmp, filepath.Join(s.cacheDir, "meta.json"))
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
