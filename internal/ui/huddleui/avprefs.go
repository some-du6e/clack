package huddleui

import (
	"log"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/globalkey"
	"github.com/some-du6e/clack/internal/ui/screenlock"
)

// Preferences > Audio & video, while you're in a huddle:
//
//   - "Enable automatic gain control" / "Enable noise suppression": the
//     microphone's processing (huddle/apm.go), kept on this device like
//     Slack's localStorage prefs.audio (the Host's HuddleAudioPrefs).
//   - "Allow keyboard shortcut to mute" (huddles_global_mute): Mod+Shift+
//     Space toggles your mic from anywhere while the huddle is up
//     (globalkey; Slack's globalMuteShortcut.enable on STARTED, disable on
//     leaving; the press is toggleSelfMicrophone).
//   - "Automatically leave huddles" when the screen locks
//     (huddles_disconnect_on_lock; screenlock and Slack's HuddleLocker:
//     a false→true lock edge with the pref on leaves, "DeviceLocked").
//   - "When alone in a huddle… Play music" (huddles_play_music_when_last)
//     and its delay (huddles_play_music_timeout): the lounge music below.

// AudioPrefsHost is the ui root's device prefs for the microphone (Slack's
// localStorage prefs.audio: enableAGC defaults on, noise suppression off).
type AudioPrefsHost interface {
	HuddleAudioPrefs() (agc, ns bool)
}

// RaiseHost brings Clack's main window forward: Slack's main process
// shows and focuses it when the global mute shortcut fires while no Slack
// window has focus (registerGlobalHandler's i6).
type RaiseHost interface{ RaiseMainWindow() }

func (c *Controller) audioPrefs() (agc, ns bool) {
	if h, ok := c.h.(AudioPrefsHost); ok {
		return h.HuddleAudioPrefs()
	}
	return true, false
}

// VideoPrefsHost is the ui root's camera background effect: Slack's
// localStorage prefs.video.effect, which Preferences > Audio & video's
// "Blur your video background…" toggles between unset and "blur".
type VideoPrefsHost interface {
	HuddleVideoEffect() huddle.VideoEffect
}

func (c *Controller) videoEffect() huddle.VideoEffect {
	if h, ok := c.h.(VideoPrefsHost); ok {
		return h.HuddleVideoEffect()
	}
	return huddle.VideoEffectNone
}

// SetVideoEffect applies the background effect to the huddle's camera and
// the open join preview at once (Slack's prefs dialog dispatches it to the
// client store, and the huddle's camera device follows). Any goroutine.
func (c *Controller) SetVideoEffect(e huddle.VideoEffect) {
	if s := c.Session(); s != nil {
		s.SetVideoEffect(e)
	}
	c.mu.Lock()
	w := c.pwin
	c.mu.Unlock()
	if w != nil {
		w.mu.Lock()
		w.fx = e
		w.mu.Unlock()
		w.invalidate()
	}
}

// SetMicProcessing applies the AGC and noise suppression prefs to the
// huddle you're in (Preferences changed them). Any goroutine.
func (c *Controller) SetMicProcessing(agc, ns bool) {
	if s := c.Session(); s != nil {
		s.SetMicProcessing(agc, ns)
	}
}

// avState is the per-huddle OS hooks and the lounge music.
type avState struct {
	mu       sync.Mutex
	s        *huddle.Session // the huddle they're for
	lockStop func()
	keyStop  func()
	keyErr   error

	together   bool // someone else has been in the huddle with you
	alone      bool
	musicT     *time.Timer
	musicOn    bool
	musicStart time.Time // the alone timer's start (tests)
}

// Hooks the tests swap.
var (
	watchLock      = screenlock.Watch
	registerHotkey = globalkey.Register
	loadMusic      = func(file string) ([]int16, error) { return loadSound(file) }
)

