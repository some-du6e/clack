// Package inputidle records when the person last gave Clack input (the
// pointer moving or pressing, a wheel or trackpad scroll, a key), for work
// that should wait for a pause in it: cmd/clack's forced collections,
// whose stop-the-world phases and full scavenge would land in the middle
// of a scroll. Frames don't count: a focused composer's caret redraws the
// window twice a second for as long as it's focused.
package inputidle

import (
	"sync/atomic"
	"time"
)

var (
	start = time.Now()
	last  atomic.Int64 // unix ns of the last input; 0 = none yet
)

// Note records input now. Any goroutine; one atomic store.
func Note() { last.Store(time.Now().UnixNano()) }

// Since is how long ago the last input was (since the process started,
// when there was none). Any goroutine.
func Since() time.Duration {
	t := last.Load()
	if t == 0 {
		return time.Since(start)
	}
	return time.Since(time.Unix(0, t))
}
