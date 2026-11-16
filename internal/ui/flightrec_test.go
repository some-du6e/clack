package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	xtrace "golang.org/x/exp/trace"

	"github.com/some-du6e/clack/internal/tracerec"
)

// traceStats parses a trace the flight recorder wrote, as go tool trace
// would, and counts what's in it.
type traceStats struct {
	events      int
	transitions int  // goroutine and proc state changes
	stw         int  // stop-the-world ranges
	gc          int  // GC mark phases
	unread      bool // a newer trace format than x/exp's reader reads (Go 1.25 and newer)
}

func readTrace(t *testing.T, path string) traceStats {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var minor int
	if len(b) < 16 {
		t.Fatalf("%s has %d bytes: not a trace", path, len(b))
	}
	if _, err := fmt.Sscanf(string(bytes.TrimRight(b[:16], "\x00")), "go 1.%d trace", &minor); err != nil {
		t.Fatalf("%s starts %q: not a trace", path, b[:16])
	}
	if minor > 23 {
		t.Logf("flightrec: %s is a go 1.%d trace, newer than x/exp/trace's reader reads: not parsed", filepath.Base(path), minor)
		return traceStats{unread: true}
	}
	rd, err := xtrace.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("%s isn't a trace: %v", path, err)
	}
	var st traceStats
	for {
		ev, err := rd.ReadEvent()
		if err == io.EOF {
			return st
		}
		if err != nil {
			t.Fatalf("%s: event %d: %v", path, st.events, err)
		}
		st.events++
		switch ev.Kind() {
		case xtrace.EventStateTransition:
			st.transitions++
		case xtrace.EventRangeBegin, xtrace.EventRangeActive:
			switch n := ev.Range().Name; {
			case strings.HasPrefix(n, "stop-the-world"):
				st.stw++
			case n == "GC concurrent mark phase":
				st.gc++
			}
		}
	}
}

// recWatchdog is a watchdog writing into a temporary config directory
// with the flight recorder on; traces receives each trace's path.
func recWatchdog(t *testing.T, gap time.Duration) (wd *watchdog, traces chan string) {
	t.Helper()
	wd = newWatchdog(t.TempDir(), time.Now())
	wd.traceGap = gap
	on := true
	<-wd.startRecorder(&on)
	rec := wd.rec.Load()
	if rec == nil {
		t.Fatal("the flight recorder didn't start")
	}
	traces = make(chan string, 8)
	rec.written = func(p string) { traces <- p }
	t.Cleanup(wd.stop)
	return wd, traces
}

func waitTrace(t *testing.T, traces chan string, what string) string {
	t.Helper()
	select {
	case p := <-traces:
		return p
	case <-time.After(30 * time.Second):
		t.Fatalf("no trace for %s", what)
		return ""
	}
}

