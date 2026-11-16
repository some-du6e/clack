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

// An on-screen image's load jumps the queue only while it's on screen,
// which is judged by frames drawn, not the clock: on an idle window (a
// slow link, nothing new to draw) a load completing runs the next load
// before the window has drawn another frame, so "when was it last drawn"
// must be the last frame, not now. Here a gap of real time passes with no
// frame drawn, and the on-screen load still jumps the queue; by the clock
// it would look long off screen and lose to the backlog.
func TestHotQueueIdleGapKeepsOnScreen(t *testing.T) {
	release := make(chan struct{})
	started := make(chan string, 1000)
	c := New(func(_ context.Context, u string) ([]byte, error) {
		started <- u
		<-release
		return pngBytes(8, 8), nil
	}, "", nil)
	url := func(i int) string {
		return fmt.Sprintf("https://emoji.slack-edge.com/T1/e%04d/0123456789abcdef.png", i)
	}
	next := func(what string) string {
		t.Helper()
		select {
		case u := <-started:
			return u
		case <-time.After(30 * time.Second):
			t.Fatalf("%s: no load started", what)
			return ""
		}
	}
	const n = 100
	for i := 0; i < n; i++ { // a backlog, every worker busy
		c.Get(url(i), 16)
	}
	for k := 0; k < laneWorkers; k++ {
		next("the first loads")
	}
	on := url(1000)
	c.Get(on, 16) // queued behind the backlog...
	c.Get(on, 16) // ...and drawn again: on screen (hot)

	// an idle gap: wall time passes, but no frame is drawn (no Get), so the
	// on-screen image is on screen still
	time.Sleep(animGrace + 300*time.Millisecond)

	release <- struct{}{}
	if u := next("the freed worker"); u != on {
		t.Fatalf("after an idle gap the freed worker loaded %s, want the on-screen %s (demotion by frames, not the clock)", u, on)
	}
	close(release)
	for dl := time.Now().Add(5 * time.Second); c.Pending() > 0 && time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
	}
	if c.Pending() != 0 {
		t.Errorf("%d loads pending after the queue drained", c.Pending())
	}
}

// When the images on screen need more decoded bytes than the budget, each
// load evicts another one still drawn, which the next frame re-queues and
// promotes to hot. The hot queue mustn't serve those ahead of the cold
// queue for ever, or an image queued once (scrolled past, not redrawn)
// never loads. nextLocked takes the cold queue's head after hotRunMax hot
// loads in a row, so every queued image loads. The scheduler is driven
// directly here: a fresh on-screen image is re-queued hot every turn
// (the thrash), while one cold entry waits in the queue.
func TestHotQueueDoesNotStarveColdQueue(t *testing.T) {
	c := New(nil, "", nil)
	c.mu.Lock()
	defer c.mu.Unlock()
	const lane = laneSmall
	c.waiting = map[key]queued{}
	c.drawnAt = time.Now().UnixMilli()
	cold := key{"https://emoji.slack-edge.com/T1/cold/0123456789abcdef.png", 16}
	c.waiting[cold] = queued{asked: c.drawnAt}
	c.queue[lane] = []key{cold}
	hot := 0
	for i := 0; i < 10*hotRunMax; i++ {
		k := key{fmt.Sprintf("https://emoji.slack-edge.com/T1/h%04d/0123456789abcdef.png", i), 16}
		c.waiting[k] = queued{asked: c.drawnAt, hot: true}
		c.hot[lane] = append(c.hot[lane], k)
		got, ok := c.nextLocked(lane)
		if !ok {
			t.Fatal("the lane served nothing with work waiting")
		}
		if got == cold {
			if hot > hotRunMax {
				t.Fatalf("the cold entry waited behind %d hot loads, cap is %d", hot, hotRunMax)
			}
			return
		}
		hot++
		delete(c.waiting, got) // the on-screen one loaded
	}
	t.Fatalf("the cold entry never loaded behind a stream of hot ones (%d served)", hot)
}
