//go:build windows && cgo

package mf

// A message's audio files on Media Foundation: the engine made audio only
// (MF_MEDIA_ENGINE_AUDIOONLY), with no window and no device of Gio's
// (nothing draws an audio player's frames, and before this the engine
// waited for Gio's ask that never came: no audio file played on
// Windows). The runners have no audio output, so there the engine is
// made muted (MF_MEDIA_ENGINE_FORCEMUTE) too: these check the decode and
// the timing, not the sound. Slack's m4a (AAC: the aac renditions), mp3
// and wav play to their end on the clock, a seek lands, and 2x plays in
// half the time. The voice clips' Opus in MP4 is logged (Go audio plays
// it when Media Foundation can't).

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
	need(t, backend{}.Available())
	id := fmt.Sprintf("FMFA%d", rigN.Add(1))
	src, _ := mediatest.Source(id, fx.Data, false)
	src.Mime, src.Ext = fx.Mime, fx.Ext
	hookMu.Lock()
	born[id] = time.Now()
	hookMu.Unlock()
	pl, err := backend{}.New(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &audioRig{t: t, p: pl.(*player), id: id, evC: make(chan media.Event, 1024)}
	if !r.p.audioOnly {
		t.Fatalf("%s (%s, %s) isn't played audio only", fx.Name, fx.Mime, fx.Ext)
	}
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
			t.Logf("%s: the engine's events %s", id, traceOf(id))
		}
		r.p.Close()
		select {
		case <-r.p.done:
		case <-time.After(10 * time.Second):
			t.Error("the player's media thread outlived Close by 10 s")
		}
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
				r.t.Fatalf("error %d waiting for %v; events %v; the engine's events %s", ev.State.Err, k, r.kinds(), traceOf(r.id))
			}
		case <-tm.C:
			r.t.Fatalf("no %v within %v; events %v; state %+v; the engine's events %s", k, within, r.kinds(), r.p.State(), traceOf(r.id))
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

// thread is what the media thread holds: whether a device was bound, and
// what it found of the audio output.
func (r *audioRig) thread() (bound bool, audio string) {
	type answer struct {
		bound bool
		audio string
	}
	done := make(chan answer, 1)
	r.p.post(func() { done <- answer{r.p.bound, r.p.audio} })
	select {
	case a := <-done:
		return a.bound, a.audio
	case <-time.After(5 * time.Second):
		return false, "(the media thread didn't answer)"
	}
}

func audioFixture(name string) mediatest.AudioFixture {
	for _, fx := range mediatest.AudioFixtures() {
		if fx.Name == name {
			return fx
		}
	}
	panic("no fixture " + name)
}

// Each of Slack's own audio formats plays to its end with no window: its
// metadata (3 s), the position running with the clock while it plays, the
// end about 3 s after it played, the element's events in order, and no
// device made.
func TestMFPlaysAudio(t *testing.T) {
	for _, name := range []string{"aac", "mp3", "wav"} {
		t.Run(name, func(t *testing.T) {
			r := newAudioRig(t, audioFixture(name))
			md := r.wait(media.LoadedMetadata, 30*time.Second)
			if d := md.State.Duration; d < 2950*time.Millisecond || d > 3100*time.Millisecond {
				t.Errorf("duration %v, want 3 s", d)
			}
			r.wait(media.LoadedData, 10*time.Second)
			r.p.Play()
			r.wait(media.Playing, 10*time.Second)
			t1 := time.Now()
			time.Sleep(time.Second)
			pos, wall := r.p.State().Pos, time.Since(t1)
			r.wait(media.Ended, 10*time.Second)
			took := time.Since(t1)
			bound, audio := r.thread()
			t.Logf("%s: loadedmetadata %v after the player was made; at %v of playing the position %v; ended %v after playing; audio: %s; the engine's events %s",
				name, md.State.Duration, wall.Round(time.Millisecond), pos, took.Round(time.Millisecond), audio, traceOf(r.id))
			if pos < 400*time.Millisecond || pos > wall+300*time.Millisecond {
				t.Errorf("after %v of playing the position is %v", wall, pos)
			}
			if took < 2500*time.Millisecond || took > 6*time.Second {
				t.Errorf("3 s played in %v", took)
			}
			if bound {
				t.Error("an audio file's player made a device")
			}
			if !inOrder(r.kinds(), []media.EventKind{media.LoadedMetadata, media.LoadedData, media.Play, media.Playing, media.Ended}) {
				t.Errorf("events %v", r.kinds())
			}
			if st := r.p.State(); !st.Ended || st.Size != (image.Point{}) {
				t.Errorf("after the end: %+v", st)
			}
		})
	}
}

// A seek before Play lands (the last second plays, in about a second);
// at Slack's 2x the whole file plays in about half its length.
func TestMFAudioSeekAndRate(t *testing.T) {
	r := newAudioRig(t, audioFixture("aac"))
	r.wait(media.LoadedData, 30*time.Second)
	r.p.Seek(2 * time.Second)
	r.wait(media.Seeked, 10*time.Second)
	if pos := r.p.State().Pos; pos < 1900*time.Millisecond || pos > 2100*time.Millisecond {
		t.Errorf("seeked to %v, want 2 s", pos)
	}
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

// A voice clip's Opus in MP4 (Chromium's MediaRecorder, the url_private
// of a clip with no aac rendition yet): what Media Foundation makes of
// it, logged. Go audio plays it where Media Foundation can't (the chain
// in internal/ui), so this doesn't fail on either outcome.
func TestMFOpusClipLogged(t *testing.T) {
	need(t, backend{}.Available())
	for _, name := range []string{"opus-mp4", "opus-frag"} {
		fx := audioFixture(name)
		id := fmt.Sprintf("FMFO%d", rigN.Add(1))
		src, _ := mediatest.Source(id, fx.Data, false)
		src.Mime, src.Ext = fx.Mime, fx.Ext
		hookMu.Lock()
		born[id] = time.Now()
		hookMu.Unlock()
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
		t.Logf("%s on Media Foundation: %v; the engine's events %s", name, got, traceOf(id))
	}
}
