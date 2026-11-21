package huddle

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle/avperm"
)

// Speaker picks mid-huddle (SwitchSpeakers): the output you're on plays
// until the new one is swapped in, and when picks overlap the newest one
// plays, whichever output finishes opening last.

// heldOutput is an output whose open can be held (a Bluetooth output still
// opening when you pick again); it counts the loud samples it plays.
type heldOutput struct {
	name   string
	hold   chan struct{} // the speakers' Start waits for it (nil: no wait)
	opened func(*heldOutput)
	loud   atomic.Int64
	closed atomic.Bool
	stop   chan struct{}
	once   sync.Once
}

// loudSound and loudSample tell the test's sound from the fake room's
// tone, which stays under loudSample.
const loudSound, loudSample = 24000, 12000

func (d *heldOutput) Start(capture func([]int16), playback func([]int16)) error {
	if playback != nil {
		d.opened(d)
		if d.hold != nil {
			<-d.hold
		}
	}
	go func() {
		tk := time.NewTicker(10 * time.Millisecond)
		defer tk.Stop()
		mic := make([]int16, 480)
		buf := make([]int16, 2*480) // 10 ms, stereo
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
					if v > loudSample {
						d.loud.Add(1)
					}
				}
			}
		}
	}()
	return nil
}

func (d *heldOutput) Close() { d.once.Do(func() { d.closed.Store(true); close(d.stop) }) }

// grows reports whether n() grows by more than by within d.
func grows(n func() int64, by int64, d time.Duration) bool {
	n0 := n()
	for dl := time.Now().Add(d); time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
		if n()-n0 > by {
			return true
		}
	}
	return false
}

func TestOverlappingSpeakerPicks(t *testing.T) {
	SelectAudioDevices("", "")
	t.Cleanup(func() { SelectAudioDevices("", "") })
	var mu sync.Mutex
	outs := map[string]*heldOutput{} // the speakers opened on each output
	speakers := func(id string) *heldOutput {
		mu.Lock()
		defer mu.Unlock()
		return outs[id]
	}
	slow := make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(slow) }) })
	o := Options{
		MicAccess: func() <-chan avperm.Status {
			ch := make(chan avperm.Status, 1)
			ch <- avperm.Authorized
			return ch
		},
		OpenAudio: func(func(string, ...any)) (Device, error) {
			_, out := SelectedAudioDevices()
			d := &heldOutput{name: out, stop: make(chan struct{}), opened: func(d *heldOutput) {
				mu.Lock()
				outs[d.name] = d
				mu.Unlock()
			}}
			if out == "out-slow" {
				d.hold = slow
			}
			return d, nil
		},
	}
	_, s := fakeJoin(t, o)
	waitUntil(t, "the speakers open", func() bool { return speakers("") != nil })
	first := speakers("")
	sound := make([]int16, 2*48000) // a second of it, stereo
	for i := range sound {
		sound[i] = loudSound
	}
	s.PlaySound("test", sound)
	if !grows(first.loud.Load, 2000, 3*time.Second) {
		t.Fatal("the speakers don't play the sound")
	}

	// Speaker › a slow output: while it opens, you keep hearing the huddle
	// on the output you're on (it used to go quiet the moment the open
	// began, the playback buffer backing up meanwhile into lasting delay)
	SelectAudioDevices("", "out-slow")
	var slowPlayed atomic.Bool
	s.SwitchSpeakers(func() { slowPlayed.Store(true) })
	waitUntil(t, "out-slow opening", func() bool { return speakers("out-slow") != nil })
	s.PlaySound("test", sound)
	if !grows(first.loud.Load, 2000, 2*time.Second) {
		t.Errorf("while out-slow opens, the output you're on went quiet (%d loud samples in all)", first.loud.Load())
	}

	// you pick again before it's open: a fast output, which opens at once
	SelectAudioDevices("", "out-fast")
	s.SwitchSpeakers(nil)
	waitUntil(t, "out-fast swapped in", func() bool { f := speakers("out-fast"); return f != nil && first.closed.Load() })
	fast := speakers("out-fast")

	// then the slow output finishes opening, after the pick that replaced it
	release.Do(func() { close(slow) })
	slowOut := speakers("out-slow")
	waitUntil(t, "one of out-slow and out-fast closed", func() bool { return slowOut.closed.Load() || fast.closed.Load() })
	time.Sleep(200 * time.Millisecond)
	s.PlaySound("test", sound)
	fastHears := grows(fast.loud.Load, 2000, 2*time.Second)
	if fast.closed.Load() {
		t.Errorf("out-fast, your last pick, was closed: the slow open you replaced was swapped in after it")
	}
	if !slowOut.closed.Load() {
		t.Errorf("out-slow, the pick you replaced, is still open")
	}
	if !fastHears {
		t.Errorf("out-fast plays nothing (%d loud samples) and out-slow %d: the huddle is silent on the output you picked",
			fast.loud.Load(), slowOut.loud.Load())
	}
	if slowPlayed.Load() {
		t.Errorf("the replaced pick ran its follow-up (Test Speaker's sound) as if its output played")
	}
}
