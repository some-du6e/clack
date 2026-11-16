package ui

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/tracerec"
)

// The flight recorder: the Go runtime's execution trace of the last few
// seconds, held in memory (internal/tracerec: runtime/trace's
// FlightRecorder on Go 1.25 and newer, golang.org/x/exp/trace's before)
// and written next to the freeze reports when a task on the UI goroutine
// (a frame, posted work, a window event; watchdog.go) takes slowTrace or
// more. A freeze report's stacks say where every goroutine was at one
// instant; the trace says what happened over the seconds before: which
// goroutines ran on which thread and which sat runnable, each GC cycle
// and stop-the-world and how long it waited for the goroutines to stop,
// mark assists, syscalls, blocking. A 100-270 ms frame that a GC cycle
// finishing during it made wait, with the UI goroutine runnable but not
// running, shows as just that.
//
// Files, in <config dir>/freeze-reports beside the reports:
//   - slow-<time>.trace: a task took slowTrace or more; at most one per
//     traceGap, the gap doubling up to traceGapMax while slow tasks keep
//     coming (slowPacer)
//   - freeze-<time>.trace: with each freeze report (same name; the
//     report's last lines name it), as the report is written, so it holds
//     the seconds before the freeze and its first two
//
// Each kind has its own budget, so slow-frame traces never push out a
// freeze's: traceKeepAge, and the newest that fit in slowKeepBytes and
// freezeTraceKeepBytes (the one just written always stays). A freeze trace
// goes with its report. A trace holds function names, file lines and the
// runtime's own events: no strings from Clack's data. Read one with `go
// tool trace <file>` (the Go that built Clack, or newer; README "If Clack
// freezes").
//
// It is on: perf.yml's plugins job measures what it costs
// (PERF_PLUGIN_RECORDER: channel-live passes with it off and on, in turns,
// on one runner), and in run 43866883085 that was nothing the passes could
// tell from noise: 9 passes each way, process CPU -0.1% and the sum of
// the frames -0.6% (one pass to the next varies by ±2.5%), the longest
// frame and p99 no higher, allocations -0.9%; its 5 s window was 0.7 MB,
// written in 7 ms. CLACK_FLIGHT_RECORDER=0 or ui.json's
// "flight_recorder": false turns it off (flightRecorderWanted).

const (
	slowTrace            = 100 * time.Millisecond // a task this long writes a trace
	tracePeriod          = 5 * time.Second        // the window the recorder holds
	traceSize            = 16 << 20               // the recorder's target size
	traceGap             = time.Minute            // between two slow-task traces, at first
	traceGapMax          = time.Hour              // … and at most
	traceQuiet           = 10 * time.Minute       // no slow task this long: the gap starts over
	traceKeepAge         = freezeKeepAge
	slowKeepBytes        = 24 << 20
	freezeTraceKeepBytes = 40 << 20
	// stopWait bounds how long quitting waits for the recorder (a trace
	// being written, then the tracer stopping): a tracer that never
	// returns (tracerec's package comment) must not keep the window's
	// DestroyEvent, and so the app, from finishing.
	stopWait = time.Second
)

// newTraceWindow makes the recorder's tracer (tests swap it).
var newTraceWindow = tracerec.New

// flightRecorderDefault is whether the recorder runs without being asked.
const flightRecorderDefault = true

// flightRecorderWanted: CLACK_FLIGHT_RECORDER=0 (or off, false, no) turns
// the recorder off and any other value on; unset, ui.json's
// "flight_recorder" decides, then flightRecorderDefault.
func flightRecorderWanted(pref *bool) bool {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv("CLACK_FLIGHT_RECORDER"))); v {
	case "":
		if pref != nil {
			return *pref
		}
		return flightRecorderDefault
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}

// flightRecorder keeps the trace and writes it on request, one file at a
// time, on its own goroutine: a write waits for the tracer to flush (it
// stops each P at a safe point, the plugin JS thread's included), which
// the watchdog's goroutine must never wait for.
type flightRecorder struct {
	win       tracerec.Recorder
	dir       string
	gap       time.Duration // traceGap
	keepAge   time.Duration // traceKeepAge
	slowMax   int64         // slowKeepBytes
	freezeMax int64         // freezeTraceKeepBytes
	reqs      chan traceReq
	quit      chan struct{}
	done      chan struct{}
	stopped   sync.Once

	// written is called with each trace's path (tests).
	written func(path string)
}

// traceReq asks for one trace: a slow task's (paced) or a freeze report's
// (always; report is its path).
type traceReq struct {
	slow   time.Duration // how long the task took (slow-task traces)
	stage  string
	at     time.Time // when it ended
	report string    // the freeze report this trace goes with
}

// startFlightRecorder starts the tracer and the goroutine that writes
// traces into dir, slow-task ones at least gap apart. Only one recorder
// may run in a process: a second fails.
func startFlightRecorder(dir string, gap time.Duration) (*flightRecorder, error) {
	win := newTraceWindow(tracePeriod, traceSize)
	if err := win.Start(); err != nil {
		return nil, err
	}
	r := &flightRecorder{win: win, dir: dir, gap: gap, keepAge: traceKeepAge, slowMax: slowKeepBytes, freezeMax: freezeTraceKeepBytes,
		reqs: make(chan traceReq, 4), quit: make(chan struct{}), done: make(chan struct{})}
	go r.run()
	return r, nil
}

