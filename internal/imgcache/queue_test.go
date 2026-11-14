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
//
// The order is checked one load at a time: each release lets exactly one
// of the eight blocked loads finish, so the next load to start is the one
// its worker took from the head of the queue, however the scheduler runs
// the workers. (Released all at once, a worker the scheduler held back
// between taking its image and starting the fetch started it out of
// place: 24 of 2000 on ci's linux-test, 2026-11-14.) The second half is
// then released at once, and the eight workers drain it together.
func TestQueuedLoadsDontParkGoroutines(t *testing.T) {
	const n = 2000
	release := make(chan struct{}, n)
	started := make(chan string, n) // the order loads start in
	var running, most atomic.Int32
	c := New(func(ctx context.Context, url string) ([]byte, error) {
		r := running.Add(1)
		for m := most.Load(); r > m && !most.CompareAndSwap(m, r); m = most.Load() {
		}
		started <- url
		<-release
		running.Add(-1)
		return pngBytes(8, 8), nil
	}, "", nil)
	url := func(i int) string {
		return fmt.Sprintf("https://emoji.slack-edge.com/T1/e%04d/0123456789abcdef.png", i)
	}
	index := func(u string) int {
		j := -1
		fmt.Sscanf(u, "https://emoji.slack-edge.com/T1/e%04d/", &j)
		return j
	}
	next := func(what string) int {
		t.Helper()
		select {
		case u := <-started:
			return index(u)
		case <-time.After(30 * time.Second):
			t.Fatalf("%s: no load started in 30 s", what)
			return -1
		}
	}
	before := runtime.NumGoroutine()
	for i := 0; i < n; i++ {
		c.Get(url(i), 16)
	}
	if g := runtime.NumGoroutine() - before; g > laneWorkers+2 {
		t.Fatalf("%d goroutines for %d queued emoji (lane workers: %d)", g, n, laneWorkers)
	}
	if p := c.Pending(); p != n {
		t.Fatalf("Pending = %d, want %d", p, n)
	}

	// the first eight: one per worker, the head of the queue (the workers
	// start them in any order)
	var first []int
	firstSet := map[int]bool{}
	for k := 0; k < laneWorkers; k++ {
		j := next("the lane's first loads")
		first = append(first, j)
		firstSet[j] = true
	}
	for i := 0; i < laneWorkers; i++ {
		if !firstSet[i] {
			t.Fatalf("the first %d loads started were %v, want 0 to %d", laneWorkers, first, laneWorkers-1)
		}
	}
	if r := running.Load(); r != laneWorkers {
		t.Fatalf("%d loads running, want the lane's %d", r, laneWorkers)
	}
	// one at a time: each release frees one worker, which starts the
	// image next in the queue
	for i := laneWorkers; i < n/2; i++ {
		release <- struct{}{}
		if j := next("a freed worker's next load"); j != i {
			t.Fatalf("load %d started when %d was next in the queue", j, i)
		}
		if r := running.Load(); r != laneWorkers {
			t.Fatalf("after load %d started: %d running, want the lane's %d", i, r, laneWorkers)
		}
	}
	// the rest at once: each of the queue's other images starts once
	close(release)
	rest := map[int]bool{}
	for k := n / 2; k < n; k++ {
		j := next("the rest of the queue")
		if j < n/2 || j >= n || rest[j] {
			t.Fatalf("load %d started again, or wasn't one of the %d still queued", j, n-n/2)
		}
		rest[j] = true
	}
	if m := most.Load(); m > laneWorkers {
		t.Fatalf("%d loads ran at once, the lane runs %d", m, laneWorkers)
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
