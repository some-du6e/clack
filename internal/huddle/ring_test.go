package huddle

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"
)

// A 1:1 huddle: Slack's client starts it with rooms.join in the DM and
// nothing else (joinRoom: channel_id, regions, multidevice). The ringing
// is the server's: it sends the callee huddle_invite and tells the
// caller through the room's last_invite_status_by_user, which the
// session turns into State.Invites (Slack's pending tiles).
func TestDMHuddleRing(t *testing.T) {
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	fk := NewFake(clip, t.Errorf)
	defer fk.Close()
	const dm, callee = "D0FAKEDM01", "U0CALLEE01"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{Client: fk.Caller(), SelfID: fakeSelf, Device: NewToneDevice(440), Logf: t.Logf}, dm)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Leave()

	calls := fk.Calls()
	if len(calls) != 1 || calls[0].Method != "rooms.join" {
		t.Fatalf("Slack calls for a DM huddle: %+v (want rooms.join only)", calls)
	}
	want := url.Values{"channel_id": {dm}, "regions": {"us-east-2"}, "multidevice": {"false"}}
	if !reflect.DeepEqual(calls[0].Params, want) {
		t.Fatalf("rooms.join %v, want %v", calls[0].Params, want)
	}
	if got := s.join.Participants; !reflect.DeepEqual(got, []string{fakeSelf}) || len(s.State().Invites) != 0 {
		t.Fatalf("a new DM huddle: participants %v, invites %+v", got, s.State().Invites)
	}

	ring := func(status string) []Invite {
		s.HandleRoomEvent("sh_room_update", FakeRingUpdate(dm, callee, status))
		return s.State().Invites
	}
	if got := ring("pending"); !reflect.DeepEqual(got, []Invite{{UserID: callee, Status: "pending"}}) {
		t.Fatalf("ringing: %+v", got)
	}
	if got := ring("joining_soon"); !reflect.DeepEqual(got, []Invite{{UserID: callee, Status: "joining_soon"}}) {
		t.Fatalf("joining soon: %+v", got)
	}
	if got := ring("declined"); !reflect.DeepEqual(got, []Invite{{UserID: callee, Status: "declined"}}) {
		t.Fatalf("declined: %+v", got)
	}
	if got := ring("missed"); len(got) != 0 {
		t.Fatalf("missed still shows: %+v", got)
	}

	// they pick up: the invite goes, they're a participant
	ring("pending")
	raw, _ := json.Marshal(map[string]any{"type": "sh_room_join", "user": callee, "room": map[string]any{"id": FakeRoomID}})
	s.HandleRoomEvent("sh_room_join", raw)
	st := s.State()
	if len(st.Invites) != 0 {
		t.Fatalf("invite after joining: %+v", st.Invites)
	}
	found := false
	for _, p := range st.Participants {
		found = found || p.UserID == callee
	}
	if !found {
		t.Fatalf("callee not a participant: %+v", st.Participants)
	}

	// the room's pending_invitees (a list or an object) count as pending
	r := roomJSON{PendingInvitees: json.RawMessage(`["U1"]`)}
	if got := r.invites(); !reflect.DeepEqual(got, map[string]string{"U1": "pending"}) {
		t.Fatalf("pending_invitees list: %v", got)
	}
	r = roomJSON{PendingInvitees: json.RawMessage(`{"U2":true}`), LastInviteStatus: map[string]string{"U3": "declined"}}
	if got := r.invites(); !reflect.DeepEqual(got, map[string]string{"U2": "pending", "U3": "declined"}) {
		t.Fatalf("pending_invitees object: %v", got)
	}
	if (roomJSON{}).invites() != nil {
		t.Fatal("no invite fields: not nil")
	}
}
