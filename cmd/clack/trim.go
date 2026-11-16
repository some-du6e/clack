package main

import (
	"os"
	"runtime/debug"
	"runtime/metrics"
	"sync"
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

// freeOSGap is the least time between two forced collections. A request
// inside it runs once when it ends (requests coalesce), so a cache pass, a
// members pass and the trim loop landing together cost one collection.
const freeOSGap = 10 * time.Second

// trimMemory hands freed heap back to the OS. The Go runtime keeps freed
// pages up to its heap goal (2x the live heap), so after boot (megabytes of
// JSON) or a burst of scrolling the process would sit at its peak while
// idle. Every 5 s for the first minute, then every 30 s, when more than 16
// MB is free but unreturned, or the heap holds much more than the last
// collection left live and none ran for a minute, run one FreeOSMemory (a
// GC plus scavenge, ~10-30 ms of background CPU).
//
// The figures come from runtime/metrics, not runtime.ReadMemStats:
// ReadMemStats stops the world, and a stop-the-world waits for every
// goroutine to reach a safe point, the plugin JS thread included, which
// Go can't stop while it runs wasm (internal/jsrt/diag.go). Reading
// metrics stops nothing.
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
			time.AfterFunc(2*time.Second, freeOS.request)
		}
	}
	// a members pass (Slack's members cache: down to 500 once 700 are
	// held) that let thousands go after an hour of scrolling: the same
	core.AfterMembersEvict = func(n int) {
		if n >= membersCollect {
			time.AfterFunc(2*time.Second, freeOS.request)
		}
	}
	go func() {
		s := trimSamples()
		start := time.Now()
		var cycles uint64
		lastGC := start // when the cycle count was last seen to move
		for {
			// every 5 s for the first minute (boot, prefetch and image
			// loads land then), every 30 s after
			d := 30 * time.Second
			if time.Since(start) < time.Minute {
				d = 5 * time.Second
			}
			time.Sleep(d)
			metrics.Read(s)
			if !allUint64(s) {
				continue // a runtime without these metrics: never guess
			}
			heap, goal, free, n := s[0].Value.Uint64(), s[1].Value.Uint64(), s[2].Value.Uint64(), s[3].Value.Uint64()
			if n != cycles {
				cycles, lastGC = n, time.Now()
			}
			// what's live is about half the goal the last collection set
			// (GOGC 100): far more than that allocated, with no collection
			// in a minute, is mostly garbage nothing will collect soon while
			// the window idles (a message cache pass let go of 200 MB)
			garbage := heap > goal/2+64<<20 && time.Since(lastGC) > time.Minute
			if free > 16<<20 || garbage {
				freeOS.request()
			}
		}
	}()
}

// trimSamples are the figures the trim loop reads, in this order.
func trimSamples() []metrics.Sample {
	return []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"}, // MemStats.HeapAlloc
		{Name: "/gc/heap/goal:bytes"},                // MemStats.NextGC
		{Name: "/memory/classes/heap/free:bytes"},    // MemStats.HeapIdle - HeapReleased
		{Name: "/gc/cycles/total:gc-cycles"},         // MemStats.NumGC
	}
}

func allUint64(s []metrics.Sample) bool {
	for _, x := range s {
		if x.Value.Kind() != metrics.KindUint64 {
			return false
		}
	}
	return true
}

// freeOS runs debug.FreeOSMemory at most once per freeOSGap.
var freeOS = &freeOSLimiter{gap: freeOSGap, run: debug.FreeOSMemory}

type freeOSLimiter struct {
	gap time.Duration
	run func()

	mu      sync.Mutex
	last    time.Time // when the last collection started
	pending bool      // one is scheduled for the end of the gap
}

// request collects now, or once at the end of the gap since the last one.
// Any goroutine; the collection runs on the caller's goroutine or a timer's.
func (l *freeOSLimiter) request() {
	l.mu.Lock()
	if l.pending {
		l.mu.Unlock()
		return
	}
	if wait := l.gap - time.Since(l.last); !l.last.IsZero() && wait > 0 {
		l.pending = true
		l.mu.Unlock()
		time.AfterFunc(wait, func() {
			l.mu.Lock()
			l.pending = false
			l.last = time.Now()
			l.mu.Unlock()
			l.run()
		})
		return
	}
	l.last = time.Now()
	l.mu.Unlock()
	l.run()
}
