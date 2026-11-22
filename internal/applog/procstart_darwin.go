//go:build darwin

package applog

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// processStart is the kernel's p_starttime (kern.proc.pid): when launchd,
// or the shell, spawned this process, in microseconds.
func processStart() (time.Time, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", os.Getpid())
	if err != nil {
		return time.Time{}, false
	}
	tv := kp.Proc.P_starttime
	return time.Unix(int64(tv.Sec), int64(tv.Usec)*1000), true
}
