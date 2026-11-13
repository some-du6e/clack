package store

import (
	"math/rand/v2"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Slack's members cache, as its web client keeps it (client-boot, build
// 0e695dd18302fa08, captured 2026-11-12):
//
//   - gIKW: the members CacheManager is {cacheSizeMaximum: 700,
//     cacheSizeTarget: 500, evictionEnabled, comparator:
//     compare_members_store_cache_objects, getObjects:
//     getMembersForCacheEviction (every member object in the store),
//     removeObjects: bulkRemoveMembersById({ids, isCacheEviction}), label
//     "members"}. getMemberById records a hit (lastAccess[id] = now)
//     when the member is in the store; otherwise it records a miss and
//     asks the UnknownFetchManager for the member (fetchObjectWithId),
//     which hands back an unknown member until users/info answers
//     (core/members.go). getRawMemberById, hasMemberWithId and the
//     selectors over every member (getMemberByName's search) record
//     nothing.
//   - compare_members_store_cache_objects: the viewer (is_self) sorts
//     last, everyone else by lastAccess, oldest first; a member not found
//     since it was stored counts as 0. Ties keep the store's order (the
//     sort is stable over Object.keys, the order members were added).
//   - WcSL CacheManager: every recordHit and recordMiss checks whether
//     evictionInterval (an hour with ±20% jitter, drawn once: pvck
//     addJitter(36e5, -.2, .2)) has passed since the last check, the
//     client is online (mG45 isOnline: clientOnline && serviceOnline)
//     and no pass is scheduled; then evict() runs in an idle callback
//     (requestIdleCallback, timeout 60 s), which first sets the last
//     check to now. evict() does nothing while the store holds fewer
//     than 700 members; otherwise it sorts them by the comparator, keeps
//     the last 500 (takeItemsToEvict) and removes the rest, and forgets
//     their lastAccess.
//   - Gx04 UnknownFetchManager: ids users/info didn't return are stored as
//     non-existent members (makeNonExistentMember: no names, the unknown
//     avatar), so they aren't asked for again; they count, sort and are
//     evicted like the others.
//
// So what stays is the viewer and whoever something read last: Slack's
// mounted components read their members through getMemberById on every
// store change (the sidebar's DM rows, the message rows on screen, the
// typing line, a huddle's people, an open profile), and Clack's views
// read them through User as they draw. Nothing else is pinned.
const (
	MembersMaximum  = 700       // cacheSizeMaximum
	MembersTarget   = 500       // cacheSizeTarget
	MembersInterval = time.Hour // evictionInterval, before its jitter
)

// membersJitter draws the evictionInterval's jitter, in [-0.2, 0.2)
// (tests replace it).
var membersJitter = func() float64 { return rand.Float64()*0.4 - 0.2 }

// memberCache is the members CacheManager's state. access and nonexistent
// change under Store.mu's write lock; the cells in access are written
// atomically under its read lock (every lookup records one).
type memberCache struct {
	once      sync.Once
	maximum   atomic.Int64 // cacheSizeMaximum (0: MembersMaximum)
	target    atomic.Int64 // cacheSizeTarget (-1 is 0; 0: MembersTarget)
	interval  int64        // evictionInterval (ms), drawn once
	lastCheck atomic.Int64 // lastEvictionCheckTimestamp (unix ms)
	scheduled atomic.Bool  // scheduledEvictionCallback
	due       chan struct{}

	// access is lastAccess (unix ms; 0: not found since it was stored)
	// for every member and non-existent id the store holds
	access map[string]*atomic.Int64
	// nonexistent are the ids users/info didn't return
	nonexistent map[string]bool
}

// membersInit is the CacheManager's constructor, at the first lookup
// (Slack makes the manager at the first getMemberById with a team).
func (s *Store) membersInit() *memberCache {
	m := &s.members
	m.once.Do(func() {
		m.interval = int64(float64(MembersInterval.Milliseconds()) * (1 + membersJitter()))
		m.lastCheck.Store(time.Now().UnixMilli())
		m.due = make(chan struct{}, 1)
	})
	return m
}

// SetMembersOnline sets mG45 isOnline for the members pass (core: the
// machine has a network and the session wasn't ended). nil: online.
func (s *Store) SetMembersOnline(f func() bool) {
	s.mu.Lock()
	s.membersOnline = f
	s.mu.Unlock()
}

// touchLocked records a hit on id (s.mu held, read or write).
func (s *Store) touchLocked(id string, now int64) {
	if c := s.members.access[id]; c != nil {
		c.Store(now)
	}
}

// cellLocked makes id's lastAccess cell (s.mu held for writing). A
// member stored again keeps its cell.
func (s *Store) cellLocked(id string) {
	if s.members.access == nil {
		s.members.access = map[string]*atomic.Int64{}
	}
	if s.members.access[id] == nil {
		s.members.access[id] = new(atomic.Int64)
	}
}

// lookupLocked is getMemberById's read of the store (s.mu held): the
// member, or known (with no member) when users/info said there is none,
// recording the hit either way.
func (s *Store) lookupLocked(id string, now int64) (u *slack.User, known bool) {
	if v, ok := s.Users[id]; ok {
		s.touchLocked(id, now)
		return v, true
	}
	if s.members.nonexistent[id] {
		s.touchLocked(id, now)
		return nil, true
	}
	return nil, false
}

// afterLookup is the rest of getMemberById once the lock is let go: a
// miss (an id the store doesn't know at all) asks for the member
// (noteMissing: core fetches it and emits "names"), and either way the
// manager checks whether its pass is due.
func (s *Store) afterLookup(id string, known bool) {
	if !known {
		s.noteMissing('u', id)
	}
	s.MaybeScheduleMembers()
}

// MaybeScheduleMembers is maybeScheduleEviction: when the interval has
// passed since the last check, the client is online and no pass is
// waiting, it schedules one (MembersDue fires). Every lookup calls it;
// core's cache loop does too, standing in for the reads Slack's mounted
// components make on every store change while Clack draws nothing.
func (s *Store) MaybeScheduleMembers() {
	m := s.membersInit()
	if time.Now().UnixMilli() < m.lastCheck.Load()+m.interval || m.scheduled.Load() {
		return
	}
	s.mu.RLock()
	online := s.membersOnline
	s.mu.RUnlock()
	if online != nil && !online() {
		return
	}
	if !m.scheduled.CompareAndSwap(false, true) {
		return
	}
	select {
	case m.due <- struct{}{}:
	default:
	}
}

// MembersDue fires when a members pass is scheduled; core runs it
// (RunMembersPass) in the background, as Slack's idle callback does.
func (s *Store) MembersDue() <-chan struct{} {
	return s.membersInit().due
}

// RunMembersPass is the scheduled callback: the last check becomes now,
// evict runs, and the next pass can be scheduled an interval later. It
// returns the ids it removed.
func (s *Store) RunMembersPass(now time.Time) []string {
	m := s.membersInit()
	m.lastCheck.Store(now.UnixMilli())
	defer m.scheduled.Store(false)
	return s.EvictMembers()
}

// SetMembersSizeForDebug is the CacheManager's setCacheSizeMaximumForDebug
// and setCacheSizeTargetForDebug (Slack's cacheEviction debug hook;
// tests): EvictMembers then trims to these. Negative restores Slack's.
func (s *Store) SetMembersSizeForDebug(maximum, target int) {
	m := &s.members
	if maximum < 0 || target < 0 {
		m.maximum.Store(0)
		m.target.Store(0)
		return
	}
	m.maximum.Store(int64(maximum))
	if target == 0 {
		m.target.Store(-1)
	} else {
		m.target.Store(int64(target))
	}
}

func (m *memberCache) limits() (maximum, target int) {
	maximum, target = MembersMaximum, MembersTarget
	if v := m.maximum.Load(); v > 0 {
		maximum = int(v)
	}
	switch v := m.target.Load(); {
	case v < 0:
		target = 0
	case v > 0:
		target = int(v)
	}
	return maximum, target
}

// MembersStats is what the members cache holds: members (users) and
// non-existent ids, as getMembersForCacheEviction counts them.
func (s *Store) MembersStats() (users, nonexistent int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Users), len(s.members.nonexistent)
}

