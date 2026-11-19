package ui

// video_review_test.go's calls into what the review's fixes added.

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// playerFrameReady is the repaint the App gives its players for a frame.
func playerFrameReady(e *perfEnv) func() { return e.a.videos().FrameReady() }

// hudPings are the times the App asked the huddle windows to draw.
func hudPings(e *perfEnv) int64 { return e.a.hudPings.Load() }

// volumeOpen is whether a tile's volume slider shows.
func volumeOpen(st videoplayer.State) bool { return st.VolumeOpen }

// keepIdlePlayers keeps paused players mounted for the test (their idle
// release would let them go 15 s after a pause, videoplayer.SetIdleAfter).
func keepIdlePlayers(t *testing.T) { t.Cleanup(videoplayer.SetIdleAfter(time.Hour)) }