// A UI-goroutine task of slowTrace or more writes the recorder's window
// next to the freeze reports, as a trace go tool trace reads (parsed here
// with the same reader); a collection during the task is in it, with its
// stop-the-world. Another slow task inside traceGap writes nothing; one
// after it does. A fast task never does.
func TestFlightRecorderSlowTask(t *testing.T) {
	wd, traces := recWatchdog(t, 3*time.Second)
	slow := func() {
		wd.begin(&stageEvent)
		wd.next(&stageLayout)
		runtime.GC() // a collection while the frame waits: what the trace is for
		time.Sleep(slowTrace + 30*time.Millisecond)
		wd.next(&stageSubmit)
		wd.end()
	}
	wd.begin(&stageLayout)
	wd.end() // fast: nothing
	slow()
	first := time.Now()
	p := waitTrace(t, traces, "a slow task")
	slow() // inside the gap
	select {
	case p := <-traces:
		t.Fatalf("a second trace inside traceGap: %s", p)
	case <-time.After(500 * time.Millisecond):
	}
	if time.Since(first) > 2*time.Second {
		t.Logf("flightrec: the first trace took %v to write: the gap check above may have come too late", time.Since(first))
	}
	if dir, name := filepath.Split(p); filepath.Clean(dir) != wd.dir || !strings.HasPrefix(name, "slow-") || !strings.HasSuffix(name, ".trace") {
		t.Errorf("trace written to %s, want %s/slow-<time>.trace", p, wd.dir)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	st := readTrace(t, p)
	t.Logf("flightrec: %s (%s): %.1f MB, %d events, %d state transitions, %d stop-the-world ranges, %d GC mark phases", filepath.Base(p), tracerec.Impl, float64(fi.Size())/1e6, st.events, st.transitions, st.stw, st.gc)
	if !st.unread && (st.transitions == 0 || st.stw == 0 || st.gc == 0) {
		t.Errorf("the trace lacks goroutine transitions, the collection or its stop-the-world: %+v", st)
	}
	time.Sleep(time.Until(first.Add(3*time.Second + 100*time.Millisecond))) // past the gap
	slow()
	if p2 := waitTrace(t, traces, "a slow task after the gap"); p2 == p {
		t.Errorf("the second trace overwrote the first: %s", p2)
	}
}

// A freeze report gets a trace of the seconds before it, written as the
// report is (so it holds the freeze's start), named like the report and
// named in it.
func TestFlightRecorderFreezeReport(t *testing.T) {
	wd, traces := recWatchdog(t, time.Hour)
	wd.after, wd.second = 300*time.Millisecond, time.Hour
	reports := make(chan string, 2)
	wd.written = func(p string) { reports <- p }
	go wd.run()
	wd.begin(&stageLayout)
	var report string
	select {
	case report = <-reports:
	case <-time.After(10 * time.Second):
		t.Fatal("no freeze report")
	}
	p := waitTrace(t, traces, "the freeze report")
	if want := strings.TrimSuffix(report, ".txt") + ".trace"; p != want {
		t.Errorf("the report's trace is %s, want %s", p, want)
	}
	if st := readTrace(t, p); !st.unread && st.transitions == 0 {
		t.Errorf("the freeze trace has no goroutine transitions: %+v", st)
	}
	b, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if name := filepath.Base(p); !strings.Contains(string(b), "Execution trace of the 5s up to this report") || !strings.Contains(string(b), "go tool trace "+name) {
		t.Errorf("the report doesn't name its trace %s:\n%.2000s", name, b)
	}
	wd.end()
}

// Each kind of trace has its own budget: slow-frame traces never push out
// a freeze's, however many come after it. Freeze traces go by age and
// their own budget too, and with their report: one whose report the
// reports' pruning deleted goes (at the next trace or report). Reports and
// other files are left alone.
func TestFlightRecorderPrune(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string, size int, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	names := func() string {
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range ents {
			got = append(got, e.Name())
		}
		return strings.Join(got, " ")
	}
	mk("slow-20261101-100000.000.trace", 100, 8*24*time.Hour) // too old
	mk("slow-20261116-100000.000.trace", 6<<10, 3*time.Hour)  // past the slow budget beside the two newer
	mk("slow-20261116-120000.000.trace", 3<<10, time.Hour)
	mk("slow-20261116-130000.000.trace", 3<<10, 0) // just written
	// freeze traces, older than every slow one: a shared budget would have
	// let the slow ones push these out
	mk("freeze-20261116-110000.000.trace", 4<<10, 2*time.Hour)
	mk("freeze-20261116-110000.000.txt", 50<<10, 2*time.Hour)
	mk("freeze-20261116-090000.000.trace", 4<<10, 4*time.Hour)
	mk("freeze-20261116-090000.000.txt", 50<<10, 4*time.Hour)
	mk("freeze-20261116-080000.000.trace", 4<<10, 5*time.Hour) // past the freeze budget beside the two newer
	mk("freeze-20261116-080000.000.txt", 50<<10, 5*time.Hour)
	mk("freeze-20261116-120000.000.trace", 1<<10, 30*time.Minute) // within the budget, but its report is gone
	mk("notes.txt", 100, 30*24*time.Hour)
	r := &flightRecorder{dir: dir, keepAge: traceKeepAge, slowMax: 10 << 10, freezeMax: 10 << 10}
	r.prune("slow-20261116-130000.000.trace")
	want := "freeze-20261116-080000.000.txt freeze-20261116-090000.000.trace freeze-20261116-090000.000.txt freeze-20261116-110000.000.trace freeze-20261116-110000.000.txt notes.txt slow-20261116-120000.000.trace slow-20261116-130000.000.trace"
	if g := names(); g != want {
		t.Errorf("after pruning the traces:\n got %s\nwant %s", g, want)
	}

	// the reports' pruning takes a report's trace with it
	wd := newWatchdog(t.TempDir(), time.Now())
	wd.dir = dir
	mk("freeze-20261108-100000.000.txt", 100, 8*24*time.Hour) // too old
	mk("freeze-20261108-100000.000.trace", 100, 8*time.Hour)  // its trace, recent
	mk("freeze-20261116-140000.000.txt", 100, 0)              // the report just written
	wd.prune(filepath.Join(dir, "freeze-20261116-140000.000.txt"))
	if g := names(); strings.Contains(g, "20261108") || !strings.Contains(g, "freeze-20261116-140000.000.txt") || !strings.Contains(g, "freeze-20261116-110000.000.trace") {
		t.Errorf("after pruning the reports: %s", g)
	}
}

// The pacer writes the first slow task of a run of them, then after 1, 2,
// 4, … gaps up to its maximum, and starts over after a quiet spell.
func TestSlowPacer(t *testing.T) {
	p := slowPacer{base: time.Minute, max: 8 * time.Minute, quiet: 10 * time.Minute}
	t0 := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		at   time.Duration
		want bool
	}{
		{0, true},
		{30 * time.Second, false},
		{time.Minute, true}, // the gap is 2 min now
		{2 * time.Minute, false},
		{3 * time.Minute, true},  // 4 min
		{6 * time.Minute, false}, // turned down, but a slow task: the run goes on
		{7 * time.Minute, true},  // 8 min (the most)
		{15 * time.Minute, true},
		{22 * time.Minute, false},
		{23 * time.Minute, true},
		{34 * time.Minute, true}, // 11 min without a slow task: a new run, 1 min
		{35 * time.Minute, true},
		{36 * time.Minute, false},
	} {
		if got := p.allow(t0.Add(c.at)); got != c.want {
			t.Fatalf("a slow task at +%v: %v, want %v (gap %v)", c.at, got, c.want, p.gap)
		}
	}
}

