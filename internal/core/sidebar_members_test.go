package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// dmPeopleApp is testApp with ims DMs (D0DMP…, with U0DMP…, every
// partner in the store) of which the first open are open, as sidebar.dms
// says, plus an open group DM with the viewer and two of the closed DMs'
// partners when mpim is set.
func dmPeopleApp(t *testing.T, ims, open int, mpim bool) *App {
	t.Helper()
	a, _ := testApp(t)
	var us []slack.User
	var list []slack.SidebarDM
	for i := range ims {
		uid, ch := fmt.Sprintf("U0DMP%06d", i), fmt.Sprintf("D0DMP%06d", i)
		us = append(us, slack.User{ID: uid, Name: strings.ToLower(uid), Profile: slack.Profile{DisplayName: "Person " + uid}})
		a.S.PutConv(slack.Conversation{ID: ch, IsIM: true, User: uid})
		list = append(list, slack.SidebarDM{ID: ch, IsIM: true, User: uid, IsOpen: i < open})
	}
	if mpim {
		a.S.PutConv(slack.Conversation{ID: "G0DMPMPIM01", Name: "mpdm-me--a--b-1", IsMPIM: true, IsGroup: true, IsPrivate: true,
			Members: []string{"USELF", "U0DMP000990", "U0DMP000991"}})
		list = append(list, slack.SidebarDM{ID: "G0DMPMPIM01", IsMPIM: true, IsOpen: true})
	}
	a.S.PutUsers(us)
	a.setSidebarDMs(list)
	return a
}

// Karim's log of 2026-11-15: each hourly members pass dropped ~580 DM
// partners and the next sidebar snapshot asked users/info for all of them
// (585 missed, 581 asked), because the snapshot read every DM's partner
// through getMemberById, closed DMs included, and the pass touched them
// all. Slack's sidebar reads only its candidates' people (lJBI
// imAndMpimMembers: open DMs and group DMs). Here the store holds more
// than 700 members and Convs 1000 DMs, 40 of them open, and a group DM:
// a pass followed by a snapshot asks for nobody, the open DMs' people
// stay and keep their names, and what went are closed DMs' partners.
func TestMembersPassThenSnapshotAsksNoClosedDMs(t *testing.T) {
	a := dmPeopleApp(t, 1000, 40, true)
	a.snapshotNow()
	if ids := a.S.TakeMissingUsers(); len(ids) != 0 {
		t.Fatalf("the first snapshot asked for %d members (all are in the store)", len(ids))
	}
	people := map[string]bool{"USELF": true, "U0DMP000990": true, "U0DMP000991": true}
	for i := range 40 {
		people[fmt.Sprintf("U0DMP%06d", i)] = true
	}
	if got := a.sidebarPeopleNow(); len(got) != len(people)-1 && len(got) != len(people) {
		t.Errorf("sidebar people %d, want the 40 open DMs' partners and the group DM's members", len(got))
	}
	for _, id := range a.sidebarPeopleNow() {
		if !people[id] {
			t.Errorf("%s is a sidebar person: only open DMs' and group DMs' people are", id)
		}
	}

	gone := a.EvictMembers()
	if n, _ := a.S.MembersStats(); len(gone) == 0 || n != store.MembersTarget {
		t.Fatalf("the pass let %d go, %d left (want %d)", len(gone), n, store.MembersTarget)
	}
	for _, id := range gone {
		if people[id] {
			t.Errorf("the pass let %s go, one of the sidebar's people", id)
		}
	}
	s := a.snapshotNow()
	if ids := a.S.TakeMissingUsers(); len(ids) != 0 {
		slices.Sort(ids)
		t.Fatalf("the snapshot after the pass asked users/info for %d members (%v…): closed DMs' partners aren't read",
			len(ids), ids[:min(len(ids), 5)])
	}
	names := map[string]string{}
	for _, c := range s.Convs {
		names[c.ID] = c.Name
	}
	for i := range 40 {
		ch, uid := fmt.Sprintf("D0DMP%06d", i), fmt.Sprintf("U0DMP%06d", i)
		if names[ch] != "Person "+uid {
			t.Fatalf("open DM %s is named %q after the pass", ch, names[ch])
		}
		if _, ok := s.Users[uid]; !ok {
			t.Fatalf("open DM %s's partner isn't in the snapshot's users", ch)
		}
	}
	// the lookups were hits: the open DMs' partners are the last found
	if a.S.MemberAccessed("U0DMP000000").IsZero() || !a.S.MemberAccessed("U0DMP000500").IsZero() {
		t.Error("the snapshot recorded hits on the wrong members")
	}
	// control: reading every DM's partner through getMemberById, as the
	// snapshot did (conv and Users for each DM), asks for the closed DMs'
	// partners the pass let go, so this test reproduces the bug
	var ims []string
	a.S.RLock()
	for _, c := range a.S.Convs {
		if c.IsIM {
			ims = append(ims, c.ID)
		}
	}
	a.S.RUnlock()
	for _, id := range ims {
		if c, ok := a.S.Conv(id); ok {
			a.users([]string{a.conv(c).User})
		}
	}
	if n := len(a.S.TakeMissingUsers()); n == 0 {
		t.Error("control: reading every DM's partner asked for nobody; the test no longer reproduces the bug")
	} else {
		t.Logf("control: the old snapshot's reads would have asked users/info for %d members", n)
	}
}

