package ui

import (
	"encoding/json"
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
	// a huddle in a DM: its row is titled with the people in it
	// (truncatedNames)
	fs.SetHuddleHistory([]fakeslack.HuddleFixture{
		{"id": "R0EVICTH001", "date_start": now.Add(-3 * time.Hour).Unix(), "date_end": now.Add(-3*time.Hour).Unix() + 20*60,
			"has_ended": true, "channels": []string{"D0FAKE00000"}, "channel_type": "im", "participants": []string{},
			"participant_history": []string{fakeslack.SelfID, ids[7], ids[8], ids[9]}},
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
		want := drawnOf(before, names)
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
			got := drawnOf(after, want)
			var missing []string
			for s := range want {
				if !got[s] {
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
		was := drawnOf(before, idsOf)
		for id := range drawnOf(after, idsOf) {
			if !was[id] {
				asIDs = append(asIDs, id)
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

// drawnOf is which of names the drawn strings show: drawn alone, or
// inside a longer string (a huddle's "A, B, C" title).
func drawnOf(drawn, names map[string]bool) map[string]bool {
	out := map[string]bool{}
	for d := range drawn {
		if names[d] {
			out[d] = true
			continue
		}
		for n := range names {
			if len(n) >= 5 && strings.Contains(d, n) {
				out[n] = true
			}
		}
	}
	return out
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

// Closed DMs aren't sidebar rows, so nothing reads their people: boot asks
// users/info for the open DMs' partners only, and neither core's
// snapshots nor the backend's group DM names (backend.apply) look up a
// closed DM's partner or a closed group DM's members, before or after a
// members pass. Karim's log (2026-11-15): every hourly pass dropped ~580
// DM partners and the next snapshot asked users/info for all of them,
// because it read every DM's partner through getMemberById. The open DMs'
// people come back after the pass.
func TestMembersPassAsksNoClosedDMPeople(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	const closed, mpA, mpB = "U0CLOSEDUI1", "U0CLOSEDUI2", "U0CLOSEDUI3" // not in the fake's directory
	fs.AddClosedDM("D0CLOSEDUI1", closed)
	fs.AddClosedMPDM("G0CLOSEDUI1", "mpdm-closed-ui-1", []string{fakeslack.SelfID, mpA, mpB})
	var mu sync.Mutex
	asked := map[string]int{}
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			var up map[string]int
			_ = json.Unmarshal(body["updated_ids"], &up)
			mu.Lock()
			for id := range up {
				asked[id]++
			}
			mu.Unlock()
		}
		return nil, false
	})
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	e.settle(time.Second)
	c := e.a.Core()
	if !c.SidebarDMsKnown() {
		t.Fatal("sidebar.dms didn't answer")
	}
	partner := ""
	for _, cv := range e.a.model().Convs {
		if cv.Kind == "im" && cv.User != fakeslack.SelfID && cv.ID != "D0CLOSEDUI1" {
			partner = cv.User
			break
		}
	}
	if partner == "" {
		t.Fatal("no open DM")
	}
	// a pass that lets every member go but the viewer (Slack's
	// cacheEviction debug hook), and the snapshots after it
	c.S.SetMembersSizeForDebug(1, 1)
	c.EvictMembers()
	c.S.SetMembersSizeForDebug(-1, -1)
	e.until("the open DM's partner back", 20*time.Second, func() bool { _, ok := c.S.RawUser(partner); return ok })
	e.settle(time.Second)
	mu.Lock()
	defer mu.Unlock()
	for _, id := range []string{closed, mpA, mpB} {
		if n := asked[id]; n > 0 {
			t.Errorf("users/info was asked for %s %d time(s), a closed DM's person", id, n)
		}
	}
	if asked[partner] == 0 {
		t.Errorf("the open DM's partner %s came back without users/info", partner)
	}
}
