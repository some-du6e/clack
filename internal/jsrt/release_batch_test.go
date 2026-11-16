package jsrt

import (
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

func queued(r *Runtime) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queue)
}

// A collection's burst of Value cleanups is one task, not one per handle
// (perf-plugin-ui-thread: thousands of release tasks after each cycle
// held the queue's lock while the UI goroutine's posts waited, ~220 ms
// frames), and every handle in it is freed.
func TestReleaseBurstIsOneTask(t *testing.T) {
	r := newTest(t)
	const n = 20000
	hs := leaked(t, r, n)
	unblock := blockJS(t, r)
	before, freed := queued(r), r.released.Load()
	for _, h := range hs {
		r.releaseLater(h)
	}
	if got := queued(r) - before; got != 1 {
		t.Errorf("%d cleanups queued %d tasks, want 1", n, got)
	}
	unblock()
	if err := r.Do(func(*Ctx) error { return nil }); err != nil { // after everything queued
		t.Fatal(err)
	}
	if got := r.released.Load() - freed; got != n {
		t.Errorf("%d of %d handles freed", got, n)
	}
}

// Posts during a burst don't wait on it, and split it: a handle never
// joins a batch queued before a task posted since (that task might use
// the handle), so a release can't run ahead of a task queued before its
// cleanup.
func TestPostDuringReleaseBurst(t *testing.T) {
	r := newTest(t)
	hs := leaked(t, r, 20002)
	unblock := blockJS(t, r)
	before := queued(r)

	// order: the first handle's batch, then a task, then the second handle
	r.releaseLater(hs[0])
	var atTask atomic.Int64
	base := r.released.Load()
	r.Post(func(*Ctx) { atTask.Store(int64(r.released.Load() - base)) })
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
	tasks := queued(r) - before - 1 - posts // the task posted above, and the posts
	if tasks > posts+2 {
		t.Errorf("%d cleanups made %d release tasks beside %d posts: at most one more than the posts that split them", len(hs), tasks, posts)
	}
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
	if got := r.released.Load() - base; got != uint64(len(hs)) {
		t.Errorf("%d of %d handles freed", got, len(hs))
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