// When the sidebar lists more people than the cache's target (600 open
// DMs against 500), the pass keeps all of them instead of dropping 100
// that the next snapshot would ask for again.
func TestMembersPassKeepsSidebarPastTarget(t *testing.T) {
	a := dmPeopleApp(t, 1000, 600, false)
	a.snapshotNow()
	a.S.TakeMissingUsers()
	gone := a.EvictMembers()
	for i := range 600 {
		if id := fmt.Sprintf("U0DMP%06d", i); slices.Contains(gone, id) {
			t.Fatalf("the pass let %s go (open DM %d of 600)", id, i)
		}
	}
	if _, ok := a.S.RawUser("USELF"); !ok {
		t.Fatal("the viewer was evicted")
	}
	if n, _ := a.S.MembersStats(); n != 601 {
		t.Errorf("%d members left, want the 600 sidebar people and the viewer", n)
	}
	a.snapshotNow()
	if ids := a.S.TakeMissingUsers(); len(ids) != 0 {
		t.Fatalf("the snapshot after the pass asked for %d members", len(ids))
	}
	// Slack's cacheEviction debug hook still lets everyone go
	a.S.SetMembersSizeForDebug(1, 1)
	a.EvictMembers()
	a.S.SetMembersSizeForDebug(-1, -1)
	if n, _ := a.S.MembersStats(); n != 1 {
		t.Errorf("the debug sizes left %d members, want only the viewer", n)
	}
}