// EvictMembers is the members CacheManager's evict(): once the store holds
// MembersMaximum members (non-existent ones included), it keeps the
// MembersTarget the comparator puts last (the viewer, then whoever was
// found most recently) and removes the rest. It returns the removed ids;
// a later lookup of one asks users/info again.
func (s *Store) EvictMembers() []string {
	maximum, target := s.members.limits()
	s.mu.Lock()
	n := len(s.Users) + len(s.members.nonexistent)
	if n < maximum {
		s.mu.Unlock()
		return nil
	}
	type cand struct {
		id     string
		self   bool
		access int64
		seen   int64 // when the store got it: the store's order for ties
	}
	cs := make([]cand, 0, n)
	self := s.Self.ID
	add := func(id string) {
		var at int64
		if c := s.members.access[id]; c != nil {
			at = c.Load()
		}
		cs = append(cs, cand{id: id, self: id == self, access: at, seen: s.userSeen[id]})
	}
	for id := range s.Users {
		add(id)
	}
	for id := range s.members.nonexistent {
		add(id)
	}
	// compare_members_store_cache_objects: is_self last, else lastAccess
	// ascending; equal ones in the order the store got them
	sort.Slice(cs, func(i, j int) bool {
		a, b := &cs[i], &cs[j]
		if a.self != b.self {
			return b.self
		}
		if a.access != b.access {
			return a.access < b.access
		}
		if a.seen != b.seen {
			return a.seen < b.seen
		}
		return a.id < b.id
	})
	// takeItemsToEvict: keep the last cacheSizeTarget
	cut := max(len(cs)-target, 0)
	out := make([]string, 0, cut)
	for _, c := range cs[:cut] {
		out = append(out, c.id)
		delete(s.Users, c.id)
		delete(s.userSeen, c.id)
		delete(s.members.nonexistent, c.id)
		delete(s.members.access, c.id) // forget
	}
	// Go's maps keep the room of their largest size: a pass that let most
	// of them go copies what's left into maps of its size (as Slack's
	// removeObjects makes a new object for the store)
	if cut > len(cs)-cut {
		s.Users = resized(s.Users)
		s.userSeen = resized(s.userSeen)
		s.members.access = resized(s.members.access)
		s.members.nonexistent = resized(s.members.nonexistent)
	}
	s.mu.Unlock()
	s.ForgetMissing('u', out...)
	if cut > len(cs)-cut {
		s.missMu.Lock()
		s.missSeen = resized(s.missSeen)
		s.missMu.Unlock()
	}
	s.gen.Add(1)
	slices.Sort(out)
	return out
}

