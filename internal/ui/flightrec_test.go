package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	xtrace "golang.org/x/exp/trace"
)

// traceStats parses a trace the flight recorder wrote, as go tool trace
// would, and counts what's in it.
type traceStats struct {
	events      int
	transitions int // goroutine and proc state changes
	stw         int // stop-the-world ranges
	gc          int // GC mark phases
}

func readTrace(t *testing.T, path string) traceStats {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rd, err := xtrace.NewReader(bufio.NewReader(f))
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
	t.Logf("flightrec: %s: %.1f MB, %d events, %d state transitions, %d stop-the-world ranges, %d GC mark phases", filepath.Base(p), float64(fi.Size())/1e6, st.events, st.transitions, st.stw, st.gc)
	if st.transitions == 0 || st.stw == 0 || st.gc == 0 {
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
	if st := readTrace(t, p); st.transitions == 0 {
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

// Traces are pruned by age and by their own byte budget; freeze reports
// and other files are left to the reports' pruning.
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
	mk("slow-20261101-100000.000.trace", 100, 8*24*time.Hour) // too old
	mk("slow-20261116-100000.000.trace", 6<<10, 3*time.Hour)  // over the budget beside the newer ones
	mk("freeze-20261116-110000.000.trace", 3<<10, 2*time.Hour)
	mk("slow-20261116-120000.000.trace", 3<<10, time.Hour)
	mk("freeze-20261116-110000.000.txt", 50<<10, 2*time.Hour) // a report: not the traces' to prune
	mk("notes.txt", 100, 30*24*time.Hour)
	mk("slow-20261116-130000.000.trace", 3<<10, 0)
	r := &flightRecorder{dir: dir, keepAge: traceKeepAge, keepMax: 10 << 10}
	r.prune("slow-20261116-130000.000.trace")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ents {
		got = append(got, e.Name())
	}
	want := "freeze-20261116-110000.000.trace freeze-20261116-110000.000.txt notes.txt slow-20261116-120000.000.trace slow-20261116-130000.000.trace"
	if g := strings.Join(got, " "); g != want {
		t.Errorf("after pruning:\n got %s\nwant %s", g, want)
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
