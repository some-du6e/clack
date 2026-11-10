package huddleui

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
)

// avHost is hcHost with the device's microphone prefs and a main window
// to raise.
type avHost struct {
	hcHost
	agc, ns bool
	raised  int
}

func (h *avHost) HuddleAudioPrefs() (bool, bool) { return h.agc, h.ns }
func (h *avHost) RaiseMainWindow()               { h.raised++ }

// avJoin joins the fake meeting without wiring OnChange, so the test
// drives the alone tracking itself.
func avJoin(t *testing.T, e *shotEnv) *huddle.Session {
	t.Helper()
	clip, err := os.ReadFile(filepath.Join("..", "..", "huddle", "testdata", "testsrc-320x180.h264"))
	if err != nil {
		t.Fatal(err)
	}
	fk := huddle.NewFake(clip, t.Errorf)
	t.Cleanup(fk.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := huddle.Join(ctx, huddle.Options{Client: fk.Caller(), SelfID: huddle.FakeSelfUser, Device: huddle.NewToneDevice(440),
		Logf: t.Logf, OnChange: func() {}}, hcChannel)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Leave)
	e.c.mu.Lock()
	e.c.s = s
	e.c.mu.Unlock()
	return s
}

// The OS hooks, swapped for recorders.
type avHooks struct {
	mu       sync.Mutex
	lock     func(bool)
	lockOff  int
	press    func()
	keyOff   int
	musicGot []string
}

func installAVHooks(t *testing.T) *avHooks {
	h := &avHooks{}
	ow, or, ol := watchLock, registerHotkey, loadMusic
	t.Cleanup(func() { watchLock, registerHotkey, loadMusic = ow, or, ol })
	watchLock = func(f func(bool)) (func(), error) {
		h.mu.Lock()
		h.lock = f
		h.mu.Unlock()
		return func() { h.mu.Lock(); h.lockOff++; h.mu.Unlock() }, nil
	}
	registerHotkey = func(f func()) (func(), error) {
		h.mu.Lock()
		h.press = f
		h.mu.Unlock()
		return func() { h.mu.Lock(); h.keyOff++; h.mu.Unlock() }, nil
	}
	loadMusic = func(file string) ([]int16, error) {
		h.mu.Lock()
		h.musicGot = append(h.musicGot, file)
		h.mu.Unlock()
		return make([]int16, 48000*2), nil
	}
	return h
}

func avWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// "Enable automatic gain control" and "Enable noise suppression" (the
// device's prefs) reach the session's options and the live session.
func TestHuddleMicProcessingPrefs(t *testing.T) {
	e := newShotEnv(t, false)
	h := &avHost{hcHost: *e.h, agc: false, ns: true}
	e.c.h = h
	o := e.c.options(e.h.app)
	if o.AGC || !o.NoiseSuppression {
		t.Fatalf("options AGC=%v NS=%v, want false/true", o.AGC, o.NoiseSuppression)
	}
	s := avJoin(t, e)
	e.c.SetMicProcessing(true, false)
	if agc, ns := s.MicProcessing(); !agc || ns {
		t.Fatalf("live session AGC=%v NS=%v after SetMicProcessing(true,false)", agc, ns)
	}
}

// huddles_global_mute: Mod+Shift+Space registers while you're in a huddle
// with the pref on, toggles your mic and brings the window forward;
// turning the pref off or leaving releases it.
func TestHuddleGlobalMute(t *testing.T) {
	hk := installAVHooks(t)
	e := newShotEnv(t, false)
	h := &avHost{hcHost: *e.h, agc: true}
	e.c.h = h
	setPrefs(e.h.app, map[string]any{"huddles_global_mute": true})
	s := avJoin(t, e)
	e.c.sessionStarted(s)
	if !e.c.GlobalMuteActive() || hk.press == nil {
		t.Fatal("the shortcut didn't register in a huddle with huddles_global_mute on")
	}
	was := s.State().Muted
	hk.press()
	if s.State().Muted == was {
		t.Fatal("the shortcut didn't toggle the mic")
	}
	hk.press()
	if s.State().Muted != was {
		t.Fatal("a second press didn't toggle it back")
	}
	if h.raised != 2 {
		t.Fatalf("main window raised %d times, want 2", h.raised)
	}
	setPrefs(e.h.app, map[string]any{"huddles_global_mute": false})
	e.c.PrefsChanged()
	if e.c.GlobalMuteActive() || hk.keyOff != 1 {
		t.Fatalf("pref off: still registered (released %d)", hk.keyOff)
	}
	setPrefs(e.h.app, map[string]any{"huddles_global_mute": true})
	e.c.PrefsChanged()
	if !e.c.GlobalMuteActive() {
		t.Fatal("pref back on: not registered")
	}
	e.c.ended(nil)
	if e.c.GlobalMuteActive() || hk.keyOff != 2 {
		t.Fatalf("left the huddle: still registered (released %d)", hk.keyOff)
	}
}

