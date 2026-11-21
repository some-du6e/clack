package huddleui

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Karim, α.112: "test speaker feels wonky". Slack's Test Speaker
// (testspeaker.go: ifMn useTestSpeakerMenuItem, Ofe8, cfKp) checked here:
// its icon, the menus it leaves open, the music it plays beside, and the
// output a Speaker pick plays it on.

func TestTestSpeakerIconLikeSlack(t *testing.T) {
	uipref.Reset() // Allow animations on, Slack's default
	t.Cleanup(uipref.Reset)
	e := newShotEnv(t, false)
	ms := time.Millisecond
	once := []time.Duration{0}
	for _, c := range []struct {
		at     time.Duration
		icon   string
		jiggle bool
	}{
		{0, "sound-filled", true}, {249 * ms, "sound-filled", true},
		{250 * ms, "sound-medium-filled", true}, {500 * ms, "sound-filled", true},
		{750 * ms, "sound-down-filled", true}, {1000 * ms, "sound-medium-filled", true},
		{1249 * ms, "sound-medium-filled", true}, {1250 * ms, "sound", false}, {3 * time.Second, "sound", false},
	} {
		if got, jig := speakerIconAt(e.c, once, c.at); got != c.icon || jig != c.jiggle {
			t.Errorf("%v after the click: %s (jiggling %v), Slack's %s (jiggling %v)", c.at, got, jig, c.icon, c.jiggle)
		}
	}
	// a second click while it runs: sound-filled until the next step, and
	// the count isn't restarted
	twice := []time.Duration{0, 800 * ms}
	for _, c := range []struct {
		at   time.Duration
		icon string
	}{{900 * ms, "sound-filled"}, {1000 * ms, "sound-medium-filled"}, {1250 * ms, "sound"}} {
		if got, _ := speakerIconAt(e.c, twice, c.at); got != c.icon {
			t.Errorf("clicked again at 800 ms: at %v %s, Slack's %s", c.at, got, c.icon)
		}
	}
	// Allow animations off: nothing changes
	uipref.Set(store.Prefs{NoAnimations: true})
	if got, jig := speakerIconAt(e.c, once, 100*ms); got != "sound" || jig {
		t.Errorf("animations off: %s (jiggling %v), want sound, still", got, jig)
	}
	uipref.Reset()

	// the row: "Test Speaker"; the bottom bar's popover closes on it, the
	// huddle window's devices menu stays open
	s := avJoin(t, e)
	if r := testSpeakerRow(e.c, s, false); r.Label != "Test Speaker" || !r.KeepOpen {
		t.Errorf("in the huddle window: %q, stays open %v; want Test Speaker, open", r.Label, r.KeepOpen)
	}
	if r := testSpeakerRow(e.c, s, true); r.KeepOpen {
		t.Error("in the bottom bar the menu stays open after Test Speaker; Slack's popover closes on any pick")
	}
}

// TestTestSpeakerKeepsTheMusic: the test sound plays beside the lounge
// music (each of Slack's sounds is its own <audio> element), not in its
// place.
func TestTestSpeakerKeepsTheMusic(t *testing.T) {
	installAVHooks(t) // every sound: one second, looped or not
	e := newShotEnv(t, false)
	setPrefs(e.h.app, map[string]any{"huddles_play_music_when_last": true, "huddles_play_music_timeout": 1})
	s := avJoin(t, e)
	e.c.sessionStarted(s)
	alone := huddle.State{Phase: huddle.Connected, Participants: []huddle.Participant{{UserID: huddle.FakeSelfUser, Self: true}}}
	e.c.musicTick(s, alone)
	avWaitFor(t, "the lounge music", func() bool { return e.c.MusicPlaying() && s.LocalPlaying() })
	r := testSpeakerRow(e.c, s, false)
	if r.Do == nil {
		t.Fatal("no Test Speaker row")
	}
	r.Do()
	time.Sleep(1600 * time.Millisecond) // the test sound has played out
	if !e.c.MusicPlaying() || !s.LocalPlaying() {
		t.Fatalf("after Test Speaker the lounge music has stopped (playing %v, its sound %v)", e.c.MusicPlaying(), s.LocalPlaying())
	}
}

const testSoundLevel = 20000

// fakeOutput is a sound card output that takes 100 ms to open (a WASAPI
// or Core Audio open does) and counts the samples it played of the test
// sound: the fake Chime's tone (0.25 of full scale) never gets past 11000,
// the test sound (testSoundLevel) always does.
type fakeOutput struct {
	name  string
	heard atomic.Int64
	stop  chan struct{}
	once  sync.Once
}

