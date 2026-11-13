package imgcache

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// Images asked for faster than they load wait in their lane's queue, not
// in a goroutine each (the emoji picker scrolled through Hack Club's
// 65,000 emoji parked 30,000 goroutines, 250 MB of stacks): thousands of
// queued emoji start at most laneWorkers goroutines, load eight at a time
// in the order they were asked for, and every one of them arrives.
func TestQueuedLoadsDontParkGoroutines(t *testing.T) {
	release := make(chan struct{})
	var running, most atomic.Int32
	var order []string
	orderCh := make(chan string, 4096)
	c := New(func(ctx context.Context, url string) ([]byte, error) {
		n := running.Add(1)
		for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
		}
		<-release
		running.Add(-1)
		orderCh <- url
		return pngBytes(8, 8), nil
	}, "", nil)
	url := func(i int) string {
		return fmt.Sprintf("https://emoji.slack-edge.com/T1/e%04d/0123456789abcdef.png", i)
	}
	before := runtime.NumGoroutine()
	const n = 2000
	for i := 0; i < n; i++ {
		c.Get(url(i), 16)
	}
	if g := runtime.NumGoroutine() - before; g > laneWorkers+2 {
		t.Fatalf("%d goroutines for %d queued emoji (lane workers: %d)", g, n, laneWorkers)
	}
	if p := c.Pending(); p != n {
		t.Fatalf("Pending = %d, want %d", p, n)
	}
	close(release)
	deadline := time.After(30 * time.Second)
	for len(order) < n {
		select {
		case u := <-orderCh:
			order = append(order, u)
		case <-deadline:
			t.Fatalf("only %d of %d emoji loaded", len(order), n)
		}
	}
	if m := most.Load(); m > laneWorkers {
		t.Fatalf("%d loads ran at once, the lane runs %d", m, laneWorkers)
	}
	// FIFO, give or take the eight in flight at a time
	for i, u := range order {
		var j int
		fmt.Sscanf(u, "https://emoji.slack-edge.com/T1/e%04d/", &j)
		if j < i-laneWorkers || j > i+laneWorkers {
			t.Fatalf("load %d was emoji %d: not in the order asked", i, j)
		}
	}
	for dl := time.Now().Add(5 * time.Second); c.Pending() > 0 && time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
	}
	for i := 0; i < n; i++ {
		if _, ok := c.Get(url(i), 16); !ok {
			t.Fatalf("emoji %d isn't cached after loading", i)
		}
	}
	for dl := time.Now().Add(5 * time.Second); runtime.NumGoroutine() > before+1 && time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
	}
	if g := runtime.NumGoroutine() - before; g > 1 {
		t.Fatalf("%d goroutines left after the queue drained", g)
	}
}
