//go:build linux

package applog

import (
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// clockTicks is USER_HZ, the unit of /proc's start times (100 on every
// Linux Go supports).
const clockTicks = 100

// processStart is /proc/self/stat's starttime (clock ticks since boot, so
// 10 ms steps) against CLOCK_BOOTTIME now: the process's age, back from
// now.
func processStart() (time.Time, bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return time.Time{}, false
	}
	// pid (comm) state ppid …: comm may hold spaces and parentheses, so
	// the fields start after its last ")"; starttime is field 22, the
	// 20th after comm.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return time.Time{}, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return time.Time{}, false
	}
	ticks, err := strconv.ParseUint(f[19], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	var ts unix.Timespec
	if unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts) != nil {
		return time.Time{}, false
	}
	now := time.Now()
	age := time.Duration(ts.Nano()) - time.Duration(ticks)*(time.Second/clockTicks)
	if age < 0 {
		age = 0
	}
	return now.Add(-age), true
}
