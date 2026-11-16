package ui

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/awake"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/openurl"
)

// The frame watchdog: field diagnosis for freezes.
//
// Run marks when it starts handling a window event (a frame, the posted
// work drained before it, the GPU submit) and when it's done. If one of
// those takes longer than freezeAfter, a goroutine writes every
// goroutine's stack to <config dir>/freeze-reports/freeze-<time>.txt and
// the app keeps running; at freezeSecond it adds a second snapshot, and
// when the stuck work finishes, the report gets how long it took. Each
// snapshot has the Go runtime's stop-the-world figures and what every
// plugin JS runtime is doing: Go can't stop a goroutine inside wasm code,
// so a stop-the-world (each GC cycle, runtime.Stack) waits for the JS
// thread (internal/jsrt/diag.go). The stacks hold function names, file
// lines and argument words only (no strings, so no message text); the
// whole report goes through slack.Redact anyway, so no token or cookie is
// written. README "If Clack freezes" says where to find the file.

// freezeAfter is how long one frame or UI-goroutine task may run before a
// report is written.
const freezeAfter = 2 * time.Second

// freezeSecond: a task still stuck this long after it began gets a second
// snapshot in its report.
const freezeSecond = 5 * time.Second

// Reports are kept for freezeKeepAge, and only the newest that fit in
// freezeKeepBytes (the one just written always stays).
const (
	freezeKeepAge   = 7 * 24 * time.Hour
	freezeKeepBytes = 20 << 20
)

type watchdog struct {
	dir     string // freeze-reports directory ("" = off)
	ui      string // ui.json, copied into the report
	started time.Time
	after   time.Duration // freezeAfter (shorter in tests)
	second  time.Duration // freezeSecond
	keepAge time.Duration // freezeKeepAge
	keepMax int64         // freezeKeepBytes

	// busy is when the current task began on the awake clock
	// (internal/awake: ns, never 0); 0 = idle. It stands still while the
	// computer sleeps, on Linux, macOS and Windows, so a frame that
	// straddles a lid-close isn't a minutes-long freeze.
	busy  atomic.Int64
	epoch atomic.Uint64
	stage atomic.Pointer[string]
	page  atomic.Pointer[string] // the route's page at the last frame

	// rec is the execution-trace flight recorder (flightrec.go; nil =
	// off, or not started yet). With it on, begin, next and end also time
	// each stage (UI goroutine only, so no atomics), and a task of
	// slowTrace or more asks it for a trace naming the stage that took
	// longest. recMu orders its start (on a goroutine of its own) against
	// stop.
	rec        atomic.Pointer[flightRecorder]
	recMu      sync.Mutex
	recStopped bool          // stop ran (recMu)
	traceGap   time.Duration // between two slow-task traces (traceGap; shorter in tests)
	timing     bool          // this task's stages are timed (the recorder was on at begin)
	stageAt    int64         // when the current stage began (now's clock)
	longest    time.Duration
	longStage  *string

	// written is called with each report's path (tests).
	written func(path string)
	quit    chan struct{}
}

func newWatchdog(configDir string, started time.Time) *watchdog {
	w := &watchdog{started: started, after: freezeAfter, second: freezeSecond,
		keepAge: freezeKeepAge, keepMax: freezeKeepBytes, traceGap: traceGap, quit: make(chan struct{})}
	if configDir != "" {
		w.dir = filepath.Join(configDir, "freeze-reports")
		w.ui = filepath.Join(configDir, "ui.json")
	}
	return w
}

var (
	stageEvent  = "window event"
	stagePosted = "posted UI work (App.Post)"
	stageLayout = "frame layout"
	stageSubmit = "frame submit (GPU, main thread)"
	stageShaper = "first frame"
)

// now is busy's clock: awake time in ns, never 0.
func (w *watchdog) now() int64 { return awake.Now() }

// startRecorder starts the flight recorder when it's wanted
// (CLACK_FLIGHT_RECORDER, ui.json's flight_recorder; flightrec.go) and
// reports are on, on a goroutine of its own: starting the tracer stops the
// world, which waits for every goroutine (a JS thread in wasm included),
// and the UI goroutine never waits. The channel closes once it's on (or
// didn't start).
func (w *watchdog) startRecorder(pref *bool) <-chan struct{} {
	done := make(chan struct{})
	if w == nil || w.dir == "" || !flightRecorderWanted(pref) {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		rec, err := startFlightRecorder(w.dir, w.traceGap)
		if err != nil {
			log.Printf("ui: the execution trace flight recorder didn't start: %v", err)
			return
		}
		w.recMu.Lock()
		if w.recStopped {
			w.recMu.Unlock()
			rec.stop()
			return
		}
		w.rec.Store(rec)
		w.recMu.Unlock()
		log.Printf("ui: the execution trace flight recorder is on: a task of %v or more writes the last %v to %s", slowTrace, tracePeriod, w.dir)
	}()
	return done
}

