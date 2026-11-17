package core

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Slack's DM badges read the member of every unread, unmuted IM: rHEx's
// DUy0 G and 6Z4k Sm call gIKW getMemberById, which records a hit or has
// Gx04's UnknownFetchManager fetch a miss. Clack's snapshot read only the
// sidebar's DM people, so an unread IM nothing listed kept a person's badge
// skips while its member was unknown (members.go readBadgePeople).
// Synthetic ids; member-shaped ones are fetched, U1-style ones aren't.

// A system user's IM never has a sidebar row (systemDMUsers), but its
// unread messages are in the badges, so the snapshot asks for its member.
// A read DM nothing lists asks for nobody.
func TestBadgeReadersAskForUnreadIMMembers(t *testing.T) {
	a, _ := testApp(t)
	for _, c := range []slack.Conversation{
		{ID: "DEXPERT", IsIM: true, User: "UCHANNELEXPERT"},
		{ID: "DQUIET", IsIM: true, User: "U0QUIET0001"},
	} {
		a.S.PutConv(c)
	}
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{
		{ID: "DEXPERT", HasUnreads: true, MentionCount: 1, LastRead: "1700000000.000100", Latest: "1700000001.000100"},
		{ID: "DQUIET", LastRead: "1700000001.000100", Latest: "1700000001.000100"},
	}})
	a.setSidebarDMs([]slack.SidebarDM{
		{ID: "D1", IsIM: true, User: "U1", IsOpen: true},
		{ID: "DEXPERT", IsIM: true, User: "UCHANNELEXPERT", IsOpen: true},
		{ID: "DQUIET", IsIM: true, User: "U0QUIET0001"},
	})
	close(a.ready)
	s := a.Snapshot()
	for _, sec := range s.Sections {
		if slices.Contains(sec.All, "DEXPERT") {
			t.Fatalf("section %s lists the system user's IM: the test wants one nothing lists", sec.Type)
		}
	}
	missing := a.S.TakeMissingUsers()
	if !slices.Contains(missing, "UCHANNELEXPERT") {
		t.Errorf("the snapshot asked for %v: not the unread IM's member UCHANNELEXPERT (DUy0 G, 6Z4k Sm getMemberById)", missing)
	}
	if slices.Contains(missing, "U0QUIET0001") {
		t.Errorf("the snapshot asked for U0QUIET0001, a read, closed DM's member nothing reads")
	}
}

// A closed DM is no sidebar row while it's read. A client.counts answer
// that makes it unread reaches the rail without a new snapshot, so the
// answer itself has the badges ask for the member.
func TestCountsAnswerAsksForUnreadIMMember(t *testing.T) {
	a, f := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "DLATE", IsIM: true, User: "U0LATEBOT01"})
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "DLATE", LastRead: "1700000001.000100", Latest: "1700000001.000100"}}})
	a.setSidebarDMs([]slack.SidebarDM{{ID: "D1", IsIM: true, User: "U1", IsOpen: true}, {ID: "DLATE", IsIM: true, User: "U0LATEBOT01"}})
	close(a.ready)
	a.Snapshot()
	if missing := a.S.TakeMissingUsers(); slices.Contains(missing, "U0LATEBOT01") {
		t.Fatalf("the snapshot asked for the closed, read DM's member (%v): the test wants it unread first", missing)
	}
	f.reply["client.counts"] = `{"ok":true,"channels":[],"mpims":[],"ims":[` +
		`{"id":"DLATE","has_unreads":true,"mention_count":1,"last_read":"1700000001.000100","latest":"1700000002.000100"}]}`
	a.refreshCounts(context.Background())
	if e, _ := a.S.Count("DLATE"); !e.HasUnreads {
		t.Fatal("client.counts' answer didn't make DLATE unread")
	}
	if missing := a.S.TakeMissingUsers(); !slices.Contains(missing, "U0LATEBOT01") {
		t.Errorf("after the counts answer: asked for %v, not the unread IM's member U0LATEBOT01", missing)
	}
}

// The badges' people stay through the members pass, as the sidebar's do:
// Slack's lookups keep them among the members found last. Here the store
// holds more than 700 members, the unread system-user IM's member the one
// found longest ago, and a snapshot reads it before the pass.
func TestMembersPassKeepsBadgePeople(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "UCHANNELEXPERT", Name: "channel_agent", Profile: slack.Profile{DisplayName: "Channel Agent"}}})
	var us []slack.User
	for i := range 800 {
		uid := fmt.Sprintf("U0MBP%06d", i)
		us = append(us, slack.User{ID: uid, Name: strings.ToLower(uid)})
	}
	a.S.PutUsers(us)
	for _, u := range us { // found since: UCHANNELEXPERT is the one found longest ago
		a.S.TouchMember(u.ID)
	}
	a.S.PutConv(slack.Conversation{ID: "DEXPERT", IsIM: true, User: "UCHANNELEXPERT"})
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{
		{ID: "DEXPERT", HasUnreads: true, MentionCount: 2, LastRead: "1700000000.000100", Latest: "1700000001.000100"},
	}})
	a.setSidebarDMs([]slack.SidebarDM{{ID: "D1", IsIM: true, User: "U1", IsOpen: true}, {ID: "DEXPERT", IsIM: true, User: "UCHANNELEXPERT", IsOpen: true}})
	close(a.ready)
	a.snapshotNow()
	a.S.TakeMissingUsers()
	gone := a.EvictMembers()
	if n, _ := a.S.MembersStats(); len(gone) == 0 || n > store.MembersTarget+2 {
		t.Fatalf("the pass let %d go, %d left: the test wants a pass that trims", len(gone), n)
	}
	if slices.Contains(gone, "UCHANNELEXPERT") {
		t.Error("the pass let UCHANNELEXPERT go, the member of an unread IM the DM badges read")
	}
	if _, ok := a.S.RawUser("UCHANNELEXPERT"); !ok {
		t.Error("UCHANNELEXPERT left the store")
	}
}

// A mark from another client can leave a DM unread (im_marked with an
// unread count) with no message and no new snapshot: the badges read its
// member then too.
func TestMarkedUnreadAsksForIMMember(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "DMARK", IsIM: true, User: "U0MARKBOT01"})
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "DMARK", LastRead: "1700000002.000100", Latest: "1700000002.000100"}}})
	a.setSidebarDMs([]slack.SidebarDM{{ID: "D1", IsIM: true, User: "U1", IsOpen: true}, {ID: "DMARK", IsIM: true, User: "U0MARKBOT01"}})
	close(a.ready)
	a.Snapshot()
	a.S.TakeMissingUsers()
	a.handle(rtm("im_marked", map[string]any{"channel": "DMARK", "ts": "1700000001.000100",
		"unread_count_display": 1, "mention_count_display": 1}))
	if e, _ := a.S.Count("DMARK"); !e.HasUnreads {
		t.Fatal("im_marked didn't leave DMARK unread")
	}
	if missing := a.S.TakeMissingUsers(); !slices.Contains(missing, "U0MARKBOT01") {
		t.Errorf("after im_marked: asked for %v, not the unread IM's member U0MARKBOT01", missing)
	}
}
