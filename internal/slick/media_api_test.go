package slick

import (
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/taut"
)

// The page's media host in the harness (taut.Options.Media): it records
// what the page's <audio> elements open, and its players play at once. A
// red run proving the tests fail on code without page media swaps this
// file for a stand-in.

type harnessMedia struct {
	mu     sync.Mutex
	opened []mediaOpened
}

type mediaOpened struct {
	src  string
	data []byte
}

func (m *harnessMedia) Open(src string, data []byte) (jsrt.MediaPlayer, error) {
	m.mu.Lock()
	m.opened = append(m.opened, mediaOpened{src, append([]byte(nil), data...)})
	m.mu.Unlock()
	return &harnessPlayer{ch: make(chan jsrt.MediaEvent, 16), done: make(chan struct{})}, nil
}

// opens is what the page's elements opened so far, oldest first.
func (m *harnessMedia) opens() []mediaOpened {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mediaOpened(nil), m.opened...)
}

type harnessPlayer struct {
	mu     sync.Mutex
	ch     chan jsrt.MediaEvent
	done   chan struct{}
	closed bool
}

func (p *harnessPlayer) Play() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.ch <- jsrt.MediaEvent{Type: "playing", Duration: time.Second, Ready: 4}
	}
}
func (p *harnessPlayer) Pause()               {}
func (p *harnessPlayer) Seek(t time.Duration) {}
func (p *harnessPlayer) SetVolume(v float64)  {}
func (p *harnessPlayer) SetMuted(m bool)      {}
func (p *harnessPlayer) SetLoop(l bool)       {}
func (p *harnessPlayer) Next() (jsrt.MediaEvent, bool) {
	select {
	case ev := <-p.ch:
		return ev, true
	case <-p.done:
		return jsrt.MediaEvent{}, false
	}
}
func (p *harnessPlayer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.done)
	}
}

// withPageMedia gives the harness's runtime its media host.
func withPageMedia(o *taut.Options, m *harnessMedia) { o.Media = m }

// playSound plays url through Slack's sound playback on the page
// (Taut.PlaySound), as Clack's notification sounds do.
func playSound(h *harness, url string) bool { return h.tt.PlaySound(url) }
