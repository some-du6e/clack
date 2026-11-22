//go:build darwin && cgo

package avf

// A message's audio files on AVFoundation, with no window: Slack's m4a
// (AAC: the aac renditions), mp3 and wav get their metadata (3 s), play
// to their end on the clock, land a seek and play 2x in half the time;
// paused, an audio-only item leaves no state watch running (the 50 ms
// timer a video keeps until its output is made). The voice clips' Opus in
// MP4 is logged (Go audio plays it when AVFoundation can't).

import (
	"fmt"
	"image"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
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

// wait is the next event of kind k within d; an error fails the test.
func (r *audioRig) wait(k media.EventKind, within time.Duration) media.Event {
	r.t.Helper()
	tm := time.NewTimer(within)
	defer tm.Stop()
	for {
		select {
		case ev := <-r.evC:
			if ev.Kind == k {
				return ev
			}
			if ev.Kind == media.Error {
				r.t.Fatalf("error %d waiting for %v; events %v", ev.State.Err, k, r.kinds())
			}
		case <-tm.C:
			r.t.Fatalf("no %v within %v; events %v; state %+v", k, within, r.kinds(), r.p.State())
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

// A voice clip's Opus in MP4: what AVFoundation makes of it, logged (Go
// audio plays it where AVFoundation can't).
func TestAVFOpusClipLogged(t *testing.T) {
	for _, name := range []string{"opus-mp4", "opus-frag"} {
		fx := audioFixture(name)
		id := fmt.Sprintf("FAVO%d", rigN.Add(1))
		src, _ := mediatest.Source(id, fx.Data, false)
		src.Mime, src.Ext = fx.Mime, fx.Ext
		pl, err := backend{}.New(src, nil)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		deadline := time.After(10 * time.Second)
		pl.Play()
	loop:
		for {
			select {
			case ev, ok := <-pl.Events():
				if !ok {
					break loop
				}
				if ev.Kind == media.TimeUpdate {
					continue
				}
				got = append(got, fmt.Sprintf("%v (pos %v, dur %v, err %d)", ev.Kind, ev.State.Pos.Round(time.Millisecond), ev.State.Duration.Round(time.Millisecond), ev.State.Err))
				if ev.Kind == media.Ended || ev.Kind == media.Error {
					break loop
				}
			case <-deadline:
				got = append(got, "(nothing more within 10 s)")
				break loop
			}
		}
		pl.Close()
		t.Logf("%s on AVFoundation: %v", name, got)
	}
}
