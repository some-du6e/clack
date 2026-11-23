package ui

import (
	"bytes"
	"context"
	"image"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
	"github.com/some-du6e/clack/internal/ui/huddleui"
)

// soundBackend is the page's players in a test: it records each source a
// page element opens (the bytes read through its Open) and plays at once:
// playing, then pause and ended.
type soundBackend struct {
	mu      sync.Mutex
	opened  []media.Source
	read    [][]byte
	players []*soundPlayer
}

func (b *soundBackend) Name() string     { return "test-sounds" }
func (b *soundBackend) Available() error { return nil }
func (b *soundBackend) New(src media.Source, _ func()) (media.Player, error) {
	rc, _, err := src.Open(context.Background(), 0)
	if err != nil {
		return nil, err
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	p := &soundPlayer{ev: make(chan media.Event, 64)}
	b.mu.Lock()
	b.opened, b.read, b.players = append(b.opened, src), append(b.read, data), append(b.players, p)
	b.mu.Unlock()
	return p, nil
}

func (b *soundBackend) plays() (opens, plays int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.players {
		p.mu.Lock()
		plays += p.plays
		p.mu.Unlock()
	}
	return len(b.players), plays
}

type soundPlayer struct {
	mu     sync.Mutex
	plays  int
	closed bool
	ev     chan media.Event
}

func (p *soundPlayer) Play() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.plays++
	st := media.State{Duration: time.Second, Ready: 4}
	p.ev <- media.Event{Kind: media.Playing, State: st}
	st.Paused, st.Ended = true, true
	p.ev <- media.Event{Kind: media.Pause, State: st}
	p.ev <- media.Event{Kind: media.Ended, State: st}
}
func (p *soundPlayer) Pause()                  {}
func (p *soundPlayer) Seek(time.Duration)      {}
func (p *soundPlayer) SetVolume(float64)       {}
func (p *soundPlayer) SetMuted(bool)           {}
func (p *soundPlayer) SetLoop(bool)            {}
func (p *soundPlayer) SetRate(float64)         {}
func (p *soundPlayer) SetDrawSize(image.Point) {}
func (p *soundPlayer) Op(still paint.ImageOp, _ image.Point) paint.ImageOp {
	return still
}
func (p *soundPlayer) State() media.State         { return media.State{} }
func (p *soundPlayer) Events() <-chan media.Event { return p.ev }
func (p *soundPlayer) Stats() media.Stats         { return media.Stats{} }
func (p *soundPlayer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.ev)
	}
}

// TestNotifySoundOnThePage: with the plugins' runtime up, a message
// notification's sound plays through Slack's sound playback on the page
// (notify.go's page branch: Taut.PlaySound, the page's <audio> element):
// pageMedia.Open takes Slack's sound from Clack's copy of its CDN file
// (SoundBytes), knows it by its bytes (an MP3), and the element plays it
// on the players, its events coming back through pagePlayer.Next. The
// same sound again plays the same element again (no second open). A
// source past the 32 MB cap isn't opened, and a page source is known by
// its bytes before its URL. In tests the page's sounds reach PlayHook,
// every one of them: a burst of six plays all arrive, and the page isn't
// held up by them.
func TestNotifySoundOnThePage(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("CLACK_HOME", t.TempDir())
	knock := huddleui.SoundURL("knock_brush.mp3")
	if err := os.MkdirAll(filepath.Join(cache, "clack", "sounds"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "clack", "sounds", path.Base(knock)), mediatest.TonesMP3, 0o644); err != nil {
		t.Fatal(err)
	}
	sounds := &soundBackend{}
	old := pageMediaBackend
	pageMediaBackend = func() media.Backend { return sounds }
	t.Cleanup(func() { pageMediaBackend = old })

	a, ca, _ := fakePrefsApp(t)
	a.notify = &recNotifier{}
	ca.SetDNDStatus(core.DNDStatus{})
	a.data.startTaut(a)
	t.Cleanup(func() { a.data.stopTaut() })
	tt := a.Taut()
	if tt == nil {
		t.Fatal("no plugin runtime")
	}
	if _, err := tt.JS(); err != nil {
		t.Fatal(err)
	}

	a.notifyMessage("t", "b", nil)
	waitFor(t, a, "the notification's sound on the page", func() bool { _, plays := sounds.plays(); return plays == 1 })
	sounds.mu.Lock()
	src, read := sounds.opened[0], sounds.read[0]
	sounds.mu.Unlock()
	if src.Ext != "mp3" || src.Mime != "audio/mp3" || !bytes.Equal(read, mediatest.TonesMP3) {
		t.Errorf("the page opened %s (%s) with %d bytes; want Clack's copy of Slack's MP3, known by its bytes", src.Ext, src.Mime, len(read))
	}
	if !tt.PlaySound(knock) {
		t.Fatal("PlaySound reported no page")
	}
	waitFor(t, a, "the same sound again", func() bool { _, plays := sounds.plays(); return plays == 2 })
	if opens, _ := sounds.plays(); opens != 1 {
		t.Errorf("%d opens for two plays of one sound, want the element's one", opens)
	}

	if _, err := (pageMedia{}).Open("blob:big", make([]byte, pageMediaMax+1)); err == nil {
		t.Error("a source past the 32 MB cap opened")
	}
	for _, c := range []struct {
		src  string
		data []byte
		want string
	}{
		{"blob:x", mediatest.TonesWAV, "wav"}, {"blob:x.mp3", mediatest.TonesOgg, "ogg"},
		{"blob:x", mediatest.TonesWebM, "webm"}, {"blob:x", mediatest.TonesOpusMP4, "m4a"},
		{"slick-custom-sounds://current/sound.flac?p=a", []byte("fLaC...."), "flac"},
	} {
		if got := pageMediaExt(c.src, c.data); got != c.want {
			t.Errorf("pageMediaExt(%q, %d bytes) = %q, want %q", c.src, len(c.data), got, c.want)
		}
	}

	var mu sync.Mutex
	var hooked []string
	huddleui.PlayHook = func(f string) { mu.Lock(); hooked = append(hooked, f); mu.Unlock() }
	t.Cleanup(func() { huddleui.PlayHook = nil })
	hummus := huddleui.SoundURL("hummus.mp3")
	for i := 0; i < 6; i++ {
		tt.PlaySound(hummus)
	}
	waitFor(t, a, "every hooked sound", func() bool { mu.Lock(); defer mu.Unlock(); return len(hooked) == 6 })
	mu.Lock()
	defer mu.Unlock()
	for _, f := range hooked {
		if f != "hummus.mp3" {
			t.Errorf("PlayHook got %q, want hummus.mp3", f)
		}
	}
}
