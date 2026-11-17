package ui

import "time"

// The plugins job's idle passes draw as the window does on trees with quiet
// frames (quiet.go): perf_plugins_test.go runs on the base tree too, where
// these stay nil and every frame is laid out.
func init() {
	perfQuietStart = func(e *benchEnv, wake func()) func() {
		// perfIdle's invalidateHook wakes it for both (Invalidate and
		// animReady call it): a second wake drew a frame nothing asked for
		onAnimRepaint(e.perfEnv, func() {})
		return func() {
			e.a.images.SetAnimInvalidate(nil)
			e.a.images.SetLiveOps(false)
		}
	}
	perfQuietFrame = func(e *benchEnv, asked bool, askedAt time.Time) bool {
		return e.perfEnv.quietFrame(asked, askedAt)
	}
	perfQuietWhy = func(e *benchEnv) string { return e.a.quietWhy }
}
