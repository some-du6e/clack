package core

import (
	"encoding/json"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/store"
)

// The header cache: what each conversation's header read last (the tab
// bar, the facepile's first people, the member count), kept on disk
// (store.HeadersFile) for the channels opened most recently. Slack's
// header renders from its persisted store on a warm boot (the channel's
// properties.tabs, the files store for the tab titles, membershipCounts
// and membershipOrdered are all persistenceEnabled); its bookmarksStore
// isn't, but Clack's folder tabs carry their bookmarks from the same read
// (Tabs), so they're kept with the tab. The UI shows a cached header at
// once and its read revalidates it (ui backend.opened).

// ChannelHeader is one conversation's header as its last read left it.
type ChannelHeader struct {
	Tabs []Tab `json:"tabs"`
	// NotMember: conversations.info said the viewer isn't in it (the
	// tab bar marks no canvas unread then).
	NotMember bool     `json:"not_member,omitempty"`
	Members   []string `json:"members,omitempty"` // the facepile's first people
	Num       int      `json:"num,omitempty"`     // num_members (0: not read)
	At        int64    `json:"at_ms"`             // when it was read, unix milliseconds
}

// headerKeep is how many conversations' headers the cache keeps (the most
// recently read).
const headerKeep = 100

type headerCache struct {
	mu     sync.Mutex
	loaded bool
	m      map[string]ChannelHeader
	dirty  bool // changed since the last write (saveWarm writes it)
}

type headersDisk struct {
	V        int                      `json:"v"`
	Team     string                   `json:"team"`
	User     string                   `json:"user"`
	Channels map[string]ChannelHeader `json:"channels"`
}

// loadHeaders reads headers.json for the account, once (Boot's warm
// load); with no account ("", "": CLACK_NO_WARM_BOOT) it reads nothing,
// and the cache holds only this session's reads.
func (a *App) loadHeaders(team, user string) {
	h := &a.warm.headers
	var d headersDisk
	var ok bool
	if team != "" || user != "" {
		b, err := a.S.ReadFile(store.HeadersFile)
		ok = err == nil && json.Unmarshal(b, &d) == nil && d.V == store.CacheVersion && d.Team == team && d.User == user
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.loaded {
		return
	}
	h.loaded = true
	if h.m == nil {
		h.m = map[string]ChannelHeader{}
	}
	if !ok {
		return
	}
	for ch, e := range d.Channels {
		if _, newer := h.m[ch]; !newer { // read this session already
			h.m[ch] = e
		}
	}
}

// CachedHeader is ch's header as its last read left it, this session or an
// earlier one (ok false when none is kept, or before Boot read the disk).
// Never waits: the UI goroutine calls it.
func (a *App) CachedHeader(ch string) (ChannelHeader, bool) {
	h := &a.warm.headers
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.m[ch]
	return e, ok
}

// SetCachedHeader records what ch's header read (the UI, after a read that
// answered). It is written with the boot data (saveWarm: 20 s after boot,
// then every 5 minutes, and when the session closes).
func (a *App) SetCachedHeader(ch string, e ChannelHeader) {
	if ch == "" {
		return
	}
	if e.At == 0 {
		e.At = time.Now().UnixMilli()
	}
	h := &a.warm.headers
	h.mu.Lock()
	if h.m == nil {
		h.m = map[string]ChannelHeader{}
	}
	h.m[ch] = e
	h.dirty = true
	h.mu.Unlock()
}

// saveHeaders writes headers.json when a read changed it: the headerKeep
// most recently read headers. Never after sign-out. Disk I/O: off the UI
// goroutine.
func (a *App) saveHeaders() {
	if a.S.CacheDir() == "" || a.S.Dropped() || a.C.SignedOut() {
		return
	}
	h := &a.warm.headers
	h.mu.Lock()
	if !h.dirty {
		h.mu.Unlock()
		return
	}
	h.dirty = false
	if len(h.m) > headerKeep {
		ids := make([]string, 0, len(h.m))
		for id := range h.m {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if a, b := h.m[ids[i]].At, h.m[ids[j]].At; a != b {
				return a > b
			}
			return ids[i] < ids[j]
		})
		for _, id := range ids[headerKeep:] {
			delete(h.m, id)
		}
	}
	team, user := a.bootAccount()
	d := headersDisk{V: store.CacheVersion, Team: team, User: user, Channels: make(map[string]ChannelHeader, len(h.m))}
	for k, v := range h.m {
		d.Channels[k] = v
	}
	h.mu.Unlock()
	b, err := json.Marshal(&d)
	if err != nil {
		return
	}
	if err := a.S.WriteFile(store.HeadersFile, b); err != nil {
		log.Printf("cache: headers: %v", err)
	}
}
