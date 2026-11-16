// Package tracerec is a flight recorder: the Go runtime's execution
// trace of the last few seconds, held in memory and written out on
// demand (internal/ui's freeze reports use it).
//
// Go 1.25 has one in runtime/trace (tracerec_go125.go). Before that it is
// golang.org/x/exp/trace's (tracerec_xexp.go), which must never be built
// with a newer Go: Go 1.26's tracer sends an in-band end-of-generation
// byte that x/exp's parser rejects, and its error path calls trace.Stop on
// the tracer's own reader goroutine, which deadlocks it, and with it every
// later WriteTo and Stop (Go 1.25 likely breaks it too, differently).
// go.mod's toolchain line is only a minimum, so a local build with a newer
// Go would; the go1.25 build tag gives that build the runtime's recorder.
// ci's newer-go job runs this package's tests on Go 1.25 and the newest Go.
package tracerec

import (
	"io"
	"time"
)

// Recorder is a flight recorder. After Start, WriteTo writes the window it
// holds: about the last MinAge of trace, at most about MaxBytes, as a trace
// file `go tool trace` reads (of the Go version that built the program, or
// newer). Stop ends it, once a WriteTo in progress returns. At most one
// recorder runs in a process at a time; Start fails while another does.
type Recorder interface {
	Start() error
	Stop()
	WriteTo(w io.Writer) (int64, error)
}

// New returns a stopped recorder that keeps about the last period of
// trace, up to about size bytes.
func New(period time.Duration, size uint64) Recorder { return newRecorder(period, size) }
