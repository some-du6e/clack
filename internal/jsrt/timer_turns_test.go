package jsrt

import (
	"context"
	"sync"
	"testing"
	"time"
)

// A user's timer that comes due during a steady stream of in-order tasks
// (socket events, store updates) fires after the ones queued before it,
// and the plain queue (the renders, the views' reads) gets its turns
// meanwhile. Promoting every in-order task waiting, again after each
// batch, held the timer and every plain task until the stream paused.
func TestUserTimerLeavesThePlainQueueItsTurns(t *testing.T) {
	r := newTest(t)
	fired := make(chan time.Time, 1)
	r.Expose("timerFired", func(*Ctx, Value, []Value) (any, error) {
		select {
		case fired <- time.Now():
		default:
		}
		return nil, nil
	})
	if _, err := r.Eval("timer.js", `globalThis.lane = { timer() { setTimeout(() => __clack.timerFired(), 20) } }`); err != nil {
		t.Fatal(err)
	}
	// in-order tasks of 3 ms, one every 2 ms: the in-order queue never
	// runs dry while the stream goes on
	stop := make(chan struct{})
	var bg sync.WaitGroup
	bg.Add(1)
	go func() {
		defer bg.Done()
		tick := time.NewTicker(2 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				r.PostInOrder(func(*Ctx) { time.Sleep(3 * time.Millisecond) })
			}
		}
	}()
	defer func() {
		close(stop)
		bg.Wait()
		_ = r.Do(func(*Ctx) error { return nil })
	}()
	time.Sleep(40 * time.Millisecond)
	if _, err := r.Call(UserBlocking(context.Background()), "lane.timer"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond) // the timer comes due while the stream goes on
	t0 := time.Now()
	ran := make(chan struct{})
	r.Post(func(*Ctx) { close(ran) })
	select {
	case <-ran:
		if d := time.Since(t0); d > 800*time.Millisecond {
			t.Errorf("a plain task ran %v after it was queued, the in-order stream going on: the due user timer held the queue", d.Round(time.Millisecond))
		}
	case <-time.After(1500 * time.Millisecond):
		t.Error("a plain task never ran while the in-order stream went on: the due user timer held the queue")
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Error("the user's timer never fired while the in-order stream went on")
	}
}
