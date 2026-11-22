//go:build darwin && cgo

package avf

// A message's audio files on AVFoundation, with no window: Slack's m4a
// (AAC: the aac renditions), mp3 and wav get their metadata (3 s), play
// to their end on the clock, land a seek and play 2x in half the time;
// paused, an audio-only item leaves no state watch running (the 50 ms
// timer a video keeps until its output is made). The voice clips' Opus in
// MP4 plays to its end through ui's chain.

import (
	"errors"
	"fmt"
	"image"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/goaudio"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

type audioRig struct {
	t      *testing.T
	p      *player
	id     string
	evC    chan media.Event
	mu     sync.Mutex
	events []media.Event
}

func newAudioRig(t *testing.T, fx mediatest.AudioFixture) *audioRig {
	t.Helper()
	history(true) // a failure logs what AVPlayer did (debug)
	id := fmt.Sprintf("FAVA%d", rigN.Add(1))
	src, _ := mediatest.Source(id, fx.Data, false)
	src.Mime, src.Ext = fx.Mime, fx.Ext
	pl, err := backend{}.New(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &audioRig{t: t, p: pl.(*player), id: id, evC: make(chan media.Event, 1024)}
	go func() {
		for ev := range r.p.Events() {
			r.mu.Lock()
			r.events = append(r.events, ev)
			r.mu.Unlock()
			select {
			case r.evC <- ev:
			default:
			}
		}
	}()
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("%s: the data requests %v", id, requestsOf(id))
		}
		r.p.Close()
	})
	return r
}

// wait is the next event of kind k within d; an error fails the test, and
// so does a timeout, with AVPlayer's history and its state then (its rate,
// timeControlStatus and reasonForWaitingToPlay).
func (r *audioRig) wait(k media.EventKind, within time.Duration) media.Event {
	r.t.Helper()
	ev, err := r.await(k, within)
	if err != nil {
		r.t.Fatalf("%v; events %v; state %+v\nAVPlayer: %s", err, r.kinds(), r.p.State(), r.p.debug())
	}
	return ev
}

// await is the next event of kind k within d, or why not: an error event,
// or the time running out.
func (r *audioRig) await(k media.EventKind, within time.Duration) (media.Event, error) {
	tm := time.NewTimer(within)
	defer tm.Stop()
	for {
		select {
		case ev := <-r.evC:
			if ev.Kind == k {
				return ev, nil
			}
			if ev.Kind == media.Error {
				return ev, fmt.Errorf("error %d waiting for %v", ev.State.Err, k)
			}
		case <-tm.C:
			return media.Event{}, fmt.Errorf("no %v within %v", k, within)
		}
	}
}

func (r *audioRig) kinds() []media.EventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []media.EventKind
	for _, ev := range r.events {
		if ev.Kind != media.TimeUpdate {
			out = append(out, ev.Kind)
		}
	}
	return out
}

func audioInOrder(got, want []media.EventKind) bool {
	i := 0
	for _, k := range got {
		if i < len(want) && k == want[i] {
			i++
		}
	}
	return i == len(want)
}

func audioFixture(name string) mediatest.AudioFixture {
	for _, fx := range mediatest.AudioFixtures() {
		if fx.Name == name {
			return fx
		}
	}
	panic("no fixture " + name)
}

func TestAVFPlaysAudio(t *testing.T) {
	for _, name := range []string{"aac", "mp3", "wav"} {
		t.Run(name, func(t *testing.T) {
			r := newAudioRig(t, audioFixture(name))
			md := r.wait(media.LoadedMetadata, 30*time.Second)
			if d := md.State.Duration; d < 2950*time.Millisecond || d > 3100*time.Millisecond {
				t.Errorf("duration %v, want 3 s", d)
			}
			r.wait(media.LoadedData, 10*time.Second)
			// paused and ready: no state watch while nothing changes
			time.Sleep(300 * time.Millisecond)
			if watching := r.watching(); watching {
				t.Error("a paused audio item keeps its 50 ms state watch")
			}
			r.p.Play()
			r.wait(media.Playing, 10*time.Second)
			t1 := time.Now()
			time.Sleep(time.Second)
			pos, wall := r.p.State().Pos, time.Since(t1)
			r.wait(media.Ended, 10*time.Second)
			took := time.Since(t1)
			t.Logf("%s: at %v of playing the position %v; ended %v after playing; events %v", name, wall.Round(time.Millisecond), pos, took.Round(time.Millisecond), r.kinds())
			if pos < 400*time.Millisecond || pos > wall+300*time.Millisecond {
				t.Errorf("after %v of playing the position is %v", wall, pos)
			}
			if took < 2500*time.Millisecond || took > 6*time.Second {
				t.Errorf("3 s played in %v", took)
			}
			if !audioInOrder(r.kinds(), []media.EventKind{media.LoadedMetadata, media.LoadedData, media.Play, media.Playing, media.Ended}) {
				t.Errorf("events %v", r.kinds())
			}
			if st := r.p.State(); !st.Ended || st.Size != (image.Point{}) {
				t.Errorf("after the end: %+v", st)
			}
		})
	}
}

