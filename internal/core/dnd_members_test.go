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
