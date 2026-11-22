package videoplayer

// Round 4, step 4: a video that starts pauses the one playing only once
// it has the data to play. Chromium fires <video autoplay>'s play when
// enough data has come, and fIJO's onPlay (tI) is where it calls
// bxCZ.playbackStarted; before, the other video paused at the click.

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
)

func TestAutoplayPausesTheOtherOnceItPlays(t *testing.T) {
	c, _ := slowEnv(t, 250*time.Millisecond, media.FakeOptions{}) // every read waits: a new player loads a while
	a, fa := c.Row("channel", "C1", "1.0", nil), videoFile("FA")
	b, fb := c.Row("channel", "C1", "2.0", nil), videoFile("FB")
	frame(c, [2]any{a, fa}, [2]any{b, fb})
	click(c, a, fa)
	waitForTile(t, c, a, fa, "A playing", 15*time.Second, func() bool { st := state(t, c, a, fa); return st.Playing && !st.Loading })
	click(c, b, fb)
	time.Sleep(150 * time.Millisecond)
	if !state(t, c, b, fb).Loading {
		t.Skip("B loaded within 150 ms")
	}
	if !state(t, c, a, fa).Playing {
		t.Error("A paused at B's click, B still loading: it plays on until B plays")
	}
	waitForTile(t, c, b, fb, "B playing", 15*time.Second, func() bool { st := state(t, c, b, fb); return st.Playing && !st.Loading })
	waitForTile(t, c, a, fa, "A paused once B plays", 5*time.Second, func() bool { return !state(t, c, a, fa).Playing })
	// a click on a loaded, paused video pauses the other at once (play()
	// on a video with its data fires play then)
	click(c, a, fa)
	waitForTile(t, c, a, fa, "A playing again", 5*time.Second, func() bool { return state(t, c, a, fa).Playing })
	waitForTile(t, c, b, fb, "B paused at once", 2*time.Second, func() bool { return !state(t, c, b, fb).Playing })
}
