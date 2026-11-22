package videoplayer

// Round 3's review, MAJOR 1: the pill's spinner while a video loads. On
// the round-3 head (0cbe7e63) the overlay ran op.InvalidateCmd mid-layout
// every 16 ms while a tile was loading, for good once it paused or failed
// before playing, and a second click while loading paused the video where
// Slack plays it. These run there through the same calls.

import (
	"context"
	"encoding/json"
	"image"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// slowEnv is fakeEnv over a slow network (every read of a body waits d,
// so the metadata takes a while), counting the frames it asks for.
func slowEnv(t *testing.T, d time.Duration, opt media.FakeOptions) (*Controller, *atomic.Int64) {
	t.Helper()
	_, o := mediatest.Source("FVS", mediatest.GreyGreyRed, false)
	o.Delay = d
	p := &prefs{vals: map[string]json.RawMessage{}}
	frames := new(atomic.Int64)
	if opt.Log == nil {
		opt.Log = func(string, ...any) {}
	}
	c := New(Env{
		Manager: media.NewManager(media.NewFake(opt)),
		Open: func(ctx context.Context, _ string, off int64) (io.ReadCloser, media.Info, error) {
			return o.Open(ctx, off)
		},
		FrameReady: func() { frames.Add(1) },
		Repaint:    func() {},
		Pref:       p.get,
		SetPref:    p.set,
		Experiment: func(string) string { return "off" },
	})
	t.Cleanup(c.CloseAll)
	return c, frames
}

// layoutWakes lays the tile out as a list does (a frame of the controller)
// through a router, and reports whether its layout asked the window for a
// frame (an op.InvalidateCmd).
func layoutWakes(c *Controller, r *Row, f *richtext.File) bool {
	var ops op.Ops
	var router input.Router
	gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(480, 270)), Source: router.Source()}
	c.BeginFrame()
	r.LayoutMedia(gtx, richview.MediaSlot{File: f, Poster: image.Rect(0, 0, 480, 270)})
	c.EndFrame()
	_, wake := router.WakeupTime()
	router.Frame(&ops)
	return wake
}

// A tile paused before it plays (another video starting pauses it, bxCZ)
// stops loading: its layout asks for no frame and nothing asks for any.
func TestLoadingPauseAsksNoFrames(t *testing.T) {
	c, frames := slowEnv(t, 300*time.Millisecond, media.FakeOptions{})
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	layoutWakes(c, r, f)
	click(c, r, f)
	st := state(t, c, r, f)
	if !st.Loading || st.Player == nil {
		t.Fatalf("after the click: %+v, want a player loading", st)
	}
	st.Player.Pause()
	waitForTile(t, c, r, f, "paused", 5*time.Second, func() bool { return !state(t, c, r, f).Playing && state(t, c, r, f).Player.State().Paused })
	time.Sleep(50 * time.Millisecond)
	if st := state(t, c, r, f); st.Loading {
		t.Error("paused before it played, it still loads: the spinner goes on")
	}
	if layoutWakes(c, r, f) {
		t.Error("the paused tile's layout asks the window for a frame")
	}
	pl := state(t, c, r, f).Player
	n, pf := frames.Load(), pl.Stats().Frames
	time.Sleep(300 * time.Millisecond)
	// the player's own (its first frame, once it loads, shows paused) aside
	if d := frames.Load() - n - (pl.Stats().Frames - pf); d != 0 {
		t.Errorf("%d frames asked in 300 ms with nothing playing, past the player's own", d)
	}
}

// A second click while the video loads plays it on (togglePlayPause on a
// <video> still waiting to autoplay plays it, and its play event ends the
// spinner), where it used to pause it.
func TestSecondClickWhileLoadingPlays(t *testing.T) {
	c, _ := slowEnv(t, 400*time.Millisecond, media.FakeOptions{})
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	layoutWakes(c, r, f)
	click(c, r, f)
	time.Sleep(30 * time.Millisecond)
	if st := state(t, c, r, f); !st.Loading {
		t.Skip("the fixture loaded within 30 ms")
	}
	click(c, r, f)
	st := state(t, c, r, f)
	if st.Loading {
		t.Error("still loading after the second click: the spinner should end")
	}
	waitForTile(t, c, r, f, "playing", 10*time.Second, func() bool {
		st := state(t, c, r, f)
		return st.Playing && st.Player != nil && !st.Player.State().Paused
	})
}

