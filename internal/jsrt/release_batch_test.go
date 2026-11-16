package jsrt

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// leaked makes n Values on the JS thread and returns their handles with
// the collector's cleanups stopped: the test frees them the way a cleanup
// does (releaseLater), without depending on when the collector gets there.
func leaked(t *testing.T, r *Runtime, n int) []uint32 {
	t.Helper()
	hs := make([]uint32, 0, n)
	if err := r.Do(func(js *Ctx) error {
		for i := 0; i < n; i++ {
			v, err := js.ParseJSON([]byte(`{"x":1}`))
			if err != nil {
				return err
			}
			v.ref.cleanup.Stop()
			hs = append(hs, v.ref.h)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return hs
}

// blockJS keeps the JS thread in a task until the returned func is
// called, so what gets queued meanwhile stays queued.
func blockJS(t *testing.T, r *Runtime) func() {
	t.Helper()
	release := make(chan struct{})
	started := make(chan struct{})
	r.Post(func(*Ctx) { close(started); <-release })
	<-started
	return func() { close(release) }
}

// freedWatch counts, from when it's made, how many of a test's handles the
// runtime frees, how many of those more than once, and how many batches
// freed any of them. r.released can't say: the runtime's own garbage
// (Values its start-up wrapped, collected whenever a cycle runs) frees
// other handles meanwhile, and joins the same batches (20018 of 20000
// once on CI).
type freedWatch struct {
	mu      sync.Mutex
	times   map[uint32]int
	freed   int
	twice   int
	batches int
}

func watchFreed(r *Runtime, hs []uint32) *freedWatch {
	w := &freedWatch{times: make(map[uint32]int, len(hs))}
	for _, h := range hs {
		w.times[h] = 0
	}
	f := func(b []uint32) {
		w.mu.Lock()
		defer w.mu.Unlock()
		mine := false
		for _, h := range b {
			n, ok := w.times[h]
			if !ok {
				continue
			}
			mine = true
			w.times[h] = n + 1
			switch n {
			case 0:
				w.freed++
			case 1:
				w.twice++
			}
		}
		if mine {
			w.batches++
		}
	}
	r.relSeen.Store(&f)
	return w
}

// counts is the test's handles freed so far, those freed more than once,
// and the batches that freed any of them.
func (w *freedWatch) counts() (freed, twice, batches int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.freed, w.twice, w.batches
}

// A collection's burst of Value cleanups is one task, not one per handle
// (perf-plugin-ui-thread: thousands of release tasks after each cycle
// held the queue's lock while the UI goroutine's posts waited, ~220 ms
// frames), and every handle in it is freed.
func TestReleaseBurstIsOneTask(t *testing.T) {
	r := newTest(t)
	const n = 20000
	hs := leaked(t, r, n)
	w := watchFreed(r, hs)
	unblock := blockJS(t, r)
	for _, h := range hs {
		r.releaseLater(h)
	}
	unblock()
	if err := r.Do(func(*Ctx) error { return nil }); err != nil { // after everything queued
		t.Fatal(err)
	}
	freed, twice, batches := w.counts()
	if freed != n || twice != 0 {
		t.Errorf("%d of %d handles freed (%d of them twice)", freed, n, twice)
	}
	// one task; two when a cleanup of the runtime's own garbage started a
	// batch while the burst's first was being queued (a batch takes
	// handles only once its task is queued last: releaseLater)
	if batches < 1 || batches > 2 {
		t.Errorf("%d cleanups were freed by %d tasks, want 1", n, batches)
	}
}

// Posts during a burst don't wait on it, and split it: a handle never
// joins a batch queued before a task posted since (that task might use
// the handle), so a release can't run ahead of a task queued before its
// cleanup.
func TestPostDuringReleaseBurst(t *testing.T) {
	r := newTest(t)
	hs := leaked(t, r, 20002)
	w := watchFreed(r, hs)
	unblock := blockJS(t, r)

	// order: the first handle's batch, then a task, then the second handle
	r.releaseLater(hs[0])
	var atTask atomic.Int64
	r.Post(func(*Ctx) { freed, _, _ := w.counts(); atTask.Store(int64(freed)) })
	r.releaseLater(hs[1])

	// the rest while another goroutine (the UI, say) keeps posting
	stop := make(chan struct{})
	done := make(chan int)
	var worst atomic.Int64
	go func() {
		k := 0
		for {
			select {
			case <-stop:
				done <- k
				return
			default:
			}
			t0 := time.Now()
			r.Post(func(*Ctx) {})
			if d := int64(time.Since(t0)); d > worst.Load() {
				worst.Store(d)
			}
			k++
			time.Sleep(100 * time.Microsecond)
		}
	}()
	for _, h := range hs[2:] {
		r.releaseLater(h)
	}
	close(stop)
	posts := <-done
	if d := time.Duration(worst.Load()); d > 50*time.Millisecond {
		t.Errorf("a Post waited %v during the burst", d)
	}
	unblock()
	if err := r.Do(func(*Ctx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := atTask.Load(); got != 1 {
		t.Errorf("when the task posted between them ran, %d handles were freed, want 1 (the second joined a batch queued before the task)", got)
	}
	freed, twice, batches := w.counts()
	if freed != len(hs) || twice != 0 {
		t.Errorf("%d of %d handles freed (%d of them twice)", freed, len(hs), twice)
	}
	// a batch per split: at most one more than the tasks posted during the
	// burst (the one between the first two handles too), plus one for a
	// garbage cleanup racing a batch being queued (TestReleaseBurstIsOneTask)
	if batches > posts+3 {
		t.Errorf("%d cleanups were freed by %d tasks beside %d posts: at most one more than the posts that split them", len(hs), batches, posts)
	}
}

// enqueue allocates nothing while the queue has room, so nothing it does
// under the lock can make it help the collector.
func TestEnqueueWithRoomAllocatesNothing(t *testing.T) {
	if raceOn {
		t.Skip("the race detector's instrumentation allocates")
	}
	r := newTest(t)
	unblock := blockJS(t, r)
	defer unblock()
	f := func() {}
	for i := 0; i < 1100; i++ { // grow it once (outside the lock)
		r.enqueue(task{kind: "test", f: f})
	}
	if a := testing.AllocsPerRun(200, func() { r.enqueue(task{kind: "test", f: f}) }); a != 0 {
		t.Errorf("enqueue with room: %.1f allocations", a)
	}
}