// begin starts timing a task on the UI goroutine. Cheap: atomics only.
func (w *watchdog) begin(stage *string) {
	if w == nil {
		return
	}
	w.stage.Store(stage)
	w.epoch.Add(1)
	now := w.now()
	w.busy.Store(now)
	if w.timing = w.rec.Load() != nil; w.timing {
		w.stageAt, w.longest, w.longStage = now, 0, stage
	}
}

// next moves the running task to another stage (same task, same clock).
func (w *watchdog) next(stage *string) {
	if w == nil {
		return
	}
	if w.timing {
		w.endStage(w.now())
	}
	w.stage.Store(stage)
}

// endStage counts the stage ending at now toward the task's longest (UI
// goroutine, w.timing).
func (w *watchdog) endStage(now int64) {
	if d := time.Duration(now - w.stageAt); d > w.longest {
		w.longest, w.longStage = d, w.stage.Load()
	}
	w.stageAt = now
}

func (w *watchdog) end() {
	if w == nil {
		return
	}
	if rec := w.rec.Load(); rec != nil {
		if since := w.busy.Load(); since != 0 {
			now := w.now()
			stage := w.stage.Load() // the recorder came on during the task: its last stage
			if w.timing {
				w.endStage(now)
				stage = w.longStage
			}
			if d := time.Duration(now - since); d >= slowTrace {
				name := "?"
				if stage != nil {
					name = *stage
				}
				rec.slowTask(d, name)
			}
		}
	}
	w.timing = false
	w.busy.Store(0)
}

// notePage records the page shown (UI goroutine; stores only on change).
func (w *watchdog) notePage(p string) {
	if w == nil {
		return
	}
	if cur := w.page.Load(); cur == nil || *cur != p {
		w.page.Store(&p)
	}
}

// showFreezeReports is Help > Troubleshooting > Show Freeze Reports: the folder in Finder (or
// a toast when there are none). Any goroutine.
func (a *App) showFreezeReports() {
	if a.opts.ConfigDir == "" {
		return
	}
	dir := filepath.Join(a.opts.ConfigDir, "freeze-reports")
	if ents, err := os.ReadDir(dir); err != nil || len(ents) == 0 {
		a.Post(func() { a.Toast("No freeze reports: Clack hasn't frozen on this computer") })
		return
	}
	_ = openurl.OpenFolder(dir)
}

