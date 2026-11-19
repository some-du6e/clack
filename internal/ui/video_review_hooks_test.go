package ui

// video_review_test.go's calls into what the review's fixes added.

// playerFrameReady is the repaint the App gives its players for a frame.
func playerFrameReady(e *perfEnv) func() { return e.a.videos().FrameReady() }

// hudPings are the times the App asked the huddle windows to draw.
func hudPings(e *perfEnv) int64 { return e.a.hudPings.Load() }