// sessionStarted wires the OS hooks for s (after Start joined).
func (c *Controller) sessionStarted(s *huddle.Session) {
	c.av.mu.Lock()
	c.av.s = s
	c.av.together, c.av.alone, c.av.musicOn = false, false, false
	watching := c.av.lockStop != nil
	c.av.mu.Unlock()
	if !watching {
		stop, err := watchLock(c.onScreenLock)
		if err != nil {
			log.Printf("huddle: screen lock events: %v", err)
		} else {
			c.av.mu.Lock()
			c.av.lockStop = stop
			c.av.mu.Unlock()
		}
	}
	c.syncGlobalMute()
	c.onSession()
}

// sessionEnded undoes sessionStarted.
func (c *Controller) sessionEnded() {
	c.av.mu.Lock()
	lock, key := c.av.lockStop, c.av.keyStop
	c.av.lockStop, c.av.keyStop, c.av.keyErr = nil, nil, nil
	if c.av.musicT != nil {
		c.av.musicT.Stop()
		c.av.musicT = nil
	}
	c.av.s, c.av.musicOn = nil, false
	c.av.mu.Unlock()
	if lock != nil {
		lock()
	}
	if key != nil {
		key()
	}
}

// PrefsChanged re-reads the huddle prefs mid-huddle (users.prefs
// changed): the shortcut comes or goes, the music stops when turned off
// (setUserPrefShouldPlayLoungeMusic false runs stopMusic).
func (c *Controller) PrefsChanged() {
	s := c.Session()
	if s == nil {
		return
	}
	c.syncGlobalMute()
	if app := c.h.Core(); app != nil && !app.PrefBool("huddles_play_music_when_last", false) {
		c.stopMusic(s)
	}
	c.onSession()
}

// syncGlobalMute registers or releases Mod+Shift+Space to match
// huddles_global_mute while in a huddle.
func (c *Controller) syncGlobalMute() {
	app := c.h.Core()
	want := app != nil && c.Session() != nil && app.PrefBool("huddles_global_mute", false)
	c.av.mu.Lock()
	have := c.av.keyStop != nil
	c.av.mu.Unlock()
	switch {
	case want && !have:
		stop, err := registerHotkey(c.onGlobalMute)
		c.av.mu.Lock()
		c.av.keyStop, c.av.keyErr = stop, err
		c.av.mu.Unlock()
		if err != nil {
			log.Printf("huddle: global mute shortcut %s: %v", globalkey.Shortcut, err)
		}
	case !want && have:
		c.av.mu.Lock()
		stop := c.av.keyStop
		c.av.keyStop = nil
		c.av.mu.Unlock()
		stop()
	}
}

// GlobalMuteActive reports whether the shortcut is registered (tests).
func (c *Controller) GlobalMuteActive() bool {
	c.av.mu.Lock()
	defer c.av.mu.Unlock()
	return c.av.keyStop != nil
}

// onGlobalMute is the shortcut: toggleSelfMicrophone, then the main
// window comes forward if no Clack window has focus.
func (c *Controller) onGlobalMute() {
	s := c.Session()
	if s == nil {
		return
	}
	s.SetMuted(!s.State().Muted)
	c.changed()
	if r, ok := c.h.(RaiseHost); ok {
		c.post(r.RaiseMainWindow)
	}
}

// onScreenLock is HuddleLocker: leave on a lock edge while the pref is on.
func (c *Controller) onScreenLock(locked bool) {
	if !locked {
		return
	}
	app := c.h.Core()
	if app == nil || !app.PrefBool("huddles_disconnect_on_lock", false) {
		return
	}
	if s := c.Session(); s != nil {
		log.Printf("huddle: the screen locked, leaving (DeviceLocked)")
		c.leave(s)
	}
}

// ---- lounge music ----

// The songs (HuddleLoungeMusicManager's getSongsMetadata, IeZG, and their
// BLe4 CDN files); the huddles_music_player_song_key pref picks one,
// "jazz" by default. The Miles Davis three play only where Slack enables
// them (isMilesDavisEnabled); Clack falls back to "jazz" for them.
var loungeSongs = map[string]string{
	"jazz":    "waiting_muzak-9187b04.mp3",     // Smooth Jazz
	"subway":  "subway_music-ff03dfb.mp3",      // Subway Music
	"waiting": "waiting_room-d5f7069.mp3",      // Waiting Room
	"whistle": "whistle_tune-894c024.mp3",      // Whistle Tune
	"focus":   "focus_beats-ee1e4a9.mp3",       // Focus Beats
	"disco":   "disco_party-6f29a34.mp3",       // Happy Bass
	"zen":     "moment_of_zen-7047c77.mp3",     // Sound Bath
	"nature":  "nature_meditation-495e2c5.mp3", // Nature Meditation
	"piano":   "piano_concerto-d00d84e.mp3",    // Piano Concerto
}

