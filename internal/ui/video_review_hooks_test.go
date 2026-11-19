package ui

// video_review_test.go's calls into what the review's fixes added.

import "github.com/some-du6e/clack/internal/ui/videoplayer"

// playerFrameReady is the repaint the App gives its players for a frame.
func playerFrameReady(e *perfEnv) func() { return e.a.videos().FrameReady() }

// hudPings are the times the App asked the huddle windows to draw.
func hudPings(e *perfEnv) int64 { return e.a.hudPings.Load() }

// volumeOpen is whether a tile's volume slider shows.
func volumeOpen(st videoplayer.State) bool { return st.VolumeOpen }
