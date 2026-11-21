package jsrt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// seenEvents records, in JS, every event the host dispatches ("k:type",
// with "@clientX" and "/deltaY" when the event has them), and returns
// what JS saw so far.
func seenEvents(t *testing.T, r *Runtime) func() []string {
	t.Helper()
	if _, err := r.Eval("seen.js", `globalThis.__seen = []
		const dispatch = __clack.dispatch
		__clack.dispatch = (k, e) => {
			let s = k + ':' + e.type
			if (e.clientX !== undefined) s += '@' + e.clientX
			if (e.deltaY !== undefined) s += '/' + e.deltaY
			__seen.push(s)
			return dispatch(k, e)
		}
		globalThis.seen = () => __seen`); err != nil {
		t.Fatal(err)
	}
	return func() []string {
		t.Helper()
		_ = r.Do(func(*Ctx) error { return nil }) // the in-order tasks queued before it ran
		raw, err := r.Call(context.Background(), "seen")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		_ = json.Unmarshal(raw, &got)
		return got
	}
}

// holdJS keeps the JS thread busy until the returned func is called.
func holdJS(r *Runtime) func() {
	hold, running := make(chan struct{}), make(chan struct{})
	r.Post(func(*Ctx) {
		close(running)
		<-hold
	})
	<-running
	return func() { close(hold) }
}

func dispatchJSON(r *Runtime, target int, fields map[string]any) {
	ev, _ := json.Marshal(fields)
	r.DispatchEvent(target, ev)
}

// The boundary events queued behind a busy JS thread reach JS each in
// its place: a later one never stands in for an earlier one of the same
// box. Merged, leave, enter, leave came out as leave, enter: the box
// hovered with the pointer outside it (a Tooltip or a hover toolbar left
// open, as slackc's setHover sends nothing more), and enter, leave, enter
// un-hovered a hovered box.
func TestBoundaryEventsKeepTheirOrder(t *testing.T) {
	for _, seq := range [][]string{
		{"mouseleave", "mouseenter", "mouseleave"},
		{"mouseenter", "mouseleave", "mouseenter"},
		{"pointerleave", "pointerenter", "pointerleave"},
		{"mouseout", "mouseover", "mouseout"},
	} {
		t.Run(strings.Join(seq, ","), func(t *testing.T) {
			r := newTest(t)
			seen := seenEvents(t, r)
			release := holdJS(r)
			var want []string
			for _, typ := range seq {
				dispatchJSON(r, 7, map[string]any{"type": typ, "clientX": 5, "clientY": 5})
				want = append(want, "7:"+typ+"@5")
			}
			release()
			if got := seen(); strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("JS saw %v, want %v", got, want)
			}
		})
	}
}

// Moves to two boxes, alternating behind a busy JS thread, reach JS in
// the order they came; consecutive moves to one box still go as one, the
// newest. Coalesced across the other box's, A at 1, B at 2, A at 3 came
// out as A at 3, then B at 2, and a drag or a slider ended at 2.
func TestMovesAcrossTargetsKeepTheirOrder(t *testing.T) {
	r := newTest(t)
	seen := seenEvents(t, r)
	release := holdJS(r)
	for i, target := range []int{1, 2, 1, 2} {
		dispatchJSON(r, target, map[string]any{"type": "mousemove", "clientX": i + 1, "clientY": 5})
	}
	for x := 10; x <= 12; x++ {
		dispatchJSON(r, 1, map[string]any{"type": "mousemove", "clientX": x, "clientY": 5})
	}
	release()
	want := []string{"1:mousemove@1", "2:mousemove@2", "1:mousemove@3", "2:mousemove@4", "1:mousemove@12"}
	if got := seen(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("JS saw %v, want %v", got, want)
	}
}

// Wheel events queued behind a busy JS thread scroll as far as the user
// did: their deltas add up to the same total. Coalesced, n of them came out
// as one carrying the last delta (Chromium sums them).
func TestWheelDeltasAddUp(t *testing.T) {
	r := newTest(t)
	seen := seenEvents(t, r)
	release := holdJS(r)
	const n, each = 10, 3
	for i := 0; i < n; i++ {
		dispatchJSON(r, 4, map[string]any{"type": "wheel", "clientX": 5, "clientY": 5, "deltaX": 0, "deltaY": each})
	}
	release()
	got := seen()
	sum := 0
	for _, s := range got {
		var d int
		if _, err := fmt.Sscanf(s[strings.LastIndex(s, "/")+1:], "%d", &d); err == nil {
			sum += d
		}
	}
	if sum != n*each {
		t.Errorf("JS saw %v: wheel deltas summing to %d, want %d", got, sum, n*each)
	}
}
