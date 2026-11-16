package jsrt

import (
	"fmt"
	"sync"
	"time"
)

// Freeze diagnostics. wazero's compiled code has no safe points, so Go
// can't stop the JS thread while it runs wasm: a Go stop-the-world (each
// GC cycle, runtime.ReadMemStats, runtime.Stack) waits until the thread
// next calls the host, with every goroutine paused. QuickJS's C loops yield
// to the host (csrc/quickjs-yield.patch); Snapshots says what each runtime
// is doing, from atomics only, so a freeze report can print it while the
// JS thread is busy.

// live holds the runtimes that haven't shut down (*Runtime → struct{}).
var live sync.Map

// Snapshot is one Runtime's state.
type Snapshot struct {
	Task       time.Duration // how long the running task has run; 0 = idle
	Running    string        // what it is ("Call taut.rtmEvent", "boot"; "" = unknown)
	SinceExit  time.Duration // since the JS thread last left the wasm to poll its interrupt or yield (when a task runs)
	GC         time.Duration // how long QuickJS's running collection has run; 0 = not collecting
	LastGC     time.Duration // the last collection's length
	GCObjects  uint32        // objects the last collection scanned
	GCs        uint64        // collections so far
	Heap       uint64        // JS heap bytes, as of the last task or collection
	Allocs     uint32        // live JS heap allocations, as of the last task
	Yields     uint64        // native-loop yields (host_yield)
	Interrupts uint64        // interrupt polls that stopped a task past its deadline
	Abandoned  bool          // the watchdog gave up on it, but its JS thread is still running
}

// Snapshots describes every live Runtime. Any goroutine; atomics only, so
// it answers while a JS thread is stuck.
func Snapshots() []Snapshot {
	var out []Snapshot
	now := monoNow()
	live.Range(func(k, _ any) bool {
		out = append(out, k.(*Runtime).snapshot(now))
		return true
	})
	return out
}

func (r *Runtime) snapshot(now int64) Snapshot {
	s := Snapshot{
		LastGC:     time.Duration(r.gcLast.Load()),
		GCObjects:  r.gcObjects.Load(),
		GCs:        r.gcs.Load(),
		Yields:     r.yields.Load(),
		Interrupts: r.interrupts.Load(),
	}
	h := r.heap.Load()
	s.Heap, s.Allocs = h>>32, uint32(h)
	since := func(t int64) time.Duration { return time.Duration(max(now-t, 1)) } // t may be a hair after now
	if t := r.taskStart.Load(); t != 0 {
		s.Task = since(t)
		if rt := r.running.Load(); rt != nil {
			s.Running = rt.name()
		}
		if x := r.lastExit.Load(); x > t {
			s.SinceExit = since(x)
		} else {
			s.SinceExit = s.Task
		}
	}
	if g := r.gcStart.Load(); g != 0 {
		s.GC = since(g)
	}
	select {
	case <-r.done:
		s.Abandoned = true // done fired, yet the loop (which unlists it) hasn't returned
	default:
	}
	return s
}

// String is the snapshot in one line.
func (s Snapshot) String() string {
	mib := func(b uint64) string { return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20)) }
	out := "idle"
	if s.Task > 0 {
		what := ""
		if s.Running != "" {
			what = s.Running + "; "
		}
		out = fmt.Sprintf("a task running for %v (%slast interrupt poll or yield %v ago)", s.Task.Round(time.Millisecond), what, s.SinceExit.Round(time.Millisecond))
	}
	if s.Abandoned {
		out = "abandoned by the jsrt watchdog but still running: " + out
	}
	if s.GC > 0 {
		out += fmt.Sprintf("; QuickJS's garbage collector running for %v", s.GC.Round(time.Millisecond))
	}
	out += fmt.Sprintf("; JS heap %s in %d allocations; %d collections, the last %v over %d objects; %d yields, %d interrupts",
		mib(s.Heap), s.Allocs, s.GCs, s.LastGC.Round(time.Millisecond), s.GCObjects, s.Yields, s.Interrupts)
	return out
}

// slowGC is a collection worth a log line.
const slowGC = 250 * time.Millisecond

// gcEvent records QuickJS's cycle collector starting (phase 0) or ending
// (1) with the objects it scanned and the heap size. JS thread, called from
// inside the collector: it must not call into the engine.
func (r *Runtime) gcEvent(phase int32, objects, heap uint32) {
	now := monoNow()
	r.lastExit.Store(now)
	r.heap.Store(uint64(heap)<<32 | r.heap.Load()&0xffffffff)
	switch phase {
	case 0:
		if r.gcDepth++; r.gcDepth == 1 {
			r.gcStart.Store(now)
		}
	case 1:
		if r.gcDepth == 0 {
			return
		}
		if r.gcDepth--; r.gcDepth > 0 {
			return
		}
		d := time.Duration(now - r.gcStart.Load())
		r.gcStart.Store(0)
		r.gcLast.Store(int64(d))
		r.gcObjects.Store(objects)
		r.gcs.Add(1)
		if d >= slowGC {
			r.slowGCs++
			r.slowGCMsg = fmt.Sprintf("%v over %d objects, JS heap %.0f MiB after", d.Round(time.Millisecond), objects, float64(heap)/(1<<20))
		}
	}
}

// logSlowGC logs the latest slow collection, at most once a minute (after
// the task: logging may do anything, the collector's callback may not).
func (r *Runtime) logSlowGC() {
	if r.slowGCs == 0 {
		return
	}
	now := monoNow()
	if r.slowGCLog != 0 && now-r.slowGCLog < int64(time.Minute) {
		return
	}
	msg := "jsrt: QuickJS's garbage collector took " + r.slowGCMsg
	if r.slowGCs > 1 {
		msg += fmt.Sprintf(" (%d collections over %v since the last line)", r.slowGCs, slowGC)
	}
	r.opts.Log("warn", msg)
	r.slowGCs, r.slowGCLog = 0, now
}

// setYield sets how many steps QuickJS's native loops take between host
// calls (n <= 0: none, as before csrc/quickjs-yield.patch). Engines
// without the patch ignore it. JS thread.
func (e *engine) setYield(n int) {
	if e.fns["jr_set_yield"] != nil && e.mod != nil {
		e.call("jr_set_yield", uint64(uint32(int32(n))))
	}
}
