package store

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

func membersStore(n int) *Store {
	s := New("")
	s.Self.ID = "USELF"
	us := []slack.User{{ID: "USELF", Name: "me"}}
	for i := range n - 1 {
		us = append(us, slack.User{ID: fmt.Sprintf("U%07d", i), Name: fmt.Sprintf("u%d", i)})
	}
	s.PutUsers(us)
	return s
}

// Slack's members CacheManager does nothing while the store holds fewer
// than cacheSizeMaximum (700) members.
func TestMembersUnderMaximum(t *testing.T) {
	s := membersStore(MembersMaximum - 1)
	if out := s.EvictMembers(); out != nil {
		t.Fatalf("evicted %d under the maximum", len(out))
	}
	if n, _ := s.MembersStats(); n != MembersMaximum-1 {
		t.Fatalf("%d members left", n)
	}
}

// At the maximum it keeps cacheSizeTarget (500): the viewer always
// (is_self sorts last), then whoever lookups found most recently;
// members never found since they were stored go first, the earliest
// stored first. An evicted member is asked for again at its next lookup.
func TestMembersEvictionOrder(t *testing.T) {
	s := membersStore(1000)
	// the store got them in order: never-found ones go oldest first
	s.mu.Lock()
	for i := range 999 {
		s.userSeen[fmt.Sprintf("U%07d", i)] = int64(1000 + i)
	}
	s.mu.Unlock()
	// 600 found, U0000000 first and U0000599 last
	for i := range 600 {
		id := fmt.Sprintf("U%07d", i)
		if _, ok := s.User(id); !ok {
			t.Fatalf("%s missing", id)
		}
		s.mu.RLock()
		s.members.access[id].Store(int64(5000 + i))
		s.mu.RUnlock()
	}
	s.TakeMissingUsers()
	out := s.EvictMembers()
	if len(out) != 500 {
		t.Fatalf("evicted %d, want 500", len(out))
	}
	if n, _ := s.MembersStats(); n != MembersTarget {
		t.Fatalf("%d left, want %d", n, MembersTarget)
	}
	if _, ok := s.RawUser("USELF"); !ok {
		t.Fatal("the viewer was evicted")
	}
	// kept: the 499 found last (U0000101..U0000599); gone: every
	// never-found one and the 101 found first
	for i := range 999 {
		id := fmt.Sprintf("U%07d", i)
		_, kept := s.RawUser(id)
		if want := i >= 101 && i < 600; kept != want {
			t.Fatalf("%s kept=%v, want %v", id, kept, want)
		}
	}
	if !slices.Contains(out, "U0000600") || !slices.Contains(out, "U0000100") {
		t.Errorf("evicted %v…", out[:5])
	}
	// forget: no lastAccess left for them, and a lookup asks again
	if !s.MemberAccessed("U0000100").IsZero() {
		t.Error("an evicted member's lastAccess stayed")
	}
	if _, ok := s.User("U0000100"); ok {
		t.Fatal("an evicted member was found")
	}
	if got := s.TakeMissingUsers(); !slices.Equal(got, []string{"U0000100"}) {
		t.Fatalf("missing after the lookup: %v", got)
	}
	// the meta.json save sees the kept ones only
	if s.UserCount() != MembersTarget {
		t.Errorf("UserCount %d", s.UserCount())
	}
}

// Ids users/info didn't return are non-existent members: lookups find
// nothing without asking again, ensureMembersArePresent counts them as
// known, and they count, sort and go like members.
func TestMembersNonexistent(t *testing.T) {
	s := membersStore(10)
	if _, ok := s.User("UGONE001"); ok {
		t.Fatal("found an unknown id")
	}
	if got := s.TakeMissingUsers(); !slices.Equal(got, []string{"UGONE001"}) {
		t.Fatalf("missing %v", got)
	}
	s.PutNonexistent([]string{"UGONE001", "U0000001"}) // a stored member stays one
	if !s.Nonexistent("UGONE001") || s.Nonexistent("U0000001") {
		t.Fatal("non-existent marks")
	}
	if _, ok := s.User("UGONE001"); ok {
		t.Fatal("a non-existent member was found")
	}
	s.ForgetMissing('u', "UGONE001") // even when asking is allowed again
	if _, ok := s.User("UGONE001"); ok {
		t.Fatal("found")
	}
	if got := s.TakeMissingUsers(); len(got) != 0 {
		t.Fatalf("a non-existent member was asked for again: %v", got)
	}
	if m := s.MissingUsers([]string{"UGONE001", "UNEW0001"}); !slices.Equal(m, []string{"UNEW0001"}) {
		t.Errorf("MissingUsers %v", m)
	}
	if _, gone := s.MembersStats(); gone != 1 {
		t.Errorf("%d non-existent", gone)
	}
	// stored after all: a member again
	s.PutUsers([]slack.User{{ID: "UGONE001", Name: "back"}})
	if s.Nonexistent("UGONE001") {
		t.Error("still non-existent once stored")
	}
	// evicted like the rest (the debug sizes: keep the viewer only)
	s.PutNonexistent([]string{"UGONE002"})
	s.SetMembersSizeForDebug(1, 1)
	out := s.EvictMembers()
	s.SetMembersSizeForDebug(-1, -1)
	if !slices.Contains(out, "UGONE002") || s.Nonexistent("UGONE002") {
		t.Errorf("non-existent id not evicted: %v", out)
	}
	if n, gone := s.MembersStats(); n != 1 || gone != 0 {
		t.Errorf("left %d members, %d non-existent", n, gone)
	}
	if _, ok := s.RawUser("USELF"); !ok {
		t.Error("the viewer was evicted")
	}
}

// maybeScheduleEviction: due once the jittered interval (an hour ±20%)
// has passed since the last check, and only while online; the pass sets
// the last check to now.
func TestMembersPassSchedule(t *testing.T) {
	old := membersJitter
	membersJitter = func() float64 { return 0.2 }
	t.Cleanup(func() { membersJitter = old })
	s := membersStore(800)
	due := s.MembersDue()
	if s.members.interval != int64(72*time.Minute/time.Millisecond) {
		t.Fatalf("interval %d ms, want 72 min", s.members.interval)
	}
	s.User("USELF")
	select {
	case <-due:
		t.Fatal("due at once")
	default:
	}
	online := false
	s.SetMembersOnline(func() bool { return online })
	s.members.lastCheck.Store(time.Now().Add(-73 * time.Minute).UnixMilli())
	s.User("USELF")
	select {
	case <-due:
		t.Fatal("scheduled while offline")
	default:
	}
	online = true
	s.User("U0000001")
	select {
	case <-due:
	default:
		t.Fatal("not scheduled once due")
	}
	s.User("U0000002") // one pass at a time
	select {
	case <-due:
		t.Fatal("scheduled twice")
	default:
	}
	now := time.Now()
	out := s.RunMembersPass(now)
	if len(out) != 300 {
		t.Fatalf("pass evicted %d, want 300", len(out))
	}
	if s.members.lastCheck.Load() != now.UnixMilli() || s.members.scheduled.Load() {
		t.Fatal("the pass didn't reset the schedule")
	}
	s.User("USELF")
	select {
	case <-due:
		t.Fatal("due again right after a pass")
	default:
	}
}
