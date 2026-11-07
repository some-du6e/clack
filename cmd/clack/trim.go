package main

import (
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

func init() { startTrim = trimMemory }

// trimMemory hands freed heap back to the OS. The Go runtime keeps freed
// pages up to its heap goal (2x the live heap), so after boot (megabytes of
// JSON) or a burst of scrolling the process would sit at its peak while
// idle. Every 5 s for the first minute, then every 30 s, when more than 16 MB is free but
// unreturned, run one FreeOSMemory (a GC plus scavenge, ~10-30 ms of
// background CPU). ReadMemStats alone costs microseconds.
func trimMemory() {
	if os.Getenv("CLACK_NOTRIM") != "" {
		return
	}
	go func() {
		var m runtime.MemStats
		start := time.Now()
		for {
			// every 5 s for the first minute (boot, prefetch and image
			// loads land then), every 30 s after
			d := 30 * time.Second
			if time.Since(start) < time.Minute {
				d = 5 * time.Second
			}
			time.Sleep(d)
			runtime.ReadMemStats(&m)
			if m.HeapIdle-m.HeapReleased > 16<<20 {
				debug.FreeOSMemory()
			}
		}
	}()
}