// run is the watchdog goroutine.
func (w *watchdog) run() {
	if w == nil || w.dir == "" {
		return
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var (
		open      string // report of the task still running
		openEpoch uint64
		openSince int64 // its busy value
		seconded  bool  // it has its second snapshot
	)
	for {
		select {
		case <-tick.C:
		case <-w.quit:
			return
		}
		since := w.busy.Load()
		ep := w.epoch.Load()
		if open != "" && (since == 0 || ep != openEpoch) {
			d := time.Duration(w.now() - openSince).Round(100 * time.Millisecond)
			appendFile(open, fmt.Sprintf("\nThe window was responsive again after about %v.\n", d))
			log.Printf("ui: the stuck UI work finished after about %v", d)
			open = ""
		}
		if open != "" && !seconded && time.Duration(w.now()-openSince) >= w.second {
			seconded = true
			title := fmt.Sprintf("Still blocked after %v, in %s: a second snapshot", time.Duration(w.now()-openSince).Round(100*time.Millisecond), w.stageName())
			appendFile(open, slack.Redact(w.snapshot(title)))
			log.Printf("ui: the window is still blocked (%s); a second snapshot went into %s", w.stageName(), open)
			continue
		}
		if since == 0 || open != "" || ep == openEpoch {
			continue
		}
		if time.Duration(w.now()-since) < w.after {
			continue
		}
		path, err := w.report(since)
		openEpoch = ep // one report per stuck task
		if err != nil {
			log.Printf("ui: the window is blocked (%s) but the freeze report couldn't be written: %v", w.stageName(), err)
			continue
		}
		open, openSince, seconded = path, since, false
		log.Printf("ui: the window has been blocked for %v (%s); freeze report: %s", time.Duration(w.now()-since).Round(100*time.Millisecond), w.stageName(), path)
		if w.written != nil {
			w.written(path)
		}
	}
}

// stop ends run, and the flight recorder (once a trace being written is
// done).
func (w *watchdog) stop() {
	if w == nil {
		return
	}
	close(w.quit)
	w.recMu.Lock()
	w.recStopped = true
	rec := w.rec.Swap(nil)
	w.recMu.Unlock()
	rec.stop()
}

func (w *watchdog) stageName() string {
	if s := w.stage.Load(); s != nil {
		return *s
	}
	return "?"
}

// report writes one freeze report for the task that began at since (busy's
// clock) and returns its path.
func (w *watchdog) report(since int64) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Clack freeze report\n\n")
	fmt.Fprintf(&b, "version: %s\n", buildVersion)
	fmt.Fprintf(&b, "os: %s/%s, %s, %d CPUs\n", runtime.GOOS, runtime.GOARCH, runtime.Version(), runtime.NumCPU())
	fmt.Fprintf(&b, "written: %s (%v after start)\n", time.Now().Format(time.RFC3339), time.Since(w.started).Round(time.Second))
	fmt.Fprintf(&b, "blocked: %v so far, in %s\n", time.Duration(w.now()-since).Round(10*time.Millisecond), w.stageName())
	if p := w.page.Load(); p != nil {
		fmt.Fprintf(&b, "page: %s\n", *p)
	}
	if ui, err := os.ReadFile(w.ui); err == nil && len(ui) < 64<<10 {
		fmt.Fprintf(&b, "\nui.json:\n%s\n", strings.TrimSpace(string(ui)))
	}
	b.WriteString(w.snapshot(""))
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(w.dir, "freeze-"+time.Now().Format("20060102-150405.000")+".txt")
	if err := os.WriteFile(path, []byte(slack.Redact(b.String())), 0o600); err != nil {
		return "", err
	}
	w.prune(path)
	w.rec.Load().freeze(path) // the trace of the seconds before, beside it (flightrec.go)
	return path, nil
}

// snapshot is the runtime figures, the JS runtimes and every goroutine's
// stack, as of now, under an optional title.
func (w *watchdog) snapshot(title string) string {
	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, "\n%s\n", title)
	}
	b.WriteString(runtimeFigures())
	b.WriteString(jsFigures())
	asked := time.Now()
	stacks := allStacks()
	got := time.Now()
	// runtime.Stack stops the world first: a long wait here is something
	// that wouldn't stop (a JS thread in a native loop, say)
	fmt.Fprintf(&b, "\nstacks: asked for at %s, got at %s (%v)\n", asked.Format("15:04:05.000"), got.Format("15:04:05.000"), got.Sub(asked).Round(time.Millisecond))
	fmt.Fprintf(&b, "\ngoroutines (%d):\n\n%s\n", runtime.NumGoroutine(), stacks)
	return b.String()
}

// freezeMetrics are the runtime/metrics a snapshot prints (reading them
// doesn't stop the world, unlike runtime.ReadMemStats).
var freezeMetrics = []string{
	"/sched/pauses/stopping/gc:seconds",
	"/sched/pauses/stopping/other:seconds",
	"/sched/pauses/total/gc:seconds",
	"/sched/pauses/total/other:seconds",
	"/gc/heap/live:bytes",
	"/gc/heap/goal:bytes",
	"/memory/classes/total:bytes",
	"/gc/cycles/total:gc-cycles",
	"/gc/gomemlimit:bytes",
	"/cpu/classes/gc/total:cpu-seconds",
	"/cpu/classes/total:cpu-seconds",
}

