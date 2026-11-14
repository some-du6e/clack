package huddleui

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Incoming huddle invites against fakeslack (dnd.info,
// rooms.inviteResponse) and the fake Chime (rooms.join on Join). Nothing
// here talks to Slack: the invite is a synthetic huddle_invite event.

// notifyRec records the notifications and rings.
type notifyRec struct {
	recHost
	nmu   sync.Mutex
	notes []Notification
	rings []string
}

func (h *notifyRec) NotifyActions(n Notification) {
	h.nmu.Lock()
	h.notes = append(h.notes, n)
	h.nmu.Unlock()
}

func (h *notifyRec) Post(f func()) { go f() } // the ui root's Post: off the caller

func (h *notifyRec) notifications() []Notification {
	h.nmu.Lock()
	defer h.nmu.Unlock()
	return append([]Notification(nil), h.notes...)
}

func (h *notifyRec) ringed() []string {
	h.nmu.Lock()
	defer h.nmu.Unlock()
	return append([]string(nil), h.rings...)
}

// splitCaller sends rooms.join to the fake Chime and the rest to fakeslack.
type splitCaller struct{ chime, slack huddle.Caller }

func (s splitCaller) Call(ctx context.Context, method string, p url.Values, out any) error {
	if method == "rooms.join" {
		return s.chime.Call(ctx, method, p, out)
	}
	return s.slack.Call(ctx, method, p, out)
}

type inviteEnv struct {
	t   *testing.T
	fs  *fakeslack.Server
	app *core.App
	c   *Controller
	h   *notifyRec
	fk  *huddle.Fake
}

const inviteCall = "R0INVITE01"

func newInviteEnv(t *testing.T, prefs map[string]any) *inviteEnv {
	t.Helper()
	return newInviteEnvSize(t, fakeslack.Small, prefs)
}

// newInviteEnvSize is newInviteEnv over a fakeslack of size sz.
func newInviteEnvSize(t *testing.T, sz fakeslack.Size, prefs map[string]any) *inviteEnv {
	t.Helper()
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	st := store.New("")
	st.PutUsers([]slack.User{
		{ID: huddle.FakeSelfUser, Name: "me", RealName: "Test Self"},
		{ID: huddle.FakeRemoteUser, Name: "remote", RealName: "Remote Example"},
	})
	st.PutConv(slack.Conversation{ID: dmChannel, IsIM: true, User: huddle.FakeRemoteUser})
	st.PutConv(slack.Conversation{ID: hcChannel, Name: "huddle-test", IsChannel: true, IsMember: true, NumMembers: 2})
	raw := map[string]json.RawMessage{}
	for k, v := range prefs {
		b, _ := json.Marshal(v)
		raw[k] = b
	}
	st.SetRawPrefs(raw)
	app := core.New(slack.New(fs.Creds()), st)
	if u, ok := st.User(huddle.FakeSelfUser); ok {
		app.S.Self = *u
	}
	h := &notifyRec{recHost: recHost{hcHost: hcHost{app: app, th: theme.New(false)}}}
	c := New(h)
	c.SelfID = huddle.FakeSelfUser
	c.NoWindow = true
	c.Ring = func(f string) { h.nmu.Lock(); h.rings = append(h.rings, f); h.nmu.Unlock() }
	fk := huddle.NewFake(testClip(t), t.Errorf)
	t.Cleanup(fk.Close)
	c.Client = splitCaller{chime: fk.Caller(), slack: app.C}
	c.Tweak = func(o *huddle.Options) { o.Device = huddle.NewToneDevice(440) }
	return &inviteEnv{t: t, fs: fs, app: app, c: c, h: h, fk: fk}
}

// invite sends the huddle_invite Slack's server sends a callee.
func (e *inviteEnv) invite(ch string) {
	raw, _ := json.Marshal(map[string]any{"type": "huddle_invite", "channel_id": ch, "call_id": inviteCall,
		"sender_user_id": huddle.FakeRemoteUser, "team_id": "T0FAKE"})
	e.c.Handle(core.Event{Type: "huddle", Name: "huddle_invite", Raw: raw})
}

func (e *inviteEnv) shown(what string) IncomingInvite {
	e.t.Helper()
	var inv IncomingInvite
	waitFor(e.t, what, func() bool {
		var ok bool
		inv, ok = e.c.Incoming()
		return ok && (inv.Window || inv.Card)
	})
	return inv
}

const ring = "boop_remix.mp3"

// An invite to a DM huddle: the window (not the card), one ring of the
// pref's sound, the silent notification with Join and Decline; Join is
// startHuddle (Begin) and a rooms.join in the DM.
func TestIncomingInviteJoin(t *testing.T) {
	e := newInviteEnv(t, map[string]any{"huddle_invite_sound_v2": ring})
	e.invite(dmChannel)
	inv := e.shown("the invite")
	if !inv.Window || inv.Card || inv.Channel != dmChannel || inv.Sender != huddle.FakeRemoteUser || inv.Call != inviteCall {
		t.Fatalf("invite %+v", inv)
	}
	waitFor(t, "the notification", func() bool { return len(e.h.notifications()) == 1 })
	if got := e.h.ringed(); len(got) != 1 || got[0] != ring {
		t.Fatalf("rings %q", got)
	}
	n := e.h.notifications()[0]
	who := e.c.displayName(huddle.FakeRemoteUser)
	if n.Body != who+" is inviting you to a huddle" || len(n.Actions) != 2 || n.Actions[0].Label != "Join" || n.Actions[1].Label != "Decline" {
		t.Fatalf("notification %+v", n)
	}
	// the same invite again is ignored
	e.invite(dmChannel)
	time.Sleep(50 * time.Millisecond)
	if len(e.h.ringed()) != 1 || len(e.h.notifications()) != 1 {
		t.Fatal("a repeated invite rang again")
	}

	e.c.JoinInvite(EntryInviteWindow)
	waitFor(t, "the huddle", func() bool { return e.c.Session() != nil })
	s := e.c.Session()
	t.Cleanup(s.Leave)
	if _, ok := e.c.Incoming(); ok {
		t.Fatal("the invite is still up after Join")
	}
	calls := e.fk.Calls()
	if len(calls) != 1 || calls[0].Method != "rooms.join" || calls[0].Params.Get("channel_id") != dmChannel {
		t.Fatalf("Slack calls on Join: %+v", calls)
	}
	if len(e.fs.InviteResponses()) != 0 {
		t.Fatalf("Join sent rooms.inviteResponse: %v", e.fs.InviteResponses())
	}
	// an invite to the huddle you're in is ignored
	raw, _ := json.Marshal(map[string]any{"channel_id": dmChannel, "call_id": s.RoomID(), "sender_user_id": huddle.FakeRemoteUser})
	e.c.Handle(core.Event{Type: "huddle", Name: "huddle_invite", Raw: raw})
	time.Sleep(50 * time.Millisecond)
	if _, ok := e.c.Incoming(); ok {
		t.Fatal("an invite to your own huddle showed")
	}
}

// Decline: rooms.inviteResponse decline, and the invite goes. Be there
// soon: joining_soon, the invite stays without that button.
func TestIncomingInviteDecline(t *testing.T) {
	e := newInviteEnv(t, nil)
	e.invite(hcChannel)
	e.shown("the invite")
	n := e.h.notifications()
	who := e.c.displayName(huddle.FakeRemoteUser)
	waitFor(t, "the notification", func() bool { n = e.h.notifications(); return len(n) == 1 })
	if n[0].Body != who+" is inviting you to a huddle in #huddle-test" || n[0].PlainBody != who+" is inviting you to a huddle" {
		t.Fatalf("channel invite: %+v", n[0])
	}
	if len(e.h.ringed()) != 0 {
		t.Fatalf("rang without a sound pref: %q", e.h.ringed())
	}

	e.c.JoiningSoon()
	waitFor(t, "Be there soon", func() bool { inv, _ := e.c.Incoming(); return inv.JoiningSoon })
	inv, _ := e.c.Incoming()
	if inv.Note != "Join when you’re ready!" {
		t.Fatalf("after Be there soon: %+v", inv)
	}
	e.c.DeclineInvite()
	waitFor(t, "declined", func() bool { _, ok := e.c.Incoming(); return !ok })
	got := e.fs.InviteResponses()
	if len(got) != 2 {
		t.Fatalf("rooms.inviteResponse calls %v", got)
	}
	for i, want := range []string{"joining_soon", "decline"} {
		if got[i].Get("response") != want || got[i].Get("channel_id") != hcChannel || got[i].Get("room_id") != inviteCall {
			t.Fatalf("call %d: %v (want response=%s channel_id=%s room_id=%s)", i, got[i], want, hcChannel, inviteCall)
		}
	}
	if e.c.Session() != nil || len(e.fk.Calls()) != 0 {
		t.Fatal("Decline joined")
	}
}

// Nobody answers: 30 s on (shortened here) the "Huddle missed" notice;
// then Slack stops ringing (pending_invitees without you) and the invite
// goes. huddle_invite_cancel takes one down too.
func TestIncomingInviteMissed(t *testing.T) {
	e := newInviteEnv(t, nil)
	e.c.MissedAfter = 150 * time.Millisecond
	e.invite(dmChannel)
	e.shown("the invite")
	waitFor(t, "the missed notice", func() bool { return len(e.h.notifications()) == 2 })
	m := e.h.notifications()[1]
	if m.Title != "Huddle missed" || m.Body != e.c.displayName(huddle.FakeRemoteUser)+" invited you to a huddle" ||
		len(m.Actions) != 1 || m.Actions[0].Label != "View" {
		t.Fatalf("missed notice %+v", m)
	}
	if _, ok := e.c.Incoming(); !ok {
		t.Fatal("the invite went before Slack stopped ringing")
	}
	upd := func(pending any) {
		raw, _ := json.Marshal(map[string]any{"type": "sh_room_update", "huddle": map[string]any{"channel_id": dmChannel},
			"room": map[string]any{"id": inviteCall, "channels": []string{dmChannel}, "participants": []string{huddle.FakeRemoteUser},
				"pending_invitees": pending}})
		e.c.Handle(core.Event{Type: "huddle", Name: "sh_room_update", Raw: raw})
	}
	upd(map[string]any{huddle.FakeSelfUser: "pending"})
	if _, ok := e.c.Incoming(); !ok {
		t.Fatal("still rung, but the invite went")
	}
	upd(map[string]any{})
	if _, ok := e.c.Incoming(); ok {
		t.Fatal("Slack stopped ringing, the invite stayed")
	}

	// huddle_invite_cancel (the caller hung up); no missed notice for it
	e.c.MissedAfter = time.Hour
	e.invite(dmChannel)
	e.shown("the second invite")
	raw, _ := json.Marshal(map[string]any{"type": "huddle_invite_cancel", "channel_id": dmChannel})
	e.c.Handle(core.Event{Type: "huddle", Name: "huddle_invite_cancel", Raw: raw})
	if _, ok := e.c.Incoming(); ok {
		t.Fatal("huddle_invite_cancel left the invite")
	}
}

// Do Not Disturb (dnd.info on fakeslack): no window, no ring, no
// notification; the sidebar card instead.
func TestIncomingInviteDND(t *testing.T) {
	e := newInviteEnv(t, map[string]any{"huddle_invite_sound_v2": ring})
	e.fs.SetDND(map[string]any{"snooze_enabled": true, "snooze_endtime": time.Now().Add(time.Hour).Unix()})
	e.invite(dmChannel)
	inv := e.shown("the invite")
	if inv.Window || !inv.Card {
		t.Fatalf("under DND: %+v", inv)
	}
	time.Sleep(100 * time.Millisecond)
	if len(e.h.ringed()) != 0 || len(e.h.notifications()) != 0 {
		t.Fatalf("under DND: rings %q, notifications %+v", e.h.ringed(), e.h.notifications())
	}
	// the snooze ends (core applies dnd_updated: TestDNDUpdated): the next
	// invite rings
	e.app.SetDNDStatus(core.DNDStatus{})
	e.c.clearInvite("")
	e.invite(dmChannel)
	inv = e.shown("the invite after DND")
	if !inv.Window || len(e.h.ringed()) != 1 {
		t.Fatalf("after DND: %+v, rings %q", inv, e.h.ringed())
	}
	// closing the window without answering leaves the card
	e.c.inviteWindowClosed(&inviteWindow{inv: e.c.inv})
	if inv, _ := e.c.Incoming(); inv.Window || !inv.Card {
		t.Fatalf("after closing the window: %+v", inv)
	}
}

// mute_huddle_sounds and "none" are silent; in a huddle there's no ring.
func TestIncomingInviteSoundPrefs(t *testing.T) {
	for _, prefs := range []map[string]any{
		{"huddle_invite_sound_v2": ring, "mute_huddle_sounds": true},
		{"huddle_invite_sound_v2": "none"},
	} {
		e := newInviteEnv(t, prefs)
		e.invite(dmChannel)
		e.shown("the invite")
		time.Sleep(50 * time.Millisecond)
		if got := e.h.ringed(); len(got) != 0 {
			t.Fatalf("%v: rang %q", prefs, got)
		}
	}
	e := newInviteEnv(t, map[string]any{"huddle_invite_sound_v2": ring})
	e.c.Start(hcChannel)
	waitFor(t, "in a huddle", func() bool { return e.c.Session() != nil })
	t.Cleanup(e.c.Session().Leave)
	e.invite(dmChannel)
	inv := e.shown("the invite in a huddle")
	if !inv.Window {
		t.Fatalf("in a huddle: %+v", inv)
	}
	if got := e.h.ringed(); len(got) != 0 {
		t.Fatalf("rang while in a huddle: %q", got)
	}
}

// Raise hand from the controller: raised_hand in your skin tone, sent as
// Slack's stickyEmoji awareness; the button again lowers it. Under the
// raised-hand queue the tile says "Raised hand".
func TestRaiseHand(t *testing.T) {
	e := newInviteEnv(t, map[string]any{"preferred_skin_tone": "3"})
	e.c.Start(hcChannel)
	waitFor(t, "in a huddle", func() bool { return e.c.Session() != nil })
	s := e.c.Session()
	t.Cleanup(s.Leave)
	waitState(t, s, "connected", func(st huddle.State) bool { return st.Phase == huddle.Connected })
	sent := func() string {
		if a := e.fk.Awareness().Last; a != nil {
			for _, f := range a.State {
				if f.FieldName == huddle.AwarenessSticky {
					return f.FieldValue
				}
			}
		}
		return ""
	}
	if tip := handTip(s.Sticker()); tip != "Raise hand" {
		t.Fatalf("tip %q", tip)
	}
	if err := e.c.ToggleHand(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the raised hand on the wire", func() bool { return sent() == "raised_hand::skin-tone-3" })
	if tip := handTip(s.Sticker()); tip != "Lower hand" {
		t.Fatalf("tip %q", tip)
	}
	if got := e.c.stickerLabel(s.Sticker()); got != "Raise hand" {
		t.Fatalf("label %q", got)
	}
	e.app.SetExperimentGroup("huddles_raised_hand_queue", "on")
	if got := e.c.stickerLabel(s.Sticker()); got != "Raised hand" {
		t.Fatalf("queue label %q", got)
	}
	if err := e.c.ToggleHand(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the hand lowered on the wire", func() bool { return e.fk.Awareness().Last != nil && sent() == "" && s.Sticker() == "" })

	// a sticker from the picker; the main button takes it off
	if err := e.c.PickSticker("coffee"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Be right back", func() bool { return sent() == "coffee" })
	if tip := handTip(s.Sticker()); tip != "Remove sticker" || e.c.stickerLabel("coffee") != "Be right back" {
		t.Fatalf("tip %q", tip)
	}
	if err := e.c.ToggleHand(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sticker off", func() bool { return sent() == "" && s.Sticker() == "" })

	// the other person's raised hand reaches the state the tiles draw
	if err := e.fk.RemoteSticker("raised_hand"); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, "their hand", func(st huddle.State) bool {
		for _, p := range st.Participants {
			if p.UserID == huddle.FakeRemoteUser {
				return p.Sticker == "raised_hand"
			}
		}
		return false
	})
}

func TestPendingStatus(t *testing.T) {
	for raw, want := range map[string]string{
		`{"U1":"joining_soon"}`: "joining_soon", `{"U1":true}`: "pending", `{"U2":"pending"}`: "",
		`["U1"]`: "pending", `["U2"]`: "", `{}`: "",
	} {
		if got := pendingStatus(json.RawMessage(raw), "U1"); got != want {
			t.Errorf("%s: %q, want %q", raw, got, want)
		}
	}
	if got := joinNames([]string{"A", "B", "C"}); got != "A, B, and C" {
		t.Errorf("joinNames %q", got)
	}
	if !strings.HasPrefix(slackSoundCDN, "https://a.slack-edge.com/") || slackSounds[ring] == "" {
		t.Error("sound table")
	}
}
