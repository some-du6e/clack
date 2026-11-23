package jsrt

import "testing"

// The audio tests' hooks into the runtime (audio_host_test.go).

// withAudio gives the page an audio host (Options.Audio).
func withAudio(o *Options, h AudioHost) { o.Audio = h }

// timersFired is how many timers r has fired.
func timersFired(r *Runtime) uint64 { return r.fired.Load() }

// recordedHeld is how many samples the host holds for the page's
// recordings.
func recordedHeld(r *Runtime) int {
	r.audioRecs.mu.Lock()
	defer r.audioRecs.mu.Unlock()
	return r.audioRecs.held
}

// setRecordCap is maxRecordSamples until the test ends.
func setRecordCap(t *testing.T, n int) {
	old := maxRecordSamples
	maxRecordSamples = n
	t.Cleanup(func() { maxRecordSamples = old })
}
