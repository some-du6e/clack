package ui

import "time"

// The plugins job's idle passes draw as the window does on trees with quiet
// frames (quiet.go): perf_plugins_test.go runs on the base tree too, where
// these stay nil and every frame is laid out.
func init() {
	perfQuietStart = func(e *benchEnv, wake func()) func() {
		onAnimRepaint(e.perfEnv, wake)
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
