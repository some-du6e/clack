package huddle

import (
	"math"
	"os"
	"time"
)

// Switching the microphone or the speakers mid-huddle: the toolbar's
// Audio settings menu (gantry-v2-async-client-boot-shared LmPT, its
// Microphone list gmEJ and Speaker list QdEk) picks a device with
// setNewPreferredDevice (hJFA cT), which the media session applies at
// once. Clack keeps the pick in SelectAudioDevices and reopens the sound
// card side it changed.

// SwitchAudio reopens the speakers and/or the microphone on the devices
// SelectAudioDevices picked. It returns at once: the speakers reopen on
// their own goroutine, the microphone in the background like at join
// (openMic, after the OS permission). A session on one test device
// (Options.Device, CLACK_HUDDLE_TONE) has nothing to switch.
func (s *Session) SwitchAudio(speakers, mic bool) {
	if s.o.Device != nil || os.Getenv("CLACK_HUDDLE_TONE") != "" {
		return
	}
	s.mu.Lock()
	ended := s.ended
	s.mu.Unlock()
	if ended {
		return
	}
	if speakers {
		go s.switchSpeakers()
	}
	if mic {
		go s.openMic()
	}
}

// switchSpeakers opens the chosen output, then closes the old one.
func (s *Session) switchSpeakers() {
	d := s.openSpeakers()
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		d.Close()
		return
	}
	old := s.dev
	s.dev = d
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	s.changed()
}

// ---- your audio level, as Slack measures it ----

// slackLevel is Slack's audio level for a window of samples: the
// AudioAnalyser's computeStrength (the window's highest positive
// excursion t, at least .01: (ln .01 − ln t)/ln .01, clamped to 0..1)
// divided by 6, which is what the level publishers send (client-boot
// sfEG and V2Jq's getLevel, M3Ec's onUpdatedSelfAudioLevel e/6).
func slackLevel(peak float64) float64 {
	t := max(peak, 0.01)
	st := (math.Log(0.01) - math.Log(t)) / math.Log(0.01)
	return min(max(st, 0), 1) / 6
}

// levelEvery is AudioLevelMonitor's cadence (window.setTimeout(…, 300)).
const levelEvery = 300 * time.Millisecond

// levelWindow collects your microphone's peak over each 300 ms window.
type levelWindow struct {
	start time.Time
	peak  float64
}

// add takes one captured frame and returns the window's level when the
// window is over (ok), starting the next one.
func (w *levelWindow) add(pcm []int16, now time.Time) (lv float64, ok bool) {
	if w.start.IsZero() {
		w.start = now
	}
	for _, v := range pcm {
		if f := float64(v) / 32768; f > w.peak {
			w.peak = f
		}
	}
	if now.Sub(w.start) < levelEvery {
		return 0, false
	}
	lv = slackLevel(w.peak)
	w.start, w.peak = now, 0
	return lv, true
}

// speakingMuted is useHuddleMutedVolumeTracker (gantry-v2-async-client-
// boot-shared owyz): while you're muted, a level of .065 or more raises
// the SpeakingWhenMuted alert once it was there 3 times before in a row
// (the 4th); a lower non-zero level starts the count again, and zero
// levels are skipped. Unmuting drops the alert (setMuted).
type speakingMuted struct {
	count int
}

const mutedLevel = 0.065

// level runs one level update; it reports whether the alert goes up.
func (m *speakingMuted) level(lv float64) bool {
	switch {
	case lv == 0:
		return false
	case lv >= mutedLevel && m.count > 2:
		m.count = 0
		return true
	case lv >= mutedLevel:
		m.count++
	default:
		m.count = 0
	}
	return false
}

// SpeakingWhileMuted reports Slack's SpeakingWhenMuted alert: you talked
// while muted (the window shows "You’re muted.").
func (s *Session) SpeakingWhileMuted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutedTalk
}

// ClearSpeakingWhileMuted drops the alert (the banner closed or timed
// out: removeHuddleAlert("SpeakingWhenMuted")).
func (s *Session) ClearSpeakingWhileMuted() {
	s.mu.Lock()
	was := s.mutedTalk
	s.mutedTalk = false
	s.mu.Unlock()
	if was {
		s.changed()
	}
}

// SelfAudioDetected reports whether your microphone has sent sound since
// it opened: Slack's hasSelfAudioBeenDetected, which the
// huddles_switch_microphone_helper check polls. Sound here is a non-zero
// Slack level (a peak above .01) while you weren't muted.
func (s *Session) SelfAudioDetected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selfAudio
}

// noteLevel takes one window's level (sendAudio, every 300 ms).
func (s *Session) noteLevel(lv float64, muted bool) {
	raise := false
	s.mu.Lock()
	if muted {
		if !s.mutedTalk && s.mutedTrack.level(lv) {
			s.mutedTalk, raise = true, true
		}
	} else {
		s.mutedTrack = speakingMuted{}
		if lv > 0 && !s.selfAudio {
			s.selfAudio, raise = true, true
		}
	}
	s.mu.Unlock()
	if raise {
		s.changed()
	}
}
