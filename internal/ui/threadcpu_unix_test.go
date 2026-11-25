//go:build linux || darwin

package ui

import (
	"time"

	"golang.org/x/sys/unix"
)

// threadCPU is the CPU time the calling OS thread has used
// (CLOCK_THREAD_CPUTIME_ID), or -1 where it can't be read.
func threadCPU() time.Duration {
	var ts unix.Timespec
	if unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts) != nil {
		return -1
	}
	return time.Duration(ts.Nano())
}
