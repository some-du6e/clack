package ui

import (
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's members cache (store/members.go) keeps the viewer and the 500
// members found most recently, and whatever shows an evicted member looks
// it up again, which asks users/info for it (core/members.go). Here every
// member but the viewer goes (Slack's cacheEviction debug hook with the
// sizes at 1) while each main view is open: the view draws its people
// again from users/info's answers, and none is left drawn as its id or
// not at all.
func TestEvictedMembersComeBack(t *testing.T) {
	sz := fakeslack.Small
	sz.Views, sz.ThreadHeader, sz.DMsTab = true, true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ids := fs.UserIDs()
	now := time.Now()
	fs.SetHuddleHistory([]fakeslack.HuddleFixture{
		{"id": "R0EVICTH001", "date_start": now.Add(-3 * time.Hour).Unix(), "date_end": now.Add(-3*time.Hour).Unix() + 20*60,
			"has_ended": true, "channels": []string{fs.ChannelIDs()[0]}, "channel_type": "public", "participants": []string{},
			"participant_history": []string{fakeslack.SelfID, ids[3], ids[4], ids[5]}},
	})
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.Core()
	ch := fs.ChannelIDs()[0]
	e.openChannel(ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	sender := ""
	if tl := c.S.Timeline(ch); tl != nil {
		for _, m := range tl.Msgs {
			if m.User != "" && m.User != fakeslack.SelfID {
				sender = m.User
			}
		}
	}
	if sender == "" {
		t.Fatal("no sender in the channel")
	}
	views := []struct {
		name string
		open func()
	}{
		{"channel", func() { e.openChannel(ch) }},
		{"thread", func() { e.openChannel(ch); e.a.OpenThread(fakeslack.ThreadDMID, fakeslack.ThreadRootTS) }},
		{"dms", func() { e.a.Navigate("dms") }},
		{"activity", func() { e.a.Navigate("activity") }},
		{"search", func() { e.a.Navigate("search?q=the") }},
		{"profile", func() { e.openChannel(ch); e.a.OpenUserProfile(sender) }},
		{"huddles", func() { e.a.Navigate("huddles") }},
	}
	asked := fs.CallCount("edge/users/info")
	for _, v := range views {
		v.open()
		e.settle(2 * time.Second) // the view's loads
		names, idsOf := memberStrings(c.S)
		before := frameStrings(e)
		want := map[string]bool{}
		for s := range before {
			if names[s] {
				want[s] = true
			}
		}
		if len(want) == 0 {
			t.Errorf("%s: no member names drawn", v.name)
			continue
		}
		// Slack's cacheEviction.evictAll, the viewer kept
		c.S.SetMembersSizeForDebug(1, 1)
		gone := c.EvictMembers()
		c.S.SetMembersSizeForDebug(-1, -1)
		if n, _ := c.S.MembersStats(); n != 1 || len(gone) == 0 {
			t.Fatalf("%s: %d members left after evicting %d", v.name, n, len(gone))
		}
		var after map[string]bool
		dl := time.Now().Add(20 * time.Second)
		for {
			after = frameStrings(e)
			var missing []string
			for s := range want {
				if !after[s] {
					missing = append(missing, s)
				}
			}
			if len(missing) == 0 {
				break
			}
			if time.Now().After(dl) {
				sort.Strings(missing)
				t.Fatalf("%s: %d member names not drawn again 20 s after the eviction: %s", v.name, len(missing), strings.Join(missing, ", "))
			}
			time.Sleep(5 * time.Millisecond)
		}
		e.settle(500 * time.Millisecond) // anything still unknown is asked for by now
		after = frameStrings(e)
		var asIDs []string
		for s := range after {
			if idsOf[s] && !before[s] {
				asIDs = append(asIDs, s)
			}
		}
		if len(asIDs) > 0 {
			sort.Strings(asIDs)
			t.Errorf("%s: members drawn as their ids after the refetch: %s", v.name, strings.Join(asIDs, ", "))
		}
		t.Logf("%s: %d member names drawn again, %d members fetched back", v.name, len(want), userCount(c.S)-1)
	}
	if n := fs.CallCount("edge/users/info") - asked; n == 0 {
		t.Error("nothing was fetched again")
	} else {
		t.Logf("%d users/info requests", n)
	}
}

// memberStrings are the names lookups of the stored members would draw
// (display, real and pref-compliant names) and their ids.
func memberStrings(s *store.Store) (names, ids map[string]bool) {
	names, ids = map[string]bool{}, map[string]bool{}
	for _, id := range s.UserIDs() {
		ids[id] = true
		u, ok := s.RawUser(id)
		if !ok || id == fakeslack.SelfID {
			continue
		}
		for _, n := range []string{store.DisplayName(u), store.PrefName(u, false), store.PrefName(u, true), u.RealName} {
			if n = strings.TrimSpace(n); n != "" && n != "​" {
				names[n] = true
			}
		}
	}
	return names, ids
}

func userCount(s *store.Store) int {
	n, _ := s.MembersStats()
	return n
}

// frameStrings lays out one frame and returns every string it drew.
func frameStrings(e *perfEnv) map[string]bool {
	var mu sync.Mutex
	set := map[string]bool{}
	theme.DrawHook = func(s string) { mu.Lock(); set[s] = true; mu.Unlock() }
	e.frame()
	theme.DrawHook = nil
	mu.Lock()
	defer mu.Unlock()
	return set
}