func (d *fakeOutput) Start(capture func([]int16), playback func([]int16)) error {
	time.Sleep(100 * time.Millisecond)
	go func() {
		tk := time.NewTicker(10 * time.Millisecond)
		defer tk.Stop()
		buf := make([]int16, 960)
		mic := make([]int16, 480)
		for {
			select {
			case <-d.stop:
				return
			case <-tk.C:
			}
			if capture != nil {
				capture(mic)
			}
			if playback != nil {
				playback(buf)
				for _, v := range buf {
					if v > 11000 {
						d.heard.Add(1)
					}
				}
			}
		}
	}()
	return nil
}

func (d *fakeOutput) Close() { d.once.Do(func() { close(d.stop) }) }

// TestSpeakerPickPlaysOnTheNewOutput: picking a Speaker saves it, reopens
// the speakers on it and plays the test sound there, not on the output you
// left (useAudioOutSettingsMenuItems: the sound plays on the preferred
// sink, once it's the new one). The old output plays the huddle until the
// new one is swapped in, then goes quiet (huddle.TestOverlappingSpeakerPicks),
// so what it played before the test sound began (the room's tone, which
// can pass testSoundLevel's threshold at its peaks) doesn't count.
func TestSpeakerPickPlaysOnTheNewOutput(t *testing.T) {
	oldList, oldLoad := listAudioDevices, loadMusic
	var firstOut atomic.Pointer[fakeOutput]
	var soundAt atomic.Int64 // the old output's loud samples when the test sound was asked for
	soundAt.Store(-1)
	listAudioDevices = func(capture bool) ([]huddle.AudioDevice, error) {
		if capture {
			return []huddle.AudioDevice{{ID: "m1", Name: "Microphone", Default: true}}, nil
		}
		return []huddle.AudioDevice{{ID: "o1", Name: "Speakers", Default: true}, {ID: "o2", Name: "Headphones"}}, nil
	}
	loadMusic = func(string) ([]int16, error) { // half a second the outputs can tell from the huddle's tone
		if f := firstOut.Load(); f != nil {
			soundAt.CompareAndSwap(-1, f.heard.Load())
		}
		pcm := make([]int16, 48000)
		for i := range pcm {
			pcm[i] = testSoundLevel
		}
		return pcm, nil
	}
	t.Cleanup(func() { listAudioDevices, loadMusic = oldList, oldLoad; huddle.SelectAudioDevices("", "") })
	huddle.SelectAudioDevices("", "")

	var mu sync.Mutex
	outs := map[string]*fakeOutput{}
	open := func(func(string, ...any)) (huddle.Device, error) {
		_, out := huddle.SelectedAudioDevices()
		d := &fakeOutput{name: out, stop: make(chan struct{})}
		mu.Lock()
		if outs[out] == nil { // the speakers' (the microphone's opens after them, on the same id)
			outs[out] = d
		}
		mu.Unlock()
		return d, nil
	}
	granted := func() <-chan avperm.Status {
		ch := make(chan avperm.Status, 1)
		ch <- avperm.Authorized
		return ch
	}
	e := newShotEnv(t, false)
	fk := huddle.NewFake(nil, t.Errorf)
	t.Cleanup(fk.Close)
	fk.SoloRoom() // no one else (the fake's mix still carries its tone)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := huddle.Join(ctx, huddle.Options{Client: fk.Caller(), SelfID: huddle.FakeSelfUser, MicAccess: granted, OpenAudio: open,
		Logf: t.Logf, OnChange: func() {}}, hcChannel)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Leave)
	e.c.mu.Lock()
	e.c.s = s
	e.c.mu.Unlock()
	waitState(t, s, "connected", func(st huddle.State) bool { return st.Phase == huddle.Connected })
	mu.Lock()
	first := outs[""]
	mu.Unlock()
	if first == nil {
		t.Fatal("the speakers didn't open")
	}
	firstOut.Store(first)

	// Speaker › Headphones
	var pick func()
	for _, it := range testAudioRows(e.c, s) {
		if it.Key == "av-speaker-device-menu-item_Headphones" {
			pick = it.Do
		}
	}
	if pick == nil {
		t.Fatal("no Headphones row")
	}
	pick()
	time.Sleep(900 * time.Millisecond)
	mu.Lock()
	head := outs["o2"]
	mu.Unlock()
	if head == nil {
		t.Fatal("the speakers didn't reopen on Headphones")
	}
	at := soundAt.Load()
	if at < 0 {
		t.Fatal("the test sound never played")
	}
	if n := first.heard.Load() - at; n > 0 {
		t.Errorf("the test sound played %d samples on the Speakers you switched away from", n)
	}
	t.Logf("the Speakers played %d loud samples of the huddle before the test sound began", at)
	if n := head.heard.Load(); n < 20000 {
		t.Errorf("the Headphones played %d samples of the test sound, want it all (48000)", n)
	}
}