// loungeSong is the CDN file for the user's song pref.
func (c *Controller) loungeSong() string {
	key := "jazz"
	if app := c.h.Core(); app != nil {
		if k := app.PrefString("huddles_music_player_song_key"); k != "" {
			key = k
		}
	}
	if f, ok := loungeSongs[key]; ok {
		return f
	}
	return loungeSongs["jazz"]
}

// Slack's alone substates: INITIAL_ALONE waits huddles_play_music_timeout
// seconds (1, 30 or 60; unset is 60) before its notice and the music;
// after someone has been with you, IN_PROGRESS_ALONE waits 10 s
// (resolveSubstateTimeoutMs). A peer joining stops it (STOP_LOUNGE_MUSIC).
const inProgressAloneAfter = 10 * time.Second

func (c *Controller) aloneDelay(together bool) time.Duration {
	if together {
		return inProgressAloneAfter
	}
	sec := 60
	if app := c.h.Core(); app != nil {
		sec = app.PrefInt("huddles_play_music_timeout", 60)
	}
	return time.Duration(max(sec, 1)) * time.Second
}

// musicTick follows the huddle's people (onSession).
func (c *Controller) musicTick(s *huddle.Session, st huddle.State) {
	if st.Phase != huddle.Connected {
		return
	}
	others := 0
	for _, p := range st.Participants {
		if !p.Self {
			others++
		}
	}
	c.av.mu.Lock()
	if c.av.s != s {
		c.av.mu.Unlock()
		return
	}
	if others > 0 {
		c.av.together = true
		wasAlone := c.av.alone
		c.av.alone = false
		if c.av.musicT != nil {
			c.av.musicT.Stop()
			c.av.musicT = nil
		}
		on := c.av.musicOn
		c.av.mu.Unlock()
		if on || wasAlone {
			c.stopMusic(s)
		}
		return
	}
	if c.av.alone {
		c.av.mu.Unlock()
		return
	}
	c.av.alone = true
	d := c.aloneDelay(c.av.together)
	c.av.musicStart = time.Now()
	c.av.musicT = time.AfterFunc(d, func() { c.startMusic(s) })
	c.av.mu.Unlock()
}

// startMusic is the alone notice going active: the song plays, looped, on
// your speakers only, if huddles_play_music_when_last is on.
func (c *Controller) startMusic(s *huddle.Session) {
	app := c.h.Core()
	if app == nil || !app.PrefBool("huddles_play_music_when_last", false) {
		return
	}
	c.av.mu.Lock()
	if c.av.s != s || !c.av.alone || c.av.musicOn {
		c.av.mu.Unlock()
		return
	}
	c.av.musicOn = true
	c.av.mu.Unlock()
	file := c.loungeSong()
	pcm, err := loadMusic(file)
	if err != nil {
		log.Printf("huddle: lounge music %s: %v", file, err)
		return
	}
	c.av.mu.Lock()
	still := c.av.s == s && c.av.alone && c.av.musicOn
	c.av.mu.Unlock()
	if still {
		s.PlayLocal(pcm, true)
		log.Printf("huddle: started lounge music (%s)", file)
	}
}

func (c *Controller) stopMusic(s *huddle.Session) {
	c.av.mu.Lock()
	c.av.musicOn = false
	c.av.mu.Unlock()
	s.PlayLocal(nil, false)
}

// MusicPlaying reports whether the lounge music is on (tests, the bar).
func (c *Controller) MusicPlaying() bool {
	c.av.mu.Lock()
	defer c.av.mu.Unlock()
	return c.av.musicOn
}