// resized copies m into a map made for its length (maps.Clone may keep
// the old one's room).
func resized[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return nil
	}
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// PutAbsentUsers is the UnknownFetchManager's upsertKnown
// (upsertMembersForTeamId): only members the store doesn't hold yet,
// so an answer never replaces a newer copy that arrived meanwhile (a
// user_change, another fetch).
func (s *Store) PutAbsentUsers(us []slack.User) {
	if len(us) == 0 {
		return
	}
	s.mu.RLock()
	add := make([]slack.User, 0, len(us))
	for _, u := range us {
		if _, ok := s.Users[u.ID]; !ok && !s.members.nonexistent[u.ID] && u.ID != "" {
			add = append(add, u)
		}
	}
	s.mu.RUnlock()
	s.PutUsers(add)
}

// PutNonexistent stores ids users/info didn't return as non-existent
// members (upsertNonExistent: only ids the store doesn't hold).
func (s *Store) PutNonexistent(ids []string) {
	if len(ids) == 0 {
		return
	}
	s.mu.Lock()
	for _, id := range ids {
		if _, ok := s.Users[id]; ok || id == "" {
			continue
		}
		if s.members.nonexistent == nil {
			s.members.nonexistent = map[string]bool{}
		}
		s.members.nonexistent[id] = true
		s.cellLocked(id)
	}
	s.mu.Unlock()
}

// Nonexistent reports whether users/info said there is no member id
// (isNonExistentMember).
func (s *Store) Nonexistent(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.members.nonexistent[id]
}

// MemberAccessed is when a lookup last found id (zero: not since it was
// stored, or not stored).
func (s *Store) MemberAccessed(id string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if c := s.members.access[id]; c != nil {
		if ms := c.Load(); ms != 0 {
			return time.UnixMilli(ms)
		}
	}
	return time.Time{}
}

// TouchMember records a hit on id if the store holds it: what a view
// that drew the member from what it resolved earlier calls as it draws,
// standing in for the getMemberById its Slack component makes on every
// render. It asks for nothing.
func (s *Store) TouchMember(id string) {
	if id == "" {
		return
	}
	now := time.Now().UnixMilli()
	s.mu.RLock()
	s.touchLocked(id, now)
	s.mu.RUnlock()
}
