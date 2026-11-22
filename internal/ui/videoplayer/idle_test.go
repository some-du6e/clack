package videoplayer

// Round 3's review, a MINOR: a paused player whose tile stays on screen
// kept its whole pipeline (Media Foundation's engine and its own device,
// the decoder's surfaces, AVPlayer, the 16 MB cache) for good. Chromium
// suspends a paused player idle 15 s and keeps its last frame on screen;
// the next play resumes it. On the round-3 head (0cbe7e63) the player
// stayed. setIdleAfter and tileStill are idle_hooks_test.go's.

import (
	"image"
	"testing"
	"time"
)

func TestIdlePlayerLetsGo(t *testing.T) {
	setIdleAfter(t, 300*time.Millisecond)
	c, _ := fakeEnv(t)
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	g := &tileRig{c: c, r: r, f: f, size: image.Pt(480, 270), at: time.Second}
	g.frame()
	click(c, r, f)
	waitForTile(t, c, r, f, "playing", 10*time.Second, func() bool { g.frame(); st := state(t, c, r, f); return st.Playing && !st.Loading })
	for t0 := time.Now(); time.Since(t0) < 300*time.Millisecond; time.Sleep(10 * time.Millisecond) {
		g.frame() // drawn, as on screen
	}
	click(c, r, f) // pause
	waitForTile(t, c, r, f, "paused", 5*time.Second, func() bool { g.frame(); return !state(t, c, r, f).Playing })
	left := state(t, c, r, f).Elapsed
	// laid out frame after frame, paused: the idle delay later it lets go
	waitFor(t, "the idle player let go", 3*time.Second, func() bool { g.frame(); return c.Mounted() == 0 })
	st := state(t, c, r, f)
	if !tileStill(st) {
		t.Error("the tile let its player go and shows no frame of it (the poster again)")
	}
	if p, ok := c.Progress(f.ID); !ok || p < left-100*time.Millisecond || p > left+100*time.Millisecond {
		t.Errorf("progress %v (%v) after the release, paused at %v", p, ok, left)
	}
	// the next click plays it again from there; the kept frame goes at the
	// new player's first
	click(c, r, f)
	waitForTile(t, c, r, f, "playing again", 10*time.Second, func() bool { g.frame(); st := state(t, c, r, f); return st.Playing && !st.Loading })
	if pos := state(t, c, r, f).Player.State().Pos; pos < left-100*time.Millisecond || pos > left+900*time.Millisecond {
		t.Errorf("the new player starts at %v, paused at %v", pos, left)
	}
	waitForTile(t, c, r, f, "the kept frame gone", 5*time.Second, func() bool { g.frame(); return !tileStill(state(t, c, r, f)) })
}
