//go:build windows

package ui

import (
	"syscall"
	"time"
)

// procCPU is the process's user + kernel CPU time so far
// (GetProcessTimes; 100 ns units).
func procCPU() time.Duration {
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0
	}
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return 0
	}
	ft := func(f syscall.Filetime) time.Duration {
		return time.Duration(int64(f.HighDateTime)<<32|int64(f.LowDateTime)) * 100
	}
	return ft(kernel) + ft(user)
}
