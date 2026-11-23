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
	"github.com/some-du6e/clack/internal/media/goaudio"
	"github.com/some-du6e/clack/internal/ui/huddleui"
)

// pageMedia is the plugin runtime's media host (taut.Options.Media): the
// page's <audio> elements play on the players Clack's own media play on
// (pageSoundsBackend: Go audio first, out of the picked speakers, then the
// OS's media stack), as sound only (a page
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
	pageMediaBackend = sync.OnceValue(pageSoundsBackend)
	// pageMediaHook, when set (tests), opens a page element's source
	// instead of the players.
	pageMediaHook func(src string, data []byte) (jsrt.MediaPlayer, error)
)

func (pageMedia) Open(src string, data []byte) (jsrt.MediaPlayer, error) {
	if h := pageMediaHook; h != nil {
		return h(src, data)
	}
	if pref, ok := huddleui.Hooked(src); ok && data == nil { // tests: PlayHook sees each of Slack's sounds, as on Clack's own path
		return newHookedPlayer(pref), nil
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

// pageSoundsBackend is the players' chain with Go audio first for every
// page sound: it plays out of the speakers picked in Audio & video
// (huddle.OpenOutput), as Clack's own sounds do (huddle.PlaySound), where
// the OS's player (AVPlayer) would play to the system's default; the OS's
// player still takes a file Go audio can't decode (AAC on macOS).
func pageSoundsBackend() media.Backend {
	osb, _ := media.Default() // nil on Linux
	return media.NewChain(func(src media.Source, bs []media.Backend) []media.Backend {
		out := make([]media.Backend, 0, len(bs))
		for _, b := range bs {
			if _, ok := b.(*goaudio.Backend); ok {
				out = append(out, b)
			}
		}
		for _, b := range bs {
			if _, ok := b.(*goaudio.Backend); !ok {
				out = append(out, b)
			}
		}
		return out
	}, osb, &goaudio.Backend{})
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

// hookedPlayer is a page sound huddleui.PlayHook takes (tests): each Play
// hands the hook its pref value, as each of Clack's own sounds does, and
// plays at once and ends, silent. Its events queue without bound, so a
// burst of plays on the JS thread never waits for the element to read
// them.
type hookedPlayer struct {
	pref   string
	mu     sync.Mutex
	evs    []jsrt.MediaEvent
	kick   chan struct{}
	closed bool
}

func newHookedPlayer(pref string) *hookedPlayer {
	return &hookedPlayer{pref: pref, kick: make(chan struct{}, 1)}
}

func (p *hookedPlayer) Play() {
	if h := huddleui.PlayHook; h != nil {
		h(p.pref)
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.evs = append(p.evs, jsrt.MediaEvent{Type: "playing", Ready: 4},
		jsrt.MediaEvent{Type: "pause", Paused: true, Ended: true, Ready: 4},
		jsrt.MediaEvent{Type: "ended", Paused: true, Ended: true, Ready: 4})
	p.mu.Unlock()
	p.wake()
}
func (p *hookedPlayer) wake() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}
func (p *hookedPlayer) Pause()               {}
func (p *hookedPlayer) Seek(t time.Duration) {}
func (p *hookedPlayer) SetVolume(v float64)  {}
func (p *hookedPlayer) SetMuted(m bool)      {}
func (p *hookedPlayer) SetLoop(l bool)       {}
func (p *hookedPlayer) Next() (jsrt.MediaEvent, bool) {
	for {
		p.mu.Lock()
		if len(p.evs) > 0 {
			ev := p.evs[0]
			p.evs = p.evs[1:]
			p.mu.Unlock()
			return ev, true
		}
		if p.closed {
			p.mu.Unlock()
			return jsrt.MediaEvent{}, false
		}
		p.mu.Unlock()
		<-p.kick
	}
}
func (p *hookedPlayer) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.wake()
}
