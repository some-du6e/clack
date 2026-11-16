// Package awake is a monotonic clock of the time the computer is awake.
// The jsrt task deadlines and the UI freeze watchdog run on it, so a task
// or a frame that was running when the lid closed isn't hours over its
// limit when the computer wakes.
//
// What each OS's clock does with sleep:
//   - Linux: Go's monotonic clock is CLOCK_MONOTONIC, which stops while
//     the system is suspended. Now is that clock.
//   - macOS: Go's monotonic clock is mach_absolute_time, which stops while
//     the Mac sleeps. Now is that clock.
//   - Windows: Go's monotonic clock reads InterruptTime, which goes on
//     through sleep and hibernation. Now reads QueryUnbiasedInterruptTime
//     instead: the same counter without the time asleep or hibernating,
//     and as fine-grained (both tick with the clock interrupt).
package awake

import "time"

// base is the zero of the clock (about when the process started).
var base = time.Now()

// Now is the awake time since the process started, in ns; never 0. Any
// goroutine.
func Now() int64 { return now() }

// Since is the awake time since t, a value of Now.
func Since(t int64) time.Duration { return time.Duration(Now() - t) }
