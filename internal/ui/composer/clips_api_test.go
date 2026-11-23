package composer

import "testing"

// setClipRecorder makes this build one that can record (Qre6 hP's
// window.MediaRecorder) for the test. The clip tests' red run (on the code
// before clips) replaces this file with a no-op.
func setClipRecorder(t *testing.T, on bool) {
	t.Helper()
	old := clipRecorder
	clipRecorder = func() bool { return on }
	t.Cleanup(func() { clipRecorder = old })
}