// While a video loads its spinner turns through quiet frames the
// controller asks for (the live image's frames), not through its layout;
// once it plays they stop.
func TestSpinnerQuietFrames(t *testing.T) {
	c, frames := slowEnv(t, 600*time.Millisecond, media.FakeOptions{})
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	layoutWakes(c, r, f)
	click(c, r, f)
	if layoutWakes(c, r, f) {
		t.Error("the loading tile's layout asks the window for a frame (an op.InvalidateCmd mid-layout)")
	}
	// the spinner's frames: the frames asked past the player's own (the
	// fake publishes the video's for a second after Play, as if on screen)
	pl := state(t, c, r, f).Player
	spun := func() func() int64 {
		n, pf := frames.Load(), pl.Stats().Frames
		return func() int64 { return frames.Load() - n - (pl.Stats().Frames - pf) }
	}
	d := spun()
	time.Sleep(300 * time.Millisecond)
	if !state(t, c, r, f).Loading {
		t.Skip("the fixture loaded within 300 ms")
	}
	if d := d(); d < 10 {
		t.Errorf("%d quiet frames in 300 ms of loading, want the spinner's (about 18)", d)
	}
	waitForTile(t, c, r, f, "playing", 15*time.Second, func() bool { st := state(t, c, r, f); return st.Playing && !st.Loading })
	time.Sleep(100 * time.Millisecond)
	d = spun()
	time.Sleep(300 * time.Millisecond)
	if d := d(); d > 1 {
		t.Errorf("%d frames asked in 300 ms after it played, past the video's: the spinner's go on", d)
	}
}

// A load that fails ends the spinner: no frames, no layout asking for one.
func TestLoadErrorStopsSpinner(t *testing.T) {
	c, frames := slowEnv(t, 10*time.Millisecond, media.FakeOptions{Err: 4})
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	layoutWakes(c, r, f)
	click(c, r, f)
	waitForTile(t, c, r, f, "the error", 10*time.Second, func() bool { return state(t, c, r, f).Err != 0 })
	time.Sleep(50 * time.Millisecond)
	if st := state(t, c, r, f); st.Loading {
		t.Error("the load failed and the tile still loads: the spinner goes on")
	}
	if layoutWakes(c, r, f) {
		t.Error("the failed tile's layout asks the window for a frame")
	}
	n := frames.Load()
	time.Sleep(300 * time.Millisecond)
	if d := frames.Load() - n; d != 0 {
		t.Errorf("%d frames asked in 300 ms after the failure", d)
	}
}

// While a video loads its pill stays, with the spinner and the duration,
// hovered or not: isPlaying waits for the play event (fIJO's tI), so the
// --playing rule that hides the pill doesn't apply yet. A MINOR: on the
// round-3 head the backend's Play made the tile playing at once, so the
// pill hid when the pointer left, and read 0:00 while hovered.
// pillShown is loading_hooks_test.go's.
func TestLoadingPillShowsDuration(t *testing.T) {
	c, _ := slowEnv(t, 400*time.Millisecond, media.FakeOptions{})
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	layoutWakes(c, r, f)
	click(c, r, f)
	waitForTile(t, c, r, f, "the play event", 5*time.Second, func() bool { st := state(t, c, r, f); return st.Playing || !st.Loading })
	st := state(t, c, r, f)
	if !st.Loading {
		t.Skip("the fixture loaded before the play event was seen")
	}
	if !pillShown(st) {
		t.Error("the pill hid while the video loads, the pointer away")
	}
	// the file's 1:23 until the metadata, the fixture's 0:03 after
	if got, want := pillTimeOf(c, r, f), clock(st.Duration); got != want || st.Duration <= 0 {
		t.Errorf("the loading pill reads %q, want the duration, %s", got, want)
	}
}
