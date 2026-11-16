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

	xtrace "golang.org/x/exp/trace"
)

// The flight recorder: the Go runtime's execution trace of the last few
// seconds, held in memory (golang.org/x/exp/trace's FlightRecorder; Go
// 1.25 has it as runtime/trace.FlightRecorder) and written next to the
// freeze reports when a task on the UI goroutine (a frame, posted work, a
// window event; watchdog.go) takes slowTrace or more. A freeze report's
// stacks say where every goroutine was at one instant; the trace says what
// happened over the seconds before: which goroutines ran on which thread
// and which sat runnable, each GC cycle and stop-the-world and how long it
// waited for the goroutines to stop, mark assists, syscalls, blocking. A
// 100-270 ms frame that a GC cycle finishing during it made wait, with
// the UI goroutine runnable but not running, shows as just that.
//
// Files, in <config dir>/freeze-reports beside the reports:
//   - slow-<time>.trace: a task took slowTrace or more; at most one per
//     traceGap
//   - freeze-<time>.trace: with each freeze report (same name; the
//     report's last lines name it), as the report is written, so it holds
//     the seconds before the freeze and its first two
//
// They are kept for traceKeepAge, and only the newest that fit in
// traceKeepBytes (the one just written always stays). A trace holds
// function names, file lines and the runtime's own events: no strings
// from Clack's data. Read one with `go tool trace <file>` (Go 1.24 or
// newer; README "If Clack freezes").
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
	slowTrace      = 100 * time.Millisecond // a task this long writes a trace
	tracePeriod    = 5 * time.Second        // the window the recorder holds
	traceSize      = 16 << 20               // the recorder's target size
	traceGap       = time.Minute            // between two slow-task traces
	traceKeepAge   = freezeKeepAge
	traceKeepBytes = 64 << 20
)

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
	fr      *xtrace.FlightRecorder
	dir     string
	gap     time.Duration // traceGap
	keepAge time.Duration // traceKeepAge
	keepMax int64         // traceKeepBytes
	reqs    chan traceReq
	quit    chan struct{}
	done    chan struct{}
	stopped sync.Once

	// written is called with each trace's path (tests).
	written func(path string)
}

// traceReq asks for one trace: a slow task's (rate-limited) or a freeze
// report's (always; report is its path).
type traceReq struct {
	slow   time.Duration // how long the task took (slow-task traces)
	stage  string
	at     time.Time // when it ended
	report string    // the freeze report this trace goes with
}

// startFlightRecorder starts the tracer and the goroutine that writes
// traces into dir, slow-task ones at least gap apart. Only one recorder
// (or runtime/trace.Start) may run in a process: a second fails.
func startFlightRecorder(dir string, gap time.Duration) (*flightRecorder, error) {
	fr := xtrace.NewFlightRecorder()
	fr.SetPeriod(tracePeriod)
	fr.SetSize(traceSize)
	if err := fr.Start(); err != nil {
		return nil, err
	}
	r := &flightRecorder{fr: fr, dir: dir, gap: gap, keepAge: traceKeepAge, keepMax: traceKeepBytes,
		reqs: make(chan traceReq, 4), quit: make(chan struct{}), done: make(chan struct{})}
	go r.run()
	return r, nil
}

// slowTask asks for a trace of a task that took d (UI goroutine: never
// blocks, allocates nothing; a request while the queue is full is
// dropped, as one inside traceGap would be).
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

// stop ends the recorder (the tracer stops; a trace being written is
// finished first).
func (r *flightRecorder) stop() {
	if r == nil {
		return
	}
	r.stopped.Do(func() {
		close(r.quit)
		<-r.done
		_ = r.fr.Stop()
	})
}

func (r *flightRecorder) run() {
	defer close(r.done)
	var lastSlow time.Time
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
			if !lastSlow.IsZero() && q.at.Sub(lastSlow) < r.gap {
				continue
			}
			lastSlow = q.at
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
			log.Printf("ui: a %v task (%s); execution trace of the %v before its end: %s (%.1f MB, written in %v)", q.slow.Round(time.Millisecond), q.stage, tracePeriod, path, float64(n)/1e6, took)
		}
		r.prune(name)
		if r.written != nil {
			r.written(path)
		}
	}
}

// write snapshots the recorder's window into path.
func (r *flightRecorder) write(path string) (int, error) {
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	bw := bufio.NewWriterSize(f, 256<<10)
	n, err := r.fr.WriteTo(bw)
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

// prune keeps the traces within traceKeepAge and traceKeepBytes; keep
// (the trace just written) always stays.
func (r *flightRecorder) prune(keep string) {
	pruneFiles(r.dir, keep, func(name string) bool { return strings.HasSuffix(name, ".trace") }, r.keepAge, r.keepMax)
}
