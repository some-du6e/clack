package slack

import (
	"sync/atomic"
	"time"
)

// ClockReading is one reading of the wall clock and of Go's monotonic
// clock. The two run apart while the machine sleeps: on darwin the
// monotonic clock is mach_absolute_time and on Linux CLOCK_MONOTONIC,
// and neither counts time asleep, while the wall clock does. So
// time.Since on a time.Time from before a sleep leaves the sleep out.
// Anything that ages across a sleep (a socket's lifetime, a
// reconnect_url's age) has to compare Wall readings, and the gap between
// the two clocks tells how long the machine slept.
type ClockReading struct {
	Wall time.Time     // wall clock, monotonic reading stripped (Round(0))
	Mono time.Duration // monotonic time since the process started
}

// WallSince is the wall-clock time from earlier to r, sleep included.
func (r ClockReading) WallSince(earlier ClockReading) time.Duration {
	return r.Wall.Sub(earlier.Wall)
}

// Slept is how much longer the wall clock ran than the monotonic one
// from earlier to r: the time the machine spent asleep (or a wall-clock
// step, which is why callers only believe it above a threshold).
func (r ClockReading) Slept(earlier ClockReading) time.Duration {
	return r.Wall.Sub(earlier.Wall) - (r.Mono - earlier.Mono)
}

var clockStart = time.Now()

func realClock() ClockReading {
	now := time.Now()
	return ClockReading{Wall: now.Round(0), Mono: now.Sub(clockStart)}
}

var clockHook atomic.Pointer[func() ClockReading]

// ReadClock reads both clocks.
func ReadClock() ClockReading {
	if f := clockHook.Load(); f != nil {
		return (*f)()
	}
	return realClock()
}

// SetClock replaces ReadClock's source (tests simulate a sleep with it)
// and returns the undo.
func SetClock(f func() ClockReading) func() {
	prev := clockHook.Swap(&f)
	return func() { clockHook.Store(prev) }
}

// WakeDrift is how far the wall clock must run ahead of the monotonic one
// before Clack believes the machine slept (smaller steps are NTP slews).
const WakeDrift = 30 * time.Second
