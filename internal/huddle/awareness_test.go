package huddle

import (
	"context"
	"os"
	"testing"
	"time"
)

// Raise hand and the other stickers against the fake Chime: Slack's
// awareness sync (an AwarenessQuery on connect, a broadcast of your
// stickyEmoji per change, AwarenessClose on leaving) both ways.
func TestHandRaiseRoundTrip(t *testing.T) {
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	fk := NewFake(clip, t.Errorf)
	defer fk.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{Client: fk.Caller(), SelfID: fakeSelf, Device: NewToneDevice(440), Logf: t.Logf}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out: %s (fake saw %+v, state %+v)", what, fk.Awareness(), s.State().Participants)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	until("connected and asked", func() bool { return s.State().Phase == Connected && fk.Awareness().Queries >= 1 })

	sticker := func(st State, user string) string {
		for _, p := range st.Participants {
			if p.UserID == user {
				return p.Sticker
			}
		}
		return "?"
	}
	broadcast := func() (string, bool) {
		a := fk.Awareness().Last
		if a == nil || a.Channel != FakeRoomID || a.User != fakeSelf {
			return "", false
		}
		for _, e := range a.State {
			if e.FieldName == AwarenessSticky {
				return e.FieldValue, true
			}
		}
		return "", true
	}

	// raise: your tile, and a broadcast with stickyEmoji
	const hand = "raised_hand::skin-tone-3"
	if err := s.ToggleSticker(hand); err != nil {
		t.Fatal(err)
	}
	if got := sticker(s.State(), fakeSelf); got != hand || s.Sticker() != hand {
		t.Fatalf("your sticker %q", got)
	}
	until("the raise broadcast", func() bool { v, ok := broadcast(); return ok && v == hand })
	n := fk.Awareness().Broadcasts

	// lower: the same click again broadcasts no sticker
	if err := s.ToggleSticker(hand); err != nil {
		t.Fatal(err)
	}
	if got := sticker(s.State(), fakeSelf); got != "" {
		t.Fatalf("lowered: %q", got)
	}
	until("the lower broadcast", func() bool { v, ok := broadcast(); return ok && v == "" && fk.Awareness().Broadcasts > n })

	// the other person raises and lowers theirs
	until("the remote person", func() bool { return sticker(s.State(), fakeRemote) != "?" })
	if err := fk.RemoteSticker("raised_hand"); err != nil {
		t.Fatal(err)
	}
	until("their raised hand", func() bool { return sticker(s.State(), fakeRemote) == "raised_hand" })
	if err := fk.RemoteSticker("coffee"); err != nil {
		t.Fatal(err)
	}
	until("their Be right back", func() bool { return sticker(s.State(), fakeRemote) == "coffee" })
	if err := fk.RemoteSticker(""); err != nil {
		t.Fatal(err)
	}
	until("their sticker off", func() bool { return sticker(s.State(), fakeRemote) == "" })

	s.Leave()
	if c := fk.Awareness().Closes; c != 1 {
		t.Errorf("AwarenessClose on leaving: %d", c)
	}
}

// Someone else's sticker lapses 60 s after their last broadcast; yours
// stays.
func TestStickerTTL(t *testing.T) {
	now := time.Now()
	p := Participant{UserID: "U1", Sticker: "raised_hand", StickerAt: now.Add(-59 * time.Second)}
	if stickerAt(p, now) != "raised_hand" {
		t.Fatal("expired early")
	}
	p.StickerAt = now.Add(-61 * time.Second)
	if stickerAt(p, now) != "" {
		t.Fatal("didn't expire")
	}
	p.Self = true
	if stickerAt(p, now) != "raised_hand" {
		t.Fatal("yours expired")
	}

	// the awareness messages survive the wire
	m := &PeerMessage{AwarenessBroadcast: &Awareness{Channel: FakeRoomID, User: "U1",
		State: []AwarenessPayload{{FieldName: AwarenessSticky, FieldValue: "raised_hand::skin-tone-2"}}}}
	got, err := DecodePeerMessage(m.Encode())
	if err != nil || got.AwarenessBroadcast == nil || got.AwarenessBroadcast.User != "U1" ||
		len(got.AwarenessBroadcast.State) != 1 || got.AwarenessBroadcast.State[0].FieldValue != "raised_hand::skin-tone-2" {
		t.Fatalf("decoded %+v (%v)", got.AwarenessBroadcast, err)
	}
}
