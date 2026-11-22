package media

// The chain (review of c38b91b8): every video went through a chainPlayer,
// which isn't a FrameCopier, so the 15 s idle release never armed on
// Windows and macOS, and whose Close waited for the backend's events to
// end, which Media Foundation's do only after its engine's shutdown: the
// controller called it holding its lock, on the UI goroutine. A source
// one backend takes is that backend's own player; a chain's Close returns
// at once.

import (
	"context"
	"errors"
	"image"
	"io"
	"strings"
	"testing"
	"time"

	"gioui.org/op/paint"
)

// takes is a backend that takes the sources ok says, playing them with
// play.
type takes struct {
	Backend
	ok func(Source) bool
}

func (b takes) Takes(src Source) bool { return b.ok(src) }

// slowClose is a backend whose players end their events late after
// Close (as Media Foundation's after its engine's shutdown).
type slowClose struct{ after time.Duration }

func (slowClose) Name() string     { return "slow close" }
func (slowClose) Available() error { return nil }
func (b slowClose) New(src Source, frameReady func()) (Player, error) {
	p := &slowPlayer{after: b.after}
	p.Init()
	return p, nil
}

type slowPlayer struct {
	Base
	after time.Duration
}

func (p *slowPlayer) Play()                   {}
func (p *slowPlayer) Pause()                  {}
func (p *slowPlayer) Seek(time.Duration)      {}
func (p *slowPlayer) SetVolume(float64)       {}
func (p *slowPlayer) SetMuted(bool)           {}
func (p *slowPlayer) SetLoop(bool)            {}
func (p *slowPlayer) SetDrawSize(image.Point) {}
func (p *slowPlayer) Op(still paint.ImageOp, _ image.Point) paint.ImageOp {
	return still
}
func (p *slowPlayer) Stats() Stats { return Stats{} }
func (p *slowPlayer) Close() {
	go func() {
		time.Sleep(p.after)
		p.CloseEvents()
	}()
}

// A video, which only the OS backend takes when the other plays audio
// alone, is the backend's own player: the fake's, a FrameCopier (the idle
// release reads its frame).
func TestChainOneBackendIsItsOwnPlayer(t *testing.T) {
	fake := NewFake(FakeOptions{Log: func(string, ...any) {}})
	audio := takes{Backend: fake, ok: func(s Source) bool { return strings.HasPrefix(s.Mime, "audio/") }}
	c := NewChain(nil, fake, audio)
	p, err := c.New(Source{FileID: "F1", Mime: "video/mp4", Ext: "mp4", Open: func(ctx context.Context, off int64) (io.ReadCloser, Info, error) {
		return nil, Info{Total: -1}, errors.New("no file (the test's)")
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, ok := p.(FrameCopier); !ok {
		t.Errorf("a video one backend takes plays through %T, which isn't a FrameCopier: the idle release never arms", p)
	}
}

// A chain's Close returns at once, though the current player's events
// end a second later.
func TestChainCloseDoesNotWait(t *testing.T) {
	c := NewChain(nil, slowClose{after: time.Second}, slowClose{after: time.Second})
	p, err := c.New(Source{FileID: "F1", Mime: "audio/mp4", Ext: "m4a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	p.Close()
	if took := time.Since(t0); took > 200*time.Millisecond {
		t.Errorf("Close took %v: it waited for the backend's events to end (the controller holds its lock)", took)
	}
	select {
	case _, ok := <-p.Events():
		for ok {
			_, ok = <-p.Events()
		}
	case <-time.After(5 * time.Second):
		t.Error("the chain's events never closed")
	}
}
