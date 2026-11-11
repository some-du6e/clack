package huddle

import (
	"context"
	"math"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/slack"
)

// The experiments and audit rows the media layer carries, against the
// fake Chime only (nothing real is joined): the event log's
// rooms.startTranscription / rooms.stopTranscription, the awareness
// timestamp (huddles_awareness_timestamp), awareness per device
// (huddles_multi_device), switching the sound card mid-huddle, and the
// speaking-while-muted level check.

func fakeJoin(t *testing.T, o Options) (*Fake, *Session) {
	t.Helper()
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	fk := NewFake(clip, t.Errorf)
	t.Cleanup(fk.Close)
	if o.Client == nil {
		o.Client = fk.Caller()
	}
	o.SelfID, o.Logf = fakeSelf, t.Logf
	if o.Device == nil && o.OpenAudio == nil {
		o.Device = NewToneDevice(440)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, o, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Leave)
	return fk, s
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func calls(fk *Fake, method string) []url.Values {
	var out []url.Values
	for _, c := range fk.Calls() {
		if c.Method == method {
			out = append(out, c.Params)
		}
	}
	return out
}

// Captions on and off: startEventLog's rooms.startTranscription for the
// call, the "Captions have been turned on." event first, transcripts only
// while the log is on; stopEventLog's rooms.stopTranscription drops them.
func TestEventLogTranscription(t *testing.T) {
	fk, s := fakeJoin(t, Options{})
	waitUntil(t, "connected", func() bool { return s.State().Phase == Connected })
	time.Sleep(2 * time.Second) // the fake has sent a transcript by now
	if c := s.State().Captions; len(c) != 0 {
		t.Fatalf("transcripts kept with the event log off: %+v", c)
	}
	ctx := context.Background()
	if err := s.StartEventLog(ctx); err != nil {
		t.Fatal(err)
	}
	if got := calls(fk, "rooms.startTranscription"); len(got) != 1 || got[0].Get("call_id") != FakeRoomID {
		t.Fatalf("rooms.startTranscription: %v", got)
	}
	// a second start while on is a no-op (huddleEvents.enabled)
	if err := s.StartEventLog(ctx); err != nil || len(calls(fk, "rooms.startTranscription")) != 1 {
		t.Fatalf("second start: %v, %d calls", err, len(calls(fk, "rooms.startTranscription")))
	}
	waitUntil(t, "a transcript after the start event", func() bool {
		c := s.State().Captions
		return len(c) == 2 && c[0].Start && c[0].UserID == "USLACKBOT" && c[1].Text == "Hello from the fake huddle"
	})
	if err := s.StopEventLog(ctx); err != nil {
		t.Fatal(err)
	}
	if got := calls(fk, "rooms.stopTranscription"); len(got) != 1 || got[0].Get("call_id") != FakeRoomID {
		t.Fatalf("rooms.stopTranscription: %v", got)
	}
	if on, _ := s.EventLog(); on || len(s.State().Captions) != 0 {
		t.Fatalf("after stop: on %v, captions %+v", on, s.State().Captions)
	}
	// stopping again does nothing
	_ = s.StopEventLog(ctx)
	if n := len(calls(fk, "rooms.stopTranscription")); n != 1 {
		t.Fatalf("second stop called the API: %d", n)
	}
}

// deniedOnce answers the first rooms.startTranscription with
// permission_denied, like Slack's server while the room is still settling.
type deniedOnce struct {
	next   Caller
	denied atomic.Bool
}

func (d *deniedOnce) Call(ctx context.Context, method string, p url.Values, out any) error {
	if method == "rooms.startTranscription" && d.denied.CompareAndSwap(false, true) {
		return &slack.Error{Code: "permission_denied"}
	}
	return d.next.Call(ctx, method, p, out)
}

// permission_denied: Slack waits 5 s and asks once more.
func TestEventLogRetriesPermissionDenied(t *testing.T) {
	old := transcriptionRetry
	transcriptionRetry = 50 * time.Millisecond
	defer func() { transcriptionRetry = old }()
	clip, _ := os.ReadFile("testdata/testsrc-320x180.h264")
	fk := NewFake(clip, t.Errorf)
	defer fk.Close()
	d := &deniedOnce{next: fk.Caller()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{Client: d, SelfID: fakeSelf, Device: NewToneDevice(440), Logf: t.Logf}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Leave()
	if err := s.StartEventLog(ctx); err != nil {
		t.Fatalf("after the retry: %v", err)
	}
	if !d.denied.Load() || len(calls(fk, "rooms.startTranscription")) != 1 {
		t.Fatal("no retry after permission_denied")
	}
	if c := s.State().Captions; len(c) != 1 || !c[0].Start {
		t.Fatalf("no start event: %+v", c)
	}
}

// A start that fails turns the log off again and shows no start event.
func TestEventLogFailure(t *testing.T) {
	clip, _ := os.ReadFile("testdata/testsrc-320x180.h264")
	fk := NewFake(clip, t.Errorf)
	defer fk.Close()
	fail := callerFunc(func(ctx context.Context, method string, p url.Values, out any) error {
		if method == "rooms.startTranscription" {
			return &slack.Error{Code: "not_allowed"}
		}
		return fk.Caller().Call(ctx, method, p, out)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{Client: fail, SelfID: fakeSelf, Device: NewToneDevice(440), Logf: t.Logf}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Leave()
	if err := s.StartEventLog(ctx); err == nil {
		t.Fatal("no error")
	}
	if on, loading := s.EventLog(); on || loading || len(s.State().Captions) != 0 {
		t.Fatalf("on %v loading %v captions %+v", on, loading, s.State().Captions)
	}
}

type callerFunc func(ctx context.Context, method string, p url.Values, out any) error

func (f callerFunc) Call(ctx context.Context, method string, p url.Values, out any) error {
	return f(ctx, method, p, out)
}

// huddles_awareness_timestamp: your sticker's broadcast carries its
// field_timestamp_ms only with the experiment on; theirs comes through.
func TestAwarenessTimestamp(t *testing.T) {
	for _, on := range []bool{true, false} {
		fk, s := fakeJoin(t, Options{Experiments: Experiments{AwarenessTimestamp: on}})
		waitUntil(t, "connected", func() bool { return s.State().Phase == Connected && fk.Awareness().Queries >= 1 })
		before := time.Now().UnixMilli()
		if err := s.SetSticker("raised_hand"); err != nil {
			t.Fatal(err)
		}
		var last *Awareness
		waitUntil(t, "the broadcast", func() bool {
			last = fk.Awareness().Last
			return last != nil && len(last.State) == 1 && last.State[0].FieldValue == "raised_hand"
		})
		ts := last.State[0].FieldTimestampMS
		if on && (ts == nil || *ts < before || *ts > time.Now().UnixMilli()) {
			t.Fatalf("on: field_timestamp_ms %v (set at %d)", ts, before)
		}
		if !on && ts != nil {
			t.Fatalf("off: field_timestamp_ms %d sent", *ts)
		}
		// flipping the experiment mid-huddle changes the next broadcast
		s.SetExperiments(Experiments{AwarenessTimestamp: !on})
		_ = s.SetSticker("coffee")
		waitUntil(t, "the flipped broadcast", func() bool {
			last = fk.Awareness().Last
			return last != nil && len(last.State) == 1 && last.State[0].FieldValue == "coffee"
		})
		if got := last.State[0].FieldTimestampMS != nil; got == on {
			t.Fatalf("after flipping to %v: timestamp sent %v", !on, got)
		}
		self := s.State().Participants[0]
		if !self.Self || self.StickerTS == 0 {
			t.Fatalf("your sticker's stamp: %+v", self)
		}
		s.Leave()
	}
}

// huddles_multi_device: someone in the huddle on two devices has two
// tiles; per device each shows its own sticker, per person (control)
// either device's broadcast is what both show.
func TestAwarenessPerDevice(t *testing.T) {
	for _, multi := range []bool{true, false} {
		s := &Session{o: Options{SelfID: "USELF"}, join: &joinInfo{RoomID: "R1"}, people: map[string]*Participant{},
			byAtt: map[string]string{}, exp: Experiments{MultiDevice: multi}, doneCh: make(chan struct{})}
		s.o.Logf = t.Logf
		s.person("USELF").Self = true
		// U1 joins from two devices
		s.mu.Lock()
		s.keyFor("att-a", "U1")
		s.keyFor("att-b", "U1")
		s.mu.Unlock()
		ts := int64(1700000000123)
		s.onAwareness(&PeerMessage{AwarenessBroadcast: &Awareness{Channel: "R1", User: "U1",
			State: []AwarenessPayload{{FieldName: AwarenessSticky, FieldValue: "raised_hand", FieldTimestampMS: &ts}}}}, "att-a")
		got := map[string]Participant{}
		for _, p := range s.State().Participants {
			got[p.AttendeeID] = p
		}
		a, b := got["att-a"], got["att-b"]
		if a.Sticker != "raised_hand" || a.StickerTS != ts {
			t.Fatalf("multi %v: the sender's tile %+v", multi, a)
		}
		if multi && b.Sticker != "" {
			t.Fatalf("per device: the other device shows %q", b.Sticker)
		}
		if !multi && b.Sticker != "raised_hand" {
			t.Fatalf("per person: the other device shows %q", b.Sticker)
		}
		// closing from one device
		s.onAwareness(&PeerMessage{AwarenessClose: &Awareness{Channel: "R1", User: "U1"}}, "att-b")
		got = map[string]Participant{}
		for _, p := range s.State().Participants {
			got[p.AttendeeID] = p
		}
		if multi && got["att-a"].Sticker != "raised_hand" {
			t.Fatalf("per device: att-b's close cleared att-a")
		}
		if !multi && got["att-a"].Sticker != "" {
			t.Fatalf("per person: the close left att-a's sticker")
		}
	}
}

// rooms.join's multidevice follows startHuddle's isMultiDevice.
func TestRoomsJoinMultiDevice(t *testing.T) {
	for _, multi := range []bool{false, true} {
		fk, _ := fakeJoin(t, Options{MultiDevice: multi})
		j := calls(fk, "rooms.join")
		if len(j) != 1 || j[0].Get("multidevice") != map[bool]string{true: "true", false: "false"}[multi] {
			t.Fatalf("multi %v: rooms.join %v", multi, j)
		}
	}
}

// countDevice is a sound card that counts its opens and closes.
type countDevice struct {
	stop chan struct{}
	once sync.Once
	tone bool
}

func (d *countDevice) Start(capture func([]int16), playback func([]int16)) error {
	go func() {
		t := time.NewTicker(20 * time.Millisecond)
		defer t.Stop()
		mic := make([]int16, frameSamples)
		out := make([]int16, frameSamples*playChannels)
		n := 0
		for {
			select {
			case <-d.stop:
				return
			case <-t.C:
			}
			if capture != nil {
				for i := range mic {
					if d.tone {
						mic[i] = int16(0.5 * 32767 * math.Sin(2*math.Pi*440*float64(n)/sampleRate))
					}
					n++
				}
				capture(mic)
			}
			if playback != nil {
				playback(out)
			}
		}
	}()
	return nil
}

func (d *countDevice) Close() { d.once.Do(func() { close(d.stop) }) }

// The Audio settings menu's pick reopens the speakers and the microphone
// mid-huddle, on the chosen devices.
func TestSwitchAudio(t *testing.T) {
	var opens atomic.Int32
	o := Options{
		MicAccess: func() <-chan avperm.Status {
			ch := make(chan avperm.Status, 1)
			ch <- avperm.Authorized
			return ch
		},
		OpenAudio: func(func(string, ...any)) (Device, error) {
			opens.Add(1)
			return &countDevice{stop: make(chan struct{}), tone: true}, nil
		},
	}
	_, s := fakeJoin(t, o)
	waitUntil(t, "speakers and microphone open", func() bool { return opens.Load() == 2 && s.State().Mic == MicOK })
	SelectAudioDevices("mic-2", "out-2")
	defer SelectAudioDevices("", "")
	s.SwitchAudio(true, false)
	waitUntil(t, "the speakers reopened", func() bool { return opens.Load() == 3 })
	s.SwitchAudio(false, true)
	waitUntil(t, "the microphone reopened", func() bool { return opens.Load() == 4 && s.State().Mic == MicOK })
	waitUntil(t, "sound from the new microphone", s.SelfAudioDetected)
}

// Slack's level scale and the muted tracker's 4-in-a-row rule.
func TestSpeakingWhileMutedLevels(t *testing.T) {
	for _, c := range []struct{ peak, want float64 }{{0, 0}, {0.01, 0}, {0.1, 0.5 / 6}, {1, 1.0 / 6}} {
		if got := slackLevel(c.peak); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("slackLevel(%v) = %v, want %v", c.peak, got, c.want)
		}
	}
	// .065 is a peak of about .06
	if slackLevel(0.059) >= mutedLevel || slackLevel(0.0605) < mutedLevel {
		t.Errorf("threshold: %v %v", slackLevel(0.059), slackLevel(0.0605))
	}
	var m speakingMuted
	loud, quiet := 0.1, 0.03
	seq := []struct {
		lv   float64
		want bool
	}{{loud, false}, {loud, false}, {0, false}, {loud, false}, {loud, true}, {loud, false}, {quiet, false}, {loud, false}}
	for i, st := range seq {
		if got := m.level(st.lv); got != st.want {
			t.Fatalf("step %d (level %v): %v", i, st.lv, got)
		}
	}
}

// Talking while muted raises the alert after 4 loud level updates (300 ms
// apart); unmuting drops it.
func TestSpeakingWhileMuted(t *testing.T) {
	o := Options{Muted: true, OpenAudio: func(func(string, ...any)) (Device, error) {
		return &countDevice{stop: make(chan struct{}), tone: true}, nil
	}, MicAccess: func() <-chan avperm.Status {
		ch := make(chan avperm.Status, 1)
		ch <- avperm.Authorized
		return ch
	}}
	fk, s := fakeJoin(t, o)
	waitUntil(t, "connected", func() bool { return s.State().Phase == Connected })
	start := time.Now()
	waitUntil(t, "SpeakingWhenMuted", s.SpeakingWhileMuted)
	if d := time.Since(start); d < 900*time.Millisecond {
		t.Errorf("the alert came after %v: before 4 level updates", d)
	}
	if s.SelfAudioDetected() {
		t.Error("audio detected while muted")
	}
	s.SetMuted(false)
	if s.SpeakingWhileMuted() {
		t.Fatal("unmuting kept the alert")
	}
	waitUntil(t, "your audio detected", s.SelfAudioDetected)
	_ = fk
}
