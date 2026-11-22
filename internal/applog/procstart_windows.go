//go:build windows

package applog

import (
	"time"

	"golang.org/x/sys/windows"
)

// processStart is GetProcessTimes' creation time (100 ns steps).
func processStart() (time.Time, bool) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user); err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, created.Nanoseconds()), true
}
