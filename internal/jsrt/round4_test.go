package jsrt

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// A setTimeout(0) yield loop the user's input started gives the queue a
// turn between its slices, as a browser's does: the user's lane passes to
// the input's own timers, not to the timers they set in turn. Fired back
// to back on the user's lane, such a loop (HaikuWarning compacting its
// dictionary after a send) held every plain task (React's scheduler, the
// plugins' renders, view reads) until its chain ran out, 10 s after Enter.
func TestUserYieldLoopLetsTheQueueRun(t *testing.T) {
	r := newTest(t)
	if _, err := r.Eval("loop.js", `globalThis.loop = {
		start(ms) {
			const end = Date.now() + ms
			const slice = () => {
				const t0 = Date.now()
				while (Date.now() - t0 < 2) {}
				if (Date.now() < end) setTimeout(slice, 0)
			}
			setTimeout(slice, 0)
		},
	}`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{{"send", UserChain(context.Background())}, {"click", UserBlocking(context.Background())}} {
		if _, err := r.Call(tc.ctx, "loop.start", 300); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond) // the loop is running
		t0 := time.Now()
		ran := make(chan time.Duration, 1)
		r.Post(func(*Ctx) { ran <- time.Since(t0) })
		d := <-ran
		t.Logf("%s: a plain task ran %v after it was queued, a 300 ms yield loop of 2 ms slices running", tc.name, d.Round(time.Millisecond))
		if d > 60*time.Millisecond {
			t.Errorf("%s: a plain task waited %v behind the user's yield loop", tc.name, d.Round(time.Millisecond))
		}
		time.Sleep(350 * time.Millisecond) // the loop ends
		_ = r.Do(func(*Ctx) error { return nil })
	}
}

// What comes in order with the user's input (a socket event, a store
// update) runs ahead of the queued renders, as Slack's store takes an
// event when it arrives and its renders follow: a user's chain that waits
// for one (Owl's view_opened, after its executeAppAction) waited behind
// every render queued before it.
func TestInOrderRunsAheadOfRenders(t *testing.T) {
	l := newLaneRecorder(t)
	g := l.hold("G", true)
	for i := 0; i < 4; i++ {
		l.post(l.r.Post, fmt.Sprintf("render%d", i))
	}
	l.post(l.r.PostInOrder, "view_opened")
	l.post(l.r.Post, "render4")
	l.post(l.r.PostInOrder, "store")
	close(g)
	l.drain()
	if got, want := l.got(), "G, view_opened, store, render0, render1, render2, render3, render4"; got != want {
		t.Errorf("ran %s,\nwant %s", got, want)
	}
}

// Rects and continuous events queued while the JS thread is busy wait as
// one task each, the newest: the layout as it is now, the pointer where it
// is now. Each frame's SetRects (every frame while scrolling) and mouse
// move queued a task of its own, and the user's next send ran behind a
// hundred stale ones.
func TestQueuedRectsAndMovesCoalesce(t *testing.T) {
	r := newTest(t)
	if _, err := r.Eval("count.js", `globalThis.__counts = { rects: 0, moves: 0, lastX: -1, keys: 0 }
		const setRects = __clack.setRects
		__clack.setRects = (rects) => { __counts.rects++; __counts.keys = Object.keys(rects).length; return setRects(rects) }
		const dispatch = __clack.dispatch
		__clack.dispatch = (k, e) => {
			if (e.type === 'mousemove') { __counts.moves++; __counts.lastX = e.clientX }
			return dispatch(k, e)
		}
		globalThis.counts = () => __counts`); err != nil {
		t.Fatal(err)
	}
	hold, running := make(chan struct{}), make(chan struct{})
	r.Post(func(*Ctx) {
		close(running)
		<-hold
	})
	<-running
	for i := 0; i < 100; i++ {
		r.SetRects(map[int]Rect{i: {X: float32(i), W: 10, H: 10}})
		ev, _ := json.Marshal(map[string]any{"type": "mousemove", "clientX": i, "clientY": 5})
		r.DispatchEvent(0, ev)
	}
	close(hold)
	_ = r.Do(func(*Ctx) error { return nil })
	raw, err := r.Call(context.Background(), "counts")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Rects, Moves, LastX, Keys int
	}
	_ = json.Unmarshal(raw, &c)
	if c.Rects != 1 || c.Keys != 100 {
		t.Errorf("%d setRects calls with %d boxes the last, want one with all 100", c.Rects, c.Keys)
	}
	if c.Moves != 1 || c.LastX != 99 {
		t.Errorf("%d mousemoves dispatched, the last at x=%d; want the newest alone (x=99)", c.Moves, c.LastX)
	}
}

