package videoplayer

// Round 3's review, a MINOR: before any click the pill and the scrubber
// show where the viewer left the file on the server (media_progress,
// fIJO's use_media_elapsed via xnwb). On the round-3 head (0cbe7e63) they
// showed only the local progress.

import (
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/richtext"
)

func TestTileShowsServerProgress(t *testing.T) {
	c, _ := slowEnv(t, 0, media.FakeOptions{})
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	f.MediaOffsetMS = 30000 // watched to 0:30 elsewhere
	layoutAt(c, r, f, image.Pt(480, 270))
	st := state(t, c, r, f)
	if st.Mounted || st.Elapsed != 30*time.Second || !st.Played {
		t.Errorf("before any click: %+v, want the time played, 0:30, from media_progress", st)
	}
	if got := pillTimeOf(c, r, f); got != "0:30" {
		t.Errorf("the pill reads %q, want 0:30", got)
	}
	w := videoFile("F2")
	w.MediaOffsetMS, w.MediaWatched = 30000, true // watched to the end: starts over
	layoutAt(c, r, w, image.Pt(480, 270))
	if st := state(t, c, r, w); st.Elapsed != 0 || st.Played {
		t.Errorf("a watched file: %+v, want it at rest at 0", st)
	}
}

// pillTimeOf is the tile's pill, not hovered.
func pillTimeOf(c *Controller, r *Row, f *richtext.File) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tiles[r.key(f)]
	if t == nil {
		return ""
	}
	return pillTime(t, false)
}

// A first player starts where the viewer left the file on the server
// (media_progress.offset_ms), or at 0 once it was watched (xnwb).
func TestPlayerStartsAtServerProgress(t *testing.T) {
	c, _ := slowEnv(t, 0, media.FakeOptions{})
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	f.MediaOffsetMS = 1500
	layoutAt(c, r, f, image.Pt(480, 270))
	click(c, r, f)
	waitForTile(t, c, r, f, "playing", 10*time.Second, func() bool { st := state(t, c, r, f); return st.Playing && !st.Loading })
	if p := state(t, c, r, f).Player.State().Pos; p < 1400*time.Millisecond || p > 2300*time.Millisecond {
		t.Errorf("the first player starts at %v, want media_progress's 1.5 s", p)
	}
	w := videoFile("F2")
	w.MediaOffsetMS, w.MediaWatched = 1500, true
	layoutAt(c, r, w, image.Pt(480, 270))
	click(c, r, w)
	waitForTile(t, c, r, w, "playing", 10*time.Second, func() bool { st := state(t, c, r, w); return st.Playing && !st.Loading })
	if p := state(t, c, r, w).Player.State().Pos; p > 700*time.Millisecond {
		t.Errorf("a watched file's player starts at %v, want 0", p)
	}
}
