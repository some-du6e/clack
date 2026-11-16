package ui

import (
	"sync/atomic"
	"time"
)

// lastFrameAt is when the window last started a frame (unix ns; 0 before
// the first).
var lastFrameAt atomic.Int64

// FrameWithin reports whether the window drew in the last d: someone is
// scrolling, typing or watching something move. cmd/clack's memory trim
// waits for a quiet moment before it forces a collection, whose
// stop-the-world phases stop the UI goroutine too (for as long as the
// plugin runtime's current JS task takes to reach a call into Go: the Go
// runtime can't preempt its compiled wasm) and whose scavenge of all free
// memory holds the heap lock new allocations need. Any goroutine.
func FrameWithin(d time.Duration) bool {
	t := lastFrameAt.Load()
	return t != 0 && time.Since(time.Unix(0, t)) < d
}