// watching is whether the item's state watch timer runs.
func (r *audioRig) watching() bool {
	r.p.objMu.RLock()
	defer r.p.objMu.RUnlock()
	if r.p.obj == nil {
		return false
	}
	return watchingOf(r.p.obj)
}

// A seek before Play lands (the last second plays, in about a second);
// at Slack's 2x the whole file plays in about half its length.
func TestAVFAudioSeekAndRate(t *testing.T) {
	r := newAudioRig(t, audioFixture("aac"))
	r.wait(media.LoadedData, 30*time.Second)
	r.p.Seek(2 * time.Second)
	r.wait(media.Seeked, 10*time.Second)
	r.p.Play()
	r.wait(media.Playing, 10*time.Second)
	t0 := time.Now()
	r.wait(media.Ended, 10*time.Second)
	if took := time.Since(t0); took < 600*time.Millisecond || took > 3*time.Second {
		t.Errorf("the last second played in %v", took)
	}
	r.p.SetRate(2)
	r.p.Seek(0)
	r.wait(media.Seeked, 10*time.Second)
	r.p.Play()
	r.wait(media.Playing, 10*time.Second)
	t1 := time.Now()
	r.wait(media.Ended, 10*time.Second)
	if took := time.Since(t1); took < time.Second || took > 3500*time.Millisecond {
		t.Errorf("3 s at 2x played in %v, want about 1.5 s", took)
	}
}

// Replaying at Slack's 2x as a user does: after the clip's end (the play
// button's restart from 0), and after a pause and a seek back to the
// start, each on four fresh players, every one to its end in about 1.5 s.
// With AVPlayer's play and then its rate set to 2, about half of these
// never moved (train-163, train-164): AVPlayer said it played at 2 while
// the item's timebase stood at rate 0.
func TestAVFAudioReplaysAt2x(t *testing.T) {
	replays := []struct {
		name string
		to2x func(r *audioRig) // ends with the replay's Play
	}{
		{"after the end", func(r *audioRig) {
			r.p.Seek(2 * time.Second)
			r.wait(media.Seeked, 10*time.Second)
			r.p.Play()
			r.wait(media.Ended, 10*time.Second)
			r.p.SetRate(2)
			r.p.Play() // the restart: from 0
		}},
		{"after a pause and a seek", func(r *audioRig) {
			r.p.Play()
			r.wait(media.Playing, 10*time.Second)
			time.Sleep(500 * time.Millisecond)
			r.p.Pause()
			r.wait(media.Pause, 10*time.Second)
			r.p.SetRate(2)
			r.p.Seek(0)
			r.wait(media.Seeked, 10*time.Second)
			r.p.Play()
		}},
	}
	for _, c := range replays {
		t.Run(c.name, func(t *testing.T) {
			for i := 0; i < 4; i++ {
				r := newAudioRig(t, audioFixture("aac"))
				r.wait(media.LoadedData, 30*time.Second)
				c.to2x(r)
				t0 := time.Now()
				r.wait(media.Ended, 5*time.Second)
				if took := time.Since(t0); took > 3500*time.Millisecond {
					t.Errorf("replay %d: 3 s at 2x played in %v, want about 1.5 s", i, took)
				}
				r.p.Close()
			}
		})
	}
}

// A voice clip's Opus in MP4 (plain, and fragmented as Chromium's
// MediaRecorder writes it) plays to its end through ui's chain:
// AVFoundation, which on the runners played both itself, then Go audio,
// which takes over a file AVFoundation can't read before its metadata.
func TestAVFOpusClipPlays(t *testing.T) {
	noCard := func(func([]int16)) (io.Closer, error) { return nil, errors.New("no sound card (the test's)") }
	chain := media.NewChain(nil, backend{}, &goaudio.Backend{Output: noCard, Logf: t.Logf})
	for _, name := range []string{"opus-mp4", "opus-frag"} {
		t.Run(name, func(t *testing.T) {
			fx := audioFixture(name)
			id := fmt.Sprintf("FAVO%d", rigN.Add(1))
			src, _ := mediatest.Source(id, fx.Data, false)
			src.Mime, src.Ext = fx.Mime, fx.Ext
			pl, err := chain.New(src, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer pl.Close()
			pl.Play()
			var got []media.EventKind
			var md media.Event
			deadline := time.After(20 * time.Second)
			for ended := false; !ended; {
				select {
				case ev, ok := <-pl.Events():
					if !ok {
						t.Fatalf("events closed: %v", got)
					}
					if ev.Kind == media.TimeUpdate {
						continue
					}
					got = append(got, ev.Kind)
					switch ev.Kind {
					case media.LoadedMetadata:
						md = ev
					case media.Error:
						t.Fatalf("error %d; events %v", ev.State.Err, got)
					case media.Ended:
						ended = true
					}
				case <-deadline:
					t.Fatalf("no end within 20 s; events %v", got)
				}
			}
			if d := md.State.Duration; d < 2950*time.Millisecond || d > 3050*time.Millisecond {
				t.Errorf("duration %v, want 3 s", d)
			}
			by := "AVFoundation"
			if b, ok := pl.(interface{ Backend() string }); ok {
				by = b.Backend()
			}
			t.Logf("%s played by %s: %v", name, by, got)
		})
	}
}
