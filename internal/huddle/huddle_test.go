package huddle

import (
	"encoding/json"
	"image"
	"reflect"
	"testing"
)

func TestPeerMessageRoundTrip(t *testing.T) {
	ts := int64(1234)
	in := &PeerMessage{
		Reacji:           &Reacji{Emoji: "tada", Type: ReacjiStandard},
		Draw:             &Draw{Type: DrawNew, Segment: &Segment{A: Point{0.1, 0.2}, B: Point{0.3, 0.4}, C: Point{0.5, 0.6}, D: Point{0.7, 0.8}}, Hue: 200, ItemID: "att"},
		FromPeerIDString: "me",
		Capabilities:     &Capabilities{IsDrawEnabled: true},
		MuteRequest:      &MuteRequest{PeerID: "you"},
		AwarenessBroadcast: &Awareness{Channel: "C1", User: "U1",
			State: []AwarenessPayload{{FieldName: "a", FieldValue: "b", FieldTimestampMS: &ts}}},
		JukeboxRequest:      &JukeboxRequest{SongKey: "k", Stop: true},
		ContentShareDetails: &ContentShareDetails{SourceType: "desktop:screen", ItemID: "att"},
		TranscriptCatchUp:   &TranscriptCatchUp{Type: 3, Lines: []TranscriptLine{{UserID: "U1", Content: "hi", StartTimeMS: 5, EndTimeMS: 9}}, RequestID: "r"},
		KickNotification:    &KickNotification{KickedUserID: "U2"},
	}
	out, err := DecodePeerMessage(in.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		a, _ := json.Marshal(in)
		b, _ := json.Marshal(out)
		t.Fatalf("round trip:\n%s\n%s", a, b)
	}
	if out.Kind() != "jukeboxRequest" {
		t.Fatalf("kind %q", out.Kind())
	}
	neg := &PeerMessage{Draw: &Draw{Type: DrawChangeHue, Hue: -5}}
	if got, _ := DecodePeerMessage(neg.Encode()); got.Draw.Hue != -5 {
		t.Fatalf("negative hue: %d", got.Draw.Hue)
	}
}

func TestDecodeFrames(t *testing.T) {
	b := buildFrame(ftJoinAck, func(w *pbw) {
		w.msg(5, func(w *pbw) {
			w.msg(1, func(w *pbw) {
				w.str(1, "user")
				w.str(2, "pass")
				w.uint(3, 300)
				w.str(4, "turn:a:3478?transport=udp")
				w.str(4, "turns:a:443?transport=tcp")
			})
		})
	})
	f, err := decodeFrame(b)
	if err != nil || f.Type != ftJoinAck || f.Turn == nil || f.Turn.Username != "user" || len(f.Turn.URIs) != 2 {
		t.Fatalf("join ack: %+v %v", f, err)
	}

	b = buildFrame(ftIndex, func(w *pbw) {
		w.msg(8, func(w *pbw) {
			w.msg(2, func(w *pbw) {
				w.uint(1, 7)
				w.uint(3, 1500)
				w.uint(6, 3)
				w.str(8, "att#content")
				w.uint(9, mediaVideo)
				w.str(10, "E1-R1-U9")
				w.uint(11, 1920)
				w.uint(12, 1080)
			})
			w.uint(4, 3)
		})
	})
	f, err = decodeFrame(b)
	if err != nil || f.Index == nil || len(f.Index.Sources) != 1 || f.Index.NumParticipants != 3 {
		t.Fatalf("index: %+v %v", f, err)
	}
	if s := f.Index.Sources[0]; s.StreamID != 7 || s.GroupID != 3 || s.AttendeeID != "att#content" || s.Width != 1920 || userOfExternal(s.ExternalUserID) != "U9" {
		t.Fatalf("source: %+v", s)
	}

	b = buildFrame(ftSubscribeAck, func(w *pbw) {
		w.msg(7, func(w *pbw) {
			w.str(3, "v=0\r\n")
			w.msg(4, func(w *pbw) {
				w.uint(1, 7)
				w.uint(2, 0xdeadbeef)
			})
		})
	})
	f, err = decodeFrame(b)
	if err != nil || f.SDPAnswer != "v=0\r\n" || len(f.Tracks) != 1 || f.Tracks[0].SSRC != 0xdeadbeef {
		t.Fatalf("suback: %+v %v", f, err)
	}

	pm := (&PeerMessage{Reacji: &Reacji{Emoji: "wave"}}).Encode()
	b = buildFrame(ftDataMessage, func(w *pbw) {
		w.msg(23, func(w *pbw) {
			w.msg(1, func(w *pbw) {
				w.str(1, TopicData)
				w.bytes(2, pm)
				w.str(4, "att")
			})
		})
	})
	f, err = decodeFrame(b)
	if err != nil || len(f.Data) != 1 || f.Data[0].SenderAttendee != "att" {
		t.Fatalf("data: %+v %v", f, err)
	}
	if m, _ := DecodePeerMessage(f.Data[0].Data); m.Reacji == nil || m.Reacji.Emoji != "wave" {
		t.Fatalf("data payload: %+v", m)
	}

	// what we send parses back
	if f, err := decodeFrame(encodePing(pingTypePong, 9)); err != nil || f.Type != ftPingPong || f.PingType != pingTypePong || f.PingID != 9 {
		t.Fatalf("ping: %+v %v", f, err)
	}
	if f, err := decodeFrame(encodeSubscribe(subscribeReq{SDP: "x", Receive: []uint32{0, 7}})); err != nil || f.Type != ftSubscribe {
		t.Fatalf("subscribe: %+v %v", f, err)
	}
}

