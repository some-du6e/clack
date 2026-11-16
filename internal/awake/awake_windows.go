//go:build windows

package awake

import (
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procQueryUnbiasedInterruptTime = windows.NewLazySystemDLL("kernel32.dll").NewProc("QueryUnbiasedInterruptTime")

// unbiased is QueryUnbiasedInterruptTime in ns: the interrupt time since
// boot (what Go's monotonic clock reads here) without the time asleep or
// hibernating. A user-mode read of the shared system page; no allocation.
func unbiased() (int64, bool) {
	var t uint64
	if r, _, _ := syscall.SyscallN(procQueryUnbiasedInterruptTime.Addr(), uintptr(unsafe.Pointer(&t))); r == 0 {
		return 0, false
	}
	return int64(t) * 100, true
}

var (
	ubase int64        // unbiased() at init
	slept atomic.Int64 // Go's clock minus Now at the last good read
)

func init() { ubase, _ = unbiased() }

func now() int64 {
	m := int64(time.Since(base))
	u, ok := unbiased()
	if !ok {
		// documented to fail only for a nil pointer; if it ever did, go on
		// from the last good read so the clock doesn't jump
		return m - slept.Load() + 1
	}
	n := u - ubase
	slept.Store(m - n)
	return n + 1
}
