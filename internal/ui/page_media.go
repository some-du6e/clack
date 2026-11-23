package ui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/ui/huddleui"
)

// pageMedia is the plugin runtime's media host (taut.Options.Media): the
// page's <audio> elements play on the players Clack's own media play on
// (playersBackend: Go audio, the OS's media stack), as sound only (a page
// element is drawn nowhere). Slack's sound playback is one (Taut.PlaySound:
// Clack's notification sounds), and whatever a plugin plays. Slack's sounds
// come from Clack's copies of its CDN files (huddleui.SoundBytes); any other
// source is the bytes the page read itself (a blob: URL, a plugin's
// protocol: CustomSounds' slick-custom-sounds://). Other http(s) URLs aren't
// fetched: nothing in the Library plays one.
type pageMedia struct{}

// pageMediaMax bounds a page element's source: a sound is a few KB, a
// recording of a name a few hundred.
const pageMediaMax = 32 << 20

var (
	pageMediaBackend = sync.OnceValue(playersBackend)
	// pageMediaHook, when set (tests), opens a page element's source
	// instead of the players.
	pageMediaHook func(src string, data []byte) (jsrt.MediaPlayer, error)
)

func (pageMedia) Open(src string, data []byte) (jsrt.MediaPlayer, error) {
	if h := pageMediaHook; h != nil {
		return h(src, data)
	}
	if data == nil && huddleui.Hooked(src) { // tests: PlayHook sees Slack's sounds, as on Clack's own path
		return newHookedPlayer(), nil
	}
	if data == nil {
		b, ok := huddleui.SoundBytes(src)
		if !ok {
			return nil, errors.New("page media: not a source Clack opens itself")
		}
		data = b
	}
	if len(data) == 0 || len(data) > pageMediaMax {
		return nil, errors.New("page media: an empty or too big source")
	}
	ext := pageMediaExt(src, data)
	s := media.Source{FileID: "page", Mime: "audio/" + ext, Ext: ext,
		Open: func(ctx context.Context, off int64) (io.ReadCloser, media.Info, error) {
			if off < 0 || off > int64(len(data)) {
				off = 0
			}
			return io.NopCloser(bytes.NewReader(data[off:])), media.Info{Status: http.StatusPartialContent, Total: int64(len(data)), Seekable: true}, nil
		}}
	if ext == "m4a" {
		s.Mime = "audio/mp4"
	}
	p, err := pageMediaBackend().New(s, func() {})
	if err != nil {
		return nil, err
	}
	return &pagePlayer{p: p}, nil
}

// pageMediaExt is what a page source is: its bytes' signature, else its
// URL's extension (a plugin's sound.mp3?p=…), else MP3.
func pageMediaExt(src string, data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("ID3")) || len(data) > 1 && data[0] == 0xff && data[1]&0xe0 == 0xe0:
		return "mp3"
	case bytes.HasPrefix(data, []byte("OggS")):
		return "ogg"
	case len(data) > 12 && bytes.HasPrefix(data, []byte("RIFF")) && string(data[8:12]) == "WAVE":
		return "wav"
	case bytes.HasPrefix(data, []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return "webm"
	case len(data) > 8 && string(data[4:8]) == "ftyp":
		return "m4a"
	}
	if u, err := url.Parse(src); err == nil {
		if e := strings.TrimPrefix(strings.ToLower(path.Ext(u.Path)), "."); e != "" {
			return e
		}
	}
	return "mp3"
}

// pagePlayer is a media.Player as a page element drives it (jsrt).
type pagePlayer struct{ p media.Player }

func (pp *pagePlayer) Play()                { pp.p.Play() }
func (pp *pagePlayer) Pause()               { pp.p.Pause() }
func (pp *pagePlayer) Seek(t time.Duration) { pp.p.Seek(t) }
func (pp *pagePlayer) SetVolume(v float64)  { pp.p.SetVolume(v) }
func (pp *pagePlayer) SetMuted(m bool)      { pp.p.SetMuted(m) }
func (pp *pagePlayer) SetLoop(l bool)       { pp.p.SetLoop(l) }
func (pp *pagePlayer) Close()               { pp.p.Close() }

// Next is the player's next HTMLMediaElement event (the first frame, a
// picture's, isn't one).
func (pp *pagePlayer) Next() (jsrt.MediaEvent, bool) {
	for ev := range pp.p.Events() {
		if ev.Kind == media.FirstFrame {
			continue
		}
		st := ev.State
		return jsrt.MediaEvent{Type: ev.Kind.String(), Pos: st.Pos, Duration: st.Duration, Paused: st.Paused, Ended: st.Ended, Ready: st.Ready, Err: st.Err}, true
	}
	return jsrt.MediaEvent{}, false
}

// hookedPlayer is a sound huddleui.PlayHook took (tests): it plays at
// once and ends, silent.
type hookedPlayer struct {
	mu     sync.Mutex
	ch     chan jsrt.MediaEvent
	closed bool
}

func newHookedPlayer() *hookedPlayer { return &hookedPlayer{ch: make(chan jsrt.MediaEvent, 8)} }

func (p *hookedPlayer) Play() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.ch <- jsrt.MediaEvent{Type: "playing", Ready: 4}
	for _, ty := range []string{"pause", "ended"} {
		p.ch <- jsrt.MediaEvent{Type: ty, Paused: true, Ended: true, Ready: 4}
	}
}
func (p *hookedPlayer) Pause()               {}
func (p *hookedPlayer) Seek(t time.Duration) {}
func (p *hookedPlayer) SetVolume(v float64)  {}
func (p *hookedPlayer) SetMuted(m bool)      {}
func (p *hookedPlayer) SetLoop(l bool)       {}
func (p *hookedPlayer) Next() (jsrt.MediaEvent, bool) {
	ev, ok := <-p.ch
	return ev, ok
}
func (p *hookedPlayer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.ch)
	}
}
