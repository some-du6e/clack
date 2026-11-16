package main

import (
	"runtime/metrics"
	"sync/atomic"
	"testing"
	"time"
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
