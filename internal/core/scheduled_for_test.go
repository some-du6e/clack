package core

import "testing"

// ScheduledFor is getScheduledDraftsForChannel: the conversation's
// scheduled drafts (not its thread's, nor plain drafts), a DM's by user
// id too, soonest first; loaded only once drafts were seen.
func TestScheduledFor(t *testing.T) {
	a, _ := testApp(t)
	if _, loaded := a.ScheduledFor("C1", ""); loaded {
		t.Fatal("loaded before any draft")
	}
	a.putDraft(Draft{ID: "d1", Channel: "C1", DateScheduled: 300, DateCreated: 1})
	a.putDraft(Draft{ID: "d2", Channel: "C1", DateScheduled: 200, DateCreated: 2})
	a.putDraft(Draft{ID: "d3", Channel: "C1", ThreadTS: "1.0", DateScheduled: 100})
	a.putDraft(Draft{ID: "d4", Channel: "C1"}) // not scheduled
	a.putDraft(Draft{ID: "d5", Users: []string{"U1"}, DateScheduled: 50})
	got, loaded := a.ScheduledFor("C1", "")
	if !loaded || len(got) != 2 || got[0].ID != "d2" || got[1].ID != "d1" {
		t.Fatalf("channel: %+v (loaded %v)", got, loaded)
	}
	if th, _ := a.ScheduledFor("C1", "1.0"); len(th) != 1 || th[0].ID != "d3" {
		t.Fatalf("thread: %+v", th)
	}
	if dm, _ := a.ScheduledFor("D1", ""); len(dm) != 1 || dm[0].ID != "d5" {
		t.Fatalf("DM by user id: %+v", dm)
	}
}
