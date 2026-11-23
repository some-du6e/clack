package jsrt

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeMedia is a MediaHost recording what the page opened. Its players
// play as soon as they're told to, two seconds long, and end when the
// test ends them.
type fakeMedia struct {
	mu      sync.Mutex
	opened  []fakeOpen
	players []*fakePlayer
}

type fakeOpen struct {
	Src  string
	Data []byte
}

func (h *fakeMedia) Open(src string, data []byte) (MediaPlayer, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.opened = append(h.opened, fakeOpen{src, append([]byte(nil), data...)})
	if src == "https://example.com/broken.mp3" {
		return nil, errors.New("unsupported")
	}
	p := &fakePlayer{ch: make(chan MediaEvent, 32), done: make(chan struct{})}
	h.players = append(h.players, p)
	return p, nil
}

func (h *fakeMedia) last() *fakePlayer {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.players) == 0 {
		return nil
	}
	return h.players[len(h.players)-1]
}

type fakePlayer struct {
	mu      sync.Mutex
	ch      chan MediaEvent
	done    chan struct{}
	closed  bool
	plays   int
	volume  float64
	muted   bool
	loop    bool
	seekPos time.Duration
}

func (p *fakePlayer) send(ev MediaEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.ch <- ev
	}
}

func (p *fakePlayer) Play() {
	p.mu.Lock()
	p.plays++
	p.mu.Unlock()
	p.send(MediaEvent{Type: "loadedmetadata", Duration: 2 * time.Second, Paused: true, Ready: 1})
	p.send(MediaEvent{Type: "playing", Duration: 2 * time.Second, Ready: 4})
}
func (p *fakePlayer) Pause() {
	p.send(MediaEvent{Type: "pause", Duration: 2 * time.Second, Paused: true, Ready: 4})
}
func (p *fakePlayer) Seek(t time.Duration) {
	p.mu.Lock()
	p.seekPos = t
	p.mu.Unlock()
}
func (p *fakePlayer) SetVolume(v float64) { p.mu.Lock(); p.volume = v; p.mu.Unlock() }
func (p *fakePlayer) SetMuted(m bool)     { p.mu.Lock(); p.muted = m; p.mu.Unlock() }
func (p *fakePlayer) SetLoop(l bool)      { p.mu.Lock(); p.loop = l; p.mu.Unlock() }
func (p *fakePlayer) Next() (MediaEvent, bool) {
	select {
	case ev := <-p.ch:
		return ev, true
	case <-p.done:
		return MediaEvent{}, false
	}
}
func (p *fakePlayer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.done)
	}
}

// end plays the player to its end: timeupdate, pause, ended, as a media
// element's player does (types: some of them, in that order).
func (p *fakePlayer) end(types ...string) {
	for _, ty := range types {
		p.send(MediaEvent{Type: ty, Pos: 2 * time.Second, Duration: 2 * time.Second, Paused: true, Ended: true, Ready: 4})
	}
}