func TestTranscript(t *testing.T) {
	var w pbw
	w.msg(1, func(w *pbw) { // event
		w.msg(2, func(w *pbw) { // transcript
			w.msg(1, func(w *pbw) { // result
				w.msg(1, func(w *pbw) { // alternative
					w.msg(1, func(w *pbw) { w.str(3, "att") })
					w.str(2, "hello there")
				})
				w.boolean(4, true)
				w.str(5, "res1")
			})
		})
	})
	res, _ := decodeTranscript(w.b)
	if len(res) != 1 || res[0].Text != "hello there" || res[0].Speaker != "att" || !res[0].Partial || res[0].ResultID != "res1" {
		t.Fatalf("transcript: %+v", res)
	}
}

func TestYUV(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	Y := []byte{235, 235, 16, 16, 235, 235, 16, 16}
	cb := []byte{128, 128}
	cr := []byte{128, 128}
	yuv420ToRGBA(img, Y, cb, cr, 4, 2, 0, 0, false)
	if img.Pix[0] < 254 || img.Pix[8] != 0 || img.Pix[3] != 255 {
		t.Fatalf("pix %v", img.Pix[:12])
	}
}

func TestOpus(t *testing.T) {
	enc, err := newOpusEnc()
	if err != nil {
		t.Fatal(err)
	}
	dec, err := newOpusDec()
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]int16, frameSamples)
	for i := range pcm {
		pcm[i] = int16(8000 * ((i/50)%2*2 - 1))
	}
	var peak float64
	for i := 0; i < 10; i++ {
		pkt, err := enc.encode(pcm, make([]byte, 0, 1500))
		if err != nil || len(pkt) == 0 {
			t.Fatalf("encode: %v", err)
		}
		out, err := dec.decode(pkt)
		if err != nil || len(out) == 0 || len(out) > frameSamples*playChannels || len(out)%playChannels != 0 { // the first frame may be short
			t.Fatalf("decode: %d %v", len(out), err)
		}
		peak = max(peak, level(out))
	}
	if peak < 0.05 {
		t.Fatalf("decoded level %.3f", peak)
	}
	if out, err := dec.decode(nil); err != nil || len(out) != frameSamples*playChannels {
		t.Fatalf("plc: %d %v", len(out), err)
	}
}

func TestParticipantsJSON(t *testing.T) {
	var p struct {
		P participants `json:"p"`
	}
	if err := json.Unmarshal([]byte(`{"p":[{"user_id":"U1"},{"id":"U2"}]}`), &p); err != nil || len(p.P) != 2 || p.P[1] != "U2" {
		t.Fatalf("%v %v", p.P, err)
	}
	if err := json.Unmarshal([]byte(`{"p":["U3"]}`), &p); err != nil || len(p.P) != 1 {
		t.Fatalf("%v %v", p.P, err)
	}
}

func TestUserOfExternal(t *testing.T) {
	for in, want := range map[string]string{
		"E09V59WQY1E-R0CGNGYKT9N-U0CFFEW8VMK":                "U0CFFEW8VMK",
		"E09V59WQY1E-R0CGNGYKT9N-U0CFFEW8VMK-12566582084688": "U0CFFEW8VMK",
		"E1-R1-U9": "U9",
	} {
		if got := userOfExternal(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestTwoDevices(t *testing.T) {
	s := &Session{o: Options{SelfID: "U1"}, people: map[string]*Participant{}, byAtt: map[string]string{}, join: &joinInfo{}}
	s.person("U1").AttendeeID = "a1"
	s.byAtt["a1"] = "U1"
	if k := s.keyFor("a2", "U1"); k != "U1/a2" || s.people[k].UserID != "U1" || s.people[k].Self {
		t.Fatalf("second device: %q %+v", k, s.people[k])
	}
	if k := s.keyFor("b1", "U2"); k != "U2" || s.userOf("b1") != "U2" {
		t.Fatalf("other user: %q", k)
	}
}
