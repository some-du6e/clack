//go:build !darwin && !linux && !windows

package applog

import "time"

func processStart() (time.Time, bool) { return time.Time{}, false }
