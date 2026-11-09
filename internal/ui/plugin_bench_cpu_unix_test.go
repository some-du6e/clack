//go:build unix

package ui

import (
	"syscall"
	"time"
)

// procCPU is the process's user + system CPU time so far.
func procCPU() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
