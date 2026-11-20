package videoplayer

// Round 4, step 2: the mini player's rules (pip.go; yF7D, YPBt) on the
// fake backend, with clips_pip on as the test account has it. Before it,
// a playing video under 10% visible paused (round 3's stand-in for the
// dock) and nothing docked.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
)

// playThen mounts r's f and waits until it plays a while.
func playThen(t *testing.T, c *Controller, r *Row, f *richtext.File) {
	t.Helper()
	frame(c, [2]any{r, f})
	click(c, r, f)
	waitFor(t, "playing", 5*time.Second, func() bool { st := state(t, c, r, f); return st.Playing && !st.Loading })
	time.Sleep(300 * time.Millisecond)
}

// pipFrame is a full layout drawing tiles and the mini player.
func pipFrame(c *Controller, tiles ...[2]any) {
	if pr, m, ok := c.Pip(); ok {
		tiles = append(tiles, [2]any{pr, m.File})
	}
	frame(c, tiles...)
}

func pipPlaying(c *Controller) bool { _, _, p := c.PipPlayback(); return p }

// A playing video crossing under 10% visible docks (useScrollDockTrigger):
// not on the observer's first report; the inline player pauses and shows
// only its pill; the mini player's own player plays on from its time.
func TestPipDocksOnScroll(t *testing.T) {
	c, _ := fakeEnv(t)
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	playThen(t, c, r, f)
	r.MediaVisible(f, 0.05) // the observer's first callback: skipped
	if _, _, ok := c.Pip(); ok {
		t.Fatal("docked on the observer's first report")
	}
	r.MediaVisible(f, 0.5)
	left := state(t, c, r, f).Elapsed
	r.MediaVisible(f, 0.05)
	_, m, ok := c.Pip()
	if !ok || m.File.ID != f.ID || m.Channel != "C1" || m.TS != "1.0" || m.Gallery {
		t.Fatalf("under 10%% visible while playing: the mini player shows %+v (%v), want the video", m, ok)
	}
	if tr := c.PipTrigger(); tr != "scroll" {
		t.Errorf("trigger %q, want scroll", tr)
	}
	waitFor(t, "the inline player paused", 5*time.Second, func() bool { return !state(t, c, r, f).Playing })
	if st := state(t, c, r, f); !st.InPip {
		t.Errorf("the inline tile while docked: %+v, want it showing it's in the mini player", st)
	}
	waitFor(t, "the mini player playing", 5*time.Second, func() bool { pipFrame(c, [2]any{r, f}); return pipPlaying(c) })
	at, _, _ := c.PipPlayback()
	if at < left-100*time.Millisecond {
		t.Errorf("the mini player is at %v, the inline player left at %v", at, left)
	}
	if st, ok := c.TileState("pip", "C1", "1.0", f.ID); !ok || !st.Mounted {
		t.Errorf("the mini player's tile: %+v (%v), want its own player", st, ok)
	}
}

// The observer goes with the VideoPlayer (useScrollDockTrigger's effect
// cleanup, its target the player's element): laid out again after a frame
// without it (its conversation left and opened again by Jump to message),
// the tile's first report is the new observer's first callback, skipped,
// whatever the old one last saw; a crossing after it docks.
func TestPipObserverPerMount(t *testing.T) {
	c, _ := fakeEnv(t)
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	playThen(t, c, r, f)
	r.MediaVisible(f, 0.5) // seen
	frame(c)               // another conversation: docks
	waitFor(t, "the mini player playing", 5*time.Second, func() bool { pipFrame(c); return pipPlaying(c) })
	c.ClosePip(true) // Jump to message: the conversation opens again
	waitFor(t, "the inline player playing on", 5*time.Second, func() bool { frame(c, [2]any{r, f}); return state(t, c, r, f).Playing })
	r.MediaVisible(f, 0.05) // the list not at the message yet
	if _, _, ok := c.Pip(); ok {
		t.Fatalf("docked (%q) on the first report after it was laid out again", c.PipTrigger())
	}
	r.MediaVisible(f, 0.5)
	r.MediaVisible(f, 0.05)
	if _, _, ok := c.Pip(); !ok || c.PipTrigger() != "scroll" {
		t.Errorf("under 10%% after the new observer saw it: docked %v by %q, want scroll", ok, c.PipTrigger())
	}
}

// clips_pip_auto_dock false: a scrolled-away video plays on inline.
func TestPipAutoDockOff(t *testing.T) {
	c, p := fakeEnv(t)
	p.vals["clips_pip_auto_dock"] = json.RawMessage(`false`)
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	playThen(t, c, r, f)
	r.MediaVisible(f, 0.5)
	r.MediaVisible(f, 0.05)
	time.Sleep(100 * time.Millisecond)
	if _, _, ok := c.Pip(); ok {
		t.Error("docked with clips_pip_auto_dock off")
	}
	if !state(t, c, r, f).Playing {
		t.Error("paused under 10% visible: it plays on")
	}
}