// Boot asks users/info for the sidebar's DM partners only: the open DMs'
// (sidebar.dms) and closed ones with unreads (client.counts), which are
// sidebar rows too. Neither the snapshot nor the members fetcher asks for
// another closed DM's partner or a closed group DM's members afterwards.
// Before, boot asked for every DM's partner (users.conversations lists
// the closed ones too), so with the closed ones dropped by the hourly pass
// every launch fetched them again.
func TestBootAsksOpenDMPartnersOnly(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	const closed, mpA, mpB = "U0CLOSEDDM1", "U0CLOSEDMP1", "U0CLOSEDMP2" // not in the fake's directory
	const unread = "U0CLOSEDUN1"                                         // a closed DM with unreads: a sidebar row
	fs.AddClosedDM("D0CLOSEDDM1", closed)
	fs.AddClosedMPDM("G0CLOSEDMP1", "mpdm-closed-1", []string{fakeslack.SelfID, mpA, mpB})
	fs.AddClosedDM("D0CLOSEDUN1", unread)
	// read up to an old message: client.counts has it unread
	if err := slack.New(fs.Creds()).Call(context.Background(), "conversations.mark",
		url.Values{"channel": {"D0CLOSEDUN1"}, "ts": {"1700000000.000000"}}, nil); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	asked := map[string]bool{}
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method == "edge/users/info" {
			var up map[string]int
			_ = json.Unmarshal(body["updated_ids"], &up)
			mu.Lock()
			for id := range up {
				asked[id] = true
			}
			mu.Unlock()
		}
		return nil, false
	})
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !a.SidebarDMsKnown() {
		t.Fatal("sidebar.dms didn't answer")
	}
	openPartner := ""
	var ims [][2]string // id, partner (dmOpen takes a.mu: not under the store's lock)
	a.S.RLock()
	for _, c := range a.S.Convs {
		if c.IsIM && c.User != fakeslack.SelfID && c.ID != "D0CLOSEDDM1" && c.ID != "D0CLOSEDUN1" {
			ims = append(ims, [2]string{c.ID, c.User})
		}
	}
	a.S.RUnlock()
	for _, im := range ims {
		if a.dmOpen(im[0]) {
			openPartner = im[1]
			break
		}
	}
	if openPartner == "" {
		t.Fatal("no open DM")
	}
	if _, ok := a.S.RawUser(openPartner); !ok {
		t.Errorf("boot didn't load open DM partner %s", openPartner)
	}
	for range 3 {
		a.Snapshot()
		a.InvalidateSnapshot()
		time.Sleep(300 * time.Millisecond) // the members fetcher's debounce: a miss is asked by now
	}
	mu.Lock()
	defer mu.Unlock()
	for _, id := range []string{closed, mpA, mpB} {
		if asked[id] {
			t.Errorf("users/info was asked for %s, a closed DM's person", id)
		}
	}
	if len(asked) == 0 {
		t.Error("the edge hook saw no users/info at all")
	}
	if !asked[unread] {
		t.Error("users/info wasn't asked for the partner of a closed DM with unreads (a sidebar row)")
	}
}

// bootDMPeople: the open DMs' partners and closed ones with unreads, by
// sidebar.dms' flags when given, else by core's own (a warm start's boot
// data: no call waits, and a failed sidebar.dms asks for no closed DM's
// partner); every DM's while nothing knows which are open (the sidebar
// lists them all then).
func TestBootDMPeople(t *testing.T) {
	a, _ := testApp(t)
	var convs []slack.Conversation
	for _, id := range []string{"OPEN", "CLOSED", "UNREAD"} {
		convs = append(convs, slack.Conversation{ID: "D" + id, IsIM: true, User: "U0" + id + "0000"})
	}
	convs = append(convs, slack.Conversation{ID: "C1", Name: "general", IsChannel: true})
	ts := "1762516800.000000"
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "DUNREAD", Latest: ts, LastRead: "1762500000.000000", HasUnreads: true},
		{ID: "DCLOSED", Latest: ts, LastRead: ts}, {ID: "DOPEN", Latest: ts, LastRead: ts}}})
	got := func(open map[string]bool) string {
		ids := a.bootDMPeople(convs, open)
		slices.Sort(ids)
		return strings.Join(ids, ",")
	}
	// nothing known yet (a cold start whose sidebar.dms failed): every DM
	if g := got(nil); g != "U0CLOSED0000,U0OPEN0000,U0UNREAD0000,USELF" {
		t.Errorf("nothing known: %s", g)
	}
	// sidebar.dms' flags (a cold start): the open one and the unread one
	if g := got(map[string]bool{"DOPEN": true}); g != "U0OPEN0000,U0UNREAD0000,USELF" {
		t.Errorf("sidebar.dms' flags: %s", g)
	}
	// core's own (a warm start's boot data), with no answer to wait for
	a.setSidebarDMs([]slack.SidebarDM{{ID: "DOPEN", IsOpen: true}, {ID: "DCLOSED"}, {ID: "DUNREAD"}})
	if g := got(nil); g != "U0OPEN0000,U0UNREAD0000,USELF" {
		t.Errorf("core's own flags: %s", g)
	}
}
