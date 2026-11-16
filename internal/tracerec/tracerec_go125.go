//go:build go1.25

package tracerec

import (
	"runtime/trace"
	"time"
)

// Impl names the recorder this build uses.
const Impl = "runtime/trace"

// newRecorder is runtime/trace's FlightRecorder (Go 1.25 and newer).
func newRecorder(period time.Duration, size uint64) Recorder {
	return trace.NewFlightRecorder(trace.FlightRecorderConfig{MinAge: period, MaxBytes: size})
}