// runtimeFigures is the Go heap and the stop-the-world histograms' worst
// cases since start. A stop-the-world first waits for every goroutine to
// stop ("stopping"); one that can't be stopped holds the rest paused.
func runtimeFigures() string {
	s := make([]metrics.Sample, len(freezeMetrics))
	for i, n := range freezeMetrics {
		s[i].Name = n
	}
	metrics.Read(s)
	u := func(i int) uint64 {
		if s[i].Value.Kind() != metrics.KindUint64 {
			return 0
		}
		return s[i].Value.Uint64()
	}
	f := func(i int) float64 {
		if s[i].Value.Kind() != metrics.KindFloat64 {
			return 0
		}
		return s[i].Value.Float64()
	}
	mib := func(v uint64) string { return fmt.Sprintf("%.1f MiB", float64(v)/(1<<20)) }
	// the soft limit and the collector's share of the CPU: near the limit
	// it runs nearly nonstop, and the UI goroutine's allocations pay for
	// it in assists (every part of a frame slows down at once)
	limit := "no limit"
	if l := u(8); l > 0 && l < math.MaxInt64 {
		limit = "limit " + mib(l)
	}
	gcShare := 0.0
	if all := f(10); all > 0 {
		gcShare = 100 * f(9) / all
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nGo runtime: heap %s live (goal %s, %s), %s mapped in all; %d collections, %.1f%% of CPU in GC since start\n", mib(u(4)), mib(u(5)), limit, mib(u(6)), u(7), gcShare)
	fmt.Fprintf(&b, "stop-the-world since start (longest; how many took 100ms or more):\n")
	for i, what := range []string{"waiting for every goroutine to stop, for a GC", "waiting for every goroutine to stop, for anything else (runtime.Stack, ReadMemStats)", "paused in all, for a GC", "paused in all, for anything else"} {
		fmt.Fprintf(&b, "  %s: %s\n", what, histWorst(s[i].Value))
	}
	return b.String()
}

// histWorst describes a time histogram's highest non-empty bucket and how
// many samples were 100ms or more.
func histWorst(v metrics.Value) string {
	if v.Kind() != metrics.KindFloat64Histogram {
		return "not measured"
	}
	h := v.Float64Histogram()
	var slow uint64
	worst := -1
	for i, c := range h.Counts {
		if c == 0 {
			continue
		}
		worst = i
		if h.Buckets[i] >= 0.1 {
			slow += c
		}
	}
	if worst < 0 {
		return "none"
	}
	lo, hi := math.Max(h.Buckets[worst], 0), h.Buckets[worst+1]
	sec := func(f float64) string {
		return time.Duration(f * float64(time.Second)).Round(time.Microsecond).String()
	}
	if math.IsInf(hi, 1) {
		return fmt.Sprintf("over %s; %d", sec(lo), slow)
	}
	return fmt.Sprintf("%s–%s; %d", sec(lo), sec(hi), slow)
}

// jsFigures is what each plugin JS runtime is doing.
func jsFigures() string {
	ss := jsrt.Snapshots()
	if len(ss) == 0 {
		return "\nJS runtimes: none\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nJS runtimes (QuickJS on wazero; a stop-the-world waits for a JS thread to leave wasm code):\n")
	for i, s := range ss {
		fmt.Fprintf(&b, "  %d: %s\n", i+1, s)
	}
	return b.String()
}

// allStacks is runtime.Stack for every goroutine (grows the buffer up to
// 16 MiB).
func allStacks() []byte {
	buf := make([]byte, 256<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) || len(buf) >= 16<<20 {
			return buf[:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}

// prune deletes reports older than keepAge, then the oldest until the rest
// fit in keepMax bytes, and the execution traces of the reports it
// deleted (flightrec.go); keep (the report just written) always stays.
func (w *watchdog) prune(keep string) {
	pruneFiles(w.dir, keep, func(n string) bool { return strings.HasPrefix(n, "freeze-") && strings.HasSuffix(n, ".txt") }, w.keepAge, w.keepMax)
	pruneOrphanTraces(w.dir, keep)
}

// pruneFiles deletes the files in dir that match and are older than
// keepAge, then the oldest until the rest fit in keepMax bytes; keep (the
// file just written) always stays. Files rank by modification time, not
// by name: a name is local wall time, which a DST fall-back or a westward
// zone change turns back, so the newest file could sort below older ones
// and go first.
func pruneFiles(dir, keep string, match func(name string) bool, keepAge time.Duration, keepMax int64) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type rep struct {
		name string
		size int64
		mod  time.Time
	}
	var reps []rep
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !match(n) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		reps = append(reps, rep{n, fi.Size(), fi.ModTime()})
	}
	// newest first; a clock set back can still date keep before older
	// reports, which is why keep is exempt rather than first
	sort.Slice(reps, func(i, j int) bool {
		if !reps[i].mod.Equal(reps[j].mod) {
			return reps[i].mod.After(reps[j].mod)
		}
		return reps[i].name > reps[j].name
	})
	keep = filepath.Base(keep)
	var total int64
	for _, r := range reps {
		total += r.size
		if r.name != keep && (time.Since(r.mod) > keepAge || total > keepMax) {
			_ = os.Remove(filepath.Join(dir, r.name))
		}
	}
}

func appendFile(path, s string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(s)
	_ = f.Close()
}