// A playing video whose message goes docks (useNavigateDockTrigger), and
// the mini player plays on while the list doesn't draw it.
func TestPipDocksOnNavigate(t *testing.T) {
	c, _ := fakeEnv(t)
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	playThen(t, c, r, f)
	frame(c) // another conversation
	if _, _, ok := c.Pip(); !ok || c.PipTrigger() != "navigate" {
		t.Fatalf("the message went while it played: no mini player (trigger %q)", c.PipTrigger())
	}
	waitFor(t, "the mini player playing", 5*time.Second, func() bool { pipFrame(c); return pipPlaying(c) })
	for i := 0; i < 10; i++ {
		pipFrame(c)
		time.Sleep(20 * time.Millisecond)
	}
	if !pipPlaying(c) {
		t.Error("the mini player stopped while drawn")
	}
}

// Close gives the inline player the mini player's time, paused; Jump to
// message plays it on there (useResumeAfterPipClose).
func TestPipCloseResumes(t *testing.T) {
	c, _ := fakeEnv(t)
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	playThen(t, c, r, f)
	r.MediaVisible(f, 0.5)
	r.MediaVisible(f, 0.05)
	waitFor(t, "the mini player playing", 5*time.Second, func() bool { pipFrame(c, [2]any{r, f}); return pipPlaying(c) })
	time.Sleep(200 * time.Millisecond)
	at, _, _ := c.PipPlayback()
	c.ClosePip(false) // Close
	if _, _, ok := c.Pip(); ok {
		t.Fatal("still docked after Close")
	}
	if _, ok := c.TileState("pip", "C1", "1.0", f.ID); ok {
		t.Error("the mini player's tile outlived it")
	}
	st := state(t, c, r, f)
	if st.InPip || st.Playing || st.Elapsed < at {
		t.Errorf("after Close: %+v, want the inline player paused at the mini player's %v", st, at)
	}

	// again, then Jump to message: it plays on inline from there
	r.MediaVisible(f, 0.5)
	click(c, r, f)
	waitFor(t, "playing again", 5*time.Second, func() bool { return state(t, c, r, f).Playing })
	r.MediaVisible(f, 0.05)
	waitFor(t, "docked and playing", 5*time.Second, func() bool { pipFrame(c, [2]any{r, f}); return pipPlaying(c) })
	at, _, _ = c.PipPlayback()
	c.ClosePip(true)
	waitFor(t, "the inline player playing on", 5*time.Second, func() bool { return state(t, c, r, f).Playing })
	if pos := state(t, c, r, f).Player.State().Pos; pos < at-100*time.Millisecond {
		t.Errorf("Jump to message: the inline player at %v, the mini player was at %v", pos, at)
	}
}

// The viewer's Dock to corner docks any video; one of a gallery can't play
// inline, so Jump to message doesn't play it there.
func TestPipViewerDockGallery(t *testing.T) {
	c, _ := fakeEnv(t)
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	frame(c, [2]any{r, f})
	c.DockViewer(PipMedia{File: f, Channel: "C1", TS: "1.0", Gallery: true}, 500*time.Millisecond, true)
	if c.PipTrigger() != "manual" {
		t.Fatalf("trigger %q, want manual", c.PipTrigger())
	}
	waitFor(t, "the mini player playing", 5*time.Second, func() bool { pipFrame(c, [2]any{r, f}); return pipPlaying(c) })
	c.ClosePip(true)
	time.Sleep(100 * time.Millisecond)
	if st := state(t, c, r, f); st.Playing || st.Mounted {
		t.Errorf("a gallery's video after Jump to message: %+v, want it not playing inline", st)
	}
}

// Closed while its inline player isn't laid out (its conversation left),
// the time goes to that player when it is.
func TestPipPendingResume(t *testing.T) {
	defer func(d time.Duration) { unmountAfter = d }(unmountAfter)
	unmountAfter = 200 * time.Millisecond
	c, _ := fakeEnv(t)
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	playThen(t, c, r, f)
	frame(c) // navigates away: docks
	waitFor(t, "the inline player let go", 5*time.Second, func() bool {
		pipFrame(c)
		_, ok := c.TileState("channel", "C1", "1.0", f.ID)
		return !ok
	})
	waitFor(t, "the mini player playing", 5*time.Second, func() bool { pipFrame(c); return pipPlaying(c) })
	at, _, _ := c.PipPlayback()
	c.ClosePip(true) // Jump to message: the conversation opens again
	frame(c, [2]any{r, f})
	waitFor(t, "the inline player playing from there", 5*time.Second, func() bool { return state(t, c, r, f).Playing })
	if pos := state(t, c, r, f).Player.State().Pos; pos < at-100*time.Millisecond {
		t.Errorf("laid out again: the inline player at %v, the mini player was at %v", pos, at)
	}
}
