package videoplayer

// The review of c38b91b8: ui plays every file through media.Chain (the
// OS backend, then Go audio), and a chain's player wasn't a FrameCopier,
// so a paused video's idle release never armed on Windows and macOS
// (TestIdlePlayerLetsGo plays on the fake alone and didn't see it).
// Through the same chain a paused video still lets go of its player, its
// frame kept, and plays on from there.

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/goaudio"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

// chainEnv is fakeEnv on ui's chain: the fake as the OS backend, then Go
// audio (with no sound card).
func chainEnv(t *testing.T) *Controller {
	t.Helper()
	_, o := mediatest.Source("FVC", mediatest.GreyGreyRed, false)
	p := &prefs{vals: map[string]json.RawMessage{}}
	noCard := func(func([]int16)) (io.Closer, error) { return nil, errors.New("no sound card (the test's)") }
	chain := media.NewChain(nil, media.NewFake(media.FakeOptions{Log: func(string, ...any) {}}), &goaudio.Backend{Output: noCard, Logf: t.Logf})
	c := New(Env{
		Manager: media.NewManager(chain),
		Open: func(ctx context.Context, _ string, off int64) (io.ReadCloser, media.Info, error) {
			return o.Open(ctx, off)
		},
		Pref:       p.get,
		SetPref:    p.set,
		Experiment: func(string) string { return "on" },
	})
	t.Cleanup(c.CloseAll)
	return c
}

func TestIdlePlayerLetsGoThroughTheChain(t *testing.T) {
	setIdleAfter(t, 300*time.Millisecond)
	c := chainEnv(t)
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	g := &tileRig{c: c, r: r, f: f, size: image.Pt(480, 270), at: time.Second}
	g.frame()
	click(c, r, f)
	waitForTile(t, c, r, f, "playing", 10*time.Second, func() bool { g.frame(); st := state(t, c, r, f); return st.Playing && !st.Loading })
	for t0 := time.Now(); time.Since(t0) < 300*time.Millisecond; time.Sleep(10 * time.Millisecond) {
		g.frame()
	}
	click(c, r, f) // pause
	waitForTile(t, c, r, f, "paused", 5*time.Second, func() bool { g.frame(); return !state(t, c, r, f).Playing })
	left := state(t, c, r, f).Elapsed
	waitFor(t, "the idle player let go", 3*time.Second, func() bool { g.frame(); return c.Mounted() == 0 })
	if !tileStill(state(t, c, r, f)) {
		t.Error("the tile let its player go and shows no frame of it (the poster again)")
	}
	click(c, r, f)
	waitForTile(t, c, r, f, "playing again", 10*time.Second, func() bool { g.frame(); st := state(t, c, r, f); return st.Playing && !st.Loading })
	if pos := state(t, c, r, f).Player.State().Pos; pos < left-100*time.Millisecond || pos > left+900*time.Millisecond {
		t.Errorf("the new player starts at %v, paused at %v", pos, left)
	}
}