// The collector's cleanups queue a handle release between two moves
// (releaseLater, on its own goroutine, after any collection): the moves
// still go out as one, the newest. A release runs no JS, so nothing sees
// the move run ahead of it; counted as a task queued after the move, it
// split them (train-163's push ci 45025576198: 2 mousemoves, the last at
// x=99, in TestQueuedRectsAndMovesCoalesce's 100).
func TestMovesCoalesceAcrossAHandleRelease(t *testing.T) {
	r := newTest(t)
	if _, err := r.Eval("count.js", `globalThis.__moves = []
		const dispatch = __clack.dispatch
		__clack.dispatch = (k, e) => { if (e.type === 'mousemove') __moves.push(e.clientX); return dispatch(k, e) }
		globalThis.moves = () => __moves`); err != nil {
		t.Fatal(err)
	}
	hs := leaked(t, r, 1)
	unblock := blockJS(t, r)
	move := func(x int) {
		ev, _ := json.Marshal(map[string]any{"type": "mousemove", "clientX": x, "clientY": 5})
		r.DispatchEvent(0, ev)
	}
	move(1)
	r.releaseLater(hs[0])
	move(2)
	unblock()
	_ = r.Do(func(*Ctx) error { return nil })
	raw, err := r.Call(context.Background(), "moves")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[2]" {
		t.Errorf("mousemoves dispatched at %s, want the newest alone ([2])", raw)
	}
}

// A drag's last pointermove, dispatched before its pointerup, runs before
// it however much plain work is queued: the pointerup goes on the user's
// lane ahead of the queue and takes the move queued before it along, as a
// browser dispatches input in order. Run behind the queue, the move came
// after the up, and a drag ended where its previous move had left it.
func TestMoveBeforeLaterUpUnderBacklog(t *testing.T) {
	r := newTest(t)
	if _, err := r.Eval("seen.js", `globalThis.__seen = []
		const dispatch = __clack.dispatch
		__clack.dispatch = (k, e) => { __seen.push(e.type); return dispatch(k, e) }
		globalThis.seen = () => __seen`); err != nil {
		t.Fatal(err)
	}
	hold, running := make(chan struct{}), make(chan struct{})
	r.Post(func(*Ctx) {
		close(running)
		<-hold
	})
	<-running
	for i := 0; i < 20; i++ { // the renders queued meanwhile
		r.Post(func(js *Ctx) {
			arr := js.Global().Get("__seen")
			_, _ = arr.Method("push", "render")
		})
	}
	move, _ := json.Marshal(map[string]any{"type": "pointermove", "clientX": 40, "clientY": 5, "buttons": 1})
	up, _ := json.Marshal(map[string]any{"type": "pointerup", "clientX": 40, "clientY": 5})
	r.DispatchEvent(0, move)
	r.DispatchEvent(0, up)
	close(hold)
	_ = r.Do(func(*Ctx) error { return nil })
	raw, err := r.Call(context.Background(), "seen")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	_ = json.Unmarshal(raw, &got)
	if len(got) < 2 || got[0] != "pointermove" || got[1] != "pointerup" {
		t.Errorf("ran %v: want the pointermove, then the pointerup, then the renders", got)
	}
}
