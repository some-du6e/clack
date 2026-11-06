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
	"sort"
	"strings"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
	"github.com/some-du6e/yoloslackgoclient/internal/render"
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
}

func New(cacheDir string) *Store {
	return &Store{
		Users: map[string]*slack.User{}, Convs: map[string]*slack.Conversation{},
		Counts: map[string]*slack.CountEntry{}, Usergroups: map[string]string{},
		Presence:  map[string]string{},
		timelines: map[string]*Timeline{}, threads: map[string][]slack.Message{},
		cacheDir: cacheDir, EmojiStyle: "apple",
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
	return s.CustomEmoji.Resolve(name, s.EmojiStyle)
}

// Proxy rewrites authenticated Slack file URLs to the local proxy.
func (s *Store) Proxy(u string) string { return render.ProxyURL(u) }

func (s *Store) SelfID() string { return s.Self.ID }

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
}

// ---- disk cache (metadata only) ----

type diskCache struct {
	Users []slack.User      `json:"users"`
	Emoji map[string]string `json:"emoji"`
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
}

func (s *Store) SaveCache() error {
	if s.cacheDir == "" {
		return nil
	}
	s.mu.RLock()
	d := diskCache{Emoji: s.CustomEmoji.Snapshot()}
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
