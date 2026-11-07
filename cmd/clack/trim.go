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
// idle. Every 30 s (first after 10 s), when more than 16 MB is free but
// unreturned, run one FreeOSMemory (a GC plus scavenge, ~10-30 ms of
// background CPU). ReadMemStats alone costs microseconds.
func trimMemory() {
	if os.Getenv("CLACK_NOTRIM") != "" {
		return
	}
	go func() {
		time.Sleep(10 * time.Second)
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			if m.HeapIdle-m.HeapReleased > 16<<20 {
				debug.FreeOSMemory()
			}
			<-t.C
		}
	}()
}