// slowTask asks for a trace of a task that took d (UI goroutine: never
// blocks, allocates nothing; a request while the queue is full is
// dropped, as one the pacer turns down would be).
func (r *flightRecorder) slowTask(d time.Duration, stage string) {
	if r == nil {
		return
	}
	select {
	case r.reqs <- traceReq{slow: d, stage: stage, at: time.Now()}:
	default:
	}
}

// freeze asks for a trace to go with the freeze report at path (the
// watchdog's goroutine).
func (r *flightRecorder) freeze(report string) {
	if r == nil {
		return
	}
	select {
	case r.reqs <- traceReq{report: report, at: time.Now()}:
	default:
	}
}

// stop ends the recorder: a trace being written is finished first, then
// the tracer stops, for stopWait at most. Past it, stop returns and leaves
// them to it (the app is quitting).
func (r *flightRecorder) stop() {
	if r == nil {
		return
	}
	r.stopped.Do(func() {
		close(r.quit)
		stopped := make(chan struct{})
		go func() {
			<-r.done
			r.win.Stop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(stopWait):
			log.Printf("ui: the execution trace flight recorder didn't stop within %v (a trace being written, or the tracer, is stuck); going on without it", stopWait)
		}
	})
}

func (r *flightRecorder) run() {
	defer close(r.done)
	pace := slowPacer{base: r.gap, max: max(traceGapMax, r.gap), quiet: traceQuiet}
	for {
		var q traceReq
		select {
		case q = <-r.reqs:
		case <-r.quit:
			return
		}
		var name string
		if q.report != "" {
			name = strings.TrimSuffix(filepath.Base(q.report), ".txt") + ".trace"
		} else {
			if !pace.allow(q.at) {
				continue
			}
			name = "slow-" + q.at.Format("20060102-150405.000") + ".trace"
		}
		path := filepath.Join(r.dir, name)
		t0 := time.Now()
		n, err := r.write(path)
		if err != nil {
			log.Printf("ui: the execution trace couldn't be written to %s: %v", path, err)
			continue
		}
		took := time.Since(t0).Round(time.Millisecond)
		if q.report != "" {
			appendFile(q.report, fmt.Sprintf("\nExecution trace of the %v up to this report (the freeze's start included): %s (%.1f MB; go tool trace %s)\n", tracePeriod, name, float64(n)/1e6, name))
			log.Printf("ui: the freeze report's execution trace: %s (%.1f MB, written in %v)", path, float64(n)/1e6, took)
		} else {
			log.Printf("ui: a %v task (%s); execution trace of the %v before its end: %s (%.1f MB, written in %v; the next in %v at the soonest)", q.slow.Round(time.Millisecond), q.stage, tracePeriod, path, float64(n)/1e6, took, pace.gap)
		}
		r.prune(name)
		if r.written != nil {
			r.written(path)
		}
	}
}

// write snapshots the recorder's window into path.
func (r *flightRecorder) write(path string) (int64, error) {
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	bw := bufio.NewWriterSize(f, 256<<10)
	n, err := r.win.WriteTo(bw)
	if err == nil {
		err = bw.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return 0, err
	}
	return n, nil
}

// isSlowTrace and isFreezeTrace tell the two kinds of trace apart by name.
func isSlowTrace(n string) bool {
	return strings.HasPrefix(n, "slow-") && strings.HasSuffix(n, ".trace")
}

func isFreezeTrace(n string) bool {
	return strings.HasPrefix(n, "freeze-") && strings.HasSuffix(n, ".trace")
}

// prune keeps each kind of trace within its own budget, so a run of
// slow-frame traces never pushes out a freeze's, and drops freeze traces
// whose report is gone; keep (the trace just written) always stays.
func (r *flightRecorder) prune(keep string) {
	pruneFiles(r.dir, keep, isSlowTrace, r.keepAge, r.slowMax)
	pruneFiles(r.dir, keep, isFreezeTrace, r.keepAge, r.freezeMax)
	pruneOrphanTraces(r.dir, keep)
}

// pruneOrphanTraces deletes the freeze traces in dir whose report
// (freeze-<time>.txt) the reports' pruning deleted; keep stays.
func pruneOrphanTraces(dir, keep string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	have := map[string]bool{}
	for _, e := range ents {
		have[e.Name()] = true
	}
	keep = filepath.Base(keep)
	for _, e := range ents {
		n := e.Name()
		if isFreezeTrace(n) && n != keep && !have[strings.TrimSuffix(n, ".trace")+".txt"] {
			_ = os.Remove(filepath.Join(dir, n))
		}
	}
}

// slowPacer spaces slow-task traces: one per gap, starting at base and
// doubling after each trace up to max while slow tasks keep coming (a
// session with one every minute writes a trace after 1, 2, 4, … minutes,
// then one an hour), and starting over once none has come for quiet.
// run's goroutine only.
type slowPacer struct {
	base, max, quiet time.Duration
	gap              time.Duration // the current gap (0: base)
	last             time.Time     // the last trace written (zero: none in this run of slow tasks)
	seen             time.Time     // the last slow task, written or not
}

// allow reports whether the slow task that ended at at gets a trace.
func (p *slowPacer) allow(at time.Time) bool {
	if p.gap == 0 || !p.seen.IsZero() && at.Sub(p.seen) >= p.quiet {
		p.gap, p.last = p.base, time.Time{}
	}
	p.seen = at
	switch {
	case p.last.IsZero():
	case at.Sub(p.last) < p.gap:
		return false
	default:
		p.gap = min(2*p.gap, p.max)
	}
	p.last = at
	return true
}
