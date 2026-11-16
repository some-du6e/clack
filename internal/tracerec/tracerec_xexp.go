//go:build !go1.25

package tracerec

import (
	"io"
	"time"

	xtrace "golang.org/x/exp/trace"
)

// Impl names the recorder this build uses.
const Impl = "golang.org/x/exp/trace"

// newRecorder is golang.org/x/exp/trace's FlightRecorder, for Go 1.24 and
// older only (see the package comment).
func newRecorder(period time.Duration, size uint64) Recorder {
	fr := xtrace.NewFlightRecorder()
	fr.SetPeriod(period)
	fr.SetSize(int(size))
	return xexp{fr}
}

// xexp is x/exp's recorder with runtime/trace's method set.
type xexp struct{ fr *xtrace.FlightRecorder }

func (x xexp) Start() error { return x.fr.Start() }

// Stop's only error is a recorder that wasn't started.
func (x xexp) Stop() { _ = x.fr.Stop() }

func (x xexp) WriteTo(w io.Writer) (int64, error) {
	n, err := x.fr.WriteTo(w)
	return int64(n), err
}
