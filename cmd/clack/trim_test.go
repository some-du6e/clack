package main

import (
	"runtime/metrics"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/pointer"
)

// Forced collections are at least the gap apart: requests inside it
// coalesce into one run when it ends, and none is dropped.
func TestFreeOSLimiter(t *testing.T) {
	var runs atomic.Int32
	l := &freeOSLimiter{gap: 300 * time.Millisecond, run: func() { runs.Add(1) }}
	l.request() // runs at once
	if n := runs.Load(); n != 1 {
		t.Fatalf("first request: %d runs", n)
	}
	l.request() // inside the gap: deferred
	l.request() // coalesced with it
	l.request()
	if n := runs.Load(); n != 1 {
		t.Fatalf("requests inside the gap ran at once: %d runs", n)
	}
	deadline := time.Now().Add(5 * time.Second)
	for runs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runs.Load(); n != 2 {
		t.Fatalf("the deferred requests: %d runs, want 2", n)
	}
	time.Sleep(500 * time.Millisecond)
	if n := runs.Load(); n != 2 {
		t.Fatalf("coalesced requests ran %d times, want once", n-1)
	}
	l.request() // a gap after the deferred run: at once
	if n := runs.Load(); n != 3 {
		t.Fatalf("a request after the gap: %d runs, want 3", n)
	}
}

// The trim loop's metrics exist (a renamed metric would make it skip
// every check without a word).
func TestTrimMetricsExist(t *testing.T) {
	s := trimSamples()
	metrics.Read(s)
	if !allUint64(s) {
		for _, x := range s {
			t.Logf("%s: kind %v", x.Name, x.Value.Kind())
		}
		t.Fatal("a trim metric is missing")
	}
}

// A collection waits for the window to be quiet; requests meanwhile don't
// start a second one beside it, and the last of them still gets one after
// it (at the gap's end).
func TestFreeOSLimiterWaitsForQuiet(t *testing.T) {
	var runs atomic.Int32
	release := make(chan struct{})
	var waits atomic.Int32
	l := &freeOSLimiter{gap: 200 * time.Millisecond, run: func() { runs.Add(1) },
		quiet: func() bool {
			if waits.Add(1) == 1 {
				<-release // the first collection waits for a quiet window
			}
			return true
		}}
	go l.request()
	deadline := time.Now().Add(5 * time.Second)
	for waits.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	l.request() // while the first waits: no second collection now
	l.request()
	time.Sleep(300 * time.Millisecond)
	if n := runs.Load(); n != 0 {
		t.Fatalf("%d collections while the window was busy", n)
	}
	close(release)
	for runs.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runs.Load(); n != 2 {
		t.Fatalf("%d collections, want 2 (the waiting one, then one for the requests meanwhile)", n)
	}
	time.Sleep(400 * time.Millisecond)
	if n := runs.Load(); n != 2 {
		t.Fatalf("the requests made while it waited ran %d times, want once", n-1)
	}
}

// A collection that never found a pause is skipped, not forced at the
// deadline; the next request tries again.
func TestFreeOSLimiterSkipsWithoutQuiet(t *testing.T) {
	var runs atomic.Int32
	quiet := atomic.Bool{}
	l := &freeOSLimiter{gap: time.Hour, run: func() { runs.Add(1) }, quiet: func() bool { return quiet.Load() }}
	l.request()
	if n := runs.Load(); n != 0 {
		t.Fatalf("%d collections without a pause", n)
	}
	quiet.Store(true)
	l.request() // the skipped one used no gap
	if n := runs.Load(); n != 1 {
		t.Fatalf("%d collections once quiet, want 1", n)
	}
}

// waitQuiet keys on input, not frames: it returns once input stops for
// quietFor, and at quietMax gives up (false) while input continues.
func TestWaitQuietOnInput(t *testing.T) {
	defer func(f, m, b time.Duration) { quietFor, quietMax, quietBusy = f, m, b }(quietFor, quietMax, quietBusy)
	quietFor, quietMax, quietBusy = 300*time.Millisecond, 900*time.Millisecond, 150*time.Millisecond
	// input as a window gives it: events queued to a Gio router (Gio
	// patch 33 keeps the clock inputidle reads)
	give := func(r *input.Router) { r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse}) }

	var r1 input.Router
	give(&r1)
	t0 := time.Now()
	if !waitQuiet() {
		t.Fatal("no pause found after one input")
	}
	if d := time.Since(t0); d < 250*time.Millisecond {
		t.Fatalf("collected %v after input, before the pause (%v)", d, quietFor)
	}

	stop := make(chan struct{})
	give(&r1)   // the scrolling has begun before the request comes
	go func() { // and goes on the whole time
		var r2 input.Router
		for {
			select {
			case <-stop:
				return
			default:
				give(&r2)
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()
	t0 = time.Now()
	ok := waitQuiet()
	close(stop)
	if ok {
		t.Fatal("collected during continuous input")
	}
	if d := time.Since(t0); d < 850*time.Millisecond || d > 4*time.Second {
		t.Fatalf("gave up after %v, want about %v", d, quietMax)
	}
}
