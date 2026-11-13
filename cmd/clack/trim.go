package main

import (
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

func init() { startTrim = trimMemory }

// memoryLimit is the Go runtime's soft memory limit (debug.SetMemoryLimit),
// a backstop and not how Clack stays small: the caches are bounded on
// their own (the memory workflow, perf.yml's scale jobs, measures them).
// With GOGC at 100 the heap may grow to twice what's live before the
// collector runs; near the limit it runs sooner instead. It sits above
// the heaviest session the scale jobs drive (two 3,000-message channels
// loaded at once: about 500 MB live), so it never makes the collector run
// nonstop while scrolling; if what's live ever exceeds it, the runtime's
// GC CPU limit (half the CPU) lets the heap grow rather than thrash.
// GOMEMLIMIT, when set, wins.
const memoryLimit = 768 << 20

// evictCollect: a cache pass that let this many messages go is collected
// at once (core.AfterEvict); about 30 MB of documents, rows and plugin
// state at Hack Club scale.
const evictCollect = 1000

// membersCollect: a members pass that let this many members go is
// collected at once (core.AfterMembersEvict); a member is about 2 KB.
const membersCollect = 1000

// trimMemory hands freed heap back to the OS. The Go runtime keeps freed
// pages up to its heap goal (2x the live heap), so after boot (megabytes of
// JSON) or a burst of scrolling the process would sit at its peak while
// idle. Every 5 s for the first minute, then every 30 s, when more than 16 MB is free but
// unreturned, or the heap holds much more than the last collection left
// live and none ran for a minute, run one FreeOSMemory (a GC plus
// scavenge, ~10-30 ms of background CPU). ReadMemStats alone costs
// microseconds.
func trimMemory() {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(memoryLimit)
	}
	if os.Getenv("CLACK_NOTRIM") != "" {
		return
	}
	// a message cache pass that let many messages go: collect once the
	// lists have dropped their rows too (a frame or two), so the memory
	// goes back now; the loop below would wait for a minute without a
	// collection (a pass let go of 150 MB seconds before the scale run's
	// idle snapshot)
	core.AfterEvict = func(n int) {
		if n >= evictCollect {
			time.AfterFunc(2*time.Second, debug.FreeOSMemory)
		}
	}
	// a members pass (Slack's members cache: down to 500 once 700 are
	// held) that let thousands go after an hour of scrolling: the same
	core.AfterMembersEvict = func(n int) {
		if n >= membersCollect {
			time.AfterFunc(2*time.Second, debug.FreeOSMemory)
		}
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
			// what's live is about half the goal the last collection set
			// (GOGC 100): far more than that allocated, with no collection
			// in a minute, is mostly garbage nothing will collect soon while
			// the window idles (a message cache pass let go of 200 MB)
			garbage := m.HeapAlloc > m.NextGC/2+64<<20 && time.Since(time.Unix(0, int64(m.LastGC))) > time.Minute
			if m.HeapIdle-m.HeapReleased > 16<<20 || garbage {
				debug.FreeOSMemory()
			}
		}
	}()
}
