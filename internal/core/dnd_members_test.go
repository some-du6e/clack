package core

import (
	"strconv"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Others' DND (Slack's dndV2): dnd.teamInfo answers each member's window,
// isInDnd is start ≤ now < end (1 = none), a fresh answer isn't asked
// again, and dnd_updated_user makes the next read ask.
func TestMemberDND(t *testing.T) {
	a, f := testApp(t)
	now := time.Now().Unix()
	a.S.PutUsers([]slack.User{{ID: "U0DNDON001"}, {ID: "U0DNDOFF01"}, {ID: "U0DNDBOT01", IsBot: true}})
	f.mu.Lock()
	f.reply["dnd.teamInfo"] = `{"ok":true,"users":{"U0DNDON001":{"dnd_enabled":true,"next_dnd_start_ts":` +
		strconv.FormatInt(now-60, 10) + `,"next_dnd_end_ts":` + strconv.FormatInt(now+3600, 10) + `},"U0DNDOFF01":{"dnd_enabled":false,"next_dnd_start_ts":1,"next_dnd_end_ts":1}}}`
	f.mu.Unlock()
	if a.ClaimMemberDND("U0DNDBOT01", time.Now()) || a.ClaimMemberDND("USELF", time.Now()) {
		t.Error("claimed a bot's or the viewer's DND")
	}
	for _, id := range []string{"U0DNDON001", "U0DNDOFF01"} {
		if !a.ClaimMemberDND(id, time.Now()) {
			t.Fatalf("%s: not claimed before any answer", id)
		}
		if a.ClaimMemberDND(id, time.Now()) {
			t.Fatalf("%s: claimed twice", id)
		}
	}
	if err := a.LoadMemberDND(t.Context(), []string{"U0DNDON001", "U0DNDOFF01", "U0DNDBOT01"}); err != nil {
		t.Fatal(err)
	}
	if p := f.last("dnd.teamInfo"); p.Get("users") != "U0DNDON001,U0DNDOFF01" {
		t.Errorf("dnd.teamInfo users %q, want the two people", p.Get("users"))
	}
	if !a.MemberInDND("U0DNDON001", time.Now()) || a.MemberInDND("U0DNDOFF01", time.Now()) {
		t.Errorf("in DND: on %v off %v", a.MemberInDND("U0DNDON001", time.Now()), a.MemberInDND("U0DNDOFF01", time.Now()))
	}
	if u, _ := a.User("U0DNDON001"); !u.DND {
		t.Error("User.DND false for a member in their window")
	}
	if a.MemberInDND("U0DNDON001", time.Unix(now+3601, 0)) {
		t.Error("still in DND after the window's end")
	}
	if a.ClaimMemberDND("U0DNDON001", time.Now()) {
		t.Error("a fresh answer claimed again")
	}
	if !a.ClaimMemberDND("U0DNDON001", time.Now().Add(memberDNDMaxAge)) {
		t.Error("not claimed after maxAge")
	}
	a.LoadMemberDND(t.Context(), []string{"U0DNDON001"})
	a.onDNDUpdatedUser(raw(map[string]any{"type": "dnd_updated_user", "user": "U0DNDOFF01"}))
	if !a.ClaimMemberDND("U0DNDOFF01", time.Now()) {
		t.Error("dnd_updated_user didn't mark the member stale")
	}
}

// A dnd.teamInfo answer that leaves a member out still makes the member
// fresh (p7oS _updateAfterRefetch marks every id it asked for), after
// maxAge and after dnd_updated_user alike, so an open card doesn't ask
// again every round trip; the schedule held stays.
func TestMemberDNDOmittedAnswerIsFresh(t *testing.T) {
	a, f := testApp(t)
	now := time.Now().Unix()
	const id = "U0DNDOMIT1"
	a.S.PutUsers([]slack.User{{ID: id}})
	f.mu.Lock()
	f.reply["dnd.teamInfo"] = `{"ok":true,"users":{"` + id + `":{"dnd_enabled":true,"next_dnd_start_ts":` +
		strconv.FormatInt(now-60, 10) + `,"next_dnd_end_ts":` + strconv.FormatInt(now+3600, 10) + `}}}`
	f.mu.Unlock()
	if err := a.LoadMemberDND(t.Context(), []string{id}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.reply["dnd.teamInfo"] = `{"ok":true,"users":{}}`
	f.mu.Unlock()
	for _, c := range []struct {
		why   string
		stale func()
	}{
		{"after maxAge", func() {
			a.mdnd.mu.Lock()
			d := a.mdnd.m[id]
			d.at = time.Now().Add(-memberDNDMaxAge - time.Second)
			a.mdnd.m[id] = d
			a.mdnd.mu.Unlock()
		}},
		{"after dnd_updated_user", func() {
			a.onDNDUpdatedUser(raw(map[string]any{"type": "dnd_updated_user", "user": id}))
		}},
	} {
		c.stale()
		if !a.ClaimMemberDND(id, time.Now()) {
			t.Fatalf("%s: not claimed", c.why)
		}
		if err := a.LoadMemberDND(t.Context(), []string{id}); err != nil {
			t.Fatal(err)
		}
		if a.ClaimMemberDND(id, time.Now()) {
			t.Errorf("%s: an answer without the member left it stale: claimed again", c.why)
		}
		if !a.MemberInDND(id, time.Now()) {
			t.Errorf("%s: the held schedule was dropped", c.why)
		}
	}
}

// A slow (full) reconnect marks every member's DND stale.
func TestMemberDNDStaleAfterSlowReconnect(t *testing.T) {
	a, f := testApp(t)
	const id = "U0DNDRECO1"
	a.S.PutUsers([]slack.User{{ID: id}})
	f.mu.Lock()
	f.reply["dnd.teamInfo"] = `{"ok":true,"users":{"` + id + `":{"dnd_enabled":false,"next_dnd_start_ts":1,"next_dnd_end_ts":1}}}`
	f.mu.Unlock()
	if err := a.LoadMemberDND(t.Context(), []string{id}); err != nil {
		t.Fatal(err)
	}
	if a.ClaimMemberDND(id, time.Now()) {
		t.Fatal("fresh answer claimed")
	}
	a.socketConnected(true)
	if a.ClaimMemberDND(id, time.Now()) {
		t.Error("a fast reconnect marked it stale")
	}
	a.socketConnected(false)
	if !a.ClaimMemberDND(id, time.Now()) {
		t.Error("a slow reconnect left it fresh")
	}
}