// TestMediaElementPlays: an <audio> element plays through the host's
// player as Chromium's does. play() opens the element's src (an https URL
// the host opens itself; a blob: URL's bytes read by the page), hands the
// player the element's volume, muted and loop, and resolves once the player
// plays; play then playing fire, paused is false, duration is the
// player's. At the end: paused and ended both true as the position gets
// there (Chromium's TimeChanged sets paused as playback ends, before its
// events fire), then pause, then ended. A
// source the host can't play rejects with NotSupportedError and fires
// error; pause() before playing rejects the pending play() with
// AbortError; a new src closes the player. Without a host play() rejects
// with NotSupportedError, as before.
func TestMediaElementPlays(t *testing.T) {
	host := &fakeMedia{}
	r := newTest(t, func(o *Options) { withMedia(o, host) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	call := func(fn string, v any) {
		t.Helper()
		got, err := r.Call(ctx, fn)
		if err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		if err := json.Unmarshal(got, v); err != nil {
			t.Fatalf("%s: %s: %v", fn, got, err)
		}
	}
	evalString(t, r, `
const slackSound = 'https://a.slack-edge.com/bv1-13-br/knock_brush-ac2b6e8.mp3';
const watch = (a) => { const seen = []; for (const t of ['play', 'playing', 'pause', 'ended', 'error']) a.addEventListener(t, () => seen.push(t)); return seen; };
globalThis.playSlack = async () => {
  const a = globalThis.a = new Audio(slackSound);
  globalThis.seen = watch(a);
  a.volume = 0.5; a.muted = true; a.loop = false;
  const before = a.paused;
  await a.play();
  await new Promise((r) => setTimeout(r, 0));
  return { before, paused: a.paused, duration: a.duration, currentSrc: a.currentSrc, seen, media: a instanceof HTMLMediaElement };
};
globalThis.atEnd = () => ({ paused: a.paused, ended: a.ended });
globalThis.waitEnded = () => new Promise((resolve) => {
  const done = () => resolve({ paused: a.paused, ended: a.ended, currentTime: a.currentTime, seen });
  if (seen.includes('ended')) done(); else a.addEventListener('ended', () => setTimeout(done, 0), { once: true });
});
globalThis.playBlob = async () => {
  const b = new Audio(URL.createObjectURL(new Blob([new Uint8Array([7, 8, 9])], { type: 'audio/mpeg' })));
  await b.play();
  return b.currentSrc.startsWith('blob:');
};
globalThis.playBroken = async () => {
  const c = new Audio('https://example.com/broken.mp3');
  const seen = watch(c);
  try { await c.play(); return { settled: 'resolved' }; } catch (err) {
    await new Promise((r) => setTimeout(r, 0));
    return { settled: 'rejected', name: err.name, seen, code: c.error && c.error.code };
  }
};
globalThis.pauseFirst = async () => {
  const d = new Audio(slackSound + '?pause');
  const p = d.play();
  d.pause();
  try { await p; return 'resolved'; } catch (err) { return err.name; }
};
globalThis.newSrc = async () => { a.src = slackSound + '?again'; return { paused: a.paused, currentSrc: a.currentSrc }; };
0`)

	var played struct {
		Before, Paused bool
		Duration       float64
		CurrentSrc     string
		Seen           []string
		Media          bool
	}
	call("playSlack", &played)
	if !played.Before || played.Paused || played.Duration != 2 || !played.Media {
		t.Errorf("playing: %+v, want paused before, playing after, duration 2", played)
	}
	if want := "https://a.slack-edge.com/bv1-13-br/knock_brush-ac2b6e8.mp3"; played.CurrentSrc != want {
		t.Errorf("currentSrc %q, want %q", played.CurrentSrc, want)
	}
	if len(played.Seen) < 2 || played.Seen[0] != "play" || played.Seen[1] != "playing" {
		t.Errorf("events %v, want play then playing", played.Seen)
	}
	host.mu.Lock()
	first := host.opened[0]
	host.mu.Unlock()
	if first.Src != played.CurrentSrc || first.Data != nil {
		t.Errorf("opened %q with %d bytes, want the https URL for the host to open itself", first.Src, len(first.Data))
	}
	p := host.last()
	p.mu.Lock()
	vol, muted, plays := p.volume, p.muted, p.plays
	p.mu.Unlock()
	if vol != 0.5 || !muted || plays != 1 {
		t.Errorf("the player got volume %v, muted %v, %d plays", vol, muted, plays)
	}

	// the position at the end (the player's timeupdate): ended, and paused
	// with it; no script sees one without the other
	p.end("timeupdate")
	var atEnd struct{ Paused, Ended bool }
	for dl := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		call("atEnd", &atEnd)
		if atEnd.Ended || time.Now().After(dl) {
			break
		}
	}
	if !atEnd.Ended || !atEnd.Paused {
		t.Errorf("at the end's timeupdate: %+v, want ended and paused", atEnd)
	}
	p.end("pause", "ended")
	var ended struct {
		Paused, Ended bool
		CurrentTime   float64
		Seen          []string
	}
	call("waitEnded", &ended)
	if !ended.Paused || !ended.Ended || ended.CurrentTime != 2 {
		t.Errorf("at the end: %+v, want paused and ended at 2 s", ended)
	}
	if n := len(ended.Seen); n < 2 || ended.Seen[n-2] != "pause" || ended.Seen[n-1] != "ended" {
		t.Errorf("events %v, want pause then ended last", ended.Seen)
	}

	var blob bool
	call("playBlob", &blob)
	host.mu.Lock()
	got := host.opened[len(host.opened)-1]
	host.mu.Unlock()
	if !blob || string(got.Data) != "\x07\x08\x09" {
		t.Errorf("a blob: source opened %q with %v, want its bytes read by the page", got.Src, got.Data)
	}

	var broken struct {
		Settled, Name string
		Seen          []string
		Code          int
	}
	call("playBroken", &broken)
	if broken.Settled != "rejected" || broken.Name != "NotSupportedError" || broken.Code != 4 || len(broken.Seen) == 0 || broken.Seen[len(broken.Seen)-1] != "error" {
		t.Errorf("a source the host can't play: %+v", broken)
	}

	var paused string
	call("pauseFirst", &paused)
	if paused != "AbortError" {
		t.Errorf("pause() before playing: play() %s, want AbortError", paused)
	}

	var again struct {
		Paused     bool
		CurrentSrc string
	}
	call("newSrc", &again)
	if !again.Paused || again.CurrentSrc != "" {
		t.Errorf("a new src: %+v, want paused and nothing loaded", again)
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Error("a new src left the old player open")
	}

	bare := newTest(t)
	evalString(t, bare, `globalThis.bare = async () => { try { await new Audio('https://a.slack-edge.com/x.mp3').play(); return 'resolved'; } catch (err) { return err.name; } }; 0`)
	got2, err := bare.Call(ctx, "bare")
	if err != nil || string(got2) != `"NotSupportedError"` {
		t.Errorf("without a media host: %s %v", got2, err)
	}
}
