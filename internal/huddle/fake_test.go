package huddle

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
)

func TestFakeChime(t *testing.T) {
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	fk := NewFake(clip, t.Errorf)
	defer fk.Close()
	f := fk.f
	if len(f.clip) < 10 {
		t.Fatalf("clip: %d access units", len(f.clip))
	}

	var peers sync.Map
	ended := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{
		Client: fk.Caller(),
		SelfID: fakeSelf, Device: NewToneDevice(440), Logf: t.Logf,
		OnPeer: func(ev PeerEvent) { peers.Store(ev.Kind, ev) },
		OnEnd:  func(err error) { ended <- err },
	}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	if st := s.State(); st.RoomID != "R0FAKEROOM" || st.ThreadTS != "1700000000.000100" || len(st.Participants) != 2 {
		t.Fatalf("after join: %+v", st)
	}

	// wait for everything the fake sends to arrive
	deadline := time.Now().Add(25 * time.Second)
	var st State
	var frame VideoFrame
	ok := func() bool {
		st = s.State()
		if st.Phase != Connected || len(st.Videos) == 0 {
			return false
		}
		frame = s.Frame(st.Videos[0].Key)
		var remote *Participant
		for i := range st.Participants {
			if st.Participants[i].UserID == fakeRemote {
				remote = &st.Participants[i]
			}
		}
		_, gotPeer := peers.Load("reacji")
		captioned := len(st.Captions) == 1 && st.Captions[0].UserID == fakeRemote && st.Captions[0].Text == "Hello from the fake huddle"
		return captioned && remote != nil && remote.InMedia && remote.Speaking(time.Now()) && remote.Reaction == "tada" &&
			gotPeer && frame.Img != nil && frame.Seq >= 3 && f.peak.Load() > 50 && s.audioInCount() > 20
	}
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: phase %s, videos %+v, frame seq %d, peak sent %d, packets in %d, stats %s, state %+v",
				st.Phase, st.Videos, frame.Seq, f.peak.Load(), f.audioIn.Load(), s.Stats(), st.Participants)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !f.gotToken.Load() {
		t.Error("the join token wasn't offered as a websocket subprotocol")
	}
	if v := st.Videos[0]; !v.Content || v.UserID != fakeRemote || v.W != 320 || v.H != 180 {
		t.Errorf("video: %+v", v)
	}
	if b := frame.Img.Bounds(); b.Dx() != 320 || b.Dy() != 180 {
		t.Errorf("frame %v", b)
	}
	if s.UserOf(fakeRemoteAtt) != fakeRemote {
		t.Errorf("UserOf: %q", s.UserOf(fakeRemoteAtt))
	}
	s.mu.Lock()
	inLevel := s.stats.inLevel
	s.mu.Unlock()
	t.Logf("received mixed audio level %.3f; the fake heard us at %.3f; %s", inLevel, float64(f.peak.Load())/1000, s.Stats())

	// controls
	s.SetMuted(true)
	if err := s.React("wave", ""); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) && (!f.muted.Load() || f.reacji.Load() == nil) {
		time.Sleep(50 * time.Millisecond)
	}
	if !f.muted.Load() {
		t.Error("AUDIO_CONTROL muted never arrived")
	}
	if r, _ := f.reacji.Load().(string); r != "wave/"+fakeSelfAtt {
		t.Errorf("reaction at the fake: %q", r)
	}
	s.SetDeafened(true)
	if st := s.State(); !st.Deafened || !st.Muted {
		t.Errorf("deafen: %+v", st)
	}

	// a room update for another room changes nothing; ours ending ends us
	s.HandleRoomEvent("sh_room_update", json.RawMessage(`{"room":{"id":"R0OTHER","has_ended":true}}`))
	if s.State().Phase == Ended {
		t.Fatal("another room's end ended us")
	}
	s.Leave()
	if !f.left.Load() {
		t.Error("LEAVE never reached the fake")
	}
	select {
	case err := <-ended:
		if err != nil {
			t.Errorf("end reason: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("OnEnd not called")
	}
	if s.State().Phase != Ended {
		t.Error("not ended")
	}
}
