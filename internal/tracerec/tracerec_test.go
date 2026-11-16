package tracerec

import (
	"bytes"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// within runs f and fails the test if it hasn't returned after d: a
// deadlocked tracer must fail the test, not hang it.
func within(t *testing.T, d time.Duration, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(d):
		buf := make([]byte, 1<<20)
		t.Fatalf("%s didn't return in %v (%s, %s):\n%s", what, d, Impl, runtime.Version(), buf[:runtime.Stack(buf, true)])
	}
}

// goMinor is the minor version of the Go that built the test (0 for a
// development build).
func goMinor() int {
	v := strings.TrimPrefix(runtime.Version(), "go1.")
	if i := strings.IndexAny(v, ".rcbeta "); i >= 0 {
		v = v[:i]
	}
	n, _ := strconv.Atoi(v)
	return n
}

// busy keeps a few goroutines handing work around and collecting, so the
// trace has scheduling, blocking and GC events in it.
func busy(stop chan struct{}) *sync.WaitGroup {
	var wg sync.WaitGroup
	ch := make(chan []byte)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				case ch <- make([]byte, 64<<10):
				case b := <-ch:
					_ = b
				case <-time.After(time.Millisecond):
				}
			}
		}()
	}
	return &wg
}

// The recorder records, and keeps working across generations: the trace
// moves to a new generation every second, which is where x/exp's
// recorder deadlocks on Go 1.26 and newer (the package comment). A window
// written after a few of them is a trace (on Go 1.24 parsed here with
// x/exp/trace's reader: goroutines, a collection and its stop-the-world),
// and WriteTo and Stop return. Then a second recorder starts and stops:
// one may run at a time, again and again.
func TestRecorderWindow(t *testing.T) {
	if m := goMinor(); m >= 25 && Impl != "runtime/trace" {
		t.Fatalf("Go 1.%d builds %s's recorder: it deadlocks on Go 1.26 and newer", m, Impl)
	}
	t.Logf("tracerec: %s on %s", Impl, runtime.Version())
	rec := New(5*time.Second, 16<<20)
	if err := rec.Start(); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	wg := busy(stop)
	runtime.GC()
	time.Sleep(2500 * time.Millisecond) // two generation boundaries at least
	runtime.GC()
	var buf bytes.Buffer
	var n int64
	var err error
	within(t, 10*time.Second, "WriteTo", func() { n, err = rec.WriteTo(&buf) })
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(buf.Len()) || n < 1000 {
		t.Fatalf("WriteTo wrote %d bytes, said %d", buf.Len(), n)
	}
	head := buf.Bytes()[:16]
	var minor int
	if _, err := fmt.Sscanf(string(bytes.TrimRight(head, "\x00")), "go 1.%d trace", &minor); err != nil || minor < 22 {
		t.Fatalf("the window starts %q: not a Go 1.22+ trace", head)
	}
	t.Logf("tracerec: a window of %.1f MB, trace format go 1.%d", float64(n)/1e6, minor)
	if events, stw, gc, ok := parseWindow(t, buf.Bytes()); ok {
		t.Logf("tracerec: %d events, %d stop-the-world ranges, %d GC mark phases", events, stw, gc)
		if stw == 0 || gc == 0 {
			t.Errorf("the window lacks the collections: %d stop-the-world ranges, %d GC mark phases", stw, gc)
		}
	}
	within(t, 10*time.Second, "Stop", rec.Stop)

	again := New(time.Second, 1<<20)
	if err := again.Start(); err != nil {
		t.Fatalf("a recorder after the first one stopped: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)
	within(t, 10*time.Second, "the second recorder's WriteTo", func() { _, err = again.WriteTo(io.Discard) })
	if err != nil {
		t.Fatal(err)
	}
	within(t, 10*time.Second, "the second recorder's Stop", again.Stop)
}

// Only one recorder runs at a time: a second Start fails instead of
// sharing the tracer, and the first one goes on.
func TestRecorderOneAtATime(t *testing.T) {
	a := New(time.Second, 1<<20)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	defer within(t, 10*time.Second, "Stop", a.Stop)
	b := New(time.Second, 1<<20)
	if err := b.Start(); err == nil {
		b.Stop()
		t.Fatal("a second recorder started beside the first")
	}
	var err error
	within(t, 10*time.Second, "WriteTo", func() { _, err = a.WriteTo(io.Discard) })
	if err != nil {
		t.Fatalf("the first recorder after a second one failed to start: %v", err)
	}
}