// stuckWindow is a tracer that doesn't come back from WriteTo, or from
// Stop, until release closes: x/exp's recorder on Go 1.26 and newer
// (internal/tracerec's package comment).
type stuckWindow struct {
	release               chan struct{}
	writing               chan struct{} // closes when WriteTo begins
	once                  sync.Once
	stuckWrite, stuckStop bool
}

func (w *stuckWindow) Start() error { return nil }

func (w *stuckWindow) Stop() {
	if w.stuckStop {
		<-w.release
	}
}

func (w *stuckWindow) WriteTo(io.Writer) (int64, error) {
	w.once.Do(func() { close(w.writing) })
	if w.stuckWrite {
		<-w.release
	}
	return 0, nil
}

// Quitting never waits long on the recorder: with its tracer stuck in a
// trace being written, or in Stop, the watchdog's stop (the window's
// DestroyEvent calls it on the UI goroutine) returns after stopWait.
func TestFlightRecorderStuckQuit(t *testing.T) {
	for _, c := range []struct {
		name        string
		write, stop bool
	}{
		{"a trace being written", true, false},
		{"the tracer's Stop", false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := &stuckWindow{release: make(chan struct{}), writing: make(chan struct{}), stuckWrite: c.write, stuckStop: c.stop}
			old := newTraceWindow
			newTraceWindow = func(time.Duration, uint64) tracerec.Recorder { return w }
			defer func() { newTraceWindow = old }()
			wd := newWatchdog(t.TempDir(), time.Now())
			on := true
			<-wd.startRecorder(&on)
			rec := wd.rec.Load()
			if rec == nil {
				t.Fatal("the recorder didn't start")
			}
			if c.write {
				wd.begin(&stageLayout)
				time.Sleep(slowTrace + 20*time.Millisecond)
				wd.end()
				select {
				case <-w.writing:
				case <-time.After(10 * time.Second):
					t.Fatal("no trace began to be written")
				}
			}
			t0 := time.Now()
			stopped := make(chan struct{})
			go func() {
				wd.stop()
				close(stopped)
			}()
			select {
			case <-stopped:
			case <-time.After(stopWait + 10*time.Second):
				t.Fatalf("the watchdog's stop still waits on the stuck recorder after %v", time.Since(t0))
			}
			if d := time.Since(t0); d > stopWait+2*time.Second {
				t.Errorf("stop took %v with the recorder stuck (bound %v)", d, stopWait)
			}
			// let the stuck goroutines go, and finish with the directory
			close(w.release)
			select {
			case <-rec.done:
			case <-time.After(10 * time.Second):
				t.Fatal("the recorder's goroutine didn't end once its tracer came back")
			}
		})
	}
}

// The recorder is on unless turned off (flightRecorderDefault, measured):
// CLACK_FLIGHT_RECORDER=0 turns it off and =1 on, over ui.json's
// "flight_recorder", which counts only when set.
func TestFlightRecorderWanted(t *testing.T) {
	on, off := true, false
	for _, c := range []struct {
		env  string
		pref *bool
		want bool
	}{
		{"", nil, true},
		{"", &off, false},
		{"", &on, true},
		{"1", &off, true},
		{"yes", nil, true},
		{"0", &on, false},
		{"off", nil, false},
	} {
		t.Setenv("CLACK_FLIGHT_RECORDER", c.env)
		if got := flightRecorderWanted(c.pref); got != c.want {
			pref := "unset"
			if c.pref != nil {
				pref = fmt.Sprint(*c.pref)
			}
			t.Errorf("CLACK_FLIGHT_RECORDER=%q, ui.json %s: %v, want %v", c.env, pref, got, c.want)
		}
	}
	// a watchdog without reports (no config directory) never starts one
	wd := newWatchdog("", time.Now())
	t.Setenv("CLACK_FLIGHT_RECORDER", "1")
	<-wd.startRecorder(&on)
	if wd.rec.Load() != nil {
		wd.stop()
		t.Error("a recorder started with nowhere to write")
	}
}