// huddles_disconnect_on_lock: a lock leaves the huddle (HuddleLocker's
// "DeviceLocked"); with the pref off, it doesn't.
func TestHuddleLeaveOnLock(t *testing.T) {
	hk := installAVHooks(t)
	e := newShotEnv(t, false)
	setPrefs(e.h.app, map[string]any{"huddles_disconnect_on_lock": false})
	s := avJoin(t, e)
	e.c.sessionStarted(s)
	if hk.lock == nil {
		t.Fatal("no lock watcher in a huddle")
	}
	hk.lock(true)
	if e.c.Session() == nil {
		t.Fatal("left on lock with huddles_disconnect_on_lock off")
	}
	hk.lock(false)
	setPrefs(e.h.app, map[string]any{"huddles_disconnect_on_lock": true})
	hk.lock(true)
	if e.c.Session() != nil {
		t.Fatal("still in the huddle after the screen locked")
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the session didn't end")
	}
	if hk.lockOff != 1 {
		t.Fatalf("lock watcher stopped %d times, want 1", hk.lockOff)
	}
}

// When alone in a huddle… Play music: Slack's INITIAL_ALONE waits
// huddles_play_music_timeout seconds, then the song plays (looped, your
// speakers only) until someone joins; alone again after that, 10 s.
func TestHuddleLoungeMusic(t *testing.T) {
	hk := installAVHooks(t)
	e := newShotEnv(t, false)
	setPrefs(e.h.app, map[string]any{"huddles_play_music_when_last": true, "huddles_play_music_timeout": 1})
	s := avJoin(t, e)
	e.c.sessionStarted(s)
	alone := huddle.State{Phase: huddle.Connected, Participants: []huddle.Participant{{UserID: huddle.FakeSelfUser, Self: true}}}
	together := huddle.State{Phase: huddle.Connected, Participants: []huddle.Participant{{UserID: huddle.FakeSelfUser, Self: true}, {UserID: huddle.FakeRemoteUser}}}

	if d := e.c.aloneDelay(false); d != time.Second {
		t.Fatalf("initial alone delay %v, want huddles_play_music_timeout's 1 s", d)
	}
	start := time.Now()
	e.c.musicTick(s, alone)
	time.Sleep(500 * time.Millisecond)
	if e.c.MusicPlaying() {
		t.Fatal("music before the timeout")
	}
	avWaitFor(t, "the lounge music", func() bool { return e.c.MusicPlaying() && s.LocalPlaying() })
	if el := time.Since(start); el < 900*time.Millisecond {
		t.Fatalf("music after %v, want ~1 s", el)
	}
	if len(hk.musicGot) != 1 || hk.musicGot[0] != "waiting_muzak-9187b04.mp3" {
		t.Fatalf("loaded %v, want Smooth Jazz (waiting_muzak)", hk.musicGot)
	}

	e.c.musicTick(s, together)
	if e.c.MusicPlaying() || s.LocalPlaying() {
		t.Fatal("music still playing with someone in the huddle")
	}
	if d := e.c.aloneDelay(true); d != 10*time.Second {
		t.Fatalf("in-progress alone delay %v, want 10 s", d)
	}

	// turning the pref off stops it at once
	setPrefs(e.h.app, map[string]any{"huddles_play_music_when_last": true, "huddles_play_music_timeout": 1, "huddles_music_player_song_key": "subway"})
	e.c.av.mu.Lock()
	e.c.av.together = false // as if just joined
	e.c.av.mu.Unlock()
	e.c.musicTick(s, alone)
	avWaitFor(t, "the music again", func() bool { return e.c.MusicPlaying() })
	if hk.musicGot[len(hk.musicGot)-1] != "subway_music-ff03dfb.mp3" {
		t.Fatalf("song pref subway loaded %v", hk.musicGot)
	}
	setPrefs(e.h.app, map[string]any{"huddles_play_music_when_last": false, "huddles_play_music_timeout": 1})
	e.c.PrefsChanged()
	if e.c.MusicPlaying() || s.LocalPlaying() {
		t.Fatal("music on after the pref went off")
	}

	// off from the start: alone stays quiet
	e.c.musicTick(s, together)
	e.c.av.mu.Lock()
	e.c.av.together = false
	e.c.av.mu.Unlock()
	e.c.musicTick(s, alone)
	time.Sleep(1300 * time.Millisecond)
	if e.c.MusicPlaying() {
		t.Fatal("music with huddles_play_music_when_last off")
	}
}
